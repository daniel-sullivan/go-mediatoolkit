package loudness

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────
// Leveller test helpers. Uniquely named (levellerTest* / levellerRun*) to
// avoid colliding with measure_test.go / limiter_test.go / helpers_test.go.
// ─────────────────────────────────────────────────────────────────────────

// TestNewLevellerBadCeiling pins that a NaN or infinite Ceiling is rejected
// with ErrBadConfig unconditionally — including when DisableLimiter is set,
// where no limiter is built. This closes the earlier gap where the ceiling
// was only validated (indirectly, via NewLimiter) when the limiter existed,
// so a DisableLimiter leveller silently accepted a garbage ceiling.
func TestNewLevellerBadCeiling(t *testing.T) {
	const rate = 48000
	badCeilings := map[string]float64{
		"NaN":  math.NaN(),
		"+Inf": math.Inf(1),
		"-Inf": math.Inf(-1),
	}
	for name, ceil := range badCeilings {
		for _, disable := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/disableLimiter=%v", name, disable), func(t *testing.T) {
				_, err := NewLeveller(LevellerConfig{
					SampleRate:     rate,
					Channels:       2,
					Ceiling:        ceil,
					DisableLimiter: disable,
				})
				require.ErrorIs(t, err, ErrBadConfig)
			})
		}
	}

	// A zero Ceiling still resolves to the CeilingEBUR128 default and
	// constructs fine, in both limiter modes.
	for _, disable := range []bool{false, true} {
		_, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: disable})
		require.NoError(t, err)
	}
}

// levellerTestProgramme builds a steady stereo 997 Hz sine at a known
// integrated loudness. Per the BS.1770 stereo-sine calibration, a sine
// at the same peak amplitude on both equally-weighted channels reads an
// integrated/momentary/short-term loudness in LUFS numerically equal to
// its peak level in dBFS, so lufs maps straight to a synthesisable
// level. It is a continuous tone (never silence-gated), so the
// leveller's meter always has a valid momentary/short-term reading —
// exactly the steady programme the convergence tests need.
func levellerTestProgramme(lufs float64, rate, frames int) []float64 {
	return testSine(mutations.Decibels(lufs), 997, rate, 0, frames, 2)
}

// levellerRun processes buf in place through lv in chunkFrames-frame
// chunks (0 = one-shot). Chunking exercises the internal 100 ms
// accumulator without affecting the result.
func levellerRun(lv *Leveller, buf []float64, chunkFrames int) {
	ch := lv.Channels()
	step := chunkFrames * ch
	if step <= 0 {
		step = len(buf)
	}
	for i := 0; i < len(buf); i += step {
		end := i + step
		if end > len(buf) {
			end = len(buf)
		}
		lv.Process(buf[i:end])
	}
}

// levellerIntegrated measures the integrated loudness of an interleaved
// stereo output segment.
func levellerIntegrated(t *testing.T, seg []float64, rate int) float64 {
	t.Helper()
	m, err := Measure(mutations.Audio{Data: seg, SampleRate: rate, Channels: 2})
	require.NoError(t, err)
	return m.Integrated
}

// levellerMomentarySeries feeds out (stereo, interleaved) to a fresh
// momentary meter in 100 ms blocks and returns, per block, the block-end
// time in seconds and the momentary reading in LUFS.
func levellerMomentarySeries(t *testing.T, out []float64, rate int) (times, mom []float64) {
	t.Helper()
	m, err := NewMeter(rate, 2, ModeMomentary)
	require.NoError(t, err)
	block := (rate + 5) / 10
	step := block * 2
	for off := 0; off+step <= len(out); off += step {
		require.NoError(t, m.AddFrames(out[off:off+step]))
		v, err := m.Momentary()
		require.NoError(t, err)
		times = append(times, float64((off+step)/2)/float64(rate))
		mom = append(mom, v)
	}
	return times, mom
}

// ── convergence: steady programme settles on target ─────────────────────

