package loudness

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// normalizeTestPeakyStereo builds a deliberately high-crest-factor
// stereo clip: a quiet 997 Hz sine (integrated loudness ≈ baseDBFS,
// per the BS.1770 stereo-sine calibration) with a full-amplitude
// `spike` sample planted on both channels every `spikeEvery` frames.
// The sparse spikes barely move the integrated loudness but set the
// true peak far above it, so the loudness-target gain and the true-peak
// ceiling gain diverge — exactly the case NormalizeClamp must clamp and
// NormalizeLimit must limit.
func normalizeTestPeakyStereo(baseDBFS, spike float64, rate, frames, spikeEvery int) []float64 {
	const freq = 997.0
	amp := mutations.Decibels(baseDBFS)
	data := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		v := amp * math.Sin(2*math.Pi*freq*float64(n)/float64(rate))
		data[n*2+0] = v
		data[n*2+1] = v
	}
	// Plant sparse spikes away from the very start so a 400 ms block of
	// clean sine establishes the loudness first.
	for n := rate; n < frames; n += spikeEvery {
		data[n*2+0] = spike
		data[n*2+1] = spike
	}
	return data
}

// ── gain-math exactness: pure constant gain to target ───────────────────

func TestNormalizeClampGainMath(t *testing.T) {
	const (
		rate     = 48000
		baseDBFS = -18.0
		target   = -23.0
		seconds  = 6
	)
	data := testSine(mutations.Decibels(baseDBFS), 997, rate, 0, rate*seconds, 2)
	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 2}

	res, err := Normalize(a, NormalizeOptions{Target: target})
	require.NoError(t, err)
	require.NotNil(t, res)

	// Input measured close to the synthesised level.
	assert.InDelta(t, baseDBFS, res.Input.Integrated, 0.1, "input integrated")

	// Clamp with a comfortable ceiling: gain is exactly the loudness
	// target term, and nothing clamps (the -5 dB cut lowers peaks).
	assert.False(t, res.Clamped, "a quiet sine cut toward target must not clamp")
	assert.False(t, res.Limited, "clamp mode never limits")
	assert.InDelta(t, target-res.Input.Integrated, res.GainDB, 1e-9, "gain == Target - measured")

	// Delivered loudness lands on target.
	assert.InDelta(t, target, res.Output.Integrated, 0.1, "output integrated on target")
}

// ── clamp engages when the ceiling term wins ────────────────────────────

func TestNormalizeClampCeilingWins(t *testing.T) {
	const (
		rate     = 48000
		baseDBFS = -30.0 // very quiet -> would want a big boost
		target   = -16.0 // loud target
		seconds  = 6
		ceiling  = CeilingEBUR128
	)
	data := normalizeTestPeakyStereo(baseDBFS, 0.9, rate, rate*seconds, rate/2)
	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 2}

	res, err := Normalize(a, NormalizeOptions{Target: target, Ceiling: ceiling})
	require.NoError(t, err)

	// The ceiling term (small) beats the loudness-target term (large boost).
	targetTerm := target - res.Input.Integrated
	ceilTerm := ceiling - res.Input.TruePeak
	require.Less(t, ceilTerm, targetTerm, "test fixture must make the ceiling bind")

	assert.True(t, res.Clamped, "ceiling should have clamped the gain")
	assert.InDelta(t, ceilTerm, res.GainDB, 1e-9, "clamped gain == Ceiling - InputTruePeak")

	// Output honours the ceiling and consequently undershoots target.
	assert.LessOrEqual(t, res.Output.TruePeak, ceiling+0.05, "output true peak at/under ceiling")
	assert.Less(t, res.Output.Integrated, target, "clamped output must undershoot target")
}

// normalizeTestBurstyStereo builds a 997 Hz stereo sine at baseDBFS
// loudness with a short raised-cosine amplitude bump (peak burstPk,
// blen frames wide) planted once per `every` frames. Unlike the
// single-sample spikes of normalizeTestPeakyStereo, the smooth,
// band-limited bumps are true-peak-limiter friendly: after the target
// gain their peak sits only modestly above the ceiling, so limiting is
// light and sparse — the case where NormalizeLimit reaches target
// (heavy hammering of hostile spikes would instead dip the loudness).
func normalizeTestBurstyStereo(baseDBFS, burstPk float64, rate, frames, every, blen int) []float64 {
	const freq = 997.0
	amp := mutations.Decibels(baseDBFS)
	data := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		env := amp
		if n >= rate {
			if ph := n % every; ph < blen {
				w := 0.5 - 0.5*math.Cos(2*math.Pi*float64(ph)/float64(blen))
				env = amp + (burstPk-amp)*w
			}
		}
		v := env * math.Sin(2*math.Pi*freq*float64(n)/float64(rate))
		data[n*2+0] = v
		data[n*2+1] = v
	}
	return data
}

