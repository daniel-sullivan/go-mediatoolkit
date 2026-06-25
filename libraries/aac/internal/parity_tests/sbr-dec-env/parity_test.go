// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrdecenv

import (
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"

	"github.com/stretchr/testify/require"
)

// --- ROM table parity (sbr_rom.cpp) -----------------------------------------

// TestParityLimGains verifies the Go FL2FXCONST_SGL-narrowed gain-limit mantissa
// ROM (and the UCHAR exponents) match the genuine in-RAM C symbols.
func TestParityLimGains(t *testing.T) {
	cM, cE := cLimGains(4)
	gM, gE := sbr.LimGains()
	require.Equal(t, cM, gM, "limGains_m")
	require.Equal(t, cE, gE, "limGains_e")
}

// TestParitySmoothFilter verifies the smoothing-filter ROM.
func TestParitySmoothFilter(t *testing.T) {
	require.Equal(t, cSmoothFilter(4), sbr.SmoothFilter())
}

// TestParityLimiterBandsPerOctaveDiv4 verifies the FIXP_SGL + FIXP_DBL
// limiter-band ROM.
func TestParityLimiterBandsPerOctaveDiv4(t *testing.T) {
	cSgl, cDbl := cLimiterBandsPerOctaveDiv4(4)
	gSgl, gDbl := sbr.LimiterBandsPerOctaveDiv4()
	require.Equal(t, cSgl, gSgl, "sgl")
	require.Equal(t, cDbl, gDbl, "dbl")
}

// TestParityRandomPhase verifies all 512 (re,im) FL2FXCONST_SGL-narrowed noise
// pairs, including the MAXVAL_SGL entry at index 301.
func TestParityRandomPhase(t *testing.T) {
	require.Equal(t, cRandomPhase(512), sbr.RandomPhaseFlat())
}

// TestParityInvTable verifies the 256-entry 1/x lookup ROM.
func TestParityInvTable(t *testing.T) {
	require.Equal(t, cInvTable(256), sbr.InvTable())
}

// --- Frequency band-table parity (sbrdec_freq_sca.cpp) ----------------------

// TestParityResetFreqBandTables drives the full master/hi/lo/noise band-table
// builder over a matrix of SBR header configurations (sample rate, start/stop
// freq index, freqScale 0..3, alterScale, noiseBands, xover_band) and compares
// the error code, the master table + count, the hi/lo/noise band tables, the
// nSfb counts, nNfb/nInvfBands and the low/high subband bounds bit-for-bit
// against the genuine resetFreqBandTables.
func TestParityResetFreqBandTables(t *testing.T) {
	cases := []struct {
		fs                                                                uint
		startFreq, stopFreq, freqScale, alterScale, noiseBands, xoverBand uint8
		analysisBands                                                     uint8
	}{
		// 44100: the canonical HE-AAC v1 rate, several freqScale/start positions.
		{44100, 5, 0, 2, 0, 2, 0, 32},
		{44100, 5, 0, 1, 0, 2, 1, 32},
		{44100, 3, 3, 1, 0, 1, 0, 32},
		{44100, 7, 5, 3, 1, 3, 2, 32},
		{44100, 0, 0, 0, 0, 0, 0, 32}, // linear scale
		{44100, 10, 8, 1, 1, 2, 1, 32},
		// 48000
		{48000, 4, 0, 2, 0, 2, 0, 32},
		{48000, 6, 4, 1, 0, 1, 1, 32},
		{48000, 0, 6, 0, 1, 2, 0, 32}, // linear, alterScale
		// 32000
		{32000, 5, 0, 2, 0, 2, 0, 32},
		{32000, 8, 5, 3, 0, 3, 2, 32},
		// 24000
		{24000, 3, 0, 1, 0, 1, 0, 32},
		{24000, 6, 4, 2, 1, 2, 1, 32},
		// 22050
		{22050, 4, 0, 2, 0, 2, 0, 32},
		// 16000
		{16000, 2, 0, 1, 0, 1, 0, 32},
		{16000, 5, 3, 3, 0, 2, 1, 32},
		// higher rates
		{64000, 3, 0, 2, 0, 2, 0, 32},
		{40000, 5, 4, 1, 0, 2, 1, 32},
	}

	for ci, c := range cases {
		got := sbr.RunResetFreqBandTables(c.fs, c.startFreq, c.stopFreq, c.freqScale, c.alterScale, c.noiseBands, c.xoverBand, c.analysisBands, 0)
		want := cResetFreqBandTables(c.fs, c.startFreq, c.stopFreq, c.freqScale, c.alterScale, c.noiseBands, c.xoverBand, c.analysisBands, 0)

		require.Equal(t, want.err, got.Err, "case %d err", ci)
		// When the config is unsupported both must agree on the error; the band
		// tables are then don't-care (the C left them partially written), so only
		// compare the full output when the build succeeded.
		require.Equal(t, want.numMaster, got.NumMaster, "case %d numMaster", ci)
		require.Equal(t, want.vKMaster, got.VKMaster, "case %d vKMaster", ci)
		if want.err != 0 {
			continue
		}
		require.Equal(t, want.nSfb[0], got.NSfbLo, "case %d nSfbLo", ci)
		require.Equal(t, want.nSfb[1], got.NSfbHi, "case %d nSfbHi", ci)
		require.Equal(t, want.nNfb, got.NNfb, "case %d nNfb", ci)
		require.Equal(t, want.nInvfBands, got.NInvfBands, "case %d nInvfBands", ci)
		require.Equal(t, want.lowSubband, got.LowSubband, "case %d lowSubband", ci)
		require.Equal(t, want.highSubband, got.HighSubband, "case %d highSubband", ci)
		require.Equal(t, want.freqBandLo, got.FreqBandLo, "case %d freqBandLo", ci)
		require.Equal(t, want.freqBandHi, got.FreqBandHi, "case %d freqBandHi", ci)
		require.Equal(t, want.freqBandNoise, got.FreqBandNoise, "case %d freqBandNoise", ci)
	}
}
