package aec

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// maxChannels bounds CancellerConfig.CaptureChannels/RenderChannels,
// matching the bound this toolkit's other multi-channel constructors use
// (e.g. loudness.LimiterConfig.Channels, resample's maxChannels) — large
// enough for any real use, small enough to catch an obviously-wrong
// argument (e.g. a byte count passed where a channel count was meant).
const maxChannels = 64

// CancellerConfig configures a Canceller.
type CancellerConfig struct {
	// SampleRate is the stream sample rate in Hz. Must be 16000, 32000,
	// or 48000 (the three rates AEC3's frequency-band splitting
	// supports); any other value is rejected with ErrBadArg.
	SampleRate int

	// CaptureChannels is the interleaved channel count Process will be
	// driven with. Must be in 1..64.
	CaptureChannels int

	// RenderChannels is the interleaved channel count FeedFarEnd will be
	// driven with. Must be in 1..64. It need not equal CaptureChannels.
	RenderChannels int

	// Tuning selects AEC3's internal tuning parameters (filter length/
	// rate, suppressor aggressiveness, echo audibility thresholds, and
	// every other field of config.Config — a field-for-field mirror of
	// upstream's EchoCanceller3Config; see the aec/config package doc).
	// nil (the zero value) selects config.DefaultConfig(), matching
	// every prior release's behaviour exactly — an existing caller that
	// never sets Tuning sees no change.
	//
	// A non-nil Tuning is validated with config.Validate before use:
	// NewCanceller runs Validate on a copy and, if Validate had to clamp
	// anything (the same tolerant, in-place clamping upstream's own
	// Validate performs — see config.Validate's doc comment), returns
	// ErrBadArg naming the offending field and its clamped value rather
	// than silently substituting the clamped config. Pass a value copied
	// from config.DefaultConfig() and tweaked, not a hand-built zero
	// value — most fields have no sensible zero default.
	//
	// Tuning has no field of its own for sample rate or channel count
	// (upstream's channel-count-related MultiChannel field is
	// intentionally unported — see aec/config's package doc), so it
	// never overlaps or conflicts with SampleRate/CaptureChannels/
	// RenderChannels above: those three remain the only way to configure
	// rate/channels, exactly as before.
	Tuning *config.Config
}

// ClockdriftLevel reports how confident the canceller's delay estimator
// is that the render and capture streams are running on clocks that are
// slowly drifting apart (as opposed to a fixed, if unknown, delay) —
// e.g. render and capture sourced from independent audio devices with
// slightly different crystal oscillators. This is a Go-port-only
// public-facing mirror of internal/aec3's ClockdriftLevel (itself a
// direct port of ClockdriftDetector::Level), kept as a distinct type
// here so the public API never leaks a type from an internal package.
type ClockdriftLevel int

const (
	// ClockdriftLevelNone: no clockdrift currently detected.
	ClockdriftLevelNone ClockdriftLevel = iota
	// ClockdriftLevelProbable: clockdrift is suspected but not yet
	// confirmed.
	ClockdriftLevelProbable
	// ClockdriftLevelVerified: clockdrift is confirmed.
	ClockdriftLevelVerified
)

func fromInternalClockdrift(l aec3.ClockdriftLevel) ClockdriftLevel {
	switch l {
	case aec3.ClockdriftLevelProbable:
		return ClockdriftLevelProbable
	case aec3.ClockdriftLevelVerified:
		return ClockdriftLevelVerified
	default:
		return ClockdriftLevelNone
	}
}

// Metrics is a snapshot of the canceller's current internal state,
// updated once per processed capture frame (see Metrics(), Process).
type Metrics struct {
	// EchoReturnLoss is the ratio, in dB, of the estimated echo power to
	// the actual residual echo power after linear (adaptive-filter)
	// cancellation: how much echo the linear stage removed. Higher is
	// better.
	EchoReturnLoss float64

	// EchoReturnLossEnhancement is the ratio, in dB, of the residual
	// echo power after linear cancellation to the residual echo power
	// after the subsequent nonlinear suppressor stage: how much
	// additional echo the suppressor removed. Higher is better.
	EchoReturnLossEnhancement float64

	// DelayMs is the currently estimated render-to-capture delay, in
	// milliseconds.
	DelayMs int

	// Clockdrift reports whether the render and capture streams are
	// suspected or confirmed to be running on drifting clocks.
	Clockdrift ClockdriftLevel

	// RenderFramesDropped is the cumulative number of 10ms render
	// frames FeedFarEnd has discarded because they were fed too far
	// ahead of Process — see FeedFarEnd's doc comment for the ~1s
	// pacing contract this counts violations of. A well-paced caller
	// (feeding render and capture at roughly the same rate) should
	// always see this stay at 0; a nonzero or growing value means
	// FeedFarEnd is being called faster than Process, and the
	// discarded render audio's echo will not be cancelled from the
	// corresponding capture frames.
	RenderFramesDropped uint64
}