// ── limit mode reaches target while taming peaks, length- & time-aligned ─

func TestNormalizeLimitHitsTarget(t *testing.T) {
	const (
		rate     = 48000
		baseDBFS = -18.0
		target   = -16.0
		seconds  = 6
		ceiling  = CeilingEBUR128
	)
	// Short (0.5 ms) smooth bumps once per second, tuned so the +2 dB
	// target gain lifts their true peak just ~0.5 dB over the ceiling.
	data := normalizeTestBurstyStereo(baseDBFS, 0.75, rate, rate*seconds, rate, 24)
	inputCopy := make([]float64, len(data))
	copy(inputCopy, data)
	origLen := len(data)

	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 2}
	res, err := Normalize(a, NormalizeOptions{Target: target, Ceiling: ceiling, Mode: NormalizeLimit})
	require.NoError(t, err)

	// Full loudness gain applied, and the peaks it raised were limited.
	assert.InDelta(t, target-res.Input.Integrated, res.GainDB, 1e-9, "limit-mode gain is the full target term")
	assert.True(t, res.Limited, "gained peaks over the ceiling must be limited")

	// Reaches target (re-measured, authoritative) and honours the ceiling.
	assert.InDelta(t, target, res.Output.Integrated, 0.15, "limit-mode output on target")
	assert.LessOrEqual(t, res.Output.TruePeak, ceiling+0.2, "output true peak under ceiling (+regrowth slack)")

	// Length preserved.
	require.Len(t, a.Data, origLen, "limit mode is length-preserving")

	// No time shift: in a sine-only region (away from any spike) the
	// signal is well under the ceiling, so the limiter is transparent
	// there and the output must equal input*gain at the SAME frame
	// index — a shift by the limiter latency would break this.
	gainLin := mutations.Decibels(res.GainDB)
	landmark := rate/2 + rate/4 // 0.75 s: between spikes at 1.0 s spacing, mid-sine
	maxErr := 0.0
	for f := landmark; f < landmark+200; f++ {
		want := inputCopy[f*2+0] * gainLin
		got := a.Data[f*2+0]
		if d := math.Abs(got - want); d > maxErr {
			maxErr = d
		}
	}
	assert.Lessf(t, maxErr, 1e-6, "sine region not time-aligned / not transparent (max err %.3g)", maxErr)
}

// ── ceiling disabled lets clamp mode reach target unconditionally ───────

func TestNormalizeCeilingDisabled(t *testing.T) {
	const (
		rate     = 48000
		baseDBFS = -30.0
		target   = -16.0
		seconds  = 6
	)
	data := normalizeTestPeakyStereo(baseDBFS, 0.9, rate, rate*seconds, rate/2)
	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 2}

	res, err := Normalize(a, NormalizeOptions{Target: target, Ceiling: math.Inf(1)})
	require.NoError(t, err)

	assert.False(t, res.Clamped, "an infinite ceiling never clamps")
	assert.InDelta(t, target-res.Input.Integrated, res.GainDB, 1e-9, "full target gain with ceiling disabled")
	assert.InDelta(t, target, res.Output.Integrated, 0.2, "output reaches target with ceiling disabled")
}

// ── error paths ─────────────────────────────────────────────────────────

func TestNormalizeSilentInput(t *testing.T) {
	a := mutations.Audio{Data: make([]float64, 48000*2), SampleRate: 48000, Channels: 2}
	res, err := Normalize(a, NormalizeOptions{Target: -23})
	assert.ErrorIs(t, err, ErrSilentInput)
	assert.Nil(t, res)
}

func TestNormalizeBadTarget(t *testing.T) {
	data := testSine(mutations.Decibels(-18), 997, 48000, 0, 48000, 2)
	for _, target := range []float64{0, 1.0, math.NaN()} {
		a := mutations.Audio{Data: append([]float64(nil), data...), SampleRate: 48000, Channels: 2}
		res, err := Normalize(a, NormalizeOptions{Target: target})
		assert.ErrorIsf(t, err, ErrBadTarget, "target=%v", target)
		assert.Nil(t, res)
	}
}

