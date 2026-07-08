package vad

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

// Silero engine defaults, substituted for the zero value of the
// corresponding SileroConfig field. They mirror the upstream
// utils_vad.py conventions: Threshold/NegThreshold/MinSilence/
// SpeechPad are VADIterator's defaults, MinSpeech is
// get_speech_timestamps' min_speech_duration_ms (VADIterator has no
// minimum-duration filter; the streaming adaptation below folds it in
// as the onset gate).
const (
	defaultSileroThreshold  = 0.5
	sileroNegOffset         = 0.15 // derived NegThreshold = max(T−0.15, 0.01)
	sileroNegFloor          = 0.01
	defaultSileroMinSpeech  = 250 * time.Millisecond
	defaultSileroMinSilence = 100 * time.Millisecond
	defaultSileroSpeechPad  = 30 * time.Millisecond

	// sileroMinSampleRate is the engine's input-rate floor. The model
	// itself is fixed at 16 kHz; anything ≥ 8 kHz resamples cleanly
	// into its band.
	sileroMinSampleRate = 8000
)

// SileroConfig configures a SileroDetector. The zero value of every
// field except SampleRate and Channels selects the upstream default,
// so SileroConfig{SampleRate: 48000, Channels: 1} is a complete,
// valid configuration.
type SileroConfig struct {
	// SampleRate is the input sample rate in Hz. Must be at least
	// 8000. The model runs at 16 kHz; any other input rate is
	// resampled internally (see the adapter notes in the package doc).
	SampleRate int

	// Channels is the interleaved input channel count, in 1..64.
	// Multi-channel input is downmixed to mono before detection.
	Channels int

	// Threshold is the speech probability at or above which a window
	// counts as speech. The zero value selects 0.5 (the upstream
	// "lazy 0.5"). Must be in (0, 1] and strictly greater than the
	// resolved NegThreshold. An exact 0 selects the default; pass a
	// tiny value (e.g. 0.02) for an extremely permissive detector.
	Threshold float64

	// NegThreshold is the probability below which a window counts as
	// silence once speech is in progress; windows between the two
	// thresholds extend the current state (the VADIterator hysteresis
	// band). The zero value DERIVES it as max(Threshold−0.15, 0.01),
	// and SetThreshold keeps re-deriving it until NegThreshold is set
	// explicitly (here or via SetNegThreshold). Must be in (0, 1) and
	// strictly below Threshold.
	NegThreshold float64

	// MinSpeech is the minimum speech-run duration before SpeechStart
	// fires: shorter bursts are discarded silently, mirroring
	// get_speech_timestamps' min_speech_duration_ms as a streaming
	// onset gate. The fired event is back-timestamped to the run's
	// start (minus SpeechPad), so MinSpeech delays the announcement,
	// not the reported position — it is the dominant term of
	// DecisionLatency. The zero value selects 250 ms. Must not be
	// negative. An exact 0 selects the default; pass time.Nanosecond
	// to fire on the first speech window (VADIterator behaviour).
	MinSpeech time.Duration

	// MinSilence is how long the probability must sit below
	// NegThreshold before SpeechEnd fires (the silence-confirmation
	// wait; windows back above Threshold cancel it, in-band windows
	// leave it running — exact VADIterator semantics). The zero value
	// selects 100 ms. Must not be negative. An exact 0 selects the
	// default; pass time.Nanosecond to end on the first sub-threshold
	// window.
	MinSilence time.Duration

	// SpeechPad pads both edges of every utterance: SpeechStart is
	// back-timestamped an extra SpeechPad before the first speech
	// window, and SpeechEnd is extended SpeechPad past the last one —
	// clamped so consecutive utterances never overlap (a start is
	// clamped to the previous padded end) and never exceed the stream
	// (a start ≥ 0; an end at most the decision position). The zero
	// value selects 30 ms. Must not be negative. An exact 0 selects
	// the default; pass time.Nanosecond for no padding.
	SpeechPad time.Duration
}

