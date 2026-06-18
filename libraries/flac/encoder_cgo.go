//go:build cgo

package flac

/*
#include <FLAC/stream_encoder.h>
#include <FLAC/format.h>
#include <FLAC/metadata.h>
#include <stdlib.h>

// flacGoInitStreamEncoder + state-string helpers are defined in
// flac_cgo_trampolines.c.
extern FLAC__StreamEncoderInitStatus flacGoInitStreamEncoder(FLAC__StreamEncoder *enc, void *client_data);
extern const char *flacGoEncoderStateString  (FLAC__StreamEncoderState  s);
extern const char *flacGoEncoderInitStatusStr(FLAC__StreamEncoderInitStatus s);
*/
import "C"

import (
	"io"
	"runtime/cgo"
	"unsafe"
)

type cgoEncoder struct {
	enc    *C.FLAC__StreamEncoder
	handle cgo.Handle
	w      io.Writer
	info   StreamInfo
	cfg    encoderConfig

	// vorbisMeta is the VORBIS_COMMENT metadata block handed to libFLAC
	// before init. We retain ownership and free it on Close.
	vorbisMeta *C.FLAC__StreamMetadata
	metaArrPtr unsafe.Pointer // C-malloc'd array passed to set_metadata

	writeErr error
	closed   bool
}

func newEncoder(w io.Writer, info StreamInfo, cfg encoderConfig) (Encoder, error) {
	enc := C.FLAC__stream_encoder_new()
	if enc == nil {
		return nil, ErrAllocFail
	}

	e := &cgoEncoder{enc: enc, w: w, info: info, cfg: cfg}
	e.handle = cgo.NewHandle(e)

	C.FLAC__stream_encoder_set_channels(enc, C.uint32_t(info.Channels))
	C.FLAC__stream_encoder_set_bits_per_sample(enc, C.uint32_t(info.BitsPerSample))
	C.FLAC__stream_encoder_set_sample_rate(enc, C.uint32_t(info.SampleRate))
	C.FLAC__stream_encoder_set_compression_level(enc, C.uint32_t(cfg.compression))
	if cfg.verify {
		C.FLAC__stream_encoder_set_verify(enc, 1)
	}
	if cfg.blockSize > 0 {
		C.FLAC__stream_encoder_set_blocksize(enc, C.uint32_t(cfg.blockSize))
	}
	if cfg.totalSamples > 0 {
		C.FLAC__stream_encoder_set_total_samples_estimate(enc, C.FLAC__uint64(cfg.totalSamples))
	}

	if cfg.vendor != "" || len(cfg.tags) > 0 {
		meta, err := buildVorbisCommentMetadata(cfg.vendor, cfg.tags)
		if err != nil {
			e.handle.Delete()
			C.FLAC__stream_encoder_delete(enc)
			return nil, err
		}
		e.vorbisMeta = meta
		// libFLAC takes ownership of the array but not the blocks
		// themselves; we keep a Go-side reference and free on Close.
		metaArr := (**C.FLAC__StreamMetadata)(C.malloc(C.size_t(unsafe.Sizeof(meta))))
		*metaArr = meta
		if C.FLAC__stream_encoder_set_metadata(enc, metaArr, 1) == 0 {
			C.free(unsafe.Pointer(metaArr))
			C.FLAC__metadata_object_delete(meta)
			e.handle.Delete()
			C.FLAC__stream_encoder_delete(enc)
			return nil, ErrInternal
		}
		// metaArr is held by libFLAC for the encoder's lifetime —
		// don't free it here; defer to Close.
		e.metaArrPtr = unsafe.Pointer(metaArr)
	}

	st := C.flacGoInitStreamEncoder(enc, unsafe.Pointer(uintptr(e.handle)))
	if st != C.FLAC__STREAM_ENCODER_INIT_STATUS_OK {
		statusStr := C.GoString(C.flacGoEncoderInitStatusStr(st))
		e.handle.Delete()
		C.FLAC__stream_encoder_delete(enc)
		return nil, errInitWithStatus(statusStr)
	}
	return e, nil
}

func (e *cgoEncoder) Encode(buf []int32) error {
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

	// FLAC__stream_encoder_process_interleaved expects a contiguous
	// FLAC__int32* of (samples_per_channel * channels). Go's int32 is
	// the same width as C's FLAC__int32 (int32_t), so the slice header
	// can be passed straight through.
	if C.FLAC__stream_encoder_process_interleaved(
		e.enc,
		(*C.FLAC__int32)(unsafe.Pointer(&buf[0])),
		C.uint32_t(samplesPerChannel),
	) == 0 {
		if e.writeErr != nil {
			err := e.writeErr
			e.writeErr = nil
			return err
		}
		state := C.FLAC__stream_encoder_get_state(e.enc)
		stateStr := C.GoString(C.flacGoEncoderStateString(state))
		return &encodeError{state: stateStr}
	}
	return nil
}

