package nativeflac

// This file is the family-C slice of the 1:1 port of
// libflac/src/libFLAC/stream_encoder.c — the frame/metadata *writing*
// glue: write_bitbuffer_, write_frame_, update_metadata_,
// update_ogg_metadata_, and add_subframe_.
//
// It mutates the shared FLAC__StreamEncoder-equivalent state owned by
// family A (encoder_state.go) and the per-block ThreadTask owned there;
// it calls into the framing writers from encode_framing.go
// (SubframeAddConstant/Fixed/LPC/Verbatim) and into family D's
// write/seek/tell callbacks held on the private state.
//
// Single-thread parity target: threadtask[0] only; Ogg paths are
// compiled out in the vendored build (FLAC__HAS_OGG == 0) and are ported
// here as unsupported / no-op while preserving structure and signatures.

// Metadata-layout byte offsets used by update_metadata_. These mirror
// FLAC__STREAM_METADATA_HEADER_LENGTH (format.h:872 == 4) and the
// STREAMINFO field lengths in format.go (which already match
// format.h:546-553). Repeated here as the encoder's own derived offsets
// to keep the byte packing in update_metadata_ a literal transcription
// of the C arithmetic.
const (
	// metadataHeaderLength == FLAC__STREAM_METADATA_HEADER_LENGTH (4).
	metadataHeaderLength = 4
	// seekpointLength == FLAC__STREAM_METADATA_SEEKPOINT_LENGTH (18).
	seekpointLength = 18
)

// writeBitbuffer_ — port of write_bitbuffer_
// (stream_encoder.c:2976). Asserts the threadtask frame is byte aligned,
// pulls its buffer, optionally drives the verify decoder (with the
// magic-block hack on the first STREAMINFO/magic block), hands the bytes
// to write_frame_, then clears the bitwriter and updates the STREAMINFO
// min/max framesize watermarks when samples > 0.
//
// Returns false (and sets encoder state) on any failure, mirroring the C.
func (encoder *StreamEncoder) writeBitbuffer_(threadtask *StreamEncoderThreadTask, samples uint32, isLastBlock bool) bool {
	// FLAC__ASSERT(FLAC__bitwriter_is_byte_aligned(threadtask->frame));

	buffer, ok := threadtask.frame.GetBuffer()
	if !ok {
		encoder.prot.state = StreamEncoderMemoryAllocationError
		return false
	}
	bytes := uint64(len(buffer))

	if encoder.prot.verify {
		encoder.priv.verify.output.data = buffer
		encoder.priv.verify.output.bytes = uint32(bytes)
		if encoder.priv.verify.stateHint == encoderInMagic {
			encoder.priv.verify.needsMagicHack = true
		} else {
			if !encoder.priv.verify.decoder.ProcessSingle() ||
				(!isLastBlock && encoder.VerifyDecoderState() == DecoderEndOfStream) ||
				encoder.prot.state == StreamEncoderVerifyDecoderError /* Happens when error callback was used */ {
				threadtask.frame.ReleaseBuffer()
				threadtask.frame.Clear()
				if encoder.prot.state != StreamEncoderVerifyMismatchInAudioData {
					encoder.prot.state = StreamEncoderVerifyDecoderError
				}
				return false
			}
		}
	}

	if encoder.writeFrame_(buffer, bytes, samples, isLastBlock) != StreamEncoderWriteStatusOK {
		threadtask.frame.ReleaseBuffer()
		threadtask.frame.Clear()
		encoder.prot.state = StreamEncoderClientError
		return false
	}

	threadtask.frame.ReleaseBuffer()
	threadtask.frame.Clear()

	if samples > 0 {
		si := &encoder.priv.streaminfo.StreamInfo
		if uint32(bytes) < si.MinFrameSize {
			si.MinFrameSize = uint32(bytes)
		}
		if uint32(bytes) > si.MaxFrameSize {
			si.MaxFrameSize = uint32(bytes)
		}
	}

	return true
}

