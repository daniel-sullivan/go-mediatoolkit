package vad

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Energy engine defaults, substituted for the zero value of the
// corresponding EnergyConfig field (see each field's doc for the
// epsilon escape hatch where a literal zero is meaningful).
const (
	defaultEnergyHighpassHz  = 200.0
	defaultEnergyLowpassHz   = 4000.0
	defaultEnergyOnDB        = 6.0
	defaultEnergyOffDB       = 3.0
	defaultEnergyAbsFloorDB  = -55.0
	defaultEnergyFloorFall   = 200 * time.Millisecond
	defaultEnergyFloorRise   = 4 * time.Second
	defaultEnergyOnset       = 30 * time.Millisecond
	defaultEnergyHangover    = 200 * time.Millisecond
	energyLowpassNyquistFrac = 0.45 // LowpassHz clamp: at most 0.45·SampleRate

	// energyFloorMin/Max clamp the adaptive noise floor (dBFS). −90 is
	// below any real capture chain's noise; 0 is full scale.
	energyFloorMin = -90.0
	energyFloorMax = 0.0

	// energyEpsilon regularises the log: E = 10·log10(mean(x²)+1e-12),
	// bounding digital silence at −120 dBFS instead of −Inf.
	energyEpsilon = 1e-12

	// energyMinSampleRate is the Energy engine's rate floor: below
	// 100 Hz a 10 ms decision frame holds less than one sample.
	energyMinSampleRate = 100

	maxChannels = 64
)

