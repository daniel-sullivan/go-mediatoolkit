// Package buffers provides lock-free sample buffers for streaming
// audio pipelines.
//
// All buffers hold float64 samples in whatever layout the caller
// chooses (mono stream, interleaved stereo, planar single-channel);
// they do not understand channel counts or sample rates and will not
// enforce frame alignment. Callers writing interleaved audio should
// push and pop sizes that are multiples of the channel count.
//
// Ring is a single-producer/single-consumer queue synchronised through
// atomic head and tail indices. It is not safe against multiple
// concurrent producers or consumers; one goroutine writes, one reads.
package buffers

import "sync/atomic"

// Ring is an SPSC ring buffer of float64 samples backed by a
// power-of-two-sized slice so index arithmetic is a mask instead of a
// modulus. It is the primitive used to bridge audio callbacks across
// threads without locks.
type Ring struct {
	buf  []float64
	mask uint64
	head atomic.Uint64 // next write index
	tail atomic.Uint64 // next read index
}

// NewRing allocates a Ring with capacity at least n samples. The
// actual capacity is rounded up to the next power of two; call Cap to
// see the effective value. n must be > 0.
func NewRing(n int) *Ring {
	if n <= 0 {
		panic("buffers: ring capacity must be positive")
	}
	size := uint64(1)
	for size < uint64(n) {
		size <<= 1
	}
	return &Ring{
		buf:  make([]float64, size),
		mask: size - 1,
	}
}

// Cap returns the effective capacity of the ring.
func (r *Ring) Cap() int { return len(r.buf) }

// Len returns the number of samples currently buffered. The value is
// a snapshot: it may change as soon as the call returns if the other
// side is active.
func (r *Ring) Len() int {
	return int(r.head.Load() - r.tail.Load())
}

// Write copies up to len(src) samples into the ring and returns the
// number actually written. Samples that do not fit are discarded —
// callers that care should compare the return value against len(src).
// Safe to call concurrently with Read but not with another Write.
func (r *Ring) Write(src []float64) int {
	head := r.head.Load()
	tail := r.tail.Load()
	free := (r.mask + 1) - (head - tail)
	n := uint64(len(src))
	if n > free {
		n = free
	}
	for i := uint64(0); i < n; i++ {
		r.buf[(head+i)&r.mask] = src[i]
	}
	r.head.Store(head + n)
	return int(n)
}

// Read copies up to len(dst) samples out of the ring and returns the
// number actually read. dst[n:] is not touched. A caller that needs
// silence-on-underrun (typical for an audio output callback) should
// zero the tail of dst themselves. Safe to call concurrently with
// Write but not with another Read.
func (r *Ring) Read(dst []float64) int {
	head := r.head.Load()
	tail := r.tail.Load()
	avail := head - tail
	n := uint64(len(dst))
	if n > avail {
		n = avail
	}
	for i := uint64(0); i < n; i++ {
		dst[i] = r.buf[(tail+i)&r.mask]
	}
	r.tail.Store(tail + n)
	return int(n)
}

// Reset discards all buffered samples. Must not be called while
// either Read or Write is in flight — there is no synchronisation
// between a reset and the atomic index updates on the hot paths.
func (r *Ring) Reset() {
	r.head.Store(0)
	r.tail.Store(0)
}
