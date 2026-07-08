package loudness

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── constructor validation ──────────────────────────────────────────────

func TestNewMeterValidation(t *testing.T) {
	tests := []struct {
		name     string
		rate     int
		channels int
		mode     Mode
		wantErr  error
	}{
		{"ok stereo integrated", 48000, 2, ModeIntegrated, nil},
		{"ok mono all", 44100, 1, ModeAll, nil},
		{"ok 64ch", 48000, 64, ModeMomentary, nil},
		{"ok rate 16 (min)", 16, 2, ModeIntegrated, nil},
		{"zero rate", 0, 2, ModeIntegrated, ErrBadSampleRate},
		{"negative rate", -48000, 2, ModeIntegrated, ErrBadSampleRate},
		{"rate 1 (divide-by-zero guard)", 1, 2, ModeIntegrated, ErrBadSampleRate},
		{"rate 4", 4, 2, ModeIntegrated, ErrBadSampleRate},
		{"rate 15 (just below min)", 15, 2, ModeIntegrated, ErrBadSampleRate},
		{"zero channels", 48000, 0, ModeIntegrated, ErrBadChannels},
		{"negative channels", 48000, -1, ModeIntegrated, ErrBadChannels},
		{"65 channels", 48000, 65, ModeIntegrated, ErrBadChannels},
		{"histogram-only mode", 48000, 2, ModeHistogram, ErrBadConfig},
		{"zero mode", 48000, 2, 0, ErrBadConfig},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := NewMeter(tc.rate, tc.channels, tc.mode)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				assert.Nil(t, m)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, m)
			assert.Equal(t, tc.rate, m.SampleRate())
			assert.Equal(t, tc.channels, m.Channels())
			assert.Equal(t, tc.mode, m.Mode())
		})
	}
}

// TestLowSampleRateRejectedNoPanic pins the libebur128 lower sample-rate
// bound (16 Hz) across EVERY public constructor path that reaches the r128
// core or the true-peak interpolator. Below 16 Hz the C oracle's VALIDATE
// rejects the rate, and the pure-Go ring allocation would divide by a
// zero-length 100 ms block at rates 1..4 — so each path must return
// ErrBadSampleRate cleanly rather than panic. Rate 16 (the minimum) must
// construct successfully.
func TestLowSampleRateRejectedNoPanic(t *testing.T) {
	badRates := []int{0, 1, 3, 4, 15}
	for _, rate := range badRates {
		t.Run(fmt.Sprintf("rate_%d", rate), func(t *testing.T) {
			_, err := NewMeter(rate, 2, ModeAll)
			require.ErrorIs(t, err, ErrBadSampleRate, "NewMeter")

			_, err = NewMonitor(rate, 2, ModeAll)
			require.ErrorIs(t, err, ErrBadSampleRate, "NewMonitor")

			_, err = NewLimiter(LimiterConfig{SampleRate: rate, Channels: 2})
			require.ErrorIs(t, err, ErrBadSampleRate, "NewLimiter")

			_, err = NewLeveller(LevellerConfig{SampleRate: rate, Channels: 2})
			require.ErrorIs(t, err, ErrBadSampleRate, "NewLeveller")

			_, err = Measure(mutations.Audio{SampleRate: rate, Channels: 2, Data: make([]float64, 64)})
			require.ErrorIs(t, err, ErrBadSampleRate, "Measure")

			_, err = Normalize(mutations.Audio{SampleRate: rate, Channels: 2, Data: make([]float64, 64)}, NormalizeOptions{Target: -23})
			require.ErrorIs(t, err, ErrBadSampleRate, "Normalize")
		})
	}

	// Rate 16 (the minimum) constructs on every path without error.
	if _, err := NewMeter(16, 2, ModeAll); err != nil {
		t.Errorf("NewMeter(16): unexpected error %v", err)
	}
	if _, err := NewMonitor(16, 2, ModeAll); err != nil {
		t.Errorf("NewMonitor(16): unexpected error %v", err)
	}
	if _, err := NewLimiter(LimiterConfig{SampleRate: 16, Channels: 2}); err != nil {
		t.Errorf("NewLimiter(16): unexpected error %v", err)
	}
	if _, err := NewLeveller(LevellerConfig{SampleRate: 16, Channels: 2}); err != nil {
		t.Errorf("NewLeveller(16): unexpected error %v", err)
	}
}

