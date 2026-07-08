package vad

import (
	"math"
	"sync/atomic"
	"time"
)

// Ducker defaults, substituted for the zero value of the corresponding
// DuckerConfig field.
const (
	defaultDuckerDepthDB = -12.0
	defaultDuckerAttack  = 50 * time.Millisecond
	defaultDuckerRelease = 400 * time.Millisecond
)

// DuckerConfig configures a Ducker.
type DuckerConfig struct {
	// SampleRate is the bed stream's sample rate in Hz. Must be
	// positive. It does NOT have to match the Detector's rate — the
	// ducker only reads the detector's boolean state, it never feeds
	// it audio — but the two streams are normally the same session,
	// so mismatched rates usually indicate a wiring mistake.
	SampleRate int

	// Channels is the bed stream's interleaved channel count, in
	// 1..64. Independent of the Detector's channel count (see
	// SampleRate).
	Channels int

	// Detector is the sidechain: the VAD engine whose Active() state
	// drives the ducking. Required (ErrNilDetector when nil).
	//
	// The Ducker only READS this detector — it must be FED elsewhere,
	// typically by inserting it (or a Gate wrapping it) in the voice
	// track's own effects chain. Detector readers are goroutine-safe
	// atomics, which is what makes this cross-track (and, in a mixer,
	// same-goroutine-different-chain) read legal; feeding the same
	// detector from two places is the one thing that is never legal,
	// the same one-feeder rule every mutations.Processor carries (see
	// its doc).
	Detector Detector

	// DepthDB is the attenuation applied to the bed while the
	// sidechain hears speech, in dB. The zero value selects −12 dB.
	// −Inf is accepted (full mute while speech plays). Must not be
	// positive or NaN. A literal 0 dB depth would make the ducker a
	// no-op — pass a tiny negative value (e.g. −1e-9) if that is
	// genuinely wanted.
	DepthDB float64

	// Attack is how long the duck takes to reach full depth once
	// speech starts. The zero value selects 50 ms. Must not be
	// negative. An exact 0 selects the default; pass time.Nanosecond
	// for an instant duck.
	Attack time.Duration

	// Release is how long the bed takes to recover to unity after
	// speech ends. The zero value selects 400 ms (slow, radio-style —
	// the bed swells back rather than pumping). Must not be negative.
	// An exact 0 selects the default; pass time.Nanosecond for an
	// instant recovery.
	Release time.Duration
}

// Ducker is a sidechain attenuator: a mutations.Processor that sits in
// a BED track's effects chain and ducks it by DepthDB whenever the
// injected Detector — fed by a different (voice) track — reports
// speech. The classic radio "music dips under the presenter" effect.
//
// # Wiring
//
// The Ducker never feeds its detector; it only reads Active(). Feed
// the detector from the voice track (insert it in that track's
// timeline.EffectSource chain) and hand the same Detector to the
// Ducker on the bed track. The ducker is deliberately NOT coupled to
// the mixer package — it works between any two chains.
//
// # Staleness
//
// Because the voice and bed chains are processed as separate chunks
// (by a mixer, one track after another), the ducker can read a
// detector state up to one chunk older than the bed audio it is
// shaping. At a mixer's ~10 ms chunk size that staleness is far
// smaller than the 50/400 ms attack/release ramps that absorb it; it
// is inaudible by construction and pinned by the chunk-interleave
// test.
//
// # Latency
//
// None. The ducker has no delay line: gain changes ramp in from "now"
// rather than anticipating speech. (If you need the bed already
// ducked when the first syllable lands, that is what the Gate's
// Lookahead is for on the voice path — the bed dipping a few tens of
// milliseconds late is the accepted broadcast sound.)
//
// # Concurrency
//
// Process and Reset must be driven from a single goroutine (the bed
// chain's). The setters (SetDepthDB, SetAttack, SetRelease) and the
// GainDB reader are safe from any goroutine while Process runs.
type Ducker struct {
	sampleRate int
	channels   int
	det        Detector

	gain rampedGain // ramp + attack(fall)/release(rise) steps + last-applied reader

	// Live parameters (float64 bits; written by setters on any
	// goroutine, snapshotted once per Process call).
	depthLin atomic.Uint64 // ducked linear gain
}

