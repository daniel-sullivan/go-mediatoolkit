package vad

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// PreRollConfig configures a PreRoll.
type PreRollConfig struct {
	// SampleRate is the stream sample rate in Hz. Must be positive.
	SampleRate int

	// Channels is the interleaved channel count, in 1..64.
	Channels int

	// Duration is how much trailing audio the buffer retains: Push
	// evicts the oldest frames once the buffered total exceeds it.
	// Zero DISABLES the buffer entirely — Push is a no-op, Replay
	// replays nothing, OldestTag reports nothing — so a caller can
	// thread one code path whether or not pre-roll is wanted. Must
	// not be negative.
	Duration time.Duration
}

// preRollFrame is one buffered frame: a copy of the pushed samples
// plus the caller's opaque tag.
type preRollFrame struct {
	samples []float64
	tag     int64
}

// PreRoll buffers the most recent PreRollConfig.Duration of
// caller-framed audio, each frame paired with an opaque int64 tag
// (typically a capture timestamp), for replay when a Detector
// announces speech.
//
// A detector cannot announce speech the instant it begins — see the
// package doc's "Positions and latency" section — so the first
// syllables of an utterance have already flowed past by the time
// SpeechStart fires. A PreRoll running alongside the detector keeps
// those frames: push every out-of-speech frame, and on SpeechStart
// replay the buffer into the consumer (an ASR, a recorder) before
// forwarding live audio, so the utterance arrives whole. OldestTag
// exposes the tag of the earliest buffered frame for back-dating the
// utterance's start timestamp to where the replayed audio actually
// begins.
//
// # Framing
//
// Frames are interleaved float64 samples in [-1, 1] with
// PreRollConfig.Channels channels. Frame lengths may vary from call
// to call — each Push buffers exactly the frame it is given, and
// Replay hands frames back with the same boundaries and tags they
// were pushed with; the buffer never re-slices audio across frame
// boundaries, so the frame↔tag pairing is preserved verbatim.
// len(frame) should be a whole multiple of Channels (retention
// accounting counts raw samples, so a stray partial frame skews the
// retained duration slightly rather than failing).
//
// # Eviction
//
// The buffer retains the newest frames whose total duration does not
// exceed Duration: when a Push takes the total over, whole oldest
// frames are evicted until it fits again. A single frame longer than
// Duration by itself is kept (evicting it would leave nothing to
// replay); it is evicted as soon as a newer frame arrives. A nonzero
// Duration shorter than one sample period therefore still retains the
// single most recent frame — only an exact zero disables the buffer.
//
// # Concurrency
//
// Like a mutations.Processor, a PreRoll is single-goroutine: drive
// Push/Replay/OldestTag/Clear/SetDuration from the audio goroutine
// that owns the stream (the same one driving the Detector's Process).
// None of its methods are safe to call concurrently.
//
// # Pairing with an immediate-onset detector
//
// PreRoll pairs naturally with a detector tuned for immediate onset.
// On SileroConfig, MinSpeech: time.Nanosecond together with
// SpeechPad: time.Nanosecond reproduces the upstream silero-vad
// VADIterator streaming semantics exactly — SpeechStart on the first
// window at or above Threshold, no onset gate, no back-padding — with
// the pre-roll supplying the lead-in audio that SpeechPad's
// back-padding would otherwise have to cover.
type PreRoll struct {
	sampleRate int
	channels   int

	// enabled is false only when the configured Duration is exactly
	// zero (at construction or via SetDuration): Push is then a no-op
	// and nothing is ever buffered.
	enabled bool

	// capSamples is Duration expressed in interleaved samples. It can
	// round to 0 for a nonzero Duration shorter than one sample
	// period; the eviction loop then still keeps the single newest
	// frame (see the type doc's Eviction section).
	capSamples int

	// frames is a FIFO queue: frames[head:] are the buffered frames,
	// oldest first. Evicted/replayed slots are recycled through free
	// so a steady-state Push cycle stops allocating once the queue
	// has warmed up.
	frames []preRollFrame
	head   int
	total  int // buffered samples across all frames
	free   [][]float64
}

// NewPreRoll constructs a PreRoll for cfg. Returns ErrBadSampleRate
// if cfg.SampleRate is not positive, ErrBadChannels if cfg.Channels
// is not in 1..64, or ErrBadConfig if cfg.Duration is negative.
func NewPreRoll(cfg PreRollConfig) (*PreRoll, error) {
	if cfg.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	if cfg.Duration < 0 {
		return nil, ErrBadConfig
	}
	return &PreRoll{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		enabled:    cfg.Duration != 0,
		capSamples: preRollSamples(cfg.SampleRate, cfg.Channels, cfg.Duration),
	}, nil
}

