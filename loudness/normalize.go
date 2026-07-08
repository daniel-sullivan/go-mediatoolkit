package loudness

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// NormalizeMode selects how Normalize reconciles the two competing
// goals of loudness normalisation: hitting the loudness Target, and
// keeping the true peak under the Ceiling. The two goals conflict
// whenever the constant gain that would reach Target also pushes the
// signal's true peak above the ceiling (a loud-but-peaky master), and
// the mode decides which goal yields.
type NormalizeMode int

const (
	// NormalizeClamp (the zero value) is the transparent mode: it
	// applies a single constant gain and reduces that gain, if
	// necessary, so the true-peak Ceiling is never exceeded — even at
	// the cost of undershooting Target. Nothing but a level change is
	// ever applied, so the waveform's peak-to-loudness relationship
	// (its dynamics) is preserved exactly; the trade is that a peaky
	// programme may land quieter than Target. This is the safe default
	// when fidelity matters more than hitting an exact number.
	NormalizeClamp NormalizeMode = iota

	// NormalizeLimit is the on-target mode: it applies the full gain
	// needed to reach Target, then runs a true-peak Limiter at Ceiling
	// to tame whatever peaks that gain raised above it. The programme
	// reaches Target, but the peaks that would have exceeded the
	// ceiling are dynamically reduced — trading some of the master's
	// peak dynamics for hitting the loudness number. Because the
	// limiter changes the signal, the delivered loudness is whatever
	// the post-limit re-measurement reports (NormalizeResult.Output),
	// which can sit a hair under Target when heavy limiting removes
	// energy; treat Output, not Target, as authoritative.
	NormalizeLimit
)

// NormalizeOptions configures a Normalize call.
type NormalizeOptions struct {
	// Target is the desired integrated loudness in LUFS. It is
	// required and must be strictly negative (LUFS/dBTP measure
	// downward from a 0 dBFS full-scale reference, so every real
	// target is below 0); a zero or positive Target returns
	// ErrBadTarget. Use a preset such as TargetEBUR128 (-23),
	// TargetStreaming (-14), or TargetPodcast (-16).
	Target float64

	// Ceiling is the true-peak ceiling in dBTP the output must respect.
	// The zero value selects CeilingEBUR128 (-1.0 dBTP), the EBU R128
	// broadcast headroom target. Pass math.Inf(1) to disable the
	// ceiling entirely: in NormalizeClamp the gain is then never
	// clamped (Target is always reached, peaks be damned), and in
	// NormalizeLimit no limiter runs (it degenerates to a plain
	// constant gain to Target, exactly like a ceiling-disabled Clamp).
	// A NaN or -Inf ceiling returns ErrBadConfig. An exact 0 selects the
	// CeilingEBUR128 default; pass a tiny non-zero value (e.g. 1e-9) for
	// a literal 0 dBTP ceiling.
	Ceiling float64

	// Mode selects NormalizeClamp (default, transparent constant gain)
	// or NormalizeLimit (full gain to Target then limit). See
	// NormalizeMode.
	Mode NormalizeMode
}

// NormalizeResult reports what Normalize did and the before/after
// loudness picture. Input is the measurement of the audio as received;
// Output is the measurement after normalisation (post-gain, and
// post-limit in NormalizeLimit mode) and is the authoritative record
// of the delivered loudness and peak.
type NormalizeResult struct {
	// GainDB is the constant gain, in dB, that Normalize applied to the
	// buffer before any limiting. In NormalizeClamp this is the
	// (possibly ceiling-reduced) gain actually used; in NormalizeLimit
	// it is the full Target-reaching gain applied ahead of the limiter.
	GainDB float64

	// Clamped is true only in NormalizeClamp, and only when the
	// true-peak ceiling term — not the loudness target term — decided
	// the gain (i.e. the gain was reduced below what Target wanted so
	// the ceiling would not be exceeded, so Output.Integrated will sit
	// below Target). It is always false in NormalizeLimit.
	Clamped bool

	// Limited is true only in NormalizeLimit, and only when the limiter
	// actually attenuated the signal (the full-gain true peak exceeded
	// the ceiling). It is always false in NormalizeClamp and false in
	// NormalizeLimit when the ceiling is disabled or the gained signal
	// already sat under it.
	Limited bool

	// Input is the loudness/peak measurement of the audio as passed in,
	// before Normalize modified it.
	Input Measurement

	// Output is the loudness/peak measurement after normalisation. In
	// NormalizeLimit this is measured after the limiter ran, so it is
	// the true delivered loudness (which may differ slightly from
	// Target under heavy limiting).
	Output Measurement
}

