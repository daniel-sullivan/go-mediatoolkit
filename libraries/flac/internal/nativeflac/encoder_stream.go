package nativeflac

import (
	"io"
	"strconv"
	"strings"
)

// This file is the family-D slice of the 1:1 port of
// libraries/flac/libflac/src/libFLAC/stream_encoder.c: the public
// process/init/finish entry surface plus the per-block pump
// (process_frame_) and the verify-decoder read/write callbacks.
//
// PORTING NOTE — shared state ownership.
// The FLAC__StreamEncoder / streamEncoderProtected / streamEncoderPrivate /
// StreamEncoderThreadTask state structs and the lifecycle helpers
// (set_defaults_, free_, resize_buffers_, init_stream_internal_, the file_*
// callbacks, append_to_verify_fifo_*) are owned by family A in
// encoder_state.go. Family B owns process_subframes_ (encoder_subframe.go);
// family C owns write_bitbuffer_ / update_metadata_ (encoder_frame.go).
// This file codes against the agreed names documented in the recon map:
//
//   type StreamEncoder struct { prot *streamEncoderProtected; priv *streamEncoderPrivate }
//   enc.prot.state                     FLAC__StreamEncoderState
//   enc.prot.channels/blocksize/...    encoder option fields
//   enc.priv.threadtask[0]             *StreamEncoderThreadTask (single-thread parity)
//   enc.priv.currentSampleNumber       uint32
//   enc.priv.currentFrameNumber        uint32
//   enc.priv.md5context                MD5Context
//   enc.priv.verify                    verify sub-state (input_fifo/output/error_stats)
//   enc.initStreamInternal(...)        init_stream_internal_
//   enc.setDefaults()                  set_defaults_
//   enc.free()                         free_
//   enc.resizeBuffers(n)               resize_buffers_
//   tt.processSubframes()/enc.processSubframes(tt)  process_subframes_  (family B)
//   enc.writeBitbuffer(tt, samples, isLast)         write_bitbuffer_    (family C)
//   enc.updateMetadata()               update_metadata_                 (family C)
//   appendToVerifyFifo / appendToVerifyFifoInterleaved (family A)
//
// Single-thread parity is the only target: every HAVE_PTHREAD branch
// (num_threads >= 2 path in process_frame_, process_frame_thread_*) is omitted.
// Ogg paths (FLAC__HAS_OGG == 0 in the vendored build) are ported as
// no-op/unsupported, keeping the structure.

// overreadConst mirrors stream_encoder.c:576 — `static const uint32_t
// OVERREAD_ = 1`. The process loop overreads exactly one sample so the
// fixed/LPC predictors can look one past the block; code below asserts on
// OVERREAD_ == 1 just as the C does.
const overreadConst = 1

// streamSyncLen mirrors FLAC__STREAM_SYNC_LENGTH (format.h:179) — the four
// bytes "fLaC" at the head of a native FLAC stream.
const streamSyncLen = 4

// The client-callback typedefs (FLAC__StreamEncoder{Write,Read,Seek,Tell,
// Metadata}Callback, stream_encoder.h:523-) are owned by family A in
// encoder_state.go as StreamEncoder{Write,Read,Seek,Tell,Metadata}Callback.
// The Init wrappers below code against those names so the callback shapes
// (multi-return tell/read) match the struct fields and init_stream_internal_.

// FLAC__stream_encoder_init_stream — stream_encoder.c:1442.
//
// InitStream wires the four client callbacks and delegates to
// init_stream_internal_ with is_ogg=false. The verify decoder (when
// enabled) is wired in init_stream_internal_ to verifyReadCallback /
// verifyWriteCallback defined in this file.
func (enc *StreamEncoder) InitStream(
	write StreamEncoderWriteCallback,
	seek StreamEncoderSeekCallback,
	tell StreamEncoderTellCallback,
	metadata StreamEncoderMetadataCallback,
	clientData any,
) StreamEncoderInitStatus {
	return enc.initStreamInternal(
		nil, // read_callback
		write,
		seek,
		tell,
		metadata,
		clientData,
		false, // is_ogg
	)
}

// FLAC__stream_encoder_init_ogg_stream — stream_encoder.c:1463.
//
// InitOggStream mirrors the Ogg entry wrapper. Ogg is not supported in the
// vendored single-config build (FLAC__HAS_OGG == 0); init_stream_internal_
// returns UNSUPPORTED_CONTAINER for is_ogg, matching the C ifdef-disabled
// behaviour.
func (enc *StreamEncoder) InitOggStream(
	read StreamEncoderReadCallback,
	write StreamEncoderWriteCallback,
	seek StreamEncoderSeekCallback,
	tell StreamEncoderTellCallback,
	metadata StreamEncoderMetadataCallback,
	clientData any,
) StreamEncoderInitStatus {
	return enc.initStreamInternal(
		read,
		write,
		seek,
		tell,
		metadata,
		clientData,
		true, // is_ogg
	)
}