// writeFrame_ — port of write_frame_ (stream_encoder.c:3026). Watches
// for the STREAMINFO and first SEEKTABLE block (samples == 0) to capture
// their stream offsets via the tell callback, marks any seekpoints the
// just-written audio frame satisfies (without breaking — duplicates are
// cleaned in update_metadata_), then hands the bytes to the write
// callback and advances the bytes/samples/frames counters.
//
// The Ogg write-callback wrapper (FLAC__HAS_OGG) is compiled out in the
// vendored build; is_ogg is always false here.
func (encoder *StreamEncoder) writeFrame_(buffer []byte, bytes uint64, samples uint32, isLastBlock bool) StreamEncoderWriteStatus {
	_ = isLastBlock // (void)is_last_block; — only used by the Ogg path.

	var status StreamEncoderWriteStatus
	var outputPosition uint64

	// Watch for the STREAMINFO block and first SEEKTABLE block to go by
	// and store their offsets.
	if samples == 0 {
		typ := MetadataType(buffer[0] & 0x7f)

		// FLAC__STREAM_ENCODER_TELL_STATUS_UNSUPPORTED just means we
		// didn't get the offset; no error.
		if encoder.priv.tellCallback != nil {
			if pos, st := encoder.priv.tellCallback(encoder, encoder.priv.clientData); st == StreamEncoderTellStatusError {
				encoder.prot.state = StreamEncoderClientError
				return StreamEncoderWriteStatusFatalError
			} else if st == StreamEncoderTellStatusOK {
				outputPosition = pos
			}
		}

		if typ == MetadataTypeStreamInfo {
			encoder.prot.streaminfoOffset = outputPosition
		} else if typ == MetadataTypeSeekTable && encoder.prot.seektableOffset == 0 {
			encoder.prot.seektableOffset = outputPosition
		}
	}

	// Mark the current seek point if hit (if audio_offset == 0 that means
	// we're still writing metadata and haven't hit the first frame yet).
	if encoder.priv.seekTable != nil && encoder.prot.audioOffset > 0 && len(encoder.priv.seekTable.Points) > 0 {
		blocksize := encoder.GetBlocksize()
		frameFirstSample := encoder.priv.samplesWritten
		frameLastSample := frameFirstSample + uint64(blocksize) - 1
		var testSample uint64
		for i := encoder.priv.firstSeekpointToCheck; i < uint32(len(encoder.priv.seekTable.Points)); i++ {
			testSample = encoder.priv.seekTable.Points[i].SampleNumber
			if testSample > frameLastSample {
				break
			} else if testSample >= frameFirstSample {
				// FLAC__STREAM_ENCODER_TELL_STATUS_UNSUPPORTED just means
				// we didn't get the offset; no error.
				if outputPosition == 0 && encoder.priv.tellCallback != nil {
					if pos, st := encoder.priv.tellCallback(encoder, encoder.priv.clientData); st == StreamEncoderTellStatusError {
						encoder.prot.state = StreamEncoderClientError
						return StreamEncoderWriteStatusFatalError
					} else if st == StreamEncoderTellStatusOK {
						outputPosition = pos
					}
				}

				encoder.priv.seekTable.Points[i].SampleNumber = frameFirstSample
				encoder.priv.seekTable.Points[i].StreamOffset = outputPosition - encoder.prot.audioOffset
				encoder.priv.seekTable.Points[i].FrameSamples = blocksize
				encoder.priv.firstSeekpointToCheck++
				// DO NOT break here — the seektable template may contain
				// more than one target sample for any given frame; we
				// keep looping and generate duplicate seekpoints, cleaned
				// up later in update_metadata_.
			} else {
				encoder.priv.firstSeekpointToCheck++
			}
		}
	}

	// FLAC__HAS_OGG == 0 in the vendored build: always the direct write
	// callback (no Ogg wrapper).
	status = encoder.priv.writeCallback(encoder, buffer, samples, encoder.priv.currentFrameNumber, encoder.priv.clientData)

	if status == StreamEncoderWriteStatusOK {
		encoder.priv.bytesWritten += bytes
		encoder.priv.samplesWritten += uint64(samples)
		// Keep a high watermark on the number of frames written because
		// when the encoder goes back to write metadata, 'current_frame'
		// drops back to 0.
		if encoder.priv.currentFrameNumber+1 > encoder.priv.framesWritten {
			encoder.priv.framesWritten = encoder.priv.currentFrameNumber + 1
		}
	} else {
		encoder.prot.state = StreamEncoderClientError
	}

	return status
}

