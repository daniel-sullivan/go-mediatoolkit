package mutations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestApplyGainConstant(t *testing.T) {
	samples := []float64{1, -1, 0.5, -0.5}
	mutations.ApplyGain(samples, 0.5)
	assert.Equal(t, []float64{0.5, -0.5, 0.25, -0.25}, samples)
}

func TestApplyGainUnityNoOp(t *testing.T) {
	samples := []float64{1, -1, 0.5}
	orig := append([]float64(nil), samples...)
	mutations.ApplyGain(samples, 1.0)
	assert.Equal(t, orig, samples)
}

func TestApplyGainZero(t *testing.T) {
	samples := []float64{1, -1, 0.5}
	mutations.ApplyGain(samples, 0)
	assert.Equal(t, []float64{0, 0, 0}, samples)
}

func TestGainProcessorScales(t *testing.T) {
	g := mutations.NewGain(0.25)
	samples := []float64{1, -1, 0.4, -0.4}
	g.Process(samples)
	assert.Equal(t, []float64{0.25, -0.25, 0.1, -0.1}, samples)
}

func TestGainProcessorUnityNoOp(t *testing.T) {
	g := mutations.NewGain(1.0)
	samples := []float64{1, -1, 0.5}
	orig := append([]float64(nil), samples...)
	g.Process(samples)
	assert.Equal(t, orig, samples)
}

func TestGainProcessorResetIsNoOp(t *testing.T) {
	g := mutations.NewGain(0.5)
	samples := []float64{1, 1}
	g.Process(samples)
	g.Reset()
	g.Process([]float64{}) // does not panic on empty
	assert.Equal(t, []float64{0.5, 0.5}, samples)
}

func TestGainProcessorPhaseInvert(t *testing.T) {
	g := mutations.NewGain(-1)
	samples := []float64{0.3, -0.7}
	g.Process(samples)
	assert.Equal(t, []float64{-0.3, 0.7}, samples)
}

func TestEnvelopeValueInterpolation(t *testing.T) {
	env := []mutations.GainPoint{
		{At: 0, Value: 0},
		{At: 100 * time.Millisecond, Value: 1.0},
		{At: 200 * time.Millisecond, Value: 0.5},
	}
	cases := []struct {
		name string
		at   time.Duration
		want float64
	}{
		{"before first holds first", -10 * time.Millisecond, 0},
		{"at first point", 0, 0},
		{"halfway up ramp", 50 * time.Millisecond, 0.5},
		{"at peak", 100 * time.Millisecond, 1.0},
		{"halfway down ramp", 150 * time.Millisecond, 0.75},
		{"at last point", 200 * time.Millisecond, 0.5},
		{"after last holds last", 500 * time.Millisecond, 0.5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.InDelta(t, tc.want, mutations.EnvelopeValue(env, tc.at), 1e-9)
		})
	}
}

func TestEnvelopeValueSinglePoint(t *testing.T) {
	env := []mutations.GainPoint{{At: 50 * time.Millisecond, Value: 0.7}}
	assert.Equal(t, 0.7, mutations.EnvelopeValue(env, 0))
	assert.Equal(t, 0.7, mutations.EnvelopeValue(env, 50*time.Millisecond))
	assert.Equal(t, 0.7, mutations.EnvelopeValue(env, time.Second))
}

func TestValidateGainEnvelope(t *testing.T) {
	assert.NoError(t, mutations.ValidateGainEnvelope(nil))
	assert.NoError(t, mutations.ValidateGainEnvelope([]mutations.GainPoint{{At: 0, Value: 1}}))
	assert.NoError(t, mutations.ValidateGainEnvelope([]mutations.GainPoint{
		{At: 0, Value: 1}, {At: 100, Value: 0}, {At: 100, Value: 0.5},
	}))
	assert.ErrorIs(t, mutations.ValidateGainEnvelope([]mutations.GainPoint{
		{At: 100 * time.Millisecond, Value: 1}, {At: 0, Value: 0},
	}), mutations.ErrUnsortedEnvelope)
}

func TestApplyGainEnvelopeEmptyNoOp(t *testing.T) {
	samples := []float64{1, -1, 0.5, -0.5}
	orig := append([]float64(nil), samples...)
	mutations.ApplyGainEnvelope(samples, nil, 0, 2, consts.SampleRate48000)
	assert.Equal(t, orig, samples)
}