// FLAC__stream_encoder_finish — stream_encoder.c:1625.
//
// Finish flushes the held partial block (process_frame_ with
// is_last_block=true), finalizes the MD5 signature into the scratch
// STREAMINFO, rewrites STREAMINFO/SEEKTABLE via update_metadata_, runs the
// verify decoder to completion, frees buffers, and resets the encoder to
// UNINITIALIZED. Returns false if any error occurred along the way.
//
// The pthread thread-finishing block (1652-1730) and the Ogg metadata path
// (1738-1742, 1766-1769) are omitted/no-op for single-thread, no-Ogg parity.
func (enc *StreamEncoder) Finish() bool {
	error_ := false

	if enc == nil {
		return false
	}

	if enc.prot.state == StreamEncoderUninitialized {
		// True in case set_metadata was used but init failed.
		if enc.prot.metadata != nil {
			enc.prot.metadata = nil
			enc.prot.numMetadataBlocks = 0
		}
		if enc.priv.file != nil {
			_ = enc.priv.file.Close()
			enc.priv.file = nil
		}
		return true
	}

	if enc.prot.state == StreamEncoderOK && !enc.priv.isBeingDeleted {
		ok := true
		// (pthread num_threads > 1 finish-threads block omitted: single-thread parity.)
		if ok && enc.priv.currentSampleNumber != 0 {
			enc.prot.blocksize = enc.priv.currentSampleNumber
			if !enc.resizeBuffers(enc.prot.blocksize) {
				// resizeBuffers sets the state for us on error.
				return false
			}
			if !enc.processFrame(true /* is_last_block */) {
				error_ = true
			}
		}
	}

	// (pthread thread-join block omitted: single-thread parity.)

	if enc.prot.doMD5 {
		enc.priv.streaminfo.StreamInfo.MD5Sum = enc.priv.md5context.Final()
	}

	if !enc.priv.isBeingDeleted {
		if enc.prot.state == StreamEncoderOK {
			if enc.priv.seekCallback != nil {
				// FLAC__HAS_OGG == 0: always the non-Ogg update_metadata_.
				enc.updateMetadata_()

				// Check if an error occurred while updating metadata.
				if enc.prot.state != StreamEncoderOK {
					error_ = true
				}
			}
			if enc.priv.metadataCallback != nil {
				enc.priv.metadataCallback(enc, &enc.priv.streaminfo, enc.priv.clientData)
			}
		}

		if enc.prot.verify && enc.priv.verify.decoder != nil && !enc.priv.verify.decoder.Finish() {
			if !error_ {
				enc.prot.state = StreamEncoderVerifyMismatchInAudioData
			}
			error_ = true
		}
	}

	if enc.priv.file != nil {
		_ = enc.priv.file.Close()
		enc.priv.file = nil
	}

	// (Ogg encoder-aspect finish omitted: FLAC__HAS_OGG == 0.)

	enc.free()
	setDefaults(enc)

	if !error_ {
		enc.prot.state = StreamEncoderUninitialized
	}

	return !error_
}

// FLAC__stream_encoder_set_ogg_serial_number — stream_encoder.c:1780.
//
// SetOggSerialNumber returns false in the no-Ogg build (FLAC__HAS_OGG == 0):
// the serial number cannot be set without Ogg support.
func (enc *StreamEncoder) SetOggSerialNumber(value int64) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	_ = value
	return false
}

// FLAC__stream_encoder_set_verify — stream_encoder.c:1797.
func (enc *StreamEncoder) SetVerify(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.verify = value
	return true
}

// FLAC__stream_encoder_set_streamable_subset — stream_encoder.c:1810.
func (enc *StreamEncoder) SetStreamableSubset(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.streamableSubset = value
	return true
}

// FLAC__stream_encoder_set_do_md5 — stream_encoder.c:1829.
func (enc *StreamEncoder) SetDoMD5(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.doMD5 = value
	return true
}

// FLAC__stream_encoder_set_channels — stream_encoder.c:1840.
func (enc *StreamEncoder) SetChannels(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.channels = value
	return true
}

// FLAC__stream_encoder_set_bits_per_sample — stream_encoder.c:1851.
func (enc *StreamEncoder) SetBitsPerSample(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.bitsPerSample = value
	return true
}

