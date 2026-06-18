package generators_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/generators"
)

func TestBeepLengthAndFormat(t *testing.T) {
	b := generators.Beep(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate48000)
	assert.Equal(t, 4800, len(b.Data))
	assert.Equal(t, consts.SampleRate48000, b.SampleRate)
	assert.Equal(t, 1, b.Channels)
}

func TestBeepFadesOut(t *testing.T) {
	b := generators.Beep(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate48000)
	// Final sample must be (near) zero — the fade-out lands at 0.
	assert.InDelta(t, 0, b.Data[len(b.Data)-1], 1e-3)

	// Mid-clip: the tone is still held so peak amplitude should be
	// near 0.4. Scan the first half for the peak.
	var peak float64
	for i := 0; i < len(b.Data)/2; i++ {
		if m := math.Abs(b.Data[i]); m > peak {
			peak = m
		}
	}
	assert.InDelta(t, 0.4, peak, 0.02, "held-tone peak")
}

func TestBeepShortDurationFadesWhole(t *testing.T) {
	// Duration shorter than the 30 ms fade — the whole clip is fade.
	b := generators.Beep(consts.FreqNoteA4, 10*time.Millisecond, consts.SampleRate48000)
	// Last sample is (near) zero either way.
	assert.InDelta(t, 0, b.Data[len(b.Data)-1], 1e-3)
}

func TestPluckDecays(t *testing.T) {
	p := generators.Pluck(consts.FreqNoteA4, 500*time.Millisecond, consts.SampleRate48000)
	require.Equal(t, consts.SampleRate48000/2, len(p.Data))

	// Envelope decays over time: peak in the first quarter must
	// exceed peak in the last quarter.
	quarter := len(p.Data) / 4
	var earlyPeak, latePeak float64
	for _, v := range p.Data[:quarter] {
		if m := math.Abs(v); m > earlyPeak {
			earlyPeak = m
		}
	}
	for _, v := range p.Data[3*quarter:] {
		if m := math.Abs(v); m > latePeak {
			latePeak = m
		}
	}
	assert.Greater(t, earlyPeak, latePeak, "envelope should decay")
	// Last sample has decayed by ~e^-3 ≈ 0.05 × initial amplitude.
	assert.Less(t, latePeak, 0.1)
}

func TestPluckAndBeepBounded(t *testing.T) {
	// Neither generator should produce values outside [-1, 1].
	for _, a := range []struct {
		name string
		data []float64
	}{
		{"Beep", generators.Beep(consts.FreqNoteE2, 200*time.Millisecond, consts.SampleRate48000).Data},
		{"Pluck", generators.Pluck(consts.FreqNoteE6, 200*time.Millisecond, consts.SampleRate48000).Data},
	} {
		for i, v := range a.data {
			assert.False(t, math.IsNaN(v), "%s NaN at %d", a.name, i)
			assert.LessOrEqual(t, math.Abs(v), 1.0, "%s out of range at %d: %g", a.name, i, v)
		}
	}
}
