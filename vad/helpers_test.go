package vad

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/events"
)

// Test-only signal builders and a stub detector for isolating
// Gate/Ducker behaviour from any real engine.

// appendSilence appends frames zero frames of ch channels to sig.
func appendSilence(sig []float64, frames, ch int) []float64 {
	for i := 0; i < frames*ch; i++ {
		sig = append(sig, 0)
	}
	return sig
}

// appendTone appends frames of a freq Hz sine at linear amplitude amp,
// duplicated across ch channels, starting at phase 0.
func appendTone(sig []float64, freq, amp float64, frames, rate, ch int) []float64 {
	w := 2 * math.Pi * freq / float64(rate)
	for i := 0; i < frames; i++ {
		v := amp * math.Sin(w*float64(i))
		for c := 0; c < ch; c++ {
			sig = append(sig, v)
		}
	}
	return sig
}

// appendMix appends frames of two summed sines (freqA at ampA plus
// freqB at ampB) across ch channels.
func appendMix(sig []float64, freqA, ampA, freqB, ampB float64, frames, rate, ch int) []float64 {
	wa := 2 * math.Pi * freqA / float64(rate)
	wb := 2 * math.Pi * freqB / float64(rate)
	for i := 0; i < frames; i++ {
		v := ampA*math.Sin(wa*float64(i)) + ampB*math.Sin(wb*float64(i))
		for c := 0; c < ch; c++ {
			sig = append(sig, v)
		}
	}
	return sig
}

// ampForDBFS returns the sine amplitude whose frame energy
// E = 10·log10(mean(x²)) lands at the given dBFS value (sine mean
// square is A²/2, so E = 20·log10(A) − 3.01).
func ampForDBFS(db float64) float64 {
	return math.Pow(10, (db+10*math.Log10(2))/20)
}

// processChunks drives det.Process over sig in chunks of chunkFrames
// frames (interleaved, ch channels), mirroring how a mixer or device
// callback would.
func processChunks(p interface{ Process([]float64) }, sig []float64, chunkFrames, ch int) {
	step := chunkFrames * ch
	for off := 0; off < len(sig); off += step {
		end := off + step
		if end > len(sig) {
			end = len(sig)
		}
		p.Process(sig[off:end])
	}
}

// collectEvents subscribes to det's bus and returns the slice pointer
// events will be appended to (single-goroutine tests only — Publish is
// synchronous from Process on the test goroutine).
func collectEvents(det Detector) *[]SpeechEvent {
	evs := new([]SpeechEvent)
	det.Events().Subscribe(func(ev SpeechEvent) { *evs = append(*evs, ev) })
	return evs
}

// stubDetector is a hand-driven Detector for Gate/Ducker unit tests.
type stubDetector struct {
	rate, ch  int
	active    atomic.Bool
	bus       *events.Bus[SpeechEvent]
	latency   time.Duration
	resets    atomic.Int64
	processed atomic.Int64 // total samples fed via Process
}

func newStubDetector(rate, ch int) *stubDetector {
	return &stubDetector{rate: rate, ch: ch, bus: events.New[SpeechEvent]()}
}

func (s *stubDetector) Process(samples []float64) { s.processed.Add(int64(len(samples))) }
func (s *stubDetector) Reset()                    { s.resets.Add(1); s.active.Store(false) }
func (s *stubDetector) Active() bool              { return s.active.Load() }
func (s *stubDetector) Probability() float64 {
	if s.active.Load() {
		return 1
	}
	return 0
}
func (s *stubDetector) LastTransition() (SpeechEvent, bool) { return SpeechEvent{}, false }
func (s *stubDetector) DecisionLatency() time.Duration      { return s.latency }
func (s *stubDetector) Events() *events.Bus[SpeechEvent]    { return s.bus }
func (s *stubDetector) SampleRate() int                     { return s.rate }
func (s *stubDetector) Channels() int                       { return s.ch }

var _ Detector = (*stubDetector)(nil)