// EnergyConfig configures an EnergyDetector. The zero value of every
// field except SampleRate and Channels selects a sensible speech
// default, so EnergyConfig{SampleRate: 48000, Channels: 1} is a
// complete, valid configuration. Wherever a literal zero would be
// meaningful, the doc notes the epsilon escape hatch that expresses it
// (the loudness package convention).
type EnergyConfig struct {
	// SampleRate is the input sample rate in Hz. Must be at least 100
	// (a 10 ms decision frame needs at least one sample).
	SampleRate int

	// Channels is the interleaved input channel count, in 1..64.
	// Multi-channel input is downmixed to mono before detection (2ch:
	// L/R average; >2ch: equal-weight average).
	Channels int

	// HighpassHz is the cutoff of the 2nd-order Butterworth highpass
	// that strips rumble, mains hum, and DC before the energy measure.
	// The zero value selects 200 Hz (below the speech band, above
	// mains harmonics). Must not be negative or NaN. An exact 0
	// selects the default; pass a tiny sub-audio cutoff (e.g. 0.001)
	// for an effectively disabled highpass.
	HighpassHz float64

	// LowpassHz is the cutoff of the 2nd-order Butterworth lowpass
	// that rejects out-of-band hiss and ultrasonics. The zero value
	// selects 4000 Hz (the top of the classic telephony speech band);
	// any value is clamped to at most 0.45·SampleRate. Must not be
	// negative or NaN. An exact 0 selects the default; pass a large
	// value (e.g. the sample rate itself) for the widest legal band —
	// the 0.45·SampleRate clamp is the "no lowpass" limit.
	LowpassHz float64

	// OnThresholdDB is how far (dB) the frame energy must rise above
	// the adaptive noise floor to count as raw-voiced. The zero value
	// selects +6 dB. Must be finite; must be strictly greater than
	// the resolved OffThresholdDB, checked at float32 precision — the
	// pair is quantised through float32 when packed for the atomic
	// live-tuning path, so a gap that survives at float64 but
	// collapses (or inverts) once quantised is also rejected
	// (ErrBadThreshold). An exact 0 selects the default; pass a tiny
	// value (e.g. 1e-9) for a literal 0 dB margin.
	OnThresholdDB float64

	// OffThresholdDB is the release margin: once raw-voiced, a frame
	// stays raw-voiced until energy falls below floor+OffThresholdDB.
	// The gap between On and Off is the hysteresis band that stops
	// threshold chatter. The zero value selects +3 dB. Must be finite
	// and strictly less than the resolved OnThresholdDB at float32
	// precision (see OnThresholdDB). An exact 0 selects the default;
	// pass a tiny value (e.g. 1e-9) for a literal 0 dB release margin.
	OffThresholdDB float64

	// AbsoluteFloorDB is the absolute energy gate in dBFS: no frame
	// below it is ever raw-voiced, regardless of how low the adaptive
	// floor has fallen. It is what stops a silent room's noise-floor
	// wobble (or a low-level level step) from registering as speech.
	// The zero value selects −55 dBFS. Must not be NaN or +Inf; −Inf
	// is accepted and disables the absolute gate entirely (a very
	// negative finite value, e.g. −200, does the same). An exact 0
	// selects the default — a literal 0 dBFS gate would reject
	// everything below full scale, which no real configuration wants.
	AbsoluteFloorDB float64

	// FloorFall is the one-pole time constant with which the adaptive
	// noise floor falls toward a QUIETER energy reading (E < floor).
	// Fast by design — minimum statistics: the floor should snap down
	// to genuine silence. The zero value selects 200 ms. Must not be
	// negative. An exact 0 selects the default; pass time.Nanosecond
	// for effectively instant tracking.
	FloorFall time.Duration

	// FloorRise is the one-pole time constant with which the floor
	// rises toward a LOUDER reading (E ≥ floor) while not raw-voiced.
	// Slow by design, so brief speech cannot drag the floor up. The
	// zero value selects 4 s. Must not be negative. An exact 0
	// selects the default; pass time.Nanosecond for instant tracking.
	FloorRise time.Duration

	// Onset is the debounce for SpeechStart: energy must stay
	// raw-voiced this long (consecutive ~10 ms decision frames,
	// rounded to the nearest frame) before the transition fires. The
	// fired event is back-timestamped to the start of the run, so
	// Onset delays the announcement, not the reported position. The
	// zero value selects 30 ms. Must not be negative. An exact 0
	// selects the default; pass time.Nanosecond for no debounce (a
	// single voiced frame fires the transition).
	Onset time.Duration

	// Hangover is the debounce for SpeechEnd: energy must stay
	// raw-unvoiced this long before the transition fires, bridging
	// stop closures and inter-word gaps (50–150 ms) while still
	// splitting real pauses. The fired event is back-timestamped to
	// where speech actually stopped. The zero value selects 200 ms.
	// Must not be negative. An exact 0 selects the default; pass
	// time.Nanosecond for no hangover.
	Hangover time.Duration
}

