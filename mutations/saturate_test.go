package mutations_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-mediatoolkit/mutations"
)

func TestSoftSaturatePassthroughBelowThreshold(t *testing.T) {
	for _, x := range []float64{0, 0.1, -0.1, 0.5, -0.5, mutations.SoftSaturationThreshold, -mutations.SoftSaturationThreshold} {
		assert.Equal(t, x, mutations.SoftSaturate(x), "x=%g", x)
	}
}

func TestSoftSaturateSymmetric(t *testing.T) {
	for _, x := range []float64{0.9, 1.0, 1.5, 3.0, 10.0} {
		assert.InDelta(t, -mutations.SoftSaturate(x), mutations.SoftSaturate(-x), 1e-12, "x=%g", x)
	}
}

func TestSoftSaturateBounded(t *testing.T) {
	for _, x := range []float64{1.0, 2.0, 10.0, 1e6} {
		y := mutations.SoftSaturate(x)
		assert.GreaterOrEqual(t, y, mutations.SoftSaturationThreshold, "x=%g", x)
		assert.LessOrEqual(t, y, 1.0, "x=%g", x)
	}
}

func TestSoftSaturateMonotonic(t *testing.T) {
	prev := math.Inf(-1)
	for x := -3.0; x <= 3.0; x += 0.01 {
		y := mutations.SoftSaturate(x)
		assert.GreaterOrEqual(t, y, prev, "x=%g", x)
		prev = y
	}
}

func TestSoftSaturateContinuousAtThreshold(t *testing.T) {
	eps := 1e-6
	assert.InDelta(t,
		mutations.SoftSaturate(mutations.SoftSaturationThreshold-eps),
		mutations.SoftSaturate(mutations.SoftSaturationThreshold+eps),
		1e-5)
}

func TestHardClip(t *testing.T) {
	assert.Equal(t, 1.0, mutations.HardClip(2.0))
	assert.Equal(t, -1.0, mutations.HardClip(-2.0))
	assert.Equal(t, 0.5, mutations.HardClip(0.5))
	assert.Equal(t, -0.5, mutations.HardClip(-0.5))
	assert.Equal(t, 1.0, mutations.HardClip(1.0))
}

func TestTanhSaturate(t *testing.T) {
	assert.Equal(t, 0.0, mutations.TanhSaturate(0))
	assert.InDelta(t, 0.76159, mutations.TanhSaturate(1), 1e-4)
	assert.InDelta(t, -0.76159, mutations.TanhSaturate(-1), 1e-4)
	// Asymptotes to 1 for large x.
	assert.InDelta(t, 1.0, mutations.TanhSaturate(100), 1e-9)
}

func TestApplySaturatorNilNoOp(t *testing.T) {
	samples := []float64{0.5, 1.5, -1.5}
	orig := append([]float64(nil), samples...)
	mutations.ApplySaturator(samples, nil)
	assert.Equal(t, orig, samples)
}

func TestApplySaturatorInPlace(t *testing.T) {
	samples := []float64{2.0, -2.0, 0.5}
	mutations.ApplySaturator(samples, mutations.HardClip)
	assert.Equal(t, []float64{1.0, -1.0, 0.5}, samples)
}

func TestCustomSaturator(t *testing.T) {
	// Demonstrate that Saturator is a user-extensible function type.
	doubled := func(x float64) float64 { return x * 2 }
	samples := []float64{0.1, 0.2, 0.3}
	mutations.ApplySaturator(samples, doubled)
	assert.Equal(t, []float64{0.2, 0.4, 0.6}, samples)
}
