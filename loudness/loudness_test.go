package loudness

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ── RMS ──────────────────────────────────────────────────────────────

func TestRMSEmpty(t *testing.T) {
	assert.Equal(t, 0.0, RMS(nil))
	assert.Equal(t, 0.0, RMS([]float64{}))
}

func TestRMSFullScaleSine(t *testing.T) {
	const n = 4096
	samples := make([]float64, n)
	for i := range samples {
		samples[i] = math.Sin(2 * math.Pi * float64(i) / 64)
	}
	// A full-cycle-aligned sine's RMS converges to 1/sqrt(2) ≈ 0.7071.
	got := RMS(samples)
	assert.InDelta(t, 1/math.Sqrt2, got, 1e-3)
}

func TestRMSFullScaleSquare(t *testing.T) {
	samples := make([]float64, 1000)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = 1
		} else {
			samples[i] = -1
		}
	}
	assert.InDelta(t, 1.0, RMS(samples), 1e-12)
}

func TestRMSDC(t *testing.T) {
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = 0.5
	}
	assert.InDelta(t, 0.5, RMS(samples), 1e-12)
}

func TestRMSPooledAcrossChannels(t *testing.T) {
	// Interleaved stereo where one channel is silent and the other is
	// full-scale DC — RMS pools every sample regardless of channel,
	// so the result reflects both channels combined, not per-channel.
	samples := []float64{1, 0, 1, 0, 1, 0}
	got := RMS(samples)
	want := math.Sqrt(0.5) // half the samples are 1, half are 0
	assert.InDelta(t, want, got, 1e-12)
}

// ── Mode bit implications ───────────────────────────────────────────

func TestModeImplications(t *testing.T) {
	assert.Equal(t, ModeMomentary, ModeShortTerm&ModeMomentary)
	assert.Equal(t, ModeMomentary, ModeIntegrated&ModeMomentary)
	assert.Equal(t, ModeShortTerm, ModeLRA&ModeShortTerm)
	assert.Equal(t, ModeMomentary, ModeSamplePeak&ModeMomentary)
	assert.Equal(t, ModeMomentary, ModeTruePeak&ModeMomentary)
	assert.Equal(t, ModeSamplePeak, ModeTruePeak&ModeSamplePeak)

	// ModeAll pulls in true peak, which subsumes sample peak and
	// momentary, plus LRA, which subsumes short term.
	assert.Equal(t, ModeIntegrated, ModeAll&ModeIntegrated)
	assert.Equal(t, ModeLRA, ModeAll&ModeLRA)
	assert.Equal(t, ModeTruePeak, ModeAll&ModeTruePeak)
	assert.Equal(t, ModeShortTerm, ModeAll&ModeShortTerm)
	assert.Equal(t, ModeSamplePeak, ModeAll&ModeSamplePeak)
	assert.Equal(t, ModeMomentary, ModeAll&ModeMomentary)

	// ModeHistogram is independent — not implied by, and does not
	// imply, any other bit.
	assert.Zero(t, ModeHistogram&ModeMomentary)
	assert.Zero(t, ModeAll&ModeHistogram)
}

func TestModeNumericValues(t *testing.T) {
	// Bit values must stay numerically identical to libebur128's enum
	// mode in ebur128.h — this is load-bearing for the cgo parity
	// oracle (Phase B), so pin the raw integers, not just relative
	// bit tests.
	assert.EqualValues(t, 1, ModeMomentary)
	assert.EqualValues(t, 3, ModeShortTerm)
	assert.EqualValues(t, 5, ModeIntegrated)
	assert.EqualValues(t, 11, ModeLRA)
	assert.EqualValues(t, 17, ModeSamplePeak)
	assert.EqualValues(t, 49, ModeTruePeak)
	assert.EqualValues(t, 64, ModeHistogram)
}

// ── Channel enum numeric spot-checks ────────────────────────────────
// Values pinned against ebur128.h's `enum channel`.

func TestChannelNumericValues(t *testing.T) {
	cases := []struct {
		name string
		ch   Channel
		want int
	}{
		{"Unused", ChannelUnused, 0},
		{"Left", ChannelLeft, 1},
		{"Mp030", ChannelMp030, 1},
		{"Right", ChannelRight, 2},
		{"Mm030", ChannelMm030, 2},
		{"Center", ChannelCenter, 3},
		{"Mp000", ChannelMp000, 3},
		{"LeftSurround", ChannelLeftSurround, 4},
		{"Mp110", ChannelMp110, 4},
		{"RightSurround", ChannelRightSurround, 5},
		{"Mm110", ChannelMm110, 5},
		{"DualMono", ChannelDualMono, 6},
		{"MpSC", ChannelMpSC, 7},
		{"MmSC", ChannelMmSC, 8},
		{"Mp060", ChannelMp060, 9},
		{"Mm060", ChannelMm060, 10},
		{"Mp090", ChannelMp090, 11},
		{"Mm090", ChannelMm090, 12},
		{"Mp135", ChannelMp135, 13},
		{"Mm135", ChannelMm135, 14},
		{"Mp180", ChannelMp180, 15},
		{"Up000", ChannelUp000, 16},
		{"Up030", ChannelUp030, 17},
		{"Um030", ChannelUm030, 18},
		{"Up045", ChannelUp045, 19},
		{"Um045", ChannelUm045, 20},
		{"Up090", ChannelUp090, 21},
		{"Um090", ChannelUm090, 22},
		{"Up110", ChannelUp110, 23},
		{"Um110", ChannelUm110, 24},
		{"Up135", ChannelUp135, 25},
		{"Um135", ChannelUm135, 26},
		{"Up180", ChannelUp180, 27},
		{"Tp000", ChannelTp000, 28},
		{"Bp000", ChannelBp000, 29},
		{"Bp045", ChannelBp045, 30},
		{"Bm045", ChannelBm045, 31},
	}
	for _, c := range cases {
		assert.EqualValues(t, c.want, c.ch, "Channel%s", c.name)
	}
}

func TestChannelAliasesShareValues(t *testing.T) {
	assert.Equal(t, ChannelLeft, ChannelMp030)
	assert.Equal(t, ChannelRight, ChannelMm030)
	assert.Equal(t, ChannelCenter, ChannelMp000)
	assert.Equal(t, ChannelLeftSurround, ChannelMp110)
	assert.Equal(t, ChannelRightSurround, ChannelMm110)
}

// ── presets ──────────────────────────────────────────────────────────

func TestTargetPresetsAreNegative(t *testing.T) {
	for _, target := range []float64{TargetEBUR128, TargetATSC, TargetPodcast, TargetStreaming, CeilingEBUR128} {
		assert.Less(t, target, 0.0)
	}
}

func TestTargetPresetValues(t *testing.T) {
	assert.Equal(t, -23.0, TargetEBUR128)
	assert.Equal(t, -24.0, TargetATSC)
	assert.Equal(t, -16.0, TargetPodcast)
	assert.Equal(t, -14.0, TargetStreaming)
	assert.Equal(t, -1.0, CeilingEBUR128)
}

func TestMaxChannels(t *testing.T) {
	assert.Equal(t, 64, maxChannels)
}
