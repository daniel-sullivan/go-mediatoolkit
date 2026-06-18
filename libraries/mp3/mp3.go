// Package mp3 implements the MP3 (MPEG-1/2/2.5 Audio Layer III) format.
//
// MP3 is a self-framed codec: the byte stream itself carries frame sync
// headers, side information, and audio data in a continuous sequence.
// Following the FLAC archetype:
//
//   - codec/mp3: Implements codec.Decoder (io.Reader -> float64) and
//     codec.Encoder (float64 -> io.Writer), reading/writing raw MP3 frames
//     from a single continuous stream. No separate framing layer is required
//     at this level. Samples are normalized to [-1.0, 1.0] by dividing
//     integer samples by 2^(bits-1)-1 on decode; on encode, float64 is
//     scaled and rounded to 16-bit int.
//
//   - containers/mp3: Parses ID3v2 metadata tags (artist, title, album,
//     etc.) that precede the audio frames, and optionally ID3v1 tags at the
//     end of the file. Projects metadata onto containers.Header[Extras] with
//     format-specific tag handling. The encoder owns audio framing; the
//     container layer adds ID3 metadata writes on top.
//
//   - libraries/mp3: The core engine, operating on interleaved int16
//     samples. Exposes Decoder (frame-by-frame) and Encoder (frame-by-frame)
//     interfaces, along with StreamInfo capturing MPEG version, channels,
//     sample rate, bit rate. Implements cgo-vs-native routing: decoder.go /
//     encoder.go (build tag dispatch), decoder_cgo.go / encoder_cgo.go (C
//     minimp3 + libmp3lame), native_stub.go / native encoder wiring.
//
//   - libraries/mp3/libminimp3 + libraries/mp3/liblame: the vendored minimp3
//     (decode, CC0/public-domain) and LAME 3.100 (encode, LGPL 2.0+; see
//     liblame/COPYING.LAME) sources, compiled via cgo. LAME reuses Min/Max
//     macros and a per-TU struct across its files, so each .c is compiled as
//     its own cgo translation unit (mp3_cgo_*.c) — following the per-TU split
//     pattern used for libraries/flac.
//
//   - parity discipline (same as FLAC/Opus): mp3_strict build tag gates
//     FMA-free Go. Parity oracles under internal/parity_tests/<area>/ compile
//     their own minimp3/lame copies (no import of libraries/mp3, which would
//     cause duplicate-symbol errors). Scalar C oracle flags: -ffp-contract=off
//     -fno-vectorize -fno-slp-vectorize -fno-unroll-loops via CGO_CFLAGS env.
//     End-to-end oracle for full frame round-trips.
//
// This package operates over the format's natural integer sample type,
// interleaved int16: for stereo, samples are laid out as [L0, R0, L1, R1, …].
//
// When built with cgo, both Decoder and Encoder use the vendored minimp3 /
// LAME reference implementations under libraries/mp3/libminimp3 and
// libraries/mp3/liblame. The
// pure-Go port (a 1:1 translation, implemented in
// libraries/mp3/internal/nativemp3) is always available via
// NewNativeDecoder / NewNativeEncoder regardless of build tags. The
// mp3_strict build tag does not gate the port's existence — it only selects
// the FMA-free build that is bit-exact with the reference; the default build
// is within PSNR noise but may fuse.
//
// Neither Decoder nor Encoder is safe for concurrent use.
package mp3

import (
	"io"
)

// MP3 format limits, as defined by the MPEG-1/2/2.5 Audio Layer III spec
// and the minimp3 / libmp3lame headers.
const (
	// MaxChannels is the maximum number of channels in an MP3 stream
	// (stereo, joint stereo, dual channel, or mono).
	MaxChannels = 2

	// BitsPerSample is the per-sample bit depth of decoded MP3 audio.
	// minimp3 and libmp3lame operate on signed 16-bit PCM.
	BitsPerSample = 16

	// MaxSamplesPerFrame is the largest number of samples per channel a
	// single MP3 frame can carry (1152 for MPEG-1 Layer III; MPEG-2/2.5
	// Layer III carry 576).
	MaxSamplesPerFrame = 1152

	// MinSampleRate is the lowest legal sample rate, in Hz (MPEG-2.5 at
	// 8000 Hz).
	MinSampleRate = 8000

	// MaxSampleRate is the highest legal sample rate, in Hz (MPEG-1 at
	// 48000 Hz).
	MaxSampleRate = 48000

	// MinBitRate is the lowest legal nominal bit rate, in bits per second.
	MinBitRate = 8000

	// MaxBitRate is the highest legal nominal bit rate, in bits per second.
	MaxBitRate = 320000
)

// MPEGVersion identifies the MPEG audio version of a frame.
type MPEGVersion int

const (
	// MPEGVersionUnknown indicates the version has not been resolved yet.
	MPEGVersionUnknown MPEGVersion = iota

	// MPEGVersion1 is MPEG-1 (sample rates 32000/44100/48000 Hz).
	MPEGVersion1

	// MPEGVersion2 is MPEG-2 (sample rates 16000/22050/24000 Hz).
	MPEGVersion2

	// MPEGVersion25 is MPEG-2.5 (sample rates 8000/11025/12000 Hz).
	MPEGVersion25
)

