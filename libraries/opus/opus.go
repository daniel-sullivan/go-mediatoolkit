// Package opus implements the Opus audio codec (RFC 6716) in pure Go.
//
// Opus is a lossy audio codec supporting bitrates from 6 to 510 kbit/s,
// designed for speech and music. It combines two codecs:
//   - SILK: optimized for speech (LPC-based)
//   - CELT: optimized for music (MDCT-based)
//
// A Decoder takes Opus packets ([]byte) and produces interleaved float64 PCM.
// An Encoder takes interleaved float64 PCM and produces Opus packets.
//
// All audio data follows the go-mediatoolkit convention of interleaved float64
// samples. For stereo: [L0, R0, L1, R1, ...].
package opus

// Mode identifies the Opus operating mode.
type Mode int

const (
	ModeSILKOnly Mode = iota // SILK codec for speech
	ModeHybrid               // SILK + CELT hybrid
	ModeCELTOnly             // CELT codec for music/general audio
)

// String returns the name of the mode.
func (m Mode) String() string {
	switch m {
	case ModeSILKOnly:
		return "SILK"
	case ModeHybrid:
		return "Hybrid"
	case ModeCELTOnly:
		return "CELT"
	default:
		return "Unknown"
	}
}

// Bandwidth identifies the audio bandwidth.
type Bandwidth int

const (
	BandwidthNarrowband    Bandwidth = iota // 4 kHz
	BandwidthMediumband                     // 6 kHz
	BandwidthWideband                       // 8 kHz
	BandwidthSuperwideband                  // 12 kHz
	BandwidthFullband                       // 20 kHz
)

// String returns the name of the bandwidth.
func (b Bandwidth) String() string {
	switch b {
	case BandwidthNarrowband:
		return "Narrowband"
	case BandwidthMediumband:
		return "Mediumband"
	case BandwidthWideband:
		return "Wideband"
	case BandwidthSuperwideband:
		return "Superwideband"
	case BandwidthFullband:
		return "Fullband"
	default:
		return "Unknown"
	}
}

// Application hints for the encoder.
type Application int

const (
	AppVoIP     Application = iota // Optimized for voice
	AppAudio                       // Optimized for music/general audio
	AppLowDelay                    // Lowest latency, restricted mode
)

// Supported sample rates (Hz).
const (
	Rate8000  = 8000
	Rate12000 = 12000
	Rate16000 = 16000
	Rate24000 = 24000
	Rate48000 = 48000
)

// Frame duration constants at 48 kHz.
const (
	FrameSamples2_5ms = 120  // 2.5 ms
	FrameSamples5ms   = 240  // 5 ms
	FrameSamples10ms  = 480  // 10 ms
	FrameSamples20ms  = 960  // 20 ms
	FrameSamples40ms  = 1920 // 40 ms
	FrameSamples60ms  = 2880 // 60 ms

	// MaxPacketDuration is the maximum packet duration in samples at 48 kHz (120 ms).
	MaxPacketDuration = 5760

	// MaxFrameBytes is the maximum number of bytes per Opus frame.
	MaxFrameBytes = 1275
)

// Decoder decodes Opus packets to interleaved float64 PCM.
type Decoder interface {
	// Decode decodes an Opus packet into pcm. The pcm slice must be large
	// enough to hold the output. Returns the number of samples per channel
	// written. Pass nil data to invoke packet loss concealment (PLC).
	Decode(data []byte, pcm []float64) (samplesPerChannel int, err error)

	// SampleRate returns the output sample rate this decoder was created for.
	SampleRate() int

	// Channels returns the number of output channels (1 or 2).
	Channels() int

	// Reset clears all decoder state for reuse with a new stream.
	Reset()

	// LastPacketDuration returns the duration in samples per channel of the
	// most recently decoded packet.
	LastPacketDuration() int
}

// DecoderOption configures a Decoder.
type DecoderOption func(*decoderConfig)

type decoderConfig struct {
	gain float64
}

// WithGain applies a fixed gain in dB to the decoder output.
func WithGain(dB float64) DecoderOption {
	return func(c *decoderConfig) {
		c.gain = dB
	}
}

