// Package flac implements the FLAC (Free Lossless Audio Codec) format.
//
// FLAC is a lossless codec: decoding then re-encoding a stream reproduces
// the original samples bit-for-bit. Samples are integers between 4 and 32
// bits per sample, organised into frames of 16–65535 samples per channel.
//
// A [Decoder] consumes a FLAC byte stream from an [io.Reader] and produces
// interleaved int32 samples sign-extended from the stream's bit depth.
// An [Encoder] consumes interleaved int32 samples and writes a FLAC byte
// stream to an [io.Writer].
//
// Sample interleaving follows the toolkit-wide convention: for stereo,
// samples are laid out as [L0, R0, L1, R1, …]. The number of int32 values
// returned per [Decoder.Decode] call is therefore samplesPerChannel ×
// [Decoder.Channels].
//
// When built with cgo, both Decoder and Encoder use the vendored libFLAC
// reference implementation under libraries/flac/libflac. The pure-Go port
// (a 1:1 translation, implemented in libraries/flac/internal/nativeflac)
// is always available via NewNativeDecoder / NewNativeEncoder regardless
// of build tags. The flac_strict build tag does not gate the port's
// existence — it only selects the FMA-free build that is bit-exact with
// the reference; the default build is within PSNR noise but may fuse.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package flac

import (
	"io"
)

// FLAC format limits, as defined by RFC 9639 / the libFLAC headers.
const (
	// MinBlockSize is the smallest legal block size in samples per channel.
	MinBlockSize = 16

	// MaxBlockSize is the largest legal block size in samples per channel.
	MaxBlockSize = 65535

	// MaxChannels is the maximum number of channels in a FLAC stream.
	MaxChannels = 8

	// MinBitsPerSample is the smallest legal sample resolution.
	MinBitsPerSample = 4

	// MaxBitsPerSample is the largest legal sample resolution. Decoded
	// samples are returned as int32 sign-extended to the full 32 bits;
	// the in-stream value is reported via [Decoder.BitsPerSample].
	MaxBitsPerSample = 32

	// MaxSampleRate is the largest legal sample rate, in Hz.
	MaxSampleRate = 1048575

	// MaxLPCOrder is the largest LPC predictor order supported by the
	// FLAC format.
	MaxLPCOrder = 32
)

// StreamInfo carries the values of a FLAC STREAMINFO metadata block.
// Decoders populate it from the stream header; encoders accept it as
// configuration and emit it as the first metadata block.
//
// MinFrameSize, MaxFrameSize, and TotalSamples are zero in a StreamInfo
// supplied to [NewEncoder] — the encoder fills them in on close. MD5 is
// likewise computed by the encoder; callers may leave it zero.
type StreamInfo struct {
	// SampleRate is the sample rate in Hz, in the range [1, MaxSampleRate].
	SampleRate int

	// Channels is the channel count, in the range [1, MaxChannels].
	Channels int

	// BitsPerSample is the per-sample bit depth, in the range
	// [MinBitsPerSample, MaxBitsPerSample].
	BitsPerSample int

	// MinBlockSize and MaxBlockSize bound the per-frame block size in
	// samples per channel. When equal, the stream uses a fixed block
	// size; otherwise it is variable.
	MinBlockSize int
	MaxBlockSize int

	// MinFrameSize and MaxFrameSize bound the on-wire size of a frame in
	// bytes. Zero means "unknown" (the encoder fills these in on close).
	MinFrameSize int
	MaxFrameSize int

	// TotalSamples is the total number of inter-channel samples in the
	// stream, or zero if unknown / streaming.
	TotalSamples uint64

	// MD5Signature is the MD5 of the unencoded interleaved sample data
	// (using the canonical little-endian byte ordering described in the
	// FLAC format spec). All-zero means "not computed".
	MD5Signature [16]byte
}

