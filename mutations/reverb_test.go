package mutations_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestReverbImpulseDecaysOverTime(t *testing.T) {
	r := mutations.NewReverb(consts.SampleRate48000, 1, 0.7, 0.4, 1.0)

	// Fire an impulse and capture a long tail.
	buf := make([]float64, consts.SampleRate48000)
	buf[0] = 1.0
	r.Process(buf)

	// Early energy (first 500ms) should be non-trivial.
	var earlyEnergy, lateEnergy float64
	for i := 0; i < consts.SampleRate24000; i++ {
		earlyEnergy += buf[i] * buf[i]
	}
	for i := consts.SampleRate24000; i < consts.SampleRate48000; i++ {
		lateEnergy += buf[i] * buf[i]
	}
	assert.Greater(t, earlyEnergy, 0.0, "reverb should have early-tail energy")
	assert.Less(t, lateEnergy, earlyEnergy, "tail should decay over time")
}

func TestReverbStaysBounded(t *testing.T) {
	// Full roomSize = large feedback; should still be stable.
	r := mutations.NewReverb(consts.SampleRate48000, 1, 1.0, 0.2, 1.0)
	buf := make([]float64, consts.SampleRate48000)
	buf[0] = 1.0
	r.Process(buf)

	for i, v := range buf {
		assert.False(t, math.IsNaN(v), "NaN at %d", i)
		assert.Less(t, math.Abs(v), 5.0, "magnitude bound at %d (got %g)", i, v)
	}
}

func TestReverbDryMix(t *testing.T) {
	// Wet 0 should return the dry signal unchanged.
	r := mutations.NewReverb(consts.SampleRate48000, 1, 0.7, 0.4, 0.0)
	buf := []float64{1, -0.5, 0.25, 0.1}
	orig := append([]float64(nil), buf...)
	r.Process(buf)
	for i := range buf {
		assert.InDelta(t, orig[i], buf[i], 1e-9, "sample %d", i)
	}
}

func TestReverbMultiChannel(t *testing.T) {
	r := mutations.NewReverb(consts.SampleRate48000, 2, 0.5, 0.3, 0.5)
	buf := make([]float64, 2*4800)
	buf[0] = 1.0 // L impulse
	r.Process(buf)

	// At least some right-channel output should be zero for the
	// first few frames (reverb is applied per-channel).
	var leftEnergy, rightEnergy float64
	for i := 0; i < len(buf); i += 2 {
		leftEnergy += buf[i] * buf[i]
		rightEnergy += buf[i+1] * buf[i+1]
	}
	assert.Greater(t, leftEnergy, 0.0, "L channel should have reverb energy")
	assert.Equal(t, 0.0, rightEnergy, "R channel should remain silent (independent state)")
}

func TestReverbReset(t *testing.T) {
	r := mutations.NewReverb(consts.SampleRate48000, 1, 0.7, 0.4, 1.0)
	buf := make([]float64, consts.SampleRate48000)
	buf[0] = 1.0
	r.Process(buf)

	r.Reset()

	// After reset, processing silence should return silence.
	silence := make([]float64, 4800)
	r.Process(silence)
	for i, v := range silence {
		assert.Equal(t, 0.0, v, "post-reset sample %d should be zero", i)
	}
}