// SileroDetector is the neural engine: a pure-Go, dependency-free
// port of the Silero VAD v6.2 fixed graph (vendored MIT weights,
// parity-gated against onnxruntime — see vad/internal/silero and the
// README's verification section). It hears speech as a trained
// posterior rather than an energy or GMM heuristic, making it the
// package's best discriminator against music, tones, and noise — at
// the price of the largest decision latency (~290 ms with defaults;
// see DecisionLatency). For low-latency gating prefer the Energy or
// WebRTC engines.
//
// # Decision pipeline
//
// The mono (downmixed) stream is resampled to 16 kHz unless it
// already is, cut into 512-sample windows (32 ms), and each window
// scored by the model — Probability reports that posterior. Raw
// window decisions then pass through the upstream VADIterator state
// machine (Threshold/NegThreshold hysteresis, MinSilence
// confirmation) with a MinSpeech onset gate and SpeechPad edge
// padding, producing SpeechStart/SpeechEnd events and Active state.
//
// # Concurrency
//
// Process and Reset must be driven from a single goroutine. Every
// reader and every setter (SetThreshold, SetNegThreshold,
// SetMinSpeech, SetMinSilence, SetSpeechPad) is safe from any
// goroutine while Process runs; the Threshold/NegThreshold pair
// shares ONE atomic word, so a torn pair (an inverted hysteresis
// band) is impossible. Threshold values are quantised through
// float32 (< 1e-7 error).
type SileroDetector struct {
	core

	adapter *engineAdapter
	model   *silero.Model
	win     []float32

	// Decision state machine (audio goroutine only), in engine-domain
	// samples (16 kHz). See decide for the exact semantics.
	inRun      bool  // a speech run is in progress (pending or announced)
	announced  bool  // SpeechStart has fired for the current run
	runStart   int64 // engine sample where the run's first window began
	tempEnd    int64 // engine sample where the pending-silence window began
	tempEndSet bool
	lastEndPos int64 // input-frame position of the last SpeechEnd (pad clamp)
	hasEnded   bool  // lastEndPos is meaningful

	// Live parameters.
	thresholds  atomic.Uint64 // packThresholds(threshold, negThreshold)
	negExplicit atomic.Bool   // NegThreshold was set explicitly
	minSpeech   atomic.Int64  // ns
	minSilence  atomic.Int64  // ns
	speechPad   atomic.Int64  // ns
}

// resolveSileroThresholds applies defaults/derivation and validates a
// (threshold, negThreshold) pair. negGiven distinguishes an explicit
// negThreshold (validated as given) from a zero value (derived).
func resolveSileroThresholds(threshold, negThreshold float64) (t, neg float64, negGiven bool, err error) {
	t = threshold
	if t == 0 {
		t = defaultSileroThreshold
	}
	if math.IsNaN(t) || t <= 0 || t > 1 {
		return 0, 0, false, ErrBadThreshold
	}
	neg = negThreshold
	negGiven = neg != 0
	if !negGiven {
		neg = t - sileroNegOffset
		if neg < sileroNegFloor {
			neg = sileroNegFloor
		}
	}
	if math.IsNaN(neg) || neg <= 0 || neg >= 1 {
		return 0, 0, false, ErrBadThreshold
	}
	if neg >= t {
		return 0, 0, false, ErrBadThreshold
	}
	return t, neg, negGiven, nil
}

