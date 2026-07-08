// Package loudness measures and normalises perceived audio loudness
// per EBU R128 / ITU-R BS.1770-4, plus supporting sample-domain peak
// and RMS metering.
//
// # Why this exists
//
// go-mediatoolkit has gain plumbing (mutations.Gain, mixer track
// faders) but nothing that *measures* loudness, so there was no way to
// normalise a clip or stream to a broadcast/streaming target. This
// package adds full R128 metering (Meter, momentary/short-term/
// integrated LUFS, loudness range, true peak, sample peak) plus RMS,
// offline measurement and normalisation of mutations.Audio (Measure,
// Normalize), and streaming mutations.Processors that plug into
// timeline.EffectSource and a mixer master-bus hook: Normalizer
// (fixed constant-gain normaliser), Limiter (true-peak limiter),
// Leveller (adaptive gain rider), and Monitor (thread-safe read-only
// loudness meter).
//
// # Units
//
// All measurements are float64 and use one of four conventions,
// chosen to match the units broadcasters and streaming platforms
// actually publish targets in:
//
//   - LUFS ("Loudness Units, relative to Full Scale") — absolute
//     integrated/momentary/short-term loudness, e.g. TargetStreaming
//     = -14 LUFS. Directly comparable across programmes.
//   - LU ("Loudness Units") — a *relative* loudness difference, used
//     for loudness range (Range) and thresholds. 1 LU == 1 dB in
//     magnitude but names a loudness delta rather than an absolute
//     level.
//   - dBTP ("dB True Peak") — inter-sample reconstructed peak level,
//     always ≤ 0 for a signal that never clips; CeilingEBUR128 = -1
//     dBTP is the standard broadcast headroom target.
//   - dBFS ("dB Full Scale") — plain sample-domain peak or RMS level,
//     0 dBFS == full-scale amplitude of 1.0.
//
// Conversion between the linear amplitude domain (used internally by
// RMS and libebur128's peak readers) and dB uses
// mutations.Decibels (dB → linear) and
// mutations.AmplitudeToDecibels (linear → dB) — the same 20*log10
// convention used everywhere else in the toolkit, so a loudness dBTP
// or dBFS figure composes directly with mutations.Gain / ApplyGain.
//
// # References
//
//   - ITU-R BS.1770-4, "Algorithms to measure audio programme loudness
//     and true-peak audio level".
//   - EBU Tech 3341/3342/3343 ("EBU R128"), the broadcast loudness
//     recommendation built on BS.1770.
//   - EBU Tech 3341 v4 (Nov 2023) and Tech 3342 v4 compliance test
//     vectors, used by this package's compliance suite (Phase C).
//   - libebur128 (https://github.com/jiixyj/libebur128, MIT), vendored
//     under loudness/libebur128/ as a cgo parity oracle: the Go port
//     added in later phases is verified bit-exact (documented ULP
//     exceptions only) against this reference implementation rather
//     than trusted by construction. See loudness/libebur128/VERSION for
//     the exact vendored revision.
package loudness

import "math"

// Mode is a bitmask selecting which measurements a Meter computes.
// Its bit values are numerically identical to libebur128's `enum
// mode` in ebur128.h — this is load-bearing, not incidental: Phase B's
// cgo parity oracle passes Mode values straight through to
// ebur128_init's `int mode` parameter, so any divergence here would
// silently miscompare against the wrong C behaviour. Combine bits
// with bitwise OR, e.g. ModeIntegrated|ModeTruePeak.
//
// Every mode implies ModeMomentary (least amount of buffering
// libebur128 will do internally), and higher modes imply the ones
// they are built from — mirroring the C enum's `|` chains — so
// checking `mode&ModeShortTerm == ModeShortTerm` is sufficient to know
// momentary loudness is also available.
type Mode uint32

