// Package duplex pairs a paced render (playback) path with a capture
// (microphone) path into one full-duplex voice-session engine: the
// core loop of a realtime voice agent that plays synthesized speech
// while listening for the user, needing echo cancellation, noise
// suppression, VAD-driven speech events, and pre-roll.
//
// # Data flow
//
// One high-precision 10 ms ticker (github.com/daniel-sullivan/go-hpt)
// drives a single audio goroutine that owns ALL DSP state. Each tick:
//
//	render:  jitter read (silence on underrun) → seam crossfade →
//	         RenderChain → ambient mix → clamp → AEC far-end feed →
//	         output callback
//	capture: bounded drain of pushed frames, re-framed to 10 ms →
//	         AEC echo removal → CaptureChain → Detector →
//	         speech-event state machine → Events channel / pre-roll
//
// The render side is fed from any goroutine: FeedChunk appends
// streamed voice audio (e.g. synthesized speech) to a jitter buffer,
// MarkChunkBoundary marks the seam between independently-generated
// chunks for an equal-power crossfade that removes discontinuity
// clicks, and ClearPending drops all queued-but-unplayed audio for a
// barge-in. An optional looping ambient bed is mixed under the voice.
//
// The capture side is fed from any goroutine: Push enqueues capture
// frames (arbitrary lengths; the engine re-frames to 10 ms) paired
// with an opaque int64 tag — typically a capture timestamp — onto a
// bounded queue that sheds the newest frame on overflow.
//
// # Why the engine owns the AEC coupling
//
// aec.Canceller requires FeedFarEnd and Process to be serialized onto
// one goroutine, paced roughly together. The engine internalises that
// contract: the same tick that emits a render frame feeds it to the
// AEC as far-end reference before the output callback fires, and
// capture frames are echo-cancelled on that same goroutine — the
// serialization cannot be misused from outside.
//
// # Speech events
//
// Capture-side events arrive on one FIFO channel (Events). For each
// utterance: SpeechStart (its Timestamp back-dated to the oldest
// pre-roll frame minus the lead-in, clamped at 0), Config.LeadIn
// worth of synthetic silence frames (giving a downstream ASR a beat
// of empty audio to spin up on), the pre-roll replay (the utterance
// onset the detector was still confirming), live post-DSP frames
// while speech lasts, then SpeechStop. Redundant detector transitions
// are absorbed: exactly one SpeechStart and one SpeechStop per
// utterance.
//
// The engine is the Detector's exclusive feeder (see the vad package
// doc's "One feeder per stream") — do not Process the same detector
// elsewhere. Detector tuning setters are lock-free atomics, safe to
// call from any goroutine on the concrete detector the caller
// constructed (reachable via Detector()).
//
// # Scope
//
// The engine speaks interleaved float64 samples in [-1, 1] at one
// sample rate and channel count, like every pipeline package in this
// toolkit. Codecs are out of scope: callers decode network audio
// before Push and encode after the output callback (codec/opus,
// codec/pcm).
package duplex

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	hpt "github.com/daniel-sullivan/go-hpt"

	"github.com/daniel-sullivan/go-mediatoolkit/aec"
	aecconfig "github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/events"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

// frameDuration is the engine's fixed frame length. Everything —
// pacing, jitter slicing, capture re-framing, AEC frames — moves in
// whole 10 ms frames, matching AEC3's native frame size.
const frameDuration = 10 * time.Millisecond

// maxChannels bounds Config.Channels, matching the bound this
// toolkit's other multi-channel constructors use.
const maxChannels = 64

// Defaults substituted for zero-valued Config fields. Wherever a
// literal zero would be meaningful the field doc names the epsilon
// escape hatch (e.g. Crossfade: time.Nanosecond for no fade) — the
// loudness convention.
const (
	defaultLeadIn        = 30 * time.Millisecond
	defaultTagUnit       = time.Millisecond
	defaultCrossfade     = 5 * time.Millisecond
	defaultCaptureBuffer = 2500 * time.Millisecond
	defaultEventBuffer   = 256
)

