package vad

import (
	"math"
	"sync/atomic"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Gate defaults, substituted for the zero value of the corresponding
// GateConfig field.
const (
	defaultGateAttack  = 5 * time.Millisecond
	defaultGateRelease = 100 * time.Millisecond
)

// GateConfig configures a Gate.
type GateConfig struct {
	// SampleRate is the stream sample rate in Hz. Must be positive
	// and must equal the injected Detector's SampleRate.
	SampleRate int

	// Channels is the interleaved channel count, in 1..64. Must equal
	// the injected Detector's Channels. The gate is linked: one gain
	// is derived from the detector state and applied to every channel.
	Channels int

	// Detector is the VAD engine that decides when the gate opens.
	// Required (ErrNilDetector when nil) and must be constructed for
	// this exact SampleRate/Channels (ErrBadConfig otherwise).
	//
	// The Gate FEEDS this detector: every Process call pushes the
	// incoming (pre-delay, pre-gain) audio through Detector.Process
	// before gating, so the detector hears the dry signal. Because a
	// Detector carries per-stream state, the injected detector must
	// NOT also be inserted anywhere else (another chain position,
	// another Gate, a direct Process caller) — that would feed it the
	// same audio twice and corrupt its stream position. This is the
	// same one-feeder rule every mutations.Processor carries (see its
	// doc): a Ducker READING the same detector is fine (readers are
	// goroutine-safe); a second FEEDER is not.
	Detector Detector

	// Attack is how long the gate takes to ramp fully open once the
	// detector reports speech (linear ramp in the gain domain; see
	// Gate's click bound). The zero value selects 5 ms. Must not be
	// negative. An exact 0 selects the default; pass time.Nanosecond
	// for an effectively instant (one frame) attack.
	Attack time.Duration

	// Release is how long the gate takes to ramp fully closed after
	// the detector reports silence. The zero value selects 100 ms.
	// Must not be negative. An exact 0 selects the default; pass
	// time.Nanosecond for an instant release.
	Release time.Duration

	// FloorDB is the closed-gate gain in dB. The zero value selects
	// full mute (−Inf dB / zero gain); −Inf is also accepted
	// explicitly. A finite negative value (e.g. −40) gives a partial
	// gate that ducks rather than silences. Must not be positive or
	// NaN. A literal 0 dB floor would make the gate a no-op — pass a
	// tiny negative value (e.g. −1e-9) if that is genuinely wanted.
	FloorDB float64

	// Lookahead delays the AUDIO path (not the detector) so the gate
	// can open before the speech that triggered it reaches the
	// output: the detector hears the signal Lookahead early relative
	// to what is being gated. The zero value selects no lookahead —
	// zero IS the "none" value here, there is no hidden default.
	// Recommended: Detector.DecisionLatency(), which makes the
	// detector's announcement delay invisible in the gated output.
	// Must not be negative. Sets the gate's Latency().
	Lookahead time.Duration
}

// Gate is a speech gate: a mutations.Processor that attenuates its
// stream to FloorDB whenever the injected Detector reports silence,
// ramping open (Attack) and closed (Release) around speech. Typical
// use: strip room tone, keyboard noise, and breath between utterances
// on a voice track.
//
// # Signal flow
//
// Per Process call, the incoming audio is first fed — dry and
// undelayed — to the injected Detector (which the Gate owns feeding;
// see GateConfig.Detector). Each output frame is then the input frame
// Lookahead earlier, scaled by a gain that ramps linearly toward 1
// (detector active) or the floor (inactive). The gain target updates
// at the detector's decision-frame cadence; the ramps make the
// transitions click-free.
//
// # Click bound
//
// The gain is a linear ramp, so the per-frame gain change never
// exceeds max(1/attackFrames, 1/releaseFrames) — a hard bound the
// tests pin. There is no gain discontinuity at any transition.
//
// # Latency honesty and tail flushing
//
// Latency()/LatencyFrames() report exactly the configured Lookahead
// (the Limiter convention): an impulse presented at input frame k
// emerges at output frame k+LatencyFrames(). Process is
// length-preserving, so the last LatencyFrames() frames of a finite
// stream are still inside the delay line when the input ends — flush
// them by wrapping the source in
// timeline.NewEffectSource(src, gate).WithTail(gate.Latency()), or by
// appending gate.Latency() of silence in an offline render.
//
// # Concurrency
//
// Process and Reset must be driven from a single goroutine. The
// setters (SetFloorDB, SetAttack, SetRelease) and the GainDB reader
// are safe from any goroutine while Process runs (single-word
// atomics, snapshotted once per Process call).
type Gate struct {
	sampleRate int
	channels   int
	det        Detector

	lookaheadFrames int
	lookaheadDur    time.Duration

	delay    []float64 // interleaved delay line, lookaheadFrames*channels
	delayPos int       // frame write cursor

	gain rampedGain // ramp + attack/release steps + last-applied reader

	// Live parameters (float64 bits; written by setters on any
	// goroutine, snapshotted once per Process call).
	floorLin atomic.Uint64 // closed-gate linear gain
}

// resolveGateFloor applies the zero-value default (full mute) and
// validates a floor/depth dB value: NaN and positive values are
// rejected; −Inf maps to zero gain.
func resolveGateFloor(db float64) (float64, error) {
	if math.IsNaN(db) || db > 0 {
		return 0, ErrBadConfig
	}
	if db == 0 || math.IsInf(db, -1) {
		return 0, nil // full mute
	}
	return mutations.Decibels(db), nil
}

// NewGate constructs a Gate from cfg, substituting the documented
// defaults for zero-valued fields. It returns ErrNilDetector (nil
// Detector), ErrBadSampleRate (non-positive rate), ErrBadChannels
// (channels not in 1..64), or ErrBadConfig (detector format mismatch,
// negative durations, NaN or positive FloorDB).
func NewGate(cfg GateConfig) (*Gate, error) {
	if cfg.Detector == nil {
		return nil, ErrNilDetector
	}
	if cfg.SampleRate <= 0 {
		return nil, ErrBadSampleRate
	}
	if err := validateChannels(cfg.Channels); err != nil {
		return nil, err
	}
	if cfg.Detector.SampleRate() != cfg.SampleRate || cfg.Detector.Channels() != cfg.Channels {
		return nil, ErrBadConfig
	}
	if cfg.Attack < 0 || cfg.Release < 0 || cfg.Lookahead < 0 {
		return nil, ErrBadConfig
	}
	floorLin, err := resolveGateFloor(cfg.FloorDB)
	if err != nil {
		return nil, err
	}

	attack := cfg.Attack
	if attack == 0 {
		attack = defaultGateAttack
	}
	release := cfg.Release
	if release == 0 {
		release = defaultGateRelease
	}

	lookaheadFrames := int(mutations.DurationToFrames(cfg.Lookahead, cfg.SampleRate))
	g := &Gate{
		sampleRate:      cfg.SampleRate,
		channels:        cfg.Channels,
		det:             cfg.Detector,
		lookaheadFrames: lookaheadFrames,
		lookaheadDur:    cfg.Lookahead,
		delay:           make([]float64, lookaheadFrames*cfg.Channels),
	}
	// a stream starts in silence: gate closed.
	g.gain.init(cfg.SampleRate, floorLin, attack, release)
	g.floorLin.Store(math.Float64bits(floorLin))
	return g, nil
}

// Process gates samples in place. samples is interleaved with
// Channels() channels; any trailing partial frame is left untouched.
// The incoming audio is fed to the injected Detector frame by frame
// (so detector events fire from within this call), then delayed by
// LatencyFrames() and scaled by the ramped gate gain. Buffers of any
// whole-frame size are accepted and chunking is invisible to the
// output. Never panics.
func (g *Gate) Process(samples []float64) {
	ch := g.channels
	frames := len(samples) / ch
	if frames == 0 {
		return
	}

	floor := math.Float64frombits(g.floorLin.Load())
	rise := g.gain.attack()
	fall := g.gain.release()

	for f := 0; f < frames; f++ {
		base := f * ch
		frame := samples[base : base+ch]

		// Feed the detector the dry, undelayed frame — pass-through,
		// so the audio is untouched. Per-frame feeding keeps the gain
		// target updating at the detector's decision cadence rather
		// than once per buffer.
		g.det.Process(frame)

		target := floor
		if g.det.Active() {
			target = 1
		}
		gain := g.gain.ramp.step(target, rise, fall)

		if g.lookaheadFrames > 0 {
			dbase := g.delayPos * ch
			for c := 0; c < ch; c++ {
				old := g.delay[dbase+c]
				g.delay[dbase+c] = frame[c]
				frame[c] = old * gain
			}
			g.delayPos++
			if g.delayPos == g.lookaheadFrames {
				g.delayPos = 0
			}
		} else {
			for c := 0; c < ch; c++ {
				frame[c] *= gain
			}
		}
	}
	g.gain.recordGain(g.gain.ramp.gain)
}

// Reset returns the gate to its just-constructed state: the delay
// line is re-primed with zeros, the gain returns to the closed floor,
// and the injected Detector is Reset too (the Gate owns its stream —
// see GateConfig.Detector). Configuration, including live-set values,
// is retained.
func (g *Gate) Reset() {
	for i := range g.delay {
		g.delay[i] = 0
	}
	g.delayPos = 0
	floor := math.Float64frombits(g.floorLin.Load())
	g.gain.reset(floor)
	g.det.Reset()
}

// Latency reports the constant processing latency the gate adds:
// exactly the configured Lookahead. Use it as the tail length when
// flushing a finite render (see the type doc).
func (g *Gate) Latency() time.Duration { return g.lookaheadDur }

// LatencyFrames reports the gate's latency in frames: an impulse
// presented at input frame k appears at output frame
// k+LatencyFrames().
func (g *Gate) LatencyFrames() int { return g.lookaheadFrames }

// GainDB reports the gate gain applied to the most recently processed
// frame, in dB (0 = fully open; −Inf = fully closed at a full-mute
// floor). Safe from any goroutine while Process runs.
func (g *Gate) GainDB() float64 {
	return g.gain.GainDB()
}

// SetFloorDB atomically replaces the closed-gate floor. Semantics
// match GateConfig.FloorDB (0 selects full mute; −Inf accepted; NaN
// and positive values return ErrBadConfig). Takes effect from the
// next Process call, reached through the release ramp. Safe from any
// goroutine.
func (g *Gate) SetFloorDB(db float64) error {
	lin, err := resolveGateFloor(db)
	if err != nil {
		return err
	}
	g.floorLin.Store(math.Float64bits(lin))
	return nil
}

// SetAttack atomically replaces the attack time. Zero selects the
// 5 ms default; pass time.Nanosecond for an instant attack; negative
// returns ErrBadConfig. Safe from any goroutine.
func (g *Gate) SetAttack(d time.Duration) error {
	return g.gain.SetAttack(d, defaultGateAttack)
}

// SetRelease atomically replaces the release time. Zero selects the
// 100 ms default; pass time.Nanosecond for an instant release;
// negative returns ErrBadConfig. Safe from any goroutine.
func (g *Gate) SetRelease(d time.Duration) error {
	return g.gain.SetRelease(d, defaultGateRelease)
}

// SampleRate reports the sample rate the gate was configured for.
func (g *Gate) SampleRate() int { return g.sampleRate }

// Channels reports the interleaved channel count the gate expects.
func (g *Gate) Channels() int { return g.channels }