// Canceller removes an estimated acoustic echo of a known far-end
// (loudspeaker) signal from a near-end (microphone) capture signal, in
// place, using a 1:1 Go port of WebRTC's AEC3 (see the package doc
// comment).
//
// # Units
//
// Every sample Canceller's public methods accept or produce — both
// FeedFarEnd's samples and Process's samples — is a normalized float64
// in [-1, 1], the convention every other Processor in this toolkit
// uses. Internally, AEC3 operates in the "FloatS16" domain (audio
// scaled to the int16 range, ~[-32768, 32767], but stored as a
// floating-point value) that the underlying C++ implementation uses
// throughout; Canceller converts at its boundary, multiplying by
// 32768.0 on the way in and dividing by 32768.0 on the way out (see
// internal/aec3/audio_buffer.go and internal/aec3/bandsplit.go's
// FloatS16ToS16/S16ToFloatS16 for where the underlying port applies the
// same convention one layer down).
//
// # Two-stream contract and concurrency
//
// Unlike a typical mutations.Processor, an echo canceller needs two
// input streams: FeedFarEnd (the render/loudspeaker signal) and Process
// (the capture/microphone signal, which Process also mutates in place
// to remove the estimated echo). The underlying AEC3 port
// (internal/aec3.EchoCanceller3) requires FeedFarEnd, Process, and
// SetAudioBufferDelay's *effect* (see below) to be externally
// serialized onto a single goroutine — this is a deliberately stricter
// contract than upstream's own EchoCanceller3, whose class comment
// permits AnalyzeRender (FeedFarEnd's counterpart) to run concurrently
// with everything else via a lock-free SwapQueue. This port replaces
// that SwapQueue with a plain unsynchronized queue (see
// internal/aec3/echo_canceller3.go's drop (3)), so FeedFarEnd and
// Process must both be called from the same one goroutine, one at a
// time, never concurrently with each other.
//
// The one exception is SetAudioBufferDelay, which is safe to call from
// any goroutine at any time, including concurrently with FeedFarEnd/
// Process — it only ever touches an atomic value; the underlying
// engine only observes the update from inside the next Process call,
// on the owning goroutine. Metrics() is likewise safe to call from any
// goroutine at any time: it returns an atomically-published snapshot
// updated once per processed capture frame inside Process, using the
// same lock-free "atomic pointer to an immutable snapshot" pattern
// mixer/track.go uses for its live-readable gain (there, a single
// float64 packed into an atomic.Uint64; here, a whole small struct via
// atomic.Pointer[Metrics], since Metrics has more than one field).
//
// # Pacing
//
// FeedFarEnd's render frames are buffered until a paired Process call
// drains them (see internal/aec3/echo_canceller3.go's renderQueue);
// that queue holds at most 100 10ms frames (~1s). A caller that feeds
// render more than ~1s ahead of capture — e.g. bursts all of a clip's
// render audio through FeedFarEnd before ever calling Process — will
// have the excess frames silently dropped rather than buffered
// without bound, matching upstream AEC3's own SwapQueue-based
// RenderTransferQueue, which fails silently on overflow the same way.
// Dropped frames are counted in Metrics().RenderFramesDropped so a
// caller can detect a pacing bug instead of just seeing degraded echo
// cancellation. Keep FeedFarEnd and Process paced roughly together
// (interleaved at a similar rate, as in a live full-duplex call) to
// stay well under this cap.
//
// # Latency
//
// Latency reports Canceller's fixed, sample-rate-independent processing
// latency: exactly one 10ms AEC3 frame (see Latency's doc comment for
// the derivation, including a deliberate, documented deviation from
// this port's original governing plan).
type Canceller struct {
	cfg       CancellerConfig
	numFrames int // one full AEC3 frame, in samples per channel: SampleRate/100

	// tuning is the resolved, already-validated config.Config the engine
	// is built with: cfg.Tuning's validated copy, or config.DefaultConfig()
	// if cfg.Tuning was nil. Resolved once, in NewCanceller, so a later
	// mutation of the config.Config a caller's cfg.Tuning pointer refers
	// to can never change a running Canceller's behaviour, and so Reset
	// (via buildEngineAndBuffers) never needs to re-validate.
	tuning config.Config

	engine *aec3.EchoCanceller3

	// renderAccum holds render samples (interleaved, normalized
	// float64) fed via FeedFarEnd that have not yet accumulated into a
	// complete numFrames-sample frame.
	renderAccum []float64
	renderFrame [][]float32 // scratch, [RenderChannels][numFrames], FloatS16

	// captureAccum holds capture samples (interleaved, normalized
	// float64) fed via Process that have not yet accumulated into a
	// complete numFrames-sample frame.
	captureAccum []float64
	captureFrame [][]float32 // scratch, [CaptureChannels][numFrames], FloatS16
	outScratch   []float64   // scratch, one processed frame, interleaved

	// outQueue is the output delay line: already-processed capture
	// samples (interleaved, normalized float64) awaiting return to the
	// caller via Process. Primed at construction (and by Reset) with
	// exactly numFrames*CaptureChannels zero samples — see Latency's
	// doc comment for why this specific size is both necessary and
	// sufficient to make Process's round-trip latency exactly Latency()
	// regardless of how the caller chunks its calls.
	outQueue []float64

	// delayGen/pendingDelayMs/appliedDelayGen implement
	// SetAudioBufferDelay's live-safe, lock-free hand-off to the
	// owning goroutine: SetAudioBufferDelay (any goroutine) stores the
	// new value and bumps delayGen; Process (owning goroutine) applies
	// it to engine and records appliedDelayGen whenever the two
	// diverge. Plain fields (not atomics) would race under this
	// pattern; delayGen/pendingDelayMs are atomic so any goroutine can
	// publish a new value without a lock, while appliedDelayGen is
	// touched only from the owning goroutine and needs no
	// synchronization of its own.
	delayGen        atomic.Uint64
	pendingDelayMs  atomic.Int64
	appliedDelayGen uint64

	// metricsPtr publishes an immutable Metrics snapshot after every
	// processed capture frame; Metrics() reads it lock-free.
	metricsPtr atomic.Pointer[Metrics]

	// renderFramesDropped mirrors engine.RenderFramesDropped(), synced
	// once per FeedFarEnd call so Metrics() can publish it without
	// touching engine itself (which is only safe from the owning
	// goroutine) — see the Canceller type doc comment's Pacing
	// section.
	renderFramesDropped atomic.Uint64
}

