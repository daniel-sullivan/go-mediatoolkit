package r128

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deriveStages recomputes the two K-weighting biquad stages from
// scratch, independently of the port under test, so the test can check
// them against the published BS.1770-4 table values before trusting the
// convolved 4th-order coefficients that newKFilter produces. It mirrors
// the bilinear-transform derivation in libebur128's ebur128_init_filter.
//
// pb/pa are the stage-1 shelving-filter numerator/denominator; rb/ra are
// the stage-2 RLB high-pass numerator/denominator. Each is [b0,b1,b2] /
// [a0,a1,a2] with a0 == 1.
func deriveStages(sampleRate int) (pb, pa, rb, ra [3]float64) {
	sr := float64(sampleRate)

	// Stage 1: +4 dB high-frequency shelf.
	f0 := 1681.974450955533
	g := 3.999843853973347
	q := 0.7071752369554196

	k := math.Tan(math.Pi * f0 / sr)
	vh := math.Pow(10.0, g/20.0)
	vb := math.Pow(vh, 0.4996667741545416)

	a0 := 1.0 + k/q + k*k
	pb[0] = (vh + vb*k/q + k*k) / a0
	pb[1] = 2.0 * (k*k - vh) / a0
	pb[2] = (vh - vb*k/q + k*k) / a0
	pa[0] = 1.0
	pa[1] = 2.0 * (k*k - 1.0) / a0
	pa[2] = (1.0 - k/q + k*k) / a0

	// Stage 2: RLB high-pass.
	f0 = 38.13547087602444
	q = 0.5003270373238773
	k = math.Tan(math.Pi * f0 / sr)

	rb = [3]float64{1.0, -2.0, 1.0}
	d := 1.0 + k/q + k*k
	ra[0] = 1.0
	ra[1] = 2.0 * (k*k - 1.0) / d
	ra[2] = (1.0 - k/q + k*k) / d
	return pb, pa, rb, ra
}

// convolve returns the coefficients of the polynomial product of two
// quadratics [x0,x1,x2] and [y0,y1,y2] — the 4th-order filter obtained
// by cascading the two stages.
func convolve(x, y [3]float64) [5]float64 {
	return [5]float64{
		x[0] * y[0],
		x[0]*y[1] + x[1]*y[0],
		x[0]*y[2] + x[1]*y[1] + x[2]*y[0],
		x[1]*y[2] + x[2]*y[1],
		x[2] * y[2],
	}
}

// TestKWeightStagesMatchBS1770Table1 checks that the per-stage
// coefficients derived by the bilinear transform reproduce the canonical
// ITU-R BS.1770-4 tabulated values at 48 kHz, and that convolving the two
// stages reproduces newKFilter's 4th-order coefficients.
func TestKWeightStagesMatchBS1770Table1(t *testing.T) {
	const sampleRate = 48000

	pb, pa, rb, ra := deriveStages(sampleRate)

	// BS.1770-4 Table 1 — stage 1, the high-frequency shelving filter
	// (a0 normalised to 1). Denominator taps are in libebur128's
	// subtract-convention, so a1 is negative and a2 positive, matching
	// y = b0 x + b1 x[-1] + b2 x[-2] - a1 y[-1] - a2 y[-2].
	const (
		table1B0 = 1.53512485958697
		table1B1 = -2.69169618940638
		table1B2 = 1.19839281085285
		table1A1 = -1.69065929318241
		table1A2 = 0.73248077421585
	)
	// BS.1770-4 Table 2 — stage 2, the RLB high-pass. Numerator is the
	// exact differentiator [1,-2,1]; denominator is rate-derived.
	const (
		table2A1 = -1.99004745483398
		table2A2 = 0.99007225036621
	)

	const tol = 1e-14
	assert.InDelta(t, table1B0, pb[0], tol, "stage1 b0")
	assert.InDelta(t, table1B1, pb[1], tol, "stage1 b1")
	assert.InDelta(t, table1B2, pb[2], tol, "stage1 b2")
	assert.InDelta(t, table1A1, pa[1], tol, "stage1 a1")
	assert.InDelta(t, table1A2, pa[2], tol, "stage1 a2")

	assert.Equal(t, 1.0, rb[0], "stage2 b0")
	assert.Equal(t, -2.0, rb[1], "stage2 b1")
	assert.Equal(t, 1.0, rb[2], "stage2 b2")
	assert.InDelta(t, table2A1, ra[1], tol, "stage2 a1")
	assert.InDelta(t, table2A2, ra[2], tol, "stage2 a2")

	// The convolved product polynomial must match what newKFilter
	// derives (both start from the identical bilinear formulas, so this
	// is essentially exact — a loose tolerance guards only against the
	// FMA-barrier differences between this reference convolution and the
	// port's barriered one).
	wantB := convolve(pb, rb)
	wantA := convolve(pa, ra)
	kf := NewKFilter(sampleRate, 1)
	gotB, gotA := kf.B, kf.A
	for i := 0; i < 5; i++ {
		assert.InDelta(t, wantB[i], gotB[i], 1e-12, "convolved b[%d]", i)
		assert.InDelta(t, wantA[i], gotA[i], 1e-12, "convolved a[%d]", i)
	}

	// a[0] is the (unit) normalisation term.
	assert.InDelta(t, 1.0, gotA[0], 1e-12, "convolved a[0] should be unity")
}