// FLAC__stream_encoder_set_sample_rate — stream_encoder.c:1862.
func (enc *StreamEncoder) SetSampleRate(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.sampleRate = value
	return true
}

// FLAC__stream_encoder_set_compression_level — stream_encoder.c:1873.
//
// SetCompressionLevel copies one CompressionLevels table row (table owned by
// family A) into the encoder's options, clamping the index to the last row.
// The apodization string is parsed by SetApodization, exactly as the C does
// via FLAC__stream_encoder_set_apodization.
func (enc *StreamEncoder) SetCompressionLevel(value uint32) bool {
	ok := true
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	if value >= uint32(len(compressionLevels)) {
		value = uint32(len(compressionLevels)) - 1
	}
	row := &compressionLevels[value]
	ok = enc.SetDoMidSideStereo(row.doMidSideStereo) && ok
	ok = enc.SetLooseMidSideStereo(row.looseMidSideStereo) && ok
	ok = enc.SetApodization(row.apodization) && ok
	ok = enc.SetMaxLPCOrder(row.maxLPCOrder) && ok
	ok = enc.SetQLPCoeffPrecision(row.qlpCoeffPrecision) && ok
	ok = enc.SetDoQLPCoeffPrecSearch(row.doQLPCoeffPrecSearch) && ok
	ok = enc.SetDoEscapeCoding(row.doEscapeCoding) && ok
	ok = enc.SetDoExhaustiveModelSearch(row.doExhaustiveModelSearch) && ok
	ok = enc.SetMinResidualPartitionOrder(row.minResidualPartitionOrder) && ok
	ok = enc.SetMaxResidualPartitionOrder(row.maxResidualPartitionOrder) && ok
	ok = enc.SetRiceParameterSearchDist(row.riceParameterSearchDist) && ok
	return ok
}

// FLAC__stream_encoder_set_blocksize — stream_encoder.c:1906.
func (enc *StreamEncoder) SetBlocksize(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.blocksize = value
	return true
}

// FLAC__stream_encoder_set_do_mid_side_stereo — stream_encoder.c:1917.
func (enc *StreamEncoder) SetDoMidSideStereo(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.doMidSideStereo = value
	return true
}

// FLAC__stream_encoder_set_loose_mid_side_stereo — stream_encoder.c:1928.
func (enc *StreamEncoder) SetLooseMidSideStereo(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.looseMidSideStereo = value
	return true
}