// NewCanceller constructs a Canceller for cfg. Returns ErrBadArg if
// cfg.SampleRate is not 16000, 32000, or 48000, if CaptureChannels/
// RenderChannels is not in 1..64, or if cfg.Tuning is non-nil and
// config.Validate would have to clamp one of its fields (see
// CancellerConfig.Tuning's doc comment).
func NewCanceller(cfg CancellerConfig) (*Canceller, error) {
	switch cfg.SampleRate {
	case 16000, 32000, 48000:
	default:
		return nil, ErrBadArg
	}
	if cfg.CaptureChannels < 1 || cfg.CaptureChannels > maxChannels {
		return nil, ErrBadArg
	}
	if cfg.RenderChannels < 1 || cfg.RenderChannels > maxChannels {
		return nil, ErrBadArg
	}

	tuning := config.DefaultConfig()
	if cfg.Tuning != nil {
		candidate := *cfg.Tuning
		if err := validateTuning(&candidate); err != nil {
			return nil, err
		}
		tuning = candidate
	}

	c := &Canceller{
		cfg:       cfg,
		tuning:    tuning,
		numFrames: cfg.SampleRate / 100,
	}
	c.buildEngineAndBuffers()
	return c, nil
}

// buildEngineAndBuffers (re)creates the internal engine and every
// buffer from scratch, per c.cfg/c.tuning/c.numFrames. Shared by
// NewCanceller and Reset so the two can never drift apart: Reset's
// "identical to a fresh instance" guarantee holds by construction, not
// by separately maintained reset logic.
func (c *Canceller) buildEngineAndBuffers() {
	c.engine = aec3.NewEchoCanceller3(c.tuning, c.cfg.SampleRate, c.cfg.RenderChannels, c.cfg.CaptureChannels)

	c.renderAccum = c.renderAccum[:0]
	c.renderFrame = newFloatS16Frame(c.cfg.RenderChannels, c.numFrames)

	c.captureAccum = c.captureAccum[:0]
	c.captureFrame = newFloatS16Frame(c.cfg.CaptureChannels, c.numFrames)
	c.outScratch = make([]float64, c.numFrames*c.cfg.CaptureChannels)

	// Prime the output delay line with exactly one frame of silence —
	// see Latency's doc comment for why this size is exactly right.
	c.outQueue = make([]float64, c.numFrames*c.cfg.CaptureChannels)

	c.delayGen.Store(0)
	c.pendingDelayMs.Store(0)
	c.appliedDelayGen = 0

	c.metricsPtr.Store(&Metrics{})
	c.renderFramesDropped.Store(0)
}

