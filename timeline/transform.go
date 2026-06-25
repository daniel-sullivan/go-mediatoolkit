package timeline

import (
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Transform declaratively shapes samples produced by a Cue. The zero
// Transform is a pass-through; all fields are independently optional.
//
// Gain is a gain envelope indexed by elapsed time within the cue. An
// empty or nil envelope is unity gain. Before the first point the
// first value is held; after the last point the last value is held —
// this lets callers describe a fade-in with two points ({0, 0} and
// {200ms, 1.0}) or a constant gain with a single point.
//
// GainCurve selects how samples between envelope points are
// interpolated. The zero value (mutations.GainCurveLinear) matches
// the previous behaviour; mutations.GainCurveExponential produces
// perceptually uniform fades across wide dynamic ranges (requires
// all envelope values to be strictly positive).
//
// GainFunc is an escape hatch for gain shapes that cannot be
// expressed as piecewise-linear points: LFO modulation, tremolo,
// user-drawn curves, etc. When non-nil it is evaluated per frame
// with the elapsed time since the cue start, and the returned
// multiplier is applied after the Gain envelope — so the two stack.
// For stateful or multi-sample transforms (filters, reverb) use a
// mutations.Processor chained through an EffectSource instead.
//
// The envelope implementation lives in the mutations package; the
// EnvelopePoint type is an alias of mutations.GainPoint so the same
// envelopes can be applied to bare []float64 buffers (see
// mutations.ApplyGainEnvelope / ApplyGainEnvelopeCurve /
// ApplyCustomGain).
type Transform struct {
	Gain      []EnvelopePoint
	GainCurve mutations.GainCurve
	GainFunc  func(elapsed time.Duration) float64
}

// EnvelopePoint is one point on a declarative envelope. Alias of
// mutations.GainPoint so the two packages' types are interchangeable.
type EnvelopePoint = mutations.GainPoint

// apply scales samples in place for the segment beginning at
// cueElapsed into the cue. channels is the interleaved channel count.
// sampleRate is the sample rate at which samples were recorded (the
// owning timeline's rate). Delegates to mutations.ApplyGainEnvelope.
func (tr Transform) apply(samples []float64, cueElapsed time.Duration, channels, sampleRate int) {
	mutations.ApplyGainEnvelopeCurve(samples, tr.Gain, cueElapsed, channels, sampleRate, tr.GainCurve)
	mutations.ApplyCustomGain(samples, tr.GainFunc, cueElapsed, channels, sampleRate)
}