const (
	// ModeMomentary enables ebur128_loudness_momentary — loudness
	// over the trailing 400 ms window. The base mode; every other
	// mode includes this bit.
	ModeMomentary Mode = 1 << 0

	// ModeShortTerm enables ebur128_loudness_shortterm — loudness
	// over the trailing 3 s window. Implies ModeMomentary.
	ModeShortTerm Mode = (1 << 1) | ModeMomentary

	// ModeIntegrated enables ebur128_loudness_global (gated
	// integrated loudness across the whole measurement) and
	// ebur128_relative_threshold. Implies ModeMomentary.
	ModeIntegrated Mode = (1 << 2) | ModeMomentary

	// ModeLRA enables ebur128_loudness_range (EBU Tech 3342 loudness
	// range, the 10th-95th percentile spread of short-term loudness).
	// Implies ModeShortTerm (LRA is computed from short-term energies).
	ModeLRA Mode = (1 << 3) | ModeShortTerm

	// ModeSamplePeak enables ebur128_sample_peak — the maximum linear
	// absolute sample value seen, no inter-sample reconstruction.
	// Implies ModeMomentary.
	ModeSamplePeak Mode = (1 << 4) | ModeMomentary

	// ModeTruePeak enables ebur128_true_peak — the maximum
	// reconstructed inter-sample peak via oversampled interpolation
	// (BS.1770-4 Annex 2). More expensive than ModeSamplePeak and
	// always ≥ it. Implies ModeMomentary and ModeSamplePeak.
	ModeTruePeak Mode = (1 << 5) | ModeMomentary | ModeSamplePeak

	// ModeHistogram selects libebur128's histogram algorithm for
	// integrated-loudness and loudness-range gating instead of the
	// exact list-based algorithm. Trades a small amount of precision
	// (samples are bucketed) for bounded memory — needed for 24/7
	// streams where ModeIntegrated/ModeLRA's default unbounded history
	// would otherwise grow forever. Does not itself enable any
	// reader; combine with ModeIntegrated and/or ModeLRA.
	ModeHistogram Mode = 1 << 6

	// ModeAll enables every commonly-wanted measurement: integrated
	// loudness, loudness range, and true peak (which subsumes
	// momentary, short-term, and sample peak through the implication
	// chains above). Does not include ModeHistogram — opt into that
	// separately for bounded-memory long-running meters.
	ModeAll = ModeIntegrated | ModeLRA | ModeTruePeak
)

// Channel identifies the loudness-weighting role of one channel in a
// Meter's channel map (see BS.1770-4 §2.1's channel weighting table).
// Its values are numerically identical to libebur128's `enum channel`
// in ebur128.h, for the same parity reason as Mode: Phase B's cgo
// oracle passes Channel values straight through to
// ebur128_set_channel's `int value` parameter.
//
// Named constants mirror the header's own aliasing — e.g. ChannelLeft
// and ChannelMp030 are the same value (1), matching
// EBUR128_LEFT/EBUR128_Mp030 in ebur128.h, because BS.1770's channel
// weighting only cares about ITU speaker position, not the historical
// "left/right" stereo naming.
type Channel int

