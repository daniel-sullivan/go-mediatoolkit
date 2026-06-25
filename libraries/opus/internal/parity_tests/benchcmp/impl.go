//go:build cgo

package benchcmp

// Shared encoder/decoder interface for parity testing. The upstream
// opus_demo CLI, the vendored cgo build, and the pure-Go port all
// satisfy these interfaces, letting tests iterate over implementations
// uniformly. The method set is the minimum needed by the three-way
// parity test.

import (
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// Encoder is a minimal common surface over the opus encode APIs.
type Encoder interface {
	EncodeFrame(pcm []float32, frameSize int, pkt []byte) int
	EncodeInt16(pcm []int16, frameSize int, pkt []byte) int
	SetBitrate(br int)
	SetComplexity(c int)
	Destroy()
}

// Decoder is a minimal common surface over the opus decode APIs.
type Decoder interface {
	DecodeFrame(pkt []byte, pcm []float32, frameSize int) int
	DecodeInt16(pkt []byte, pcm []int16, frameSize int) int
	DecodeInt24(pkt []byte, pcm []int32, frameSize int) int
	Destroy()
}

// Compile-time assertions that both implementations satisfy the
// interface. If either drifts, this will fail to build rather than
// producing confusing runtime errors in the three-way tests.
var (
	_ Encoder = (*CEncoder)(nil)
	_ Encoder = (*GoEncoder)(nil)
	_ Decoder = (*CDecoder)(nil)
	_ Decoder = (*GoDecoder)(nil)
)

// ---- Go implementation (thin wrapper over internal/nativeopus) ---------

// GoEncoder wraps an internal/nativeopus encoder with a method-style API
// matching CEncoder. The underlying nativeopus.OpusEncoder mirrors C 1:1;
// this wrapper exists so tests can hold any Encoder interface.
type GoEncoder struct{ st *nativeopus.OpusEncoder }

// NewGoEncoder creates a Go opus encoder. Returns nil on failure.
func NewGoEncoder(sampleRate, channels, app int) *GoEncoder {
	st, code := nativeopus.NewEncoder(int32(sampleRate), channels, app)
	if code != nativeopus.ErrorOK {
		return nil
	}
	return &GoEncoder{st: st}
}

func (e *GoEncoder) Destroy() { nativeopus.DestroyEncoder(e.st) }

func (e *GoEncoder) SetBitrate(br int) {
	nativeopus.EncoderCtl(e.st, nativeopus.CtlSetBitrate, int32(br))
}

func (e *GoEncoder) SetComplexity(c int) {
	nativeopus.EncoderCtl(e.st, nativeopus.CtlSetComplexity, int32(c))
}

// EncodeFrame matches CEncoder.EncodeFrame: encodes `frameSize`
// samples per channel of interleaved float32 PCM into pkt. Returns
// packet length or a negative opus error code.
func (e *GoEncoder) EncodeFrame(pcm []float32, frameSize int, pkt []byte) int {
	return nativeopus.EncodeFloat(e.st, pcm, frameSize, pkt)
}

// EncodeInt16 matches CEncoder.EncodeInt16.
func (e *GoEncoder) EncodeInt16(pcm []int16, frameSize int, pkt []byte) int {
	return nativeopus.EncodeInt16(e.st, pcm, frameSize, pkt)
}

// GoDecoder is the Go-port counterpart of CDecoder.
type GoDecoder struct{ st *nativeopus.OpusDecoder }

// NewGoDecoder creates a Go opus decoder.
func NewGoDecoder(sampleRate, channels int) *GoDecoder {
	st, code := nativeopus.NewDecoder(int32(sampleRate), channels)
	if code != nativeopus.ErrorOK {
		return nil
	}
	return &GoDecoder{st: st}
}

func (d *GoDecoder) Destroy() { nativeopus.DestroyDecoder(d.st) }

func (d *GoDecoder) DecodeFrame(pkt []byte, pcm []float32, frameSize int) int {
	return nativeopus.DecodeFloat(d.st, pkt, pcm, frameSize, 0)
}

func (d *GoDecoder) DecodeInt16(pkt []byte, pcm []int16, frameSize int) int {
	return nativeopus.DecodeInt16(d.st, pkt, pcm, frameSize, 0)
}

func (d *GoDecoder) DecodeInt24(pkt []byte, pcm []int32, frameSize int) int {
	return nativeopus.DecodeInt24(d.st, pkt, pcm, frameSize, 0)
}