func newFloatS16Frame(channels, numFrames int) [][]float32 {
	frame := make([][]float32, channels)
	for ch := range frame {
		frame[ch] = make([]float32, numFrames)
	}
	return frame
}

// FeedFarEnd buffers samples (interleaved, normalized float64 in
// [-1, 1], RenderChannels channels) as render/far-end signal: whatever
// is being (or about to be) played out of the loudspeaker. len(samples)
// must be a whole multiple of RenderChannels; any other length returns
// ErrBadArg without buffering anything. samples need not be a multiple
// of one full 10ms AEC3 frame — FeedFarEnd accumulates arbitrary-sized
// calls internally, feeding the underlying engine one complete frame at
// a time as enough render audio arrives.
//
// Each sample is clamped to [-1, 1] and any non-finite value (NaN or
// +/-Inf) is mapped to 0 before being scaled into the engine's
// internal domain (see deinterleaveToFloatS16), so a bad caller-
// supplied sample can never propagate a NaN, an infinity, or an
// out-of-range scaled value into the underlying AEC3 engine.
//
// Do not feed render more than ~1s ahead of Process — see the
// Canceller type doc comment's Pacing section; excess frames are
// silently dropped and counted in Metrics().RenderFramesDropped.
//
// See the Canceller type doc comment for FeedFarEnd's single-goroutine-
// caller concurrency contract.
func (c *Canceller) FeedFarEnd(samples []float64) error {
	ch := c.cfg.RenderChannels
	if len(samples)%ch != 0 {
		return ErrBadArg
	}
	if len(samples) == 0 {
		return nil
	}

	c.renderAccum = append(c.renderAccum, samples...)

	frameLen := c.numFrames * ch
	n := 0
	for len(c.renderAccum)-n >= frameLen {
		deinterleaveToFloatS16(c.renderAccum[n:n+frameLen], ch, c.renderFrame)
		c.engine.AnalyzeRender(c.renderFrame)
		n += frameLen
	}
	if n > 0 {
		c.renderAccum = append(c.renderAccum[:0], c.renderAccum[n:]...)
	}
	c.renderFramesDropped.Store(c.engine.RenderFramesDropped())
	return nil
}

// Process removes the estimated echo from samples (interleaved,
// normalized float64 in [-1, 1], CaptureChannels channels), in place —
// it implements mutations.Processor, so a Canceller drops straight into
// a mixer.Config.Processors chain or any other Processor chain.
//
// Like loudness.Monitor.Process, Process computes the channel-aligned
// prefix of samples up front (aligned := len(samples) -
// len(samples)%CaptureChannels) and only touches that: a trailing
// partial frame — which should never happen with a well-behaved
// caller — is left completely untouched rather than causing an error.
//
// Unlike Monitor, Process is not a pure pass-through: the audio
// returned for a given call is not that same call's input, delayed and
// echo-cancelled — it is the input from Latency() earlier, per the
// fixed internal delay line described in Latency's doc comment. This
// holds regardless of how the caller chunks its calls to Process (any
// sizes, any alignment beyond the channel count above), including calls
// smaller than one full AEC3 frame.
//
// Each sample is clamped to [-1, 1] and any non-finite value (NaN or
// +/-Inf) is mapped to 0 before being scaled into the engine's
// internal domain (see deinterleaveToFloatS16) — the same guard
// FeedFarEnd applies to its input.
//
// See the Canceller type doc comment for Process's single-goroutine-
// caller concurrency contract.
func (c *Canceller) Process(samples []float64) {
	ch := c.cfg.CaptureChannels
	aligned := len(samples) - len(samples)%ch
	if aligned <= 0 {
		return
	}

	c.applyPendingDelay()

	c.captureAccum = append(c.captureAccum, samples[:aligned]...)

	frameLen := c.numFrames * ch
	n := 0
	for len(c.captureAccum)-n >= frameLen {
		deinterleaveToFloatS16(c.captureAccum[n:n+frameLen], ch, c.captureFrame)

		c.engine.AnalyzeCapture(c.captureFrame)
		// levelChange (upstream: an AGC level-step hint) is always
		// false: the public API has no surface for a caller to signal
		// "the capture gain just stepped," so this always takes the
		// conservative (no-hint) path, exactly as if no AGC ran ahead
		// of the canceller.
		c.engine.ProcessCapture(c.captureFrame, false)

		interleaveFromFloatS16(c.captureFrame, ch, c.outScratch)
		c.outQueue = append(c.outQueue, c.outScratch...)

		n += frameLen
	}
	if n > 0 {
		c.captureAccum = append(c.captureAccum[:0], c.captureAccum[n:]...)
		c.publishMetrics()
	}

	// Pop exactly `aligned` already-processed samples from the front of
	// the output delay line. c.outQueue is always at least `aligned`
	// samples long at this point — see Latency's doc comment for the
	// proof.
	copy(samples[:aligned], c.outQueue[:aligned])
	c.outQueue = append(c.outQueue[:0], c.outQueue[aligned:]...)
}

