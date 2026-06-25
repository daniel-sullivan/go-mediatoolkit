package mutations_test

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInterleave(t *testing.T) {
	got := mutations.Interleave([][]float64{{1, 2, 3}, {4, 5, 6}})
	assert.Equal(t, []float64{1, 4, 2, 5, 3, 6}, got)
}

func TestInterleaveThreeChannels(t *testing.T) {
	got := mutations.Interleave([][]float64{{1, 4}, {2, 5}, {3, 6}})
	assert.Equal(t, []float64{1, 2, 3, 4, 5, 6}, got)
}

func TestInterleaveMono(t *testing.T) {
	got := mutations.Interleave([][]float64{{1, 2, 3}})
	assert.Equal(t, []float64{1, 2, 3}, got)
}

func TestInterleaveEmpty(t *testing.T) {
	assert.Nil(t, mutations.Interleave(nil))
	assert.Nil(t, mutations.Interleave([][]float64{}))
}

func TestInterleaveUnequalLengths(t *testing.T) {
	got := mutations.Interleave([][]float64{{1, 2, 3, 4}, {5, 6}})
	assert.Equal(t, []float64{1, 5, 2, 6, 3, 0, 4, 0}, got)
}

func TestInterleaveUnequalThreeChannels(t *testing.T) {
	got := mutations.Interleave([][]float64{{1, 2, 3}, {4}, {5, 6}})
	assert.Equal(t, []float64{1, 4, 5, 2, 0, 6, 3, 0, 0}, got)
}

func TestDeinterleave(t *testing.T) {
	got := mutations.Deinterleave([]float64{1, 4, 2, 5, 3, 6}, 2)
	require.Len(t, got, 2)
	assert.Equal(t, []float64{1, 2, 3}, got[0])
	assert.Equal(t, []float64{4, 5, 6}, got[1])
}

func TestDeinterleaveEmpty(t *testing.T) {
	assert.Nil(t, mutations.Deinterleave(nil, 2))
	assert.Nil(t, mutations.Deinterleave([]float64{1, 2}, 0))
}

func TestRoundTrip(t *testing.T) {
	left := []float64{0.1, 0.2, 0.3, 0.4}
	right := []float64{0.5, 0.6, 0.7, 0.8}

	got := mutations.Deinterleave(mutations.Interleave([][]float64{left, right}), 2)
	require.Len(t, got, 2)
	assert.Equal(t, left, got[0])
	assert.Equal(t, right, got[1])
}
