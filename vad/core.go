package vad

import (
	"math"
	"sync/atomic"

	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// core is the shared state block every detector engine embeds: the
// stream format, the event bus, and the lock-free reader atomics
// behind Active/Probability/LastTransition. The audio goroutine (the
// engine's Process) is the only writer; any goroutine may read.
//
// The write path preserves one asserted ordering: emit stores ALL
// state atomics before publishing the event, so a bus subscriber
// observing a SpeechStart already sees Active() == true,
// Probability() == the event's probability, and LastTransition() ==
// the event being delivered. Tests pin this ordering.
type core struct {
	sampleRate int
	channels   int
	bus        *events.Bus[SpeechEvent]

	active atomic.Bool
	prob   atomic.Uint64 // float64 bits
	last   atomic.Pointer[SpeechEvent]
}

// newCore initialises a core for the given (already validated) stream
// format.
func newCore(sampleRate, channels int) core {
	return core{
		sampleRate: sampleRate,
		channels:   channels,
		bus:        events.New[SpeechEvent](),
	}
}

// Active reports the current hysteresis-smoothed speech state. Safe
// from any goroutine.
func (c *core) Active() bool { return c.active.Load() }

// Probability reports the engine's current speech probability in
// [0, 1]. Safe from any goroutine.
func (c *core) Probability() float64 {
	return math.Float64frombits(c.prob.Load())
}

// LastTransition returns the most recent SpeechEvent and true, or a
// zero event and false if no transition has fired since
// construction/Reset. Safe from any goroutine.
func (c *core) LastTransition() (SpeechEvent, bool) {
	p := c.last.Load()
	if p == nil {
		return SpeechEvent{}, false
	}
	return *p, true
}

// Events returns the detector's transition bus. See the package doc
// for the subscriber contract.
func (c *core) Events() *events.Bus[SpeechEvent] { return c.bus }

// SampleRate reports the input sample rate the detector was
// constructed for.
func (c *core) SampleRate() int { return c.sampleRate }

// Channels reports the interleaved input channel count the detector
// was constructed for.
func (c *core) Channels() int { return c.channels }

// emit records a transition and publishes it: state atomics first
// (active, probability, last transition), Publish last. Called only
// from the audio goroutine.
func (c *core) emit(kind SpeechEventKind, frame int64, probability float64) {
	ev := SpeechEvent{
		Kind:        kind,
		Frame:       frame,
		Pos:         mutations.FramesToDuration(frame, c.sampleRate),
		Probability: probability,
	}
	c.active.Store(kind == SpeechStart)
	c.prob.Store(math.Float64bits(probability))
	c.last.Store(&ev)
	c.bus.Publish(ev)
}

// setProbability updates the live probability reading without a
// transition (engines call it per decision frame when the probability
// moves within a state). Called only from the audio goroutine.
func (c *core) setProbability(p float64) {
	c.prob.Store(math.Float64bits(p))
}

// binaryProbability reports the probability a non-neural engine
// (Energy, WebRTC) reports for a fired transition: 1 for SpeechStart,
// 0 for SpeechEnd — the "binary engines report exactly 0 or 1" contract
// documented on Detector.Probability and SpeechEvent.Probability.
func binaryProbability(kind SpeechEventKind) float64 {
	if kind == SpeechStart {
		return 1.0
	}
	return 0.0
}

// validateChannels checks a Channels config field against the shared
// 1..maxChannels range every constructor enforces.
func validateChannels(ch int) error {
	if ch < 1 || ch > maxChannels {
		return ErrBadChannels
	}
	return nil
}

// resetState clears the reader atomics back to the just-constructed
// state (inactive, probability 0, no last transition). The bus and
// its subscriptions are retained — Reset starts a fresh stream, it
// does not tear down observers. Called only from the audio goroutine.
func (c *core) resetState() {
	c.active.Store(false)
	c.prob.Store(0)
	c.last.Store(nil)
}
