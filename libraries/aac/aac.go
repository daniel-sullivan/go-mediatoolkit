// Package aac implements the Advanced Audio Coding (AAC) packet codec.
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
// AAC is a lossy, packet-based codec: an AAC stream is a sequence of
// independent access units (raw data blocks / packets), each decoding to
// a fixed number of samples per channel (1024 for AAC-LC, or 2048 for the
// long-frame variants). Because AAC, like Opus, has no single canonical
// byte-stream framing — access units may be carried in ADTS, LATM/LOAS,
// or an ISOBMFF/MP4 sample table — this library works on individual
// packets. Framing is the container layer's concern (see
// [go-mediatoolkit/containers/mp4]).
//
// A [Decoder] takes one AAC access unit ([]byte) and produces interleaved
// float64 PCM. An [Encoder] takes interleaved float64 PCM (exactly one
// frame per call) and produces one AAC access unit. All audio data follows
// the go-mediatoolkit convention of interleaved float64 samples normalized
// to [-1.0, 1.0]; for stereo: [L0, R0, L1, R1, ...].
//
// When built with cgo, both Decoder and Encoder use the vendored C
// reference implementation (libfdk-aac) under libraries/aac. The pure-Go
// port — a 1:1 translation, implemented in
// libraries/aac/internal/nativeaac — is always available via
// [NewNativeDecoder] / [NewNativeEncoder] regardless of build tags. The
// FDK-AAC engine is fixed-point (int32 Q-format) in both the cgo and pure-Go
// paths, so parity is EXACT integer equality on decode (the pure-Go port
// reproduces the vendored C libfdk-aac int32 PCM bit-for-bit) and a
// byte-identical AAC access unit on encode. There are no floating-point or
// FMA concerns; the aac_strict build tag un-skips integer-parity assertions
// and test gates.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package aac

// AAC format limits. AAC-LC frames decode to 1024 samples per channel;
// the SBR/long-frame variants decode to 2048. These bound the sizes a
// caller must reserve for decode buffers and packet allocations.
const (
	// FrameSamplesShort is the samples-per-channel produced by a single
	// AAC-LC access unit (the common short-frame length).
	FrameSamplesShort = 1024

	// FrameSamplesLong is the samples-per-channel produced by a single
	// long-frame (e.g. SBR-doubled) access unit.
	FrameSamplesLong = 2048

	// MaxChannels is the maximum number of channels addressable by the
	// channel-configuration field of an AudioSpecificConfig (1..7 map to
	// the standard configs; 7 denotes 7.1, i.e. eight physical channels).
	MaxChannels = 8

	// MaxFrameBytes bounds the on-wire size of a single AAC access unit.
	// (An ADTS frame length field is 13 bits, i.e. up to 8191 bytes
	// including its header.)
	MaxFrameBytes = 8191

	// MaxSampleRate is the largest sample rate expressible by the
	// AudioSpecificConfig sampling-frequency table (96000 Hz is index 0;
	// the explicit-frequency escape allows larger, but the standard
	// decode path is bounded by this).
	MaxSampleRate = 96000
)

// AudioObjectType identifies the AAC profile / coding tool set, as encoded
// in the first field of an [AudioSpecificConfig]. Only the subset relevant
// to this codec's decode/encode paths is enumerated; the underlying value
// is the MPEG-4 Audio Object Type index.
type AudioObjectType int

const (
	// AOTNull is the unset/invalid object type (index 0).
	AOTNull AudioObjectType = iota

	// AOTAACMain is AAC Main profile (index 1).
	AOTAACMain

	// AOTAACLC is AAC Low Complexity — the dominant profile, used by
	// virtually all .m4a files (index 2).
	AOTAACLC

	// AOTAACSSR is AAC Scalable Sample Rate (index 3).
	AOTAACSSR

	// AOTAACLTP is AAC Long Term Prediction (index 4).
	AOTAACLTP

	// AOTSBR is Spectral Band Replication (HE-AAC v1) (index 5).
	AOTSBR
)

