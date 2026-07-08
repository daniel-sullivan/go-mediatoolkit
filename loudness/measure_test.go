package loudness

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// measureTestDualToneStereo returns an interleaved stereo buffer where
// the left channel carries a sine at leftAmp and the right channel an
// independent sine (same frequency, different amplitude) at rightAmp
// — used to exercise per-channel peak reporting.
func measureTestDualToneStereo(leftAmp, rightAmp, freqHz float64, rate, frames int) []float64 {
	data := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		phase := 2 * math.Pi * freqHz * float64(n) / float64(rate)
		data[n*2+0] = leftAmp * math.Sin(phase)
		data[n*2+1] = rightAmp * math.Sin(phase)
	}
	return data
}

// ── validation ──────────────────────────────────────────────────────────

func TestMeasureValidation(t *testing.T) {
	tests := []struct {
		name     string
		rate     int
		channels int
		wantErr  error
	}{
		{"zero rate", 0, 2, ErrBadSampleRate},
		{"negative rate", -1, 2, ErrBadSampleRate},
		{"zero channels", 48000, 0, ErrBadChannels},
		{"negative channels", 48000, -2, ErrBadChannels},
		{"too many channels", 48000, 65, ErrBadChannels},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := mutations.Audio{
				Data:       make([]float64, 100),
				SampleRate: tc.rate,
				Channels:   tc.channels,
			}
			m, err := Measure(a)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, m)

			m, err = MeasureWithChannels(a, nil)
			require.ErrorIs(t, err, tc.wantErr)
			assert.Nil(t, m)
		})
	}
}

func TestMeasureWithChannelsLengthMismatch(t *testing.T) {
	a := mutations.Audio{
		Data:       testSine(0.5, 440, 48000, 0, 4800, 2),
		SampleRate: 48000,
		Channels:   2,
	}

	// Too few entries.
	m, err := MeasureWithChannels(a, []Channel{ChannelLeft})
	assert.ErrorIs(t, err, ErrBadConfig)
	assert.Nil(t, m)

	// Too many entries.
	m, err = MeasureWithChannels(a, []Channel{ChannelLeft, ChannelRight, ChannelCenter})
	assert.ErrorIs(t, err, ErrBadConfig)
	assert.Nil(t, m)

	// Exactly matching is fine.
	m, err = MeasureWithChannels(a, []Channel{ChannelLeft, ChannelRight})
	require.NoError(t, err)
	require.NotNil(t, m)

	// nil is equivalent to Measure (no override).
	m, err = MeasureWithChannels(a, nil)
	require.NoError(t, err)
	require.NotNil(t, m)
}

// ── integrated loudness on a known calibration tone ─────────────────────

// TestMeasureStereoSineIntegrated exercises the classic BS.1770
// calibration fact: a 997 Hz sine at the same peak amplitude on both
// (equally-weighted) channels of a stereo signal reads an integrated
// loudness in LUFS numerically equal to its peak level in dBFS — the
// -0.691 LUFS constant and the K-weighting filter's response at 997 Hz
// are designed to cancel for exactly this calibration case, and
// summing two equal-weight channels contributes the same +3.01 dB
// that a mono channel's mean-square-vs-peak factor otherwise costs.
func TestMeasureStereoSineIntegrated(t *testing.T) {
	const (
		rate      = 48000
		freq      = 997.0
		targetDB  = -23.0
		seconds   = 5
		tolerance = 0.1
	)
	amp := mutations.Decibels(targetDB)
	a := mutations.Audio{
		Data:       testSine(amp, freq, rate, 0, rate*seconds, 2),
		SampleRate: rate,
		Channels:   2,
	}

	m, err := Measure(a)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.InDelta(t, targetDB, m.Integrated, tolerance,
		"stereo 997 Hz sine at %.1f dBFS peak should read ~%.1f LUFS", targetDB, targetDB)
}

// ── RMS ──────────────────────────────────────────────────────────────────

func TestMeasureRMSFullScaleSine(t *testing.T) {
	const (
		rate   = 48000
		freq   = 1000.0 // divides rate evenly: whole cycles, no truncation error
		frames = rate   // 1 s == 48 whole cycles at 1 kHz
	)
	a := mutations.Audio{
		Data:       testSine(1.0, freq, rate, 0, frames, 1),
		SampleRate: rate,
		Channels:   1,
	}

	m, err := Measure(a)
	require.NoError(t, err)

	assert.InDelta(t, -3.01, m.RMS, 0.01,
		"full-scale sine RMS should read ~-3.01 dBFS")
}

// ── momentary / short-term maxima track integrated for a steady tone ───

func TestMeasureMaxMomentaryShortTermTrackSteadyTone(t *testing.T) {
	const (
		rate     = 48000
		freq     = 997.0
		targetDB = -18.0
		seconds  = 6
	)
	amp := mutations.Decibels(targetDB)
	a := mutations.Audio{
		Data:       testSine(amp, freq, rate, 0, rate*seconds, 2),
		SampleRate: rate,
		Channels:   2,
	}

	m, err := Measure(a)
	require.NoError(t, err)

	require.False(t, math.IsInf(m.MaxMomentary, 0), "MaxMomentary should be finite for a 6 s tone")
	require.False(t, math.IsInf(m.MaxShortTerm, 0), "MaxShortTerm should be finite for a 6 s tone")

	assert.GreaterOrEqual(t, m.MaxMomentary, m.Integrated-0.05,
		"MaxMomentary should not read below Integrated for a steady tone")
	assert.GreaterOrEqual(t, m.MaxShortTerm, m.Integrated-0.05,
		"MaxShortTerm should not read below Integrated for a steady tone")

	assert.InDelta(t, m.Integrated, m.MaxMomentary, 0.2,
		"MaxMomentary should closely track Integrated for a steady tone")
	assert.InDelta(t, m.Integrated, m.MaxShortTerm, 0.2,
		"MaxShortTerm should closely track Integrated for a steady tone")
}

