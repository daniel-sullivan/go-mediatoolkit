package r128

import (
	"fmt"
	"math"
	"math/rand/v2"
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
	for i, v := range tp.z {
		assert.Zero(t, v, "delay line cleared (flat index %d, both mirrors)", i)
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

// ---------------------------------------------------------------------------
// Invariants of the optimised delay-line layout (mirrored + interleaved)
// and the channel-lane kernels. See the TruePeaker doc comment for the
// layout contract these pin down.
// ---------------------------------------------------------------------------

// testSignal builds a deterministic interleaved float32 stream that
// exercises the numerically awkward corners: full-range values, exact
// zeros of both signs, and float32 denormals.
func testSignal(frames, ch int, seed uint64) []float32 {
	rng := rand.New(rand.NewPCG(seed, 0xF00D))
	sig := make([]float32, frames*ch)
	for k := range sig {
		switch k % 61 {
		case 7:
			sig[k] = float32(math.Copysign(0, -1)) // -0.0
		case 19:
			sig[k] = 0.0
		case 31:
			sig[k] = 1e-42 // float32 denormal
		case 43:
			sig[k] = -1e-42
		default:
			sig[k] = float32((rng.Float64()*2 - 1) * 1.2)
		}
	}
	return sig
}

// TestMirrorConsistencyAfterWrap pins the mirror-doubling invariant the
// wrap-free reads rely on: after the write cursor has wrapped several
// times (in uneven chunks, so wraps land mid-chunk), every ring slot k
// holds bit-identical data to its mirror slot k+delay, for every
// channel column.
func TestMirrorConsistencyAfterWrap(t *testing.T) {
	for _, rate := range []int{48000, 96000} {
		for _, ch := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				tp := NewTruePeaker(rate, ch)
				require.NotNil(t, tp)

				frames := 3*tp.delay + 5 // several wraps
				sig := testSignal(frames, ch, 0xA11CE)
				out := make([]float32, frames*tp.factor*ch)
				// Uneven chunks so wraps occur mid-call too.
				for pos, i := 0, 0; pos < frames; i++ {
					s := []int{5, 3, 11}[i%3]
					if s > frames-pos {
						s = frames - pos
					}
					tp.Oversample(sig[pos*ch:(pos+s)*ch], out[pos*tp.factor*ch:(pos+s)*tp.factor*ch])
					pos += s
				}

				require.NotZero(t, tp.zi, "test should end mid-ring")
				for k := 0; k < tp.delay; k++ {
					for c := 0; c < ch; c++ {
						lo := tp.z[k*ch+c]
						hi := tp.z[(k+tp.delay)*ch+c]
						if math.Float32bits(lo) != math.Float32bits(hi) {
							t.Fatalf("mirror mismatch slot %d ch %d: lo=%v (%08x) hi=%v (%08x)",
								k, c, lo, math.Float32bits(lo), hi, math.Float32bits(hi))
						}
					}
				}
			})
		}
	}
}

// TestInterleavedLayoutPerChannelIndependence pins the interleaved
// layout itself: slot k, channel c lives at z[k*ch+c], and a channel's
// column only ever holds that channel's samples. Each channel is fed a
// distinguishable integer stream (value%8 == channel index), so any
// cross-channel bleed or layout slip is caught exactly.
func TestInterleavedLayoutPerChannelIndependence(t *testing.T) {
	const ch = 3
	tp := NewTruePeaker(48000, ch)
	require.NotNil(t, tp)

	frames := 2*tp.delay + 3
	in := make([]float32, frames*ch)
	for f := 0; f < frames; f++ {
		for c := 0; c < ch; c++ {
			in[f*ch+c] = float32((f+1)*8 + c) // exact small ints; %8 encodes channel
		}
	}
	out := make([]float32, frames*tp.factor*ch)
	tp.Oversample(in, out)

	for k := 0; k < 2*tp.delay; k++ {
		for c := 0; c < ch; c++ {
			v := tp.z[k*ch+c]
			require.NotZero(t, v, "slot %d ch %d written (enough frames fed)", k, c)
			assert.Equal(t, c, int(v)%8, "slot %d column %d holds channel %d's data", k, c, c)
		}
	}
}