// NewDucker constructs a Ducker from cfg, substituting the documented
// defaults for zero-valued fields. It returns ErrNilDetector (nil
// Detector), ErrBadSampleRate (non-positive rate), ErrBadChannels
// (channels not in 1..64), or ErrBadConfig (negative durations, NaN
// or positive DepthDB).
func NewDucker(cfg DuckerConfig) (*Ducker, error) {
	if cfg.Detector == nil {
		return nil, ErrNilDetector
	}
	if cfg.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	if cfg.Attack < 0 || cfg.Release < 0 {
		return nil, ErrBadConfig
	}
	depth := cfg.DepthDB
	if depth == 0 {
		depth = defaultDuckerDepthDB
	}
	depthLin, err := resolveGateFloor(depth) // same validation domain: ≤ 0 dB, −Inf OK
	if err != nil {
		return nil, err
	}

	attack := cfg.Attack
	if attack == 0 {
		attack = defaultDuckerAttack
	}
	release := cfg.Release
	if release == 0 {
		release = defaultDuckerRelease
	}

	d := &Ducker{
		sampleRate: cfg.SampleRate,
		channels:   cfg.Channels,
		det:        cfg.Detector,
	}
	// bed plays at unity until speech appears; attack = fall (duck
	// engaging), release = rise (duck recovering).
	d.gain.init(cfg.SampleRate, 1, attack, release)
	d.depthLin.Store(math.Float64bits(depthLin))
	return d, nil
}

// Process ducks samples in place. samples is interleaved with
// Channels() channels; any trailing partial frame is left untouched.
// Per frame, the gain ramps toward the ducked depth while the
// sidechain detector reports speech and back toward unity otherwise.
// No delay is added; chunking is invisible to the output. Never
// panics.
func (d *Ducker) Process(samples []float64) {
	ch := d.channels
	frames := len(samples) / ch
	if frames == 0 {
		return
	}

	depth := math.Float64frombits(d.depthLin.Load())
	fall := d.gain.attack()
	rise := d.gain.release()

	for f := 0; f < frames; f++ {
		target := 1.0
		if d.det.Active() {
			target = depth
		}
		gain := d.gain.ramp.step(target, rise, fall)
		base := f * ch
		for c := 0; c < ch; c++ {
			samples[base+c] *= gain
		}
	}
	d.gain.recordGain(d.gain.ramp.gain)
}

// Reset returns the ducker's gain to unity. It does NOT reset the
// sidechain detector — the ducker doesn't own it (the voice chain
// feeding it does). Configuration, including live-set values, is
// retained.
func (d *Ducker) Reset() {
	d.gain.reset(1)
}

// GainDB reports the ducking gain applied to the most recently
// processed frame, in dB (0 = no ducking; DepthDB = fully ducked).
// Safe from any goroutine while Process runs.
func (d *Ducker) GainDB() float64 {
	return d.gain.GainDB()
}

// SetDepthDB atomically replaces the ducking depth. Semantics match
// DuckerConfig.DepthDB (0 selects −12 dB; −Inf accepted; NaN and
// positive values return ErrBadConfig). Reached through the ramps
// from the next Process call. Safe from any goroutine.
func (d *Ducker) SetDepthDB(db float64) error {
	if db == 0 {
		db = defaultDuckerDepthDB
	}
	lin, err := resolveGateFloor(db)
	if err != nil {
		return err
	}
	d.depthLin.Store(math.Float64bits(lin))
	return nil
}

// SetAttack atomically replaces the duck-engage time. Zero selects
// the 50 ms default; pass time.Nanosecond for an instant duck;
// negative returns ErrBadConfig. Safe from any goroutine.
func (d *Ducker) SetAttack(dur time.Duration) error {
	return d.gain.SetAttack(dur, defaultDuckerAttack)
}

// SetRelease atomically replaces the recovery time. Zero selects the
// 400 ms default; pass time.Nanosecond for an instant recovery;
// negative returns ErrBadConfig. Safe from any goroutine.
func (d *Ducker) SetRelease(dur time.Duration) error {
	return d.gain.SetRelease(dur, defaultDuckerRelease)
}

// SampleRate reports the sample rate the ducker was configured for.
func (d *Ducker) SampleRate() int { return d.sampleRate }

// Channels reports the interleaved channel count the ducker expects.
func (d *Ducker) Channels() int { return d.channels }