func TestApplyGainEnvelopeConstant(t *testing.T) {
	tr := []mutations.GainPoint{{At: 0, Value: 0.5}}
	samples := []float64{1, -1, 0.4, -0.4}
	mutations.ApplyGainEnvelope(samples, tr, 0, 2, consts.SampleRate48000)
	assert.Equal(t, []float64{0.5, -0.5, 0.2, -0.2}, samples)
}

func TestApplyGainEnvelopeFadeIn(t *testing.T) {
	env := []mutations.GainPoint{
		{At: 0, Value: 0},
		{At: 10 * time.Millisecond, Value: 1},
	}
	samples := []float64{1, 1, 1, 1}
	mutations.ApplyGainEnvelope(samples, env, 0, 1, consts.SampleRate48000)
	require.Len(t, samples, 4)
	for i, v := range samples {
		// Each sample is at t = i / consts.SampleRate48000 seconds. gain = t / 10ms.
		// Tolerance loose due to ns quantisation at 48 kHz.
		assert.InDelta(t, float64(i)/480.0, v, 1e-6, "sample %d", i)
	}
}

func TestFadeInEnvelope(t *testing.T) {
	assert.Nil(t, mutations.FadeInEnvelope(0))
	env := mutations.FadeInEnvelope(100 * time.Millisecond)
	require.Len(t, env, 2)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(env, 0), 1e-9)
	assert.InDelta(t, 0.5, mutations.EnvelopeValue(env, 50*time.Millisecond), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(env, 100*time.Millisecond), 1e-9)
}

func TestFadeOutEnvelopeHoldsThenFades(t *testing.T) {
	env := mutations.FadeOutEnvelope(200*time.Millisecond, 100*time.Millisecond)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(env, 0), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(env, 200*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.5, mutations.EnvelopeValue(env, 250*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(env, 300*time.Millisecond), 1e-9)
}

func TestFadeOutEnvelopeAtZero(t *testing.T) {
	env := mutations.FadeOutEnvelope(0, 100*time.Millisecond)
	require.Len(t, env, 2)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(env, 0), 1e-9)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(env, 100*time.Millisecond), 1e-9)
}

func TestEnvelopeValueCurveExponential(t *testing.T) {
	// Exponential interpolation is linear in log space; halfway
	// through 0.01 → 1.0 the value is sqrt(0.01 * 1) = 0.1.
	env := []mutations.GainPoint{
		{At: 0, Value: 0.01},
		{At: 100 * time.Millisecond, Value: 1.0},
	}
	assert.InDelta(t, 0.01, mutations.EnvelopeValueCurve(env, 0, mutations.GainCurveExponential), 1e-9)
	assert.InDelta(t, 0.1, mutations.EnvelopeValueCurve(env, 50*time.Millisecond, mutations.GainCurveExponential), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValueCurve(env, 100*time.Millisecond, mutations.GainCurveExponential), 1e-9)
}

func TestEnvelopeValueCurveExponentialFallsBackLinearOnZero(t *testing.T) {
	// If any endpoint is 0 the log is undefined; the curve must fall
	// back to linear to stay finite.
	env := []mutations.GainPoint{
		{At: 0, Value: 0},
		{At: 100 * time.Millisecond, Value: 1.0},
	}
	got := mutations.EnvelopeValueCurve(env, 50*time.Millisecond, mutations.GainCurveExponential)
	assert.InDelta(t, 0.5, got, 1e-9, "expected linear fallback")
}

func TestApplyGainEnvelopeCurveLinearDefault(t *testing.T) {
	// Linear curve produces the same output as the curve-less API.
	env := []mutations.GainPoint{{At: 0, Value: 0.25}}
	a := []float64{1, 1, 1}
	b := []float64{1, 1, 1}
	mutations.ApplyGainEnvelope(a, env, 0, 1, consts.SampleRate48000)
	mutations.ApplyGainEnvelopeCurve(b, env, 0, 1, consts.SampleRate48000, mutations.GainCurveLinear)
	assert.Equal(t, a, b)
}

