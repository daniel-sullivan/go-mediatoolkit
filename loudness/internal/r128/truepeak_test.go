package r128

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// windowedSinc recomputes libebur128's interp_create prototype
// coefficient for tap j (a Hann-windowed sinc), using the exact same
// expression order and double precision as buildCoefficients. The unit
// tests use it as an independent reference for the polyphase
// decomposition and the impulse response.
func windowedSinc(j, taps, factor int) float64 {
	m := float64(j) - float64(taps-1)/2.0
	c := 1.0
	if math.Abs(m) > almostZero {
		c = math.Sin(m*math.Pi/float64(factor)) / (m * math.Pi / float64(factor))
	}
	c *= 0.5 * (1 - math.Cos(2*math.Pi*float64(j)/float64(taps-1)))
	return c
}

// TestNewTruePeakerFactorSelection pins the rate->factor/taps/delay
// selection to ebur128_init_resampler: 4x below 96 kHz, 2x from 96 kHz up
// to (not including) 192 kHz, and a nil bypass at 192 kHz and above.
func TestNewTruePeakerFactorSelection(t *testing.T) {
	tests := []struct {
		name       string
		rate       int
		wantNil    bool
		wantFactor int
		wantDelay  int
	}{
		{"44.1k -> 4x", 44100, false, 4, 13},   // (49+3)/4
		{"48k -> 4x", 48000, false, 4, 13},     //
		{"88.2k -> 4x", 88200, false, 4, 13},   // still < 96k
		{"96k -> 2x", 96000, false, 2, 25},     // (49+1)/2
		{"176.4k -> 2x", 176400, false, 2, 25}, //
		{"just under 192k -> 2x", 191999, false, 2, 25},
		{"192k -> bypass", 192000, true, 0, 0},
		{"200k -> bypass", 200000, true, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tp := NewTruePeaker(tc.rate, 2)
			if tc.wantNil {
				require.Nil(t, tp, "expected nil bypass interpolator at %d Hz", tc.rate)
				// Nil-safe accessors and methods must not panic.
				assert.Equal(t, 1, tp.Factor())
				assert.Equal(t, 0, tp.Delay())
				assert.Equal(t, 0, tp.Channels())
				tp.Reset()
				var peaks []float64
				tp.Process(nil, peaks)
				return
			}
			require.NotNil(t, tp)
			assert.Equal(t, tc.wantFactor, tp.factor, "factor")
			assert.Equal(t, tc.wantFactor, tp.Factor())
			assert.Equal(t, truePeakTaps, tp.taps, "taps")
			assert.Equal(t, tc.wantDelay, tp.delay, "delay")
			assert.Equal(t, tc.wantDelay, tp.Delay())
			assert.Len(t, tp.filters, tc.wantFactor, "one sub-filter per phase")
			assert.Equal(t, 2, tp.Channels())
		})
	}
}

// TestCoefficientDecomposition verifies the polyphase split reproduces
// the windowed-sinc prototype tap-for-tap: every live coefficient equals
// the prototype value at its reconstructed tap index j = index*factor + f,
// every prototype tap with |c| > ALMOST_ZERO lands in exactly one
// sub-filter, and taps with |c| <= ALMOST_ZERO are dropped.
func TestCoefficientDecomposition(t *testing.T) {
	for _, factor := range []int{4, 2} {
		rate := 44100
		if factor == 2 {
			rate = 96000
		}
		tp := NewTruePeaker(rate, 1)
		require.NotNil(t, tp)

		// Count how many prototype taps should survive per phase, and
		// check each live coefficient matches the prototype exactly.
		wantCount := make([]int, factor)
		for j := 0; j < tp.taps; j++ {
			c := windowedSinc(j, tp.taps, factor)
			if math.Abs(c) > almostZero {
				wantCount[j%factor]++
			}
		}

		total := 0
		for f := 0; f < factor; f++ {
			assert.Equal(t, wantCount[f], tp.filters[f].count,
				"factor=%d phase=%d live tap count", factor, f)
			for tt := 0; tt < tp.filters[f].count; tt++ {
				j := tp.filters[f].index[tt]*factor + f
				want := windowedSinc(j, tp.taps, factor)
				// Bit-exact: same formula, same order, same precision.
				assert.Equal(t, want, tp.filters[f].coeff[tt],
					"factor=%d phase=%d tap=%d (j=%d) coefficient", factor, f, tt, j)
				assert.Greater(t, math.Abs(tp.filters[f].coeff[tt]), almostZero,
					"live coeff must exceed ALMOST_ZERO")
			}
			total += tp.filters[f].count
		}
		// Sum over phases equals number of surviving prototype taps.
		wantTotal := 0
		for j := 0; j < tp.taps; j++ {
			if math.Abs(windowedSinc(j, tp.taps, factor)) > almostZero {
				wantTotal++
			}
		}
		assert.Equal(t, wantTotal, total, "factor=%d total live taps", factor)
	}
}

