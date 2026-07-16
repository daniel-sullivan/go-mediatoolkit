package duplex

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/buffers"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

// captureQueue is the bounded, thread-safe hand-off between Push (any
// goroutine) and the audio goroutine's per-tick drain. Capacity is
// bounded in samples; overload sheds the NEWEST frame (the push that
// would overflow is rejected with ErrCaptureOverflow and counted)
// rather than evicting buffered audio — the buffered frames are older
// and already owed to the detector/ASR in order, so dropping them
// would tear a hole mid-stream, while the rejected push surfaces the
// error to the caller who can meter it.
type captureQueue struct {
	mu sync.Mutex

	capSamples int

	frames buffers.Queue[captureFrame]
	total  int
	slab   buffers.Slab

	dropped atomic.Uint64
}

// captureFrame is one pushed capture frame: a copy of the samples
// plus the caller's tag.
type captureFrame struct {
	samples []float64
	tag     int64
}

// newCaptureQueue constructs an empty queue bounded at capSamples
// buffered samples.
func newCaptureQueue(capSamples int) *captureQueue {
	return &captureQueue{capSamples: capSamples}
}

// push copies frame into the queue, or drops it (counted) when the
// queue is full.
func (q *captureQueue) push(frame []float64, tag int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.total+len(frame) > q.capSamples {
		q.dropped.Add(1)
		return ErrCaptureOverflow
	}
	q.frames.Push(captureFrame{samples: append(q.slab.Take(), frame...), tag: tag})
	q.total += len(frame)
	return nil
}

// queued reports the buffered sample count.
func (q *captureQueue) queued() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.total
}

// drainInto pops whole pushed frames — appending their samples to
// accum and their tag spans to spans — until accum holds at least
// want samples or the queue is empty, returning the updated slices.
// One locked pass per tick keeps lock churn off the Push side.
func (q *captureQueue) drainInto(accum []float64, spans []tagSpan, want int) ([]float64, []tagSpan) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(accum) < want {
		f, ok := q.frames.Pop()
		if !ok {
			break
		}
		accum = append(accum, f.samples...)
		spans = append(spans, tagSpan{tag: f.tag, n: len(f.samples)})
		q.total -= len(f.samples)
		q.slab.Put(f.samples)
	}
	return accum, spans
}

// tagSpan records that the next n accumulated capture samples came
// from a push tagged tag; the re-framer assigns each 10 ms frame the
// tag of the span its first sample belongs to.
type tagSpan struct {
	tag int64
	n   int
}

// captureBatchFrames bounds how many 10 ms capture frames one tick
// processes: enough to drain a network burst several times faster
// than real time, small enough that a full queue cannot stall the
// paced render side for more than a fraction of a tick.
const captureBatchFrames = 8

// captureTick drains and processes up to captureBatchFrames capture
// frames. Runs on the audio goroutine, after the render half of the
// tick (so the AEC has always analysed this tick's far-end frame
// before processing capture).
func (e *Engine) captureTick() {
	e.captureAccum, e.captureSpans = e.capture.drainInto(
		e.captureAccum, e.captureSpans, captureBatchFrames*e.frameSamples)

	// Consume whole frames through a cursor, then shift the remainder
	// down once — not once per frame.
	off := 0
	for i := 0; i < captureBatchFrames && len(e.captureAccum)-off >= e.frameSamples; i++ {
		copy(e.captureFrame, e.captureAccum[off:off+e.frameSamples])
		off += e.frameSamples
		e.processCaptureFrame(e.captureFrame, e.consumeSpanTag())
	}
	if off > 0 {
		e.captureAccum = append(e.captureAccum[:0], e.captureAccum[off:]...)
	}
}

