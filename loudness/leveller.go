package loudness

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Leveller defaults substituted for the zero value of the corresponding
// LevellerConfig fields, plus the fixed internal constants of the
// control loop.
const (
	// defaultLevellerAttack is the time constant used when the leveller
	// is *cutting* gain (the programme is too loud): a fast 200 ms so
	// loud passages are pulled down promptly.
	defaultLevellerAttack = 200 * time.Millisecond

	// defaultLevellerRelease is the time constant used when the
	// leveller is *boosting* gain (the programme is too quiet): a slow
	// 3 s so the level is lifted gently and the noise floor / room tone
	// is not pumped up audibly between phrases.
	defaultLevellerRelease = 3 * time.Second

	// defaultLevellerGate is the momentary-loudness gate in LUFS below
	// which the gain is frozen (see LevellerConfig.Gate). -50 LUFS sits
	// well below normal programme material but above room tone, so
	// pauses and silences hold the last sensible gain instead of
	// ramping up to chase a noise floor.
	defaultLevellerGate = -50.0

	// defaultLevellerMaxGain is the default symmetric bound on both
	// boost and cut, ±12 dB — broad enough to level typical
	// programme-to-programme loudness spread without letting the AGC run
	// away on pathological material.
	defaultLevellerMaxGain = 12.0

	// levellerEmergencyTau is the fixed, fast time constant (40 ms) of
	// the emergency path that reacts to a sudden loud transient the
	// slower short-term loop has not yet seen.
	levellerEmergencyTau = 40 * time.Millisecond

	// levellerEmergencyLU is how far the *output* momentary loudness may
	// exceed Target (in LU) before the emergency path engages: 8 LU
	// over target is a clearly-too-loud transient worth reacting to at
	// once rather than waiting for the short-term loop.
	levellerEmergencyLU = 8.0

	// levellerBlockSeconds is the control-loop update cadence in
	// seconds — one gain decision per 100 ms block, matching the meter's
	// own gating hop. It is the "0.1" in the one-pole coefficient
	// α = 1 − exp(−0.1/τ).
	levellerBlockSeconds = 0.1
)

// LevellerConfig configures a Leveller. Every field's zero value
// selects a broadcast-oriented default, so
// LevellerConfig{SampleRate: 48000, Channels: 2} is a complete, valid
// configuration targeting -23 LUFS.
type LevellerConfig struct {
	// SampleRate is the stream sample rate in Hz. Must be positive.
	SampleRate int

	// Channels is the interleaved channel count. Must be in 1..64. The
	// leveller is linked: one gain is derived from the programme's
	// loudness and applied equally to every channel, preserving the
	// inter-channel balance.
	Channels int

	// Target is the loudness the leveller steers the programme toward,
	// in LUFS. The zero value selects TargetEBUR128 (-23). Must be
	// strictly negative (a zero-after-default or positive value returns
	// ErrBadTarget).
	Target float64

	// Attack is the gain-*cut* time constant (used when the programme
	// is too loud). The zero value selects 200 ms. Must not be
	// negative.
	Attack time.Duration

	// Release is the gain-*boost* time constant (used when the
	// programme is too quiet). The zero value selects 3 s. Must not be
	// negative.
	Release time.Duration

	// Gate is the momentary-loudness threshold in LUFS below which the
	// gain is frozen rather than updated, so pauses and near-silence
	// hold the last gain instead of ramping up to chase a noise floor.
	// The zero value selects -50 LUFS; pass math.Inf(-1) to disable
	// gating entirely (the gain then always tracks the programme).
	Gate float64

	// MaxBoost is the largest gain increase the leveller may apply, in
	// dB (a positive number). The zero value selects 12 dB. Must not be
	// negative. To allow no boost at all, pass a tiny non-zero value
	// (e.g. 1e-9); an exact 0 is treated as unset and selects the 12 dB
	// default.
	MaxBoost float64

	// MaxCut is the largest gain reduction the leveller may apply, in dB
	// (a positive number denoting attenuation). The zero value selects
	// 12 dB. Must not be negative. To allow no cut at all, pass a tiny
	// non-zero value (e.g. 1e-9); an exact 0 is treated as unset and
	// selects the 12 dB default.
	MaxCut float64

	// Ceiling is the true-peak ceiling in dBTP for the embedded output
	// limiter. The zero value selects CeilingEBUR128 (-1.0 dBTP).
	// Ignored when DisableLimiter is set. An exact 0 selects the
	// CeilingEBUR128 default; pass a tiny non-zero value (e.g. 1e-9) for
	// a literal 0 dBTP ceiling.
	Ceiling float64

	// DisableLimiter removes the embedded true-peak limiter from the
	// output stage. With it set the leveller applies gain only (no peak
	// protection) and its Latency drops to zero.
	DisableLimiter bool
}