// EnergyDetector is the original-design energy VAD: cheap, zero added
// dependencies, and the lowest-latency engine in the package. It is a
// band-limited adaptive-threshold energy detector, not a speech-model
// classifier — it hears "sustained in-band energy above the recent
// noise floor", which is speech in a normal capture chain but will
// also fire on music or sustained tones in the 200 Hz–4 kHz band. For
// model-based discrimination use the WebRTC or Silero engines
// (follow-up phases).
//
// # DSP
//
// The mono (downmixed) stream is band-limited by a 2nd-order
// Butterworth highpass (HighpassHz) and lowpass (LowpassHz), then cut
// into non-overlapping ~10 ms decision frames. Per frame the energy is
// E = 10·log10(mean(x²)+1e-12) dBFS. An adaptive noise floor F
// (initialised to max(E₀, −90), clamped to [−90, 0], FROZEN while
// raw-voiced) tracks E with a one-pole smoother — fast down
// (FloorFall) and slow up (FloorRise), a minimum-statistics-lite
// asymmetry. A frame turns raw-voiced when E > F+OnThresholdDB AND
// E > AbsoluteFloorDB, and stays raw-voiced until E < F+OffThresholdDB
// (threshold hysteresis). The raw decisions then pass through the
// Onset/Hangover state machine to become SpeechStart/SpeechEnd events
// and the Active() state.
//
// Because the floor initialises to the FIRST frame's energy, audio
// already in progress at construction time reads as the noise floor,
// not as speech — the detector needs to hear some quiet before it can
// tell loud from background. This is inherent to any adaptive-floor
// design.
//
// # Concurrency
//
// Process and Reset must be driven from a single goroutine. Every
// reader (Active, Probability, LastTransition, DecisionLatency,
// Events) and every setter (SetThresholdsDB, SetAbsoluteFloorDB,
// SetOnset, SetHangover) is safe from any goroutine while Process
// runs: parameters live in single-word atomics snapshotted once per
// decision frame, and the on/off threshold pair shares ONE word so a
// torn READ (observing one half updated without the other) is
// impossible. Setter values are quantised through float32 when
// packed, and validated AFTER quantisation: off < on is guaranteed to
// hold for the exact float32 pair a decision frame reads, not just for
// the float64 values passed in — a requested gap narrower than a
// float32 ULP at that magnitude (well under 0.0001 dB for the default
// range) is rejected with ErrBadThreshold rather than silently
// collapsing or inverting after packing.
//
// # Latency
//
// DecisionLatency = one decision frame fill (~10 ms) + the current
// Onset. With defaults that is ~40 ms: a SpeechStart is published
// ~40 ms after the speech it is back-timestamped to began. Feed
// Process small buffers (≤ 10 ms) to keep the announcement delay near
// that bound; a large buffer batches decisions to its end.
type EnergyDetector struct {
	core

	adapter *engineAdapter
	hp, lp  *mutations.Biquad
	hyst    hysteresis

	fallAlpha float64 // one-pole coefficient toward quieter E
	riseAlpha float64 // one-pole coefficient toward louder E

	floor      float64
	floorInit  bool
	rawVoiced  bool
	frameDurNS int64 // decision frame duration in ns (for onset/hangover rounding)

	// Live parameters (written by setters on any goroutine, snapshotted
	// once per decision frame by the audio goroutine).
	thresholds atomic.Uint64 // packThresholds(on, off)
	absFloor   atomic.Uint64 // float64 bits
	onset      atomic.Int64  // resolved duration, ns
	hangover   atomic.Int64  // resolved duration, ns
}

// packThresholds packs an (on, off) dB pair into one 64-bit word as
// two float32s, so the pair is stored and loaded with pair coherence —
// a reader can never observe one half updated without the other.
func packThresholds(on, off float64) uint64 {
	return uint64(math.Float32bits(float32(on)))<<32 | uint64(math.Float32bits(float32(off)))
}

// unpackThresholds is the inverse of packThresholds.
func unpackThresholds(w uint64) (on, off float64) {
	return float64(math.Float32frombits(uint32(w >> 32))), float64(math.Float32frombits(uint32(w)))
}

// resolveEnergyThresholds applies the zero-value defaults and
// validates an (on, off) pair: both must be finite (after default
// substitution) and off must be strictly below on — checked at the
// float32 precision the pair is actually packed to (packThresholds),
// since a float64 gap smaller than a float32 ULP would otherwise
// collapse to on == off (or invert) only after packing, evading a
// float64-only check.
func resolveEnergyThresholds(on, off float64) (float64, float64, error) {
	if on == 0 {
		on = defaultEnergyOnDB
	}
	if off == 0 {
		off = defaultEnergyOffDB
	}
	if math.IsNaN(on) || math.IsInf(on, 0) || math.IsNaN(off) || math.IsInf(off, 0) {
		return 0, 0, ErrBadThreshold
	}
	if !(float32(on) > float32(off)) {
		return 0, 0, ErrBadThreshold
	}
	return on, off, nil
}

// resolveAbsFloor applies the zero-value default and validates an
// absolute floor: NaN and +Inf are rejected; −Inf (gate disabled) is
// accepted.
func resolveAbsFloor(db float64) (float64, error) {
	if db == 0 {
		db = defaultEnergyAbsFloorDB
	}
	if math.IsNaN(db) || math.IsInf(db, 1) {
		return 0, ErrBadThreshold
	}
	return db, nil
}