// FLAC__stream_encoder_set_apodization — stream_encoder.c:1940.
//
// SetApodization is a locale-independent parser of the libFLAC apodization
// specification string. It builds the apodizations[] array (capped at
// FLAC__MAX_APODIZATION_FUNCTIONS == 32). Window parameters use float32
// (FLAC__real) just as the C does, so window selection matches bit-for-bit.
// On an empty/all-invalid spec it falls back to a single tukey(0.5), exactly
// as the C does.
func (enc *StreamEncoder) SetApodization(specification string) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}

	enc.prot.numApodizations = 0
	// spec is the full remaining string (libFLAC's `specification`); tok is
	// only the current ';'-delimited token. The strncmp prefix tests use n
	// (token length) but strtod / strchr operate on the full remaining
	// `spec` exactly as the C does — a faithful quirk: a '/' or numeric
	// content in a *later* token can be consumed if the current token lacks
	// its own.
	spec := specification
	for {
		var tok string
		semi := strings.IndexByte(spec, ';')
		if semi >= 0 {
			tok = spec[:semi]
		} else {
			tok = spec
		}
		n := len(tok)

		switch {
		case n == 8 && tok == "bartlett":
			enc.appendApodizationType(ApodizationBartlett)
		case n == 13 && tok == "bartlett_hann":
			enc.appendApodizationType(ApodizationBartlettHann)
		case n == 8 && tok == "blackman":
			enc.appendApodizationType(ApodizationBlackman)
		case n == 26 && tok == "blackman_harris_4term_92db":
			enc.appendApodizationType(ApodizationBlackmanHarris4Term92dBSidelobe)
		case n == 6 && tok == "connes":
			enc.appendApodizationType(ApodizationConnes)
		case n == 7 && tok == "flattop":
			enc.appendApodizationType(ApodizationFlattop)
		case n > 7 && strings.HasPrefix(tok, "gauss("):
			stddev := float32(strtod(spec[6:]))
			if stddev > 0.0 && stddev <= 0.5 {
				a := &enc.prot.apodizations[enc.prot.numApodizations]
				a.Gauss.StdDev = stddev
				enc.appendApodizationType(ApodizationGauss)
			}
		case n == 7 && tok == "hamming":
			enc.appendApodizationType(ApodizationHamming)
		case n == 4 && tok == "hann":
			enc.appendApodizationType(ApodizationHann)
		case n == 13 && tok == "kaiser_bessel":
			enc.appendApodizationType(ApodizationKaiserBessel)
		case n == 7 && tok == "nuttall":
			enc.appendApodizationType(ApodizationNuttall)
		case n == 9 && tok == "rectangle":
			enc.appendApodizationType(ApodizationRectangle)
		case n == 8 && tok == "triangle":
			enc.appendApodizationType(ApodizationTriangle)
		case n > 7 && strings.HasPrefix(tok, "tukey("):
			p := float32(strtod(spec[6:]))
			if p >= 0.0 && p <= 1.0 {
				a := &enc.prot.apodizations[enc.prot.numApodizations]
				a.Tukey.P = p
				enc.appendApodizationType(ApodizationTukey)
			}
		case n > 15 && strings.HasPrefix(tok, "partial_tukey("):
			enc.parseMultipleTukey(spec, true /* partial */)
		case n > 16 && strings.HasPrefix(tok, "punchout_tukey("):
			enc.parseMultipleTukey(spec, false /* punchout */)
		case n > 17 && strings.HasPrefix(tok, "subdivide_tukey("):
			parts := int32(strtod(spec[16:]))
			if parts > 1 {
				p := float32(5e-1)
				if si1 := strings.IndexByte(spec, '/'); si1 >= 0 {
					p = float32(strtod(spec[si1+1:]))
				}
				if p > 1 {
					p = 1
				} else if p < 0 {
					p = 0
				}
				a := &enc.prot.apodizations[enc.prot.numApodizations]
				a.SubdivideTukey.Parts = parts
				a.SubdivideTukey.P = p / float32(parts)
				enc.appendApodizationType(ApodizationSubdivideTukey)
			}
		case n == 5 && tok == "welch":
			enc.appendApodizationType(ApodizationWelch)
		}

		if enc.prot.numApodizations == 32 {
			break
		}
		if semi >= 0 {
			spec = spec[semi+1:]
		} else {
			break
		}
	}

	if enc.prot.numApodizations == 0 {
		enc.prot.numApodizations = 1
		enc.prot.apodizations[0].Type = ApodizationTukey
		enc.prot.apodizations[0].Tukey.P = 0.5
	}
	return true
}

// appendApodizationType stores the current apodization type and bumps
// num_apodizations, mirroring the `apodizations[num_apodizations++].type = X`
// idiom in FLAC__stream_encoder_set_apodization.
func (enc *StreamEncoder) appendApodizationType(t ApodizationFunction) {
	enc.prot.apodizations[enc.prot.numApodizations].Type = t
	enc.prot.numApodizations++
}

// parseMultipleTukey reproduces the partial_tukey(/punchout_tukey( branches of
// FLAC__stream_encoder_set_apodization (stream_encoder.c:1993-2034). spec is
// the full remaining specification string (libFLAC's `specification`); strtod
// and strchr operate on it, matching the C. The two branches differ only in
// the default overlap (0.1 for partial, 0.2 for punchout) and the resulting
// apodization type.
func (enc *StreamEncoder) parseMultipleTukey(spec string, partial bool) {
	var prefixLen int
	var defaultOverlap float32
	var typ ApodizationFunction
	if partial {
		prefixLen = 14
		defaultOverlap = 0.1
		typ = ApodizationPartialTukey
	} else {
		prefixLen = 15
		defaultOverlap = 0.2
		typ = ApodizationPunchoutTukey
	}

	tukeyParts := int32(strtod(spec[prefixLen:]))

	si1 := strings.IndexByte(spec, '/')
	overlap := defaultOverlap
	if si1 >= 0 {
		overlap = min(float32(strtod(spec[si1+1:])), 0.99)
	}
	overlapUnits := 1.0/(1.0-overlap) - 1.0

	// si_2 = strchr((si_1?(si_1+1):specification), '/')
	var rest string
	if si1 >= 0 {
		rest = spec[si1+1:]
	} else {
		rest = spec
	}
	tukeyP := float32(0.2)
	if si2 := strings.IndexByte(rest, '/'); si2 >= 0 {
		tukeyP = float32(strtod(rest[si2+1:]))
	}

	if tukeyParts <= 1 {
		a := &enc.prot.apodizations[enc.prot.numApodizations]
		a.Tukey.P = tukeyP
		enc.appendApodizationType(ApodizationTukey)
	} else if int32(enc.prot.numApodizations)+tukeyParts < 32 {
		for m := int32(0); m < tukeyParts; m++ {
			a := &enc.prot.apodizations[enc.prot.numApodizations]
			a.MultipleTukey.P = tukeyP
			a.MultipleTukey.Start = float32(m) / (float32(tukeyParts) + overlapUnits)
			a.MultipleTukey.End = (float32(m) + 1 + overlapUnits) / (float32(tukeyParts) + overlapUnits)
			enc.appendApodizationType(typ)
		}
	}
}

