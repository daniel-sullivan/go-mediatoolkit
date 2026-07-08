// Package vad provides streaming voice-activity detection (VAD) for
// interleaved float64 audio, plus two dynamics processors driven by it:
// a speech Gate and a sidechain Ducker.
//
// # Detectors
//
// A detector is a pass-through mutations.Processor: Process feeds the
// audio into the detection engine and never modifies the samples, so a
// detector drops into any effects chain (a timeline.EffectSource, a
// mixer track, a mixer.Config.Processors master bus) purely to observe
// the stream. Detection state is exposed four ways:
//
//   - Pass-through chain insertion — the Detector itself, via Process.
//   - Polled state — Active, Probability, LastTransition, all safe to
//     read from any goroutine while another goroutine drives Process.
//   - Events — an events.Bus[SpeechEvent] publishing SpeechStart and
//     SpeechEnd transitions.
//   - Dynamics — Gate (mutes/attenuates non-speech in the detector's
//     own stream) and Ducker (attenuates a different stream while the
//     sidechain detector hears speech).
//
// Three engines implement the interface: Energy (NewEnergyDetector,
// a band-limited adaptive-threshold design), WebRTC-GMM
// (NewWebRTCDetector, a bit-exact pure-Go libfvad port), and Silero
// (NewSileroDetector, a pure-Go port of the Silero VAD v6.2 neural
// model with vendored weights).
//
// # Reconfiguration model
//
// The engine and its stream format are fixed at construction; every
// tuning knob is live — the Set* methods on each concrete type may be
// called from any goroutine mid-stream. Setters store into single-word
// atomics that the audio goroutine snapshots once per decision frame,
// so there is no lock on the audio path and no torn state: threshold
// pairs whose relative order matters (on/off hysteresis) are packed
// into ONE atomic word, making a torn pair — and the hysteresis
// inversion it would cause — impossible.
//
// # Event delivery contract
//
// Events are published synchronously from Process, on the audio
// goroutine, per the events.Bus contract: subscriber callbacks must be
// fast and non-blocking (spawn a goroutine to do real work).
// State atomics (Active, Probability, LastTransition) are updated
// BEFORE the event is published, so a subscriber observing a
// SpeechStart already sees Active() == true. Transitions are
// per-utterance rare, so a subscriber doing a channel send or an
// atomic store adds no meaningful audio-path cost.
//
// # Positions and latency
//
// SpeechEvent positions are stream positions: input frames since
// construction (or the last Reset), independent of wall clock. A
// detector cannot announce speech the instant it begins — it needs a
// decision frame to fill and an onset debounce to pass — so events are
// back-timestamped: SpeechStart's Frame points at the frame where the
// detected run actually began, not where the decision was made.
// DecisionLatency reports the announcement delay honestly; use it as
// Gate lookahead to gate without clipping onsets.
//
// # Flushing a finite stream
//
// A detector that resamples internally (Silero always; Energy and
// WebRTC only when constructed for an input rate other than their
// engine's native rate) holds pending samples in the resampler's FIR
// history and the not-yet-full decision frame being accumulated. Up to
// DecisionLatency() worth of trailing audio can still be sitting there
// — undecided — when a finite stream's last Process call returns, so a
// speech run right at end-of-stream may never cross the decision
// boundary and its SpeechEnd (or, for a short utterance entirely inside
// that tail, both events) is never announced. Flush it the same way
// Gate recommends flushing its own delay line: feed DecisionLatency()
// worth of trailing silence at end-of-stream (an offline append, or
// timeline.NewEffectSource(src, detector).WithTail(detector.DecisionLatency())
// when driving the detector through a Source pipeline).
//
// # One feeder per stream
//
// A Detector is a mutations.Processor, and inherits its contract in
// full: a single instance is bound to the stream it was constructed
// for and is not safe to share across logical streams or goroutines
// without explicit synchronisation. Feeding one Detector (or a Gate or
// Ducker built on one) from two sources, or interleaving two unrelated
// streams through the same instance, corrupts its stream positions and
// internal state (SpeechEvent.Frame, the resampler's FIR history, the
// adaptive floor or carried model state) silently — there is no panic
// or error, just wrong answers. Construct one Detector per stream
// instead; they are cheap, and Reset only rewinds a single stream's
// worth of state, it does not make an instance safe to share.
package vad