// Leveller is an adaptive automatic-gain-control (AGC) mutations.Processor
// that steers a stream's loudness toward a Target over time — a
// broadcast-style "leveller" that rides the gain to keep speech or
// music at a consistent loudness without the operator riding a fader.
//
// # This is an original design, not a port
//
// Unlike the Meter and Limiter (which are bit-exact ports of, or built
// on, the vendored libebur128 reference), the Leveller has no reference
// implementation and no parity oracle: it is a spec-driven original
// design. Its behaviour is defined by this documentation and its unit
// tests (convergence, gate-freeze, bounds, emergency response, and
// zipper-free ramping), not by matching any external tool. The
// constants below encode common broadcast-leveller practice rather than
// a published standard.
//
// # How it works
//
// A feed-forward loudness Meter (ModeShortTerm) is fed the *input*
// signal — never the leveller's own output. Feed-forward metering is
// chosen deliberately: the desired gain is a well-defined function of
// the untouched programme loudness, and the loop cannot enter a
// feedback runaway, so it is unconditionally stable regardless of how
// aggressively the gain moves.
//
// Once per 100 ms block (the meter's gating cadence) the leveller reads
// momentary (M, trailing 400 ms) and short-term (S, trailing 3 s)
// loudness and updates a single control gain:
//
//   - Warm-up: until M is valid (the first 400 ms) the gain is held at
//     0 dB; once M is valid but S is not yet (first 3 s), the desired
//     gain is steered from M instead of S.
//   - Gate: if M is below Gate the gain is frozen (held), so pauses and
//     near-silence do not ramp the gain up chasing a noise floor.
//   - Desired gain: d = clamp(Target − L, −MaxCut, +MaxBoost), where L
//     is the LOUDER of short-term and momentary loudness, max(S, M).
//     Steering from the louder window is what lets one rule cover both
//     the steady case (S ≈ M) and a sudden loud transient (M ≫ S): the
//     loop reacts to a rising step immediately via M, and — unlike a
//     steer-from-S-only loop — does not boost back on the still-quiet S
//     while M is hot. A brief momentary dip (M < S) leaves L at S, so a
//     short quiet patch does not trigger a spurious cut.
//   - Smoothing: the control gain moves toward d by a one-pole step in
//     the dB domain, α = 1 − exp(−0.1/τ) per block, with τ = Attack
//     when cutting (d below the current gain) and τ = Release when
//     boosting. Fast down, slow up.
//   - Emergency time constant: if the *output* momentary loudness
//     (approximated as input M + current gain — cheap, and close enough
//     because the applied gain is nearly constant over a 400 ms window;
//     no second meter on the output is needed) exceeds Target + 8 LU,
//     the one-pole step uses a fast fixed τ = 40 ms instead of Attack,
//     catching a sudden loud transient before even the 200 ms attack
//     ramp would.
//
// The per-block gain decision is applied not as a step but as a
// per-sample geometric ramp across the following block (one multiply
// per frame: a precomputed ratio r = 10^((gNew−gPrev)/(20·N)) applied
// cumulatively), so the gain is continuous everywhere and no zipper
// noise is introduced at block boundaries.
//
// Finally, unless DisableLimiter is set, the gained signal passes
// through an embedded true-peak Limiter at Ceiling, which guarantees
// the output honours the peak ceiling even when the leveller is
// boosting. The limiter's own delay line handles its alignment; it is
// the sole source of the leveller's Latency.
//
// # Pumping and the gate
//
// Any AGC can "pump" — audibly ramp the gain up during quiet passages
// and back down when loud material returns. The slow 3 s boost and the
// gate together suppress the worst of it: quiet passages below Gate
// simply hold the previous gain rather than lifting the noise floor,
// and even above the gate the boost is deliberately sluggish.
//
// # Latency
//
// Latency is exactly the embedded limiter's latency (its lookahead plus
// the true-peak interpolator group delay), or zero when DisableLimiter
// is set — the leveller's own gain stage adds none. When rendering a
// finite clip, flush the tail as for the Limiter (see Latency and
// mutations.Audio.RenderWithEffects).
//
// # Usage
//
// Wrap it as a streaming effect with
// timeline.NewEffectSource(src, lev), or install it on a mixer's master
// bus via mixer.Config.Processors, to keep a live programme at a
// consistent loudness. Like every Processor it holds per-stream state
// and is NOT safe for concurrent use; drive one Leveller from a single
// goroutine.
type Leveller struct {
	sampleRate int
	channels   int
	target     float64 // LUFS
	gate       float64 // LUFS, math.Inf(-1) when gating disabled
	maxBoost   float64 // dB, positive
	maxCut     float64 // dB, positive
	ceiling    float64 // dBTP (embedded limiter)

	alphaAttack    float64 // one-pole coefficient for cutting
	alphaRelease   float64 // one-pole coefficient for boosting
	alphaEmergency float64 // one-pole coefficient for the emergency path

	meter       *Meter   // fed the INPUT signal (feed-forward)
	limiter     *Limiter // nil when DisableLimiter
	blockFrames int      // frames per 100 ms control block

	// --- per-stream state (all cleared by Reset) ---

	blockInput []float64 // one block of pre-gain input, fed to the meter
	blockPos   int       // frames accumulated into the current block

	// Gain ramp across the current block. linGain is the linear gain
	// applied to the current frame; it is multiplied by r each frame to
	// trace a geometric (linear-in-dB) ramp from gStartDB to gTargetDB.
	gStartDB  float64
	gTargetDB float64
	linGain   float64
	r         float64
}