// Normalize adjusts the loudness of a in place — mutating a.Data
// directly, length-preserving and without time-shifting it — so the
// programme meets opts.Target subject to the opts.Ceiling true-peak
// limit, then returns a full before/after report.
//
// # Algorithm
//
// Normalize first measures the whole buffer (via Measure) to obtain
// its integrated loudness and true peak. Silent or unmeasurable input
// (integrated loudness of -Inf — digital silence, or too short/quiet
// for any gated block to pass the -70 LUFS absolute gate) returns
// ErrSilentInput, because there is no finite gain that normalises
// silence to a target.
//
// From the measured integrated loudness it computes the constant gain
// Target - Integrated that would place the programme exactly on
// target, then reconciles that against the ceiling per opts.Mode:
//
//   - NormalizeClamp applies min(Target - Integrated,
//     Ceiling - InputTruePeak) — the smaller (more negative, or less
//     positive) of the loudness-target gain and the largest gain that
//     keeps the true peak at the ceiling. When the ceiling term wins,
//     GainDB is that reduced value, Clamped is true, and the delivered
//     loudness (Output.Integrated) lands below Target. A single
//     constant gain is applied and nothing else touches the waveform,
//     so its dynamics are preserved exactly.
//
//   - NormalizeLimit applies the full Target - Integrated gain, then —
//     if the ceiling is enabled — runs a true-peak Limiter at Ceiling
//     over the gained data. The programme reaches Target; peaks that
//     the gain raised above the ceiling are dynamically limited. The
//     limiter has latency (its lookahead plus interpolator group
//     delay); Normalize handles this internally by flushing
//     LatencyFrames of tail through the limiter and dropping the
//     leading LatencyFrames of its output, so the written-back result
//     stays exactly aligned with — and the same length as — the input.
//     Because limiting alters the signal, the delivered loudness is
//     re-measured into Output and is authoritative; Output.Integrated
//     may sit a little under Target when limiting is heavy.
//
// When the ceiling is disabled (opts.Ceiling == math.Inf(1)) both
// modes reduce to a plain constant gain to Target (no clamping, no
// limiter).
//
// # Which mode to choose
//
// Prefer NormalizeClamp when the source's dynamics must be preserved
// bit-for-bit apart from a level change, and it is acceptable for a
// peaky programme to land below Target. Prefer NormalizeLimit when
// hitting the loudness number matters more than preserving every
// inter-sample peak — accepting that the limiter trades a small amount
// of peak dynamics to get there. In either case read the returned
// Output for the loudness/peak actually delivered.
func Normalize(a mutations.Audio, opts NormalizeOptions) (*NormalizeResult, error) {
	if a.SampleRate < minSampleRate {
		return nil, ErrBadSampleRate
	}
	if a.Channels < 1 || a.Channels > maxChannels {
		return nil, ErrBadChannels
	}
	// Target is required and must be strictly negative. The negated
	// form also rejects NaN (NaN < 0 is false) and zero.
	if !(opts.Target < 0) {
		return nil, ErrBadTarget
	}

	ceiling, err := resolveCeiling(opts.Ceiling, true)
	if err != nil {
		return nil, err
	}
	ceilingEnabled := !math.IsInf(ceiling, 1)

	input, err := Measure(a)
	if err != nil {
		return nil, err
	}
	if math.IsInf(input.Integrated, -1) {
		return nil, ErrSilentInput
	}

	res := &NormalizeResult{Input: *input}

	gainDB := opts.Target - input.Integrated
	switch opts.Mode {
	case NormalizeLimit:
		res.GainDB = gainDB
		mutations.ApplyGain(a.Data, mutations.Decibels(gainDB))

		if ceilingEnabled {
			// A constant gain shifts the true peak by exactly gainDB, so
			// the gained signal's true peak is input.TruePeak + gainDB.
			// The limiter attenuates iff that exceeds the ceiling — an
			// exact, cheap predicate that avoids a second pre-limit
			// measurement.
			res.Limited = input.TruePeak+gainDB > ceiling
			normalizeLimit(a, ceiling)
		}

	default: // NormalizeClamp
		if ceilingEnabled {
			if ceilTerm := ceiling - input.TruePeak; ceilTerm < gainDB {
				gainDB = ceilTerm
				res.Clamped = true
			}
		}
		res.GainDB = gainDB
		mutations.ApplyGain(a.Data, mutations.Decibels(gainDB))
	}

	output, err := Measure(a)
	if err != nil {
		return nil, err
	}
	res.Output = *output
	return res, nil
}

