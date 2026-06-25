package flac

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/flac/internal/nativeflac"
)

// nativeEncoder adapts the pure-Go nativeflac.StreamEncoder to the public
// Encoder interface. It mirrors cgoEncoder's behaviour field-for-field so
// the two backends produce identical output: the same configuration
// setters, the same VORBIS_COMMENT injection, and a write callback that
// forwards framed bytes to the caller's io.Writer.
type nativeEncoder struct {
	enc  *nativeflac.StreamEncoder
	w    io.Writer
	info StreamInfo
	cfg  encoderConfig

	// vorbisMeta is the VORBIS_COMMENT metadata block handed to the
	// encoder before init when the caller supplied a vendor / tags. The
	// encoder retains the pointer for its lifetime; Go's GC reclaims it.
	vorbisMeta *nativeflac.StreamMetadata

	writeErr error
	closed   bool
}

// newNativeStreamEncoder builds the nativeflac-backed Encoder. It applies
// the same set_* sequence the cgo path uses (channels, bits, sample rate,
// compression level, verify, block size, total samples, metadata) and
// then InitStream-s the encoder against a write callback that forwards to
// w. Init failures map onto the package error sentinels.
func newNativeStreamEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	enc := nativeflac.NewStreamEncoder()
	if enc == nil {
		return nil, ErrAllocFail
	}

	e := &nativeEncoder{enc: enc, w: w, info: info, cfg: cfg}

	enc.SetChannels(uint32(info.Channels))
	enc.SetBitsPerSample(uint32(info.BitsPerSample))
	enc.SetSampleRate(uint32(info.SampleRate))
	enc.SetCompressionLevel(uint32(cfg.compression))
	if cfg.verify {
		enc.SetVerify(true)
	}
	if cfg.blockSize > 0 {
		enc.SetBlocksize(uint32(cfg.blockSize))
	}
	if cfg.totalSamples > 0 {
		enc.SetTotalSamplesEstimate(cfg.totalSamples)
	}

	if cfg.vendor != "" || len(cfg.tags) > 0 {
		meta := buildNativeVorbisComment(cfg.vendor, cfg.tags)
		e.vorbisMeta = meta
		if !enc.SetMetadata([]*nativeflac.StreamMetadata{meta}) {
			return nil, ErrInternal
		}
	}

	st := enc.InitStream(
		e.writeCallback,
		nil, // seek — streaming-only, no rewrite of STREAMINFO/SEEKTABLE
		nil, // tell
		nil, // metadata
		nil, // clientData (the callback closes over e)
	)
	if st != nativeflac.StreamEncoderInitStatusOK {
		return nil, errInitWithStatus(nativeInitStatusString(st))
	}
	return e, nil
}

// nativeInitStatusString maps a nativeflac init status onto the libFLAC
// FLAC__StreamEncoderInitStatusString text so init failures carry the
// same diagnostic the cgo path surfaces.
func nativeInitStatusString(st nativeflac.StreamEncoderInitStatus) string {
	switch st {
	case nativeflac.StreamEncoderInitStatusOK:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_OK"
	case nativeflac.StreamEncoderInitStatusEncoderError:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_ENCODER_ERROR"
	case nativeflac.StreamEncoderInitStatusUnsupportedContainer:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_UNSUPPORTED_CONTAINER"
	case nativeflac.StreamEncoderInitStatusInvalidCallbacks:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_CALLBACKS"
	case nativeflac.StreamEncoderInitStatusInvalidNumberOfChannels:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_NUMBER_OF_CHANNELS"
	case nativeflac.StreamEncoderInitStatusInvalidBitsPerSample:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_BITS_PER_SAMPLE"
	case nativeflac.StreamEncoderInitStatusInvalidSampleRate:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_SAMPLE_RATE"
	case nativeflac.StreamEncoderInitStatusInvalidBlockSize:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_BLOCK_SIZE"
	case nativeflac.StreamEncoderInitStatusInvalidMaxLPCOrder:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_MAX_LPC_ORDER"
	case nativeflac.StreamEncoderInitStatusInvalidQLPCoeffPrecision:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_QLP_COEFF_PRECISION"
	case nativeflac.StreamEncoderInitStatusBlockSizeTooSmallForLPCOrder:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_BLOCK_SIZE_TOO_SMALL_FOR_LPC_ORDER"
	case nativeflac.StreamEncoderInitStatusNotStreamable:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_NOT_STREAMABLE"
	case nativeflac.StreamEncoderInitStatusInvalidMetadata:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_INVALID_METADATA"
	case nativeflac.StreamEncoderInitStatusAlreadyInitialized:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_ALREADY_INITIALIZED"
	default:
		return "FLAC__STREAM_ENCODER_INIT_STATUS_UNKNOWN"
	}
}