// AECConfig enables acoustic echo cancellation. The engine builds the
// aec.Canceller itself (rate and channels come from the enclosing
// Config) and owns its two-stream serialization contract entirely.
type AECConfig struct {
	// Tuning selects AEC3's internal tuning parameters; nil selects
	// aec/config.DefaultConfig(). See aec.CancellerConfig.Tuning.
	Tuning *aecconfig.Config
}

// AmbientConfig loops a fixed audio bed under the rendered voice —
// room tone, hold music — so the far side never hears dead air.
type AmbientConfig struct {
	// PCM is the loop asset: interleaved float64 in [-1, 1] at the
	// engine's sample rate and channel count, at least one frame
	// (10 ms) long and a whole multiple of Channels. The loop wraps
	// sample-precise, mid-frame if need be. Cloned at construction.
	PCM []float64

	// Gain is the linear gain applied to the bed before mixing
	// (mutations.Decibels converts from dB). Zero selects 1.0
	// (unity); must not be negative.
	Gain float64
}

// Config configures an Engine. SampleRate, Channels, and Detector are
// required; every other field has a working default.
type Config struct {
	// SampleRate is the session sample rate in Hz. Must be a positive
	// multiple of 100 (whole 10 ms frames); with AEC enabled it must
	// be one of 16000, 32000, or 48000 (the rates AEC3 supports).
	SampleRate int

	// Channels is the interleaved channel count for BOTH paths, in
	// 1..64.
	Channels int

	// Detector is the voice-activity engine driving speech events
	// (vad.NewSileroDetector, vad.NewEnergyDetector, ...). Required;
	// must be constructed for this SampleRate/Channels. The engine
	// becomes its exclusive feeder.
	Detector vad.Detector

	// AEC enables acoustic echo cancellation; nil disables it. When
	// enabled, every rendered frame feeds the canceller as far-end
	// reference and every capture frame is echo-cancelled before the
	// capture chain runs. Note the canceller's fixed 10 ms processing
	// latency (aec.Canceller.Latency): a capture frame's post-DSP
	// content lags its tag by one frame.
	AEC *AECConfig

	// PreRoll is the capture pre-roll window: how much out-of-speech
	// post-DSP audio to retain for replay when speech starts (see
	// vad.PreRoll). Zero disables pre-roll; must not be negative.
	PreRoll time.Duration

	// LeadIn is how much synthetic silence to emit (as AudioFrame
	// events with back-dated tags) between SpeechStart and the
	// pre-roll replay, giving a downstream ASR a beat of empty audio
	// to spin up on. Floored to whole frames. Zero selects 30 ms;
	// pass time.Nanosecond (or anything under one frame) for none.
	// Must not be negative.
	LeadIn time.Duration

	// TagUnit is the duration one capture-tag increment represents,
	// used to back-date the synthetic lead-in tags and the
	// SpeechStart timestamp (e.g. time.Millisecond for
	// milliseconds-since-session-start tags). Zero selects 1 ms.
	// Must not be negative; a TagUnit longer than one frame
	// degenerates to no back-dating.
	TagUnit time.Duration

	// RenderChain is an ordered effects chain applied to every voice
	// frame after the seam crossfade and before the ambient mix
	// (gain, biquad filters, a limiter...). Each processor must be
	// constructed for this SampleRate/Channels and not shared with
	// any other stream; the engine never calls Reset on it.
	RenderChain []mutations.Processor

	// CaptureChain is the capture-side counterpart, applied after
	// echo cancellation and before the detector (e.g. a
	// denoise.Engine). Same construction and ownership rules as
	// RenderChain.
	CaptureChain []mutations.Processor

	// Ambient loops a bed under the voice; nil disables it.
	Ambient *AmbientConfig

	// Crossfade is the equal-power fade window applied across marked
	// chunk seams (MarkChunkBoundary), capped at one frame (10 ms).
	// Zero selects 5 ms; pass time.Nanosecond for a hard cut. Must
	// not be negative.
	Crossfade time.Duration

	// CaptureBuffer bounds the capture queue: pushes beyond this much
	// buffered audio are dropped (ErrCaptureOverflow). Zero selects
	// 2.5 s; floored to one frame. Must not be negative.
	CaptureBuffer time.Duration

	// EventBuffer is the Events channel capacity. The audio goroutine
	// BLOCKS when it is full rather than dropping (a dropped frame
	// would corrupt a downstream ASR transcript), so size it for the
	// consumer's worst-case lag. Zero selects 256; must not be
	// negative.
	EventBuffer int

	// Output, if non-nil, is installed as the initial output callback
	// (see SetOutput).
	Output func(frame []float64, seq int64)
}

