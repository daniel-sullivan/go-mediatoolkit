// Package opus provides streaming Opus encoding and decoding between Opus
// packets and interleaved float64 samples.
//
// Because Opus has no single standard byte-stream framing, this package uses
// [PacketReader] and [PacketWriter] interfaces for packet I/O. Callers provide
// their own framing implementation (OGG, WebM, RTP, length-prefixed, etc.).
//
// The encoder and decoder wrap the [go-mediatoolkit/libraries/opus] package,
// adding streaming semantics and buffering so callers can read/write arbitrary
// sample counts without worrying about Opus frame boundaries.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package opus

import (
	"go-mediatoolkit/codec"
	opuslib "go-mediatoolkit/libraries/opus"
	"go-mediatoolkit/mutations"
	"io"
)

// PacketReader reads individual Opus packets from a source.
// The caller is responsible for framing (e.g., reading from an OGG container).
type PacketReader interface {
	// ReadPacket returns the next Opus packet. Returns io.EOF when no more
	// packets are available.
	ReadPacket() ([]byte, error)
}

// PacketWriter writes individual Opus packets to a destination.
type PacketWriter interface {
	// WritePacket writes an encoded Opus packet.
	WritePacket(data []byte) error
}

// DecoderOption configures a Decoder.
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	gain float64
}

// WithGain applies a fixed gain in dB to the decoded output.
func WithGain(dB float64) DecoderOption {
	return func(c *decoderConfig) {
		c.gain = dB
	}
}

// NewDecoder creates a streaming Opus decoder. It reads Opus packets from pr
// and decodes them to interleaved float64 samples.
//
// Supported sample rates: 8000, 12000, 16000, 24000, 48000.
// Supported channels: 1 (mono) or 2 (stereo).
func NewDecoder(pr PacketReader, sampleRate, channels int, opts ...DecoderOption) (codec.Decoder, error) {
	if err := validateParams(sampleRate, channels); err != nil {
		return nil, err
	}

	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	var libOpts []opuslib.DecoderOption
	if cfg.gain != 0 {
		libOpts = append(libOpts, opuslib.WithGain(cfg.gain))
	}

	dec, err := opuslib.NewDecoder(sampleRate, channels, libOpts...)
	if err != nil {
		return nil, err
	}

	maxSamples := opuslib.MaxFrameSize(sampleRate) * channels
	return &decoder{
		pr:         pr,
		channels:   channels,
		sampleRate: sampleRate,
		dec:        dec,
		frameBuf:   make([]float64, maxSamples),
		remainder:  make([]float64, maxSamples),
	}, nil
}

// EncoderOption configures an Encoder.
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	bitrate         int
	complexity      int
	application     opuslib.Application
	frameDurationMs float64
}

// WithBitrate sets the target bitrate in bits per second.
func WithBitrate(bps int) EncoderOption {
	return func(c *encoderConfig) {
		c.bitrate = bps
	}
}

// WithComplexity sets the encoder complexity (0-10, higher = better quality).
func WithComplexity(complexity int) EncoderOption {
	return func(c *encoderConfig) {
		c.complexity = complexity
	}
}

// WithApplication sets the Opus application hint.
func WithApplication(app opuslib.Application) EncoderOption {
	return func(c *encoderConfig) {
		c.application = app
	}
}

// WithFrameDuration sets the frame duration in milliseconds.
// Supported values: 2.5, 5, 10, 20, 40, 60. Default: 20.
func WithFrameDuration(ms float64) EncoderOption {
	return func(c *encoderConfig) {
		c.frameDurationMs = ms
	}
}

// NewEncoder creates a streaming Opus encoder. It encodes interleaved float64
// samples and writes Opus packets to pw.
//
// Supported sample rates: 8000, 12000, 16000, 24000, 48000.
// Supported channels: 1 (mono) or 2 (stereo).
//
// Close must be called to flush any remaining buffered samples. The final
// frame is padded with silence if it contains fewer samples than a full frame.
func NewEncoder(pw PacketWriter, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error) {
	if err := validateParams(sampleRate, channels); err != nil {
		return nil, err
	}

	cfg := encoderConfig{
		bitrate:         64000,
		complexity:      10,
		application:     opuslib.AppAudio,
		frameDurationMs: 20,
	}
	for _, o := range opts {
		o(&cfg)
	}

	var libOpts []opuslib.EncoderOption
	if cfg.bitrate != 64000 {
		libOpts = append(libOpts, opuslib.WithBitrate(cfg.bitrate))
	}
	if cfg.complexity != 10 {
		libOpts = append(libOpts, opuslib.WithComplexity(cfg.complexity))
	}
	if cfg.application != opuslib.AppAudio {
		libOpts = append(libOpts, opuslib.WithApplication(cfg.application))
	}

	enc, err := opuslib.NewEncoder(sampleRate, channels, libOpts...)
	if err != nil {
		return nil, err
	}

	// Samples per channel for this frame duration.
	samplesPerCh := int(cfg.frameDurationMs * float64(sampleRate) / 1000.0)
	frameSamples := samplesPerCh * channels

	return &encoder{
		pw:         pw,
		channels:   channels,
		sampleRate: sampleRate,
		enc:        enc,
		chunker:    mutations.NewStreamChunker(frameSamples),
		maxPktSize: 4000,
	}, nil
}

func validateParams(sampleRate, channels int) error {
	switch sampleRate {
	case opuslib.Rate8000, opuslib.Rate12000, opuslib.Rate16000, opuslib.Rate24000, opuslib.Rate48000:
	default:
		return ErrBadSampleRate
	}
	if channels < 1 || channels > 2 {
		return ErrBadChannels
	}
	return nil
}

// PacketReaderFunc adapts a function into a [PacketReader].
type PacketReaderFunc func() ([]byte, error)

func (f PacketReaderFunc) ReadPacket() ([]byte, error) { return f() }

// PacketWriterFunc adapts a function into a [PacketWriter].
type PacketWriterFunc func([]byte) error

func (f PacketWriterFunc) WritePacket(data []byte) error { return f(data) }

// NewSlicePacketReader returns a PacketReader that yields packets from a slice.
// Returns io.EOF after all packets have been read.
func NewSlicePacketReader(packets [][]byte) PacketReader {
	i := 0
	return PacketReaderFunc(func() ([]byte, error) {
		if i >= len(packets) {
			return nil, io.EOF
		}
		pkt := packets[i]
		i++
		return pkt, nil
	})
}