// TestImpulseResponseReconstructsPrototype feeds a unit impulse and shows
// the interpolated output, read out in interpolation order (frame*factor
// + phase), reconstructs the windowed-sinc prototype filter tap-for-tap.
//
// Why this holds: with a single 1.0 at input frame 0 and zeros after, the
// only non-zero delay-line entry is z[0][0]=1. At output frame `frame`,
// phase f, the accumulator picks up exactly the coefficient whose delay
// index equals `frame`, i.e. prototype tap j = frame*factor + f. So
// output[frame*factor+f] == float32(prototype[j]).
func TestImpulseResponseReconstructsPrototype(t *testing.T) {
	for _, factor := range []int{4, 2} {
		rate := 48000
		if factor == 2 {
			rate = 96000
		}
		tp := NewTruePeaker(rate, 1)
		require.NotNil(t, tp)

		// Feed `delay` input frames: an impulse then zeros, enough to
		// exercise every tap index (max index < delay).
		in := make([]float32, tp.delay)
		in[0] = 1.0
		out := make([]float32, tp.delay*factor)
		n := tp.Oversample(in, out)
		require.Equal(t, tp.delay*factor, n, "output frame count")

		for j := 0; j < tp.taps; j++ {
			want := float32(windowedSinc(j, tp.taps, factor))
			// Dropped taps (|c|<=ALMOST_ZERO) never get written into a
			// sub-filter, so the reconstructed sample is exactly 0 while
			// the prototype value is a tiny non-zero; allow that gap.
			if math.Abs(float64(want)) <= almostZero {
				assert.LessOrEqual(t, math.Abs(float64(out[j])), almostZero,
					"factor=%d dropped tap j=%d should reconstruct ~0", factor, j)
				continue
			}
			assert.Equal(t, want, out[j],
				"factor=%d impulse response tap j=%d", factor, j)
		}
	}
}

// TestInterSamplePeakExceedsSamplePeak drives an fs/4 sine offset by 45
// degrees at 44.1 kHz. Every sample of such a tone has magnitude
// A/sqrt(2) (the sample peak), but the underlying continuous tone reaches
// A between samples — the classic inter-sample overshoot of about +3 dB.
// The 4x interpolator must reveal a true peak close to A, comfortably
// above the sample peak.
func TestInterSamplePeakExceedsSamplePeak(t *testing.T) {
	const (
		rate   = 44100
		amp    = 0.5
		frames = 4000
	)
	tp := NewTruePeaker(rate, 1)
	require.NotNil(t, tp)

	samples := make([]float64, frames)
	samplePeak := 0.0
	for n := 0; n < frames; n++ {
		// fs/4 tone: angular step pi/2 per sample, phase offset pi/4.
		v := amp * math.Sin(math.Pi/2*float64(n)+math.Pi/4)
		samples[n] = v
		if math.Abs(v) > samplePeak {
			samplePeak = math.Abs(v)
		}
	}

	peaks := make([]float64, 1)
	tp.Process(samples, peaks)
	truePeak := peaks[0]

	// Sample peak is A/sqrt(2) ~= 0.35355.
	assert.InDelta(t, amp/math.Sqrt2, samplePeak, 1e-9, "sample peak of fs/4 45deg tone")
	// True peak must overshoot the sample peak substantially...
	assert.Greater(t, truePeak, samplePeak*1.3, "true peak should overshoot sample peak (~+3dB)")
	// ...and land near the true amplitude A (peak sits on the 4x grid, so
	// the 49-tap reconstruction is close; allow modest truncation slack).
	assert.InDelta(t, amp, truePeak, amp*0.03, "true peak should approach the continuous amplitude")
	assert.LessOrEqual(t, truePeak, amp*1.02, "true peak must not blow past the amplitude")
}

