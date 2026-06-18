// Package benchcmp provides Go vs C comparison benchmarks for the Opus codec.
package benchcmp

/*
#cgo CFLAGS: -DHAVE_CONFIG_H -DOPUS_BUILD
#cgo LDFLAGS: -lm
#cgo CFLAGS: -I${SRCDIR}/../libopus
#cgo CFLAGS: -I${SRCDIR}/../libopus/include
#cgo CFLAGS: -I${SRCDIR}/../libopus/celt
#cgo CFLAGS: -I${SRCDIR}/../libopus/silk
#cgo CFLAGS: -I${SRCDIR}/../libopus/silk/float
#cgo CFLAGS: -I${SRCDIR}/../libopus/src
// Optimization level intentionally omitted; clang processes flags
// left-to-right and a trailing -O2 re-enables vectorization even if
// -fno-vectorize preceded it. mise.toml's CGO_CFLAGS carries the
// correct sequence (-O2 first, disables after) via env which cgo
// places before directive flags.
#cgo CFLAGS: -Wno-unused-parameter -Wno-sign-compare

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
import "unsafe"

// CEncoder wraps a C OpusEncoder.
type CEncoder struct{ p *C.OpusEncoder }

// CDecoder wraps a C OpusDecoder.
type CDecoder struct{ p *C.OpusDecoder }

// NewCEncoder creates a C opus encoder.
func NewCEncoder(sampleRate, channels, app int) *CEncoder {
	var err C.int
	enc := C.opus_encoder_create(C.opus_int32(sampleRate), C.int(channels), C.int(app), &err)
	if err != 0 {
		return nil
	}
	return &CEncoder{p: enc}
}

func (e *CEncoder) Destroy()            { C.opus_encoder_destroy(e.p) }
func (e *CEncoder) SetBitrate(br int)   { C.enc_set_bitrate(e.p, C.opus_int32(br)) }
func (e *CEncoder) SetComplexity(c int) { C.enc_set_complexity(e.p, C.opus_int32(c)) }

// Encode encodes float32 PCM into an Opus packet.
func (e *CEncoder) Encode(pcm []float32, pkt []byte) int {
	n := C.opus_encode_float(e.p,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// EncodeFrame is like Encode but takes the frame size in samples per
// channel (matching the C opus_encode_float API). Use this for stereo
// input where `len(pcm)` is 2*frameSize.
func (e *CEncoder) EncodeFrame(pcm []float32, frameSize int, pkt []byte) int {
	n := C.opus_encode_float(e.p,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// EncodeInt16 encodes int16 PCM into an Opus packet, matching the
// C opus_encode API.
func (e *CEncoder) EncodeInt16(pcm []int16, frameSize int, pkt []byte) int {
	n := C.opus_encode(e.p,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize),
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)))
	return int(n)
}

// DecodeFrame is like Decode but takes the output frame size in
// samples per channel, matching opus_decode_float.
func (d *CDecoder) DecodeFrame(pkt []byte, pcm []float32, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_decode_float(d.p, pktPtr, pktLen,
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// NewCDecoder creates a C opus decoder.
func NewCDecoder(sampleRate, channels int) *CDecoder {
	var err C.int
	dec := C.opus_decoder_create(C.opus_int32(sampleRate), C.int(channels), &err)
	if err != 0 {
		return nil
	}
	return &CDecoder{p: dec}
}

func (d *CDecoder) Destroy() { C.opus_decoder_destroy(d.p) }

// Decode decodes an Opus packet into float32 PCM.
func (d *CDecoder) Decode(pkt []byte, pcm []float32) int {
	n := C.opus_decode_float(d.p,
		(*C.uchar)(unsafe.Pointer(&pkt[0])),
		C.opus_int32(len(pkt)),
		(*C.float)(unsafe.Pointer(&pcm[0])),
		C.int(len(pcm)), 0)
	return int(n)
}

// DecodeInt16 decodes an Opus packet into int16 PCM via opus_decode
// (which applies soft-clip + FLOAT2INT16 in the vendored C build).
// `frameSize` is samples per channel.
func (d *CDecoder) DecodeInt16(pkt []byte, pcm []int16, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_decode(d.p, pktPtr, pktLen,
		(*C.opus_int16)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// DecodeInt24 decodes an Opus packet via opus_decode24 (no soft-clip,
// FLOAT2INT24 — 1/8388608 resolution). `frameSize` is samples per
// channel. Output range: [-2^24, 2^24].
func (d *CDecoder) DecodeInt24(pkt []byte, pcm []int32, frameSize int) int {
	var pktPtr *C.uchar
	var pktLen C.opus_int32
	if len(pkt) > 0 {
		pktPtr = (*C.uchar)(unsafe.Pointer(&pkt[0]))
		pktLen = C.opus_int32(len(pkt))
	}
	n := C.opus_decode24(d.p, pktPtr, pktLen,
		(*C.opus_int32)(unsafe.Pointer(&pcm[0])),
		C.int(frameSize), 0)
	return int(n)
}

// COpusVersion returns the libopus version string.
func COpusVersion() string {
	return C.GoString(C.opus_get_version_string())
}

// Application constants matching libopus.
const (
	AppVOIP             = C.OPUS_APPLICATION_VOIP
	AppAudio            = C.OPUS_APPLICATION_AUDIO
	AppRestrictedLowDel = C.OPUS_APPLICATION_RESTRICTED_LOWDELAY
)