// TestPhase0Passthrough pins the phase-0 special case: phase 0 of the
// polyphase bank is a single center tap with coefficient exactly 1.0,
// so output phase-row 0 of frame n is exactly the input of frame
// n-phase0Index — except that the reference arithmetic's `prod + 0.0`
// canonicalises a -0.0 sample to +0.0, which the fast kernels must
// reproduce bit-for-bit (a raw passthrough copy would leak the sign).
func TestPhase0Passthrough(t *testing.T) {
	for _, rate := range []int{48000, 96000} {
		for _, ch := range []int{1, 2, 3} {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				tp := NewTruePeaker(rate, ch)
				require.NotNil(t, tp)
				require.True(t, tp.simd, "libebur128 configs are SIMD-shaped")
				p0 := tp.phase0Index

				frames := tp.delay * 3
				in := testSignal(frames, ch, 0xBEEF)
				out := make([]float32, frames*tp.factor*ch)
				tp.Oversample(in, out)

				outStride := ch * tp.factor
				for f := p0; f < frames; f++ {
					for c := 0; c < ch; c++ {
						src := in[(f-p0)*ch+c]
						want := float32(float64(src) + 0.0) // -0.0 -> +0.0, else identity
						got := out[f*outStride+c]
						if math.Float32bits(got) != math.Float32bits(want) {
							t.Fatalf("phase-0 frame %d ch %d: got %v (%08x) want %v (%08x) src %v (%08x)",
								f, c, got, math.Float32bits(got), want, math.Float32bits(want),
								src, math.Float32bits(src))
						}
					}
				}
				// The signal generator must actually have produced -0.0
				// inputs for the canonicalisation clause to be exercised.
				negZeros := 0
				for _, v := range in {
					if v == 0 && math.Signbit(float64(v)) {
						negZeros++
					}
				}
				require.NotZero(t, negZeros, "test signal must contain -0.0 samples")
			})
		}
	}
}

// originalInterpolator is a TEST-ONLY, faithful reproduction of the
// true-peak interpolator's algorithm exactly as it existed BEFORE the
// mirrored/interleaved delay-line rewrite (the commit that introduced
// TruePeaker.z as one flat mirrored slice; see git history for
// loudness/internal/r128/truepeak.go, or `git show <pre-rewrite rev>`).
// The original shape: one independent []float32 ring buffer per
// channel, no mirroring, and the C's own modulo-wrap tap addressing —
//
//	i := zi - index[t]
//	if i < 0 {
//	    i += delay
//	}
//
// — rather than the mirror-doubling trick that makes the production
// z[top-index[t]*ch] read unconditional. It shares only the polyphase
// filter coefficients (tp.filters) with the production TruePeaker —
// already independently pinned to the windowed-sinc formula by
// TestCoefficientDecomposition — and reimplements every byte of the
// ring-buffer bookkeeping from scratch, with a completely different
// memory layout (per-channel slices, not a channel-interleaved flat
// array) and a completely different wrap strategy (branch-and-add, not
// mirror doubling).
//
// # Why this is an independent oracle (and oversampleGeneric is not)
//
// oversampleGeneric was rewritten in the SAME commit that introduced the
// mirrored/interleaved layout: it also reads through tp.z as a flat
// mirrored slice, just without the SIMD channel-pair shape constraint.
// A bug in that rewrite — an off-by-one in the mirror write, a stride
// error in the interleaving, a sign slip in the wrap math — would
// reproduce identically in oversampleGeneric, because both share the
// same z/zi representation and the same "top - index[t]*ch" address
// arithmetic. It is a twin of the fast kernels' data structure, not an
// independent check of it. originalInterpolator uses none of that
// representation, so the same class of bug would surface here as a
// genuine mismatch instead of passing by common-mode construction.
//
// The arithmetic per tap is intentionally kept identical in width and
// order to the production kernels (float32 delay line, float64
// coefficient, float64 accumulator, the float64(float64(z)*c)+acc
// rounding-barrier idiom) — that part isn't the thing under test here;
// the delay-line layout and indexing is.
type originalInterpolator struct {
	tp *TruePeaker
	z  [][]float32 // per-channel ring buffers, len channels, each len delay
	zi int
}

func newOriginalInterpolator(tp *TruePeaker) *originalInterpolator {
	oi := &originalInterpolator{tp: tp}
	oi.z = make([][]float32, tp.channels)
	for c := range oi.z {
		oi.z[c] = make([]float32, tp.delay)
	}
	return oi
}

