package loudness

import (
	"math"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// LimiterConfig configures a Limiter. The zero value of every field
// selects a sensible EBU R128 default, so LimiterConfig{SampleRate:
// 48000, Channels: 2} is a complete, valid configuration.
type LimiterConfig struct {
	// SampleRate is the stream sample rate in Hz. Must be positive.
	SampleRate int

	// Channels is the interleaved channel count the Limiter will be
	// driven with. Must be in 1..64. The Limiter is *linked*: a single
	// gain is derived from the loudest channel each frame and applied
	// to every channel, so the inter-channel balance (stereo image) is
	// preserved through gain reduction (see the type doc).
	Channels int

	// Ceiling is the true-peak ceiling in dBTP that the output must not
	// exceed (modulo the documented inter-sample regrowth slack). The
	// zero value selects CeilingEBUR128 (-1.0 dBTP), the EBU R128
	// broadcast headroom target. Values above 0 dBTP are unusual but
	// legal (they let inter-sample peaks past full scale); any finite
	// value is accepted, NaN/±Inf are rejected. An exact 0 selects the
	// CeilingEBUR128 default; pass a tiny non-zero value (e.g. 1e-9) for
	// a literal 0 dBTP ceiling.
	Ceiling float64

	// Lookahead is how far ahead the limiter sees a peak before it
	// arrives, so the attack ramp can bring the gain down smoothly
	// rather than instantaneously (which would distort). It sets the
	// limiter's latency together with the true-peak interpolator's
	// group delay (see Latency). The zero value selects 5ms. Must not
	// be negative. An exact 0 selects the 5 ms default; pass a tiny
	// non-zero duration (e.g. time.Nanosecond, which rounds to zero
	// lookahead frames) if you genuinely want no lookahead.
	Lookahead time.Duration

	// Release is the time constant of the gain *recovery* after a peak
	// passes: the control signal relaxes back toward unity with a
	// one-pole envelope whose coefficient is exp(-1/(Release*rate)).
	// The zero value selects 100ms. Must not be negative. (Attack is
	// not a separate parameter — it is governed by Lookahead, because a
	// lookahead limiter's attack IS the ramp across the lookahead
	// window.)
	Release time.Duration
}

// defaultLimiterLookahead and defaultLimiterRelease are the EBU-R128
// oriented defaults substituted for the zero value of the corresponding
// LimiterConfig fields.
const (
	defaultLimiterLookahead = 5 * time.Millisecond
	defaultLimiterRelease   = 100 * time.Millisecond
)

// limiterSumRecompute is how often (in frames) the boxcar smoother's
// running sum is recomputed from scratch to shed accumulated
// floating-point drift. The running sum adds one value and subtracts
// one value per frame; over millions of frames the repeated add/sub can
// drift by a few ULP, so every 1<<20 frames (~21.8 s at 48 kHz) the sum
// is rebuilt exactly from the window contents. This keeps a
// non-limiting stream bit-transparent (see the Limiter type doc) over
// arbitrarily long runs. 1<<20 is a power of two so the check is a
// cheap mask.
const limiterSumRecompute = 1 << 20

// Limiter is a lookahead true-peak limiter: a streaming
// mutations.Processor that guarantees its output never exceeds a
// configured true-peak Ceiling (in dBTP), while introducing the least
// gain reduction that achieves that, smoothly enough to avoid audible
// distortion.
//
// # What it protects against
//
// A digital sample stream can reconstruct ("inter-sample") peaks
// between its samples that are higher than any stored sample value — a
// signal whose samples all sit at -1 dBFS can still hit +2 dBTP once a
// DAC or a downstream sample-rate conversion reconstructs the waveform.
// BS.1770-4 / EBU R128 therefore specify a *true-peak* meter, and
// broadcasters cap true peak (CeilingEBUR128 = -1 dBTP) rather than
// sample peak. This limiter detects the true peak with the exact same
// 49-tap polyphase oversampling interpolator the Meter uses for
// ModeTruePeak (loudness/internal/r128), so what it limits is what a
// compliant true-peak meter measures.
//
// # DSP (why the output cannot overshoot in the sample domain)
//
// Per input frame the limiter computes p, the linked true peak across
// all channels (the max of the sample peak and every oversampled
// inter-sample magnitude). From p it derives the instantaneous gain
// needed to bring that frame to the ceiling, c = min(1, ceilingLin/p).
// A one-pole *release* smoother turns c into a control signal that can
// drop instantly (attack) but only recover gradually,
// u = min(c, 1 + (u_prev-1)*kRel). u then passes through a two-stage
// cascade over a window of L frames:
//
//   - a sliding MINIMUM m over the last L values of u (an O(1)
//     monotonic deque), which pulls the gain down to its target
//     *before* the peak arrives — this is what the lookahead buys; and
//   - a boxcar AVERAGE g over the last L values of m, which rounds the
//     sharp corners of the minimum into a click-free ramp.
//
// The output is the input delayed by L frames, scaled by g:
// y_t = x_{t-L} * g_t, read from a delay line primed with zeros. Taking
// the minimum first and averaging second means the gain applied to any
// output sample is never larger than the gain that sample's own peak
// demanded, so the *sample-domain* output provably never exceeds the
// ceiling.
//
// # The inter-sample regrowth caveat
//
// Reducing the gain smoothly (the boxcar) means the gain is not a
// perfect brick wall at the ceiling; a limited waveform can, after
// gain reduction, reconstruct an inter-sample peak fractionally above
// the ceiling — typically 0.1-0.3 dB. This is inherent to any
// low-distortion true-peak limiter and is why the default -1 dBTP
// ceiling leaves margin below 0 dBFS. Downstream, treat the ceiling as
// "within a few tenths of a dB", not exact.
//
// # Latency and how to flush the tail
//
// Because the output is the input delayed by L frames, the limiter has
// a constant latency of L frames (LatencyFrames) = Latency. Process is
// length-preserving: each call returns exactly as many samples as it
// was given, so the last L frames of a finite stream are still inside
// the delay line when the input ends. To flush them in an offline
// render, run L frames of silence through the limiter after the signal:
//
//	lim, _ := loudness.NewLimiter(loudness.LimiterConfig{
//		SampleRate: a.SampleRate, Channels: a.Channels})
//	out := a.RenderWithEffects([]mutations.Processor{lim}, lim.Latency())
//
// mutations.Audio.RenderWithEffects appends the tail of silence for
// you; equivalently, wrap a streaming source with
// timeline.NewEffectSource(src, lim).WithTail(lim.Latency()) so the
// EffectSource keeps pulling zero-input frames through the limiter
// until the delayed signal has fully drained. The leading L frames of
// the output are the primed zeros (the delay-line fill); trim them if
// exact time alignment with the input matters.
//
// # ≥192 kHz
//
// At sample rates of 192 kHz and above the interpolator is bypassed
// (BS.1770 treats the true peak as the sample peak at those rates, as
// there is no meaningful oversampling headroom), so the limiter runs
// purely in the sample domain there. It still limits correctly; it
// simply has no inter-sample term to add and no interpolator group
// delay, so L reduces to the lookahead alone.
//
// # Concurrency
//
// Like every mutations.Processor, a Limiter carries per-stream state
// (the delay line, the control smoother, the interpolator history) and
// is NOT safe for concurrent use; drive one Limiter from a single
// goroutine. For a mutex-guarded meter on a shared bus use Monitor.
type Limiter struct {
	sampleRate int
	channels   int
	ceilingLin float64 // linear amplitude equivalent of the dBTP ceiling
	ceilingDB  float64 // resolved ceiling in dBTP (for reference)

	lookaheadFrames int           // lookahead in input frames
	groupDelay      int           // interpolator FIR group delay in input frames
	l               int           // total lookahead L = lookaheadFrames + groupDelay (>=1)
	lookaheadDur    time.Duration // resolved lookahead
	releaseDur      time.Duration // resolved release
	kRel            float64       // one-pole release coefficient exp(-1/(Release*rate))

	tp     *r128.TruePeaker // nil at >=192 kHz (bypass)
	factor int              // oversampling factor (1 when bypassed)

	// Per-buffer float32 scratch for the interpolator, grown on demand
	// and fully overwritten each call (mirrors r128.TruePeaker's own
	// scratch strategy — keeps the steady state allocation-free).
	in32  []float32
	out32 []float32

	// --- per-stream state (all cleared by Reset) ---

	delay    []float64 // interleaved delay line, len l*channels, primed with zeros
	delayPos int       // frame write cursor into delay

	spRing []float64 // sample-peak delay (len groupDelay), aligns the sample-peak floor with the interpolator's delayed output
	spPos  int

	uPrev float64 // previous release-smoothed control value u_{t-1}

	// Monotonic deque for the sliding minimum of u over the last L
	// frames. Kept as a fixed-capacity ring (max L live entries) of
	// (value, position) pairs, values non-decreasing from the front so
	// the front is always the window minimum.
	dqVal  []float64
	dqIdx  []int64
	dqHead int
	dqSize int

	mHist []float64 // ring of the last L sliding-minimum values, len l
	mSum  float64   // running sum of mHist (the boxcar numerator)

	pos      int64   // monotonic frame counter across all Process calls
	lastGain float64 // g applied to the most recent frame (for GainReductionDB)
}

// NewLimiter constructs a Limiter from cfg, substituting EBU-R128
// defaults for any zero-valued field (see LimiterConfig). It returns:
//
//   - ErrBadSampleRate if SampleRate < 16,
//   - ErrBadChannels if Channels is not in 1..64,
//   - ErrBadConfig if Lookahead or Release is negative, or if Ceiling
//     is NaN or infinite.
//
// A Ceiling of exactly 0 is treated as "unset" and becomes
// CeilingEBUR128 (-1.0 dBTP); pass a tiny non-zero value (e.g. 1e-9) if
// you genuinely want a 0 dBTP ceiling.
func NewLimiter(cfg LimiterConfig) (*Limiter, error) {
	// The limiter builds an r128.TruePeaker (not a full meter State), which
	// alone would not divide-by-zero at a sub-16 Hz rate; the < 16 bound is
	// applied anyway so every public constructor in this package shares one
	// consistent minimum (see minSampleRate), and so a Limiter embedded in a
	// Leveller/Normalize rejects the same rates the meter does.
	if cfg.SampleRate < minSampleRate {
		return nil, ErrBadSampleRate
	}
	if cfg.Channels < 1 || cfg.Channels > maxChannels {
		return nil, ErrBadChannels
	}
	if cfg.Lookahead < 0 || cfg.Release < 0 {
		return nil, ErrBadConfig
	}
	ceilingDB, err := resolveCeiling(cfg.Ceiling, false)
	if err != nil {
		return nil, err
	}

	lookahead := cfg.Lookahead
	if lookahead == 0 {
		lookahead = defaultLimiterLookahead
	}
	release := cfg.Release
	if release == 0 {
		release = defaultLimiterRelease
	}

	tp := r128.NewTruePeaker(cfg.SampleRate, cfg.Channels)
	factor := tp.Factor() // 1 when tp is nil (>=192 kHz bypass)

	// Interpolator FIR group delay, in INPUT frames. The interpolator
	// is a linear-phase FIR of odd length `taps`, whose group delay is
	// (taps-1)/2 samples at the OVERSAMPLED rate. taps is not exported,
	// but it is recoverable from the two accessors that are:
	// r128 sizes its per-channel delay line as (taps+factor-1)/factor,
	// i.e. Delay()*factor == taps+factor-1, so taps = Delay()*factor -
	// factor + 1 (== 49). Dividing the output-rate group delay by the
	// factor converts it to input frames: 6 frames at 4x (<96 kHz),
	// 12 at 2x (96..192 kHz), 0 when bypassed.
	groupDelay := 0
	if tp != nil {
		taps := tp.Delay()*factor - factor + 1
		groupDelay = ((taps - 1) / 2) / factor
	}

	lookaheadFrames := int(mutations.DurationToFrames(lookahead, cfg.SampleRate))
	l := lookaheadFrames + groupDelay
	if l < 1 {
		// A degenerate sub-frame lookahead at a bypassed rate could
		// leave L at zero; the delay line and the sliding windows need
		// at least one frame to be well-defined. Clamp to one frame.
		l = 1
	}

	lim := &Limiter{
		sampleRate:      cfg.SampleRate,
		channels:        cfg.Channels,
		ceilingLin:      mutations.Decibels(ceilingDB),
		ceilingDB:       ceilingDB,
		lookaheadFrames: lookaheadFrames,
		groupDelay:      groupDelay,
		l:               l,
		lookaheadDur:    lookahead,
		releaseDur:      release,
		kRel:            math.Exp(-1.0 / (release.Seconds() * float64(cfg.SampleRate))),
		tp:              tp,
		factor:          factor,
		delay:           make([]float64, l*cfg.Channels),
		spRing:          make([]float64, groupDelay),
		uPrev:           1.0,
		dqVal:           make([]float64, l+1),
		dqIdx:           make([]int64, l+1),
		mHist:           make([]float64, l),
		lastGain:        1.0,
	}
	// Prime the boxcar window with unity gain so a stream begins at 0 dB
	// of reduction (the primed-zero output frames are unaffected either
	// way, but this keeps GainReductionDB honest during warm-up).
	for i := range lim.mHist {
		lim.mHist[i] = 1.0
	}
	lim.mSum = float64(l)
	return lim, nil
}

// Process limits samples in place. samples is interleaved with
// Channels() channels; its length should be a multiple of Channels().
// Any trailing partial frame (a length not divisible by the channel
// count — which well-formed interleaved audio never has) is left
// untouched rather than corrupting the delay-line alignment. Buffers of
// any whole-frame size are accepted, and the result is identical to
// feeding the whole stream at once: all state (delay line, control
// smoother, interpolator history, sliding windows) carries across
// calls, so chunking is invisible.
//
// The output is the input delayed by LatencyFrames() and scaled by the
// smoothed limiting gain (see the type doc). Never panics.
func (l *Limiter) Process(samples []float64) {
	ch := l.channels
	frames := len(samples) / ch
	if frames == 0 {
		return
	}

	// Oversample the whole buffer once (frame-by-frame extraction
	// follows). The float32 narrowing and unity scaling replicate
	// libebur128's double-input true-peak path exactly, so detection
	// matches the Meter's ModeTruePeak reading.
	if l.tp != nil {
		need := frames * ch
		if cap(l.in32) < need {
			l.in32 = make([]float32, need)
		}
		l.in32 = l.in32[:need]
		for k := 0; k < need; k++ {
			l.in32[k] = float32(samples[k])
		}
		outNeed := frames * l.factor * ch
		if cap(l.out32) < outNeed {
			l.out32 = make([]float32, outNeed)
		}
		l.out32 = l.out32[:outNeed]
		l.tp.Oversample(l.in32, l.out32)
	}

	fch := l.factor * ch
	for f := 0; f < frames; f++ {
		base := f * ch

		// Sample peak of this input frame (linked: max across channels).
		sp := 0.0
		for c := 0; c < ch; c++ {
			v := math.Abs(samples[base+c])
			if v > sp {
				sp = v
			}
		}

		// Inter-sample (oversampled) peak of this input frame. The
		// interpolator's output for input frame f reflects the signal
		// ~groupDelay frames earlier; the sample-peak floor is delayed
		// by the same amount below so the two describe the same instant.
		interpPeak := 0.0
		if l.factor > 1 {
			ob := f * fch
			for k := 0; k < fch; k++ {
				v := math.Abs(float64(l.out32[ob+k]))
				if v > interpPeak {
					interpPeak = v
				}
			}
		}

		// Align the sample-peak floor with the delayed interpolator
		// output. At a bypassed rate groupDelay==0 and the floor is the
		// current frame's sample peak (which is also the true peak).
		var spDelayed float64
		if l.groupDelay > 0 {
			spDelayed = l.spRing[l.spPos]
			l.spRing[l.spPos] = sp
			l.spPos++
			if l.spPos == l.groupDelay {
				l.spPos = 0
			}
		} else {
			spDelayed = sp
		}

		p := interpPeak
		if spDelayed > p {
			p = spDelayed
		}

		// Instantaneous gain to bring this peak to the ceiling.
		c := 1.0
		if p > 0 {
			if cc := l.ceilingLin / p; cc < 1 {
				c = cc
			}
		}

		// One-pole release smoothing: instant attack (min with c),
		// exponential recovery toward unity.
		u := c
		if rel := 1 + (l.uPrev-1)*l.kRel; rel < u {
			u = rel
		}
		l.uPrev = u

		// Sliding minimum of u over the last L frames via monotonic
		// deque (front is the minimum). Values are kept non-decreasing
		// from the front.
		for l.dqSize > 0 {
			back := (l.dqHead + l.dqSize - 1) % len(l.dqVal)
			if l.dqVal[back] >= u {
				l.dqSize--
			} else {
				break
			}
		}
		tail := (l.dqHead + l.dqSize) % len(l.dqVal)
		l.dqVal[tail] = u
		l.dqIdx[tail] = l.pos
		l.dqSize++
		// Expire entries older than the window [pos-L+1, pos].
		minIdx := l.pos - int64(l.l) + 1
		for l.dqSize > 0 && l.dqIdx[l.dqHead] < minIdx {
			l.dqHead = (l.dqHead + 1) % len(l.dqVal)
			l.dqSize--
		}
		m := l.dqVal[l.dqHead]

		// Boxcar average of m over the last L frames (running sum).
		slot := int(l.pos % int64(l.l))
		l.mSum -= l.mHist[slot]
		l.mHist[slot] = m
		l.mSum += m
		// Periodically rebuild the sum from scratch to shed FP drift so
		// a non-limiting stream stays bit-transparent indefinitely.
		if l.pos%limiterSumRecompute == 0 {
			s := 0.0
			for _, v := range l.mHist {
				s += v
			}
			l.mSum = s
		}
		g := l.mSum / float64(l.l)
		l.lastGain = g

		// Emit the delayed input scaled by g; refill the delay slot with
		// the current input frame. delay==L frames, so an impulse at
		// input frame k emerges at output frame k+L.
		dbase := l.delayPos * ch
		for c := 0; c < ch; c++ {
			old := l.delay[dbase+c]
			l.delay[dbase+c] = samples[base+c]
			samples[base+c] = old * g
		}
		l.delayPos++
		if l.delayPos == l.l {
			l.delayPos = 0
		}

		l.pos++
	}
}

// Reset returns the limiter to its just-constructed state: the delay
// line is re-primed with zeros, the control smoother, sliding windows,
// and interpolator history are cleared, and the gain returns to unity.
// Configuration (sample rate, channels, ceiling, lookahead, release) is
// retained. After Reset the limiter produces bit-identical output to a
// freshly constructed one of the same configuration.
func (l *Limiter) Reset() {
	for i := range l.delay {
		l.delay[i] = 0
	}
	l.delayPos = 0
	for i := range l.spRing {
		l.spRing[i] = 0
	}
	l.spPos = 0
	l.uPrev = 1.0
	l.dqHead = 0
	l.dqSize = 0
	for i := range l.mHist {
		l.mHist[i] = 1.0
	}
	l.mSum = float64(l.l)
	l.pos = 0
	l.lastGain = 1.0
	l.tp.Reset()
}

// Latency reports the constant processing latency the limiter adds: the
// lookahead plus the true-peak interpolator's group delay, expressed as
// a duration. Use it as the tail length when flushing a finite render
// (see the type doc) and to compensate timing when the limited output
// must line up with an unprocessed reference.
func (l *Limiter) Latency() time.Duration {
	return mutations.FramesToDuration(int64(l.l), l.sampleRate)
}

// LatencyFrames reports the limiter's latency in input frames — the L
// of the y_t = x_{t-L} * g_t delay line. An impulse presented at input
// frame k appears at output frame k+LatencyFrames().
func (l *Limiter) LatencyFrames() int { return l.l }

// GainReductionDB reports the amount of gain reduction currently being
// applied, as a POSITIVE number of dB (0 means no limiting, larger
// means more attenuation) — the convention level meters and UI gain-
// reduction displays use. It reflects the gain applied to the most
// recently processed frame; drive Process, then read.
func (l *Limiter) GainReductionDB() float64 {
	if l.lastGain >= 1 {
		return 0
	}
	return -mutations.AmplitudeToDecibels(l.lastGain)
}

// SampleRate reports the sample rate the limiter was configured for.
func (l *Limiter) SampleRate() int { return l.sampleRate }

// Channels reports the interleaved channel count the limiter expects.
func (l *Limiter) Channels() int { return l.channels }

// Ceiling reports the resolved true-peak ceiling in dBTP (with the
// zero-value default applied).
func (l *Limiter) Ceiling() float64 { return l.ceilingDB }
