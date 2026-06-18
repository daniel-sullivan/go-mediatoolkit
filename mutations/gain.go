package mutations

import (
	"math"
	"time"
)

// GainPoint is one point on a declarative gain envelope. At is the
// elapsed time at which Value applies; Value is always a linear
// amplitude multiplier (1.0 = unity). Interpolation between points is
// controlled by the caller via GainCurve; before the first/after the
// last point the bracketing value is held.
type GainPoint struct {
	At    time.Duration
	Value float64
}

// GainCurve selects how EnvelopeValueCurve and ApplyGainEnvelopeCurve
// interpolate between adjacent GainPoints.
type GainCurve int

const (
	// GainCurveLinear interpolates linearly between the two
	// bracketing Value fields. Cheap; gives a "late rush" feel on
	// fades that span a wide dynamic range because perception is
	// logarithmic.
	GainCurveLinear GainCurve = iota

	// GainCurveExponential interpolates linearly in the log domain,
	// so a fade from 0.001 to 1.0 is perceptually uniform rather
	// than concentrating most of the change at the end. Requires
	// both bracketing Value fields to be strictly positive; falls
	// back to linear when either endpoint is ≤ 0 to stay defined
	// for envelopes that touch silence.
	GainCurveExponential
)

// ApplyGain multiplies samples in place by a constant scalar. It
// operates on interleaved or planar buffers identically — gain is
// scale-invariant across channel layout.
func ApplyGain(samples []float64, gain float64) {
	if gain == 1 {
		return
	}
	for i := range samples {
		samples[i] *= gain
	}
}

// Gain is a stateless Processor that scales every sample by a constant
// factor. It exists for the inline-stage case — putting attenuation or
// make-up gain inside a timeline.EffectSource chain so it composes
// with the surrounding filters (e.g. trim before an HPF, boost after a
// reverb's wet/dry blend). For the live "fader" case prefer
// mixer.TrackHandle.SetGain, which is fused into the mix-summing pass
// and atomically settable from any goroutine.
type Gain struct {
	g float64
}

// NewGain returns a Gain processor that multiplies every sample by g.
// g == 1 is allowed and short-circuits in Process; negative values are
// permitted (phase invert).
func NewGain(g float64) *Gain {
	return &Gain{g: g}
}

// Process scales samples in place by the configured gain.
func (p *Gain) Process(samples []float64) {
	ApplyGain(samples, p.g)
}

// Reset is a no-op — Gain holds no per-stream state.
func (p *Gain) Reset() {}

// ApplyGainEnvelope multiplies samples in place using linear
// interpolation between points. Equivalent to
// ApplyGainEnvelopeCurve(..., GainCurveLinear).
func ApplyGainEnvelope(samples []float64, env []GainPoint, elapsed time.Duration, channels, sampleRate int) {
	ApplyGainEnvelopeCurve(samples, env, elapsed, channels, sampleRate, GainCurveLinear)
}

// ApplyGainEnvelopeCurve multiplies samples in place by a time-varying
// gain envelope, interpolating between points with the given curve.
// samples is an interleaved buffer with channels interleave;
// sampleRate converts frame index to elapsed time. elapsed is the
// envelope time at samples[0:channels] — typically the duration
// already consumed from the cue before this buffer. An empty or nil
// env is a no-op.
func ApplyGainEnvelopeCurve(samples []float64, env []GainPoint, elapsed time.Duration, channels, sampleRate int, curve GainCurve) {
	if len(env) == 0 || len(samples) == 0 || channels <= 0 {
		return
	}
	frames := len(samples) / channels
	nsPerFrame := time.Second / time.Duration(sampleRate)
	for f := 0; f < frames; f++ {
		at := elapsed + time.Duration(f)*nsPerFrame
		g := EnvelopeValueCurve(env, at, curve)
		base := f * channels
		for ch := 0; ch < channels; ch++ {
			samples[base+ch] *= g
		}
	}
}

// EnvelopeValue returns the linearly-interpolated value of env at at.
// Equivalent to EnvelopeValueCurve(env, at, GainCurveLinear).
func EnvelopeValue(env []GainPoint, at time.Duration) float64 {
	return EnvelopeValueCurve(env, at, GainCurveLinear)
}

