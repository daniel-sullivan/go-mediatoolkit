package loudness

import "errors"

// Sentinel errors returned by the loudness package. All messages are
// prefixed "loudness: " per repo convention.
var (
	// ErrBadSampleRate is returned when a sample rate is below 16 Hz —
	// libebur128's own lower bound (VALIDATE_MAX_SAMPLERATE's floor): at
	// rates 1..15 the meter's 100 ms block would be zero or too small to
	// size the ring buffer, so the C oracle rejects them and so do we.
	ErrBadSampleRate = errors.New("loudness: sample rate must be at least 16 Hz")

	// ErrBadChannels is returned when a channel count is not positive,
	// or exceeds the libebur128 VALIDATE_MAX_CHANNELS limit (64).
	ErrBadChannels = errors.New("loudness: channel count must be positive and at most 64")

	// ErrBadChannelIndex is returned when a channel index passed to a
	// per-channel accessor or SetChannel is out of range for the
	// configured channel count.
	ErrBadChannelIndex = errors.New("loudness: channel index out of range")

	// ErrInvalidMode is returned when a reader is called for a
	// measurement that the Meter was not configured (via its Mode) to
	// compute — e.g. asking for Range without ModeLRA.
	ErrInvalidMode = errors.New("loudness: mode not enabled for this measurement")

	// ErrUnalignedSamples is returned when an interleaved sample slice
	// passed to AddFrames is not an exact multiple of the channel
	// count.
	ErrUnalignedSamples = errors.New("loudness: sample count is not a multiple of channel count")

	// ErrBadWindow is returned when a requested measurement window
	// (SetMaxWindow, LoudnessWindow) is non-positive or otherwise
	// unusable.
	ErrBadWindow = errors.New("loudness: window duration must be positive")

	// ErrBadTarget is returned when a normalisation target LUFS value
	// is not strictly negative (0 LUFS is full-scale-sine loud; no
	// real target normalises upward past it).
	ErrBadTarget = errors.New("loudness: target loudness must be negative")

	// ErrSilentInput is returned when Measure/Normalize is given audio
	// whose integrated loudness is -Inf (silence, or too short/quiet
	// for any gated block to pass the absolute gate) — there is no
	// finite gain that normalises silence to a target loudness.
	ErrSilentInput = errors.New("loudness: input has no measurable loudness (silent or too short)")

	// ErrBadConfig is returned when a products constructor (Normalizer,
	// Limiter, Leveller, Monitor) receives a configuration that fails
	// validation beyond the specific errors above.
	ErrBadConfig = errors.New("loudness: invalid configuration")
)
