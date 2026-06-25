// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// ResetLimiterBands, ported 1:1 from ResetLimiterBands (env_calc.cpp:3060-3202):
// build the gain-limiter band-border table (limiterBandTable / noLimiterBands)
// from the transposer patch areas and the selected limiter-band density. Called
// once per header reset by the SBR decode driver. HE-AAC v1 (LPP) path: the
// HBE-only xOverQmf branch (sbrPatchingMode==0 && xOverQmf!=nil) is preserved for
// fidelity but never taken (HBE is USAC), so xOverQmf is nil here. Pure
// fixed-point — bit-exact in any build.

// resetLimiterStatus mirrors SBR_ERROR (SBRDEC_OK / SBRDEC_UNSUPPORTED_CONFIG).
const (
	resetLimiterOK                = 0
	resetLimiterUnsupportedConfig = 4
)

// ResetLimiterBands is the 1:1 port of ResetLimiterBands (env_calc.cpp:3060).
// limiterBandTable / noLimiterBands are written; freqBandTable holds the possible
// borders (noFreqBands entries valid). patchParam/noPatches come from the LPP
// transposer. xOverQmf is the HBE crossover table (nil for HE-AAC v1).
//
// C counterpart: ResetLimiterBands (env_calc.cpp:3060).
func ResetLimiterBands(limiterBandTable []uint8, noLimiterBands *uint8, freqBandTable []uint8, noFreqBands int, patchParam []patchParam, noPatches int, limiterBands int, sbrPatchingMode uint8, xOverQmf []int, b41Sbr int) int {
	var isPatchBorder [2]int
	var workLimiterBandTable [maxFreqCoeffs/2 + maxNumPatches + 1]uint8
	var patchBorders [maxNumPatches + 1]int

	lowSubband := int(freqBandTable[0])
	highSubband := int(freqBandTable[noFreqBands])

	var nBands int

	if limiterBands == 0 {
		// 1 limiter band.
		limiterBandTable[0] = 0
		limiterBandTable[1] = uint8(highSubband - lowSubband)
		nBands = 1
	} else {
		var i int
		if sbrPatchingMode == 0 && xOverQmf != nil {
			noPatches = 0
			if b41Sbr == 1 {
				for i = 1; i < maxNumPatchesHBE; i++ {
					if xOverQmf[i] != 0 {
						noPatches++
					}
				}
			} else {
				for i = 1; i < maxStretchHBE; i++ {
					if xOverQmf[i] != 0 {
						noPatches++
					}
				}
			}
			for i = 0; i < noPatches; i++ {
				patchBorders[i] = xOverQmf[i] - lowSubband
			}
		} else {
			for i = 0; i < noPatches; i++ {
				patchBorders[i] = int(patchParam[i].guardStartBand) - lowSubband
			}
		}
		patchBorders[i] = highSubband - lowSubband

		// 1.2, 2, or 3 limiter bands/octave plus bandborders at patchborders.
		for k := 0; k <= noFreqBands; k++ {
			workLimiterBandTable[k] = uint8(int(freqBandTable[k]) - lowSubband)
		}
		for k := 1; k < noPatches; k++ {
			workLimiterBandTable[noFreqBands+k] = uint8(patchBorders[k])
		}

		tempNoLim := noFreqBands + noPatches - 1
		nBands = tempNoLim
		shellsort(workLimiterBandTable[:tempNoLim+1], uint8(tempNoLim+1))

		loLimIndex := 0
		hiLimIndex := 1

		for hiLimIndex <= tempNoLim {
			k2 := int(workLimiterBandTable[hiLimIndex]) + lowSubband
			kx := int(workLimiterBandTable[loLimIndex]) + lowSubband

			divM, divE := nativeaac.FDivNorm(int32(k2), int32(kx))

			// calculate number of octaves
			octM, octE := nativeaac.CalcLog2(divM, divE)

			// multiply with limiterbands per octave (scale factor of 2)
			temp, tempE := nativeaac.FMultNorm(octM, sbrLimiterBPODiv4DBL[limiterBands])
			tempEInt := int(tempE) + int(octE) + 2

			// scale factor of 5 for comparison; 0.49f >> 5.
			if (temp >> uint(5-tempEInt)) < (nativeaac.Fl2fxconstDBL(0.49) >> 5) {
				if workLimiterBandTable[hiLimIndex] == workLimiterBandTable[loLimIndex] {
					workLimiterBandTable[hiLimIndex] = uint8(highSubband)
					nBands--
					hiLimIndex++
					continue
				}
				isPatchBorder[0], isPatchBorder[1] = 0, 0
				for k := 0; k <= noPatches; k++ {
					if int(workLimiterBandTable[hiLimIndex]) == patchBorders[k] {
						isPatchBorder[1] = 1
						break
					}
				}
				if isPatchBorder[1] == 0 {
					workLimiterBandTable[hiLimIndex] = uint8(highSubband)
					nBands--
					hiLimIndex++
					continue
				}
				for k := 0; k <= noPatches; k++ {
					if int(workLimiterBandTable[loLimIndex]) == patchBorders[k] {
						isPatchBorder[0] = 1
						break
					}
				}
				if isPatchBorder[0] == 0 {
					workLimiterBandTable[loLimIndex] = uint8(highSubband)
					nBands--
				}
			}
			loLimIndex = hiLimIndex
			hiLimIndex++
		}
		shellsort(workLimiterBandTable[:tempNoLim+1], uint8(tempNoLim+1))

		if nBands > maxNumLimiters || nBands <= 0 {
			return resetLimiterUnsupportedConfig
		}

		if int(workLimiterBandTable[tempNoLim]) > highSubband {
			return resetLimiterUnsupportedConfig
		}

		for k := 0; k <= nBands; k++ {
			limiterBandTable[k] = workLimiterBandTable[k]
		}
	}
	*noLimiterBands = uint8(nBands)

	return resetLimiterOK
}