// ApplyCustomGain multiplies samples in place by a user-provided gain
// function evaluated per frame. fn is called with the elapsed time at
// each frame; its return value is the linear gain applied to every
// channel of that frame. Use it as an escape hatch for gain shapes
// that cannot be expressed as a piecewise-linear GainPoint sequence:
// LFO modulation, tremolo, user-drawn curves, etc.
//
// A nil fn is a no-op.
func ApplyCustomGain(samples []float64, fn func(elapsed time.Duration) float64, elapsed time.Duration, channels, sampleRate int) {
	if fn == nil || len(samples) == 0 || channels <= 0 {
		return
	}
	frames := len(samples) / channels
	nsPerFrame := time.Second / time.Duration(sampleRate)
	for f := 0; f < frames; f++ {
		at := elapsed + time.Duration(f)*nsPerFrame
		g := fn(at)
		base := f * channels
		for ch := 0; ch < channels; ch++ {
			samples[base+ch] *= g
		}
	}
}

// EnvelopeValueCurve returns the interpolated value of env at at using
// the selected curve. env must be sorted by At in non-decreasing
// order and must not be empty (callers should validate with
// ValidateGainEnvelope first). Before the first point the first value
// is held; after the last the last value is held.
func EnvelopeValueCurve(env []GainPoint, at time.Duration, curve GainCurve) float64 {
	if at <= env[0].At {
		return env[0].Value
	}
	last := len(env) - 1
	if at >= env[last].At {
		return env[last].Value
	}
	lo, hi := 0, last
	for lo+1 < hi {
		mid := (lo + hi) / 2
		if env[mid].At <= at {
			lo = mid
		} else {
			hi = mid
		}
	}
	a, b := env[lo], env[hi]
	span := b.At - a.At
	if span == 0 {
		return b.Value
	}
	frac := float64(at-a.At) / float64(span)
	if curve == GainCurveExponential && a.Value > 0 && b.Value > 0 {
		return a.Value * math.Pow(b.Value/a.Value, frac)
	}
	return a.Value + frac*(b.Value-a.Value)
}

// ValidateGainEnvelope returns ErrUnsortedEnvelope if env is not
// sorted by At in non-decreasing order. An empty envelope is valid.
func ValidateGainEnvelope(env []GainPoint) error {
	for i := 1; i < len(env); i++ {
		if env[i].At < env[i-1].At {
			return ErrUnsortedEnvelope
		}
	}
	return nil
}

// FadeInEnvelope returns a gain envelope that rises linearly from 0
// to 1 over [0, duration] and holds at 1 thereafter. A non-positive
// duration returns nil (unity passthrough).
func FadeInEnvelope(duration time.Duration) []GainPoint {
	if duration <= 0 {
		return nil
	}
	return []GainPoint{
		{At: 0, Value: 0},
		{At: duration, Value: 1},
	}
}

// FadeOutEnvelope returns a gain envelope that holds unity until at,
// then ramps linearly from 1 to 0 over [at, at+duration]. A
// non-positive duration returns nil.
func FadeOutEnvelope(at, duration time.Duration) []GainPoint {
	if duration <= 0 {
		return nil
	}
	if at <= 0 {
		return []GainPoint{
			{At: 0, Value: 1},
			{At: duration, Value: 0},
		}
	}
	return []GainPoint{
		{At: 0, Value: 1},
		{At: at, Value: 1},
		{At: at + duration, Value: 0},
	}
}

// defaultSilenceFloor is the linear amplitude used as "silence" by
// exponential fade helpers. -60 dB is below typical noise floors and
// imperceptible for music playback; callers needing a quieter floor
// can build their own envelope.
const defaultSilenceFloor = 1e-3 // -60 dB

// FadeInEnvelopeExp returns a gain envelope going from defaultSilenceFloor
// (≈ -60 dB) to 1.0 over [0, duration]. Intended to be applied with
// GainCurveExponential so the result is a perceptually uniform fade
// in. A non-positive duration returns nil.
func FadeInEnvelopeExp(duration time.Duration) []GainPoint {
	if duration <= 0 {
		return nil
	}
	return []GainPoint{
		{At: 0, Value: defaultSilenceFloor},
		{At: duration, Value: 1},
	}
}

// FadeOutEnvelopeExp returns a gain envelope that holds unity until at,
// then ramps from 1 to defaultSilenceFloor (≈ -60 dB) over
// [at, at+duration]. Intended to be applied with GainCurveExponential.
// A non-positive duration returns nil.
func FadeOutEnvelopeExp(at, duration time.Duration) []GainPoint {
	if duration <= 0 {
		return nil
	}
	if at <= 0 {
		return []GainPoint{
			{At: 0, Value: 1},
			{At: duration, Value: defaultSilenceFloor},
		}
	}
	return []GainPoint{
		{At: 0, Value: 1},
		{At: at, Value: 1},
		{At: at + duration, Value: defaultSilenceFloor},
	}
}
