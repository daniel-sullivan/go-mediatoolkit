package timeline

import (
	"io"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// EffectSource wraps a Source with an ordered chain of
// mutations.Processor effects. Each Pull first reads n samples from
// the inner source, then runs every processor over the filled
// [0:n] slice in declaration order before returning to the caller.
//
// Optionally, WithTail extends Pull after the inner source exhausts
// so that tail-carrying effects (echo, reverb) can decay cleanly —
// necessary for offline/non-realtime rendering where the caller
// wants the full decayed signal, not the signal truncated at the
// original source's end.
//
// EffectSource preserves the wrapped source's SampleRate, Channels,
// and Live properties. Duration is the inner source's duration plus
// any tail configured via WithTail (when both are finite). A single
// EffectSource should not be shared across independent streams —
// processors hold state.
type EffectSource struct {
	src        Source
	processors []mutations.Processor

	tailTotalFrames int64
	tailRemaining   int64
	srcEOF          bool
}

// NewEffectSource wraps src with the given processor chain. A nil or
// empty processor list returns a source that behaves identically to
// src (a thin pass-through).
func NewEffectSource(src Source, processors ...mutations.Processor) *EffectSource {
	return &EffectSource{src: src, processors: processors}
}

// WithTail configures Pull to continue emitting tail samples after
// the wrapped source exhausts. tail is the duration of zero-input
// frames that will be pushed through the processor chain before
// EffectSource itself returns io.EOF.
//
// Use this when rendering effects over a finite clip to preserve
// echo/reverb decay beyond the source boundary. A non-positive tail
// is a no-op (matching the default behaviour).
//
// Returns the receiver for chaining. Must be called before the first
// Pull; changing the tail mid-stream has undefined behaviour.
func (e *EffectSource) WithTail(tail time.Duration) *EffectSource {
	if tail <= 0 {
		return e
	}
	e.tailTotalFrames = mutations.DurationToFrames(tail, e.src.SampleRate())
	return e
}

// Pull reads from the wrapped source, runs each processor on the
// filled portion, and — once the source has exhausted and a tail
// was configured — continues emitting zero-input silence through
// the chain until the tail is drained.
func (e *EffectSource) Pull(dst []float64) (int, error) {
	if !e.srcEOF {
		n, err := e.src.Pull(dst)
		if n > 0 {
			for _, p := range e.processors {
				p.Process(dst[:n])
			}
		}
		if err != io.EOF {
			return n, err
		}
		e.srcEOF = true
		e.tailRemaining = e.tailTotalFrames
		// Inner source gave us a partial read — fill the rest with
		// tail silence so the caller gets a seamless transition.
		if n < len(dst) && e.tailRemaining > 0 {
			extra := e.pullTail(dst[n:])
			n += extra
		}
		if e.tailRemaining > 0 {
			return n, nil
		}
		return n, io.EOF
	}

	if e.tailRemaining <= 0 {
		return 0, io.EOF
	}
	n := e.pullTail(dst)
	if e.tailRemaining > 0 {
		return n, nil
	}
	return n, io.EOF
}

// pullTail fills dst with zero-input samples run through the
// processor chain, updating tailRemaining. Returns the number of
// samples written (≤ len(dst)).
func (e *EffectSource) pullTail(dst []float64) int {
	channels := e.src.Channels()
	availSamples := e.tailRemaining * int64(channels)
	n := int64(len(dst))
	if n > availSamples {
		n = availSamples
	}
	// Zero the slice — processors receive silence as input.
	for i := int64(0); i < n; i++ {
		dst[i] = 0
	}
	for _, p := range e.processors {
		p.Process(dst[:n])
	}
	e.tailRemaining -= n / int64(channels)
	return int(n)
}

// Reset clears every processor's internal state and rewinds the
// tail. Useful when the underlying source is restarted (new
// recording session, loop wrap).
func (e *EffectSource) Reset() {
	for _, p := range e.processors {
		p.Reset()
	}
	e.srcEOF = false
	e.tailRemaining = 0
}

func (e *EffectSource) SampleRate() int { return e.src.SampleRate() }
func (e *EffectSource) Channels() int   { return e.src.Channels() }
func (e *EffectSource) Live() bool      { return e.src.Live() }

// Duration is the inner source's duration plus any configured tail.
// Returns -1 when the inner source is indefinite, matching Source
// convention; tails only compose meaningfully with finite sources.
func (e *EffectSource) Duration() time.Duration {
	d := e.src.Duration()
	if d < 0 || e.tailTotalFrames <= 0 {
		return d
	}
	return d + mutations.FramesToDuration(e.tailTotalFrames, e.src.SampleRate())
}