// FLAC__stream_encoder_set_max_lpc_order — stream_encoder.c:2067.
func (enc *StreamEncoder) SetMaxLPCOrder(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.maxLPCOrder = value
	return true
}

// FLAC__stream_encoder_set_qlp_coeff_precision — stream_encoder.c:2078.
func (enc *StreamEncoder) SetQLPCoeffPrecision(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.qlpCoeffPrecision = value
	return true
}

// FLAC__stream_encoder_set_do_qlp_coeff_prec_search — stream_encoder.c:2089.
func (enc *StreamEncoder) SetDoQLPCoeffPrecSearch(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.doQLPCoeffPrecSearch = value
	return true
}

// FLAC__stream_encoder_set_do_escape_coding — stream_encoder.c:2100.
//
// SetDoEscapeCoding is a no-op in the vendored build: escape coding was
// deprecated and the value is discarded unless FUZZING_BUILD_MODE is set
// (it is not). The setter still returns true.
func (enc *StreamEncoder) SetDoEscapeCoding(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	_ = value
	return true
}

// FLAC__stream_encoder_set_do_exhaustive_model_search — stream_encoder.c:2118.
func (enc *StreamEncoder) SetDoExhaustiveModelSearch(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.doExhaustiveModelSearch = value
	return true
}

// FLAC__stream_encoder_set_min_residual_partition_order — stream_encoder.c:2129.
func (enc *StreamEncoder) SetMinResidualPartitionOrder(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.minResidualPartitionOrder = value
	return true
}

// FLAC__stream_encoder_set_max_residual_partition_order — stream_encoder.c:2140.
func (enc *StreamEncoder) SetMaxResidualPartitionOrder(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.maxResidualPartitionOrder = value
	return true
}

// FLAC__stream_encoder_set_num_threads — stream_encoder.c:2151.
//
// SetNumThreads returns NOT_COMPILED_WITH_MULTITHREADING_ENABLED: the pure-Go
// port is single-threaded, matching the !HAVE_PTHREAD branch of the C.
func (enc *StreamEncoder) SetNumThreads(value uint32) StreamEncoderSetNumThreadsStatus {
	_ = value
	return EncoderSetNumThreadsNotCompiledWithMultithreadingEnabled
}

// FLAC__stream_encoder_set_rice_parameter_search_dist — stream_encoder.c:2174.
//
// SetRiceParameterSearchDist is a no-op in the vendored build (the value is
// discarded under the #if 0); the setter still returns true.
func (enc *StreamEncoder) SetRiceParameterSearchDist(value uint32) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	_ = value
	return true
}

// FLAC__stream_encoder_set_total_samples_estimate — stream_encoder.c:2190.
func (enc *StreamEncoder) SetTotalSamplesEstimate(value uint64) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	// flac_min(value, (1 << FLAC__STREAM_METADATA_STREAMINFO_TOTAL_SAMPLES_LEN) - 1)
	const maxTotalSamples = (uint64(1) << StreamInfoTotalSamplesLen) - 1
	if value > maxTotalSamples {
		value = maxTotalSamples
	}
	enc.prot.totalSamplesEstimate = value
	return true
}

// FLAC__stream_encoder_set_metadata — stream_encoder.c:2202.
//
// SetMetadata copies the caller's metadata block pointer slice. As in the C,
// nil or zero-length collapses to no metadata. The slice is copied (not
// aliased) so later caller mutation of the slice header does not affect the
// encoder, matching the malloc+memcpy of the C.
func (enc *StreamEncoder) SetMetadata(metadata []*StreamMetadata) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	numBlocks := uint32(len(metadata))
	if metadata == nil {
		numBlocks = 0
	}
	if numBlocks == 0 {
		metadata = nil
	}
	// realloc()-equivalent: drop any prior copy first.
	enc.prot.metadata = nil
	enc.prot.numMetadataBlocks = 0
	if numBlocks != 0 {
		m := make([]*StreamMetadata, numBlocks)
		copy(m, metadata)
		enc.prot.metadata = m
		enc.prot.numMetadataBlocks = numBlocks
	}
	return true
}