// ── mode-gating errors on readers ───────────────────────────────────────

func TestReaderModeGating(t *testing.T) {
	// A momentary-only meter: everything above momentary is unavailable.
	m, err := NewMeter(48000, 2, ModeMomentary)
	require.NoError(t, err)

	_, err = m.ShortTerm()
	assert.ErrorIs(t, err, ErrInvalidMode)
	_, err = m.Integrated()
	assert.ErrorIs(t, err, ErrInvalidMode)
	_, err = m.Range()
	assert.ErrorIs(t, err, ErrInvalidMode)
	_, err = m.RelativeThreshold()
	assert.ErrorIs(t, err, ErrInvalidMode)
	_, err = m.SamplePeak(0)
	assert.ErrorIs(t, err, ErrInvalidMode)
	_, err = m.TruePeak(0)
	assert.ErrorIs(t, err, ErrInvalidMode)

	// Momentary itself is always available (returns -Inf before data).
	mom, err := m.Momentary()
	require.NoError(t, err)
	assert.True(t, math.IsInf(mom, -1))
}

func TestRangeNeedsLRA(t *testing.T) {
	// ModeIntegrated has short-term (via momentary chain) but not LRA.
	m, err := NewMeter(48000, 2, ModeIntegrated)
	require.NoError(t, err)
	_, err = m.Range()
	assert.ErrorIs(t, err, ErrInvalidMode)
}

func TestPeakModeGating(t *testing.T) {
	// Sample-peak but not true-peak.
	m, err := NewMeter(48000, 2, ModeSamplePeak)
	require.NoError(t, err)
	_, err = m.SamplePeak(0)
	assert.NoError(t, err)
	_, err = m.TruePeak(0)
	assert.ErrorIs(t, err, ErrInvalidMode)
}

// ── channel-index errors ────────────────────────────────────────────────

func TestPeakChannelIndex(t *testing.T) {
	m, err := NewMeter(48000, 2, ModeTruePeak)
	require.NoError(t, err)
	for _, ch := range []int{-1, 2, 100} {
		_, err := m.SamplePeak(ch)
		assert.ErrorIs(t, err, ErrBadChannelIndex, "SamplePeak(%d)", ch)
		_, err = m.TruePeak(ch)
		assert.ErrorIs(t, err, ErrBadChannelIndex, "TruePeak(%d)", ch)
	}
	// Valid channels succeed.
	_, err = m.SamplePeak(0)
	assert.NoError(t, err)
	_, err = m.SamplePeak(1)
	assert.NoError(t, err)
}

func TestSetChannelValidation(t *testing.T) {
	m, err := NewMeter(48000, 2, ModeIntegrated)
	require.NoError(t, err)

	assert.NoError(t, m.SetChannel(0, ChannelLeft))
	assert.NoError(t, m.SetChannel(1, ChannelRight))
	assert.ErrorIs(t, m.SetChannel(2, ChannelLeft), ErrBadChannelIndex)
	assert.ErrorIs(t, m.SetChannel(-1, ChannelLeft), ErrBadChannelIndex)
	// Dual-mono is only valid on channel 0 of a mono meter.
	assert.ErrorIs(t, m.SetChannel(0, ChannelDualMono), ErrBadChannelIndex)

	mono, err := NewMeter(48000, 1, ModeIntegrated)
	require.NoError(t, err)
	assert.NoError(t, mono.SetChannel(0, ChannelDualMono))
}

// ── unaligned input ─────────────────────────────────────────────────────

