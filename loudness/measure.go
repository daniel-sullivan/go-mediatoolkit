package loudness

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// Measurement is a full loudness/peak report for one buffer of audio,
// produced by Measure or MeasureWithChannels.
//
// Loudness fields (Integrated, MaxMomentary, MaxShortTerm) are LUFS;
// Range is LU (a relative spread, see the package doc's Units
// section); TruePeak/SamplePeak/RMS are dBFS/dBTP; the Channel*Peaks
// slices are LINEAR amplitude (1.0 == full scale), one entry per
// input channel, matching Meter.SamplePeak/TruePeak's convention so
// callers that want dB can convert with mutations.AmplitudeToDecibels
// themselves.
//
// Any loudness field that never saw enough audio to pass its gate —
// silence, or a buffer shorter than one measurement window — reads
// math.Inf(-1) (mirrors Meter's readers; this is not an error
// condition). SamplePeak/TruePeak/RMS are math.Inf(-1) only for true
// digital silence (every sample exactly 0).
type Measurement struct {
	// Integrated is the EBU R128 gated integrated loudness over the
	// whole buffer, in LUFS. math.Inf(-1) if no 400 ms block passed
	// the absolute (-70 LUFS) gate.
	Integrated float64

	// MaxMomentary is the highest momentary (trailing 400 ms) loudness
	// reading seen while feeding the buffer at the standard 100 ms
	// cadence, in LUFS. math.Inf(-1) for input shorter than 400 ms or
	// pure silence.
	MaxMomentary float64

	// MaxShortTerm is the highest short-term (trailing 3 s) loudness
	// reading seen at the same 100 ms cadence, in LUFS. math.Inf(-1)
	// for input shorter than 3 s or pure silence.
	MaxShortTerm float64

	// Range is the EBU Tech 3342 loudness range, in LU.
	Range float64

	// TruePeak is the maximum inter-sample reconstructed peak across
	// every channel, in dBTP. math.Inf(-1) for pure silence.
	TruePeak float64

	// SamplePeak is the maximum sample-domain (no inter-sample
	// reconstruction) peak across every channel, in dBFS.
	// math.Inf(-1) for pure silence.
	SamplePeak float64

	// RMS is the root-mean-square level of the whole programme, in
	// dBFS, pooled across every channel (see the package RMS
	// function's doc for the pooling convention). math.Inf(-1) for
	// pure silence.
	RMS float64

	// ChannelTruePeaks holds the linear true peak for each input
	// channel, indexed the same way as the input's interleaving
	// (len == the Audio's Channels).
	ChannelTruePeaks []float64

	// ChannelSamplePeaks holds the linear sample peak for each input
	// channel, indexed the same way as ChannelTruePeaks.
	ChannelSamplePeaks []float64
}

// Measure computes a full Measurement (integrated/max-momentary/
// max-short-term loudness, loudness range, true/sample peak, and RMS)
// for an already-loaded mutations.Audio buffer, using the default
// BS.1770 channel map for a.Channels (see NewMeter).
//
// It returns ErrBadSampleRate if a.SampleRate <= 0, or ErrBadChannels
// if a.Channels is not in 1..64.
//
// Measure allocates a fresh Meter per call and feeds the whole buffer
// through it — convenient for one-shot offline analysis of a clip, but
// wasteful for a stream you already have a Meter for. For streaming
// audio, or when you want to reuse one Meter across many buffers,
// drive a Meter directly instead.
func Measure(a mutations.Audio) (*Measurement, error) {
	return measure(a, nil)
}

// MeasureWithChannels is Measure with an explicit per-channel BS.1770
// weighting override (see Meter.SetChannel), applied before any audio
// is fed to the meter. A non-nil channels must have exactly
// a.Channels entries, one per interleaved channel in order; a
// mismatched length returns ErrBadConfig (the mismatch is a caller
// configuration error distinct from a.Channels itself being out of
// range, which is still ErrBadChannels). A nil channels is equivalent
// to calling Measure — no override is applied.
//
// Use this to weight a channel as ChannelDualMono (mono-in-stereo
// programme), mark a channel ChannelUnused (e.g. an LFE track), or
// otherwise override the default channel map's assumptions — any
// override changes which channels contribute to Integrated/Range and
// how much, so the resulting Measurement can differ materially from
// Measure's.
func MeasureWithChannels(a mutations.Audio, channels []Channel) (*Measurement, error) {
	return measure(a, channels)
}