// Oversample is interp_process transliterated with the pre-rewrite
// per-channel ring buffers: frame-major, then channel, then phase, then
// tap, with the modulo-wrap tap address computed per tap exactly as the
// original Go port (and the C) computed it.
func (oi *originalInterpolator) Oversample(in, out []float32) int {
	tp := oi.tp
	ch := tp.channels
	frames := len(in) / ch
	outStride := ch * tp.factor

	inIdx := 0
	for frame := 0; frame < frames; frame++ {
		base := frame * outStride
		for chn := 0; chn < ch; chn++ {
			oi.z[chn][oi.zi] = in[inIdx]
			inIdx++

			outp := base + chn
			for f := 0; f < tp.factor; f++ {
				acc := 0.0
				filt := &tp.filters[f]
				z := oi.z[chn]
				for t := 0; t < filt.count; t++ {
					// i = (int)zi - (int)index[t]; if (i<0) i += delay
					i := oi.zi - filt.index[t]
					if i < 0 {
						i += tp.delay
					}
					acc = float64(float64(z[i])*filt.coeff[t]) + acc
				}
				out[outp] = float32(acc)
				outp += ch
			}
		}
		oi.zi++
		if oi.zi == tp.delay {
			oi.zi = 0
		}
	}
	return frames * tp.factor
}

// TestOptimizedMatchesGenericMatrix is the kernel-equality matrix: the
// dispatched fast path (assembly channel-pair kernels on arm64/amd64
// plus the scalar column tail) must produce bit-identical output and
// delay-line state to TWO independent references:
//
//   - oversampleGeneric (always compiled, on every architecture): guards
//     the fully general fallback path itself, and catches any dispatch
//     or channel-column-boundary bug in Oversample;
//   - originalInterpolator (test-only, see its doc comment above): the
//     pre-rewrite algorithm with a wholly different delay-line
//     representation, so it cannot share a layout bug with either
//     oversampleGeneric or the fast kernels.
//
// The matrix runs across channel counts, both oversampling
// configurations, and a cycling chunk schedule with persistent state.
func TestOptimizedMatchesGenericMatrix(t *testing.T) {
	const totalFrames = 12000 // > 2*4800 so the largest chunk recurs
	chunkSizes := []int{1, 7, 113, 4800}
	for _, rate := range []int{44100, 48000, 96000, 176400} {
		for _, ch := range []int{1, 2, 3, 5, 6} {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				fast := NewTruePeaker(rate, ch)
				require.NotNil(t, fast)
				require.True(t, fast.simd)
				ref := NewTruePeaker(rate, ch)
				ref.simd = false // force the fully general reference path
				orig := newOriginalInterpolator(NewTruePeaker(rate, ch))

				sig := testSignal(totalFrames, ch, uint64(rate)*17+uint64(ch))
				factor := fast.factor
				fastOut := make([]float32, 4800*factor*ch)
				refOut := make([]float32, 4800*factor*ch)
				origOut := make([]float32, 4800*factor*ch)

				pos, i := 0, 0
				for pos < totalFrames {
					s := chunkSizes[i%len(chunkSizes)]
					if s > totalFrames-pos {
						s = totalFrames - pos
					}
					in := sig[pos*ch : (pos+s)*ch]
					nFast := fast.Oversample(in, fastOut[:s*factor*ch])
					nRef := ref.Oversample(in, refOut[:s*factor*ch])
					nOrig := orig.Oversample(in, origOut[:s*factor*ch])
					require.Equal(t, nRef, nFast, "output frame count")
					require.Equal(t, nRef, nOrig, "output frame count (original algorithm)")
					for j := 0; j < s*factor*ch; j++ {
						if math.Float32bits(fastOut[j]) != math.Float32bits(refOut[j]) {
							t.Fatalf("output mismatch (fast vs oversampleGeneric) at sample %d of chunk at frame %d: fast=%v (%08x) ref=%v (%08x)",
								j, pos, fastOut[j], math.Float32bits(fastOut[j]),
								refOut[j], math.Float32bits(refOut[j]))
						}
						if math.Float32bits(fastOut[j]) != math.Float32bits(origOut[j]) {
							t.Fatalf("output mismatch (fast vs original algorithm) at sample %d of chunk at frame %d: fast=%v (%08x) orig=%v (%08x)",
								j, pos, fastOut[j], math.Float32bits(fastOut[j]),
								origOut[j], math.Float32bits(origOut[j]))
						}
					}
					pos += s
					i++
				}

				require.Equal(t, ref.zi, fast.zi, "cursors must agree")
				require.Equal(t, orig.zi, fast.zi, "cursors must agree (original algorithm)")
				for j := range ref.z {
					if math.Float32bits(ref.z[j]) != math.Float32bits(fast.z[j]) {
						t.Fatalf("delay-line state mismatch at flat index %d", j)
					}
				}
				// orig.z is a per-channel [][]float32, a different shape
				// entirely from fast.z's flat mirrored layout; compare it
				// against the logical ring contents instead, i.e. against
				// the low mirror of fast.z (flat index k*ch+c holds ring
				// slot k, channel c — see the TruePeaker doc comment).
				for c := 0; c < ch; c++ {
					for k := 0; k < fast.delay; k++ {
						o := orig.z[c][k]
						f := fast.z[k*ch+c]
						if math.Float32bits(o) != math.Float32bits(f) {
							t.Fatalf("delay-line state mismatch (original algorithm) ch %d slot %d: orig=%v (%08x) fast=%v (%08x)",
								c, k, o, math.Float32bits(o), f, math.Float32bits(f))
						}
					}
				}
			})
		}
	}
}