func TestAddFramesAlignment(t *testing.T) {
	m, err := NewMeter(48000, 2, ModeIntegrated)
	require.NoError(t, err)

	assert.ErrorIs(t, m.AddFrames([]float64{1, 2, 3}), ErrUnalignedSamples)
	assert.NoError(t, m.AddFrames([]float64{1, 2, 3, 4}))
	assert.NoError(t, m.AddFrames(nil))         // empty is a no-op
	assert.NoError(t, m.AddFrames([]float64{})) // empty is a no-op
}

// ── insufficient data → -Inf ────────────────────────────────────────────

func TestInsufficientDataReturnsNegInf(t *testing.T) {
	m, err := NewMeter(48000, 2, ModeAll)
	require.NoError(t, err)

	// Feed less than one 400 ms block.
	require.NoError(t, m.AddFrames(make([]float64, 2*100)))

	mom, err := m.Momentary()
	require.NoError(t, err)
	assert.True(t, math.IsInf(mom, -1), "momentary should be -Inf")

	integ, err := m.Integrated()
	require.NoError(t, err)
	assert.True(t, math.IsInf(integ, -1), "integrated should be -Inf")

	// LRA and relative threshold have their own empty sentinels.
	lra, err := m.Range()
	require.NoError(t, err)
	assert.Equal(t, 0.0, lra)

	rel, err := m.RelativeThreshold()
	require.NoError(t, err)
	assert.Equal(t, -70.0, rel)
}

// ── SetChannel changes the measured loudness ────────────────────────────

// TestSetChannelDualMonoShiftsIntegrated proves an override is not cosmetic:
// counting a mono channel twice (ChannelDualMono, weight 2.0) raises the
// integrated loudness by exactly 10*log10(2) ≈ 3.01 LU versus the default
// single-weight mono channel, because the block energy doubles.
func TestSetChannelDualMonoShiftsIntegrated(t *testing.T) {
	const (
		rate   = 48000
		frames = rate // 1 s, enough for gated blocks
	)
	sig := make([]float64, frames)
	for n := range sig {
		sig[n] = 0.5 * math.Sin(2*math.Pi*997*float64(n)/rate)
	}

	base, err := NewMeter(rate, 1, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, base.AddFrames(sig))
	baseI, err := base.Integrated()
	require.NoError(t, err)
	require.False(t, math.IsInf(baseI, 0))

	dual, err := NewMeter(rate, 1, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, dual.SetChannel(0, ChannelDualMono))
	require.NoError(t, dual.AddFrames(sig))
	dualI, err := dual.Integrated()
	require.NoError(t, err)

	assert.InDelta(t, 10*math.Log10(2), dualI-baseI, 1e-9,
		"dual-mono should raise integrated loudness by 10*log10(2) LU")
}

// TestSetChannelUnusedExcludesEnergy shows marking a channel UNUSED drops it
// from the loudness sum entirely: a stereo signal with a loud right channel
// measures quieter once the right channel is excluded.
func TestSetChannelUnusedExcludesEnergy(t *testing.T) {
	const (
		rate   = 48000
		frames = rate
	)
	sig := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		v := 0.5 * math.Sin(2*math.Pi*997*float64(n)/rate)
		sig[n*2+0] = v
		sig[n*2+1] = v
	}

	both, err := NewMeter(rate, 2, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, both.AddFrames(sig))
	bothI, err := both.Integrated()
	require.NoError(t, err)

	left, err := NewMeter(rate, 2, ModeIntegrated)
	require.NoError(t, err)
	require.NoError(t, left.SetChannel(1, ChannelUnused))
	require.NoError(t, left.AddFrames(sig))
	leftI, err := left.Integrated()
	require.NoError(t, err)

	// Dropping one of two equal channels halves the energy: -10*log10(2) LU.
	assert.InDelta(t, -10*math.Log10(2), leftI-bothI, 1e-9,
		"excluding an equal channel should lower loudness by 10*log10(2) LU")
}

// ── SetMaxWindow / SetMaxHistory validation ─────────────────────────────