// measure is the shared implementation behind Measure and
// MeasureWithChannels: construct a Meter, apply channel overrides (if
// any), feed the buffer in 100 ms frame-aligned chunks while tracking
// momentary/short-term maxima at that same cadence (matching the Tech
// 3341 meter's own reading cadence), then read the whole-programme
// figures.
func measure(a mutations.Audio, channels []Channel) (*Measurement, error) {
	m, err := NewMeter(a.SampleRate, a.Channels, ModeAll)
	if err != nil {
		return nil, err
	}

	if channels != nil {
		if len(channels) != a.Channels {
			return nil, ErrBadConfig
		}
		for i, ch := range channels {
			if err := m.SetChannel(i, ch); err != nil {
				return nil, err
			}
		}
	}

	// 100 ms frame-aligned chunks, matching libebur128's own gating
	// cadence ((sampleRate+5)/10 frames per 100 ms) — this is what
	// makes feeding the meter here and reading Momentary/ShortTerm
	// after every chunk equivalent to the Tech 3341 meter's own
	// reading rate, rather than an arbitrary chunking choice.
	framesPerChunk := (a.SampleRate + 5) / 10
	chunkSamples := framesPerChunk * a.Channels

	maxMomentary := math.Inf(-1)
	maxShortTerm := math.Inf(-1)

	// Accumulate the pooled sum of squares for RMS in the SAME element
	// order the standalone RMS(a.Data) would visit, fused into this loop
	// so the buffer is scanned once. The chunks partition a.Data
	// contiguously in order, so summing each chunk's samples in turn is
	// bit-identical to RMS(a.Data)'s single pass.
	var sumSquares float64

	for start := 0; start < len(a.Data); start += chunkSamples {
		end := start + chunkSamples
		if end > len(a.Data) {
			end = len(a.Data)
		}
		chunk := a.Data[start:end]
		if err := m.AddFrames(chunk); err != nil {
			return nil, err
		}
		for _, s := range chunk {
			sumSquares += s * s
		}

		mom, err := m.Momentary()
		if err != nil {
			return nil, err
		}
		maxMomentary = math.Max(maxMomentary, mom)

		st, err := m.ShortTerm()
		if err != nil {
			return nil, err
		}
		maxShortTerm = math.Max(maxShortTerm, st)
	}

	// RMS from the fused accumulator, bit-identical to RMS(a.Data): an
	// empty buffer yields 0, matching RMS's len==0 guard.
	rms := 0.0
	if len(a.Data) > 0 {
		rms = math.Sqrt(sumSquares / float64(len(a.Data)))
	}

	integrated, err := m.Integrated()
	if err != nil {
		return nil, err
	}
	loudnessRange, err := m.Range()
	if err != nil {
		return nil, err
	}

	samplePeaks := make([]float64, a.Channels)
	truePeaks := make([]float64, a.Channels)
	maxSamplePeak := 0.0
	maxTruePeak := 0.0
	for ch := 0; ch < a.Channels; ch++ {
		sp, err := m.SamplePeak(ch)
		if err != nil {
			return nil, err
		}
		samplePeaks[ch] = sp
		if sp > maxSamplePeak {
			maxSamplePeak = sp
		}

		tp, err := m.TruePeak(ch)
		if err != nil {
			return nil, err
		}
		truePeaks[ch] = tp
		if tp > maxTruePeak {
			maxTruePeak = tp
		}
	}

	return &Measurement{
		Integrated:         integrated,
		MaxMomentary:       maxMomentary,
		MaxShortTerm:       maxShortTerm,
		Range:              loudnessRange,
		TruePeak:           mutations.AmplitudeToDecibels(maxTruePeak),
		SamplePeak:         mutations.AmplitudeToDecibels(maxSamplePeak),
		RMS:                mutations.AmplitudeToDecibels(rms),
		ChannelTruePeaks:   truePeaks,
		ChannelSamplePeaks: samplePeaks,
	}, nil
}
