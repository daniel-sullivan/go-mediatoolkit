package loudness

import (
	"math"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- signal generators (interleaved, all channels identical unless noted) ---

// genSquareBursts builds a hostile full-scale square wave that flips
// polarity every `period` frames — a worst case for inter-sample peaks
// because the sharp edges reconstruct well above +/-1.0.
func genSquareBursts(frames, channels, period int, amp float64) []float64 {
	out := make([]float64, frames*channels)
	for f := 0; f < frames; f++ {
		v := amp
		if (f/period)%2 == 1 {
			v = -amp
		}
		for c := 0; c < channels; c++ {
			out[f*channels+c] = v
		}
	}
	return out
}

// genFs4Tone builds an fs/4 tone phased so its SAMPLES sit at +/-amp
// while the reconstructed waveform peaks at amp*sqrt(2). At amp=1.0 the
// samples are +/-1.0 (0 dBFS) but the true peak is ~+3 dBTP — the
// EBU Tech 3341 case-19 style over-full-scale signal.
func genFs4Tone(frames, channels int, amp float64) []float64 {
	out := make([]float64, frames*channels)
	for f := 0; f < frames; f++ {
		v := amp * math.Cos(math.Pi*float64(f)/2+math.Pi/4)
		for c := 0; c < channels; c++ {
			out[f*channels+c] = v
		}
	}
	return out
}

// genRandomPCM builds a deterministic random signal in [-amp, amp].
func genRandomPCM(frames, channels int, amp float64, seed uint64) []float64 {
	r := rand.New(rand.NewPCG(seed, seed*2+1))
	out := make([]float64, frames*channels)
	for i := range out {
		out[i] = (r.Float64()*2 - 1) * amp
	}
	return out
}

// --- harness helpers ---

// runChunked feeds input through l in frame-aligned chunks of chunkFrames
// (0 = one-shot), then flushes LatencyFrames of silence, and returns the
// full concatenated output (len == len(input) + L*channels).
func runChunked(l *Limiter, input []float64, channels, chunkFrames int) []float64 {
	buf := make([]float64, len(input))
	copy(buf, input)
	step := chunkFrames * channels
	if step <= 0 {
		step = len(buf)
	}
	out := make([]float64, 0, len(buf)+l.LatencyFrames()*channels)
	for i := 0; i < len(buf); i += step {
		end := i + step
		if end > len(buf) {
			end = len(buf)
		}
		l.Process(buf[i:end])
		out = append(out, buf[i:end]...)
	}
	tail := make([]float64, l.LatencyFrames()*channels)
	l.Process(tail)
	out = append(out, tail...)
	return out
}

// outputTruePeakDB measures the true peak (max across channels) of an
// interleaved output buffer with a ModeTruePeak Meter, in dBTP.
func outputTruePeakDB(t *testing.T, output []float64, rate, channels int) float64 {
	t.Helper()
	m, err := NewMeter(rate, channels, ModeTruePeak)
	require.NoError(t, err)
	require.NoError(t, m.AddFrames(output))
	peak := 0.0
	for c := 0; c < channels; c++ {
		p, err := m.TruePeak(c)
		require.NoError(t, err)
		if p > peak {
			peak = p
		}
	}
	return 20 * math.Log10(peak)
}

// --- tests ---

func TestLimiterCeilingHonoured(t *testing.T) {
	const channels = 2
	const slack = 0.2 // dB, per the inter-sample regrowth caveat

	rates := []int{44100, 48000, 96000}
	ceilings := []float64{CeilingEBUR128, -3.0, 0.0 /* -> default */}

	signals := []struct {
		name string
		gen  func(frames, rate int) []float64
	}{
		{"square-bursts", func(frames, _ int) []float64 { return genSquareBursts(frames, channels, 17, 1.0) }},
		{"fs4-plus3dbtp", func(frames, _ int) []float64 { return genFs4Tone(frames, channels, 1.0) }},
		{"random-1.4", func(frames, _ int) []float64 { return genRandomPCM(frames, channels, 1.4, 0xC0FFEE) }},
	}

	for _, rate := range rates {
		frames := rate / 5 // 0.2 s
		for _, ceil := range ceilings {
			for _, sig := range signals {
				name := sig.name
				l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels, Ceiling: ceil})
				require.NoError(t, err)
				input := sig.gen(frames, rate)
				out := runChunked(l, input, channels, 0)
				got := outputTruePeakDB(t, out, rate, channels)
				want := l.Ceiling() // resolved ceiling in dBTP
				assert.LessOrEqualf(t, got, want+slack,
					"rate=%d ceiling=%.1f signal=%s: output true peak %.3f dBTP exceeds %.3f+%.1f",
					rate, want, name, got, want, slack)
			}
		}
	}
}

