// Package mp3 provides streaming MP3 encoding and decoding between a native
// MP3 byte stream and interleaved float64 samples.
//
// MP3 is a self-framed codec: the byte stream itself carries frame sync
// headers, side information, and audio data in a continuous sequence.
// Following the FLAC archetype, the [Decoder] reads raw MP3 frames from a
// single continuous [io.Reader] and the [Encoder] writes raw MP3 frames to a
// single continuous [io.Writer] — no separate framing layer is required at
// this level. Use [go-mediatoolkit/containers/mp3] for ID3 metadata
// inspection and tag projection on top of the raw stream.
//
// Samples are normalized to [-1.0, 1.0] by dividing integer samples by
// 2^(bits-1)-1 on decode; on encode, float64 is scaled and rounded to 16-bit
// int. MP3 decodes to signed 16-bit PCM, so the scale factor is 2^15-1.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package mp3

import (
	"io"

	"go-mediatoolkit/codec"
	mp3lib "go-mediatoolkit/libraries/mp3"
)

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct{}

// NewDecoder creates a streaming MP3 decoder. It reads a native MP3 byte
// stream from r — a continuous sequence of MP3 frames — and produces
// interleaved float64 samples in [-1.0, 1.0].
//
// The decoder's SampleRate and Channels are not known until the first frame
// header has been parsed; they are surfaced as soon as [codec.Decoder.Read]
// returns its first samples. Until then [codec.Decoder.SampleRate] and
// [codec.Decoder.Channels] return zero.
func NewDecoder(r io.Reader, opts ...DecoderOption) (codec.Decoder, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	dec, err := mp3lib.NewDecoder(r)
	if err != nil {
		return nil, err
	}
	return &decoder{dec: dec}, nil
}

// EncoderOption configures an [Encoder].
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	bitRate int
	quality int
	vbr     bool
}

// WithBitRate sets the target constant bit rate, in bits per second.
// Default: 128000. Ignored when [WithVBR] is enabled.
func WithBitRate(bps int) EncoderOption {
	return func(c *encoderConfig) { c.bitRate = bps }
}

// WithQuality sets the LAME encoding quality, in the range [0, 9] where 0 is
// highest quality / slowest. Default: 3.
func WithQuality(q int) EncoderOption {
	return func(c *encoderConfig) { c.quality = q }
}

// WithVBR enables variable-bit-rate encoding. When enabled, [WithBitRate] is
// ignored and [WithQuality] selects the VBR quality target.
func WithVBR(enable bool) EncoderOption {
	return func(c *encoderConfig) { c.vbr = enable }
}

// NewEncoder creates a streaming MP3 encoder. It encodes interleaved float64
// samples and writes a native MP3 byte stream to w.
//
// sampleRate and channels are baked into the encoder at init; every
// subsequent [codec.Encoder.Write] must supply a [mutations.Audio] with
// matching SampleRate and Channels.
//
// Close must be called to flush the final frames (including the encoder's
// trailing flush); the underlying writer is not closed.
func NewEncoder(w io.Writer, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	cfg := encoderConfig{bitRate: 128000, quality: 3}
	for _, o := range opts {
		o(&cfg)
	}

	info := mp3lib.StreamInfo{
		SampleRate: sampleRate,
		Channels:   channels,
		BitRate:    cfg.bitRate,
	}
	libOpts := []mp3lib.EncoderOption{
		mp3lib.WithBitRate(cfg.bitRate),
		mp3lib.WithQuality(cfg.quality),
	}
	if cfg.vbr {
		libOpts = append(libOpts, mp3lib.WithVBR(true))
	}

	enc, err := mp3lib.NewEncoder(w, info, libOpts...)
	if err != nil {
		return nil, err
	}
	return &encoder{
		enc:        enc,
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}
