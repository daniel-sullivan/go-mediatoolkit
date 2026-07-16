package duplex

import (
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

// fakeDetector is a scriptable vad.Detector: tests queue transitions
// with trigger, and the next Process call publishes them — giving
// deterministic, tick-aligned control over the engine's speech-event
// state machine without a real engine's decision latency.
type fakeDetector struct {
	rate, ch int
	bus      *events.Bus[vad.SpeechEvent]
	active   bool
	frames   int64
	pending  []vad.SpeechEventKind
}

func newFakeDetector(rate, ch int) *fakeDetector {
	return &fakeDetector{rate: rate, ch: ch, bus: events.New[vad.SpeechEvent]()}
}

// trigger queues a transition for the next Process call.
func (d *fakeDetector) trigger(kind vad.SpeechEventKind) {
	d.pending = append(d.pending, kind)
}

func (d *fakeDetector) Process(samples []float64) {
	d.frames += int64(len(samples) / d.ch)
	for _, kind := range d.pending {
		d.active = kind == vad.SpeechStart
		d.bus.Publish(vad.SpeechEvent{Kind: kind, Frame: d.frames})
	}
	d.pending = d.pending[:0]
}

func (d *fakeDetector) Reset()                                  { d.active = false; d.frames = 0 }
func (d *fakeDetector) Active() bool                            { return d.active }
func (d *fakeDetector) Probability() float64                    { return 0 }
func (d *fakeDetector) LastTransition() (vad.SpeechEvent, bool) { return vad.SpeechEvent{}, false }
func (d *fakeDetector) DecisionLatency() time.Duration          { return 0 }
func (d *fakeDetector) Events() *events.Bus[vad.SpeechEvent]    { return d.bus }
func (d *fakeDetector) SampleRate() int                         { return d.rate }
func (d *fakeDetector) Channels() int                           { return d.ch }

// captureProbe is a pass-through capture-chain processor recording
// every post-AEC frame it sees, for asserting on the echo-cancelled
// signal without consuming events.
type captureProbe struct {
	frames [][]float64
}

func (p *captureProbe) Process(samples []float64) {
	p.frames = append(p.frames, append([]float64(nil), samples...))
}

func (p *captureProbe) Reset() { p.frames = nil }

// drainEvents empties every event currently buffered on the engine's
// channel without blocking.
func drainEvents(e *Engine) []Event {
	var out []Event
	for {
		select {
		case ev, ok := <-e.Events():
			if !ok {
				return out
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}

// collectOutput installs an output callback appending copies of every
// emitted frame (and its seq) to the returned slices' pointees.
func collectOutput(e *Engine) (*[][]float64, *[]int64) {
	frames := new([][]float64)
	seqs := new([]int64)
	e.SetOutput(func(frame []float64, seq int64) {
		*frames = append(*frames, append([]float64(nil), frame...))
		*seqs = append(*seqs, seq)
	})
	return frames, seqs
}

// constFrame returns n samples all equal to v.
func constFrame(n int, v float64) []float64 {
	f := make([]float64, n)
	for i := range f {
		f[i] = v
	}
	return f
}