// Decoder decodes a FLAC byte stream into interleaved int32 samples.
//
// Samples are sign-extended from the stream's [Decoder.BitsPerSample]
// to fill int32. For stereo: [L0, R0, L1, R1, …].
type Decoder interface {
	// Decode fills buf with interleaved samples and returns the number
	// of samples-per-channel produced (so the populated portion of buf
	// is buf[:n*Channels()]). A short read is valid; callers should keep
	// calling Decode until io.EOF.
	//
	// buf must have capacity for at least one full FLAC block — that is,
	// MaxBlockSize × Channels() int32 values — to guarantee a non-zero
	// return for any well-formed stream. Smaller buffers may still
	// succeed for streams whose actual block size is bounded.
	Decode(buf []int32) (samplesPerChannel int, err error)

	// StreamInfo returns the parsed STREAMINFO block. It is valid after
	// the first successful Decode call (or earlier if the caller has
	// driven the metadata phase explicitly via Reset+Decode).
	StreamInfo() StreamInfo

	// SampleRate returns the stream sample rate in Hz.
	SampleRate() int

	// Channels returns the channel count.
	Channels() int

	// BitsPerSample returns the per-sample bit depth in the stream.
	BitsPerSample() int

	// Vendor returns the VORBIS_COMMENT vendor string parsed from the
	// stream, or "" if the stream has no VORBIS_COMMENT block. Valid
	// after the first successful Decode call.
	Vendor() string

	// Tags returns VORBIS_COMMENT tag entries parsed from the stream
	// as a multi-value map keyed by upper-case Vorbis-comment names
	// (TITLE, ARTIST, …). Valid after the first successful Decode
	// call. Returns nil when the stream has no VORBIS_COMMENT block.
	Tags() map[string][]string

	// Reset clears decoder state and rewinds to the start of the
	// underlying reader. Reset returns an error if the reader does
	// not implement [io.Seeker].
	Reset() error

	// Close releases native resources. After Close the Decoder must
	// not be used.
	Close() error
}

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	md5Check bool
}

// WithMD5Check enables verification of the STREAMINFO MD5 signature
// against the decoded samples. When the signature does not match, the
// final [Decoder.Decode] call returns [ErrMD5Mismatch] alongside the
// last samples and io.EOF — i.e., the mismatch is reported but does
// not discard valid output.
//
// Default: disabled (matching libFLAC's default).
func WithMD5Check(enable bool) DecoderOption {
	return func(c *decoderConfig) { c.md5Check = enable }
}

// NewDecoder creates a Decoder reading a FLAC byte stream from r.
//
// When built with cgo (the default), NewDecoder uses the vendored
// libFLAC. Use [NewNativeDecoder] to force the pure-Go implementation.
func NewDecoder(r io.Reader, opts ...DecoderOption) (Decoder, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return newDecoder(r, cfg)
}

// NewNativeDecoder creates a Decoder using the pure-Go FLAC port,
// regardless of whether cgo is available. Useful for benchmarking the
// Go port against libFLAC.
func NewNativeDecoder(r io.Reader, opts ...DecoderOption) (Decoder, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeDecoder(r, cfg)
}

// Encoder encodes interleaved int32 samples into a FLAC byte stream.
//
// Samples must already be sign-extended to int32 from the configured
// bit depth: a 16-bit sample with value −1 is the int32 −1, not
// 0x0000_FFFF.
type Encoder interface {
	// Encode submits a block of interleaved samples (length must be a
	// multiple of [StreamInfo.Channels]) for compression. Encode may
	// emit zero, one, or many output frames; output is delivered via
	// the [io.Writer] passed to [NewEncoder].
	Encode(buf []int32) error

	// Close flushes pending frames, writes the final metadata, and
	// releases native resources. After Close the Encoder must not be
	// used.
	Close() error
}

// EncoderOption configures an [Encoder].
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	compression  int
	verify       bool
	blockSize    int
	totalSamples uint64
	vendor       string
	tags         [][2]string // ordered "KEY=VALUE" pairs, KEY upper-cased by caller
}

// WithCompressionLevel sets the encoder compression level, in the
// range [0, 8]. Higher levels yield smaller files at the cost of
// encoder CPU. Default: 5 (matches libFLAC).
func WithCompressionLevel(level int) EncoderOption {
	return func(c *encoderConfig) { c.compression = level }
}

// WithVerify enables encoder self-verification: every encoded frame is
// fed through an in-process decoder and compared against the input.
// Costs roughly one decode worth of CPU on top of encoding.
func WithVerify(enable bool) EncoderOption {
	return func(c *encoderConfig) { c.verify = enable }
}