func TestLimiterTransparency(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
		frames   = 4096
	)
	l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)

	// Amplitude 0.4 sits well below the -1 dBTP ceiling (and below
	// ceiling-1 dB), so no limiting ever triggers: g stays exactly 1.
	input := testSine(0.4, 997, rate, 0, frames, channels)
	buf := make([]float64, len(input))
	copy(buf, input)
	l.Process(buf)

	lf := l.LatencyFrames()
	for f := lf; f < frames; f++ {
		for c := 0; c < channels; c++ {
			got := buf[f*channels+c]
			want := input[(f-lf)*channels+c]
			require.Equalf(t, want, got,
				"frame %d ch %d: expected exact delayed passthrough", f, c)
		}
	}
	assert.Zero(t, l.GainReductionDB(), "no limiting expected on a quiet signal")
}

func TestLimiterLatencyExactness(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
	)
	l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)
	lf := l.LatencyFrames()

	const k = 100
	frames := k + 2*lf
	input := make([]float64, frames*channels)
	// Small impulse (0.5 < ceiling) on channel 0 -> no attenuation.
	input[k*channels+0] = 0.5

	buf := make([]float64, len(input))
	copy(buf, input)
	l.Process(buf)

	// The impulse emerges exactly LatencyFrames later, unattenuated.
	assert.Equal(t, 0.5, buf[(k+lf)*channels+0], "impulse must emerge at k+LatencyFrames")
	// Everything else is exactly zero.
	for i, v := range buf {
		if i == (k+lf)*channels+0 {
			continue
		}
		require.Zerof(t, v, "unexpected non-zero sample at index %d", i)
	}
}

func TestLimiterAttackRampSmoothness(t *testing.T) {
	const (
		rate     = 48000
		channels = 1
	)
	l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)

	// 50 ms silence, then a sustained full-scale square (a hard step
	// into a peak). Record the linear gain frame-by-frame.
	sil := rate / 20
	loud := rate / 20
	input := make([]float64, 0, (sil+loud)*channels)
	input = append(input, make([]float64, sil*channels)...)
	input = append(input, genSquareBursts(loud, channels, 23, 1.0)...)

	gains := make([]float64, sil+loud)
	frame := make([]float64, channels)
	for f := 0; f < sil+loud; f++ {
		copy(frame, input[f*channels:(f+1)*channels])
		l.Process(frame)
		gains[f] = mutations.Decibels(-l.GainReductionDB())
	}

	// No gain discontinuity: bounded per-frame delta everywhere.
	maxDelta := 0.0
	for f := 1; f < len(gains); f++ {
		d := math.Abs(gains[f] - gains[f-1])
		if d > maxDelta {
			maxDelta = d
		}
	}
	assert.Lessf(t, maxDelta, 0.05, "gain moved by %.4f in one frame (attack not smooth)", maxDelta)

	// The step is actually limited (gain drops well below unity).
	minGain := 1.0
	minIdx := 0
	for f, g := range gains {
		if g < minGain {
			minGain, minIdx = g, f
		}
	}
	assert.Lessf(t, minGain, 0.9, "expected meaningful gain reduction, min gain was %.4f", minGain)

	// The descent into the peak is monotone (a clean ramp, no wobble):
	// from the first drop below unity to the minimum, gain never rises.
	firstDrop := -1
	for f, g := range gains {
		if g < 1.0-1e-9 {
			firstDrop = f
			break
		}
	}
	require.GreaterOrEqual(t, firstDrop, 0)
	for f := firstDrop + 1; f <= minIdx; f++ {
		require.LessOrEqualf(t, gains[f], gains[f-1]+1e-9,
			"attack ramp not monotone at frame %d (%.5f -> %.5f)", f, gains[f-1], gains[f])
	}
}