// FLAC__stream_encoder_set_limit_min_bitrate — stream_encoder.c:2234.
func (enc *StreamEncoder) SetLimitMinBitrate(value bool) bool {
	if enc.prot.state != StreamEncoderUninitialized {
		return false
	}
	enc.prot.limitMinBitrate = value
	return true
}

// FLAC__stream_encoder_process — stream_encoder.c:2513.
//
// Process buffers planar (per-channel) input into blocks. It optionally feeds
// the verify FIFO, range-checks every sample against the bits-per-sample
// window (setting CLIENT_ERROR on violation), copies into threadtask[0]'s
// integer_signal with the OVERREAD_-by-1 semantics (i <= blocksize), and when
// a full block + 1 overread sample is buffered, emits a frame via
// process_frame_ then shifts the overread sample to index 0 and resets
// current_sample_number to 1.
func (enc *StreamEncoder) Process(buffer [][]int32, samples uint32) bool {
	channels := enc.prot.channels
	blocksize := enc.prot.blocksize
	// INT32_MAX >> (32 - bps) and INT32_MIN >> (32 - bps).
	shift := 32 - enc.prot.bitsPerSample
	sampleMax := int32(int32(0x7fffffff) >> shift)
	sampleMin := int32(int32(-0x80000000) >> shift)

	if enc.prot.state != StreamEncoderOK {
		return false
	}

	var j uint32 = 0
	tt := enc.priv.threadtask[0]
	for {
		n := min(blocksize+overreadConst-enc.priv.currentSampleNumber, samples-j)

		if enc.prot.verify {
			appendToVerifyFifo(&enc.priv.verify.inputFifo, buffer, j, channels, n)
		}

		for channel := uint32(0); channel < channels; channel++ {
			if buffer[channel] == nil {
				return false
			}
			for i, k := enc.priv.currentSampleNumber, j; i <= blocksize && k < samples; i, k = i+1, k+1 {
				if buffer[channel][k] < sampleMin || buffer[channel][k] > sampleMax {
					enc.prot.state = StreamEncoderClientError
					return false
				}
			}
			// memcpy(&integer_signal[channel][current_sample_number], &buffer[channel][j], n)
			copy(tt.integerSignal[channel][enc.priv.currentSampleNumber:enc.priv.currentSampleNumber+n], buffer[channel][j:j+n])
		}
		j += n
		enc.priv.currentSampleNumber += n

		// We only process if we have a full block + 1 extra sample; the final
		// block is always handled by Finish().
		if enc.priv.currentSampleNumber > blocksize {
			// FLAC__ASSERT(current_sample_number == blocksize+OVERREAD_)
			if !enc.processFrame(false /* is_last_block */) {
				return false
			}
			// Move unprocessed overread samples to beginnings of arrays.
			for channel := uint32(0); channel < channels; channel++ {
				tt.integerSignal[channel][0] = tt.integerSignal[channel][blocksize]
			}
			enc.priv.currentSampleNumber = 1
		}

		if j >= samples {
			break
		}
	}

	return true
}

// FLAC__stream_encoder_process_interleaved — stream_encoder.c:2564.
//
// ProcessInterleaved is the interleaved counterpart of Process: the input is a
// single channel-interleaved buffer of `samples` wide-samples. The buffering,
// range clamp, OVERREAD_-by-1 semantics, frame emission and overread shift
// match Process exactly.
func (enc *StreamEncoder) ProcessInterleaved(buffer []int32, samples uint32) bool {
	channels := enc.prot.channels
	blocksize := enc.prot.blocksize
	shift := 32 - enc.prot.bitsPerSample
	sampleMax := int32(int32(0x7fffffff) >> shift)
	sampleMin := int32(int32(-0x80000000) >> shift)

	if enc.prot.state != StreamEncoderOK {
		return false
	}

	var j, k uint32 = 0, 0
	tt := enc.priv.threadtask[0]
	for {
		if enc.prot.verify {
			appendToVerifyFifoInterleaved(&enc.priv.verify.inputFifo, buffer, j, channels,
				min(blocksize+overreadConst-enc.priv.currentSampleNumber, samples-j))
		}

		// "i <= blocksize" to overread 1 sample.
		i := enc.priv.currentSampleNumber
		for ; i <= blocksize && j < samples; i, j = i+1, j+1 {
			for channel := uint32(0); channel < channels; channel++ {
				if buffer[k] < sampleMin || buffer[k] > sampleMax {
					enc.prot.state = StreamEncoderClientError
					return false
				}
				tt.integerSignal[channel][i] = buffer[k]
				k++
			}
		}
		enc.priv.currentSampleNumber = i

		// We only process if we have a full block + 1 extra sample; the final
		// block is always handled by Finish().
		if i > blocksize {
			if !enc.processFrame(false /* is_last_block */) {
				return false
			}
			// Move unprocessed overread samples to beginnings of arrays.
			for channel := uint32(0); channel < channels; channel++ {
				tt.integerSignal[channel][0] = tt.integerSignal[channel][blocksize]
			}
			enc.priv.currentSampleNumber = 1
		}

		if j >= samples {
			break
		}
	}

	return true
}