const (
	// ChannelUnused marks a channel that is excluded from loudness
	// summation entirely (e.g. an LFE channel, which BS.1770 excludes
	// by definition).
	ChannelUnused Channel = 0

	// ChannelLeft is the conventional stereo/5.1 left channel.
	// Weight 1.0. Alias of ChannelMp030 (ITU M+030).
	ChannelLeft Channel = 1
	// ChannelMp030 is the ITU M+030 speaker position (front left at
	// 30°). Same value as ChannelLeft.
	ChannelMp030 Channel = 1

	// ChannelRight is the conventional stereo/5.1 right channel.
	// Weight 1.0. Alias of ChannelMm030 (ITU M-030).
	ChannelRight Channel = 2
	// ChannelMm030 is the ITU M-030 speaker position (front right at
	// -30°). Same value as ChannelRight.
	ChannelMm030 Channel = 2

	// ChannelCenter is the conventional 5.1 center channel. Weight
	// 1.0. Alias of ChannelMp000 (ITU M+000).
	ChannelCenter Channel = 3
	// ChannelMp000 is the ITU M+000 speaker position (dead center).
	// Same value as ChannelCenter.
	ChannelMp000 Channel = 3

	// ChannelLeftSurround is the conventional 5.1 left-surround
	// channel. Weight 1.41 (BS.1770's literal surround boost). Alias
	// of ChannelMp110 (ITU M+110).
	ChannelLeftSurround Channel = 4
	// ChannelMp110 is the ITU M+110 speaker position (left surround
	// at 110°). Same value as ChannelLeftSurround.
	ChannelMp110 Channel = 4

	// ChannelRightSurround is the conventional 5.1 right-surround
	// channel. Weight 1.41. Alias of ChannelMm110 (ITU M-110).
	ChannelRightSurround Channel = 5
	// ChannelMm110 is the ITU M-110 speaker position (right surround
	// at -110°). Same value as ChannelRightSurround.
	ChannelMm110 Channel = 5

	// ChannelDualMono marks a channel that should be counted twice —
	// used when a nominally-mono programme is carried in a stereo
	// container with both channels identical, so the measured
	// loudness matches what a true mono BS.1770 measurement would
	// give. Weight 2.0.
	ChannelDualMono Channel = 6

	// ChannelMpSC is the ITU M+SC speaker position (front left of
	// center).
	ChannelMpSC Channel = 7
	// ChannelMmSC is the ITU M-SC speaker position (front right of
	// center).
	ChannelMmSC Channel = 8
	// ChannelMp060 is the ITU M+060 speaker position.
	ChannelMp060 Channel = 9
	// ChannelMm060 is the ITU M-060 speaker position.
	ChannelMm060 Channel = 10
	// ChannelMp090 is the ITU M+090 speaker position (side left).
	ChannelMp090 Channel = 11
	// ChannelMm090 is the ITU M-090 speaker position (side right).
	ChannelMm090 Channel = 12
	// ChannelMp135 is the ITU M+135 speaker position (rear left).
	ChannelMp135 Channel = 13
	// ChannelMm135 is the ITU M-135 speaker position (rear right).
	ChannelMm135 Channel = 14
	// ChannelMp180 is the ITU M+180 speaker position (rear center).
	ChannelMp180 Channel = 15
	// ChannelUp000 is the ITU U+000 speaker position (upper center
	// front).
	ChannelUp000 Channel = 16
	// ChannelUp030 is the ITU U+030 speaker position.
	ChannelUp030 Channel = 17
	// ChannelUm030 is the ITU U-030 speaker position.
	ChannelUm030 Channel = 18
	// ChannelUp045 is the ITU U+045 speaker position.
	ChannelUp045 Channel = 19
	// ChannelUm045 is the ITU U-045 speaker position.
	ChannelUm045 Channel = 20
	// ChannelUp090 is the ITU U+090 speaker position (upper side
	// left).
	ChannelUp090 Channel = 21
	// ChannelUm090 is the ITU U-090 speaker position (upper side
	// right).
	ChannelUm090 Channel = 22
	// ChannelUp110 is the ITU U+110 speaker position.
	ChannelUp110 Channel = 23
	// ChannelUm110 is the ITU U-110 speaker position.
	ChannelUm110 Channel = 24
	// ChannelUp135 is the ITU U+135 speaker position.
	ChannelUp135 Channel = 25
	// ChannelUm135 is the ITU U-135 speaker position.
	ChannelUm135 Channel = 26
	// ChannelUp180 is the ITU U+180 speaker position (upper rear
	// center).
	ChannelUp180 Channel = 27
	// ChannelTp000 is the ITU T+000 speaker position (top center).
	ChannelTp000 Channel = 28
	// ChannelBp000 is the ITU B+000 speaker position (bottom center
	// front).
	ChannelBp000 Channel = 29
	// ChannelBp045 is the ITU B+045 speaker position.
	ChannelBp045 Channel = 30
	// ChannelBm045 is the ITU B-045 speaker position.
	ChannelBm045 Channel = 31
)