// Push buffers a copy of frame paired with tag, evicting the oldest
// frames if the buffered total now exceeds Duration (see the type
// doc's Eviction section). frame is copied — the caller may reuse or
// mutate it freely after Push returns. A no-op when the buffer is
// disabled (Duration zero) or frame is empty.
func (p *PreRoll) Push(frame []float64, tag int64) {
	if !p.enabled || len(frame) == 0 {
		return
	}

	var buf []float64
	if n := len(p.free); n > 0 {
		buf = p.free[n-1][:0]
		p.free[n-1] = nil
		p.free = p.free[:n-1]
	}
	buf = append(buf, frame...)

	p.frames = append(p.frames, preRollFrame{samples: buf, tag: tag})
	p.total += len(frame)
	p.evictOverflow()
}

// Replay calls fn once per buffered frame, oldest first, with the
// frame's samples and tag, then clears the buffer. The samples slice
// is the buffer's internal storage — valid only for the duration of
// the callback; a consumer that retains audio past the callback must
// copy it. A no-op (fn is never called) when nothing is buffered.
//
// The buffer is emptied before the first callback runs, so fn may
// Push new frames into the same PreRoll: they accumulate normally and
// are not replayed (or cleared) by this call.
func (p *PreRoll) Replay(fn func(frame []float64, tag int64)) {
	frames := p.frames[p.head:]
	p.frames = nil
	p.head = 0
	p.total = 0
	for i := range frames {
		fn(frames[i].samples, frames[i].tag)
	}
	for i := range frames {
		p.free = append(p.free, frames[i].samples)
		frames[i].samples = nil
	}
}

// OldestTag returns the tag of the earliest buffered frame and true,
// or 0 and false if nothing is buffered. Use it to back-date a speech
// start timestamp to where a Replay's audio actually begins.
func (p *PreRoll) OldestTag() (int64, bool) {
	if p.count() == 0 {
		return 0, false
	}
	return p.frames[p.head].tag, true
}

// Clear discards all buffered frames (their storage is retained for
// reuse by later Pushes).
func (p *PreRoll) Clear() {
	for p.count() > 0 {
		p.popOldest()
	}
	p.frames = p.frames[:0]
	p.head = 0
}

// SetDuration live-resizes the retention window to d, evicting the
// oldest frames if the buffered total now exceeds it (the newest
// audio is kept). Zero clears the buffer and disables it, exactly as
// constructing with a zero Duration would; a later SetDuration can
// re-enable it. Returns ErrBadConfig (and changes nothing) if d is
// negative.
func (p *PreRoll) SetDuration(d time.Duration) error {
	if d < 0 {
		return ErrBadConfig
	}
	p.enabled = d != 0
	p.capSamples = preRollSamples(p.sampleRate, p.channels, d)
	if !p.enabled {
		p.Clear()
		return nil
	}
	p.evictOverflow()
	return nil
}

// count reports the number of buffered frames.
func (p *PreRoll) count() int { return len(p.frames) - p.head }

// evictOverflow evicts whole oldest frames until the buffered total
// fits capSamples again — always keeping at least one frame — then
// re-anchors the queue.
func (p *PreRoll) evictOverflow() {
	for p.count() > 1 && p.total > p.capSamples {
		p.popOldest()
	}
	p.compact()
}

// popOldest evicts the oldest buffered frame, recycling its storage.
func (p *PreRoll) popOldest() {
	f := &p.frames[p.head]
	p.total -= len(f.samples)
	p.free = append(p.free, f.samples)
	f.samples = nil
	p.head++
}

// compact re-anchors the frame queue at index 0 once the dead prefix
// left by popOldest dominates the slice, keeping the backing array
// from growing without bound under a steady push/evict cycle.
func (p *PreRoll) compact() {
	if p.head > 0 && p.head >= len(p.frames)/2 {
		n := copy(p.frames, p.frames[p.head:])
		clear(p.frames[n:])
		p.frames = p.frames[:n]
		p.head = 0
	}
}

// preRollSamples converts a retention duration to a whole count of
// interleaved samples at the stream format. A duration so large that
// the conversion would overflow int64 (tens of hours at audio rates)
// saturates to an effectively unbounded window instead of wrapping.
func preRollSamples(sampleRate, channels int, d time.Duration) int {
	if d > time.Duration((math.MaxInt64-int64(time.Second)/2)/int64(sampleRate)) {
		return math.MaxInt
	}
	return int(mutations.DurationToFrames(d, sampleRate)) * channels
}