// process_frame_ — stream_encoder.c:3423.
//
// processFrame emits one frame for threadtask[0]: accumulate the raw block to
// the MD5 signature, build the frame header + subframes into the frame
// bitbuffer (process_subframes_, family B), zero-pad to a byte boundary,
// append the CRC-16 footer, and write the frame (write_bitbuffer_, family C).
// It then advances current_frame_number and the running total_samples.
//
// Only the num_threads < 2 || is_last_block branch is ported (single-thread
// parity); the pthread else-branch (3478-3602) and process_frame_thread_* are
// omitted. For the non-threaded case the C accumulates MD5 here (3436), so we
// do too.
func (enc *StreamEncoder) processFrame(isLastBlock bool) bool {
	_ = isLastBlock
	tt := enc.priv.threadtask[0]

	// num_threads < 2 || is_last_block — always true here (single-thread).

	// Accumulate raw signal to the MD5 signature.
	if enc.prot.doMD5 && !enc.priv.md5context.Accumulate(
		tt.integerSignal[:enc.prot.channels],
		enc.prot.channels,
		enc.prot.blocksize,
		(enc.prot.bitsPerSample+7)/8,
	) {
		enc.prot.state = StreamEncoderMemoryAllocationError
		return false
	}

	// Process the frame header and subframes into the frame bitbuffer.
	tt.currentFrameNumber = enc.priv.currentFrameNumber
	if !enc.processSubframes_(tt) {
		// processSubframes sets the state for us on error.
		return false
	}

	// Zero-pad the frame to a byte boundary.
	if !tt.frame.ZeroPadToByteBoundary() {
		enc.prot.state = StreamEncoderMemoryAllocationError
		return false
	}

	// CRC-16 the whole thing.
	// FLAC__ASSERT(FLAC__bitwriter_is_byte_aligned(tt.frame))
	crc, ok := tt.frame.GetWriteCRC16()
	if !ok || !tt.frame.WriteRawUint32(uint32(crc), FrameFooterCRCLen) {
		enc.prot.state = StreamEncoderMemoryAllocationError
		return false
	}

	// Write it.
	if !enc.writeBitbuffer_(tt, enc.prot.blocksize, isLastBlock) {
		// writeBitbuffer sets the state for us on error.
		return false
	}

	// Get ready for the next frame.
	enc.priv.currentSampleNumber = 0
	enc.priv.currentFrameNumber++
	enc.priv.streaminfo.StreamInfo.TotalSamples += uint64(enc.prot.blocksize)

	return true
}

// verify_read_callback_ — stream_encoder.c:5143.
//
// verifyReadCallback feeds the verify decoder from verify.output.data. On the
// first call after a metadata block, needs_magic_hack prepends the "fLaC"
// stream sync (the encoder skips re-emitting the magic to the verify decoder
// otherwise). It satisfies the io.Reader contract used by the native
// Decoder.InitStream: n is the number of bytes supplied, and a zero-length
// supply on FIFO underflow surfaces as an error so the decoder aborts (the C
// returns ABORT in that case).
//
// PORTING NOTE — the native Decoder reads its input through an io.Reader
// (decoder_state.go) rather than libFLAC's *bytes read callback. Family A is
// expected to wire enc.verifyReadCallback as the io.Reader handed to the
// verify decoder's InitStream. The remaining-output bookkeeping lives on
// verify.output: data is the *remaining* encoded bytes (resliced forward as
// the decoder consumes), and bytes mirrors len(data).
func (enc *StreamEncoder) verifyReadCallback(buffer []byte) (n int, err error) {
	out := &enc.priv.verify.output

	if enc.priv.verify.needsMagicHack {
		// FLAC__ASSERT(len(buffer) >= FLAC__STREAM_SYNC_LENGTH)
		n = streamSyncLen
		copy(buffer[:n], streamSyncString[:])
		enc.priv.verify.needsMagicHack = false
		return n, nil
	}

	encodedBytes := out.bytes
	if encodedBytes == 0 {
		// A FIFO underflow occurred, which means a bug somewhere. The C
		// returns FLAC__STREAM_DECODER_READ_STATUS_ABORT; surface as an
		// error so the io.Reader-driven decoder aborts.
		return 0, io.ErrUnexpectedEOF
	}
	n = len(buffer)
	if encodedBytes < uint32(n) {
		n = int(encodedBytes)
	}
	copy(buffer[:n], out.data[:n])
	// output.data += n; output.bytes -= n
	out.data = out.data[n:]
	out.bytes -= uint32(n)
	return n, nil
}

