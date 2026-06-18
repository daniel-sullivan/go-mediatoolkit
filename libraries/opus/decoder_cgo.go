//go:build cgo

package opus

/*
#include <opus.h>
#include <stdlib.h>

static int dec_get_last_packet_duration(OpusDecoder *d, opus_int32 *dur) {
	return opus_decoder_ctl(d, OPUS_GET_LAST_PACKET_DURATION(dur));
}
*/
import "C"
import (
	"runtime"
	"unsafe"
)

// cgoDecoder implements the Decoder interface using the C libopus library.
type cgoDecoder struct {
	p          *C.OpusDecoder
	sampleRate int
	channels   int
	f32Buf     []float32 // reusable conversion buffer
}

func newDecoder(sampleRate, channels int, cfg decoderConfig) (Decoder, error) {
	var cerr C.int
	p := C.opus_decoder_create(C.opus_int32(sampleRate), C.int(channels), &cerr)
	if cerr != C.OPUS_OK {
		return nil, ErrInternalError
	}
	d := &cgoDecoder{
		p:          p,
		sampleRate: sampleRate,
		channels:   channels,
	}
	runtime.SetFinalizer(d, (*cgoDecoder).destroy)
	return d, nil
}

func (d *cgoDecoder) destroy() {
	if d.p != nil {
		C.opus_decoder_destroy(d.p)
		d.p = nil
	}
}

func (d *cgoDecoder) Decode(data []byte, pcm []float64) (int, error) {
	maxSamplesPerChannel := len(pcm) / d.channels

	// Ensure float32 conversion buffer is large enough.
	need := maxSamplesPerChannel * d.channels
	if cap(d.f32Buf) < need {
		d.f32Buf = make([]float32, need)
	}
	f32 := d.f32Buf[:need]

	var n C.int
	if data == nil {
		// Packet loss concealment.
		n = C.opus_decode_float(d.p, nil, 0,
			(*C.float)(unsafe.Pointer(&f32[0])),
			C.int(maxSamplesPerChannel), 0)
	} else {
		n = C.opus_decode_float(d.p,
			(*C.uchar)(unsafe.Pointer(&data[0])),
			C.opus_int32(len(data)),
			(*C.float)(unsafe.Pointer(&f32[0])),
			C.int(maxSamplesPerChannel), 0)
	}
	if n < 0 {
		return 0, opusError(int(n))
	}

	// Convert float32 → float64.
	samples := int(n) * d.channels
	for i := 0; i < samples; i++ {
		pcm[i] = float64(f32[i])
	}

	return int(n), nil
}

func (d *cgoDecoder) SampleRate() int { return d.sampleRate }
func (d *cgoDecoder) Channels() int   { return d.channels }

func (d *cgoDecoder) LastPacketDuration() int {
	var dur C.opus_int32
	C.dec_get_last_packet_duration(d.p, &dur)
	return int(dur)
}

func (d *cgoDecoder) Reset() {
	if d.p != nil {
		C.opus_decoder_destroy(d.p)
	}
	var cerr C.int
	d.p = C.opus_decoder_create(C.opus_int32(d.sampleRate), C.int(d.channels), &cerr)
}
