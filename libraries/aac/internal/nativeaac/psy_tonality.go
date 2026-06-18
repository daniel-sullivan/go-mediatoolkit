// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Tonality (chaos -> tonality index) for the psychoacoustic model — a 1:1
// port of tonality.cpp (FDKaacEnc_CalculateFullTonality, line 121;
// FDKaacEnc_CalcSfbTonality, line 175). FDKaacEnc_psyMain calls
// CalculateFullTonality per long-block channel (when PNS is enabled) to derive
// per-SFB tonality from the per-line chaos measure (calculateChaosMeasure,
// already ported) and the SFB energies in ldData form. Pure fixed-point:
// FIXP_DBL/FIXP_SGL fractions, fMultDiv2, CalcLdData — bit-identical
// regardless of build tag.

// normlogTonality is the FL2FXCONST_DBL constant from tonality.cpp:110-112:
//
//	static const FIXP_DBL normlog = (FIXP_DBL)0xd977d949;
//	/* FL2FXCONST_DBL(-0.4342944819f * FDKlog(2.0)/FDKlog(2.7182818)); */
const normlogTonality int32 = -646457015 // 0xd977d949 as signed int32

// maxvalSGL is MAXVAL_SGL == (signed)0x7FFF (common_fix.h), the largest
// FIXP_SGL value, used to saturate the tonality index.
const maxvalSGL int16 = 0x7FFF

// tonalityScratchLines mirrors the C_ALLOC_SCRATCH_START(chaosMeasurePerLine,
// FIXP_DBL, 1024) scratch in CalculateFullTonality.
const tonalityScratchLines = 1024

// calculateFullTonality computes per-SFB tonality from the MDCT spectrum: it
// runs the chaos measure over the active lines, smooths it with a one-pole
// 0.25/0.75 IIR, then maps to a tonality index per SFB. When usePns is 0 it is
// a no-op (matching the C: tonality is only needed for PNS). C counterpart:
// FDKaacEnc_CalculateFullTonality, tonality.cpp:121.
//
// Writes sfbTonality[0:sfbCnt]. spectrum/sfbMaxScaleSpec/sfbEnergyLD64 are the
// long-block arrays; sfbOffset has sfbCnt+1 entries.
func calculateFullTonality(
	spectrum []int32, sfbMaxScaleSpec []int, sfbEnergyLD64 []int32,
	sfbTonality []int16, sfbCnt int, sfbOffset []int, usePns int,
) {
	numberOfLines := sfbOffset[sfbCnt]

	if usePns == 0 {
		return
	}

	chaosMeasurePerLine := make([]int32, tonalityScratchLines)

	// calculate chaos measure
	calculateChaosMeasure(spectrum, numberOfLines, chaosMeasurePerLine)

	// smooth ChaosMeasure: 0.25*left + 0.75*right
	left := chaosMeasurePerLine[0]
	var right int32
	var j int
	for j = 1; j < numberOfLines-1; j += 2 {
		right = chaosMeasurePerLine[j]
		right = right - (right >> 2)
		left = right + (left >> 2)
		chaosMeasurePerLine[j] = left

		right = chaosMeasurePerLine[j+1]
		right = right - (right >> 2)
		left = right + (left >> 2)
		chaosMeasurePerLine[j+1] = left
	}
	if j == numberOfLines-1 {
		right = chaosMeasurePerLine[j]
		right = right - (right >> 2)
		left = right + (left >> 2)
		chaosMeasurePerLine[j] = left
	}

	calcSfbTonality(spectrum, sfbMaxScaleSpec, chaosMeasurePerLine,
		sfbTonality, sfbCnt, sfbOffset, sfbEnergyLD64)
}

// calcSfbTonality computes per-SFB tonality values from energies and chaos
// measure: it accumulates a chaos-weighted energy per SFB, then derives a
// log-domain tonality index, limiting range and computing log(). C
// counterpart: FDKaacEnc_CalcSfbTonality, tonality.cpp:175.
//
//	for (i = 0; i < sfbCnt; i++) {
//	  INT shiftBits = fixMax(0, sfbMaxScaleSpec[i] - 4);
//	  FIXP_DBL chaosMeasureSfb = 0;
//	  for (j = (sfbOffset[i+1]-sfbOffset[i])-1; j >= 0; j--) {
//	    FIXP_DBL tmp = (*spectrum++) << shiftBits;
//	    FIXP_DBL lineNrg = fMultDiv2(tmp, tmp);
//	    chaosMeasureSfb = fMultAddDiv2(chaosMeasureSfb, lineNrg, *chaosMeasure++);
//	  }
//	  ... log-domain mapping ...
//	}
func calcSfbTonality(
	spectrum []int32, sfbMaxScaleSpec []int, chaosMeasure []int32,
	sfbTonality []int16, sfbCnt int, sfbOffset []int, sfbEnergyLD64 []int32,
) {
	// FL2FXCONST_DBL constants from tonality.cpp:
	//   FL2FXCONST_DBL(3.0f/64)   and  FL2FXCONST_DBL(-0.0519051), FL2FXCONST_DBL(-1.0)?
	const c3over64 = int32(0x06000000)    // FL2FXCONST_DBL(3.0/64) == 100663296
	const cNeg0519051 = int32(-111465353) // FL2FXCONST_DBL(-0.0519051f) == 0xf95b2c77: ld(0.05)+ld(2)

	specIdx := 0
	chaosIdx := 0
	for i := 0; i < sfbCnt; i++ {
		// max sfbWidth = 96 ; 2^7=128 => 7/2 = 4 (spc*spc)
		shiftBits := 0
		if sfbMaxScaleSpec[i]-4 > 0 {
			shiftBits = sfbMaxScaleSpec[i] - 4
		}

		var chaosMeasureSfb int32 = 0

		// calc chaosMeasurePerSfb
		for j := (sfbOffset[i+1] - sfbOffset[i]) - 1; j >= 0; j-- {
			tmp := spectrum[specIdx] << uint(shiftBits)
			specIdx++
			lineNrg := fMultDiv2DD(tmp, tmp)
			chaosMeasureSfb = fMultAddDiv2(chaosMeasureSfb, lineNrg, chaosMeasure[chaosIdx])
			chaosIdx++
		}

		// calc tonalityPerSfb
		if chaosMeasureSfb != 0 {
			// add ld(convtone)/64 and 2/64 bec.fMultDiv2
			chaosMeasureSfbLD64 := calcLdData(chaosMeasureSfb) - sfbEnergyLD64[i]
			chaosMeasureSfbLD64 += c3over64 - (int32(shiftBits) << (chaosDfractBits - 6))

			if chaosMeasureSfbLD64 > cNeg0519051 { // > ld(0.05)+ld(2)
				if chaosMeasureSfbLD64 <= 0 {
					sfbTonality[i] = int16((fMultDiv2DD(chaosMeasureSfbLD64, normlogTonality) << 7) >> 16)
				} else {
					sfbTonality[i] = 0 // FL2FXCONST_SGL(0.0)
				}
			} else {
				sfbTonality[i] = maxvalSGL
			}
		} else {
			sfbTonality[i] = maxvalSGL
		}
	}
}
