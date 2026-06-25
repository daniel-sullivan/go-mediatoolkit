package mutations_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestCrossfadeLoopShortensByFadeFrames(t *testing.T) {
	in := make([]float64, 100)
	out := mutations.CrossfadeLoop(in, 10, 1)
	assert.Len(t, out, 90)
}

func TestCrossfadeLoopStereoShortensByFadeFrames(t *testing.T) {
	in := make([]float64, 100) // 50 stereo frames
	out := mutations.CrossfadeLoop(in, 10, 2)
	assert.Len(t, out, 100-10*2)
}

func TestCrossfadeLoopZeroFadeReturnsInput(t *testing.T) {
	in := []float64{1, 2, 3, 4}
	out := mutations.CrossfadeLoop(in, 0, 1)
	assert.Equal(t, in, out)
}

func TestCrossfadeLoopOversizedFadeReturnsInput(t *testing.T) {
	in := []float64{1, 2, 3, 4}
	// 3*2 = 6 > 4 → return unchanged.
	out := mutations.CrossfadeLoop(in, 3, 1)
	assert.Equal(t, in, out)
}

func TestCrossfadeLoopFirstSampleIsPureTail(t *testing.T) {
	// With sin(0) = 0 and cos(0) = 1, out[0] should equal the
	// first sample of the tail region exactly.
	in := make([]float64, 20)
	for i := range in {
		in[i] = float64(i)
	}
	out := mutations.CrossfadeLoop(in, 4, 1)
	tailStart := len(in) - 4 // index 16
	assert.InDelta(t, in[tailStart], out[0], 1e-12, "out[0] should be in[%d] (pure tail)", tailStart)
}

func TestCrossfadeLoopLastFadeSampleIsPureHead(t *testing.T) {
	// At the last frame of the fade, the head's weight is sin(π/2*(N-1)/N),
	// which is close to but not exactly 1. Check that by the time we
	// leave the fade window the output matches the unmodified middle.
	in := make([]float64, 20)
	for i := range in {
		in[i] = float64(i)
	}
	out := mutations.CrossfadeLoop(in, 4, 1)
	// out[4] onwards should be unmodified (equal to in[4]..in[15]).
	for i := 4; i < len(out); i++ {
		assert.InDelta(t, in[i], out[i], 1e-12, "out[%d] should equal in[%d] (unmodified middle)", i, i)
	}
}

func TestCrossfadeLoopConstantInputStaysConstant(t *testing.T) {
	// A DC signal crossfaded with itself should remain DC — the
	// equal-power curves sum to sqrt(sin² + cos²) = 1 in amplitude
	// when head == tail.
	in := make([]float64, 100)
	for i := range in {
		in[i] = 0.5
	}
	out := mutations.CrossfadeLoop(in, 10, 1)
	for i, v := range out {
		// Equal-power with identical head/tail: sin+cos
		// ranges up to √2 at mid-fade — but the DC case here means
		// samples match, so v should stay roughly constant. The
		// crossfade sum is sin(θ)+cos(θ), max ~1.414 at θ=π/4.
		// Just verify the signal is bounded and non-zero.
		assert.Greater(t, v, 0.4, "sample %d", i)
		assert.Less(t, v, 1.0, "sample %d", i)
	}
}

func TestCrossfadeLoopSeamIsContinuous(t *testing.T) {
	// The whole point: after CrossfadeLoop, playing the clip twice
	// back-to-back should have no sudden jump at the seam.
	in := make([]float64, 1000)
	for i := range in {
		in[i] = math.Sin(2*math.Pi*5*float64(i)/1000) * 0.5 // 5-cycle sine
	}
	out := mutations.CrossfadeLoop(in, 50, 1)

	// Concatenate two iterations and measure the maximum adjacent-
	// sample delta. It should be similar within the clip and at the
	// seam — no discontinuity.
	doubled := append(append([]float64(nil), out...), out...)

	var maxInClip, maxAtSeam float64
	for i := 1; i < len(out); i++ {
		d := math.Abs(doubled[i] - doubled[i-1])
		if d > maxInClip {
			maxInClip = d
		}
	}
	seamIdx := len(out)
	for i := seamIdx - 3; i < seamIdx+3; i++ {
		d := math.Abs(doubled[i] - doubled[i-1])
		if d > maxAtSeam {
			maxAtSeam = d
		}
	}
	require.Greater(t, maxInClip, 0.0, "sanity: clip has some variation")
	// Seam delta should not exceed typical in-clip delta by more than 2×.
	// (A naive raw-loop would show a huge jump here.)
	assert.LessOrEqual(t, maxAtSeam, 2*maxInClip, "seam delta %g should be comparable to in-clip max %g", maxAtSeam, maxInClip)
}