type encodeError struct{ state string }

func (e *encodeError) Error() string { return "flac: encode failed: " + e.state }
func (e *encodeError) Unwrap() error { return ErrInternal }

// buildVorbisCommentMetadata constructs a VORBIS_COMMENT metadata block
// owned by libFLAC. The caller must FLAC__metadata_object_delete it (or
// pass it to set_metadata, which transfers freeing to libFLAC's
// FLAC__stream_encoder_delete only when libFLAC made the object — we
// always made it, so we always free).
func buildVorbisCommentMetadata(vendor string, tags [][2]string) (*C.FLAC__StreamMetadata, error) {
	meta := C.FLAC__metadata_object_new(C.FLAC__METADATA_TYPE_VORBIS_COMMENT)
	if meta == nil {
		return nil, ErrAllocFail
	}
	if vendor != "" {
		cv := C.CString(vendor)
		defer C.free(unsafe.Pointer(cv))
		// _set_vendor_string copies its argument when copy=true.
		if C.FLAC__metadata_object_vorbiscomment_set_vendor_string(
			meta,
			C.FLAC__StreamMetadata_VorbisComment_Entry{
				length: C.FLAC__uint32(len(vendor)),
				entry:  (*C.FLAC__byte)(unsafe.Pointer(cv)),
			},
			1, // copy
		) == 0 {
			C.FLAC__metadata_object_delete(meta)
			return nil, ErrAllocFail
		}
	}
	for _, kv := range tags {
		entry := kv[0] + "=" + kv[1]
		ce := C.CString(entry)
		ok := C.FLAC__metadata_object_vorbiscomment_append_comment(
			meta,
			C.FLAC__StreamMetadata_VorbisComment_Entry{
				length: C.FLAC__uint32(len(entry)),
				entry:  (*C.FLAC__byte)(unsafe.Pointer(ce)),
			},
			1, // copy — libFLAC duplicates the bytes, so we free ours
		)
		C.free(unsafe.Pointer(ce))
		if ok == 0 {
			C.FLAC__metadata_object_delete(meta)
			return nil, ErrAllocFail
		}
	}
	return meta, nil
}

func (e *cgoEncoder) Close() error {
	if e.closed {
		return nil
	}
	e.closed = true
	ok := C.FLAC__stream_encoder_finish(e.enc)
	C.FLAC__stream_encoder_delete(e.enc)
	e.enc = nil
	if e.metaArrPtr != nil {
		C.free(e.metaArrPtr)
		e.metaArrPtr = nil
	}
	if e.vorbisMeta != nil {
		C.FLAC__metadata_object_delete(e.vorbisMeta)
		e.vorbisMeta = nil
	}
	e.handle.Delete()

	if ok == 0 {
		if e.writeErr != nil {
			return e.writeErr
		}
		// libFLAC reports a verify mismatch via the encoder state.
		return ErrEncoderVerify
	}
	if e.writeErr != nil {
		err := e.writeErr
		e.writeErr = nil
		return err
	}
	return nil
}

//export flacGoEncWriteCallback
func flacGoEncWriteCallback(enc *C.FLAC__StreamEncoder, buffer *C.FLAC__byte, bytes C.size_t, samples C.uint32_t, currentFrame C.uint32_t, clientData unsafe.Pointer) C.FLAC__StreamEncoderWriteStatus {
	_ = enc
	_ = samples
	_ = currentFrame
	e := cgo.Handle(uintptr(clientData)).Value().(*cgoEncoder)
	if bytes == 0 {
		return C.FLAC__STREAM_ENCODER_WRITE_STATUS_OK
	}
	goBuf := unsafe.Slice((*byte)(unsafe.Pointer(buffer)), int(bytes))
	n, err := e.w.Write(goBuf)
	if err != nil {
		e.writeErr = err
		return C.FLAC__STREAM_ENCODER_WRITE_STATUS_FATAL_ERROR
	}
	if n != len(goBuf) {
		e.writeErr = io.ErrShortWrite
		return C.FLAC__STREAM_ENCODER_WRITE_STATUS_FATAL_ERROR
	}
	return C.FLAC__STREAM_ENCODER_WRITE_STATUS_OK
}