// StreamInfo carries the per-frame format parameters of an MP3 stream.
// Decoders populate it from the first decoded frame header; encoders accept
// the SampleRate / Channels / BitRate fields as configuration.
//
// MP3 frames are self-describing, so a stream may in principle change
// parameters between frames; StreamInfo reflects the most recently decoded
// frame.
type StreamInfo struct {
	// Version is the MPEG audio version of the stream.
	Version MPEGVersion

	// SampleRate is the sample rate in Hz.
	SampleRate int

	// Channels is the channel count, in the range [1, MaxChannels].
	Channels int

	// BitRate is the nominal bit rate in bits per second. Zero if unknown.
	// For VBR streams this reflects the most recently decoded frame.
	BitRate int

	// SamplesPerFrame is the number of samples per channel in a frame
	// (1152 for MPEG-1 Layer III, 576 for MPEG-2/2.5 Layer III).
	SamplesPerFrame int
}

// Decoder decodes an MP3 byte stream into interleaved int16 samples.
//
// For stereo: [L0, R0, L1, R1, …].
type Decoder interface {
	// DecodeFrame decodes the next MP3 frame from the stream into buf and
	// returns the number of samples-per-channel produced (so the populated
	// portion of buf is buf[:n*Channels()]). A return of (0, nil) means a
	// non-audio frame (e.g. an ID3 or skipped frame) was consumed and the
	// caller should call again. Returns io.EOF when the stream is
	// exhausted.
	//
	// buf must have capacity for at least one full MP3 frame — that is,
	// MaxSamplesPerFrame × Channels() int16 values.
	DecodeFrame(buf []int16) (samplesPerChannel int, err error)

	// StreamInfo returns the parsed parameters of the most recently
	// decoded frame. Valid after the first successful DecodeFrame call.
	StreamInfo() StreamInfo

	// SampleRate returns the stream sample rate in Hz, or zero before the
	// first frame is decoded.
	SampleRate() int

	// Channels returns the channel count, or zero before the first frame
	// is decoded.
	Channels() int

	// Close releases native resources. After Close the Decoder must not be
	// used.
	Close() error
}

// DecoderOption configures a [Decoder].
type DecoderOption func(*decoderConfig)

type decoderConfig struct{}

// NewDecoder creates a Decoder reading an MP3 byte stream from r.
//
// When built with cgo (the default), NewDecoder uses the vendored minimp3.
// Use [NewNativeDecoder] to force the pure-Go implementation.
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

// NewNativeDecoder creates a Decoder using the pure-Go MP3 port, regardless
// of whether cgo is available. Useful for benchmarking the Go port against
// minimp3.
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

// Encoder encodes interleaved int16 samples into an MP3 byte stream.
//
// Samples are 16-bit signed PCM. For stereo: [L0, R0, L1, R1, …].
type Encoder interface {
	// EncodeFrame submits a block of interleaved samples (length must be a
	// multiple of [StreamInfo.Channels]) for compression. EncodeFrame may
	// emit zero, one, or many output frames; output is delivered via the
	// [io.Writer] passed to [NewEncoder].
	EncodeFrame(buf []int16) error

	// Close flushes pending frames (including the LAME encoder's final
	// flush) and releases native resources. After Close the Encoder must
	// not be used.
	Close() error
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

// WithQuality sets the LAME encoding quality, in the range [0, 9] where 0
// is highest quality / slowest. Default: 3.
func WithQuality(q int) EncoderOption {
	return func(c *encoderConfig) { c.quality = q }
}

// WithVBR enables variable-bit-rate encoding. When enabled, [WithBitRate]
// is ignored and [WithQuality] selects the VBR quality target.
func WithVBR(enable bool) EncoderOption {
	return func(c *encoderConfig) { c.vbr = enable }
}

// NewEncoder creates an Encoder writing an MP3 byte stream to w. The
// stream's sample rate and channel count come from info; bit rate and
// quality come from the options.
//
// When built with cgo (the default), NewEncoder uses the vendored
// libmp3lame. Use [NewNativeEncoder] to force the pure-Go implementation.
func NewEncoder(w io.Writer, info StreamInfo, opts ...EncoderOption) (Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	if err := validateStreamInfo(info); err != nil {
		return nil, err
	}
	cfg := encoderConfig{bitRate: 128000, quality: 3}
	for _, o := range opts {
		o(&cfg)
	}
	return newEncoder(w, info, cfg)
}

// NewNativeEncoder creates an Encoder using the pure-Go MP3 port,
// regardless of whether cgo is available.
func NewNativeEncoder(w io.Writer, info StreamInfo, opts ...EncoderOption) (Encoder, error) {
	if w == nil {
		return nil, ErrBadArg
	}
	if err := validateStreamInfo(info); err != nil {
		return nil, err
	}
	cfg := encoderConfig{bitRate: 128000, quality: 3}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeEncoder(w, info, cfg)
}

func validateStreamInfo(info StreamInfo) error {
	if info.SampleRate < MinSampleRate || info.SampleRate > MaxSampleRate {
		return ErrBadSampleRate
	}
	if info.Channels < 1 || info.Channels > MaxChannels {
		return ErrBadChannels
	}
	return nil
}