func TestFadeInEnvelopeExp(t *testing.T) {
	env := mutations.FadeInEnvelopeExp(100 * time.Millisecond)
	require.Len(t, env, 2)
	assert.InDelta(t, 1e-3, env[0].Value, 1e-12, "floor at -60 dB")
	assert.Equal(t, 1.0, env[1].Value)
	assert.Nil(t, mutations.FadeInEnvelopeExp(0))
}

func TestFadeOutEnvelopeExp(t *testing.T) {
	env := mutations.FadeOutEnvelopeExp(200*time.Millisecond, 100*time.Millisecond)
	require.Len(t, env, 3)
	assert.Equal(t, 1.0, env[0].Value)
	assert.Equal(t, 1.0, env[1].Value)
	assert.InDelta(t, 1e-3, env[2].Value, 1e-12)
}

func TestDecibelsRoundTrip(t *testing.T) {
	for _, db := range []float64{0, -6, -12, -20, -60, 6} {
		amp := mutations.Decibels(db)
		got := mutations.AmplitudeToDecibels(amp)
		assert.InDelta(t, db, got, 1e-9, "db=%g", db)
	}
}

func TestApplyCustomGainNilNoOp(t *testing.T) {
	samples := []float64{1, -1, 0.5}
	orig := append([]float64(nil), samples...)
	mutations.ApplyCustomGain(samples, nil, 0, 1, consts.SampleRate48000)
	assert.Equal(t, orig, samples)
}

func TestApplyCustomGainConstantFunc(t *testing.T) {
	samples := []float64{1, 1, 1, 1}
	mutations.ApplyCustomGain(samples, func(time.Duration) float64 { return 0.5 }, 0, 1, consts.SampleRate48000)
	assert.Equal(t, []float64{0.5, 0.5, 0.5, 0.5}, samples)
}

func TestApplyCustomGainTimeVarying(t *testing.T) {
	// Each frame should see a gain equal to its elapsed ms, allowing
	// us to verify that elapsed is threaded through correctly.
	samples := make([]float64, 4)
	for i := range samples {
		samples[i] = 1
	}
	mutations.ApplyCustomGain(samples, func(t time.Duration) float64 {
		return float64(t.Milliseconds())
	}, 0, 1, 1000) // 1 kHz → 1 ms per frame
	assert.Equal(t, []float64{0, 1, 2, 3}, samples)
}

func TestApplyCustomGainRespectsElapsed(t *testing.T) {
	samples := []float64{1, 1}
	mutations.ApplyCustomGain(samples, func(t time.Duration) float64 {
		return float64(t.Milliseconds())
	}, 5*time.Millisecond, 1, 1000)
	assert.Equal(t, []float64{5, 6}, samples)
}

func TestApplyCustomGainMultiChannel(t *testing.T) {
	// Same gain applied to all channels of a frame.
	samples := []float64{1, 1, 1, 1} // 2 stereo frames
	calls := 0
	mutations.ApplyCustomGain(samples, func(time.Duration) float64 {
		calls++
		return 0.5
	}, 0, 2, consts.SampleRate48000)
	assert.Equal(t, 2, calls, "fn should be called once per frame, not per sample")
	assert.Equal(t, []float64{0.5, 0.5, 0.5, 0.5}, samples)
}

func TestDecibelsKnownValues(t *testing.T) {
	assert.InDelta(t, 1.0, mutations.Decibels(0), 1e-12)
	assert.InDelta(t, 0.5, mutations.Decibels(-6.0205999), 1e-6)
	assert.InDelta(t, 0.1, mutations.Decibels(-20), 1e-9)
}

// Demonstrates the bare-buffer use case the user asked for: apply a
// fade to a raw []float64 with no Source or Timeline involvement.
func TestBareBufferFadeInUsage(t *testing.T) {
	samples := []float64{1, 1, 1, 1, 1, 1, 1, 1}
	env := mutations.FadeInEnvelope(mutations.FramesToDuration(8, consts.SampleRate48000))
	mutations.ApplyGainEnvelope(samples, env, 0, 1, consts.SampleRate48000)
	// First sample silent, last near unity.
	assert.InDelta(t, 0.0, samples[0], 1e-6)
	assert.Greater(t, samples[7], samples[0])
}