// TestPairKernelMatchesPairGeneric drives the architecture-resolved
// channel-pair kernel (NEON on arm64, SSE2 on amd64) directly against
// its always-compiled pure-Go shadow oversamplePairGeneric, pair by
// pair, on the same input — the most surgical asm-vs-Go comparison
// (TestOptimizedMatchesGenericMatrix covers the composed dispatch). On
// architectures without assembly the two are the same function and the
// test is a tautology, which is fine.
func TestPairKernelMatchesPairGeneric(t *testing.T) {
	const frames = 600
	for _, rate := range []int{48000, 96000} {
		for _, ch := range []int{2, 3, 6} {
			t.Run(fmt.Sprintf("rate=%d/ch=%d", rate, ch), func(t *testing.T) {
				asmTP := NewTruePeaker(rate, ch)
				goTP := NewTruePeaker(rate, ch)
				require.NotNil(t, asmTP)
				factor := asmTP.factor
				sig := testSignal(frames, ch, uint64(rate)+uint64(ch)*7919)

				for c0 := 0; c0+1 < ch; c0 += 2 {
					asmOut := make([]float32, frames*factor*ch)
					goOut := make([]float32, frames*factor*ch)
					// Feed in two chunks so persistent cursor handling is
					// covered; commit the cursor between chunks as
					// Oversample does.
					split := 217 * ch
					for _, span := range [][2]int{{0, split}, {split, frames * ch}} {
						in := sig[span[0]:span[1]]
						outLo, outHi := span[0]*factor, span[1]*factor
						asmZi := asmTP.oversamplePair(in, asmOut[outLo:outHi], c0)
						goZi := goTP.oversamplePairGeneric(in, goOut[outLo:outHi], c0)
						require.Equal(t, goZi, asmZi, "returned cursor")
						asmTP.zi = asmZi
						goTP.zi = goZi
					}
					// Only the pair's two columns are written; compare those.
					for f := 0; f < frames*factor; f++ {
						for _, c := range []int{c0, c0 + 1} {
							a, g := asmOut[f*ch+c], goOut[f*ch+c]
							if math.Float32bits(a) != math.Float32bits(g) {
								t.Fatalf("pair c0=%d output frame %d ch %d: asm=%v (%08x) go=%v (%08x)",
									c0, f, c, a, math.Float32bits(a), g, math.Float32bits(g))
							}
						}
					}
					// Reset cursors for the next pair (columns are disjoint,
					// but both kernels share the cursor).
					asmTP.zi = 0
					goTP.zi = 0
				}
				for j := range goTP.z {
					if math.Float32bits(goTP.z[j]) != math.Float32bits(asmTP.z[j]) {
						t.Fatalf("delay-line mismatch at flat index %d", j)
					}
				}
			})
		}
	}
}