func TestLimiterReleaseBehaviour(t *testing.T) {
	const (
		rate     = 48000
		channels = 1
	)
	// Silence, a short loud burst, then a long silence to observe recovery.
	burst := rate / 100 // 10 ms
	quiet := rate / 2   // 500 ms
	build := func() []float64 {
		in := make([]float64, 0, (burst+quiet)*channels)
		in = append(in, genSquareBursts(burst, channels, 13, 1.0)...)
		in = append(in, make([]float64, quiet*channels)...)
		return in
	}

	// recoveryThreshold is the gain we consider "recovered to unity".
	// A one-pole release only approaches 1 asymptotically, so we pick a
	// level both release constants under test can reach inside `quiet`.
	const recoveryThreshold = 0.95

	// recoveryFrames returns how many frames it takes, after the burst,
	// for the gain to climb back to recoveryThreshold, asserting monotone
	// recovery along the way.
	recoveryFrames := func(release time.Duration) int {
		l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels, Release: release})
		require.NoError(t, err)
		input := build()
		gains := make([]float64, len(input)/channels)
		frame := make([]float64, channels)
		for f := 0; f < len(gains); f++ {
			copy(frame, input[f*channels:(f+1)*channels])
			l.Process(frame)
			gains[f] = mutations.Decibels(-l.GainReductionDB())
		}
		// Find the minimum gain, then verify monotone climb to ~unity.
		minIdx, minGain := 0, 1.0
		for f, g := range gains {
			if g < minGain {
				minGain, minIdx = g, f
			}
		}
		require.Less(t, minGain, 0.9, "burst should have driven gain down")
		recovered := -1
		for f := minIdx + 1; f < len(gains); f++ {
			require.GreaterOrEqualf(t, gains[f], gains[f-1]-1e-9,
				"release should be monotone non-decreasing at frame %d", f)
			if gains[f] >= recoveryThreshold {
				recovered = f - minIdx
				break
			}
		}
		require.Greater(t, recovered, 0, "gain never recovered to unity")
		return recovered
	}

	fast := recoveryFrames(30 * time.Millisecond)
	slow := recoveryFrames(120 * time.Millisecond)
	// A longer release constant must take (meaningfully) longer to recover.
	assert.Greaterf(t, slow, fast*2,
		"120ms release recovered in %d frames vs %d for 30ms; release constant not honoured", slow, fast)
}

func TestLimiterReset(t *testing.T) {
	const (
		rate     = 44100
		channels = 2
		frames   = 8000
	)
	input := genFs4Tone(frames, channels, 1.0)

	a, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)
	first := runChunked(a, input, channels, 0)

	a.Reset()
	afterReset := runChunked(a, input, channels, 0)

	b, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)
	fresh := runChunked(b, input, channels, 0)

	require.Equal(t, len(fresh), len(afterReset))
	for i := range fresh {
		require.Equalf(t, fresh[i], afterReset[i], "sample %d differs after Reset", i)
		require.Equalf(t, first[i], afterReset[i], "sample %d differs between runs", i)
	}
}