// ── per-channel peaks ────────────────────────────────────────────────────

func TestMeasureChannelPeaks(t *testing.T) {
	const (
		rate     = 48000
		freq     = 100.0 // dense sampling per cycle -> sample peak ~= true peak
		frames   = rate
		loudAmp  = 0.8
		quietAmp = 0.05
	)
	a := mutations.Audio{
		Data:       measureTestDualToneStereo(loudAmp, quietAmp, freq, rate, frames),
		SampleRate: rate,
		Channels:   2,
	}

	m, err := Measure(a)
	require.NoError(t, err)

	require.Len(t, m.ChannelSamplePeaks, 2)
	require.Len(t, m.ChannelTruePeaks, 2)

	assert.InDelta(t, loudAmp, m.ChannelSamplePeaks[0], 0.01, "left sample peak")
	assert.InDelta(t, quietAmp, m.ChannelSamplePeaks[1], 0.01, "right sample peak")

	assert.InDelta(t, loudAmp, m.ChannelTruePeaks[0], 0.02, "left true peak ~= sample peak at this density")
	assert.InDelta(t, quietAmp, m.ChannelTruePeaks[1], 0.02, "right true peak ~= sample peak at this density")

	// Overall figures report the loud channel's peak.
	assert.InDelta(t, mutations.AmplitudeToDecibels(loudAmp), m.SamplePeak, 0.1)
	assert.InDelta(t, mutations.AmplitudeToDecibels(loudAmp), m.TruePeak, 0.15)
}

// ── channel weighting override is visible in Integrated ─────────────────

// TestMeasureWithChannelsWeightingVisible proves overriding a channel's
// BS.1770 role changes the measured loudness by exactly the weight
// ratio: a 4-channel buffer with an identical signal on every channel
// defaults to L,R,Ls,Rs (weights 1,1,1.41,1.41 — total 4.82); telling
// MeasureWithChannels that channels 2/3 are also plain Left/Right
// (weight 1 each, total 4) must lower Integrated by exactly
// 10*log10(4/4.82) LU, since the underlying K-weighted per-channel
// energies are identical by construction and only the weights differ.
func TestMeasureWithChannelsWeightingVisible(t *testing.T) {
	const (
		rate    = 48000
		freq    = 440.0
		seconds = 3
	)
	data := testSine(0.3, freq, rate, 0, rate*seconds, 4)
	a := mutations.Audio{Data: data, SampleRate: rate, Channels: 4}

	def, err := Measure(a)
	require.NoError(t, err)

	override, err := MeasureWithChannels(a, []Channel{ChannelLeft, ChannelRight, ChannelLeft, ChannelRight})
	require.NoError(t, err)

	wantDelta := 10 * math.Log10(4.0/4.82)
	assert.InDelta(t, wantDelta, override.Integrated-def.Integrated, 1e-6,
		"re-weighting Ls/Rs to plain L/R should shift Integrated by exactly 10*log10(4/4.82) LU")
}

// ── silence ──────────────────────────────────────────────────────────────

func TestMeasureSilence(t *testing.T) {
	a := mutations.Audio{
		Data:       make([]float64, 48000*2), // 1 s stereo silence
		SampleRate: 48000,
		Channels:   2,
	}

	m, err := Measure(a)
	require.NoError(t, err)
	require.NotNil(t, m)

	assert.True(t, math.IsInf(m.Integrated, -1), "Integrated should be -Inf for silence")
	assert.True(t, math.IsInf(m.MaxMomentary, -1), "MaxMomentary should be -Inf for silence")
	assert.True(t, math.IsInf(m.MaxShortTerm, -1), "MaxShortTerm should be -Inf for silence")
	assert.True(t, math.IsInf(m.TruePeak, -1), "TruePeak should be -Inf for silence")
	assert.True(t, math.IsInf(m.SamplePeak, -1), "SamplePeak should be -Inf for silence")
	assert.True(t, math.IsInf(m.RMS, -1), "RMS should be -Inf for silence")
	assert.Equal(t, 0.0, m.Range, "Range should be 0 before enough short-term blocks exist")

	require.Len(t, m.ChannelSamplePeaks, 2)
	require.Len(t, m.ChannelTruePeaks, 2)
	for ch := 0; ch < 2; ch++ {
		assert.Equal(t, 0.0, m.ChannelSamplePeaks[ch])
		assert.Equal(t, 0.0, m.ChannelTruePeaks[ch])
	}
}

func TestMeasureEmptyBuffer(t *testing.T) {
	a := mutations.Audio{SampleRate: 48000, Channels: 2}
	m, err := Measure(a)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.True(t, math.IsInf(m.Integrated, -1))
}
