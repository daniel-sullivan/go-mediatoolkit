package mutations_test

import (
	"fmt"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunkEven(t *testing.T) {
	got := mutations.Chunk([]float64{1, 2, 3, 4, 5, 6}, 2)
	require.Len(t, got, 3)
	assert.Equal(t, []float64{1, 2}, got[0])
	assert.Equal(t, []float64{3, 4}, got[1])
	assert.Equal(t, []float64{5, 6}, got[2])
}

func TestChunkRemainder(t *testing.T) {
	got := mutations.Chunk([]float64{1, 2, 3, 4, 5}, 2)
	require.Len(t, got, 3)
	assert.Equal(t, []float64{1, 2}, got[0])
	assert.Equal(t, []float64{3, 4}, got[1])
	assert.Equal(t, []float64{5}, got[2])
}

func TestChunkSingleChunk(t *testing.T) {
	buf := []float64{1, 2, 3}
	got := mutations.Chunk(buf, 10)
	require.Len(t, got, 1)
	assert.Equal(t, buf, got[0])
}

func TestChunkEmpty(t *testing.T) {
	assert.Nil(t, mutations.Chunk(nil, 4))
	assert.Nil(t, mutations.Chunk([]float64{1}, 0))
}

func TestChunkSharesBackingArray(t *testing.T) {
	buf := []float64{1, 2, 3, 4}
	got := mutations.Chunk(buf, 2)
	got[0][0] = 99
	assert.Equal(t, 99.0, buf[0], "chunk should share backing array")
}

func TestChunkFunc(t *testing.T) {
	buf := []float64{1, 2, 3, 4, 5}
	var chunks [][]float64
	var lastFlags []bool

	err := mutations.ChunkFunc(buf, 2, func(chunk []float64, last bool) error {
		cp := make([]float64, len(chunk))
		copy(cp, chunk)
		chunks = append(chunks, cp)
		lastFlags = append(lastFlags, last)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, chunks, 3)
	assert.Equal(t, []float64{1, 2}, chunks[0])
	assert.Equal(t, []float64{3, 4}, chunks[1])
	assert.Equal(t, []float64{5}, chunks[2])
	assert.Equal(t, []bool{false, false, true}, lastFlags)
}

func TestChunkFuncError(t *testing.T) {
	calls := 0
	err := mutations.ChunkFunc([]float64{1, 2, 3, 4}, 2, func(_ []float64, _ bool) error {
		calls++
		return fmt.Errorf("stop")
	})
	assert.Error(t, err)
	assert.Equal(t, 1, calls, "should stop on first error")
}

func TestChunkFuncEmpty(t *testing.T) {
	err := mutations.ChunkFunc(nil, 4, func(_ []float64, _ bool) error {
		t.Fatal("should not be called")
		return nil
	})
	assert.NoError(t, err)
}