// resolveDebounce applies a zero-value default to an onset/hangover
// duration and rejects negatives.
func resolveDebounce(d, def time.Duration) (time.Duration, error) {
	if d < 0 {
		return 0, ErrBadConfig
	}
	if d == 0 {
		return def, nil
	}
	return d, nil
}

// NewEnergyDetector constructs an EnergyDetector from cfg,
// substituting the documented defaults for zero-valued fields. It
// returns ErrBadSampleRate (rate < 100), ErrBadChannels (channels not
// in 1..64), ErrBadThreshold (non-finite thresholds, off ≥ on, NaN or
// +Inf absolute floor), or ErrBadConfig (negative or NaN filter
// cutoffs, negative durations).
func NewEnergyDetector(cfg EnergyConfig) (*EnergyDetector, error) {
	if cfg.SampleRate < energyMinSampleRate {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	if cfg.HighpassHz < 0 || math.IsNaN(cfg.HighpassHz) ||
		cfg.LowpassHz < 0 || math.IsNaN(cfg.LowpassHz) {
		return nil, ErrBadConfig
	}
	if cfg.FloorFall < 0 || cfg.FloorRise < 0 {
		return nil, ErrBadConfig
	}

	on, off, err := resolveEnergyThresholds(cfg.OnThresholdDB, cfg.OffThresholdDB)
	if err != nil {
		return nil, err
	}
	absFloor, err := resolveAbsFloor(cfg.AbsoluteFloorDB)
	if err != nil {
		return nil, err
	}
	onset, err := resolveDebounce(cfg.Onset, defaultEnergyOnset)
	if err != nil {
		return nil, err
	}
	hangover, err := resolveDebounce(cfg.Hangover, defaultEnergyHangover)
	if err != nil {
		return nil, err
	}

	highpass := cfg.HighpassHz
	if highpass == 0 {
		highpass = defaultEnergyHighpassHz
	}
	lowpass := cfg.LowpassHz
	if lowpass == 0 {
		lowpass = defaultEnergyLowpassHz
	}
	if maxLP := energyLowpassNyquistFrac * float64(cfg.SampleRate); lowpass > maxLP {
		lowpass = maxLP
	}

	floorFall := cfg.FloorFall
	if floorFall == 0 {
		floorFall = defaultEnergyFloorFall
	}
	floorRise := cfg.FloorRise
	if floorRise == 0 {
		floorRise = defaultEnergyFloorRise
	}

	frameSize := int(mutations.DurationToFrames(10*time.Millisecond, cfg.SampleRate))
	if frameSize < 1 {
		frameSize = 1
	}
	adapter, err := newEngineAdapter(cfg.SampleRate, cfg.Channels, cfg.SampleRate, frameSize)
	if err != nil {
		return nil, err
	}

	frameDur := float64(frameSize) / float64(cfg.SampleRate)
	e := &EnergyDetector{
		core:       newCore(cfg.SampleRate, cfg.Channels),
		adapter:    adapter,
		hp:         mutations.NewHighpass(highpass, 0.707, cfg.SampleRate, 1),
		lp:         mutations.NewLowpass(lowpass, 0.707, cfg.SampleRate, 1),
		fallAlpha:  1 - math.Exp(-frameDur/floorFall.Seconds()),
		riseAlpha:  1 - math.Exp(-frameDur/floorRise.Seconds()),
		frameDurNS: int64(mutations.FramesToDuration(int64(frameSize), cfg.SampleRate)),
	}
	e.thresholds.Store(packThresholds(on, off))
	e.absFloor.Store(math.Float64bits(absFloor))
	e.onset.Store(int64(onset))
	e.hangover.Store(int64(hangover))
	return e, nil
}

// Process feeds samples (interleaved, Channels() wide) into the
// detector and leaves the audio itself completely untouched — the
// detector is pass-through, so it can sit in any effects chain purely
// to observe. Decisions are made per ~10 ms frame; SpeechStart and
// SpeechEnd events are published synchronously from within this call
// (see the package doc's subscriber contract). Any trailing partial
// frame (a length not divisible by the channel count) is ignored.
// A frame containing any non-finite sample (NaN/Inf — a decoder
// glitch or deliberately malformed input) is treated as silence for
// that frame and never reaches the highpass/lowpass filters or the
// adaptive floor; both are left exactly as they were, so state is not
// corrupted and later all-finite frames are decided normally. Never
// panics.
func (e *EnergyDetector) Process(samples []float64) {
	e.adapter.push(samples, e.decide)
}

// decide is the per-decision-frame engine step (adapter callback).
// frame is ~10 ms of mono audio (adapter scratch — mutating it in
// place is allowed and the filters do); startSample is its
// engine-domain position, which for the Energy engine (never
// resampled) is also the input frame position.
func (e *EnergyDetector) decide(frame []float64, startSample int64) {
	// A non-finite input sample (NaN/Inf — a decoder glitch, a silent
	// buffer underrun, deliberately malformed input) must be kept OUT
	// of the highpass/lowpass filters entirely: hp/lp are IIR biquads
	// with a persistent per-channel delay line (mutations.Biquad), so
	// even a single non-finite sample fed through them poisons that
	// state forever — every later frame's filtered output would stay
	// NaN regardless of the raw input, with no way back to a finite
	// reading short of Reset. Detecting non-finite content up front and
	// skipping the filters (and the energy/floor logic below) for that
	// frame keeps the filter state, and the adaptive floor, untouched;
	// a later all-finite frame is then decided exactly as if the
	// non-finite frame had simply been a dropped buffer.
	finite := true
	for i := 0; i < len(frame); i++ {
		if v := frame[i]; math.IsNaN(v) || math.IsInf(v, 0) {
			finite = false
			break
		}
	}

	var energy float64
	if finite {
		e.hp.Process(frame)
		e.lp.Process(frame)

		sum := 0.0
		for i := 0; i < len(frame); i++ {
			sum += frame[i] * frame[i]
		}
		energy = 10 * math.Log10(sum/float64(len(frame))+energyEpsilon)
	}

	// Snapshot every live parameter once, up front: the whole frame
	// decision sees one coherent parameter set.
	on, off := unpackThresholds(e.thresholds.Load())
	absFloor := math.Float64frombits(e.absFloor.Load())
	onsetFrames := debounceFrames(e.onset.Load(), e.frameDurNS)
	hangoverFrames := debounceFrames(e.hangover.Load(), e.frameDurNS)

	// Treat a non-finite frame as non-voiced and touch NEITHER the
	// floor init NOR the floor update: an adaptive floor that ever
	// absorbs a NaN/Inf reading is poisoned for the rest of the stream
	// (NaN propagates through every later comparison and update).
	// Skipping leaves the floor exactly as it was — including "not yet
	// initialised" — so a later finite frame initialises or updates it
	// normally.
	if finite && !e.floorInit {
		e.floor = energy
		if e.floor < energyFloorMin {
			e.floor = energyFloorMin
		}
		if e.floor > energyFloorMax {
			e.floor = energyFloorMax
		}
		e.floorInit = true
	}

	if !finite {
		e.rawVoiced = false
	} else if !e.rawVoiced {
		// Raw decision with threshold hysteresis: enter on On (plus
		// the absolute gate), leave on Off.
		e.rawVoiced = energy > e.floor+on && energy > absFloor
	} else {
		e.rawVoiced = energy >= e.floor+off
	}

	// Adaptive floor: frozen while raw-voiced (speech must not drag
	// its own reference up) or while the reading is non-finite,
	// otherwise one-pole toward the reading — fast down, slow up.
	if finite && !e.rawVoiced {
		alpha := e.riseAlpha
		if energy < e.floor {
			alpha = e.fallAlpha
		}
		e.floor += alpha * (energy - e.floor)
		if e.floor < energyFloorMin {
			e.floor = energyFloorMin
		}
		if e.floor > energyFloorMax {
			e.floor = energyFloorMax
		}
	}

	kind, eventFrame, fired := e.hyst.step(e.rawVoiced, e.adapter.inputFrame(startSample), onsetFrames, hangoverFrames)
	if fired {
		e.emit(kind, eventFrame, binaryProbability(kind))
	}
}

// debounceFrames converts a debounce duration (ns) to a decision-frame
// count, rounded to nearest. A sub-frame duration rounds to zero,
// which the hysteresis machine treats as "one frame" — the minimum
// evidence a transition can have.
func debounceFrames(ns, frameNS int64) int {
	return int((ns + frameNS/2) / frameNS)
}

// Reset returns the detector to its just-constructed state: filters,
// adaptive floor, raw/hysteresis state, position counters, and the
// Active/Probability/LastTransition readers are all cleared, and event
// positions restart at frame 0. Configuration — including values
// changed by live setters — is retained, as are bus subscriptions.
func (e *EnergyDetector) Reset() {
	e.adapter.reset()
	e.hp.Reset()
	e.lp.Reset()
	e.hyst.reset()
	e.floor = 0
	e.floorInit = false
	e.rawVoiced = false
	e.resetState()
}

// DecisionLatency reports how long after speech begins the detector
// announces it, worst case: one decision frame fill (~10 ms) plus the
// current Onset debounce. It reflects live SetOnset changes. Safe
// from any goroutine.
func (e *EnergyDetector) DecisionLatency() time.Duration {
	return e.adapter.latencyBase() + time.Duration(e.onset.Load())
}

// SetThresholdsDB atomically replaces the on/off threshold pair
// (dB above the adaptive floor). The pair is packed into a single
// atomic word, so the audio goroutine always sees a coherent pair —
// never a torn on-without-off. Zero values select the defaults
// (+6/+3 dB) exactly as in EnergyConfig; the same epsilon hatches
// apply. Values are quantised through float32 when packed; returns
// ErrBadThreshold for non-finite values or for a pair whose gap does
// not survive that quantisation (off < on must hold at float32
// precision, not just float64). Safe from any goroutine.
func (e *EnergyDetector) SetThresholdsDB(on, off float64) error {
	on, off, err := resolveEnergyThresholds(on, off)
	if err != nil {
		return err
	}
	e.thresholds.Store(packThresholds(on, off))
	return nil
}

// SetAbsoluteFloorDB atomically replaces the absolute energy gate
// (dBFS). Zero selects the −55 dBFS default; −Inf (or any very
// negative value) disables the gate. Returns ErrBadThreshold for NaN
// or +Inf. Safe from any goroutine.
func (e *EnergyDetector) SetAbsoluteFloorDB(db float64) error {
	db, err := resolveAbsFloor(db)
	if err != nil {
		return err
	}
	e.absFloor.Store(math.Float64bits(db))
	return nil
}

// SetOnset atomically replaces the SpeechStart debounce. Zero selects
// the 30 ms default; pass time.Nanosecond for no debounce. Returns
// ErrBadConfig for a negative duration. Takes effect from the next
// decision frame (including for a voiced run already in progress) and
// changes DecisionLatency. Safe from any goroutine.
func (e *EnergyDetector) SetOnset(d time.Duration) error {
	d, err := resolveDebounce(d, defaultEnergyOnset)
	if err != nil {
		return err
	}
	e.onset.Store(int64(d))
	return nil
}

// SetHangover atomically replaces the SpeechEnd debounce. Zero
// selects the 200 ms default; pass time.Nanosecond for no hangover.
// Returns ErrBadConfig for a negative duration. Takes effect from the
// next decision frame. Safe from any goroutine.
func (e *EnergyDetector) SetHangover(d time.Duration) error {
	d, err := resolveDebounce(d, defaultEnergyHangover)
	if err != nil {
		return err
	}
	e.hangover.Store(int64(d))
	return nil
}

// EnergyDetector implements Detector.
var _ Detector = (*EnergyDetector)(nil)