func TestLevellerConvergence(t *testing.T) {
	const rate = 48000
	cases := []struct {
		name     string
		inputDB  float64
		wantGain float64
	}{
		{"cut", -18, -5},  // -18 -> -23 target: cut 5 dB (fast attack)
		{"boost", -30, 7}, // -30 -> -23 target: boost 7 dB (slow release)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// DisableLimiter isolates the AGC: output == input * gain, so
			// the measured loudness reflects the control loop alone. At
			// these levels (<= -23 LUFS) the limiter would be transparent
			// anyway.
			lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: true})
			require.NoError(t, err)

			buf := levellerTestProgramme(tc.inputDB, rate, rate*30)
			levellerRun(lv, buf, 0)

			last10 := buf[20*rate*2:]
			got := levellerIntegrated(t, last10, rate)
			assert.InDeltaf(t, -23.0, got, 1.0,
				"last 10 s should converge to target; got %.2f LUFS", got)
			assert.InDeltaf(t, tc.wantGain, lv.GainDB(), 1.0,
				"steady-state gain should be ~%.0f dB; got %.2f", tc.wantGain, lv.GainDB())
		})
	}
}

// ── gate freeze: near-silence holds the last gain instead of boosting ───

func TestLevellerGateFreeze(t *testing.T) {
	const rate = 48000
	lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: true})
	require.NoError(t, err)

	// 12 s of -18 LUFS programme (gain converges to ~-5), then 5 s of
	// -70 LUFS near-silence (well under the -50 default gate).
	prog := levellerTestProgramme(-18, rate, rate*12)
	levellerRun(lv, prog, 0)
	gainBefore := lv.GainDB()
	require.Less(t, gainBefore, 0.0, "programme should have settled to a cut")

	sil := levellerTestProgramme(-70, rate, rate*5)
	levellerRun(lv, sil, 0)
	gainAfter := lv.GainDB()

	// Frozen: the gain must not have risen toward +MaxBoost chasing the
	// noise floor — it holds the pre-silence value.
	assert.InDeltaf(t, gainBefore, gainAfter, 1.0,
		"gate should freeze gain over near-silence: before %.2f, after %.2f", gainBefore, gainAfter)
	assert.Lessf(t, gainAfter, 0.0, "gain must not boost during gated silence; got %.2f", gainAfter)
}

// ── MaxBoost clamp bounds the applied gain ──────────────────────────────

func TestLevellerMaxBoostClamp(t *testing.T) {
	const rate = 48000
	lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, MaxBoost: 12, DisableLimiter: true})
	require.NoError(t, err)

	// -45 LUFS wants a +22 dB boost toward -23; MaxBoost caps it at +12.
	// (-45 is above the -50 gate, so it is not frozen.)
	buf := levellerTestProgramme(-45, rate, rate*30)
	levellerRun(lv, buf, 0)

	assert.InDeltaf(t, 12.0, lv.GainDB(), 0.5, "boost should cap at MaxBoost; got %.2f", lv.GainDB())
	assert.LessOrEqual(t, lv.GainDB(), 12.0+1e-9, "gain must never exceed MaxBoost")
}

// ── emergency path: a sudden loud step is caught fast ───────────────────