// normalizeLimit runs a true-peak Limiter at ceiling (dBTP) over a.Data
// in place, preserving both its length and its time alignment. The
// limiter delays its output by LatencyFrames; to undo that, the signal
// is processed together with a LatencyFrames tail of zeros and the
// leading LatencyFrames of primed-zero output are dropped so output
// frame k corresponds to input frame k. The audio's format is already
// validated by the caller, so NewLimiter cannot fail here for a finite
// ceiling.
func normalizeLimit(a mutations.Audio, ceiling float64) {
	lim, err := NewLimiter(LimiterConfig{
		SampleRate: a.SampleRate,
		Channels:   a.Channels,
		Ceiling:    ceiling,
	})
	if err != nil {
		// The caller has validated sample rate, channels and ceiling
		// (finite, non-NaN), which are the only things NewLimiter
		// rejects — so this is unreachable. Fail safe by leaving the
		// gained-but-unlimited data in place rather than panicking.
		return
	}

	ch := a.Channels
	lf := lim.LatencyFrames()
	n := len(a.Data)

	if n < lf*ch {
		// Clip shorter than the delay line: the in-place scheme below
		// needs at least one full delay line of signal, so fall back to a
		// separate extended buffer. Process input followed by lf frames of
		// silence in one pass; ext[lf*ch:] then holds the limited input,
		// time-aligned, and ext[:lf*ch] is the primed-zero fill we drop.
		ext := make([]float64, n+lf*ch)
		copy(ext, a.Data)
		lim.Process(ext)
		copy(a.Data, ext[lf*ch:lf*ch+n])
		return
	}

	// Limit the whole clip in place. The limiter reads x_{t-L} from its
	// internal delay line, so overwriting a.Data as it goes is safe: after
	// this pass a.Data holds output frames [-L .. n-L), i.e. the leading
	// L frames are the primed-zero fill and a.Data[lf*ch:] is the limited
	// input frames 0 .. n-L-1, time-shifted late by L.
	lim.Process(a.Data)
	// Flush L frames of silence to push the final L frames of limited
	// output (input frames n-L .. n-1) out of the delay line.
	flush := make([]float64, lf*ch)
	lim.Process(flush)
	// Drop the primed lead by shifting the valid tail of a.Data forward by
	// L frames (a forward overlapping copy is memmove-safe in Go), then
	// append the flushed final L frames, reconstructing input frames
	// 0 .. n-1 exactly time-aligned.
	copy(a.Data, a.Data[lf*ch:])
	copy(a.Data[n-lf*ch:], flush)
}