func TestSetMaxWindowValidation(t *testing.T) {
	m, err := NewMeter(48000, 1, ModeIntegrated)
	require.NoError(t, err)

	assert.ErrorIs(t, m.SetMaxWindow(0), ErrBadWindow)
	assert.ErrorIs(t, m.SetMaxWindow(-time.Second), ErrBadWindow)
	// A widen within the M-mode min is fine (returns nil; may be a no-op if
	// clamped to the current window).
	assert.NoError(t, m.SetMaxWindow(500*time.Millisecond))
}

func TestSetMaxHistoryValidation(t *testing.T) {
	m, err := NewMeter(48000, 1, ModeIntegrated)
	require.NoError(t, err)

	assert.ErrorIs(t, m.SetMaxHistory(0), ErrBadWindow)
	assert.ErrorIs(t, m.SetMaxHistory(-time.Second), ErrBadWindow)
	assert.NoError(t, m.SetMaxHistory(10*time.Second))
}

func TestLoudnessWindowValidation(t *testing.T) {
	m, err := NewMeter(48000, 1, ModeShortTerm)
	require.NoError(t, err)
	require.NoError(t, m.AddFrames(make([]float64, 48000))) // 1 s

	_, err = m.LoudnessWindow(0)
	assert.ErrorIs(t, err, ErrBadWindow)
	_, err = m.LoudnessWindow(-time.Second)
	assert.ErrorIs(t, err, ErrBadWindow)
	// Larger than the configured (3 s) window → ErrInvalidMode.
	_, err = m.LoudnessWindow(5 * time.Second)
	assert.ErrorIs(t, err, ErrInvalidMode)
	// Within the window → ok (silence → -Inf, not an error).
	v, err := m.LoudnessWindow(400 * time.Millisecond)
	assert.NoError(t, err)
	assert.True(t, math.IsInf(v, -1))
}

// ── Reset ───────────────────────────────────────────────────────────────

// TestResetRestoresFreshState proves Reset returns identical readings to a
// brand-new meter of the same configuration: measure a clip, Reset, measure
// again, and the second reading must match a fresh meter's.
func TestResetRestoresFreshState(t *testing.T) {
	const (
		rate   = 48000
		frames = rate
	)
	sig := make([]float64, frames*2)
	for n := 0; n < frames; n++ {
		v := 0.4 * math.Sin(2*math.Pi*440*float64(n)/rate)
		sig[n*2+0] = v
		sig[n*2+1] = 0.6 * v
	}

	m, err := NewMeter(rate, 2, ModeAll)
	require.NoError(t, err)
	require.NoError(t, m.AddFrames(sig))
	firstI, _ := m.Integrated()
	firstTP, _ := m.TruePeak(0)

	m.Reset()

	// After reset, before any new data, readings are the fresh sentinels.
	integ, _ := m.Integrated()
	assert.True(t, math.IsInf(integ, -1), "integrated after reset should be -Inf")
	sp, _ := m.SamplePeak(0)
	assert.Equal(t, 0.0, sp, "sample peak after reset should be 0")

	// Re-measuring the same clip reproduces the original readings.
	require.NoError(t, m.AddFrames(sig))
	secondI, _ := m.Integrated()
	secondTP, _ := m.TruePeak(0)
	assert.Equal(t, firstI, secondI, "integrated must match pre-reset run")
	assert.Equal(t, firstTP, secondTP, "true peak must match pre-reset run")

	fresh, err := NewMeter(rate, 2, ModeAll)
	require.NoError(t, err)
	require.NoError(t, fresh.AddFrames(sig))
	freshI, _ := fresh.Integrated()
	assert.Equal(t, freshI, secondI, "reset meter must match a fresh one")
}

// ── 64-channel bound ────────────────────────────────────────────────────

func TestMaxChannelMeter(t *testing.T) {
	m, err := NewMeter(48000, 64, ModeIntegrated)
	require.NoError(t, err)
	// A full 64-channel frame must be accepted and measured.
	frame := make([]float64, 64*48000) // 1 s
	for i := range frame {
		frame[i] = 0.1
	}
	require.NoError(t, m.AddFrames(frame))
	_, err = m.Integrated()
	assert.NoError(t, err)
}