// TestLevellerEmergencyPath drives a steady -30 LUFS programme (gain
// settles ~+7) then a sudden +15 dB step to -15 LUFS. The 3 s
// short-term loop would not notice for seconds; the fast momentary
// emergency path must catch it.
//
// DEVIATION from the plan's "output momentary recovers to <= Target+2
// within 500 ms": the *control loop* reacts within one 100 ms block
// (asserted directly on the gain below), but a genuine EBU momentary
// reading is a 400 ms-windowed integral, so the ~100 ms loud burst that
// slips through before the first gain update sits inside the momentary
// window for a further 400 ms. Measured with a real momentary meter the
// output therefore settles to Target+2 at ~800 ms, not 500 ms — a
// property of the measurement window, not the loop speed. The test
// asserts the honest windowed figure (<=900 ms) and the sub-500 ms loop
// reaction separately.
func TestLevellerEmergencyPath(t *testing.T) {
	const rate = 48000
	const target = -23.0
	lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: true})
	require.NoError(t, err)

	pre := levellerTestProgramme(-30, rate, rate*12)
	step := levellerTestProgramme(-15, rate, rate*3)

	// Process the pre-step programme and confirm the gain settled to a boost.
	preBuf := append([]float64(nil), pre...)
	levellerRun(lv, preBuf, 0)
	require.InDelta(t, 7.0, lv.GainDB(), 1.0, "gain should settle ~+7 dB before the step")

	// Process the first 500 ms of the step, then read the gain: the
	// emergency control loop must already have reacted (dropped well
	// below the +7 boost) within 500 ms of the step.
	half := rate / 2 * 2 // 500 ms of interleaved stereo samples
	first500 := append([]float64(nil), step[:half]...)
	levellerRun(lv, first500, 0)
	assert.Lessf(t, lv.GainDB(), 0.0,
		"emergency loop should react within 500 ms: gain still %.2f dB", lv.GainDB())

	// Process the remainder and confirm the windowed output momentary
	// settles to <= Target+2 within ~900 ms of the step and stays there.
	rest := append([]float64(nil), step[half:]...)
	levellerRun(lv, rest, 0)

	buf := append(append([]float64(nil), pre...), step...)
	full := append([]float64(nil), buf...)
	lv2, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: true})
	require.NoError(t, err)
	levellerRun(lv2, full, 0)

	times, mom := levellerMomentarySeries(t, full, rate)
	const stepT = 12.0
	checked := 0
	maxAfter := math.Inf(-1)
	for i, tm := range times {
		if tm < stepT+0.9 {
			continue
		}
		if math.IsInf(mom[i], 0) {
			continue
		}
		checked++
		if mom[i] > maxAfter {
			maxAfter = mom[i]
		}
	}
	require.Positive(t, checked, "expected momentary readings after the step")
	assert.LessOrEqualf(t, maxAfter, target+2.0,
		"output momentary should settle to <= %.0f LU by ~900 ms; peak after was %.2f", target+2.0, maxAfter)
}

// ── zipper: reconstructed per-sample gain is smooth (no discontinuities) ─

func TestLevellerNoZipper(t *testing.T) {
	const rate = 48000
	lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, DisableLimiter: true})
	require.NoError(t, err)

	// A level change forces the gain to ramp: -12 LUFS (needs a cut) for
	// 2 s, then -28 LUFS (needs a boost) for 4 s.
	seg1 := levellerTestProgramme(-12, rate, rate*2)
	seg2 := levellerTestProgramme(-28, rate, rate*4)
	input := append(append([]float64(nil), seg1...), seg2...)
	buf := append([]float64(nil), input...)

	levellerRun(lv, buf, 0)

	// With the limiter disabled the output is exactly input*gain, so
	// output/input recovers the effective per-sample gain. Assert it
	// never jumps between consecutive frames (a zipper would show as a
	// step). Use channel 0; skip frames where the sine is near a
	// zero-crossing to avoid dividing by ~0.
	prevGain := math.NaN()
	maxDelta := 0.0
	for f := 0; f < len(buf)/2; f++ {
		in := input[f*2]
		if math.Abs(in) < 0.02 {
			continue
		}
		g := buf[f*2] / in
		if !math.IsNaN(prevGain) {
			if d := math.Abs(g - prevGain); d > maxDelta {
				maxDelta = d
			}
		}
		prevGain = g
	}
	assert.Lessf(t, maxDelta, 1e-3, "per-sample gain jumped by %.3g (zipper)", maxDelta)
}

// ── DisableLimiter removes latency and peak protection ──────────────────

func TestLevellerDisableLimiter(t *testing.T) {
	const rate = 48000
	const frames = rate // 1 s

	enabled, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, Target: -3})
	require.NoError(t, err)
	disabled, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2, Target: -3, DisableLimiter: true})
	require.NoError(t, err)

	assert.Positive(t, enabled.Latency(), "embedded limiter should add latency")
	assert.Zero(t, disabled.Latency(), "DisableLimiter -> zero latency")

	// A hostile fs/4 tone (+3 dBTP inter-sample) run through both. The
	// gain path is identical; only the embedded limiter differs.
	input := genFs4Tone(frames, 2, 1.0)

	be := append([]float64(nil), input...)
	enabled.Process(be)
	peakEnabled := outputTruePeakDB(t, be, rate, 2)

	bd := append([]float64(nil), input...)
	disabled.Process(bd)
	peakDisabled := outputTruePeakDB(t, bd, rate, 2)

	assert.Greaterf(t, peakDisabled, CeilingEBUR128+0.5,
		"without a limiter the true peak should exceed the ceiling; got %.2f dBTP", peakDisabled)
	assert.LessOrEqualf(t, peakEnabled, CeilingEBUR128+0.5,
		"the embedded limiter should hold the ceiling; got %.2f dBTP", peakEnabled)
	assert.Greaterf(t, peakDisabled, peakEnabled+1.0,
		"the limiter should visibly reduce peaks (%.2f vs %.2f dBTP)", peakDisabled, peakEnabled)
}

