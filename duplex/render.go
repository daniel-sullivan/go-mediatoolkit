package duplex

import (
	"math"
	"sync"

	"github.com/daniel-sullivan/go-mediatoolkit/buffers"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// jitterBuffer decouples arbitrarily-timed, arbitrarily-sized render
// feeds (FeedChunk, any goroutine) from the paced 10 ms consumer (the
// audio goroutine's per-tick read). It slices incoming audio into
// whole engine frames, records chunk seams positionally for the
// equal-power crossfade, and answers underruns with silence. It grows
// without bound to absorb feeds that arrive faster than real time —
// the producer is trusted (it is the session's own synthesis stream,
// not a network peer); ClearPending is the pressure valve.
//
// All methods lock; frame storage is recycled through a slab so a
// steady feed/read cycle stops allocating once warm.
type jitterBuffer struct {
	mu sync.Mutex

	frameSamples int
	channels     int

	// fadeSamples is the crossfade window in interleaved samples
	// (whole frames' worth, at most one engine frame); 0 disables
	// seam fading.
	fadeSamples int

	// chunker accumulates fed samples and emits whole frames.
	chunker *mutations.StreamChunker

	// frames is the FIFO of ready 10 ms frames, oldest first.
	frames buffers.Queue[jitterFrame]

	// pendingSeam marks that the next frame sliced begins a new
	// logical chunk: it is transferred onto that frame (seam), so the
	// blend lands on the actual chunk head no matter how far behind
	// the paced reader is.
	pendingSeam bool

	// tail holds the last fadeSamples samples of the most recent
	// voiced frame read — the outgoing side of the next seam blend.
	tail []float64

	slab buffers.Slab
}

// jitterFrame is one queued frame; seam marks it as the first frame
// of a new logical chunk (blend its head against tail when read).
type jitterFrame struct {
	samples []float64
	seam    bool
}

// newJitterBuffer constructs an empty buffer emitting frameSamples-
// sample interleaved frames of the given channel count, blending
// seams over fadeSamples samples (0 disables fading).
func newJitterBuffer(frameSamples, channels, fadeSamples int) *jitterBuffer {
	return &jitterBuffer{
		frameSamples: frameSamples,
		channels:     channels,
		fadeSamples:  fadeSamples,
		chunker:      mutations.NewStreamChunker(frameSamples),
	}
}

// feed appends pcm, enqueueing every completed frame; the remainder
// stays buffered for the next feed to complete.
func (j *jitterBuffer) feed(pcm []float64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	// enqueueFrame never fails, so neither can Write.
	_, _ = j.chunker.Write(pcm, j.enqueueFrame)
}

// markBoundary flushes a pending partial frame (zero-padded to a
// whole frame — the next chunk is an independent generation, so its
// audio must not splice mid-frame onto this one's remainder) and
// arms the seam for the next frame sliced.
func (j *jitterBuffer) markBoundary() {
	j.mu.Lock()
	defer j.mu.Unlock()
	_ = j.chunker.Flush(j.enqueueFrame)
	j.pendingSeam = true
}

// enqueueFrame copies one completed frame into a recycled slot and
// queues it, transferring an armed seam onto it. Callers must hold
// mu. Always returns nil (the error is the chunker callback shape).
func (j *jitterBuffer) enqueueFrame(chunk []float64) error {
	j.frames.Push(jitterFrame{
		samples: append(j.slab.Take(), chunk...),
		seam:    j.pendingSeam,
	})
	j.pendingSeam = false
	return nil
}

// clear drops all queued-but-unread audio, the pending partial frame,
// the armed seam, and the crossfade tail (audio from before a
// barge-in must not blend into what plays after it).
func (j *jitterBuffer) clear() {
	j.mu.Lock()
	defer j.mu.Unlock()
	for {
		f, ok := j.frames.Pop()
		if !ok {
			break
		}
		j.slab.Put(f.samples)
	}
	j.chunker.Reset()
	j.pendingSeam = false
	j.tail = j.tail[:0]
}

// read fills dst (one engine frame) with the next queued frame,
// blending a seam-marked frame's head equal-power against the stored
// tail, and reports whether voice audio was read. On underrun dst is
// zeroed and the tail is left untouched (a seam frame arriving after
// a gap still blends against the last audio that actually played).
func (j *jitterBuffer) read(dst []float64) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	f, ok := j.frames.Pop()
	if !ok {
		for i := range dst {
			dst[i] = 0
		}
		return false
	}
	copy(dst, f.samples)
	j.slab.Put(f.samples)
	if f.seam && len(j.tail) > 0 && j.fadeSamples > 0 {
		equalPowerBlend(j.tail, dst, j.channels)
	}

	// Retain this frame's tail as the outgoing side of the next seam.
	n := min(j.fadeSamples, len(dst))
	j.tail = append(j.tail[:0], dst[len(dst)-n:]...)
	return true
}

// equalPowerBlend crossfades the head of curr against prevTail in
// place: out = prevTail·cos(θ) + curr·sin(θ) with θ swept over
// (0, π/2) at per-frame granularity (all channels of one frame share
// a θ), sampled at frame midpoints so neither endpoint gain is
// exactly 0 or 1. The window is the shorter of the two slices, in
// whole frames. Constant-power in the blend region for uncorrelated
// signals — the sum can exceed [-1, 1] transiently; the render path's
// final clamp bounds it.
func equalPowerBlend(prevTail, curr []float64, channels int) {
	n := min(len(prevTail), len(curr)) / channels
	if n <= 0 {
		return
	}
	for f := 0; f < n; f++ {
		theta := (float64(f) + 0.5) / float64(n) * math.Pi / 2
		gOut, gIn := math.Cos(theta), math.Sin(theta)
		for c := 0; c < channels; c++ {
			i := f*channels + c
			curr[i] = prevTail[i]*gOut + curr[i]*gIn
		}
	}
}

// ambientBed loops a fixed pcm asset under the voice, sample-precise
// (the loop may wrap mid-frame). Owned by the audio goroutine; the
// asset and gain are fixed at construction.
type ambientBed struct {
	pcm  []float64
	gain float64
	pos  int
}

// mixInto adds the next frame's worth of the looping bed into frame.
func (a *ambientBed) mixInto(frame []float64) {
	for i := range frame {
		frame[i] += a.pcm[a.pos] * a.gain
		a.pos++
		if a.pos == len(a.pcm) {
			a.pos = 0
		}
	}
}