// Loudness targets in LUFS, and the associated true-peak ceiling in
// dBTP, for common broadcast/streaming normalisation presets. Pass
// these directly as NormalizeOptions.Target / .Ceiling (added in a
// later phase). Each is negative because LUFS/dBTP measure downward
// from a 0 dBFS/0 LU full-scale reference — a "louder" target is a
// smaller magnitude, not a larger one.
const (
	// TargetEBUR128 is the EBU R128 broadcast loudness target,
	// -23 LUFS. Source: EBU Tech 3341/3342/3343 ("EBU R128").
	TargetEBUR128 = -23.0

	// TargetATSC is the US broadcast loudness target defined by the
	// CALM Act's reference standard, -24 LUFS. Source: ATSC A/85,
	// "Techniques for Establishing and Maintaining Audio Loudness for
	// Digital Television".
	TargetATSC = -24.0

	// TargetPodcast is the commonly-used podcast/spoken-word target,
	// -16 LUFS. Source: Apple Podcasts' publishing guidelines and the
	// AES streaming loudness recommendation for speech content —
	// louder than music-streaming targets because podcasts are mixed
	// mono/near-mono with little dynamic range to spare.
	TargetPodcast = -16.0

	// TargetStreaming is the loudness target used by most major
	// music-streaming platforms' normalisation (Spotify, YouTube
	// Music, Amazon Music), -14 LUFS. Source: informal industry
	// convergence around this figure rather than a single published
	// standard; treat it as "common streaming reference", not a
	// normative spec value.
	TargetStreaming = -14.0

	// CeilingEBUR128 is the EBU R128 true-peak ceiling, -1 dBTP.
	// Source: EBU R128 / Tech 3343. Used as the default Ceiling in
	// NormalizeOptions when the caller does not override it.
	CeilingEBUR128 = -1.0
)

// maxChannels is the largest channel count a Meter accepts. It
// mirrors libebur128's internal VALIDATE_MAX_CHANNELS bound (64) —
// staying in lockstep matters because the cgo parity oracle rejects
// ebur128_init calls above this count with EBUR128_ERROR_NOMEM-style
// failure, and the Go port needs to fail the same way at the same
// boundary for parity, not just for its own sanity.
const maxChannels = 64

// minSampleRate is the smallest sample rate a Meter accepts. It mirrors
// libebur128's VALIDATE_CHANNELS_AND_SAMPLERATE lower bound (16 Hz):
// below it, samples_in_100ms = (rate+5)/10 rounds toward zero and the
// ring-buffer / block sizing degenerates (a divide-by-zero in the ring
// allocation at rates 1..4, and a rejected VALIDATE at 5..15). Rejecting
// < 16 up front matches the C oracle exactly and, crucially, turns what
// would be a panic in the pure-Go core into a clean ErrBadSampleRate.
const minSampleRate = 16

// resolveCeiling normalises a user-supplied true-peak ceiling in dBTP to
// the value the products actually use. A ceiling of exactly 0 is treated
// as "unset" and becomes the EBU R128 default (CeilingEBUR128, -1 dBTP);
// pass a tiny non-zero value (e.g. 1e-9) for a literal 0 dBTP ceiling. A
// NaN or -Inf ceiling is always rejected with ErrBadConfig. +Inf is
// rejected too, except when allowDisable is set — Normalize uses +Inf as
// its "ceiling disabled" sentinel and passes it through unchanged.
func resolveCeiling(c float64, allowDisable bool) (float64, error) {
	if c == 0 {
		return CeilingEBUR128, nil
	}
	if math.IsNaN(c) {
		return 0, ErrBadConfig
	}
	if math.IsInf(c, 1) {
		if allowDisable {
			return c, nil
		}
		return 0, ErrBadConfig
	}
	if math.IsInf(c, -1) {
		return 0, ErrBadConfig
	}
	return c, nil
}

// RMS returns the linear root-mean-square amplitude of samples,
// pooled across every value in the slice — callers with interleaved
// multi-channel audio get one combined RMS across all channels, not a
// per-channel figure. A full-scale sine wave (peak amplitude 1.0)
// measures ≈0.707 (1/√2) RMS; a full-scale square wave measures 1.0.
// An empty slice returns 0.
//
// The result is linear amplitude, not dBFS — convert with
// mutations.AmplitudeToDecibels(RMS(samples)) for a dB figure. Unlike
// the AES-17 RMS convention used by some hardware meters, this
// applies no +3 dB (√2) offset to align with a full-scale sine's peak
// — 0 dBFS RMS here means a full-scale square wave, not a full-scale
// sine.
func RMS(samples []float64) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSquares float64
	for _, s := range samples {
		sumSquares += s * s
	}
	return math.Sqrt(sumSquares / float64(len(samples)))
}