// Engine is a running full-duplex voice session. Construct with New,
// drive with Start/Stop; every exported method is safe from any
// goroutine unless its doc says otherwise. An Engine is single-use:
// after Stop it cannot be restarted.
type Engine struct {
	sampleRate   int
	channels     int
	frameSamples int

	detector     vad.Detector
	detectorSub  *events.Subscription[vad.SpeechEvent]
	aec          *aec.Canceller
	renderChain  []mutations.Processor
	captureChain []mutations.Processor

	jitter  *jitterBuffer
	ambient *ambientBed
	capture *captureQueue

	preroll      *vad.PreRoll
	leadInFrames int
	tagsPerFrame int64

	// Tick-owned state (the audio goroutine, or a test driving tick
	// directly).
	renderFrame  []float64
	captureFrame []float64
	captureAccum []float64
	captureSpans []tagSpan
	transitions  []vad.SpeechEvent
	inSpeech     bool

	out atomic.Pointer[func(frame []float64, seq int64)]
	seq atomic.Int64

	events     chan Event
	stop       chan struct{}
	done       chan struct{}
	eventsOnce sync.Once

	mu      sync.Mutex
	started bool
	stopped bool
}

// New constructs an Engine for cfg. It validates every field and
// returns a sentinel error (or, for AEC tuning problems, the aec
// package's error) without starting anything; call Start to begin
// ticking.
func New(cfg Config) (*Engine, error) {
	if cfg.SampleRate <= 0 || cfg.SampleRate%100 != 0 {
		return nil, ErrBadSampleRate
	}
	if cfg.Channels < 1 || cfg.Channels > maxChannels {
		return nil, ErrBadChannels
	}
	if cfg.Detector == nil {
		return nil, ErrNilDetector
	}
	if cfg.Detector.SampleRate() != cfg.SampleRate || cfg.Detector.Channels() != cfg.Channels {
		return nil, ErrDetectorMismatch
	}
	if cfg.AEC != nil {
		switch cfg.SampleRate {
		case 16000, 32000, 48000:
		default:
			return nil, ErrBadSampleRate
		}
	}
	for _, chain := range [][]mutations.Processor{cfg.RenderChain, cfg.CaptureChain} {
		for _, p := range chain {
			if p == nil {
				return nil, ErrNilProcessor
			}
		}
	}
	if cfg.PreRoll < 0 || cfg.LeadIn < 0 || cfg.TagUnit < 0 || cfg.Crossfade < 0 ||
		cfg.CaptureBuffer < 0 || cfg.EventBuffer < 0 {
		return nil, ErrBadConfig
	}

	frameSamples := cfg.SampleRate / 100 * cfg.Channels

	var bed *ambientBed
	if cfg.Ambient != nil {
		pcm := cfg.Ambient.PCM
		if len(pcm) < frameSamples || len(pcm)%cfg.Channels != 0 || cfg.Ambient.Gain < 0 {
			return nil, ErrBadConfig
		}
		gain := cfg.Ambient.Gain
		if gain == 0 {
			gain = 1.0
		}
		bed = &ambientBed{pcm: append([]float64(nil), pcm...), gain: gain}
	}

	var canceller *aec.Canceller
	if cfg.AEC != nil {
		c, err := aec.NewCanceller(aec.CancellerConfig{
			SampleRate:      cfg.SampleRate,
			CaptureChannels: cfg.Channels,
			RenderChannels:  cfg.Channels,
			Tuning:          cfg.AEC.Tuning,
		})
		if err != nil {
			return nil, err
		}
		canceller = c
	}

	// SampleRate/Channels are already validated, and PreRoll >= 0, so
	// NewPreRoll cannot fail here.
	preroll, err := vad.NewPreRoll(vad.PreRollConfig{
		SampleRate: cfg.SampleRate,
		Channels:   cfg.Channels,
		Duration:   cfg.PreRoll,
	})
	if err != nil {
		return nil, err
	}

	leadIn := cfg.LeadIn
	if leadIn == 0 {
		leadIn = defaultLeadIn
	}
	tagUnit := cfg.TagUnit
	if tagUnit == 0 {
		tagUnit = defaultTagUnit
	}
	crossfade := cfg.Crossfade
	if crossfade == 0 {
		crossfade = defaultCrossfade
	}
	if crossfade > frameDuration {
		crossfade = frameDuration
	}
	captureBuffer := cfg.CaptureBuffer
	if captureBuffer == 0 {
		captureBuffer = defaultCaptureBuffer
	}
	eventBuffer := cfg.EventBuffer
	if eventBuffer == 0 {
		eventBuffer = defaultEventBuffer
	}

	fadeSamples := int(mutations.DurationToFrames(crossfade, cfg.SampleRate)) * cfg.Channels
	capSamples := max(int(mutations.DurationToFrames(captureBuffer, cfg.SampleRate))*cfg.Channels, frameSamples)

	e := &Engine{
		sampleRate:   cfg.SampleRate,
		channels:     cfg.Channels,
		frameSamples: frameSamples,
		detector:     cfg.Detector,
		aec:          canceller,
		renderChain:  append([]mutations.Processor(nil), cfg.RenderChain...),
		captureChain: append([]mutations.Processor(nil), cfg.CaptureChain...),
		jitter:       newJitterBuffer(frameSamples, cfg.Channels, fadeSamples),
		ambient:      bed,
		capture:      newCaptureQueue(capSamples),
		preroll:      preroll,
		leadInFrames: int(leadIn / frameDuration),
		tagsPerFrame: int64(frameDuration / tagUnit),
		renderFrame:  make([]float64, frameSamples),
		captureFrame: make([]float64, frameSamples),
		events:       make(chan Event, eventBuffer),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	if cfg.Output != nil {
		e.SetOutput(cfg.Output)
	}

	// The detector publishes transitions synchronously from Process —
	// which only the audio goroutine calls — so appending to the
	// tick-owned transitions slice here is single-goroutine.
	e.detectorSub = e.detector.Events().Subscribe(func(ev vad.SpeechEvent) {
		e.transitions = append(e.transitions, ev)
	})
	return e, nil
}

// Start launches the paced audio goroutine. The engine stops when ctx
// is cancelled or Stop is called, whichever comes first. Returns
// ErrEngineStarted if already running, ErrEngineStopped after Stop.
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.stopped {
		return ErrEngineStopped
	}
	if e.started {
		return ErrEngineStarted
	}
	e.started = true
	go e.run(ctx)
	return nil
}

