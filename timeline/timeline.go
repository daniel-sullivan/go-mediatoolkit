// Package timeline schedules and plays audio clips along a monotonically
// advancing frame cursor. It is the playback-layer foundation for the
// toolkit: a Timeline consumes Cues and produces a single interleaved
// float64 stream that a mixer or device callback can read.
//
// # Core abstractions
//
// Source is the unit of playable audio. CachedClip playheads, streaming
// clips, live inputs, and Timelines themselves all implement Source, so
// they compose freely — a Timeline may be scheduled as a Cue on another
// Timeline.
//
// Cue pairs a Source with a start time (relative to the owning
// Timeline's frame 0) and an optional Transform. Timeline.Schedule
// places a Cue on the timeline's frame axis and returns a Handle for
// cancellation and completion notification.
//
// # Time and frames
//
// The public API accepts time.Duration; internal state is kept in frame
// counts at the owning Timeline's sample rate. Time is monotonic — the
// cursor only advances. A Cue scheduled at a time in the past is
// fast-forwarded into its own content (mid-clip playback); a Cue whose
// entire duration is already behind the cursor produces no samples.
//
// # Data layout
//
// All audio is interleaved float64 in [-1, 1], matching codec, devices,
// and the rest of the toolkit. Each Timeline has a fixed sample rate
// and channel count set at construction. In Phase 1, cues whose Source
// does not match the Timeline's format are rejected at Schedule time;
// automatic rate and channel adaptation arrives with the mixer package.
//
// # Concurrency
//
// Timeline.Schedule is safe to call from any goroutine. Timeline.Pull
// is expected to have a single consumer (a mixer goroutine or an output
// callback's ring filler). Handle methods are safe from any goroutine.
package timeline

import "time"

// Source is the unit of playable audio. Clips, nested timelines, and
// live inputs all implement Source.
type Source interface {
	// Pull reads up to len(dst) interleaved samples (frames * channels)
	// into dst and returns the number of samples written. When the
	// source is permanently exhausted, Pull returns io.EOF; a partial
	// read (n > 0 with io.EOF) is valid and callers must consume the
	// partial data before treating EOF as final.
	//
	// Returning n < len(dst) without io.EOF indicates a live source
	// waiting for real-time arrival — the caller should treat the
	// unfilled tail as silence for this pull and try again later.
	Pull(dst []float64) (n int, err error)

	// SampleRate reports the native sample rate of this source in Hz.
	SampleRate() int

	// Channels reports the interleaved channel count of this source.
	Channels() int

	// Duration reports the total length of this source. A negative
	// return value means indefinite (live input, looping timeline).
	Duration() time.Duration

	// Live reports whether this source produces samples in real time
	// and cannot be consumed faster than wall-clock. Live sources
	// constrain how far ahead a mixer may buffer.
	Live() bool
}

// Seekable is an optional capability a Source may implement to support
// efficient random access. Timeline calls Seek with positive
// values when scheduling a past-dated cue; callers may also Seek
// negatively to rewind. Sources without Seek fall back to discarding
// samples through Pull for the forward catch-up case.
//
// Seek here is the standard media verb — frame-denominated and
// relative — deliberately distinct from io.Seeker's byte-denominated
// whence-relative signature. go vet's stdmethods check flags the name
// mismatch; the warning is expected and may be ignored.
type Seekable interface {
	// Seek moves the source cursor by the given frame delta.
	// Positive values fast-forward; negative values rewind. Seeking
	// past the end clamps to the end and returns io.EOF (the source
	// is exhausted). Seeking past the start clamps to frame 0 and
	// returns nil.
	Seek(frames int64) error
}

// Cue places a Source on a Timeline's frame axis.
//
// Start is measured relative to the owning Timeline's cursor origin
// (frame 0); ignored by Append/AppendAudio which compute Start
// automatically.
//
// Transform shapes the cue's contribution; the zero Transform is a
// no-op pass-through.
//
// Factory is optional. When non-nil it describes how to rebuild the
// Source from scratch, which Timeline uses to support Seek under
// KeepHistory (the factory is called to instantiate a fresh Source
// at the seek target). AppendAudio sets a Factory automatically
// using the internal CachedClip; callers of Schedule who want their
// cues seekable should supply one.
type Cue struct {
	Source    Source
	Factory   func() Source
	Start     time.Duration
	Transform Transform
}

// Handle is the caller's view of a scheduled Cue.
type Handle interface {
	// ID uniquely identifies this cue within its timeline for the
	// lifetime of the program.
	ID() uint64

	// Cancel stops the cue; subsequent Pulls will not produce samples
	// for it. Cancel is idempotent and safe to call from any
	// goroutine.
	Cancel()

	// Done returns a channel that is closed when the cue has finished
	// (naturally exhausted or cancelled).
	Done() <-chan struct{}
}