// applyPendingDelay applies the most recently SetAudioBufferDelay-d
// value to the engine, if it has changed since the last time Process
// applied one. Must only be called from Process (the owning goroutine).
func (c *Canceller) applyPendingDelay() {
	if g := c.delayGen.Load(); g != c.appliedDelayGen {
		c.engine.SetAudioBufferDelay(int(c.pendingDelayMs.Load()))
		c.appliedDelayGen = g
	}
}

// publishMetrics reads the engine's current metrics and atomically
// publishes them for Metrics() to observe. Called once per processed
// capture frame, from Process (the owning goroutine); Metrics() itself
// never touches the engine.
func (c *Canceller) publishMetrics() {
	m := c.engine.GetMetrics()
	c.metricsPtr.Store(&Metrics{
		EchoReturnLoss:            m.EchoReturnLoss,
		EchoReturnLossEnhancement: m.EchoReturnLossEnhancement,
		DelayMs:                   m.DelayMs,
		Clockdrift:                fromInternalClockdrift(c.engine.Clockdrift()),
	})
}

// Metrics returns the most recently published metrics snapshot (see
// publishMetrics): a lock-free read safe to call from any goroutine,
// concurrently with FeedFarEnd/Process. Before the first complete
// capture frame has been processed, everything except
// RenderFramesDropped is the zero value. RenderFramesDropped is
// synced independently, once per FeedFarEnd call, so it reflects
// drops even before any Process call has run.
func (c *Canceller) Metrics() Metrics {
	m := *c.metricsPtr.Load()
	m.RenderFramesDropped = c.renderFramesDropped.Load()
	return m
}

// SetAudioBufferDelay reports an external estimate of the audio buffer
// delay (e.g. the round-trip device latency reported by the audio
// backend) to help the delay estimator converge faster/more robustly.
// It is the one Canceller setter safe to call from any goroutine at any
// time, including concurrently with FeedFarEnd/Process (see the
// Canceller type doc comment) — the update is only actually applied to
// the underlying engine from inside the next Process call.
//
// Negative delay is clamped to 0 rather than returning an error,
// matching the underlying engine's own tolerant handling: internally,
// AEC3's render delay buffer already floors an unreasonably small
// (including negative) external delay estimate to its minimum
// representable delay (one block) rather than rejecting it. An
// excessively large delay is likewise not rejected: it is silently
// capped to the render delay buffer's maximum representable delay.
// Both ends are therefore always safe to pass — the practical effect
// of an out-of-range value is just a clamped delay hint, not a panic
// or an error return.
func (c *Canceller) SetAudioBufferDelay(delay time.Duration) {
	if delay < 0 {
		delay = 0
	}
	c.pendingDelayMs.Store(int64(delay / time.Millisecond))
	c.delayGen.Add(1)
}