// Stop halts the audio goroutine, cancels the detector subscription,
// and closes the Events channel (already-queued events remain
// readable; an event the audio goroutine was still blocked on is
// discarded). Idempotent, safe from any goroutine, returns after the
// audio goroutine has exited.
func (e *Engine) Stop() {
	e.mu.Lock()
	first := !e.stopped
	started := e.started
	e.stopped = true
	e.mu.Unlock()

	if first {
		close(e.stop)
	}
	if started {
		<-e.done
	} else {
		e.finalize()
	}
}

// run is the audio goroutine: one tick per frameDuration on a
// high-precision ticker until stopped.
func (e *Engine) run(ctx context.Context) {
	defer close(e.done)
	defer e.finalize()

	ticker := hpt.NewTicker(frameDuration)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stop:
			return
		case <-ticker.C:
			e.tick()
		}
	}
}

// finalize releases session resources exactly once: the detector
// subscription and the Events channel.
func (e *Engine) finalize() {
	e.eventsOnce.Do(func() {
		e.detectorSub.Cancel()
		close(e.events)
	})
}

// tick advances the session by one 10 ms frame: render first (so the
// AEC always sees this tick's far-end frame before capture), then
// capture.
func (e *Engine) tick() {
	e.renderTick()
	e.captureTick()
}

