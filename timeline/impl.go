package timeline

import (
	"io"
	"sync"
	"sync/atomic"
	"time"

	"go-mediatoolkit/mutations"
)

// Timeline plays scheduled cues at their declared times and emits
// silence where no cue overlaps the cursor. It has an indefinite
// Duration — Pull never returns io.EOF unless Close has been called.
//
// There are two ways to place cues:
//
//   - Schedule(cue) — place at an explicit Start time. Parallel cues
//     sum. Useful for game SFX or DAW-style arrangements where clips
//     live at specific project times.
//
//   - Append(cue) / AppendAudio(audio) — place at the end of the
//     existing schedule. Back-to-back playback. The source's
//     Duration must be finite (use Schedule for live / looping /
//     infinite sources).
//
// The two APIs mix freely: you can Append a track and then Schedule
// a parallel cue over it, or vice versa.
//
// # KeepHistory + Seek
//
// By default, cues are dropped from memory once Pull has consumed
// them. Construct with KeepHistory=true to retain played cues so
// Seek(delta) can rewind. Retained timelines' memory grows with
// scheduled duration; call Close when done to release.
//
// Seek only succeeds when the cue(s) active at the target frame
// expose a way to rewind — i.e. the cue was constructed with a
// Factory (cue.Factory != nil), which AppendAudio sets up
// automatically via the internal CachedClip. For cues built with a
// one-shot Source (Schedule(Cue{Source: ...}) without a Factory),
// Seek returns ErrNotSeekable for that region.
//
// # Concurrency
//
// Schedule / Append / AppendAudio / Seek are safe to call from any
// goroutine. Pull expects a single consumer.
type Timeline struct {
	sampleRate  int
	channels    int
	keepHistory bool

	mu     sync.Mutex
	cursor int64 // frames consumed
	end    int64 // frames scheduled (max endFrame across all cues)
	active []*activeCue
	closed bool

	nextID atomic.Uint64
}

// Config configures a Timeline. SampleRate and Channels are required.
type Config struct {
	SampleRate  int
	Channels    int
	KeepHistory bool // retain played cues so Seek can rewind
}

// NewTimeline returns a realtime-mode Timeline (no history). For the
// scrub-back use case, construct via NewTimelineWith(Config{...,
// KeepHistory: true}).
func NewTimeline(sampleRate, channels int) (*Timeline, error) {
	return NewTimelineWith(Config{SampleRate: sampleRate, Channels: channels})
}

// NewTimelineWith returns a Timeline with explicit configuration.
func NewTimelineWith(cfg Config) (*Timeline, error) {
	if cfg.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if cfg.Channels <= 0 {
		return nil, ErrBadChannels
	}
	return &Timeline{
		sampleRate:  cfg.SampleRate,
		channels:    cfg.Channels,
		keepHistory: cfg.KeepHistory,
	}, nil
}

// activeCue is the mutable per-cue state owned by the Timeline.
type activeCue struct {
	id         uint64
	startFrame int64
	endFrame   int64 // -1 if unknown (indefinite source)
	source     Source
	factory    func() Source // non-nil if the cue supports replay (set by AppendAudio, optional on Schedule)
	transform  Transform
	handle     *handleImpl
	scratch    []float64
	finished   bool // true once Source EOFs; kept around when keepHistory
}

// Schedule places cue on the timeline at cue.Start. Parallel cues at
// overlapping times are summed during Pull. Returns a Handle or an
// error (ErrNilSource, ErrNegativeStart, ErrFormatMismatch,
// mutations.ErrUnsortedEnvelope, ErrTimelineClosed).
//
// Optionally populate cue.Factory so the cue supports Seek under
// KeepHistory — if set, the Factory is used to reinstantiate the
// Source on each Seek.
func (t *Timeline) Schedule(cue Cue) (Handle, error) {
	if err := validateCue(cue, t.sampleRate, t.channels); err != nil {
		return nil, err
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, ErrTimelineClosed
	}

	id := t.nextID.Add(1)
	h := newHandle(id)
	startFrame := mutations.DurationToFrames(cue.Start, t.sampleRate)

	// Past-scheduled: fast-forward into the source so mid-clip playback
	// begins at the timeline cursor.
	if startFrame < t.cursor {
		skip := t.cursor - startFrame
		err := advanceSource(cue.Source, skip, t.channels)
		if err == io.EOF {
			h.finish()
			return h, nil
		}
		if err != nil {
			return nil, err
		}
	}

	endFrame := endFrameOf(cue.Source, startFrame, t.sampleRate)
	ac := &activeCue{
		id:         id,
		startFrame: startFrame,
		endFrame:   endFrame,
		source:     cue.Source,
		factory:    cue.Factory,
		transform:  cue.Transform,
		handle:     h,
	}
	insertActive(&t.active, ac)
	if endFrame > t.end {
		t.end = endFrame
	}
	return h, nil
}

