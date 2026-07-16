package buffers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueue_FIFOOrder(t *testing.T) {
	var q Queue[int]
	assert.Equal(t, 0, q.Len())
	_, ok := q.Pop()
	assert.False(t, ok, "empty queue pops nothing")
	assert.Nil(t, q.Peek())

	for i := 0; i < 5; i++ {
		q.Push(i)
	}
	assert.Equal(t, 5, q.Len())
	require.NotNil(t, q.Peek())
	assert.Equal(t, 0, *q.Peek())

	for i := 0; i < 5; i++ {
		v, ok := q.Pop()
		require.True(t, ok)
		assert.Equal(t, i, v, "strict FIFO order")
	}
	assert.Equal(t, 0, q.Len())
}

func TestQueue_InterleavedPushPopCompaction(t *testing.T) {
	// A long push/pop cycle far past any initial capacity: order must
	// survive the periodic re-anchoring and the backing array must
	// track the live contents, not the total history.
	var q Queue[int]
	next, expect := 0, 0
	for round := 0; round < 1000; round++ {
		for i := 0; i < 3; i++ {
			q.Push(next)
			next++
		}
		for i := 0; i < 2; i++ {
			v, ok := q.Pop()
			require.True(t, ok)
			require.Equal(t, expect, v)
			expect++
		}
	}
	assert.Equal(t, next-expect, q.Len())
	assert.LessOrEqual(t, cap(q.items), 4*q.Len()+8,
		"backing array must stay proportional to live contents")
}

func TestQueue_PopZeroesSlot(t *testing.T) {
	var q Queue[[]float64]
	q.Push(make([]float64, 4))
	v, ok := q.Pop()
	require.True(t, ok)
	assert.NotNil(t, v)
	// The vacated slot must not retain the payload (would pin it for
	// the backing array's lifetime).
	assert.Nil(t, q.items[:1][0])
}

func TestQueue_Reset(t *testing.T) {
	var q Queue[string]
	q.Push("a")
	q.Push("b")
	q.Reset()
	assert.Equal(t, 0, q.Len())
	_, ok := q.Pop()
	assert.False(t, ok)
	q.Push("c")
	v, ok := q.Pop()
	require.True(t, ok)
	assert.Equal(t, "c", v, "queue is reusable after Reset")
}

func TestSlab_Recycles(t *testing.T) {
	var s Slab
	assert.Nil(t, s.Take(), "empty slab takes nil (append-ready)")

	buf := append(s.Take(), 1, 2, 3)
	s.Put(buf)
	got := s.Take()
	assert.Equal(t, 0, len(got), "recycled buffer comes back empty")
	assert.Equal(t, cap(buf), cap(got), "recycled buffer keeps its capacity")
	assert.Nil(t, s.Take(), "slab is drained after one Take")
}
