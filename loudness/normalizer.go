package loudness

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Normalizer is a streaming mutations.Processor that applies one
// constant, precomputed gain to reach a target loudness from an
// already-known measured loudness (typically the Integrated field of
// a prior Measure call, or a Meter's Integrated reading). Unlike
// Normalize, it does not measure or clamp to a true-peak ceiling
// itself — it is the fixed-gain building block for cases where the
// loudness is measured once (e.g. offline, or from a prior pass) and
// then applied continuously to a live or chunked stream. Combine it
// with a Limiter (or use Normalize's NormalizeLimit mode instead) if
// the gained signal also needs a peak ceiling enforced.
//
// A Normalizer holds no per-stream state beyond its constant gain, so
// Reset is a no-op and it is safe to share a single instance's Process
// method across multiple independent streams (though, like any
// mutations.Processor, not concurrently from more than one goroutine
// at a time — see mutations.Processor).
type Normalizer struct {
	gainDB float64
	gain   float64 // linear, precomputed from gainDB
}

// NewNormalizer constructs a Normalizer that applies the constant gain
// targetLUFS - measuredLUFS. It returns:
//
//   - ErrSilentInput if measuredLUFS is -Inf (no measurable loudness —
//     there is no finite gain that normalises silence to a target).
//   - ErrBadConfig if measuredLUFS is NaN or +Inf.
//   - ErrBadTarget if targetLUFS is not strictly negative.
func NewNormalizer(measuredLUFS, targetLUFS float64) (*Normalizer, error) {
	if math.IsInf(measuredLUFS, -1) {
		return nil, ErrSilentInput
	}
	if math.IsNaN(measuredLUFS) || math.IsInf(measuredLUFS, 1) {
		return nil, ErrBadConfig
	}
	if !(targetLUFS < 0) {
		return nil, ErrBadTarget
	}

	gainDB := targetLUFS - measuredLUFS
	return &Normalizer{
		gainDB: gainDB,
		gain:   mutations.Decibels(gainDB),
	}, nil
}

// Process applies the constant gain to samples in place. samples may
// be any length or chunking — the gain is stateless, so the result is
// identical regardless of how the stream is split across calls.
func (n *Normalizer) Process(samples []float64) {
	mutations.ApplyGain(samples, n.gain)
}

// Reset is a no-op: a Normalizer carries no per-stream state, only its
// constant configured gain.
func (n *Normalizer) Reset() {}

// GainDB returns the constant gain, in dB, this Normalizer applies.
func (n *Normalizer) GainDB() float64 { return n.gainDB }

var _ mutations.Processor = (*Normalizer)(nil)
