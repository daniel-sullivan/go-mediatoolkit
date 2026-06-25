// Package aac provides streaming AAC encoding and decoding between AAC
// access units (packets) and interleaved float64 samples.
//
// AAC = packet codec (codec/aac via PacketReader/PacketWriter interface,
// mirroring the codec/opus archetype). M4A = ISO Base Media File Format
// (ISOBMFF/MP4) container parsed in pure Go by containers/mp4 box reader
// (ftyp/moov/mdat structures, esds AudioSpecificConfig, sample tables
// stsz/stsc/stco, ilst iTunes metadata) with zero C code in the container
// layer. The packet-codec split is identical to codec/opus +
// containers/ogg/opus.go: the AAC library exposes packet-oriented
// encoding/decoding (Encoder.Encode(pcm []float64) -> []byte;
// Decoder.Decode(pkt []byte) -> samplesPerChannel int), and codec/aac
// wraps this with StreamChunker + remainder buffering to support
// arbitrary-sized Read/Write calls from callers. FDK-AAC is fixed-point, so
// parity is EXACT integer equality on decode (the pure-Go port reproduces the
// vendored C libfdk-aac int32 PCM bit-for-bit) and a byte-identical AAC access
// unit on encode — there is no floating-point/ULP tolerance involved.
//
// Because AAC has no single standard byte-stream framing, this package
// uses [PacketReader] and [PacketWriter] interfaces for packet I/O, exactly
// as [github.com/daniel-sullivan/go-mediatoolkit/codec/opus] does. Callers provide their own framing
// implementation (MP4/ISOBMFF sample tables, ADTS, LATM/LOAS, …); tags and
// metadata are a container concern (see [github.com/daniel-sullivan/go-mediatoolkit/containers/mp4]),
// not a codec one.
//
// The encoder and decoder wrap the [github.com/daniel-sullivan/go-mediatoolkit/libraries/aac] package,
// adding streaming semantics and buffering (via [mutations.StreamChunker]
// plus a remainder buffer) so callers can read/write arbitrary sample
// counts without worrying about AAC access-unit boundaries.
//
// All audio data is interleaved float64 normalized to [-1.0, 1.0]; for
// stereo: [L0, R0, L1, R1, ...].
//
// Neither Decoder nor Encoder is safe for concurrent use.
package aac

import (
	"io"

	"github.com/daniel-sullivan/go-mediatoolkit/codec"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// PacketReader reads individual AAC access units from a source. The caller
// is responsible for framing (e.g., reading samples from an MP4 sample
// table via [github.com/daniel-sullivan/go-mediatoolkit/containers/mp4]).
type PacketReader interface {
	// ReadPacket returns the next AAC access unit. Returns io.EOF when no
	// more packets are available.
	ReadPacket() ([]byte, error)
}

// PacketWriter writes individual AAC access units to a destination.
type PacketWriter interface {
	// WritePacket writes one encoded AAC access unit.
	WritePacket(data []byte) error
}

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	// reserved for future decode-time options.
}

// NewDecoder creates a streaming AAC decoder. It reads AAC access units
// from pr and decodes them to interleaved float64 samples.
//
// asc is the AudioSpecificConfig describing the stream — typically the
// bytes parsed from the MP4 esds box. It carries the sample rate, channel
// count, and profile the decoder needs.
//
// HE-AAC is routed by object type: an AOT-5 (SBR / HE-AAC v1) stream reports
// the SBR-doubled output sample rate, and an AOT-29 (PS / HE-AAC v2) stream
// reports a stereo (2-channel) output even though its AAC-LC core is mono.
// The reported rate/channels come from [aaclib.AudioSpecificConfig.Output],
// which resolves the explicit-hierarchical extension signalling in the ASC.
func NewDecoder(pr PacketReader, asc aaclib.AudioSpecificConfig, opts ...DecoderOption) (codec.Decoder, error) {
	var cfg decoderConfig
	for _, o := range opts {
		o(&cfg)
	}

	dec, err := aaclib.NewDecoder(asc)
	if err != nil {
		return nil, err
	}

	// Resolve the true decoded output format. For HE-AAC the underlying
	// engine reports the SBR-doubled rate / PS-stereo channel count only once
	// frames flow; asc.Output projects them up front from the ASC signalling
	// so the adapter advertises the correct format immediately. Fall back to
	// the engine's own report when the ASC carries no explicit extension.
	sampleRate, channels := asc.Output()
	if channels <= 0 {
		channels = dec.Channels()
	}
	if sampleRate <= 0 {
		sampleRate = dec.SampleRate()
	}

	frame := asc.FrameSamples
	if frame == 0 {
		frame = aaclib.FrameSamplesLong
	}
	// The decode scratch buffer must hold a full frame at the OUTPUT channel
	// count (PS widens a mono core to stereo, so size by the output channels).
	maxSamples := frame * channels

	return &decoder{
		pr:         pr,
		channels:   channels,
		sampleRate: sampleRate,
		dec:        dec,
		frameBuf:   make([]float64, maxSamples),
		remainder:  make([]float64, maxSamples),
	}, nil
}

// EncoderOption configures an [Encoder].
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	objectType aaclib.AudioObjectType
	bitrate    int
}

// WithObjectType selects the AAC profile to encode (default AAC-LC).
func WithObjectType(t aaclib.AudioObjectType) EncoderOption {
	return func(c *encoderConfig) {
		c.objectType = t
	}
}

// WithBitrate sets the target bitrate in bits per second.
func WithBitrate(bps int) EncoderOption {
	return func(c *encoderConfig) {
		c.bitrate = bps
	}
}

// NewEncoder creates a streaming AAC encoder. It encodes interleaved
// float64 samples and writes AAC access units to pw.
//
// Close must be called to flush any remaining buffered samples; the final
// frame is padded with silence if it contains fewer samples than a full
// frame. Use [Encoder.Config] (via a type assertion to the concrete type)
// or query the underlying library for the AudioSpecificConfig the container
// layer must emit.
func NewEncoder(pw PacketWriter, sampleRate, channels int, opts ...EncoderOption) (codec.Encoder, error) {
	cfg := encoderConfig{
		objectType: aaclib.AOTAACLC,
		bitrate:    128000,
	}
	for _, o := range opts {
		o(&cfg)
	}

	var libOpts []aaclib.EncoderOption
	libOpts = append(libOpts, aaclib.WithObjectType(cfg.objectType))
	if cfg.bitrate != 0 {
		libOpts = append(libOpts, aaclib.WithBitrate(cfg.bitrate))
	}

	enc, err := aaclib.NewEncoder(sampleRate, channels, libOpts...)
	if err != nil {
		return nil, err
	}

	frame := enc.Config().FrameSamples
	if frame == 0 {
		frame = aaclib.FrameSamplesShort
	}
	frameSamples := frame * channels

	return &encoder{
		pw:         pw,
		channels:   channels,
		sampleRate: sampleRate,
		enc:        enc,
		chunker:    mutations.NewStreamChunker(frameSamples),
	}, nil
}

// PacketReaderFunc adapts a function into a [PacketReader].
type PacketReaderFunc func() ([]byte, error)

func (f PacketReaderFunc) ReadPacket() ([]byte, error) { return f() }

// PacketWriterFunc adapts a function into a [PacketWriter].
type PacketWriterFunc func([]byte) error

func (f PacketWriterFunc) WritePacket(data []byte) error { return f(data) }

// NewSlicePacketReader returns a PacketReader that yields packets from a
// slice. Returns io.EOF after all packets have been read.
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