// TestBypassReportsSamplePeak documents the >=192 kHz bypass seam: the
// interpolator is nil, Process contributes nothing, so a meter's
// max(true_peak, sample_peak) reduces to the sample peak.
func TestBypassReportsSamplePeak(t *testing.T) {
	const (
		rate   = 192000
		frames = 512
	)
	tp := NewTruePeaker(rate, 1)
	require.Nil(t, tp, "192 kHz must bypass the interpolator")

	samples := make([]float64, frames)
	samplePeak := 0.0
	for n := 0; n < frames; n++ {
		v := 0.9 * math.Sin(2*math.Pi*1000*float64(n)/rate)
		samples[n] = v
		if math.Abs(v) > samplePeak {
			samplePeak = math.Abs(v)
		}
	}

	// The meter folds the interpolator's contribution into a true-peak
	// accumulator seeded from the sample peak. With a nil interpolator the
	// Process call is a no-op, so the reported true peak equals the sample
	// peak exactly.
	truePeakAccum := []float64{samplePeak}
	tp.Process(samples, truePeakAccum)
	assert.Equal(t, samplePeak, truePeakAccum[0], "bypass true peak must equal sample peak")
}

// TestResetClearsDelayLines shows Reset returns the interpolator to its
// fresh state: identical input after a Reset produces identical output to
// a brand-new interpolator, and the delay lines and cursor are zeroed.
func TestResetClearsDelayLines(t *testing.T) {
	tp := NewTruePeaker(48000, 2)
	require.NotNil(t, tp)

	// Warm up with arbitrary data to dirty the delay lines and cursor.
	warm := make([]float64, 200)
	for i := range warm {
		warm[i] = math.Sin(float64(i) * 0.3)
	}
	scratchPeaks := make([]float64, 2)
	tp.Process(warm, scratchPeaks)
	require.NotZero(t, tp.zi, "cursor should have advanced")

	tp.Reset()
	assert.Zero(t, tp.zi, "cursor cleared")
	for c := range tp.z {
		for _, v := range tp.z[c] {
			assert.Zero(t, v, "delay line cleared")
		}
	}

	// A fresh interpolator and the reset one must agree bit-for-bit.
	probe := make([]float64, 2*64)
	for i := range probe {
		probe[i] = math.Sin(float64(i)*0.11) * 0.7
	}
	fresh := NewTruePeaker(48000, 2)
	gotFresh := make([]float64, 2)
	gotReset := make([]float64, 2)
	fresh.Process(probe, gotFresh)
	tp.Process(probe, gotReset)
	assert.Equal(t, gotFresh, gotReset, "reset interpolator must match a fresh one")
}

// TestPerChannelIndependence confirms each channel's peak is tracked in
// its own delay line: a loud left channel does not bleed into a silent
// right channel.
func TestPerChannelIndependence(t *testing.T) {
	tp := NewTruePeaker(48000, 2)
	require.NotNil(t, tp)

	const frames = 1024
	samples := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		samples[n*2+0] = 0.8 * math.Sin(2*math.Pi*997*float64(n)/48000) // loud L
		samples[n*2+1] = 0.0                                            // silent R
	}
	peaks := make([]float64, 2)
	tp.Process(samples, peaks)

	assert.Greater(t, peaks[0], 0.5, "loud channel has a true peak")
	assert.Zero(t, peaks[1], "silent channel stays at zero")
}

// TestChunkingInvariance shows the persistent delay-line state makes
// chunked processing bit-identical to whole-buffer processing: the same
// signal fed in prime-sized chunks yields the same per-channel true peak
// as one call.
func TestChunkingInvariance(t *testing.T) {
	const (
		rate   = 44100
		ch     = 2
		frames = 5000
	)
	sig := make([]float64, frames*ch)
	for n := 0; n < frames; n++ {
		sig[n*ch+0] = 0.6 * math.Sin(2*math.Pi*440*float64(n)/rate)
		sig[n*ch+1] = 0.9 * math.Sin(2*math.Pi*7000*float64(n)/rate+0.5)
	}

	whole := NewTruePeaker(rate, ch)
	wholePeaks := make([]float64, ch)
	whole.Process(sig, wholePeaks)

	chunked := NewTruePeaker(rate, ch)
	chunkPeaks := make([]float64, ch)
	// Walk the stream in prime-sized (in frames) chunks, folding into the
	// same accumulator across chunks — mirroring how a meter feeds the
	// interpolator over many add_frames calls. The delay-line state
	// carries filter history across the chunk boundaries.
	sizes := []int{1, 7, 113, 977}
	pos, i := 0, 0
	for pos < frames {
		s := sizes[i%len(sizes)]
		if s > frames-pos {
			s = frames - pos
		}
		chunked.Process(sig[pos*ch:(pos+s)*ch], chunkPeaks)
		pos += s
		i++
	}
	assert.Equal(t, wholePeaks, chunkPeaks, "chunked processing must match whole-buffer")
}