// consumeSpanTag consumes one frame's worth of tag spans and returns
// the tag of the span the frame's first sample belongs to.
func (e *Engine) consumeSpanTag() int64 {
	tag := e.captureSpans[0].tag
	n := e.frameSamples
	for n > 0 {
		s := &e.captureSpans[0]
		take := min(s.n, n)
		s.n -= take
		n -= take
		if s.n == 0 {
			e.captureSpans = e.captureSpans[1:]
		}
	}
	if len(e.captureSpans) == 0 {
		e.captureSpans = nil // release the drained backing array
	}
	return tag
}

// processCaptureFrame runs one 10 ms capture frame through the DSP
// chain and the speech-event state machine: AEC (echo removal against
// the render side's far-end feed) → capture chain → detector
// (pass-through observer), then routes the post-DSP frame by speech
// state — forwarded as an EventAudioFrame while in speech, banked
// into the pre-roll otherwise.
func (e *Engine) processCaptureFrame(frame []float64, tag int64) {
	if e.aec != nil {
		e.aec.Process(frame)
	}
	for _, p := range e.captureChain {
		p.Process(frame)
	}

	e.transitions = e.transitions[:0]
	e.detector.Process(frame) // publishes transitions synchronously into e.transitions

	for _, tr := range e.transitions {
		switch {
		case tr.Kind == vad.SpeechStart && !e.inSpeech:
			e.inSpeech = true
			e.emitSpeechStart(tr, tag)
		case tr.Kind == vad.SpeechEnd && e.inSpeech:
			e.inSpeech = false
			e.send(Event{Kind: EventSpeechStop, Tag: tag})
		}
	}

	if e.inSpeech {
		e.send(Event{Kind: EventAudioFrame, Frame: append([]float64(nil), frame...), Tag: tag})
	} else {
		e.preroll.Push(frame, tag)
	}
}

// emitSpeechStart delivers the utterance-start sequence: the
// EventSpeechStart (back-dated by the lead-in), the synthetic lead-in
// silence frames counting up toward the real start, then the pre-roll
// replay. The utterance's start is the oldest pre-roll tag — the
// earliest audio about to be replayed — or the triggering live
// frame's tag when the pre-roll is empty or disabled. Every
// synthesised tag is clamped at 0, so tags near the start of a
// session can repeat rather than go negative.
func (e *Engine) emitSpeechStart(tr vad.SpeechEvent, liveTag int64) {
	realStart := liveTag
	if t, ok := e.preroll.OldestTag(); ok {
		realStart = t
	}

	e.send(Event{
		Kind:       EventSpeechStart,
		Timestamp:  clampTag(realStart - int64(e.leadInFrames)*e.tagsPerFrame),
		OnsetFrame: tr.Frame,
	})

	for i := 0; i < e.leadInFrames; i++ {
		e.send(Event{
			Kind:  EventAudioFrame,
			Frame: make([]float64, e.frameSamples),
			Tag:   clampTag(realStart - int64(e.leadInFrames-i)*e.tagsPerFrame),
		})
	}

	e.preroll.Replay(func(f []float64, tag int64) {
		e.send(Event{Kind: EventAudioFrame, Frame: append([]float64(nil), f...), Tag: tag})
	})
}

// clampTag floors a synthesised (back-dated) tag at 0.
func clampTag(t int64) int64 {
	if t < 0 {
		return 0
	}
	return t
}

// send delivers ev to the event channel in FIFO order. When the
// channel is full it blocks — an EventAudioFrame dropped here would
// tear a hole in the audio a downstream ASR transcribes, which is
// strictly worse than a late tick — but not forever: a send still
// blocked after Config.StallTimeout declares the consumer stalled,
// records ErrEventsStalled (see Err), and shuts the session down.
// Stop likewise unblocks a pending send (the event is then discarded
// with the session).
func (e *Engine) send(ev Event) {
	select {
	case e.events <- ev:
		return
	case <-e.stop:
		return
	default:
	}

	timer := time.NewTimer(e.stallTimeout)
	defer timer.Stop()
	select {
	case e.events <- ev:
	case <-e.stop:
	case <-timer.C:
		err := ErrEventsStalled
		e.err.Store(&err)
		e.shutdown()
	}
}