// NewSileroDetector constructs a SileroDetector from cfg,
// substituting the documented defaults for zero-valued fields. It
// returns ErrBadSampleRate (rate < 8000 or a resample ratio the
// resampler rejects), ErrBadChannels (channels not in 1..64),
// ErrBadThreshold (thresholds outside (0,1], NaN, or a pair without a
// positive hysteresis band), or ErrBadConfig (negative durations).
func NewSileroDetector(cfg SileroConfig) (*SileroDetector, error) {
	if cfg.SampleRate < sileroMinSampleRate {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	threshold, neg, negGiven, err := resolveSileroThresholds(cfg.Threshold, cfg.NegThreshold)
	if err != nil {
		return nil, err
	}
	minSpeech, err := resolveDebounce(cfg.MinSpeech, defaultSileroMinSpeech)
	if err != nil {
		return nil, err
	}
	minSilence, err := resolveDebounce(cfg.MinSilence, defaultSileroMinSilence)
	if err != nil {
		return nil, err
	}
	speechPad, err := resolveDebounce(cfg.SpeechPad, defaultSileroSpeechPad)
	if err != nil {
		return nil, err
	}

	adapter, err := newEngineAdapter(cfg.SampleRate, cfg.Channels, silero.SampleRate, silero.WindowSize)
	if err != nil {
		return nil, err
	}
	model, err := silero.New()
	if err != nil {
		return nil, err
	}

	s := &SileroDetector{
		core:    newCore(cfg.SampleRate, cfg.Channels),
		adapter: adapter,
		model:   model,
		win:     make([]float32, silero.WindowSize),
	}
	s.thresholds.Store(packThresholds(threshold, neg))
	s.negExplicit.Store(negGiven)
	s.minSpeech.Store(int64(minSpeech))
	s.minSilence.Store(int64(minSilence))
	s.speechPad.Store(int64(speechPad))
	return s, nil
}

// Process feeds samples (interleaved, Channels() wide) into the
// detector and leaves the audio itself completely untouched.
// Decisions are made per 512-sample 16 kHz window (32 ms);
// SpeechStart and SpeechEnd events are published synchronously from
// within this call (see the package doc's subscriber contract). Any
// trailing partial frame (a length not divisible by the channel
// count) is ignored. Unlike Energy, non-finite (NaN/Inf) samples are
// NOT special-cased here: they narrow to float32 and are scored by
// the model like any other input, so a NaN sample can propagate into
// the model's posterior (and, through it, the carried LSTM state) per
// upstream Silero behaviour — callers feeding possibly-invalid audio
// should sanitise it upstream if that propagation is unwanted. Never
// panics.
func (s *SileroDetector) Process(samples []float64) {
	s.adapter.push(samples, s.decide)
}

// decide is the per-window engine step (adapter callback): score the
// window, then run one step of the streaming VADIterator adaptation.
//
// The state machine is upstream utils_vad.py, adapted for streaming:
//
//   - p ≥ threshold cancels any pending silence confirmation, and
//     out of a run begins one at the window's start.
//   - p < negThreshold during a run records the first such window
//     (tempEnd); in-band probabilities (neg ≤ p < threshold) leave a
//     pending confirmation running — exactly VADIterator's temp_end.
//   - A run announces SpeechStart as soon as its guaranteed final
//     length exceeds MinSpeech (get_speech_timestamps'
//     min_speech_duration_ms, applied as an onset gate) — checked
//     BEFORE the end-confirmation below fires, so a run that qualifies
//     and whose silence confirms in the very same window (MinSilence
//     ≈ 0) still gets its Start announced ahead of its End, rather
//     than being discarded silently by racing the confirmation.
//   - Once the run has stayed below Threshold for MinSilence past
//     tempEnd, the end confirms there; a run whose end confirms before
//     it ever qualified for MinSpeech is discarded without any event.
//   - Positions are padded by SpeechPad on both edges, clamped
//     non-overlapping against the previous utterance's padded end,
//     to ≥ 0, and (for ends) to the decision position.
func (s *SileroDetector) decide(frame []float64, startSample int64) {
	frameToFloat32(frame, s.win)
	p32, err := s.model.Process(s.win)
	if err != nil {
		// Unreachable: the adapter emits exactly WindowSize samples.
		return
	}
	p := float64(p32)
	s.setProbability(p)
	s.stepFSM(p, startSample)
}

// stepFSM runs one step of the streaming VADIterator adaptation for a
// window already scored at probability p. Split from decide so the
// state machine can be driven directly (e.g. by tests) without going
// through the model.
func (s *SileroDetector) stepFSM(p float64, startSample int64) {
	// Snapshot every live parameter once per window.
	threshold, neg := unpackThresholds(s.thresholds.Load())
	minSpeechS := durationToEngineSamples(s.minSpeech.Load())
	minSilenceS := durationToEngineSamples(s.minSilence.Load())
	pad := time.Duration(s.speechPad.Load())

	if p >= threshold {
		s.tempEndSet = false
		if !s.inRun {
			s.inRun = true
			s.announced = false
			s.runStart = startSample
		}
	}

	if s.inRun && p < neg && !s.tempEndSet {
		s.tempEnd = startSample
		s.tempEndSet = true
	}

	// Announce BEFORE confirming the end: a MinSilence of (near) zero
	// can confirm the end in the very same window that first sets
	// tempEnd, so the announce check must not be skipped by an early
	// return on the confirmation path below (that was the bug: a
	// qualifying run could be discarded with no Start/End at all).
	if s.inRun && !s.announced {
		// The earliest the run can now end: at the pending tempEnd if
		// silence confirmation is running, else no sooner than the end
		// of this window.
		earliestEnd := startSample + int64(silero.WindowSize)
		if s.tempEndSet {
			earliestEnd = s.tempEnd
		}
		if earliestEnd-s.runStart > minSpeechS {
			s.announced = true
			s.emitStart(s.runStart, pad, p)
		}
	}

	if s.inRun && p < neg && startSample-s.tempEnd >= minSilenceS {
		// End confirmed at tempEnd. Discarded silently if the run
		// never got long enough to announce.
		if s.announced {
			s.emitEnd(s.tempEnd, startSample, pad, p)
		}
		s.inRun = false
		s.announced = false
		s.tempEndSet = false
	}
}

// emitStart publishes SpeechStart for the run beginning at engine
// sample runStart, back-timestamped by pad and clamped to ≥ 0 and to
// the previous utterance's padded end.
func (s *SileroDetector) emitStart(runStart int64, pad time.Duration, p float64) {
	pos := s.adapter.inputFrame(runStart) - mutations.DurationToFrames(pad, s.sampleRate)
	if pos < 0 {
		pos = 0
	}
	if s.hasEnded && pos < s.lastEndPos {
		pos = s.lastEndPos
	}
	s.emit(SpeechStart, pos, p)
}

// emitEnd publishes SpeechEnd for speech that stopped at engine
// sample tempEnd, extended by pad and clamped to the decision
// position (the start of the confirming window).
func (s *SileroDetector) emitEnd(tempEnd, decisionSample int64, pad time.Duration, p float64) {
	pos := s.adapter.inputFrame(tempEnd) + mutations.DurationToFrames(pad, s.sampleRate)
	if limit := s.adapter.inputFrame(decisionSample); pos > limit {
		pos = limit
	}
	s.lastEndPos = pos
	s.hasEnded = true
	s.emit(SpeechEnd, pos, p)
}

// durationToEngineSamples converts a live-parameter duration (ns) to
// engine-rate (16 kHz) samples.
func durationToEngineSamples(ns int64) int64 {
	return mutations.DurationToFrames(time.Duration(ns), silero.SampleRate)
}

// Reset returns the detector to its just-constructed state: the
// resampler/framing pipeline, the model's carried context and LSTM
// state, the decision state machine, position counters, and the
// Active/Probability/LastTransition readers are all cleared, and
// event positions restart at frame 0. Configuration — including
// values changed by live setters — is retained, as are bus
// subscriptions.
func (s *SileroDetector) Reset() {
	s.adapter.reset()
	s.model.Reset()
	s.inRun = false
	s.announced = false
	s.tempEndSet = false
	s.lastEndPos = 0
	s.hasEnded = false
	s.resetState()
}

// DecisionLatency reports how long after speech begins the detector
// announces it, worst case: internal resampler group delay + one
// 32 ms window fill (speech starting just after a window boundary is
// first scored a window later) + the MinSpeech onset gate, which
// confirms at the end of the first window whose position exceeds it
// (MinSpeech rounded DOWN to whole windows, plus the confirming
// window). ~288 ms with defaults. It reflects live SetMinSpeech
// changes. Safe from any goroutine.
func (s *SileroDetector) DecisionLatency() time.Duration {
	windows := durationToEngineSamples(s.minSpeech.Load())/silero.WindowSize + 1
	gate := mutations.FramesToDuration(windows*silero.WindowSize, silero.SampleRate)
	return s.adapter.latencyBase() + gate
}

// SetThreshold atomically replaces the speech threshold. Unless
// NegThreshold has been set explicitly (in the config or via
// SetNegThreshold), the negative threshold is re-derived as
// max(Threshold−0.15, 0.01); the pair is stored in a single atomic
// word, so the audio goroutine always sees a coherent pair. Zero
// selects the 0.5 default. Returns ErrBadThreshold for values outside
// (0, 1] or a pair without a positive hysteresis band. Safe from any
// goroutine.
func (s *SileroDetector) SetThreshold(threshold float64) error {
	if s.negExplicit.Load() {
		_, neg := unpackThresholds(s.thresholds.Load())
		t, neg, _, err := resolveSileroThresholds(threshold, neg)
		if err != nil {
			return err
		}
		s.thresholds.Store(packThresholds(t, neg))
		return nil
	}
	t, neg, _, err := resolveSileroThresholds(threshold, 0)
	if err != nil {
		return err
	}
	s.thresholds.Store(packThresholds(t, neg))
	return nil
}

// SetNegThreshold atomically replaces the negative (silence)
// threshold and marks it explicit: subsequent SetThreshold calls stop
// re-deriving it. Zero restores the derived behaviour
// (max(Threshold−0.15, 0.01), tracking future SetThreshold calls).
// Returns ErrBadThreshold for values outside (0, 1) or not strictly
// below the current Threshold. Safe from any goroutine.
func (s *SileroDetector) SetNegThreshold(negThreshold float64) error {
	t, _ := unpackThresholds(s.thresholds.Load())
	t, neg, negGiven, err := resolveSileroThresholds(t, negThreshold)
	if err != nil {
		return err
	}
	s.negExplicit.Store(negGiven)
	s.thresholds.Store(packThresholds(t, neg))
	return nil
}

// SetMinSpeech atomically replaces the minimum speech duration (the
// onset gate). Zero selects the 250 ms default; pass time.Nanosecond
// to fire on the first speech window. Returns ErrBadConfig for a
// negative duration. Takes effect from the next decision window
// (including for a pending run) and changes DecisionLatency. Safe
// from any goroutine.
func (s *SileroDetector) SetMinSpeech(d time.Duration) error {
	d, err := resolveDebounce(d, defaultSileroMinSpeech)
	if err != nil {
		return err
	}
	s.minSpeech.Store(int64(d))
	return nil
}

// SetMinSilence atomically replaces the silence-confirmation wait.
// Zero selects the 100 ms default; pass time.Nanosecond to end on the
// first sub-threshold window. Returns ErrBadConfig for a negative
// duration. Safe from any goroutine.
func (s *SileroDetector) SetMinSilence(d time.Duration) error {
	d, err := resolveDebounce(d, defaultSileroMinSilence)
	if err != nil {
		return err
	}
	s.minSilence.Store(int64(d))
	return nil
}

// SetSpeechPad atomically replaces the utterance edge padding. Zero
// selects the 30 ms default; pass time.Nanosecond for no padding.
// Returns ErrBadConfig for a negative duration. Safe from any
// goroutine.
func (s *SileroDetector) SetSpeechPad(d time.Duration) error {
	d, err := resolveDebounce(d, defaultSileroSpeechPad)
	if err != nil {
		return err
	}
	s.speechPad.Store(int64(d))
	return nil
}

// SileroDetector implements Detector.
var _ Detector = (*SileroDetector)(nil)