// Append places cue immediately after the current end of the
// schedule. cue.Start is ignored. The source must have a finite
// Duration (use Schedule for indefinite sources); otherwise
// ErrUnboundedSource.
//
// Optionally populate cue.Factory so the cue supports Seek under
// KeepHistory.
func (t *Timeline) Append(cue Cue) (Handle, error) {
	if cue.Source == nil {
		return nil, ErrNilSource
	}
	if cue.Source.Duration() < 0 {
		return nil, ErrUnboundedSource
	}
	cue.Start = mutations.FramesToDuration(t.endLocked(), t.sampleRate)
	return t.Schedule(cue)
}

// AppendAudio is a convenience that caches audio as a CachedClip and
// Appends its playhead. Sets up a Factory automatically so Seek
// works under KeepHistory.
func (t *Timeline) AppendAudio(audio mutations.Audio) (Handle, error) {
	if audio.SampleRate != t.sampleRate || audio.Channels != t.channels {
		return nil, ErrFormatMismatch
	}
	clip, err := LoadClipFromAudio(audio)
	if err != nil {
		return nil, err
	}
	return t.Append(Cue{
		Source:  clip.Playhead(),
		Factory: func() Source { return clip.Playhead() },
	})
}

func (t *Timeline) endLocked() int64 {
	t.mu.Lock()
	e := t.end
	t.mu.Unlock()
	return e
}

// Seek shifts the read cursor by delta. Negative deltas rewind;
// KeepHistory must be true for any cue active at the target to remain
// seekable. Returns ErrSeekOutOfRange if the target is past the
// scheduled start (frame 0) or — for rewinds — beyond what history
// can provide; ErrNotSeekable if a cue at the target was scheduled
// without a Factory.
func (t *Timeline) Seek(delta time.Duration) error {
	deltaFrames := mutations.DurationToFrames(delta, t.sampleRate)

	t.mu.Lock()
	defer t.mu.Unlock()

	if deltaFrames < 0 && !t.keepHistory {
		return ErrSeekOutOfRange
	}
	target := t.cursor + deltaFrames
	if target < 0 {
		return ErrSeekOutOfRange
	}

	// Rebuild every cue whose play region is at or ahead of target.
	// For cues whose endFrame is at or before target, mark them
	// finished (seek-forward past their end). Cues that haven't
	// started yet and aren't finished stay untouched. Everything
	// else needs a fresh Source so playback resumes from the right
	// place (a finished cue rewound into still needs to replay from
	// target within its own span).
	for _, ac := range t.active {
		if ac.endFrame >= 0 && target >= ac.endFrame {
			if !ac.finished {
				ac.finished = true
				ac.handle.finish()
			}
			continue // fully in the past
		}
		if !ac.finished && target < ac.startFrame {
			continue // not yet started, not finished — nothing to rewind
		}
		if ac.factory == nil {
			return ErrNotSeekable
		}
		fresh := ac.factory()
		if fresh == nil {
			return ErrNilSource
		}
		ac.source = fresh
		ac.finished = false
		if target > ac.startFrame {
			offset := target - ac.startFrame
			if err := advanceSource(ac.source, offset, t.channels); err != nil && err != io.EOF {
				return err
			}
		}
	}
	t.cursor = target
	return nil
}

// Position returns the cursor as a duration since timeline start.
func (t *Timeline) Position() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return mutations.FramesToDuration(t.cursor, t.sampleRate)
}

// ScheduledDuration returns the end of the latest scheduled cue.
func (t *Timeline) ScheduledDuration() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	return mutations.FramesToDuration(t.end, t.sampleRate)
}