// NewLeveller constructs a Leveller from cfg, substituting the
// broadcast-oriented defaults for any zero-valued field (see
// LevellerConfig). It returns:
//
//   - ErrBadSampleRate if SampleRate < 16,
//   - ErrBadChannels if Channels is not in 1..64,
//   - ErrBadTarget if the resolved Target is not strictly negative,
//   - ErrBadConfig if Attack or Release is negative, MaxBoost or MaxCut
//     is negative, Gate is NaN, or Ceiling is NaN or infinite (validated
//     even when DisableLimiter is set).
func NewLeveller(cfg LevellerConfig) (*Leveller, error) {
	if cfg.SampleRate < minSampleRate {
		return nil, ErrBadSampleRate
	}
	if cfg.Channels < 1 || cfg.Channels > maxChannels {
		return nil, ErrBadChannels
	}

	target := cfg.Target
	if target == 0 {
		target = TargetEBUR128
	}
	if target > 0 || math.IsNaN(target) {
		return nil, ErrBadTarget
	}

	attack := cfg.Attack
	if attack == 0 {
		attack = defaultLevellerAttack
	}
	release := cfg.Release
	if release == 0 {
		release = defaultLevellerRelease
	}
	if attack < 0 || release < 0 {
		return nil, ErrBadConfig
	}

	gate := cfg.Gate
	if gate == 0 {
		gate = defaultLevellerGate
	}
	if math.IsNaN(gate) {
		return nil, ErrBadConfig
	}

	maxBoost := cfg.MaxBoost
	if maxBoost == 0 {
		maxBoost = defaultLevellerMaxGain
	}
	maxCut := cfg.MaxCut
	if maxCut == 0 {
		maxCut = defaultLevellerMaxGain
	}
	if maxBoost < 0 || maxCut < 0 {
		return nil, ErrBadConfig
	}

	// Validate the ceiling unconditionally — even when DisableLimiter is
	// set and no limiter is built — so a NaN/Inf ceiling is a
	// configuration error rather than a silently-ignored field.
	ceiling, err := resolveCeiling(cfg.Ceiling, false)
	if err != nil {
		return nil, err
	}

	meter, err := NewMeter(cfg.SampleRate, cfg.Channels, ModeShortTerm)
	if err != nil {
		return nil, err
	}

	var lim *Limiter
	if !cfg.DisableLimiter {
		lim, err = NewLimiter(LimiterConfig{
			SampleRate: cfg.SampleRate,
			Channels:   cfg.Channels,
			Ceiling:    ceiling,
		})
		if err != nil {
			return nil, err
		}
	}

	blockFrames := (cfg.SampleRate + 5) / 10

	return &Leveller{
		sampleRate:     cfg.SampleRate,
		channels:       cfg.Channels,
		target:         target,
		gate:           gate,
		maxBoost:       maxBoost,
		maxCut:         maxCut,
		ceiling:        ceiling,
		alphaAttack:    1 - math.Exp(-levellerBlockSeconds/attack.Seconds()),
		alphaRelease:   1 - math.Exp(-levellerBlockSeconds/release.Seconds()),
		alphaEmergency: 1 - math.Exp(-levellerBlockSeconds/levellerEmergencyTau.Seconds()),
		meter:          meter,
		limiter:        lim,
		blockFrames:    blockFrames,
		blockInput:     make([]float64, blockFrames*cfg.Channels),
		linGain:        1,
		r:              1,
	}, nil
}

