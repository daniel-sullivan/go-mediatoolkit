// Package pcm provides streaming PCM encoding and decoding between raw bytes
// and interleaved float64 samples.
//
// A Decoder reads raw PCM bytes from an io.Reader and produces interleaved
// float64 samples normalized to [-1.0, 1.0]. An Encoder does the reverse.
//
// Both require the sample format, sample rate, and channel count to be known
// at construction time (raw PCM has no header).
package pcm

import (
	"encoding/binary"
	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"io"
)

// DecoderOption configures a Decoder.
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	order binary.ByteOrder
}

// WithByteOrder sets the byte order for PCM decoding. Default: binary.LittleEndian.
func WithByteOrder(order binary.ByteOrder) DecoderOption {
	return func(c *decoderConfig) {
		c.order = order
	}
}

// NewDecoder creates a streaming PCM decoder. It reads raw PCM bytes from r
// and decodes them to interleaved float64 samples using the specified format.
func NewDecoder(r io.Reader, sampleRate, channels int, format mutations.SampleFormat, opts ...DecoderOption) (codec.Decoder, error) {
	if channels < 1 {
		return nil, codec.ErrBadChannelCount
	}
	if sampleRate < 1 {
		return nil, codec.ErrBadSampleRate
	}
	bps := format.BytesPerSample()
	if bps == 0 {
		return nil, ErrUnsupportedFormat
	}

	cfg := decoderConfig{order: binary.LittleEndian}
	for _, o := range opts {
		o(&cfg)
	}

	d := &decoder{
		r:          r,
		channels:   channels,
		sampleRate: sampleRate,
		format:     format,
		order:      cfg.order,
		bps:        bps,
		byteBuf:    make([]byte, 4096),
	}
	d.decode = decodeFuncFor(format)
	return d, nil
}

// EncoderOption configures an Encoder.
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	order binary.ByteOrder
}

// WithEncoderByteOrder sets the byte order for PCM encoding. Default: binary.LittleEndian.
func WithEncoderByteOrder(order binary.ByteOrder) EncoderOption {
	return func(c *encoderConfig) {
		c.order = order
	}
}

// NewEncoder creates a streaming PCM encoder. It encodes interleaved float64
// samples and writes raw PCM bytes to w using the specified format.
func NewEncoder(w io.Writer, sampleRate, channels int, format mutations.SampleFormat, opts ...EncoderOption) (codec.Encoder, error) {
	if channels < 1 {
		return nil, codec.ErrBadChannelCount
	}
	if sampleRate < 1 {
		return nil, codec.ErrBadSampleRate
	}
	bps := format.BytesPerSample()
	if bps == 0 {
		return nil, ErrUnsupportedFormat
	}

	cfg := encoderConfig{order: binary.LittleEndian}
	for _, o := range opts {
		o(&cfg)
	}

	e := &encoder{
		w:          w,
		channels:   channels,
		sampleRate: sampleRate,
		format:     format,
		order:      cfg.order,
		bps:        bps,
		byteBuf:    make([]byte, 4096),
	}
	e.encode = encodeFuncFor(format)
	return e, nil
}

type decodeFunc func([]byte, []float64, binary.ByteOrder) int
type encodeFunc func([]float64, []byte, binary.ByteOrder) int

func decodeFuncFor(f mutations.SampleFormat) decodeFunc {
	switch f {
	case mutations.FormatUint8:
		return mutations.Uint8ToFloat64
	case mutations.FormatInt16:
		return mutations.Int16ToFloat64
	case mutations.FormatInt24:
		return mutations.Int24ToFloat64
	case mutations.FormatInt32:
		return mutations.Int32ToFloat64
	case mutations.FormatFloat32:
		return mutations.Float32ToFloat64
	case mutations.FormatFloat64:
		return mutations.BytesToFloat64
	default:
		return nil
	}
}

func encodeFuncFor(f mutations.SampleFormat) encodeFunc {
	switch f {
	case mutations.FormatUint8:
		return mutations.Float64ToUint8
	case mutations.FormatInt16:
		return mutations.Float64ToInt16
	case mutations.FormatInt24:
		return mutations.Float64ToInt24
	case mutations.FormatInt32:
		return mutations.Float64ToInt32
	case mutations.FormatFloat32:
		return mutations.Float64ToFloat32Bytes
	case mutations.FormatFloat64:
		return mutations.Float64ToBytes
	default:
		return nil
	}
}