// ── Reset restores a fresh-instance state (bit-identical output) ─────────

func TestLevellerReset(t *testing.T) {
	const rate = 48000
	input := levellerTestProgramme(-14, rate, rate*3)

	a, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2})
	require.NoError(t, err)
	first := append([]float64(nil), input...)
	levellerRun(a, first, 0)

	a.Reset()
	assert.Equal(t, 0.0, a.GainDB(), "Reset returns gain to 0 dB")
	afterReset := append([]float64(nil), input...)
	levellerRun(a, afterReset, 0)

	b, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2})
	require.NoError(t, err)
	fresh := append([]float64(nil), input...)
	levellerRun(b, fresh, 0)

	require.Len(t, afterReset, len(fresh))
	for i := range fresh {
		require.Equalf(t, fresh[i], afterReset[i], "sample %d differs after Reset", i)
	}
}

// ── chunk-size invariance: block accumulator makes chunking invisible ───

func TestLevellerChunkInvariance(t *testing.T) {
	const rate = 48000
	input := levellerTestProgramme(-16, rate, rate*4)

	ref := append([]float64(nil), input...)
	lv0, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2})
	require.NoError(t, err)
	levellerRun(lv0, ref, 0) // one-shot

	// 10 ms (480-frame) and 4800-frame chunks must reproduce it exactly:
	// the 100 ms accumulator aligns blocks to absolute frame position,
	// not to the Process call boundary.
	for _, chunk := range []int{480, 4800, 137} {
		got := append([]float64(nil), input...)
		lv, err := NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2})
		require.NoError(t, err)
		levellerRun(lv, got, chunk)
		for i := range ref {
			require.Equalf(t, ref[i], got[i], "chunk=%d differs at sample %d", chunk, i)
		}
	}
}

// ── config validation ───────────────────────────────────────────────────

func TestLevellerConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LevellerConfig
		wantErr error
	}{
		{"valid-default", LevellerConfig{SampleRate: 48000, Channels: 2}, nil},
		{"valid-target-zero-defaults", LevellerConfig{SampleRate: 48000, Channels: 2, Target: 0}, nil},
		{"valid-gate-disabled", LevellerConfig{SampleRate: 48000, Channels: 2, Gate: math.Inf(-1)}, nil},
		{"zero-rate", LevellerConfig{SampleRate: 0, Channels: 2}, ErrBadSampleRate},
		{"too-many-channels", LevellerConfig{SampleRate: 48000, Channels: 65}, ErrBadChannels},
		{"positive-target", LevellerConfig{SampleRate: 48000, Channels: 2, Target: 1}, ErrBadTarget},
		{"nan-target", LevellerConfig{SampleRate: 48000, Channels: 2, Target: math.NaN()}, ErrBadTarget},
		{"negative-attack", LevellerConfig{SampleRate: 48000, Channels: 2, Attack: -time.Millisecond}, ErrBadConfig},
		{"negative-release", LevellerConfig{SampleRate: 48000, Channels: 2, Release: -time.Millisecond}, ErrBadConfig},
		{"negative-maxboost", LevellerConfig{SampleRate: 48000, Channels: 2, MaxBoost: -1}, ErrBadConfig},
		{"negative-maxcut", LevellerConfig{SampleRate: 48000, Channels: 2, MaxCut: -1}, ErrBadConfig},
		{"nan-gate", LevellerConfig{SampleRate: 48000, Channels: 2, Gate: math.NaN()}, ErrBadConfig},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lv, err := NewLeveller(tc.cfg)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, lv)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, lv)
			assert.Equal(t, TargetEBUR128, lv.Target(), "zero/default target resolves to -23")
			assert.Zero(t, lv.GainDB(), "fresh leveller starts at 0 dB")
		})
	}
}