func TestNormalizeBadAudioAndCeiling(t *testing.T) {
	good := testSine(mutations.Decibels(-18), 997, 48000, 0, 48000, 2)

	// Bad sample rate / channels.
	_, err := Normalize(mutations.Audio{Data: good, SampleRate: 0, Channels: 2}, NormalizeOptions{Target: -23})
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = Normalize(mutations.Audio{Data: good, SampleRate: 48000, Channels: 0}, NormalizeOptions{Target: -23})
	assert.ErrorIs(t, err, ErrBadChannels)

	// NaN / -Inf ceiling are configuration errors (+Inf is the disable
	// sentinel and is valid, covered elsewhere).
	for _, ceil := range []float64{math.NaN(), math.Inf(-1)} {
		a := mutations.Audio{Data: append([]float64(nil), good...), SampleRate: 48000, Channels: 2}
		_, err := Normalize(a, NormalizeOptions{Target: -23, Ceiling: ceil})
		assert.ErrorIsf(t, err, ErrBadConfig, "ceiling=%v", ceil)
	}
}

// ── in-place mutation is visible on the caller's slice ──────────────────

func TestNormalizeInPlace(t *testing.T) {
	const rate = 48000
	data := testSine(mutations.Decibels(-10), 997, rate, 0, rate*5, 2)
	before := make([]float64, len(data))
	copy(before, data)

	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 2}
	res, err := Normalize(a, NormalizeOptions{Target: -23})
	require.NoError(t, err)

	// The caller's backing slice was modified in place (cut toward -23).
	gainLin := mutations.Decibels(res.GainDB)
	require.Less(t, res.GainDB, 0.0, "a -10 LUFS clip must be cut to reach -23")
	changed := false
	for i := range data {
		if data[i] != before[i] {
			changed = true
		}
		// Clamp mode is a pure constant gain: every sample scaled exactly.
		assert.InDelta(t, before[i]*gainLin, data[i], 1e-12, "sample %d", i)
	}
	assert.True(t, changed, "Normalize must mutate the caller's slice")

	// Result fields are coherent.
	assert.False(t, math.IsInf(res.Input.Integrated, 0))
	assert.False(t, math.IsInf(res.Output.Integrated, 0))
	assert.InDelta(t, -23.0, res.Output.Integrated, 0.1)
}

// TestNormalizeLimitInPlaceEquivalence pins that the in-place
// normalizeLimit (which limits a.Data directly, then flushes a tail of
// silence and shifts the result forward to undo the limiter's latency)
// produces bit-identical output to the older whole-buffer-plus-tail
// approach: feed a fresh limiter of the same config the signal followed by
// LatencyFrames of silence in one pass, then drop the leading primed
// frames. Both the long path (clip longer than the delay line) and the
// short-clip fallback (clip shorter than the delay line) are covered.
func TestNormalizeLimitInPlaceEquivalence(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
	)
	ceiling := CeilingEBUR128

	// LatencyFrames for this config, to size the short/long boundary.
	probe, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels, Ceiling: ceiling})
	require.NoError(t, err)
	lf := probe.LatencyFrames()

	cases := map[string]int{
		"long_clip_main_path": 4800,   // many delay lines
		"short_clip_fallback": lf / 2, // shorter than the delay line
	}
	for name, frames := range cases {
		t.Run(name, func(t *testing.T) {
			require.Positive(t, frames)
			// A loud tone so the gained true peak exceeds the ceiling and
			// the limiter actually attenuates (exercises the non-trivial
			// gain path, not just a passthrough).
			base := testSine(0.9, 997, rate, 0, frames, channels)

			// Subject: in-place normalizeLimit.
			got := mutations.Audio{
				SampleRate: rate, Channels: channels,
				Data: append([]float64(nil), base...),
			}
			normalizeLimit(got, ceiling)

			// Expected: an independent limiter of the same config fed the
			// signal + LatencyFrames of silence in one pass, leading primed
			// frames dropped. This is the pre-optimisation reference, built
			// fresh here rather than kept as dead code in normalize.go.
			lim, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels, Ceiling: ceiling})
			require.NoError(t, err)
			ext := make([]float64, len(base)+lf*channels)
			copy(ext, base)
			lim.Process(ext)
			want := ext[lf*channels : lf*channels+len(base)]

			require.Equal(t, want, got.Data)
		})
	}
}