// TestKWeightImpulseResponse feeds a unit impulse through a mono filter
// and checks the first output sample equals b0 exactly: with zero
// initial state, v[0] = impulse = 1 and y = b0·1, so the leading tap is
// the numerator lead coefficient. Subsequent taps must stay finite.
func TestKWeightImpulseResponse(t *testing.T) {
	f := NewKFilter(48000, 1)
	b := f.B

	const n = 256
	src := make([]float64, n)
	src[0] = 1.0
	dst := make([]float64, n)
	f.FilterInto(dst, src)

	assert.Equal(t, b[0], dst[0], "impulse leading sample must equal b0 exactly")
	for i, v := range dst {
		require.False(t, math.IsNaN(v) || math.IsInf(v, 0), "non-finite impulse response at %d", i)
	}
}

// TestKWeightLinearity exercises the two defining properties of a linear
// filter: homogeneity (scaling the input scales the output) and
// additivity (the response to a sum is the sum of the responses).
func TestKWeightLinearity(t *testing.T) {
	const (
		sampleRate = 48000
		n          = 4000
	)
	x := make([]float64, n)
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		x[i] = 0.4 * math.Sin(2*math.Pi*440*float64(i)/sampleRate)
		y[i] = 0.3 * math.Sin(2*math.Pi*1000*float64(i)/sampleRate)
	}

	filt := func(src []float64) []float64 {
		dst := make([]float64, len(src))
		NewKFilter(sampleRate, 1).FilterInto(dst, src)
		return dst
	}

	// Homogeneity with a power-of-two scale is exact in IEEE-754 (it only
	// bumps exponents), and the filter is linear, so the two outputs must
	// be bit-identical.
	scaled := make([]float64, n)
	for i := range x {
		scaled[i] = 4.0 * x[i]
	}
	fx := filt(x)
	fScaled := filt(scaled)
	for i := range fx {
		assert.Equal(t, 4.0*fx[i], fScaled[i], "homogeneity broken at %d", i)
	}

	// Additivity holds only up to floating-point rounding, so compare
	// with a small relative tolerance.
	sum := make([]float64, n)
	for i := range x {
		sum[i] = x[i] + y[i]
	}
	fy := filt(y)
	fSum := filt(sum)
	for i := range fSum {
		assert.InDelta(t, fx[i]+fy[i], fSum[i], 1e-9, "additivity broken at %d", i)
	}
}

// TestKWeightChannelIndependence proves that per-channel state does not
// cross-contaminate: a silent channel stays exactly silent while its
// neighbour carries a signal, and the active channel's output is
// bit-identical to a standalone mono filter fed the same samples.
func TestKWeightChannelIndependence(t *testing.T) {
	const (
		sampleRate = 48000
		frames     = 3000
	)
	// Interleaved stereo: channel 0 carries a tone, channel 1 is silent.
	stereo := make([]float64, frames*2)
	mono := make([]float64, frames)
	for i := 0; i < frames; i++ {
		v := 0.6 * math.Sin(2*math.Pi*523.25*float64(i)/sampleRate)
		stereo[i*2+0] = v
		stereo[i*2+1] = 0.0
		mono[i] = v
	}

	stereoOut := make([]float64, frames*2)
	NewKFilter(sampleRate, 2).FilterInto(stereoOut, stereo)

	monoOut := make([]float64, frames)
	NewKFilter(sampleRate, 1).FilterInto(monoOut, mono)

	for i := 0; i < frames; i++ {
		assert.Equal(t, 0.0, stereoOut[i*2+1], "silent channel leaked signal at frame %d", i)
		assert.Equal(t, monoOut[i], stereoOut[i*2+0], "active channel diverged from mono at frame %d", i)
	}
}

// TestKWeightReset returns the filter to its just-constructed state so a
// second run over the same input reproduces the first bit-for-bit.
func TestKWeightReset(t *testing.T) {
	f := NewKFilter(44100, 1)
	const n = 512
	src := make([]float64, n)
	for i := range src {
		src[i] = 0.5 * math.Sin(2*math.Pi*300*float64(i)/44100)
	}

	first := make([]float64, n)
	f.FilterInto(first, src)

	f.Reset()
	second := make([]float64, n)
	f.FilterInto(second, src)

	assert.Equal(t, first, second, "reset did not restore initial filter state")
}