// renderTick emits one paced output frame: jitter read (silence on
// underrun) → seam crossfade (inside the jitter read) → render chain
// → ambient mix → clamp → AEC far-end → output callback.
func (e *Engine) renderTick() {
	frame := e.renderFrame
	e.jitter.read(frame)
	for _, p := range e.renderChain {
		p.Process(frame)
	}
	if e.ambient != nil {
		e.ambient.mixInto(frame)
	}
	mutations.ApplySaturator(frame, mutations.HardClip)

	if e.aec != nil {
		// The frame is engine-built and always channel-aligned, the
		// one condition FeedFarEnd can reject.
		_ = e.aec.FeedFarEnd(frame)
	}

	seq := e.seq.Add(1)
	if out := e.out.Load(); out != nil {
		(*out)(frame, seq)
	}
}

// FeedChunk appends a chunk of voice audio (interleaved, any length —
// e.g. one streamed synthesis packet) to the render jitter buffer,
// where it is sliced into 10 ms frames; a trailing partial frame
// waits for the next chunk to complete it. Safe from any goroutine.
// Returns ErrBadFrame if len(pcm) is not a whole multiple of
// Channels.
func (e *Engine) FeedChunk(pcm []float64) error {
	if len(pcm)%e.channels != 0 {
		return ErrBadFrame
	}
	if len(pcm) == 0 {
		return nil
	}
	e.jitter.feed(pcm)
	return nil
}

// MarkChunkBoundary marks a seam between independently-generated
// chunks: a pending partial frame is flushed (zero-padded), and the
// first frame of the NEXT chunk fed — however far behind the paced
// reader is running — is blended equal-power against the tail of the
// frame played before it, removing the discontinuity click two
// unrelated generations meet with. Safe from any goroutine.
func (e *Engine) MarkChunkBoundary() {
	e.jitter.markBoundary()
}

// ClearPending immediately drops all queued-but-unplayed voice audio,
// including any partial frame and pending seam — the barge-in
// primitive. Audio already handed to the output callback is gone;
// ambient (if any) keeps playing. Safe from any goroutine.
func (e *Engine) ClearPending() {
	e.jitter.clear()
}

// SetOutput installs (or, with nil, removes) the output callback the
// audio goroutine invokes with each paced frame and its sequence
// number. The frame slice is reused by the next tick — the callback
// must copy anything it keeps, and must return quickly (it runs on
// the audio goroutine; encode-and-send is fine, blocking I/O is not).
// seq increments by exactly 1 per tick from 1. Safe from any
// goroutine.
func (e *Engine) SetOutput(fn func(frame []float64, seq int64)) {
	if fn == nil {
		e.out.Store(nil)
		return
	}
	e.out.Store(&fn)
}

// Push enqueues one capture frame (interleaved, any length that is a
// whole multiple of Channels) tagged tag onto the bounded capture
// queue; the engine re-frames internally to 10 ms, assigning each
// internal frame the tag of the push its first sample came from
// (push exact 10 ms frames for exact tags). frame is copied. Safe
// from any goroutine.
//
// Returns ErrBadFrame for a misaligned length, or ErrCaptureOverflow
// when the queue is full — the frame is then dropped (drop-newest)
// and counted in Metrics().CaptureFramesDropped rather than blocking
// a network receive loop.
func (e *Engine) Push(frame []float64, tag int64) error {
	if len(frame)%e.channels != 0 {
		return ErrBadFrame
	}
	if len(frame) == 0 {
		return nil
	}
	return e.capture.push(frame, tag)
}

