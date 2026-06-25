package mutations_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// sineAmplitude feeds a sine wave of the given frequency through the
// filter and returns the peak magnitude of the steady-state output.
// Used as a rough frequency-response probe — a Butterworth filter at
// its cutoff should measure around 0.707.
func sineAmplitude(p mutations.Processor, freq float64, sampleRate int, frames int) float64 {
	buf := make([]float64, frames)
	dt := 1.0 / float64(sampleRate)
	for i := range buf {
		buf[i] = math.Sin(2 * math.Pi * freq * float64(i) * dt)
	}
	p.Process(buf)
	// Ignore the first half to skip the startup transient.
	var peak float64
	for _, v := range buf[frames/2:] {
		if math.Abs(v) > peak {
			peak = math.Abs(v)
		}
	}
	return peak
}

func TestLowpassPassesDCAttenuatesHighFreq(t *testing.T) {
	lpf := mutations.NewLowpass(1000, 0.707, consts.SampleRate48000, 1)

	// DC (very low freq) — should pass roughly unchanged.
	lowAmp := sineAmplitude(lpf, 50, consts.SampleRate48000, 8192)
	assert.InDelta(t, 1.0, lowAmp, 0.05, "LPF should pass 50 Hz")

	// Well above cutoff — should attenuate heavily.
	lpf.Reset()
	highAmp := sineAmplitude(lpf, 8000, consts.SampleRate48000, 8192)
	assert.Less(t, highAmp, 0.1, "LPF should attenuate 8 kHz (got %g)", highAmp)
}

func TestLowpassCutoffIsNear707(t *testing.T) {
	// Butterworth (Q=0.707) has -3 dB at the cutoff → amplitude ~0.707.
	lpf := mutations.NewLowpass(2000, 0.707, consts.SampleRate48000, 1)
	amp := sineAmplitude(lpf, 2000, consts.SampleRate48000, 16384)
	assert.InDelta(t, 0.707, amp, 0.05, "LPF at cutoff should measure ≈ 0.707 (got %g)", amp)
}

func TestHighpassBlocksDCPassesHighFreq(t *testing.T) {
	hpf := mutations.NewHighpass(1000, 0.707, consts.SampleRate48000, 1)

	lowAmp := sineAmplitude(hpf, 50, consts.SampleRate48000, 8192)
	assert.Less(t, lowAmp, 0.1, "HPF should attenuate 50 Hz (got %g)", lowAmp)

	hpf.Reset()
	highAmp := sineAmplitude(hpf, 8000, consts.SampleRate48000, 16384)
	// Digital biquad HPFs roll off slightly as frequency approaches
	// Nyquist; 0.1 tolerance captures the "effectively passing"
	// passband without demanding the ideal analogue response.
	assert.InDelta(t, 1.0, highAmp, 0.1, "HPF should pass 8 kHz (got %g)", highAmp)
}

func TestBandpassPeaksAtCutoff(t *testing.T) {
	centre := 2000.0
	bpf := mutations.NewBandpass(centre, 2.0, consts.SampleRate48000, 1)

	atCentre := sineAmplitude(bpf, centre, consts.SampleRate48000, 16384)
	assert.InDelta(t, 1.0, atCentre, 0.1, "BPF should have near-unity gain at centre (got %g)", atCentre)

	bpf.Reset()
	wayBelow := sineAmplitude(bpf, centre/10, consts.SampleRate48000, 16384)
	assert.Less(t, wayBelow, 0.3, "BPF should attenuate below centre (got %g)", wayBelow)

	bpf.Reset()
	wayAbove := sineAmplitude(bpf, centre*5, consts.SampleRate48000, 16384)
	assert.Less(t, wayAbove, 0.3, "BPF should attenuate above centre (got %g)", wayAbove)
}

func TestBiquadStable(t *testing.T) {
	// Pathological: very high Q shouldn't blow up on reasonable input.
	f := mutations.NewLowpass(1000, 20, consts.SampleRate48000, 1)
	buf := make([]float64, consts.SampleRate48000)
	for i := range buf {
		buf[i] = math.Sin(2 * math.Pi * 1000 * float64(i) / consts.SampleRate48000)
	}
	f.Process(buf)
	// Q=20 can legitimately amplify ~Q× at resonance; only check
	// that the filter stays finite and bounded within a sane range.
	for i, v := range buf {
		assert.False(t, math.IsNaN(v), "NaN at %d", i)
		assert.False(t, math.IsInf(v, 0), "Inf at %d", i)
		assert.Less(t, math.Abs(v), 50.0, "runaway magnitude at %d (got %g)", i, v)
	}
}

func TestBiquadReset(t *testing.T) {
	lpf := mutations.NewLowpass(1000, 0.707, consts.SampleRate48000, 1)
	buf := make([]float64, 480)
	buf[0] = 1.0
	lpf.Process(buf)
	require.NotZero(t, buf[20], "filter should have non-zero impulse response")

	lpf.Reset()

	silence := make([]float64, 480)
	lpf.Process(silence)
	for i, v := range silence {
		assert.Equal(t, 0.0, v, "post-reset sample %d should be zero", i)
	}
}

func TestBiquadMultiChannelIndependentState(t *testing.T) {
	// Stereo LPF: an impulse on L alone should not drive R.
	lpf := mutations.NewLowpass(1000, 0.707, consts.SampleRate48000, 2)
	buf := make([]float64, 200) // 100 stereo frames
	buf[0] = 1.0                // L impulse

	lpf.Process(buf)

	// R should remain all-zero since its state was never driven.
	for i := 1; i < len(buf); i += 2 {
		assert.Equal(t, 0.0, buf[i], "R channel sample %d (frame %d)", i, i/2)
	}
	// L should have a decaying impulse response.
	assert.NotEqual(t, 0.0, buf[0], "L channel should carry filtered impulse")
}

func TestBiquadCutoffClamping(t *testing.T) {
	// Cutoff > Nyquist is clamped rather than erroring; filter still
	// functions (approximately as allpass near Nyquist).
	f := mutations.NewLowpass(1e9, 0.707, consts.SampleRate48000, 1)
	buf := []float64{0.5, -0.3, 0.1}
	f.Process(buf)
	for i, v := range buf {
		assert.False(t, math.IsNaN(v), "NaN at %d", i)
	}
}

func TestBiquadChunkedMatchesWhole(t *testing.T) {
	// State should be preserved across Pull boundaries — chunked
	// processing must match single-buffer processing.
	whole := make([]float64, 1000)
	for i := range whole {
		whole[i] = math.Sin(2 * math.Pi * 500 * float64(i) / consts.SampleRate48000)
	}
	chunked := append([]float64(nil), whole...)

	a := mutations.NewLowpass(800, 0.707, consts.SampleRate48000, 1)
	b := mutations.NewLowpass(800, 0.707, consts.SampleRate48000, 1)

	a.Process(whole)

	const chunk = 47
	for i := 0; i < len(chunked); i += chunk {
		end := i + chunk
		if end > len(chunked) {
			end = len(chunked)
		}
		b.Process(chunked[i:end])
	}

	for i := range whole {
		assert.InDelta(t, whole[i], chunked[i], 1e-12, "sample %d", i)
	}
}
