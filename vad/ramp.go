package vad

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// gainRamp is the shared Gate/Ducker gain smoother: a linear ramp in
// the linear-gain domain that slews the current gain toward a target
// with independent rise and fall rates, one step per frame.
//
// The linear (constant-slope) shape is deliberate: it gives a hard,
// provable click bound — the per-frame gain change never exceeds
// max(riseStep, fallStep) — which the Gate/Ducker click tests pin.
// riseStep/fallStep are expressed as gain-per-frame; a processor that
// wants "attack = time to traverse the full 0→1 range" passes
// 1/attackFrames.
//
// Rates are passed per step (not stored) because attack/release are
// live-tunable: the caller snapshots its atomics and hands the current
// steps in. gainRamp is owned by the audio goroutine and is not safe
// for concurrent use.
type gainRamp struct {
	gain float64
}

// step slews the gain one frame toward target and returns the new
// gain. Rising edges move by at most riseStep, falling edges by at
// most fallStep; the gain never overshoots the target.
func (r *gainRamp) step(target, riseStep, fallStep float64) float64 {
	switch {
	case r.gain < target:
		r.gain += riseStep
		if r.gain > target {
			r.gain = target
		}
	case r.gain > target:
		r.gain -= fallStep
		if r.gain < target {
			r.gain = target
		}
	}
	return r.gain
}

// rampStepForDuration converts an attack/release duration to a
// per-frame linear-gain step: the full 0→1 range traversed in d.
// A sub-frame duration yields a full-range (instant) step.
func rampStepForDuration(d time.Duration, sampleRate int) float64 {
	frames := mutations.DurationToFrames(d, sampleRate)
	if frames < 1 {
		return 1
	}
	return 1 / float64(frames)
}

// rampedGain is the Gate/Ducker gain-smoothing block: a gainRamp plus
// its live-tunable attack/release per-frame steps and the reader for
// the gain applied to the most recently processed frame. Both
// processors embed one; the shared SetAttack/SetRelease/GainDB logic
// below is byte-identical between them — only which ramp direction
// "attack" feeds (rise, for the Gate opening; fall, for the Ducker
// engaging) differs, and that's decided at each Process call site by
// which of attack()/release() is passed as gainRamp.step's rise vs
// fall argument.
//
// Live parameters are float64 bits in atomics, written by setters on
// any goroutine and snapshotted once per Process call by the audio
// goroutine; rampedGain itself is owned by that goroutine and is not
// safe for concurrent use beyond those atomics.
type rampedGain struct {
	sampleRate int
	ramp       gainRamp

	attackStep  atomic.Uint64 // gain step per frame, attack direction
	releaseStep atomic.Uint64 // gain step per frame, release direction
	lastGain    atomic.Uint64 // gain applied to the most recent frame
}

// init sets up a zero-value rampedGain (in its final, already-addressed
// struct location) at the steady-state gain (the just-constructed
// value), with attack/release converted to per-frame steps at
// sampleRate. Split from construction — rather than a value-returning
// constructor — because rampedGain embeds atomics: copying one out of a
// return value would trip go vet's copylocks check.
func (r *rampedGain) init(sampleRate int, gain float64, attack, release time.Duration) {
	r.sampleRate = sampleRate
	r.ramp = gainRamp{gain: gain}
	r.attackStep.Store(math.Float64bits(rampStepForDuration(attack, sampleRate)))
	r.releaseStep.Store(math.Float64bits(rampStepForDuration(release, sampleRate)))
	r.lastGain.Store(math.Float64bits(gain))
}

// attack/release load the current per-frame steps for this Process
// call; the caller decides which feeds gainRamp.step's rise vs fall.
func (r *rampedGain) attack() float64  { return math.Float64frombits(r.attackStep.Load()) }
func (r *rampedGain) release() float64 { return math.Float64frombits(r.releaseStep.Load()) }

// recordGain stores the gain applied to the most recently processed
// frame, for the GainDB reader.
func (r *rampedGain) recordGain(g float64) { r.lastGain.Store(math.Float64bits(g)) }

// GainDB reports the gain applied to the most recently processed
// frame, in dB. Safe from any goroutine while Process runs.
func (r *rampedGain) GainDB() float64 {
	return mutations.AmplitudeToDecibels(math.Float64frombits(r.lastGain.Load()))
}

// SetAttack atomically replaces the attack ramp time (def is the
// caller's zero-value default). Pass time.Nanosecond for an instant
// step. Returns ErrBadConfig for a negative duration. Safe from any
// goroutine.
func (r *rampedGain) SetAttack(d, def time.Duration) error {
	d, err := resolveDebounce(d, def)
	if err != nil {
		return err
	}
	r.attackStep.Store(math.Float64bits(rampStepForDuration(d, r.sampleRate)))
	return nil
}

// SetRelease atomically replaces the release ramp time (def is the
// caller's zero-value default). Pass time.Nanosecond for an instant
// step. Returns ErrBadConfig for a negative duration. Safe from any
// goroutine.
func (r *rampedGain) SetRelease(d, def time.Duration) error {
	d, err := resolveDebounce(d, def)
	if err != nil {
		return err
	}
	r.releaseStep.Store(math.Float64bits(rampStepForDuration(d, r.sampleRate)))
	return nil
}

// reset returns the ramp to gain (the caller's steady-state value)
// with the last-applied reading matching.
func (r *rampedGain) reset(gain float64) {
	r.ramp.gain = gain
	r.lastGain.Store(math.Float64bits(gain))
}