// Pull mixes active cues into dst and advances the cursor. dst must
// hold a whole number of frames. Returns len(dst), nil on success;
// len(dst), io.EOF if the timeline has been closed.
func (t *Timeline) Pull(dst []float64) (int, error) {
	if len(dst)%t.channels != 0 {
		return 0, ErrPartialFrame
	}
	for i := range dst {
		dst[i] = 0
	}
	frames := int64(len(dst) / t.channels)
	if frames == 0 {
		return 0, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return len(dst), io.EOF
	}

	segStart := t.cursor
	segEnd := t.cursor + frames

	kept := t.active[:0]
	for _, ac := range t.active {
		if ac.handle.cancelled.Load() {
			ac.handle.finish()
			continue
		}
		if ac.startFrame >= segEnd {
			kept = append(kept, ac)
			continue
		}
		if ac.finished {
			// Already EOF'd in a previous Pull; keep around only if
			// history is enabled. Still need to check if cursor has
			// rewound to before its endFrame (Seek case).
			if t.keepHistory {
				kept = append(kept, ac)
			}
			continue
		}

		var segFrameOffset int64
		var cueElapsed int64
		if ac.startFrame >= segStart {
			segFrameOffset = ac.startFrame - segStart
			cueElapsed = 0
		} else {
			segFrameOffset = 0
			cueElapsed = segStart - ac.startFrame
		}
		segFrames := frames - segFrameOffset
		if segFrames <= 0 {
			kept = append(kept, ac)
			continue
		}

		want := int(segFrames) * t.channels
		ac.scratch = mutations.ResizeScratch(ac.scratch, want)

		n, err := ac.source.Pull(ac.scratch)
		if n > 0 {
			ac.transform.apply(ac.scratch[:n], mutations.FramesToDuration(cueElapsed, t.sampleRate), t.channels, t.sampleRate)
			base := int(segFrameOffset) * t.channels
			for i := 0; i < n; i++ {
				dst[base+i] += ac.scratch[i]
			}
		}

		if err == io.EOF {
			ac.handle.finish()
			ac.finished = true
			if t.keepHistory {
				kept = append(kept, ac)
			}
			continue
		}
		kept = append(kept, ac)
	}
	t.active = kept
	t.cursor = segEnd

	return len(dst), nil
}

func (t *Timeline) SampleRate() int         { return t.sampleRate }
func (t *Timeline) Channels() int           { return t.channels }
func (t *Timeline) Duration() time.Duration { return -1 }
func (t *Timeline) Live() bool              { return false }

// Close marks the timeline closed. Pending cue handles are finished;
// subsequent Schedule/Append calls return ErrTimelineClosed;
// subsequent Pulls return io.EOF. Idempotent.
func (t *Timeline) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	for _, ac := range t.active {
		ac.handle.finish()
	}
	t.active = nil
	return nil
}

// validateCue checks basic invariants on cue against the timeline's
// format. Called under either lock or no lock — reads only cue fields
// and the given rate/channels.
func validateCue(cue Cue, sampleRate, channels int) error {
	if cue.Source == nil {
		return ErrNilSource
	}
	if cue.Start < 0 {
		return ErrNegativeStart
	}
	if cue.Source.SampleRate() != sampleRate || cue.Source.Channels() != channels {
		return ErrFormatMismatch
	}
	if err := mutations.ValidateGainEnvelope(cue.Transform.Gain); err != nil {
		return err
	}
	return nil
}

// endFrameOf returns startFrame + source duration in frames, or -1 if
// the source is indefinite.
func endFrameOf(src Source, startFrame int64, sampleRate int) int64 {
	d := src.Duration()
	if d < 0 {
		return -1
	}
	return startFrame + mutations.DurationToFrames(d, sampleRate)
}

// insertActive inserts ac into active in startFrame-sorted order.
func insertActive(active *[]*activeCue, ac *activeCue) {
	list := *active
	idx := len(list)
	for i, a := range list {
		if ac.startFrame < a.startFrame {
			idx = i
			break
		}
	}
	list = append(list, nil)
	copy(list[idx+1:], list[idx:])
	list[idx] = ac
	*active = list
}

// advanceSource skips frames of output from src, using Seek if the
// source supports it, otherwise pulling and discarding samples.
// Returns io.EOF if the source exhausts during the skip.
func advanceSource(src Source, frames int64, channels int) error {
	if s, ok := src.(Seekable); ok {
		return s.Seek(frames)
	}
	remaining := frames * int64(channels)
	scratch := make([]float64, 4096)
	for remaining > 0 {
		want := int64(len(scratch))
		if want > remaining {
			want = remaining
		}
		n, err := src.Pull(scratch[:want])
		remaining -= int64(n)
		if err == io.EOF {
			if remaining > 0 {
				return io.EOF
			}
			return nil
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.EOF
		}
	}
	return nil
}