// Process levels samples in place. samples is interleaved with
// Channels() channels. Frames may be delivered in any chunking — a
// mixer's ~10 ms buffers or a whole offline clip — because the 100 ms
// control block is accumulated internally against an absolute frame
// count, not against the Process call boundary: the gain trajectory,
// and therefore the output, is identical regardless of how the stream
// is chunked.
//
// Each frame is scaled by the current point on the block's gain ramp;
// the untouched input is fed to the internal feed-forward meter. When a
// block completes, the next block's gain is decided and its ramp
// precomputed. Finally the whole buffer passes through the embedded
// limiter (unless disabled), whose delay line carries its own
// alignment across calls.
func (lv *Leveller) Process(samples []float64) {
	ch := lv.channels
	frames := len(samples) / ch

	for f := 0; f < frames; f++ {
		base := f * ch
		ib := lv.blockPos * ch
		for c := 0; c < ch; c++ {
			in := samples[base+c]
			lv.blockInput[ib+c] = in
			samples[base+c] = in * lv.linGain
		}
		lv.linGain *= lv.r
		lv.blockPos++
		if lv.blockPos == lv.blockFrames {
			lv.completeBlock()
		}
	}

	if lv.limiter != nil {
		lv.limiter.Process(samples)
	}
}

// completeBlock is invoked once a full 100 ms input block has been
// accumulated: it feeds that block to the feed-forward meter, decides
// the next block's control gain, and precomputes the per-frame
// geometric ramp from the gain reached at this block's end to that new
// target.
func (lv *Leveller) completeBlock() {
	lv.meter.AddFrames(lv.blockInput)

	gEnd := lv.gTargetDB // gain the just-finished ramp reached
	gNew := lv.decideGain(gEnd)

	lv.gStartDB = gEnd
	lv.gTargetDB = gNew
	// Re-seed linGain exactly from gEnd rather than trusting the
	// accumulated per-frame products, shedding any multiplicative drift
	// at every block boundary (the ramp is continuous because gStartDB
	// == the previous gTargetDB). mutations.Decibels(db) == 10^(db/20),
	// so the per-frame ratio 10^((gNew-gEnd)/(20·N)) is Decibels of the
	// per-frame dB step (gNew-gEnd)/N.
	lv.linGain = mutations.Decibels(gEnd)
	lv.r = mutations.Decibels((gNew - gEnd) / float64(lv.blockFrames))
	lv.blockPos = 0
}