// updateMetadata_ — port of update_metadata_ (stream_encoder.c:3127).
// Called once encoding finishes to rewrite the STREAMINFO block (MD5,
// total samples, min/max framesize) and the SEEKTABLE block at the
// offsets captured by write_frame_, seeking with the seek callback and
// emitting with the write callback. Seekpoints whose sample_number
// exceeds the final total are converted to placeholders, then the table
// is sorted and de-duplicated before being written back.
//
// The byte packing of each rewritten field mirrors the C exactly (big
// endian, the local b[] buffer layout) for byte-identical parity.
func (encoder *StreamEncoder) updateMetadata_() {
	var b [seekpointLength]byte // flac_max(6u, FLAC__STREAM_METADATA_SEEKPOINT_LENGTH) == 18
	metadata := &encoder.priv.streaminfo
	samples := metadata.StreamInfo.TotalSamples
	minFramesize := metadata.StreamInfo.MinFrameSize
	maxFramesize := metadata.StreamInfo.MaxFrameSize
	bps := metadata.StreamInfo.BitsPerSample
	var seekStatus StreamEncoderSeekStatus

	// FLAC__ASSERT(metadata->type == FLAC__METADATA_TYPE_STREAMINFO);

	// Write MD5 signature.
	{
		md5Offset := uint64(metadataHeaderLength) +
			(StreamInfoMinBlockSizeLen+
				StreamInfoMaxBlockSizeLen+
				StreamInfoMinFrameSizeLen+
				StreamInfoMaxFrameSizeLen+
				StreamInfoSampleRateLen+
				StreamInfoChannelsLen+
				StreamInfoBitsPerSampleLen+
				StreamInfoTotalSamplesLen)/8

		if seekStatus = encoder.priv.seekCallback(encoder, encoder.prot.streaminfoOffset+md5Offset, encoder.priv.clientData); seekStatus != StreamEncoderSeekStatusOK {
			if seekStatus == StreamEncoderSeekStatusError {
				encoder.prot.state = StreamEncoderClientError
			}
			return
		}
		if encoder.priv.writeCallback(encoder, metadata.StreamInfo.MD5Sum[:], 0, 0, encoder.priv.clientData) != StreamEncoderWriteStatusOK {
			encoder.prot.state = StreamEncoderClientError
			return
		}
	}

	// Write total samples.
	{
		totalSamplesByteOffset := uint64(metadataHeaderLength) +
			(StreamInfoMinBlockSizeLen+
				StreamInfoMaxBlockSizeLen+
				StreamInfoMinFrameSizeLen+
				StreamInfoMaxFrameSizeLen+
				StreamInfoSampleRateLen+
				StreamInfoChannelsLen+
				StreamInfoBitsPerSampleLen-
				4)/8
		samplesUint36 := samples
		if samples > (uint64(1) << StreamInfoTotalSamplesLen) {
			samplesUint36 = 0
		}

		b[0] = (byte(bps-1) << 4) | byte((samplesUint36>>32)&0x0F)
		b[1] = byte((samplesUint36 >> 24) & 0xFF)
		b[2] = byte((samplesUint36 >> 16) & 0xFF)
		b[3] = byte((samplesUint36 >> 8) & 0xFF)
		b[4] = byte(samplesUint36 & 0xFF)
		if seekStatus = encoder.priv.seekCallback(encoder, encoder.prot.streaminfoOffset+totalSamplesByteOffset, encoder.priv.clientData); seekStatus != StreamEncoderSeekStatusOK {
			if seekStatus == StreamEncoderSeekStatusError {
				encoder.prot.state = StreamEncoderClientError
			}
			return
		}
		if encoder.priv.writeCallback(encoder, b[0:5], 0, 0, encoder.priv.clientData) != StreamEncoderWriteStatusOK {
			encoder.prot.state = StreamEncoderClientError
			return
		}
	}

	// Write min/max framesize.
	{
		minFramesizeOffset := uint64(metadataHeaderLength) +
			(StreamInfoMinBlockSizeLen+
				StreamInfoMaxBlockSizeLen)/8

		b[0] = byte((minFramesize >> 16) & 0xFF)
		b[1] = byte((minFramesize >> 8) & 0xFF)
		b[2] = byte(minFramesize & 0xFF)
		b[3] = byte((maxFramesize >> 16) & 0xFF)
		b[4] = byte((maxFramesize >> 8) & 0xFF)
		b[5] = byte(maxFramesize & 0xFF)
		if seekStatus = encoder.priv.seekCallback(encoder, encoder.prot.streaminfoOffset+minFramesizeOffset, encoder.priv.clientData); seekStatus != StreamEncoderSeekStatusOK {
			if seekStatus == StreamEncoderSeekStatusError {
				encoder.prot.state = StreamEncoderClientError
			}
			return
		}
		if encoder.priv.writeCallback(encoder, b[0:6], 0, 0, encoder.priv.clientData) != StreamEncoderWriteStatusOK {
			encoder.prot.state = StreamEncoderClientError
			return
		}
	}

	// Write seektable.
	if encoder.priv.seekTable != nil && len(encoder.priv.seekTable.Points) > 0 && encoder.prot.seektableOffset > 0 {
		// Convert unused seekpoints to placeholders.
		for i := range encoder.priv.seekTable.Points {
			if encoder.priv.seekTable.Points[i].SampleNumber > samples {
				encoder.priv.seekTable.Points[i].SampleNumber = SeekpointPlaceholder
			}
		}

		FormatSeektableSort(encoder.priv.seekTable)

		// FLAC__ASSERT(FLAC__format_seektable_is_legal(encoder->private_->seek_table));

		if seekStatus = encoder.priv.seekCallback(encoder, encoder.prot.seektableOffset+metadataHeaderLength, encoder.priv.clientData); seekStatus != StreamEncoderSeekStatusOK {
			if seekStatus == StreamEncoderSeekStatusError {
				encoder.prot.state = StreamEncoderClientError
			}
			return
		}

		for i := range encoder.priv.seekTable.Points {
			var xx uint64
			var x uint32
			xx = encoder.priv.seekTable.Points[i].SampleNumber
			b[7] = byte(xx)
			xx >>= 8
			b[6] = byte(xx)
			xx >>= 8
			b[5] = byte(xx)
			xx >>= 8
			b[4] = byte(xx)
			xx >>= 8
			b[3] = byte(xx)
			xx >>= 8
			b[2] = byte(xx)
			xx >>= 8
			b[1] = byte(xx)
			xx >>= 8
			b[0] = byte(xx)
			xx >>= 8
			xx = encoder.priv.seekTable.Points[i].StreamOffset
			b[15] = byte(xx)
			xx >>= 8
			b[14] = byte(xx)
			xx >>= 8
			b[13] = byte(xx)
			xx >>= 8
			b[12] = byte(xx)
			xx >>= 8
			b[11] = byte(xx)
			xx >>= 8
			b[10] = byte(xx)
			xx >>= 8
			b[9] = byte(xx)
			xx >>= 8
			b[8] = byte(xx)
			xx >>= 8
			x = encoder.priv.seekTable.Points[i].FrameSamples
			b[17] = byte(x)
			x >>= 8
			b[16] = byte(x)
			x >>= 8
			_ = x
			if encoder.priv.writeCallback(encoder, b[0:18], 0, 0, encoder.priv.clientData) != StreamEncoderWriteStatusOK {
				encoder.prot.state = StreamEncoderClientError
				return
			}
		}
	}
}