// AOTPS is Parametric Stereo (HE-AAC v2) — the object type that layers
// parametric stereo on top of SBR over a mono AAC-LC core, so the decoded
// output is stereo (index 29). It is not contiguous with the lower object
// types, so it is declared explicitly rather than via iota.
const AOTPS AudioObjectType = 29

// String returns the short name of the object type.
func (a AudioObjectType) String() string {
	switch a {
	case AOTAACMain:
		return "AAC-Main"
	case AOTAACLC:
		return "AAC-LC"
	case AOTAACSSR:
		return "AAC-SSR"
	case AOTAACLTP:
		return "AAC-LTP"
	case AOTSBR:
		return "SBR"
	case AOTPS:
		return "PS"
	default:
		return "Unknown"
	}
}

// AudioSpecificConfig is the MPEG-4 AudioSpecificConfig (ASC) — the
// out-of-band decoder-configuration record that precedes the AAC access
// units in a container (carried in the MP4 esds box, see
// [go-mediatoolkit/containers/mp4]). It tells the decoder the profile,
// sample rate, and channel layout needed to interpret the packets.
//
// The raw ASC bytes are preserved verbatim so the container layer can copy
// them byte-for-byte; the parsed fields are a convenience projection.
type AudioSpecificConfig struct {
	// ObjectType is the AAC profile / coding tool set.
	ObjectType AudioObjectType

	// SampleRate is the decoder sample rate in Hz. When the ASC encodes
	// the sampling-frequency index, this is the resolved value; when it
	// uses the explicit-frequency escape (index 15), this is that value.
	SampleRate int

	// Channels is the channel count derived from the channel-configuration
	// field. Zero means the configuration is "defined in AOT-specific
	// config" and must be taken from the first decoded frame.
	Channels int

	// FrameSamples is the samples-per-channel each access unit decodes to
	// ([FrameSamplesShort] or [FrameSamplesLong]).
	FrameSamples int

	// Raw is the verbatim ASC byte string as found in the container, so a
	// re-muxer can copy it without re-serialising.
	Raw []byte
}

// StreamInfo summarises the stream-level parameters a decoder reports and
// an encoder is configured with. It is the AAC analogue of
// [go-mediatoolkit/libraries/flac.StreamInfo].
type StreamInfo struct {
	// Config is the AudioSpecificConfig describing the stream.
	Config AudioSpecificConfig

	// SampleRate is the output sample rate in Hz (mirrors Config.SampleRate
	// after any SBR rate-doubling has been applied).
	SampleRate int

	// Channels is the output channel count.
	Channels int

	// FrameSamples is the samples-per-channel of one decoded access unit.
	FrameSamples int
}

// Decoder decodes AAC access units to interleaved float64 PCM.
//
// One [Decoder.Decode] call consumes exactly one access unit and produces
// one frame of samples. For stereo the output is [L0, R0, L1, R1, ...].
type Decoder interface {
	// Decode decodes a single AAC access unit into pcm and returns the
	// number of samples per channel produced (so the populated portion of
	// pcm is pcm[:n*Channels()]). pcm must have capacity for at least one
	// full frame — FrameSamples × Channels() float64 values.
	Decode(pkt []byte, pcm []float64) (samplesPerChannel int, err error)

	// SampleRate returns the output sample rate in Hz.
	SampleRate() int

	// Channels returns the number of output channels.
	Channels() int

	// Config returns the AudioSpecificConfig the decoder was created with.
	Config() AudioSpecificConfig

	// Reset clears all decoder state for reuse with a new stream of the
	// same configuration.
	Reset()
}

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	// reserved for future decode-time options (e.g. SBR downsample mode).
}

// NewDecoder creates a Decoder for the stream described by asc.
//
// When built with cgo the decoder uses the vendored C reference; otherwise
// it uses the pure-Go port. Use [NewNativeDecoder] to force the pure-Go
// path regardless of build tags.
func NewDecoder(asc AudioSpecificConfig, opts ...DecoderOption) (Decoder, error) {
	if asc.Channels < 1 || asc.Channels > MaxChannels {
		return nil, ErrBadArg
	}
	var cfg decoderConfig
	for _, o := range opts {
		o(&cfg)
	}
	return newDecoder(asc, cfg)
}

