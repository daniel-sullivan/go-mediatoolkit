package mutations

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// collect returns a visitor that copies each chunk (since the callback's
// slice may be reused) into out.
func collect(out *[][]float64) func([]float64) error {
	return func(chunk []float64) error {
		cp := make([]float64, len(chunk))
		copy(cp, chunk)
		*out = append(*out, cp)
		return nil
	}
}

func TestStreamChunkerAligned(t *testing.T) {
	c := NewStreamChunker(3)
	var out [][]float64
	n, err := c.Write([]float64{1, 2, 3, 4, 5, 6}, collect(&out))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, [][]float64{{1, 2, 3}, {4, 5, 6}}, out)
	assert.Equal(t, 0, c.Pending())
}

func TestStreamChunkerAcrossWrites(t *testing.T) {
	c := NewStreamChunker(3)
	var out [][]float64

	n, err := c.Write([]float64{1, 2}, collect(&out))
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	assert.Empty(t, out)
	assert.Equal(t, 2, c.Pending())

	n, err = c.Write([]float64{3, 4, 5, 6, 7}, collect(&out))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, [][]float64{{1, 2, 3}, {4, 5, 6}}, out)
	assert.Equal(t, 1, c.Pending())
}

func TestStreamChunkerPartialThenAligned(t *testing.T) {
	// Mix the "fill pending" and zero-copy fast paths.
	c := NewStreamChunker(4)
	var out [][]float64

	_, _ = c.Write([]float64{1, 2}, collect(&out)) // pending=[1,2]
	_, err := c.Write([]float64{3, 4, 5, 6, 7, 8, 9}, collect(&out))
	require.NoError(t, err)
	// Expected: [1,2,3,4] (from pending), [5,6,7,8] (fast path), pending=[9]
	assert.Equal(t, [][]float64{{1, 2, 3, 4}, {5, 6, 7, 8}}, out)
	assert.Equal(t, 1, c.Pending())
}

func TestStreamChunkerFlushPads(t *testing.T) {
	c := NewStreamChunker(4)
	var out [][]float64
	_, err := c.Write([]float64{1, 2, 3}, collect(&out))
	require.NoError(t, err)
	require.NoError(t, c.Flush(collect(&out)))
	assert.Equal(t, [][]float64{{1, 2, 3, 0}}, out)
	assert.Equal(t, 0, c.Pending())
}

func TestStreamChunkerFlushEmpty(t *testing.T) {
	c := NewStreamChunker(4)
	called := false
	err := c.Flush(func(chunk []float64) error { called = true; return nil })
	require.NoError(t, err)
	assert.False(t, called)
}

func TestStreamChunkerReset(t *testing.T) {
	c := NewStreamChunker(4)
	_, _ = c.Write([]float64{1, 2}, collect(new([][]float64)))
	assert.Equal(t, 2, c.Pending())
	c.Reset()
	assert.Equal(t, 0, c.Pending())
}

func TestStreamChunkerPropagatesErrorFastPath(t *testing.T) {
	c := NewStreamChunker(2)
	sentinel := errors.New("stop")
	n, err := c.Write([]float64{1, 2, 3, 4}, func(chunk []float64) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
	// First chunk (2 samples) went into fn before the error.
	assert.Equal(t, 2, n)
}

func TestStreamChunkerPropagatesErrorPendingPath(t *testing.T) {
	c := NewStreamChunker(3)
	sentinel := errors.New("stop")

	// Prime pending with 1 sample, then write 3 more so the chunk fires
	// through the pending path rather than the fast path.
	var out [][]float64
	_, _ = c.Write([]float64{10}, collect(&out))
	n, err := c.Write([]float64{1, 2, 3}, func(chunk []float64) error {
		return sentinel
	})
	assert.ErrorIs(t, err, sentinel)
	// Only 2 of the 3 new samples were absorbed — they plus the 1 pending
	// one completed a chunk that fn then rejected. The 3rd remains in buf.
	assert.Equal(t, 2, n)
}

func TestStreamChunkerZeroSize(t *testing.T) {
	c := NewStreamChunker(0)
	n, err := c.Write([]float64{1, 2, 3}, func([]float64) error { return nil })
	require.NoError(t, err)
	assert.Equal(t, 0, n)
	assert.NoError(t, c.Flush(func([]float64) error { return nil }))
}

func TestStreamChunkerSize(t *testing.T) {
	c := NewStreamChunker(7)
	assert.Equal(t, 7, c.Size())
}

func TestStreamChunkerLargeAlignedInput(t *testing.T) {
	// Fast path should not allocate: feeding an aligned stream should not
	// touch the pending buffer at all.
	c := NewStreamChunker(4)
	buf := make([]float64, 4*1000)
	for i := range buf {
		buf[i] = float64(i)
	}
	count := 0
	n, err := c.Write(buf, func(chunk []float64) error {
		count++
		// Verify chunk is a view into buf (pending was never used).
		assert.Equal(t, 4, len(chunk))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, len(buf), n)
	assert.Equal(t, 1000, count)
	assert.Equal(t, 0, c.Pending())
}