// updateOggMetadata_ — port of update_ogg_metadata_
// (stream_encoder.c:3291). The whole function is guarded by
// FLAC__HAS_OGG in the C; the vendored build compiles with
// FLAC__HAS_OGG == 0, so the symbol does not exist there. The signature
// is preserved for structural fidelity but the body is a no-op: the
// native port does not support Ogg output. (If Ogg is ever wired up this
// becomes the simple_ogg_page__get_at / patch / set_at sequence.)
func (encoder *StreamEncoder) updateOggMetadata_() {
	// FLAC__HAS_OGG == 0: not compiled in the vendored configuration.
}

// addSubframe_ — port of add_subframe_ (stream_encoder.c:4383).
// Dispatches on the subframe type to the matching framing writer from
// encode_framing.go, passing the residual-sample count (blocksize minus
// predictor order) where the writer needs it. On any framing failure it
// sets FLAC__STREAM_ENCODER_FRAMING_ERROR and returns false.
func (encoder *StreamEncoder) addSubframe_(blocksize, subframeBps uint32, subframe *Subframe, frame *BitWriter) bool {
	switch subframe.Type {
	case SubframeConstant:
		if !SubframeAddConstant(&subframe.Constant, subframeBps, subframe.WastedBits, frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}
	case SubframeFixed:
		if !SubframeAddFixed(&subframe.Fixed, blocksize-subframe.Fixed.Order, subframeBps, subframe.WastedBits, frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}
	case SubframeLPC:
		if !SubframeAddLPC(&subframe.LPC, blocksize-subframe.LPC.Order, subframeBps, subframe.WastedBits, frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}
	case SubframeVerbatim:
		if !SubframeAddVerbatim(&subframe.Verbatim, blocksize, subframeBps, subframe.WastedBits, frame) {
			encoder.prot.state = StreamEncoderFramingError
			return false
		}
	default:
		// FLAC__ASSERT(0)
	}

	return true
}

// spotcheck_subframe_estimate_ (stream_encoder.c:4425) is guarded by
// SPOTCHECK_ESTIMATE == 0 in the vendored build and is therefore not
// ported (it is a debug-only sanity check that re-frames a subframe to
// compare the estimated bit count against the actual). Intentionally
// omitted.
