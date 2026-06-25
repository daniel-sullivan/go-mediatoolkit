// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencfreqsca

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// coreRates are the legal AAC-LC core sampling rates for HE-AAC v1 (the SBR
// output rate is 2× these); 64 QMF bands (dual-rate).
var coreRates = []int{16000, 22050, 24000, 32000, 44100, 48000}

// TestStartStopAndFreqScale sweeps the full legal (coreRate, startFreq,
// stopFreq, freqScale, alterScale) grid: for each combination it runs
// FindStartAndStopBand, then UpdateFreqScale -> UpdateHiRes -> UpdateLoRes on
// both the C reference and the Go port and asserts every band table is
// bit-identical.
func TestStartStopAndFreqScale(t *testing.T) {
	const noChannels = 64
	for _, srCore := range coreRates {
		srSbr := 2 * srCore
		for startFreq := 0; startFreq <= 15; startFreq++ {
			for stopFreq := 0; stopFreq <= 13; stopFreq++ {
				ck0, ck2, cerr := cStartStop(srSbr, srCore, noChannels, startFreq, stopFreq)
				gk0, gk2, gerr := sbr.FindStartAndStopBand(srSbr, srCore, noChannels, startFreq, stopFreq)
				require.Equal(t, cerr, gerr, "FindStartAndStopBand err sr=%d sf=%d ef=%d", srCore, startFreq, stopFreq)
				require.Equal(t, ck0, gk0, "k0 sr=%d sf=%d ef=%d", srCore, startFreq, stopFreq)
				require.Equal(t, ck2, gk2, "k2 sr=%d sf=%d ef=%d", srCore, startFreq, stopFreq)
				if cerr != 0 {
					continue
				}
				// freqScale 1..3 are the Bark modes; the HE-AAC encoder never
				// uses freqScale 0 (linear) — SBR_FREQ_SCALE_DEFAULT==2 and the
				// tuning table only carries 2/3 — and the C linear path reads
				// out-of-bounds stack memory for degenerate tiny ranges (UB), so
				// it is not a meaningful parity target.
				for freqScale := 1; freqScale <= 3; freqScale++ {
					for alterScale := 0; alterScale <= 1; alterScale++ {
						cvk, cn := cUpdateFreqScale(gk0, gk2, freqScale, alterScale)
						gvk := make([]uint8, 64)
						gn, gerr2 := sbr.UpdateFreqScale(gvk, gk0, gk2, freqScale, alterScale)
						if cn < 0 {
							require.NotEqual(t, 0, gerr2, "Go should also fail UpdateFreqScale")
							continue
						}
						require.Equal(t, 0, gerr2)
						require.Equal(t, cn, gn, "numMaster sr=%d k0=%d k2=%d fs=%d as=%d", srCore, gk0, gk2, freqScale, alterScale)
						require.Equal(t, cvk, gvk[:gn+1], "v_k_master sr=%d k0=%d k2=%d fs=%d as=%d", srCore, gk0, gk2, freqScale, alterScale)

						// Drive HiRes/LoRes for a couple of crossover bands.
						for _, xover := range []int{0, 1, gn / 2, gn} {
							if xover < 0 || xover > gn {
								continue
							}
							chi, clo, cnh, cnl, cxo := cHiResLoRes(cvk, cn, xover)

							ghi := make([]uint8, 64)
							gnh, gxo, _ := sbr.UpdateHiRes(ghi, gvk, gn, xover)
							glo := make([]uint8, 64)
							gnl := sbr.UpdateLoRes(glo, ghi, gnh)

							assert.Equal(t, cxo, gxo, "xover clip")
							require.Equal(t, cnh, gnh, "numHires")
							require.Equal(t, cnl, gnl, "numLores")
							require.Equal(t, chi, ghi[:gnh+1], "hires")
							require.Equal(t, clo, glo[:gnl+1], "lores")
						}
					}
				}
			}
		}
	}
}

// TestRawFreqs checks the getSbrStart/StopFreqRAW helpers across the legal grid.
func TestRawFreqs(t *testing.T) {
	for _, srCore := range coreRates {
		for sf := 0; sf <= 15; sf++ {
			assert.Equal(t, cStartFreqRAW(sf, srCore), sbr.GetSbrStartFreqRAW(sf, srCore),
				"startRAW sr=%d sf=%d", srCore, sf)
		}
		for ef := 0; ef <= 13; ef++ {
			assert.Equal(t, cStopFreqRAW(ef, srCore), sbr.GetSbrStopFreqRAW(ef, srCore),
				"stopRAW sr=%d ef=%d", srCore, ef)
		}
	}
}
