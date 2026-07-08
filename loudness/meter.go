package loudness

import (
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Meter measures the loudness of a stream of interleaved audio frames per
// EBU R128 / ITU-R BS.1770-4. It is a thin, idiomatic wrapper over the
// bit-exact libebur128 v1.2.6 port in loudness/internal/r128: feed it
// frames with AddFrames, then read momentary/short-term/integrated LUFS,
// loudness range, relative threshold, and sample/true peak.
//
// # Which readers are available
//
// A Meter only computes what its Mode selects. Calling a reader whose
// mode bit is absent returns ErrInvalidMode:
//
//   - Momentary is always available (every mode implies ModeMomentary).
//   - ShortTerm / LoudnessWindow(>400 ms) need ModeShortTerm.
//   - Integrated and RelativeThreshold need ModeIntegrated.
//   - Range needs ModeLRA.
//   - SamplePeak / PrevSamplePeak need ModeSamplePeak.
//   - TruePeak / PrevTruePeak need ModeTruePeak.
//
// Before enough audio has been processed for a measurement, the loudness
// readers return (-Inf, nil) — that is a valid "no measurable loudness
// yet" signal, not an error (Range returns 0, RelativeThreshold returns
// -70 in that state, matching libebur128).
//
// # Units
//
// Loudness readers return LUFS (Momentary/ShortTerm/Integrated) or LU
// (Range, a relative spread). Peak readers return LINEAR amplitude (1.0 ==
// full scale); convert to dBFS/dBTP with mutations.AmplitudeToDecibels or
// 20*log10.
//
// # History and long-running streams
//
// By default the integrated-loudness and LRA history is UNBOUNDED
// (libebur128's ULONG_MAX default), which grows without limit on a 24/7
// stream. For continuous metering either cap the retained history with
// SetMaxHistory or construct the meter with ModeHistogram (bounded,
// fixed-size histogram accounting) — both are documented on those methods
// and on ModeHistogram.
//
// # Concurrency
//
// A Meter is NOT safe for concurrent use. Drive one Meter from a single
// goroutine; use the Monitor type (added in a later phase) for a
// mutex-guarded meter on a live bus.
type Meter struct {
	state *r128.State
	mode  Mode
}

// NewMeter constructs a Meter for sampleRate Hz, channels interleaved
// channels, and the measurements selected by mode.
//
// It returns:
//   - ErrBadSampleRate if sampleRate < 16 (libebur128's own lower bound;
//     below it the 100 ms block would be too small to size the ring
//     buffer),
//   - ErrBadChannels if channels is not in 1..64,
//   - ErrBadConfig if mode selects no valid measurement window (mode must
//     carry at least ModeMomentary; e.g. ModeHistogram alone is invalid).
//
// The default channel map follows libebur128: mono/stereo map to
// left/right, 5 channels to L/R/C/Ls/Rs, and so on (channel index 3 is
// UNUSED for 6+ channel layouts unless overridden). Use SetChannel to
// override individual channel weightings (e.g. ChannelDualMono for a
// mono-in-stereo programme).
func NewMeter(sampleRate, channels int, mode Mode) (*Meter, error) {
	if sampleRate < minSampleRate {
		return nil, ErrBadSampleRate
	}
	if channels < 1 || channels > maxChannels {
		return nil, ErrBadChannels
	}
	// Mode must carry at least the momentary bit — mirrors ebur128_init,
	// which fails when neither the M nor S window can be sized (e.g. a
	// bare ModeHistogram, or a raw bit pattern without ModeMomentary).
	if mode&ModeMomentary != ModeMomentary {
		return nil, ErrBadConfig
	}
	return &Meter{
		state: r128.NewState(channels, sampleRate, int(mode)),
		mode:  mode,
	}, nil
}

// SampleRate reports the sample rate the meter was constructed with.
func (m *Meter) SampleRate() int { return m.state.SampleRate() }

// Channels reports the interleaved channel count the meter expects.
func (m *Meter) Channels() int { return m.state.Channels() }

// Mode reports the measurement mode bitmask.
func (m *Meter) Mode() Mode { return m.mode }

// AddFrames processes a block of interleaved audio. samples must contain a
// whole number of frames — its length must be a multiple of Channels() —
// otherwise it returns ErrUnalignedSamples. An empty slice is a no-op.
//
// Frames may be delivered in any chunking; the meter carries all filter
// and gating state across calls, so the result is identical to feeding the
// whole stream at once.
func (m *Meter) AddFrames(samples []float64) error {
	if len(samples)%m.state.Channels() != 0 {
		return ErrUnalignedSamples
	}
	m.state.AddFrames(samples)
	return nil
}

// SetChannel overrides the weighting role of channel i (0-based). Returns
// ErrBadChannelIndex if i is out of range, or if ch is ChannelDualMono on
// anything other than channel 0 of a mono meter (dual-mono only makes
// sense for a single-channel programme counted twice).
func (m *Meter) SetChannel(i int, ch Channel) error {
	if code := m.state.SetChannel(i, int(ch)); code != r128.Success {
		return mapErr(code)
	}
	return nil
}

// SetMaxWindow sets the maximum trailing window LoudnessWindow may query.
// d must be positive (else ErrBadWindow); it is clamped up to the mode
// minimum (3 s with ModeShortTerm, 400 ms with ModeMomentary). Setting the
// same window again is a no-op. Changing it clears the current audio
// buffer (any in-progress block is lost).
//
// NOTE: because this ports libebur128 v1.2.6 faithfully, the internal
// buffer is sized proportional to sampleRate*window (a known v1.2.6 sizing
// quirk that over-allocates ~1000x relative to the millisecond window).
// Keep d modest — a few seconds at most — to avoid very large allocations.
func (m *Meter) SetMaxWindow(d time.Duration) error {
	if d <= 0 {
		return ErrBadWindow
	}
	code := m.state.SetMaxWindow(uint64(d.Milliseconds()))
	if code == r128.ErrorNoChange {
		return nil
	}
	if code != r128.Success {
		return mapErr(code)
	}
	return nil
}

// SetMaxHistory bounds how much loudness history the integrated-loudness
// and LRA measurements retain, trimming the oldest blocks immediately.
// d must be positive (else ErrBadWindow); it is clamped up to the mode
// minimum (3 s with ModeLRA, 400 ms with ModeMomentary). Setting the same
// history again is a no-op.
//
// The default history is effectively unbounded — set a finite history (or
// use ModeHistogram) for 24/7 streams so memory does not grow without
// limit.
func (m *Meter) SetMaxHistory(d time.Duration) error {
	if d <= 0 {
		return ErrBadWindow
	}
	code := m.state.SetMaxHistory(uint64(d.Milliseconds()))
	if code == r128.ErrorNoChange {
		return nil
	}
	if code != r128.Success {
		return mapErr(code)
	}
	return nil
}

// Momentary returns the momentary loudness (trailing 400 ms) in LUFS, or
// (-Inf, nil) before enough audio has been processed / for silence.
func (m *Meter) Momentary() (float64, error) {
	v, code := m.state.LoudnessMomentary()
	return v, mapErr(code)
}

// ShortTerm returns the short-term loudness (trailing 3 s) in LUFS. Needs
// ModeShortTerm (else ErrInvalidMode); returns (-Inf, nil) for silence or
// insufficient data.
func (m *Meter) ShortTerm() (float64, error) {
	v, code := m.state.LoudnessShortterm()
	return v, mapErr(code)
}

// Integrated returns the gated integrated loudness over everything
// processed so far, in LUFS. Needs ModeIntegrated (else ErrInvalidMode);
// returns (-Inf, nil) when no block passes the absolute/relative gates
// (silence or too little audio).
func (m *Meter) Integrated() (float64, error) {
	v, code := m.state.GatedLoudness()
	return v, mapErr(code)
}

// Range returns the loudness range (EBU Tech 3342) in LU. Needs ModeLRA
// (else ErrInvalidMode); returns (0, nil) before enough short-term blocks
// exist.
func (m *Meter) Range() (float64, error) {
	v, code := m.state.LoudnessRange()
	return v, mapErr(code)
}

// RelativeThreshold returns the relative gating threshold in LUFS. Needs
// ModeIntegrated (else ErrInvalidMode); returns (-70, nil) before any
// block passes the absolute gate.
func (m *Meter) RelativeThreshold() (float64, error) {
	v, code := m.state.RelativeThreshold()
	return v, mapErr(code)
}

// LoudnessWindow returns the loudness over an arbitrary trailing window in
// LUFS. d must be positive (else ErrBadWindow) and no larger than the
// configured max window (SetMaxWindow, or the mode default) else
// ErrInvalidMode. Returns (-Inf, nil) for silence / insufficient data.
func (m *Meter) LoudnessWindow(d time.Duration) (float64, error) {
	if d <= 0 {
		return 0, ErrBadWindow
	}
	v, code := m.state.LoudnessWindow(uint64(d.Milliseconds()))
	return v, mapErr(code)
}

// SamplePeak returns the maximum linear sample peak seen on channel ch
// across all processed frames (1.0 == full scale). Needs ModeSamplePeak
// (else ErrInvalidMode); ErrBadChannelIndex if ch is out of range.
func (m *Meter) SamplePeak(ch int) (float64, error) {
	v, code := m.state.SamplePeak(ch)
	return v, mapErr(code)
}

// PrevSamplePeak returns the maximum linear sample peak on channel ch from
// the most recent AddFrames call only. Same mode/index errors as
// SamplePeak.
func (m *Meter) PrevSamplePeak(ch int) (float64, error) {
	v, code := m.state.PrevSamplePeak(ch)
	return v, mapErr(code)
}

// TruePeak returns the maximum linear true (inter-sample) peak seen on
// channel ch across all processed frames (1.0 == full scale; convert to
// dBTP with 20*log10). Needs ModeTruePeak (else ErrInvalidMode);
// ErrBadChannelIndex if ch is out of range. Always >= the sample peak.
func (m *Meter) TruePeak(ch int) (float64, error) {
	v, code := m.state.TruePeak(ch)
	return v, mapErr(code)
}

// PrevTruePeak returns the maximum linear true peak on channel ch from the
// most recent AddFrames call only. Same mode/index errors as TruePeak.
func (m *Meter) PrevTruePeak(ch int) (float64, error) {
	v, code := m.state.PrevTruePeak(ch)
	return v, mapErr(code)
}

// Reset returns the meter to a fresh-stream state, discarding all measured
// loudness/peak history while KEEPING the configuration (sample rate,
// channel count, mode, channel-map overrides, and any SetMaxWindow /
// SetMaxHistory settings). Use it to reuse one Meter across independent
// clips without reallocating.
//
// libebur128 itself has no reset entry point; this reproduces exactly the
// state a fresh meter of the same configuration would have.
func (m *Meter) Reset() { m.state.Reset() }

// mapErr translates an r128 errcode into the package's exported sentinel
// errors. Non-finite loudness values (-Inf) are NOT errors — they flow
// through with a nil error.
func mapErr(code int) error {
	switch code {
	case r128.Success:
		return nil
	case r128.ErrorInvalidMode:
		return ErrInvalidMode
	case r128.ErrorInvalidChannelIndex:
		return ErrBadChannelIndex
	default:
		// r128.ErrorNoMem / ErrorNoChange never reach a public reader;
		// surface anything unexpected rather than swallowing it.
		return ErrBadConfig
	}
}