// NewDecoder creates a Decoder for the given sample rate and channel count.
// Supported sample rates: 8000, 12000, 16000, 24000, 48000.
// Supported channels: 1 (mono) or 2 (stereo).
func NewDecoder(sampleRate int, channels int, opts ...DecoderOption) (Decoder, error) {
	switch sampleRate {
	case Rate8000, Rate12000, Rate16000, Rate24000, Rate48000:
	default:
		return nil, ErrBadArg
	}
	if channels < 1 || channels > 2 {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return newDecoder(sampleRate, channels, cfg)
}

// MaxFrameSize returns the maximum number of samples per channel for any
// valid Opus packet at the given sample rate. Use this to size the pcm buffer.
func MaxFrameSize(sampleRate int) int {
	// 120 ms maximum duration.
	return sampleRate / 400 * 48
}

// Encoder encodes interleaved float64 PCM to Opus packets.
type Encoder interface {
	// Encode encodes pcm samples into an Opus packet. pcm must contain exactly
	// one frame of interleaved samples. Returns the encoded packet.
	Encode(pcm []float64, maxPacketSize int) ([]byte, error)

	// SampleRate returns the input sample rate.
	SampleRate() int

	// Channels returns the number of input channels.
	Channels() int

	// Reset resets the encoder state.
	Reset()

	// SetBitrate sets the target bitrate in bits per second.
	SetBitrate(bps int) error
}

// EncoderOption configures an Encoder.
type EncoderOption func(*encoderConfig)

type encoderConfig struct {
	bitrate     int
	complexity  int
	application Application
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

// WithApplication sets the application hint.
func WithApplication(app Application) EncoderOption {
	return func(c *encoderConfig) {
		c.application = app
	}
}

// NewEncoder creates an Encoder for the given sample rate and channel count.
// Supported sample rates: 8000, 12000, 16000, 24000, 48000.
// Supported channels: 1 (mono) or 2 (stereo).
//
// When built with Cgo enabled, the encoder uses the C libopus implementation
// (with NEON/SSE intrinsics where available). Otherwise it uses the pure Go
// implementation. Use [NewNativeEncoder] to force the pure Go path.
func NewEncoder(sampleRate int, channels int, opts ...EncoderOption) (Encoder, error) {
	switch sampleRate {
	case Rate8000, Rate12000, Rate16000, Rate24000, Rate48000:
	default:
		return nil, ErrBadArg
	}
	if channels < 1 || channels > 2 {
		return nil, ErrBadArg
	}
	cfg := encoderConfig{
		bitrate:     64000,
		complexity:  10,
		application: AppAudio,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return newEncoder(sampleRate, channels, cfg)
}

// NewNativeEncoder creates an Encoder that always uses the pure Go
// implementation, regardless of whether Cgo is enabled. This is useful
// for benchmarking the Go implementation against the C implementation.
func NewNativeEncoder(sampleRate int, channels int, opts ...EncoderOption) (Encoder, error) {
	switch sampleRate {
	case Rate8000, Rate12000, Rate16000, Rate24000, Rate48000:
	default:
		return nil, ErrBadArg
	}
	if channels < 1 || channels > 2 {
		return nil, ErrBadArg
	}
	cfg := encoderConfig{
		bitrate:     64000,
		complexity:  10,
		application: AppAudio,
	}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeEncoder(sampleRate, channels, cfg)
}

// NewNativeDecoder creates a Decoder that always uses the pure Go
// implementation, regardless of whether Cgo is enabled.
func NewNativeDecoder(sampleRate int, channels int, opts ...DecoderOption) (Decoder, error) {
	switch sampleRate {
	case Rate8000, Rate12000, Rate16000, Rate24000, Rate48000:
	default:
		return nil, ErrBadArg
	}
	if channels < 1 || channels > 2 {
		return nil, ErrBadArg
	}
	cfg := decoderConfig{}
	for _, o := range opts {
		o(&cfg)
	}
	return newNativeDecoder(sampleRate, channels, cfg)
}
