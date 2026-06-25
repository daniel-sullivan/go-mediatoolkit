package mutations_test

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
)

func TestTrimStart(t *testing.T) {
	got := mutations.TrimSilence([]float64{0, 0, 0, 1, 2, 3, 0}, mutations.TrimStart, 0)
	assert.Equal(t, []float64{1, 2, 3, 0}, got)
}

func TestTrimEnd(t *testing.T) {
	got := mutations.TrimSilence([]float64{0, 1, 2, 3, 0, 0}, mutations.TrimEnd, 0)
	assert.Equal(t, []float64{0, 1, 2, 3}, got)
}

func TestTrimBoth(t *testing.T) {
	got := mutations.TrimSilence([]float64{0, 0, 1, 2, 3, 0, 0}, mutations.TrimBoth, 0)
	assert.Equal(t, []float64{1, 2, 3}, got)
}

func TestTrimSilenceWithThreshold(t *testing.T) {
	got := mutations.TrimSilence([]float64{0.001, 0.005, 0.1, 0.5, 0.9, 0.01, 0.002}, mutations.TrimBoth, 0.01)
	assert.Equal(t, []float64{0.1, 0.5, 0.9}, got)
}

func TestTrimSilenceAll(t *testing.T) {
	got := mutations.TrimSilenceAll([]float64{0, 1, 0, 2, 0, 0, 3, 0}, 0)
	assert.Equal(t, []float64{1, 2, 3}, got)
}

func TestTrimSilenceAllWithThreshold(t *testing.T) {
	got := mutations.TrimSilenceAll([]float64{0.001, 0.5, 0.002, 0.8, 0.003}, 0.01)
	assert.Equal(t, []float64{0.5, 0.8}, got)
}

func TestTrimAllSilent(t *testing.T) {
	got := mutations.TrimSilence([]float64{0, 0, 0}, mutations.TrimBoth, 0)
	assert.Empty(t, got)
}

func TestTrimNoSilence(t *testing.T) {
	got := mutations.TrimSilence([]float64{1, 2, 3}, mutations.TrimBoth, 0)
	assert.Equal(t, []float64{1, 2, 3}, got)
}

func TestTrimEmpty(t *testing.T) {
	assert.Nil(t, mutations.TrimSilence(nil, mutations.TrimBoth, 0))
}

func TestTrimNegativeValues(t *testing.T) {
	got := mutations.TrimSilence([]float64{0, -0.001, -0.5, 0.5, 0.001, 0}, mutations.TrimBoth, 0.01)
	assert.Equal(t, []float64{-0.5, 0.5}, got)
}

func TestTrimCustomFunc(t *testing.T) {
	got := mutations.Trim([]float64{-1, -0.5, 0, 0.5, 1}, mutations.TrimStart, func(s float64) bool { return s < 0 })
	assert.Equal(t, []float64{0, 0.5, 1}, got)
}

func TestTrimAllCustomFunc(t *testing.T) {
	got := mutations.TrimAll([]float64{1, 2, 3, 4, 5, 6}, func(s float64) bool { return math.Mod(s, 2) == 0 })
	assert.Equal(t, []float64{1, 3, 5}, got)
}

func TestTrimSharesBackingArray(t *testing.T) {
	buf := []float64{0, 0, 1, 2, 0, 0}
	got := mutations.TrimSilence(buf, mutations.TrimBoth, 0)
	got[0] = 99
	assert.Equal(t, 99.0, buf[2], "should share backing array")
}