func TestLimiterPerChannelCoherence(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
		frames   = 4000
	)
	l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
	require.NoError(t, err)

	// Left is a hot fs/4 tone (clips); right is a quiet constant. Linked
	// limiting must apply the SAME gain to both channels.
	input := make([]float64, frames*channels)
	left := genFs4Tone(frames, 1, 1.0)
	const rightConst = 0.5
	for f := 0; f < frames; f++ {
		input[f*channels+0] = left[f]
		input[f*channels+1] = rightConst
	}

	buf := make([]float64, len(input))
	copy(buf, input)
	frameBuf := make([]float64, channels)
	lf := l.LatencyFrames()
	maxDiff := 0.0
	checked := 0
	for f := 0; f < frames; f++ {
		copy(frameBuf, buf[f*channels:(f+1)*channels])
		l.Process(buf[f*channels : (f+1)*channels])
		// The gain applied at output frame f corresponds to input frame f-lf.
		src := f - lf
		if src < 0 {
			continue
		}
		inL := input[src*channels+0]
		outL := buf[f*channels+0]
		outR := buf[f*channels+1]
		if math.Abs(inL) < 1e-6 {
			continue // avoid dividing by a near-zero left sample
		}
		gL := outL / inL
		gR := outR / rightConst
		d := math.Abs(gL - gR)
		if d > maxDiff {
			maxDiff = d
		}
		checked++
	}
	require.Positive(t, checked)
	assert.Lessf(t, maxDiff, 1e-9, "channels received different gains (max diff %.3g) - not linked", maxDiff)
}

func TestLimiterConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     LimiterConfig
		wantErr error
	}{
		{"valid-default", LimiterConfig{SampleRate: 48000, Channels: 2}, nil},
		{"valid-positive-ceiling", LimiterConfig{SampleRate: 48000, Channels: 2, Ceiling: 3.0}, nil},
		{"valid-192k-bypass", LimiterConfig{SampleRate: 192000, Channels: 1}, nil},
		{"zero-rate", LimiterConfig{SampleRate: 0, Channels: 2}, ErrBadSampleRate},
		{"negative-rate", LimiterConfig{SampleRate: -1, Channels: 2}, ErrBadSampleRate},
		{"zero-channels", LimiterConfig{SampleRate: 48000, Channels: 0}, ErrBadChannels},
		{"too-many-channels", LimiterConfig{SampleRate: 48000, Channels: 65}, ErrBadChannels},
		{"negative-lookahead", LimiterConfig{SampleRate: 48000, Channels: 2, Lookahead: -time.Millisecond}, ErrBadConfig},
		{"negative-release", LimiterConfig{SampleRate: 48000, Channels: 2, Release: -time.Millisecond}, ErrBadConfig},
		{"nan-ceiling", LimiterConfig{SampleRate: 48000, Channels: 2, Ceiling: math.NaN()}, ErrBadConfig},
		{"inf-ceiling", LimiterConfig{SampleRate: 48000, Channels: 2, Ceiling: math.Inf(1)}, ErrBadConfig},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l, err := NewLimiter(tc.cfg)
			if tc.wantErr != nil {
				assert.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, l)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, l)
			assert.Positive(t, l.LatencyFrames())
			assert.Positive(t, l.Latency())
		})
	}

	// Zero-value ceiling resolves to the EBU R128 default.
	l, err := NewLimiter(LimiterConfig{SampleRate: 48000, Channels: 2})
	require.NoError(t, err)
	assert.Equal(t, CeilingEBUR128, l.Ceiling())
	// A positive ceiling is retained verbatim.
	l2, err := NewLimiter(LimiterConfig{SampleRate: 48000, Channels: 2, Ceiling: 3.0})
	require.NoError(t, err)
	assert.Equal(t, 3.0, l2.Ceiling())
}

func TestLimiterChunkingInvariance(t *testing.T) {
	const (
		rate     = 48000
		channels = 2
		frames   = 12000
	)
	input := genRandomPCM(frames, channels, 1.3, 0xBEEF)

	oneShot := func() []float64 {
		l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
		require.NoError(t, err)
		return runChunked(l, input, channels, 0)
	}
	ref := oneShot()

	for _, chunk := range []int{1, 7, 113, 4800} {
		l, err := NewLimiter(LimiterConfig{SampleRate: rate, Channels: channels})
		require.NoError(t, err)
		got := runChunked(l, input, channels, chunk)
		require.Equalf(t, len(ref), len(got), "chunk=%d length mismatch", chunk)
		for i := range ref {
			require.Equalf(t, ref[i], got[i], "chunk=%d differs at sample %d", chunk, i)
		}
	}
}
