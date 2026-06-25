package hwaccel

import "github.com/daniel-sullivan/go-mediatoolkit/video"

// Config describes an encoder or decoder to build. It is populated with
// functional Options passed to NewConfig (or directly to the Open*
// entry points). The zero Config is invalid; at minimum Codec, Width,
// and Height must be set for an encoder.
//
// Not every field is meaningful to every direction: bitrate, framerate,
// and profile shape the encoder; a decoder uses only Codec (and learns
// resolution from the stream's parameter sets). Backends ignore fields
// they do not honour.
type Config struct {
	// Codec is the compressed format to encode to / decode from.
	Codec video.Codec
	// Width and Height are the coded luma dimensions in pixels.
	Width  int
	Height int
	// Bitrate is the target average bitrate in bits per second. Zero
	// lets the backend pick a default.
	Bitrate int
	// FrameRateNum / FrameRateDen express the frame rate as a rational
	// (e.g. 30000/1001). A zero denominator is treated as
	// FrameRateNum/1; a zero numerator lets the backend default.
	FrameRateNum int
	FrameRateDen int
	// Profile names the requested codec profile ("baseline", "main",
	// "high", "main10"). Empty selects the backend default.
	Profile string
	// KeyframeInterval is the maximum number of frames between
	// keyframes (the GOP length). Zero lets the backend choose.
	KeyframeInterval int
	// PixelFormat is the raw frame layout the encoder will be fed. Zero
	// (PixelFormatUnknown) lets the encoder accept whatever the first
	// frame carries among the formats it supports.
	PixelFormat video.PixelFormat
}

// Option mutates a Config. Options are applied in order.
type Option func(*Config)

// NewConfig builds a Config from options.
func NewConfig(opts ...Option) Config {
	var c Config
	for _, opt := range opts {
		opt(&c)
	}
	return c
}

// WithCodec sets the compressed codec.
func WithCodec(c video.Codec) Option {
	return func(cfg *Config) { cfg.Codec = c }
}

// WithResolution sets the coded width and height in pixels.
func WithResolution(w, h int) Option {
	return func(cfg *Config) { cfg.Width, cfg.Height = w, h }
}

// WithBitrate sets the target average bitrate in bits per second.
func WithBitrate(bps int) Option {
	return func(cfg *Config) { cfg.Bitrate = bps }
}

// WithFrameRate sets the frame rate as a rational num/den. Pass den == 1
// for integer rates.
func WithFrameRate(num, den int) Option {
	return func(cfg *Config) { cfg.FrameRateNum, cfg.FrameRateDen = num, den }
}

// WithProfile sets the requested codec profile.
func WithProfile(p string) Option {
	return func(cfg *Config) { cfg.Profile = p }
}

// WithKeyframeInterval sets the maximum GOP length in frames.
func WithKeyframeInterval(frames int) Option {
	return func(cfg *Config) { cfg.KeyframeInterval = frames }
}

// WithPixelFormat sets the raw input pixel format for an encoder.
func WithPixelFormat(p video.PixelFormat) Option {
	return func(cfg *Config) { cfg.PixelFormat = p }
}

// validateEncode returns ErrInvalidConfig unless the Config has the
// fields an encoder requires: a known codec and a non-zero resolution.
func (c Config) validateEncode() error {
	if c.Codec == video.CodecUnknown {
		return ErrInvalidConfig
	}
	if c.Width <= 0 || c.Height <= 0 {
		return ErrInvalidConfig
	}
	return nil
}

// validateDecode returns ErrInvalidConfig unless the Config names a
// known codec. A decoder learns geometry from the bitstream.
func (c Config) validateDecode() error {
	if c.Codec == video.CodecUnknown {
		return ErrInvalidConfig
	}
	return nil
}

// frameRate returns the configured frame rate as a float, defaulting to
// 30.0 when unset.
func (c Config) frameRate() float64 {
	if c.FrameRateNum <= 0 {
		return 30.0
	}
	den := c.FrameRateDen
	if den <= 0 {
		den = 1
	}
	return float64(c.FrameRateNum) / float64(den)
}
