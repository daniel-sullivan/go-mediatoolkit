//go:build cgo

package opus

/*
#include <opus.h>
#include <stdlib.h>

static int enc_set_bitrate(OpusEncoder *e, opus_int32 br) {
	return opus_encoder_ctl(e, OPUS_SET_BITRATE(br));
}
static int enc_set_complexity(OpusEncoder *e, opus_int32 c) {
	return opus_encoder_ctl(e, OPUS_SET_COMPLEXITY(c));
}
*/
import "C"
import (
	"runtime"
	"unsafe"
)

// cgoEncoder implements the Encoder interface using the C libopus library.
type cgoEncoder struct {
	p          *C.OpusEncoder
	sampleRate int
	channels   int
	buf        []byte // reusable output buffer
}

func newEncoder(sampleRate int, channels int, cfg encoderConfig) (Encoder, error) {
	var app C.int
	switch cfg.application {
	case AppVoIP:
		app = C.OPUS_APPLICATION_VOIP
	case AppLowDelay:
		app = C.OPUS_APPLICATION_RESTRICTED_LOWDELAY
	default:
		app = C.OPUS_APPLICATION_AUDIO
	}

	var cerr C.int
	p := C.opus_encoder_create(C.opus_int32(sampleRate), C.int(channels), app, &cerr)
	if cerr != C.OPUS_OK {
		return nil, ErrInternalError
	}
	e := &cgoEncoder{
		p:          p,
		sampleRate: sampleRate,
		channels:   channels,
	}
	runtime.SetFinalizer(e, (*cgoEncoder).destroy)

	C.enc_set_bitrate(p, C.opus_int32(cfg.bitrate))
	C.enc_set_complexity(p, C.opus_int32(cfg.complexity))

	return e, nil
}

func (e *cgoEncoder) destroy() {
	if e.p != nil {
		C.opus_encoder_destroy(e.p)
		e.p = nil
	}
}

func (e *cgoEncoder) Encode(pcm []float64, maxPacketSize int) ([]byte, error) {
	if len(pcm) == 0 {
		return nil, ErrBadArg
	}
	frameSamples := len(pcm) / e.channels

	// Convert float64 → float32 for the C API.
	f32 := make([]float32, len(pcm))
	for i, v := range pcm {
		f32[i] = float32(v)
	}

	// Ensure output buffer is large enough.
	if cap(e.buf) < maxPacketSize {
		e.buf = make([]byte, maxPacketSize)
	}
	out := e.buf[:maxPacketSize]

	n := C.opus_encode_float(e.p,
		(*C.float)(unsafe.Pointer(&f32[0])),
		C.int(frameSamples),
		(*C.uchar)(unsafe.Pointer(&out[0])),
		C.opus_int32(maxPacketSize))
	if n < 0 {
		return nil, opusError(int(n))
	}

	pkt := make([]byte, int(n))
	copy(pkt, out[:int(n)])
	return pkt, nil
}

func (e *cgoEncoder) SampleRate() int { return e.sampleRate }
func (e *cgoEncoder) Channels() int   { return e.channels }

func (e *cgoEncoder) Reset() {
	if e.p != nil {
		C.opus_encoder_destroy(e.p)
	}
	var cerr C.int
	e.p = C.opus_encoder_create(C.opus_int32(e.sampleRate), C.int(e.channels),
		C.OPUS_APPLICATION_AUDIO, &cerr)
}

func (e *cgoEncoder) SetBitrate(bps int) error {
	if bps < 6000 || bps > 510000 {
		return ErrBadArg
	}
	C.enc_set_bitrate(e.p, C.opus_int32(bps))
	return nil
}

// opusError converts a C libopus error code to a Go error.
func opusError(code int) error {
	switch {
	case code == C.OPUS_BAD_ARG:
		return ErrBadArg
	case code == C.OPUS_BUFFER_TOO_SMALL:
		return ErrBufferTooSmall
	case code == C.OPUS_INVALID_PACKET:
		return ErrInvalidPacket
	case code == C.OPUS_UNIMPLEMENTED:
		return ErrUnimplemented
	default:
		return ErrInternalError
	}
}