// verify_write_callback_ — stream_encoder.c:5174.
//
// verifyWriteCallback compares each decoded block sample-by-sample against the
// head of the verify input FIFO. On the first mismatch it records error_stats
// (absolute sample, frame number, channel, sample index, expected, got) and
// sets VERIFY_MISMATCH_IN_AUDIO_DATA, aborting the decode. On a clean block it
// dequeues the block from the FIFO (memmove the tail down). It matches the
// native DecoderWriteCallback signature so Family A can wire it directly into
// the verify decoder's InitStream.
func (enc *StreamEncoder) verifyWriteCallback(frame *FrameHeader, buffer [][]int32) DecoderWriteStatus {
	channels := frame.Channels
	blocksize := frame.Blocksize

	if enc.prot.state == StreamEncoderVerifyDecoderError {
		// Set when verify_error_callback_ fired.
		return DecoderWriteAbort
	}

	for channel := uint32(0); channel < channels; channel++ {
		fifo := enc.priv.verify.inputFifo.data[channel]
		mismatch := false
		var sample uint32
		var expect, got int32
		for i := uint32(0); i < blocksize; i++ {
			if buffer[channel][i] != fifo[i] {
				sample = i
				expect = fifo[i]
				got = buffer[channel][i]
				mismatch = true
				break
			}
		}
		if mismatch {
			// FLAC__ASSERT(number_type == SAMPLE_NUMBER)
			enc.priv.verify.errorStats.absoluteSample = frame.Number + uint64(sample)
			enc.priv.verify.errorStats.frameNumber = uint32(frame.Number / uint64(blocksize))
			enc.priv.verify.errorStats.channel = channel
			enc.priv.verify.errorStats.sample = sample
			enc.priv.verify.errorStats.expected = expect
			enc.priv.verify.errorStats.got = got
			enc.prot.state = StreamEncoderVerifyMismatchInAudioData
			return DecoderWriteAbort
		}
	}

	// Dequeue the frame from the FIFO.
	enc.priv.verify.inputFifo.tail -= blocksize
	tail := enc.priv.verify.inputFifo.tail
	for channel := uint32(0); channel < channels; channel++ {
		d := enc.priv.verify.inputFifo.data[channel]
		copy(d[0:tail], d[blocksize:blocksize+tail])
	}
	return DecoderWriteContinue
}

// verify_error_callback_ — stream_encoder.c:5226.
//
// verifyErrorCallback sets VERIFY_DECODER_ERROR when the verify decoder
// reports an error; the next verifyWriteCallback then aborts. It matches the
// native DecoderErrorCallback signature.
func (enc *StreamEncoder) verifyErrorCallback(status DecoderErrorStatus) {
	_ = status
	enc.prot.state = StreamEncoderVerifyDecoderError
}

// strtod mirrors C's strtod(s, /*endptr=*/0): it parses the longest leading
// prefix of s that forms a valid C floating-point literal and returns its
// value, or 0.0 if no conversion can be performed. The apodization parser
// relies on this lenient prefix behaviour (e.g. "5e-1)" parses to 0.5, and a
// non-numeric tail is ignored). Locale is irrelevant — the radix is always
// '.', matching libFLAC's locale-independent parser.
func strtod(s string) float64 {
	// Skip leading ASCII whitespace as C strtod does.
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' || s[i] == '\f' || s[i] == '\v') {
		i++
	}
	start := i

	// Optional sign.
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}

	// Mantissa: digits, optional '.', digits.
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		// No mantissa digits: not a number (C strtod hex/inf/nan forms are
		// not produced by the apodization specs, so we treat as 0).
		return 0
	}

	// Optional exponent: only consume it if well-formed, otherwise leave it
	// for the caller's tail (matching strtod's endptr placement).
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		expDigits := 0
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
			expDigits++
		}
		if expDigits > 0 {
			i = j
		}
	}

	v, err := strconv.ParseFloat(s[start:i], 64)
	if err != nil {
		return 0
	}
	return v
}