// NewNativeDecoder creates a Decoder that always uses the pure-Go port,
// regardless of whether cgo is enabled. Useful for benchmarking and for
// pure-Go consumers.
func NewNativeDecoder(asc AudioSpecificConfig, opts ...DecoderOption) (Decoder, error) {
	if asc.Channels < 1 || asc.Channels > MaxChannels {
		return nil, ErrBadArg
	}
	var cfg decoderConfig
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeDecoder(asc, cfg)
}

// Encoder encodes interleaved float64 PCM to AAC access units.
//
// One [Encoder.Encode] call consumes exactly one frame of interleaved
// samples and produces one access unit.
type Encoder interface {
	// Encode encodes one frame of interleaved PCM (FrameSamples ×
	// Channels() samples) into a single AAC access unit and returns it.
	Encode(pcm []float64) ([]byte, error)

	// Config returns the AudioSpecificConfig describing the encoded
	// stream — the bytes the container layer must emit (e.g. in esds).
	Config() AudioSpecificConfig

	// SampleRate returns the input sample rate in Hz.
	SampleRate() int

	// Channels returns the number of input channels.
	Channels() int

	// Reset resets the encoder state for a new stream.
	Reset()
}

// EncoderOption configures an [Encoder].
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	objectType AudioObjectType
	bitrate    int
	// vbrMode is 0 for CBR (use bitrate) or 1..5 for one of the five AAC VBR
	// quality modes (AACENC_BITRATEMODE). When non-zero the encoder runs in VBR:
	// the nominal bitrate is derived from the mode + channel mode and the bitrate
	// field is ignored.
	vbrMode int
}

// WithObjectType selects the AAC profile to encode (default [AOTAACLC]).
func WithObjectType(t AudioObjectType) EncoderOption {
	return func(c *encoderConfig) {
		c.objectType = t
	}
}

// WithBitrate sets the target bitrate in bits per second (constant-bitrate
// mode). Ignored when [WithVBR] selects a variable-bitrate mode.
func WithBitrate(bps int) EncoderOption {
	return func(c *encoderConfig) {
		c.bitrate = bps
	}
}

// WithVBR selects variable-bitrate encoding at the given quality mode (1..5,
// lowest to highest quality / bitrate), mirroring fdk-aac's AACENC_BITRATEMODE.
// In VBR the nominal bitrate is derived from the quality mode and channel mode,
// so [WithBitrate] is ignored. A quality outside 1..5 is clamped into range.
func WithVBR(quality int) EncoderOption {
	return func(c *encoderConfig) {
		if quality < 1 {
			quality = 1
		} else if quality > 5 {
			quality = 5
		}
		c.vbrMode = quality
	}
}

// NewEncoder creates an Encoder for the given sample rate and channel
// count.
//
// When built with cgo the encoder uses the vendored C reference; otherwise
// it uses the pure-Go port. Use [NewNativeEncoder] to force the pure-Go
// path regardless of build tags.
func NewEncoder(sampleRate, channels int, opts ...EncoderOption) (Encoder, error) {
	if err := validateFormat(sampleRate, channels); err != nil {
		return nil, err
	}
	cfg := encoderConfig{
		objectType: AOTAACLC,
		bitrate:    128000,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return newEncoder(sampleRate, channels, cfg)
}

// NewNativeEncoder creates an Encoder that always uses the pure-Go port,
// regardless of whether cgo is enabled.
func NewNativeEncoder(sampleRate, channels int, opts ...EncoderOption) (Encoder, error) {
	if err := validateFormat(sampleRate, channels); err != nil {
		return nil, err
	}
	cfg := encoderConfig{
		objectType: AOTAACLC,
		bitrate:    128000,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeEncoder(sampleRate, channels, cfg)
}

func validateFormat(sampleRate, channels int) error {
	if sampleRate < 1 || sampleRate > MaxSampleRate {
		return ErrBadArg
	}
	if channels < 1 || channels > MaxChannels {
		return ErrBadArg
	}
	return nil
}