// WithBlockSize sets a fixed block size (samples per channel) for
// encoded frames. Zero (the default) lets the encoder pick.
func WithBlockSize(samples int) EncoderOption {
	return func(c *encoderConfig) { c.blockSize = samples }
}

// WithTotalSamples declares the total number of inter-channel samples
// the caller will submit before [Encoder.Close]. When non-zero, the
// encoder writes the value into STREAMINFO; when zero, STREAMINFO
// reports an unknown total.
func WithTotalSamples(n uint64) EncoderOption {
	return func(c *encoderConfig) { c.totalSamples = n }
}

// WithVendor sets the vendor string of the emitted VORBIS_COMMENT
// metadata block. Note: when the cgo backend (libFLAC) is in use, this
// value is silently overridden — libFLAC always rewrites the vendor
// string with its own library identification (e.g. "reference libFLAC
// 1.5.0 …") in [stream_encoder_framing.c]. The option is honoured by
// the pure-Go port. Set it for forward compatibility / round-trip
// testing; for production reads use [Decoder.Vendor] instead.
func WithVendor(vendor string) EncoderOption {
	return func(c *encoderConfig) { c.vendor = vendor }
}

// WithTag adds a single VORBIS_COMMENT entry. The key is upper-cased
// per Vorbis comment convention. Repeated calls with the same key
// append additional values (e.g., two ARTIST entries).
func WithTag(key, value string) EncoderOption {
	return func(c *encoderConfig) {
		c.tags = append(c.tags, [2]string{upper(key), value})
	}
}

// WithTags adds many VORBIS_COMMENT entries at once. Iteration order
// of the input map is unspecified — use [WithTag] in sequence if a
// stable order is required.
func WithTags(tags map[string][]string) EncoderOption {
	return func(c *encoderConfig) {
		for k, vs := range tags {
			ku := upper(k)
			for _, v := range vs {
				c.tags = append(c.tags, [2]string{ku, v})
			}
		}
	}
}

// upper is a tiny ASCII-only ToUpper. VORBIS_COMMENT keys are restricted
// to ASCII 0x20..0x7D excluding '=' (RFC 9639 §8.6) so this is safe.
func upper(s string) string {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			if b == nil {
				b = []byte(s)
			}
			b[i] = c - ('a' - 'A')
		}
	}
	if b == nil {
		return s
	}
	return string(b)
}

// NewEncoder creates an Encoder writing a FLAC byte stream to w.
// The stream's sample rate, channel count, and bit depth come from
// info; the encoder will write info.SampleRate, info.Channels, and
// info.BitsPerSample into the STREAMINFO metadata block.
//
// Block sizes, frame sizes, total samples, and the MD5 signature on
// info are ignored — the encoder fills them on Close. To set a fixed
// block size, use [WithBlockSize].
//
// When built with cgo (the default), NewEncoder uses the vendored
// libFLAC. Use [NewNativeEncoder] to force the pure-Go implementation.
func NewEncoder(w io.Writer, info StreamInfo, opts ...EncoderOption) (Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	if err := validateStreamInfo(info); err != nil {
		return nil, err
	}
	cfg := encoderConfig{compression: 5}
	for _, o := range opts {
		o(&cfg)
	}
	return newEncoder(w, info, cfg)
}

// NewNativeEncoder creates an Encoder using the pure-Go FLAC port,
// regardless of whether cgo is available.
func NewNativeEncoder(w io.Writer, info StreamInfo, opts ...EncoderOption) (Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	if err := validateStreamInfo(info); err != nil {
		return nil, err
	}
	cfg := encoderConfig{compression: 5}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeEncoder(w, info, cfg)
}

func validateStreamInfo(info StreamInfo) error {
	if info.SampleRate < 1 || info.SampleRate > MaxSampleRate {
		return ErrBadSampleRate
	}
	if info.Channels < 1 || info.Channels > MaxChannels {
		return ErrBadChannels
	}
	if info.BitsPerSample < MinBitsPerSample || info.BitsPerSample > MaxBitsPerSample {
		return ErrBadBitDepth
	}
	return nil
}