// decideGain computes the control gain (dB) for the next block given
// gPrev, the gain in effect at the end of the current block, and the
// meter's current momentary/short-term readings. See the type doc for
// the warm-up, gate, desired-gain, smoothing, and emergency rules.
func (lv *Leveller) decideGain(gPrev float64) float64 {
	m, _ := lv.meter.Momentary()
	if math.IsInf(m, -1) {
		// Warm-up (no valid momentary yet) or true silence: hold.
		return gPrev
	}
	if m < lv.gate {
		// Gate: freeze the gain over pauses / near-silence.
		return gPrev
	}

	// Steer from the LOUDER of short-term (S) and momentary (M). This
	// unifies the ordinary short-term loop with the "emergency" reaction
	// to a sudden loud transient: on a rising step M jumps well above
	// the slow 3 s S, so max(S, M) tracks M and the loop cuts at once —
	// crucially, it does NOT boost back on the now-stale (still quiet) S
	// while M is hot, which a steer-from-S-only loop would, leaving the
	// output stuck ~8 LU over target until S caught up. On a brief dip
	// (M below S) max stays at S, so a momentary quiet patch does not
	// trigger a spurious cut. Before S is valid (warm-up) M alone is
	// used.
	loud := m
	if s, _ := lv.meter.ShortTerm(); !math.IsInf(s, -1) && s > loud {
		loud = s
	}
	desired := clampDB(lv.target-loud, -lv.maxCut, lv.maxBoost)

	// Time constant: fastest (emergency, 40 ms) when the output
	// momentary (≈ input M + current gain) is running more than
	// levellerEmergencyLU over target — a hot transient worth catching
	// before the attack ramp would; otherwise Attack when cutting
	// (desired below the current gain) and Release when boosting.
	var alpha float64
	switch {
	case m+gPrev > lv.target+levellerEmergencyLU:
		alpha = lv.alphaEmergency
	case desired < gPrev:
		alpha = lv.alphaAttack
	default:
		alpha = lv.alphaRelease
	}
	return gPrev + alpha*(desired-gPrev)
}

// clampDB constrains a desired dB gain to [lo, hi].
func clampDB(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Reset returns the leveller to its just-constructed state: the
// feed-forward meter, the block accumulator, and the embedded limiter
// are all cleared, and the control gain returns to 0 dB. Configuration
// is retained. After Reset the leveller produces identical output to a
// freshly constructed one of the same configuration.
func (lv *Leveller) Reset() {
	lv.meter.Reset()
	if lv.limiter != nil {
		lv.limiter.Reset()
	}
	lv.blockPos = 0
	lv.gStartDB = 0
	lv.gTargetDB = 0
	lv.linGain = 1
	lv.r = 1
}

// Latency reports the processing latency the leveller adds: the
// embedded limiter's latency, or zero when DisableLimiter was set. Use
// it as the tail length when flushing a finite render (see the type
// doc) and to time-align the levelled output against an unprocessed
// reference.
func (lv *Leveller) Latency() time.Duration {
	if lv.limiter != nil {
		return lv.limiter.Latency()
	}
	return 0
}

// GainDB reports the current smoothed control gain in dB — the value
// the leveller most recently steered toward at a block boundary
// (positive is boost, negative is cut). It is a UI/telemetry read of
// the control loop, not a per-sample figure; drive Process, then read.
func (lv *Leveller) GainDB() float64 { return lv.gTargetDB }

// SampleRate reports the sample rate the leveller was configured for.
func (lv *Leveller) SampleRate() int { return lv.sampleRate }

// Channels reports the interleaved channel count the leveller expects.
func (lv *Leveller) Channels() int { return lv.channels }

// Target reports the resolved loudness target in LUFS (with the
// zero-value default applied).
func (lv *Leveller) Target() float64 { return lv.target }

// ensure Leveller satisfies the Processor contract.
var _ mutations.Processor = (*Leveller)(nil)