// Events returns the FIFO speech-event channel. It is closed by Stop
// (buffered events remain readable). See the package doc for the
// per-utterance sequence and Config.EventBuffer for the
// block-don't-drop delivery contract.
func (e *Engine) Events() <-chan Event {
	return e.events
}

// Detector returns the detector the engine was configured with, for
// live tuning: detector setters (e.g. SileroDetector.SetThreshold)
// are lock-free atomics, safe from any goroutine while the engine
// runs. Do NOT call Process or Reset on it — the engine is its
// exclusive feeder.
func (e *Engine) Detector() vad.Detector {
	return e.detector
}

// SetVADThreshold forwards v to the detector's SetThreshold — the
// speech-probability knob on probability-scored engines
// (vad.SileroDetector). The setter is a lock-free atomic on the
// detector, so it is safe from any goroutine while the engine runs
// and takes effect at the detector's next decision window: adjusting
// it live mid-stream — even mid-utterance — is supported (e.g.
// raising the bar while known far-end audio plays). Returns
// ErrTuningUnsupported when the configured detector has no such
// setter (e.g. the Energy engine, whose thresholds are a dB pair —
// tune it directly via Detector()); other errors are the detector's
// own validation.
func (e *Engine) SetVADThreshold(v float64) error {
	det, ok := e.detector.(interface{ SetThreshold(float64) error })
	if !ok {
		return ErrTuningUnsupported
	}
	return det.SetThreshold(v)
}

// SetVADMinSilence forwards d to the detector's SetMinSilence — how
// long the probability must sit below the release threshold before
// the utterance closes (the endpointing wait). Like SetVADThreshold
// it is a lock-free atomic on the detector: safe from any goroutine,
// effective at the next decision window, and explicitly supported
// mid-utterance — e.g. lengthening the wait while a long-form answer
// is expected, shortening it for rapid turn-taking. Returns
// ErrTuningUnsupported when the configured detector has no such
// setter; other errors are the detector's own validation.
func (e *Engine) SetVADMinSilence(d time.Duration) error {
	det, ok := e.detector.(interface{ SetMinSilence(time.Duration) error })
	if !ok {
		return ErrTuningUnsupported
	}
	return det.SetMinSilence(d)
}

// SetAudioBufferDelay forwards an external render→capture delay
// estimate to the echo canceller (see aec.Canceller.
// SetAudioBufferDelay), helping its delay estimator converge faster.
// A no-op when AEC is disabled. Safe from any goroutine.
func (e *Engine) SetAudioBufferDelay(delay time.Duration) {
	if e.aec != nil {
		e.aec.SetAudioBufferDelay(delay)
	}
}

// Metrics is a snapshot of the engine's counters, safe to read from
// any goroutine. Each field is individually consistent, but the
// snapshot is not transactional: the fields are sampled from
// independently-synchronized sources, so under concurrent activity
// two fields can reflect instants a few microseconds apart.
type Metrics struct {
	// AECEnabled reports whether echo cancellation is configured;
	// when false, AEC is the zero value.
	AECEnabled bool

	// AEC is the canceller's snapshot: ERL/ERLE, estimated delay,
	// clockdrift, dropped render frames.
	AEC aec.Metrics

	// CaptureQueued is how much pushed capture audio is waiting to be
	// drained by the audio goroutine.
	CaptureQueued time.Duration

	// CaptureFramesDropped counts Push calls rejected with
	// ErrCaptureOverflow.
	CaptureFramesDropped uint64

	// RenderedFrames counts paced output frames emitted since Start
	// (the current output sequence number).
	RenderedFrames int64
}

// Metrics returns the current snapshot.
func (e *Engine) Metrics() Metrics {
	m := Metrics{
		CaptureQueued:        mutations.FramesToDuration(int64(e.capture.queued()/e.channels), e.sampleRate),
		CaptureFramesDropped: e.capture.dropped.Load(),
		RenderedFrames:       e.seq.Load(),
	}
	if e.aec != nil {
		m.AECEnabled = true
		m.AEC = e.aec.Metrics()
	}
	return m
}
