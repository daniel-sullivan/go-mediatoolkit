package buffers

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRingRoundsCapacity(t *testing.T) {
	cases := []struct {
		in, want int
	}{
		{1, 1},
		{2, 2},
		{3, 4},
		{5, 8},
		{1000, 1024},
	}
	for _, tc := range cases {
		r := NewRing(tc.in)
		assert.Equal(t, tc.want, r.Cap(), "n=%d", tc.in)
	}
}

func TestNewRingPanicsOnNonPositive(t *testing.T) {
	assert.Panics(t, func() { NewRing(0) })
	assert.Panics(t, func() { NewRing(-1) })
}

func TestWriteReadRoundtrip(t *testing.T) {
	r := NewRing(8)
	in := []float64{1, 2, 3, 4, 5}

	n := r.Write(in)
	require.Equal(t, 5, n)
	assert.Equal(t, 5, r.Len())

	out := make([]float64, 5)
	n = r.Read(out)
	require.Equal(t, 5, n)
	assert.Equal(t, in, out)
	assert.Equal(t, 0, r.Len())
}

func TestReadOnEmptyReturnsZero(t *testing.T) {
	r := NewRing(4)
	out := []float64{9, 9, 9, 9}
	n := r.Read(out)
	assert.Equal(t, 0, n)
	// Read must not touch dst[n:].
	assert.Equal(t, []float64{9, 9, 9, 9}, out)
}

func TestWriteOverflowReturnsPartial(t *testing.T) {
	r := NewRing(4)
	in := []float64{1, 2, 3, 4, 5, 6}

	n := r.Write(in)
	assert.Equal(t, 4, n)
	assert.Equal(t, 4, r.Len())

	// Second write on a full buffer returns 0 without error.
	n = r.Write(in)
	assert.Equal(t, 0, n)
}

func TestWraparound(t *testing.T) {
	r := NewRing(4) // cap=4

	// Pre-fill and drain to move head and tail off zero.
	require.Equal(t, 3, r.Write([]float64{1, 2, 3}))
	out := make([]float64, 3)
	require.Equal(t, 3, r.Read(out))

	// Now head=tail=3. Write 4 samples — this crosses the mask.
	n := r.Write([]float64{10, 20, 30, 40})
	require.Equal(t, 4, n)

	got := make([]float64, 4)
	require.Equal(t, 4, r.Read(got))
	assert.Equal(t, []float64{10, 20, 30, 40}, got)
}

func TestPartialReadConsumesOnlyRequested(t *testing.T) {
	r := NewRing(8)
	require.Equal(t, 6, r.Write([]float64{1, 2, 3, 4, 5, 6}))

	small := make([]float64, 3)
	require.Equal(t, 3, r.Read(small))
	assert.Equal(t, []float64{1, 2, 3}, small)
	assert.Equal(t, 3, r.Len())

	rest := make([]float64, 5)
	n := r.Read(rest)
	require.Equal(t, 3, n)
	assert.Equal(t, []float64{4, 5, 6}, rest[:3])
	// dst[n:] untouched.
	assert.Equal(t, []float64{0, 0}, rest[3:])
}

func TestResetClearsIndices(t *testing.T) {
	r := NewRing(4)
	_ = r.Write([]float64{1, 2, 3})
	r.Reset()
	assert.Equal(t, 0, r.Len())
	out := make([]float64, 3)
	assert.Equal(t, 0, r.Read(out))
}

// TestConcurrentSPSC stresses the one-producer/one-consumer contract.
// The producer writes a monotonically-increasing sequence; the
// consumer verifies every sample it receives matches the expected
// value for its position. Any lost or duplicated sample fails the
// test.
func TestConcurrentSPSC(t *testing.T) {
	const total = 100_000
	r := NewRing(256)

	var wg sync.WaitGroup
	wg.Add(2)

	var mismatchAt atomic.Int64
	mismatchAt.Store(-1)

	go func() {
		defer wg.Done()
		chunk := make([]float64, 17)
		i := 0
		for i < total {
			for j := range chunk {
				chunk[j] = float64(i + j)
			}
			written := r.Write(chunk[:min(len(chunk), total-i)])
			i += written
			if written == 0 {
				runtime.Gosched()
			}
		}
	}()

	go func() {
		defer wg.Done()
		chunk := make([]float64, 13)
		i := 0
		for i < total {
			n := r.Read(chunk)
			if n == 0 {
				runtime.Gosched()
				continue
			}
			for j := 0; j < n; j++ {
				if chunk[j] != float64(i+j) {
					mismatchAt.CompareAndSwap(-1, int64(i+j))
					return
				}
			}
			i += n
		}
	}()

	wg.Wait()
	if m := mismatchAt.Load(); m >= 0 {
		t.Fatalf("sequence mismatch at sample %d", m)
	}
}