import (
	"strconv"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// SpeechEventKind identifies the direction of a speech transition.
type SpeechEventKind int

const (
	// SpeechStart marks a silence→speech transition.
	SpeechStart SpeechEventKind = iota
	// SpeechEnd marks a speech→silence transition.
	SpeechEnd
)

// String returns "SpeechStart"/"SpeechEnd" (or a decorated integer for
// an out-of-range value).
func (k SpeechEventKind) String() string {
	switch k {
	case SpeechStart:
		return "SpeechStart"
	case SpeechEnd:
		return "SpeechEnd"
	default:
		return "SpeechEventKind(" + strconv.Itoa(int(k)) + ")"
	}
}

// SpeechEvent describes one hysteresis-smoothed speech transition.
//
// Positions are stream positions — input frames since construction or
// the last Reset — NOT wall-clock times. Events are back-timestamped:
// a SpeechStart carries the position where the voiced run began (the
// audio the onset debounce was confirming), and a SpeechEnd carries
// the position where speech actually stopped (the start of the silence
// run the hangover was bridging). The event is therefore always
// published AFTER the position it describes, by roughly the engine's
// DecisionLatency for a start and the hangover for an end.
type SpeechEvent struct {
	// Kind is the transition direction.
	Kind SpeechEventKind

	// Frame is the input-stream frame position of the transition
	// (frames since construction/Reset, at the detector's input sample
	// rate). Back-timestamped as described in the type doc. For
	// engines that resample internally, Frame is mapped back to the
	// input timeline and is accurate to within one engine decision
	// frame.
	Frame int64

	// Pos is Frame expressed as a duration at the input sample rate
	// (mutations.FramesToDuration of Frame).
	Pos time.Duration

	// Probability is the engine's speech probability at the decision
	// that fired the transition. Binary engines (Energy, WebRTC)
	// report 1 for SpeechStart and 0 for SpeechEnd; neural engines
	// report their model posterior.
	Probability float64
}

// Detector is the interface all VAD engines implement, and the type
// Gate, Ducker, and user code consume. A Detector is a pass-through
// mutations.Processor: Process feeds samples into the engine and never
// modifies them.
//
// Process/Reset must be driven from a single goroutine (the audio
// goroutine); every other method is safe to call concurrently from any
// goroutine while Process runs — the readers are lock-free atomics.
// Like any mutations.Processor, a Detector is bound to the one stream
// it was constructed for — see the package doc's "One feeder per
// stream" section before sharing an instance across streams.
type Detector interface {
	mutations.Processor

	// Active reports the current hysteresis-smoothed speech state.
	// Safe from any goroutine.
	Active() bool

	// Probability reports the engine's current speech probability in
	// [0, 1]. Binary engines report exactly 0 or 1 (mirroring
	// Active). Safe from any goroutine.
	Probability() float64

	// LastTransition returns the most recent SpeechEvent and true, or
	// a zero event and false if no transition has fired since
	// construction/Reset. Safe from any goroutine.
	LastTransition() (SpeechEvent, bool)

	// DecisionLatency reports how long after speech begins the engine
	// announces it, worst case: internal resampler group delay + one
	// decision frame fill + the onset debounce. It reflects the
	// CURRENT live-set onset, so it can change after SetOnset (and
	// equivalents). Use it as Gate lookahead to open before the onset.
	DecisionLatency() time.Duration

	// Events returns the bus publishing SpeechStart/SpeechEnd
	// transitions. See the package doc for the subscriber contract
	// (synchronous delivery on the audio goroutine; callbacks must be
	// fast and non-blocking).
	Events() *events.Bus[SpeechEvent]

	// SampleRate reports the input sample rate in Hz the detector was
	// constructed for.
	SampleRate() int

	// Channels reports the interleaved input channel count the
	// detector was constructed for.
	Channels() int
}