// Latency returns Canceller's fixed processing latency: the delay
// between a sample entering via Process and the corresponding
// (echo-cancelled) sample leaving via Process, which is exactly one
// full AEC3 frame — 10ms, regardless of SampleRate — implemented as a
// fixed-length delay line (see the outQueue field doc comment),
// following the same "delay-then-overwrite" pattern loudness.Limiter
// uses for its own fixed Latency.
//
// # Why one frame, not one block
//
// This port's originating design note called for a much smaller fixed
// latency: BlockSize samples at 16kHz-equivalent (4ms), matching AEC3's
// own internal block-processing granularity. That figure is real —
// internal/aec3's BlockFramer (the component that reassembles capture
// output from 4ms blocks back into frames) does carry exactly that much
// permanent, stream-wide delay in the samples it emits, primed with a
// zero-filled block at construction — but it is not the whole latency
// story for a Process implementing mutations.Processor's arbitrary-
// chunk-size, in-place, same-length contract.
//
// Splitting a frame into frequency bands (SplitIntoFrequencyBands,
// internal/aec3/audio_buffer.go — needed at every supported rate except
// 16kHz's single-band passthrough) is not causal at sub-frame
// granularity: the band-splitting filters need every sample of a full
// 10ms frame before they can produce output for any of it. So no matter
// how Process's internal buffering is arranged, at least one full frame
// of input must be collected before the engine can run on it at all —
// a hard floor of 10ms, above the 4ms BlockFramer figure, for any
// design that must also guarantee correct (never underrunning) output
// under worst-case chunking (e.g. a caller invoking Process one sample
// at a time).
//
// One frame turns out to be not just necessary but sufficient: with the
// output delay line (outQueue) primed with exactly one frame's worth of
// zero samples, and each Process call (a) accumulating its input,
// (b) draining every complete frame that accumulates into the engine
// and appending its output to outQueue, then (c) popping exactly as
// many samples as it was called with off the front of outQueue, the
// queue is provably never short: after n total input samples have been
// accumulated, the engine has processed and pushed
// floor(n/frameLen)*frameLen samples, so the queue's size at pop time
// is frameLen + floor(n/frameLen)*frameLen - (n - aligned) = frameLen -
// (n mod frameLen) >= 1, for every possible call-chunking pattern. This
// also makes the 4ms BlockFramer figure moot at the wrapper level: it
// is already reflected inside the content the engine returns for a
// frame, not an additional delay Process needs to budget for on top.
func (c *Canceller) Latency() time.Duration {
	return mutations.FramesToDuration(int64(c.numFrames), c.cfg.SampleRate)
}

// Reset clears Canceller's internal state so it can be reused for a
// fresh stream (e.g. after a seek or loop wrap), exactly as
// mutations.Processor documents. It rebuilds the underlying engine and
// every buffer from scratch (see buildEngineAndBuffers) — after Reset,
// a Canceller produces bit-identical output to a freshly constructed
// one of the same CancellerConfig; no state is deliberately carried
// across a Reset.
func (c *Canceller) Reset() {
	c.buildEngineAndBuffers()
}

// deinterleaveToFloatS16 splits interleaved (normalized float64,
// channels channels) into dst[channel] (FloatS16 float32, one frame's
// worth per channel), scaling by 32768.0 (see the Canceller type doc
// comment's Units section). Each input sample is first passed through
// clampFinite — clamped to [-1, 1] and any non-finite value (NaN or
// +/-Inf) mapped to 0 — so a bad caller-supplied sample can never
// reach the *32768.0 scale, matching mutations/format.go's clamp
// convention for the in-range case, extended here to also cover the
// non-finite case that helper doesn't handle (NaN compares false
// against both bounds, so a bare clamp silently passes it through
// unchanged). This is the public float64 boundary FeedFarEnd's and
// Process's doc comments describe.
func deinterleaveToFloatS16(interleaved []float64, channels int, dst [][]float32) {
	numFrames := len(interleaved) / channels
	for ch := 0; ch < channels; ch++ {
		out := dst[ch]
		for i := 0; i < numFrames; i++ {
			out[i] = float32(clampFinite(interleaved[i*channels+ch]) * 32768.0)
		}
	}
}

// clampFinite maps a non-finite v (NaN or +/-Inf) to 0, then clamps
// the result to [-1, 1].
func clampFinite(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v > 1.0 {
		return 1.0
	}
	if v < -1.0 {
		return -1.0
	}
	return v
}

// interleaveFromFloatS16 is deinterleaveToFloatS16's inverse: it
// combines src[channel] (FloatS16 float32) into interleaved (normalized
// float64), scaling by 1/32768.0.
func interleaveFromFloatS16(src [][]float32, channels int, interleaved []float64) {
	numFrames := len(src[0])
	for ch := 0; ch < channels; ch++ {
		in := src[ch]
		for i := 0; i < numFrames; i++ {
			interleaved[i*channels+ch] = float64(in[i]) / 32768.0
		}
	}
}
