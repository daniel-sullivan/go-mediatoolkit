package vad

import (
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// WebRTC engine defaults, substituted for the zero value of the
// corresponding WebRTCConfig field.
const (
	defaultWebRTCFrameDuration = 20 * time.Millisecond
	defaultWebRTCOnsetFrames   = 2 // engine decision frames
	defaultWebRTCHangover      = 200 * time.Millisecond

	// webRTCMinSampleRate is the engine's rate floor. Any rate ≥ 8 kHz
	// is accepted: the native libfvad rates pass through, everything
	// else is resampled to 16 kHz.
	webRTCMinSampleRate = 8000

	// webRTCEngineRate is the rate non-native inputs are resampled to.
	// 16 kHz (not 8 k) so none of the 0–4 kHz bands the GMM's features
	// live in are lost before the engine's own internal decimation.
	webRTCEngineRate = 16000
)

// webRTCNativeRate reports whether libfvad accepts rate directly.
func webRTCNativeRate(rate int) bool {
	switch rate {
	case 8000, 16000, 32000, 48000:
		return true
	}
	return false
}

// WebRTCConfig configures a WebRTCDetector. The zero value of every
// field except SampleRate and Channels selects the documented default,
// so WebRTCConfig{SampleRate: 48000, Channels: 1} is a complete, valid
// configuration. Note Mode's zero value IS a valid mode (0, "quality")
// — it doubles as the default, so there is no epsilon hatch to need.
type WebRTCConfig struct {
	// SampleRate is the input sample rate in Hz. Must be at least
	// 8000. The libfvad-native rates (8000, 16000, 32000, 48000) are
	// fed to the engine unresampled; any other rate is resampled to
	// 16 kHz internally (event positions are then accurate to within
	// one engine decision frame — see SpeechEvent.Frame).
	SampleRate int

	// Channels is the interleaved input channel count, in 1..64.
	// Multi-channel input is downmixed to mono before detection (2ch:
	// L/R average; >2ch: equal-weight average).
	Channels int

	// Mode is the libfvad operating ("aggressiveness") mode, 0..3:
	// 0 "quality" (the default), 1 "low bitrate", 2 "aggressive",
	// 3 "very aggressive". A higher mode is more restrictive in
	// reporting speech — fewer false positives, more missed detections.
	Mode int

	// FrameDuration is the engine decision frame length — exactly
	// 10, 20 or 30 ms (the only lengths libfvad accepts). The zero
	// value selects 20 ms. Shorter frames decide sooner; longer frames
	// see more context per decision.
	FrameDuration time.Duration

	// Onset is the debounce for SpeechStart: the engine must report
	// voice for this long (consecutive decision frames, rounded to the
	// nearest frame) before the transition fires. The fired event is
	// back-timestamped to the start of the run. The zero value selects
	// 2 engine frames (2 × FrameDuration). Must not be negative. An
	// exact 0 selects the default; pass time.Nanosecond for no debounce
	// (a single voiced frame fires the transition).
	Onset time.Duration

	// Hangover is the debounce for SpeechEnd: the engine must report
	// silence this long before the transition fires, bridging stop
	// closures and inter-word gaps. This is IN ADDITION to libfvad's
	// own internal overhang smoothing, so the effective gap-bridging is
	// slightly longer than the configured value. The zero value selects
	// 200 ms. Must not be negative. An exact 0 selects the default;
	// pass time.Nanosecond for no hangover.
	Hangover time.Duration
}

// WebRTCDetector is the WebRTC-GMM engine: a bit-exact pure-Go port of
// libfvad (the WebRTC project's legacy voice-activity detector, as
// packaged by github.com/dpirch/libfvad — see vad/libfvad/VERSION).
// Unlike the Energy engine it is a trained speech model: a Gaussian
// mixture over six sub-band log-energies (80 Hz–4 kHz), adapted online,
// with a likelihood-ratio hypothesis test per decision frame. It
// discriminates speech from steady tones, hum, and stationary noise far
// better than an energy threshold, at nearly the same latency and cost.
// It is not immune to tonal false positives, though — a pure tone can
// still fool a Gaussian-mixture-over-energies model; Silero is the
// discriminating engine when that matters more than latency/cost.
//
// # Pipeline
//
// Input is downmixed to mono; the libfvad-native rates 8/16/32/48 kHz
// pass straight through while any other rate is resampled to 16 kHz
// (SincFastest); the mono engine-rate stream is cut into FrameDuration
// decision frames, converted to int16, and fed to the ported engine.
// The engine's binary decisions then pass through the package-level
// Onset/Hangover state machine to become SpeechStart/SpeechEnd events
// and the Active() state. Probability() reports a binary 0/1 mirroring
// Active(), as for every non-neural engine.
//
// # Concurrency
//
// Process and Reset must be driven from a single goroutine. Every
// reader (Active, Probability, LastTransition, DecisionLatency, Events)
// and every setter (SetMode, SetOnset, SetHangover) is safe from any
// goroutine while Process runs: parameters live in single-word atomics
// snapshotted once per decision frame.
//
// # Latency
//
// DecisionLatency = resampler group delay (zero at the native rates) +
// one decision frame fill + the current Onset. With defaults at a
// native rate that is 20 ms + 40 ms = 60 ms. Feed Process small buffers
// to keep the announcement delay near that bound; a large buffer
// batches decisions at its end.
type WebRTCDetector struct {
	core

	adapter *engineAdapter
	vad     *fvad.VAD
	hyst    hysteresis

	engineRate int
	frameSize  int   // engine samples per decision frame
	frameDurNS int64 // decision frame duration in ns

	i16 []int16 // per-frame conversion scratch

	appliedMode int // engine's current mode (audio-goroutine local)

	// Live parameters (written by setters on any goroutine, snapshotted
	// once per decision frame by the audio goroutine).
	mode     atomic.Int64
	onset    atomic.Int64 // resolved duration, ns
	hangover atomic.Int64 // resolved duration, ns
}

// NewWebRTCDetector constructs a WebRTCDetector from cfg, substituting
// the documented defaults for zero-valued fields. It returns
// ErrBadSampleRate (rate < 8000, or a rate the internal resampler
// cannot reach 16 kHz from), ErrBadChannels (channels not in 1..64),
// ErrBadMode (mode outside 0..3), ErrBadFrameDuration (a non-zero
// duration other than 10/20/30 ms), or ErrBadConfig (negative
// onset/hangover).
func NewWebRTCDetector(cfg WebRTCConfig) (*WebRTCDetector, error) {
	if cfg.SampleRate < webRTCMinSampleRate {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	if cfg.Mode < 0 || cfg.Mode > 3 {
		return nil, ErrBadMode
	}
	frameDur := cfg.FrameDuration
	if frameDur == 0 {
		frameDur = defaultWebRTCFrameDuration
	}
	switch frameDur {
	case 10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond:
	default:
		return nil, ErrBadFrameDuration
	}
	onset, err := resolveDebounce(cfg.Onset, defaultWebRTCOnsetFrames*frameDur)
	if err != nil {
		return nil, err
	}
	hangover, err := resolveDebounce(cfg.Hangover, defaultWebRTCHangover)
	if err != nil {
		return nil, err
	}

	engineRate := webRTCEngineRate
	if webRTCNativeRate(cfg.SampleRate) {
		engineRate = cfg.SampleRate
	}
	frameSize := engineRate / 1000 * int(frameDur/time.Millisecond)

	adapter, err := newEngineAdapter(cfg.SampleRate, cfg.Channels, engineRate, frameSize)
	if err != nil {
		return nil, err
	}

	// The engine itself: rate and mode were validated above, so the
	// setters cannot fail.
	engine, err := fvad.New()
	if err != nil {
		return nil, err
	}
	if err := engine.SetSampleRate(engineRate); err != nil {
		return nil, err
	}
	if err := engine.SetMode(cfg.Mode); err != nil {
		return nil, err
	}

	w := &WebRTCDetector{
		core:        newCore(cfg.SampleRate, cfg.Channels),
		adapter:     adapter,
		vad:         engine,
		engineRate:  engineRate,
		frameSize:   frameSize,
		frameDurNS:  int64(frameDur),
		i16:         make([]int16, frameSize),
		appliedMode: cfg.Mode,
	}
	w.mode.Store(int64(cfg.Mode))
	w.onset.Store(int64(onset))
	w.hangover.Store(int64(hangover))
	return w, nil
}

// Process feeds samples (interleaved, Channels() wide) into the
// detector and leaves the audio itself completely untouched — the
// detector is pass-through, so it can sit in any effects chain purely
// to observe. Decisions are made per FrameDuration engine frame;
// SpeechStart and SpeechEnd events are published synchronously from
// within this call (see the package doc's subscriber contract). Any
// trailing partial frame (a length not divisible by the channel count)
// is ignored. A non-finite (NaN/Inf) sample is not clamped like an
// in-range one (frameToInt16's clamp compares against ±1, which NaN
// never satisfies) before its int16 conversion; that conversion's
// result for a non-finite float is implementation-specific per the Go
// spec — it happens to be 0 on every platform this package targets,
// which reads as silence to the engine, but that is an observed
// behaviour of the toolchain, not a guarantee this package makes.
// Never panics.
func (w *WebRTCDetector) Process(samples []float64) {
	w.adapter.push(samples, w.decide)
}

// decide is the per-decision-frame engine step (adapter callback).
func (w *WebRTCDetector) decide(frame []float64, startSample int64) {
	// Snapshot every live parameter once, up front: the whole frame
	// decision sees one coherent parameter set.
	if m := int(w.mode.Load()); m != w.appliedMode {
		// Validated by SetMode, so this cannot fail.
		_ = w.vad.SetMode(m)
		w.appliedMode = m
	}
	onsetFrames := debounceFrames(w.onset.Load(), w.frameDurNS)
	hangoverFrames := debounceFrames(w.hangover.Load(), w.frameDurNS)

	frameToInt16(frame, w.i16)
	// The frame length is an engine constant validated at construction,
	// so Process cannot fail.
	raw, _ := w.vad.Process(w.i16[:w.frameSize])

	kind, eventFrame, fired := w.hyst.step(raw, w.adapter.inputFrame(startSample), onsetFrames, hangoverFrames)
	if fired {
		w.emit(kind, eventFrame, binaryProbability(kind))
	}
}

// Reset returns the detector to its just-constructed state: the engine
// model, resampler/framing pipeline, hysteresis state, position
// counters, and the Active/Probability/LastTransition readers are all
// cleared, and event positions restart at frame 0. Configuration —
// including values changed by live setters — is retained, as are bus
// subscriptions.
func (w *WebRTCDetector) Reset() {
	w.adapter.reset()
	// fvad's own Reset also resets its mode and sample rate to library
	// defaults; re-apply the detector's configuration (the Detector
	// contract retains it). Both values were validated, so neither
	// setter can fail.
	w.vad.Reset()
	_ = w.vad.SetSampleRate(w.engineRate)
	w.appliedMode = int(w.mode.Load())
	_ = w.vad.SetMode(w.appliedMode)
	w.hyst.reset()
	w.resetState()
}

// DecisionLatency reports how long after speech begins the detector
// announces it, worst case: the internal resampler group delay (zero
// at the native 8/16/32/48 kHz rates) plus one decision frame fill
// plus the current Onset debounce. It reflects live SetOnset changes.
// Safe from any goroutine.
func (w *WebRTCDetector) DecisionLatency() time.Duration {
	return w.adapter.latencyBase() + time.Duration(w.onset.Load())
}

// SetMode atomically replaces the libfvad aggressiveness mode (0..3).
// Takes effect from the next decision frame; the engine's adapted
// speech/noise models are retained across the switch (only the decision
// thresholds change), exactly as in libfvad. Returns ErrBadMode for a
// mode outside 0..3. Safe from any goroutine.
func (w *WebRTCDetector) SetMode(mode int) error {
	if mode < 0 || mode > 3 {
		return ErrBadMode
	}
	w.mode.Store(int64(mode))
	return nil
}

// SetOnset atomically replaces the SpeechStart debounce. Zero selects
// the default (2 engine frames, i.e. 2 × FrameDuration); pass
// time.Nanosecond for no debounce. Returns ErrBadConfig for a negative
// duration. Takes effect from the next decision frame and changes
// DecisionLatency. Safe from any goroutine.
func (w *WebRTCDetector) SetOnset(d time.Duration) error {
	d, err := resolveDebounce(d, defaultWebRTCOnsetFrames*time.Duration(w.frameDurNS))
	if err != nil {
		return err
	}
	w.onset.Store(int64(d))
	return nil
}

// SetHangover atomically replaces the SpeechEnd debounce. Zero selects
// the 200 ms default; pass time.Nanosecond for no hangover (libfvad's
// internal overhang smoothing still applies). Returns ErrBadConfig for
// a negative duration. Takes effect from the next decision frame. Safe
// from any goroutine.
func (w *WebRTCDetector) SetHangover(d time.Duration) error {
	d, err := resolveDebounce(d, defaultWebRTCHangover)
	if err != nil {
		return err
	}
	w.hangover.Store(int64(d))
	return nil
}

// WebRTCDetector implements Detector.
var _ Detector = (*WebRTCDetector)(nil)
