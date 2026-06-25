// Package flac provides streaming FLAC encoding and decoding between a
// native FLAC byte stream and interleaved float64 samples.
//
// FLAC is byte-stream framed (the magic + metadata blocks + frames live
// in a single continuous stream), so the [Decoder] takes an [io.Reader]
// and the [Encoder] takes an [io.Writer] — no separate framing layer
// is required at this level. Use [github.com/daniel-sullivan/go-mediatoolkit/containers/flac] for
// metadata inspection and tag projection on top of the raw stream, or
// [github.com/daniel-sullivan/go-mediatoolkit/containers/ogg] for Ogg-FLAC encapsulation.
//
// Float64 samples are normalised to [-1.0, 1.0]: the decoder divides
// each integer sample by (2^(BitsPerSample-1)-1); the encoder
// multiplies by the same factor and saturates at the bit-depth limits
// to avoid integer overflow on values just above ±1.0.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package flac

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	flaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/flac"
)

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	md5Check bool
}

// WithMD5Check enables verification of the STREAMINFO MD5 signature
// against the decoded samples. See [libraries/flac.WithMD5Check] for
// details on when the mismatch is reported.
func WithMD5Check(enable bool) DecoderOption {
	return func(c *decoderConfig) { c.md5Check = enable }
}

// NewDecoder creates a streaming FLAC decoder. It reads a native FLAC
// byte stream from r — including the "fLaC" magic, metadata blocks and
// audio frames — and produces interleaved float64 samples.
//
// The decoder's SampleRate, Channels and BitsPerSample are not known
// until STREAMINFO has been parsed; they are surfaced as soon as
// [codec.Decoder.Read] returns its first samples (or after the first call
// returns io.EOF on a metadata-only stream). Until then [codec.Decoder.SampleRate]
// and [codec.Decoder.Channels] return zero.
func NewDecoder(r io.Reader, opts ...DecoderOption) (codec.Decoder, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	var libOpts []flaclib.DecoderOption
	if cfg.md5Check {
		libOpts = append(libOpts, flaclib.WithMD5Check(true))
	}
	dec, err := flaclib.NewDecoder(r, libOpts...)
	if err != nil {
		return nil, err
	}
	return &decoder{dec: dec}, nil
}

// EncoderOption configures an [Encoder].
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	bitsPerSample int
	compression   int
	verify        bool
	blockSize     int
	totalSamples  uint64
	tags          [][2]string
	vendor        string
}

// WithBitsPerSample sets the per-sample bit depth used to convert
// float64 samples to FLAC's integer encoding. Default: 16. Valid range
// is the FLAC format limit [4, 32].
func WithBitsPerSample(bits int) EncoderOption {
	return func(c *encoderConfig) { c.bitsPerSample = bits }
}

// WithCompressionLevel sets the libFLAC compression level [0, 8].
// Default: 5.
func WithCompressionLevel(level int) EncoderOption {
	return func(c *encoderConfig) { c.compression = level }
}

// WithVerify enables encoder self-verification.
func WithVerify(enable bool) EncoderOption {
	return func(c *encoderConfig) { c.verify = enable }
}

// WithBlockSize sets a fixed block size (samples per channel).
func WithBlockSize(samples int) EncoderOption {
	return func(c *encoderConfig) { c.blockSize = samples }
}

// WithTotalSamples declares the total number of inter-channel samples
// the caller will submit before [codec.Encoder.Close].
func WithTotalSamples(n uint64) EncoderOption {
	return func(c *encoderConfig) { c.totalSamples = n }
}

// WithTag adds a single VORBIS_COMMENT entry. The key is upper-cased
// per Vorbis-comment convention. Repeated calls append additional
// values (e.g., two ARTIST entries).
func WithTag(key, value string) EncoderOption {
	return func(c *encoderConfig) {
		c.tags = append(c.tags, [2]string{key, value})
	}
}

// WithVendor sets the VORBIS_COMMENT vendor string. See
// [libraries/flac.WithVendor] — libFLAC silently overrides the value
// when the cgo backend is in use.
func WithVendor(vendor string) EncoderOption {
	return func(c *encoderConfig) { c.vendor = vendor }
}

// NewEncoder creates a streaming FLAC encoder. It encodes interleaved
// float64 samples and writes a native FLAC byte stream to w.
//
// sampleRate and channels are baked into the STREAMINFO at init; every
// subsequent [codec.Encoder.Write] must supply an [mutations.Audio]
// with matching SampleRate and Channels.
//
// Close must be called to flush the final frame and write the trailing
// metadata; the underlying writer is not closed.
func NewEncoder(w io.Writer, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	cfg := encoderConfig{bitsPerSample: 16, compression: 5}
	for _, o := range opts {
		o(&cfg)
	}

	info := flaclib.StreamInfo{
		SampleRate:    sampleRate,
		Channels:      channels,
		BitsPerSample: cfg.bitsPerSample,
	}
	libOpts := []flaclib.EncoderOption{
		flaclib.WithCompressionLevel(cfg.compression),
	}
	if cfg.verify {
		libOpts = append(libOpts, flaclib.WithVerify(true))
	}
	if cfg.blockSize > 0 {
		libOpts = append(libOpts, flaclib.WithBlockSize(cfg.blockSize))
	}
	if cfg.totalSamples > 0 {
		libOpts = append(libOpts, flaclib.WithTotalSamples(cfg.totalSamples))
	}
	if cfg.vendor != "" {
		libOpts = append(libOpts, flaclib.WithVendor(cfg.vendor))
	}
	for _, kv := range cfg.tags {
		libOpts = append(libOpts, flaclib.WithTag(kv[0], kv[1]))
	}

	enc, err := flaclib.NewEncoder(w, info, libOpts...)
	if err != nil {
		return nil, err
	}
	return &encoder{
		enc:           enc,
		sampleRate:    sampleRate,
		channels:      channels,
		bitsPerSample: cfg.bitsPerSample,
	}, nil
}