// buildNativeVorbisComment constructs a VORBIS_COMMENT StreamMetadata
// block mirroring buildVorbisCommentMetadata on the cgo path. Length is
// the framed body size: the 4-byte vendor-length field + vendor bytes,
// the 4-byte comment count, and per comment a 4-byte length + the
// "KEY=VALUE" bytes. AddMetadataBlock substitutes libFLAC's own vendor
// string at framing time (updateVendorString == true), exactly as
// libFLAC does, so the caller-supplied vendor only affects Length here —
// matching the cgo backend's silent vendor override documented on
// WithVendor.
func buildNativeVorbisComment(vendor string, tags [][2]string) *nativeflac.StreamMetadata {
	m := new(nativeflac.StreamMetadata)
	m.Type = nativeflac.MetadataTypeVorbisComment

	length := uint32(4) // vendor length field
	vb := []byte(vendor)
	length += uint32(len(vb))
	m.VorbisComment.VendorString.Length = uint32(len(vb))
	m.VorbisComment.VendorString.Entry = vb

	length += 4 // num_comments field
	comments := make([]nativeflac.VorbisCommentEntry, 0, len(tags))
	for _, kv := range tags {
		entry := []byte(kv[0] + "=" + kv[1])
		comments = append(comments, nativeflac.VorbisCommentEntry{
			Length: uint32(len(entry)),
			Entry:  entry,
		})
		length += 4 + uint32(len(entry))
	}
	m.VorbisComment.NumComments = uint32(len(comments))
	m.VorbisComment.Comments = comments
	m.Length = length
	return m
}

// writeCallback is the nativeflac write callback. It forwards every
// framed buffer (metadata and audio frames) to the caller's io.Writer,
// recording the first write error so Encode / Close can surface it,
// matching flacGoEncWriteCallback on the cgo path.
func (e *nativeEncoder) writeCallback(_ *nativeflac.StreamEncoder, buffer []byte, _, _ uint32, _ any) nativeflac.StreamEncoderWriteStatus {
	if len(buffer) == 0 {
		return nativeflac.StreamEncoderWriteStatusOK
	}
	n, err := e.w.Write(buffer)
	if err != nil {
		e.writeErr = err
		return nativeflac.StreamEncoderWriteStatusFatalError
	}
	if n != len(buffer) {
		e.writeErr = io.ErrShortWrite
		return nativeflac.StreamEncoderWriteStatusFatalError
	}
	return nativeflac.StreamEncoderWriteStatusOK
}

func (e *nativeEncoder) Encode(buf []int32) error {
	if e.closed {
		return ErrClosed
	}
	if len(buf) == 0 {
		return nil
	}
	if len(buf)%e.info.Channels != 0 {
		return ErrBadArg
	}
	samplesPerChannel := len(buf) / e.info.Channels

	if !e.enc.ProcessInterleaved(buf, uint32(samplesPerChannel)) {
		if e.writeErr != nil {
			err := e.writeErr
			e.writeErr = nil
			return err
		}
		return &nativeEncodeError{state: e.enc.ResolvedStateString()}
	}
	return nil
}

// nativeEncodeError carries the encoder's resolved state string out of a
// failed Encode, mirroring encodeError on the cgo path but available in
// the non-cgo build too. It unwraps to ErrInternal.
type nativeEncodeError struct{ state string }

func (e *nativeEncodeError) Error() string { return "flac: encode failed: " + e.state }
func (e *nativeEncodeError) Unwrap() error { return ErrInternal }

func (e *nativeEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	ok := e.enc.Finish()
	e.enc = nil

	if !ok {
		if e.writeErr != nil {
			return e.writeErr
		}
		// A verify mismatch is reported via the encoder state, mapped to
		// ErrEncoderVerify like the cgo path.
		return ErrEncoderVerify
	}
	if e.writeErr != nil {
		err := e.writeErr
		e.writeErr = nil
		return err
	}
	return nil
}
