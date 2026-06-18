// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the Fraunhofer FDK-AAC SBR-encoder
// missing-harmonics detector, libSBRenc/src/mh_det.cpp
// (FDKsbrEnc_SbrMissingHarmonicsDetectorQmf + InitSbrMissingHarmonicsDetector
// and the static chain diff / calculateFlatnessMeasure / calculateDetectorInput
// / removeLowPassDetection / isDetectionOfNewToneAllowed / transientCleanUp /
// detection / detectionWithPrediction / calculateCompVector, with the AAC
// detector-parameter ROM). From the per-estimate tonality (quota) matrix, sign
// buffer and the frame grid, it decides which scalefactor bands need an added
// synthetic harmonic and the per-band envelope compensation.
//
// fdk-aac SBR is FIXED-POINT: EXACT integer parity. The shared libFDK kernels
// (fMult, fDivNorm, CalcLdData, CalcInvLdData, schur_div, scaleValue, fMin/fMax,
// CountLeadingBits, GetInvInt) are reused bit-for-bit from internal/nativeaac.
//
// Scope: HE-AAC v1 (paramsAac). The paramsAacLd (SBR_SYNTAX_LOW_DELAY) parameter
// set and its frame-size branches are ported faithfully in the init (small,
// table-driven), but the HE-AAC v1 caller selects paramsAac. The
// Create/Delete RAM-pool plumbing (GetRam_*) is replaced by plain Go slice
// allocation in the detector state — RAM pooling is not part of the algorithm.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// SFM_SHIFT / SFM_SCALE (mh_det.cpp:110-111).
const (
	mhSfmShift = 2
	mhSfmScale = int32(0x7FFFFFFF) >> mhSfmShift // MAXVAL_DBL >> SFM_SHIFT
)

// relaxationFloat / RELAXATION / RELAXATION_FRACT / RELAXATION_SHIFT /
// RELAXATION_LD64 (sbr_def.h:133-140).
const (
	relaxationFloat   = float32(1e-6)
	relaxationShift   = 19
	relaxationLdDataS = 6 // LD_DATA_SHIFT
)

func relaxation() int32      { return fl2f(relaxationFloat) }
func relaxationFract() int32 { return fl2f(0.524288) }
func relaxationLd64() int32  { return fl2f(0.31143075889) }

// paramsAac is the 1:1 port of the AAC-core DETECTOR_PARAMETERS_MH ROM
// (mh_det.cpp:114-137). Built at first use because the FL2FXCONST_DBL constants
// route through the runtime narrowing kernel.
func paramsAac() *DetectorParametersMH {
	return &DetectorParametersMH{
		DeltaTime: 9,
		ThresHolds: ThresHolds{
			ThresHoldDiff:       fl2f(20.0 * relaxationFloat),
			ThresHoldDiffGuide:  fl2f(1.26 * relaxationFloat),
			ThresHoldTone:       fl2f(15.0 * relaxationFloat),
			InvThresHoldTone:    fl2f((1.0 / 15.0) * relaxationFloat),
			ThresHoldToneGuide:  fl2f(1.26 * relaxationFloat),
			SfmThresSbr:         fl2f(0.3) >> mhSfmShift,
			SfmThresOrig:        fl2f(0.1) >> mhSfmShift,
			DecayGuideOrig:      fl2f(0.3),
			DecayGuideDiff:      fl2f(0.5),
			DerivThresMaxLD64:   fl2(-0.000112993269),
			DerivThresBelowLD64: fl2(-0.000112993269),
			DerivThresAboveLD64: fl2f(-0.005030126483),
		},
		MaxComp: 50,
	}
}

// paramsAacLd is the AAC-LD DETECTOR_PARAMETERS_MH ROM (mh_det.cpp:140-163).
func paramsAacLd() *DetectorParametersMH {
	p := paramsAac()
	p.DeltaTime = 16
	p.ThresHolds.ThresHoldDiff = fl2f(25.0 * relaxationFloat)
	p.ThresHolds.DecayGuideDiff = fl2f(0.2)
	return p
}

// lsiDivideScaleFract is the 1:1 port of FDKsbrEnc_LSI_divide_scale_fract
// (sbr_misc.cpp): num*scale/denom with best precision + scaling.
func lsiDivideScaleFract(num, denom, scale int32) int32 {
	tmp := int32(0)
	if num != 0 {
		shiftNum := nativeaac.CountLeadingBits(num)
		shiftDenom := nativeaac.CountLeadingBits(denom)
		shiftScale := nativeaac.CountLeadingBits(scale)

		num = num << uint(shiftNum)
		scale = scale << uint(shiftScale)

		tmp = nativeaac.FMultDiv2DD(num, scale)

		if denom > (tmp >> uint(nativeaac.FMinI(shiftNum+shiftScale-1, dfractBits-1))) {
			denom = denom << uint(shiftDenom)
			tmp = nativeaac.SchurDiv(tmp, denom, 15)
			shiftCommon := nativeaac.FMinI(shiftNum-shiftDenom+shiftScale-1, dfractBits-1)
			if shiftCommon < 0 {
				tmp <<= uint(-shiftCommon)
			} else {
				tmp >>= uint(shiftCommon)
			}
		} else {
			tmp = encMaxvalDBL
		}
	}
	return tmp
}

// diff is the 1:1 port of diff (mh_det.cpp:176-204).
func diff(pTonalityOrig, pDiffMapped2Scfb []int32, pFreqBandTable []uint8, nScfb int, indexVector []int8) {
	for i := 0; i < nScfb; i++ {
		ll := pFreqBandTable[i]
		lu := pFreqBandTable[i+1]

		maxValOrig := int32(0)
		maxValSbr := int32(0)

		for k := ll; k < lu; k++ {
			maxValOrig = nativeaac.FMaxDBL(maxValOrig, pTonalityOrig[k])
			maxValSbr = nativeaac.FMaxDBL(maxValSbr, pTonalityOrig[indexVector[k]])
		}

		if maxValSbr >= relaxation() {
			tmp, scale := nativeaac.FDivNorm(maxValOrig, maxValSbr)
			pDiffMapped2Scfb[i] = nativeaac.ScaleValue(nativeaac.FMultDD(tmp, relaxationFract()), int32(nativeaac.FMaxI(-(dfractBits-1), int(scale)-relaxationShift)))
		} else {
			pDiffMapped2Scfb[i] = maxValOrig
		}
	}
}

// calculateFlatnessMeasure is the 1:1 port of calculateFlatnessMeasure
// (mh_det.cpp:227-304).
func calculateFlatnessMeasure(pQuotaBuffer []int32, indexVector []int8, pSfmOrigVec, pSfmSbrVec []int32, pFreqBandTable []uint8, nSfb int) {
	for i := 0; i < nSfb; i++ {
		ll := int(pFreqBandTable[i])
		lu := int(pFreqBandTable[i+1])
		pSfmOrigVec[i] = encMaxvalDBL >> 2
		pSfmSbrVec[i] = encMaxvalDBL >> 2

		if lu-ll > 1 {
			invBands := nativeaac.GetInvInt(lu - ll)
			shiftFacSum0 := 0
			shiftFacSum1 := 0
			amOrig := int32(0)
			amTransp := int32(0)
			gmOrig := encMaxvalDBL
			gmTransp := encMaxvalDBL

			for j := ll; j < lu; j++ {
				sfmOrig := pQuotaBuffer[j]
				sfmTransp := pQuotaBuffer[indexVector[j]]

				amOrig += nativeaac.FMultDD(sfmOrig, invBands)
				amTransp += nativeaac.FMultDD(sfmTransp, invBands)

				shiftFac0 := int(nativeaac.CountLeadingBits(sfmOrig))
				shiftFac1 := int(nativeaac.CountLeadingBits(sfmTransp))

				gmOrig = nativeaac.FMultDD(gmOrig, sfmOrig<<uint(shiftFac0))
				gmTransp = nativeaac.FMultDD(gmTransp, sfmTransp<<uint(shiftFac1))

				shiftFacSum0 += shiftFac0
				shiftFacSum1 += shiftFac1
			}

			if gmOrig > 0 {
				tmp1 := nativeaac.CalcLdData(gmOrig)
				tmp1 = nativeaac.FMultDD(invBands, tmp1)
				accu := int32(-shiftFacSum0) << (dfractBits - 1 - 8)
				tmp2 := nativeaac.FMultDiv2DD(invBands, accu) << (2 + 1)
				tmp2 = tmp1 + tmp2
				gmOrig = nativeaac.CalcInvLdData(tmp2)
			} else {
				gmOrig = 0
			}

			if gmTransp > 0 {
				tmp1 := nativeaac.CalcLdData(gmTransp)
				tmp1 = nativeaac.FMultDD(invBands, tmp1)
				accu := int32(-shiftFacSum1) << (dfractBits - 1 - 8)
				tmp2 := nativeaac.FMultDiv2DD(invBands, accu) << (2 + 1)
				tmp2 = tmp1 + tmp2
				gmTransp = nativeaac.CalcInvLdData(tmp2)
			} else {
				gmTransp = 0
			}

			if amOrig != 0 {
				pSfmOrigVec[i] = lsiDivideScaleFract(gmOrig, amOrig, mhSfmScale)
			}
			if amTransp != 0 {
				pSfmSbrVec[i] = lsiDivideScaleFract(gmTransp, amTransp, mhSfmScale)
			}
		}
	}
}

// calculateDetectorInput is the 1:1 port of calculateDetectorInput
// (mh_det.cpp:315-333).
func calculateDetectorInput(pQuotaBuffer [][]int32, indexVector []int8, tonalityDiff, pSfmOrig, pSfmSbr [][]int32, freqBandTable []uint8, nSfb, noEstPerFrame, move int) {
	for est := 0; est < noEstPerFrame; est++ {
		diff(pQuotaBuffer[est+move], tonalityDiff[est+move], freqBandTable, nSfb, indexVector)
		calculateFlatnessMeasure(pQuotaBuffer[est+move], indexVector, pSfmOrig[est+move], pSfmSbr[est+move], freqBandTable, nSfb)
	}
}

// removeLowPassDetection is the 1:1 port of removeLowPassDetection
// (mh_det.cpp:347-444).
func removeLowPassDetection(pAddHarmSfb []uint8, pDetectionVectors [][]uint8, start, stop, nSfb int, pFreqBandTable []uint8, pNrgVector []int32, mhThresh ThresHolds) {
	maxDerivPos := int(pFreqBandTable[nSfb])
	numBands := int(pFreqBandTable[nSfb])
	bLPsignal := 0

	maxValLD64 := fl2f(-1.0)
	for i := numBands - 1 - 2; i > int(pFreqBandTable[0]); i-- {
		nrgLow := pNrgVector[i]
		nrgHigh := pNrgVector[i+2]
		if nrgLow != 0 && nrgLow > nrgHigh {
			nrgLowLD64 := nativeaac.CalcLdData(nrgLow >> 1)
			nrgDiffLD64 := nativeaac.CalcLdData((nrgLow >> 1) - (nrgHigh >> 1))
			valLD64 := nrgDiffLD64 - nrgLowLD64
			if valLD64 > maxValLD64 {
				maxDerivPos = i
				maxValLD64 = valLD64
			}
			if maxValLD64 > mhThresh.DerivThresMaxLD64 {
				break
			}
		}
	}

	maxValAboveLD64 := fl2f(-1.0)
	for i := numBands - 1 - 2; i > maxDerivPos+2; i-- {
		nrgLow := pNrgVector[i]
		nrgHigh := pNrgVector[i+2]
		if nrgLow != 0 && nrgLow > nrgHigh {
			nrgLowLD64 := nativeaac.CalcLdData(nrgLow >> 1)
			nrgDiffLD64 := nativeaac.CalcLdData((nrgLow >> 1) - (nrgHigh >> 1))
			valLD64 := nrgDiffLD64 - nrgLowLD64
			if valLD64 > maxValAboveLD64 {
				maxValAboveLD64 = valLD64
			}
		} else {
			if nrgHigh != 0 && nrgHigh > nrgLow {
				nrgHighLD64 := nativeaac.CalcLdData(nrgHigh >> 1)
				nrgDiffLD64 := nativeaac.CalcLdData((nrgHigh >> 1) - (nrgLow >> 1))
				valLD64 := nrgDiffLD64 - nrgHighLD64
				if valLD64 > maxValAboveLD64 {
					maxValAboveLD64 = valLD64
				}
			}
		}
	}

	if maxValLD64 > mhThresh.DerivThresMaxLD64 && maxValAboveLD64 < mhThresh.DerivThresAboveLD64 {
		bLPsignal = 1
		for i := maxDerivPos - 1; i > maxDerivPos-5 && i >= 0; i-- {
			if pNrgVector[i] != 0 && pNrgVector[i] > pNrgVector[maxDerivPos+2] {
				nrgDiffLD64 := nativeaac.CalcLdData((pNrgVector[i] >> 1) - (pNrgVector[maxDerivPos+2] >> 1))
				nrgLD64 := nativeaac.CalcLdData(pNrgVector[i] >> 1)
				valLD64 := nrgDiffLD64 - nrgLD64
				if valLD64 < mhThresh.DerivThresBelowLD64 {
					bLPsignal = 0
					break
				}
			} else {
				bLPsignal = 0
				break
			}
		}
	}

	if bLPsignal != 0 {
		i := 0
		for i = 0; i < nSfb; i++ {
			if maxDerivPos >= int(pFreqBandTable[i]) && maxDerivPos < int(pFreqBandTable[i+1]) {
				break
			}
		}
		if i < nSfb && pAddHarmSfb[i] != 0 {
			pAddHarmSfb[i] = 0
			for est := start; est < stop; est++ {
				pDetectionVectors[est][i] = 0
			}
		}
	}
}

// isDetectionOfNewToneAllowed is the 1:1 port of isDetectionOfNewToneAllowed
// (mh_det.cpp:456-516).
func isDetectionOfNewToneAllowed(pFrameInfo *SbrFrameInfo, pDetectionStartPos *int, noEstPerFrame, prevTransientFrame, prevTransientPos, prevTransientFlag, transientPosOffset, transientFlag, transientPos, deltaTime int, h *SbrMissingHarmonicsDetector) int {
	transientFrame := 0
	if transientFlag != 0 {
		if transientPos+transientPosOffset < pFrameInfo.Borders[pFrameInfo.NEnvelopes] {
			transientFrame = 1
			if noEstPerFrame > 1 {
				if transientPos+transientPosOffset > h.TimeSlots>>1 {
					*pDetectionStartPos = noEstPerFrame
				} else {
					*pDetectionStartPos = noEstPerFrame >> 1
				}
			} else {
				*pDetectionStartPos = noEstPerFrame
			}
		}
	} else {
		if prevTransientFlag != 0 && prevTransientFrame == 0 {
			transientFrame = 1
			*pDetectionStartPos = 0
		}
	}

	newDetectionAllowed := 0
	if transientFrame != 0 {
		newDetectionAllowed = 1
	} else {
		if prevTransientFrame != 0 && int(nativeaac.FixpAbs(int32(pFrameInfo.Borders[0]-(prevTransientPos+transientPosOffset-h.TimeSlots)))) < deltaTime {
			newDetectionAllowed = 1
			*pDetectionStartPos = 0
		}
	}

	h.PreviousTransientFlag = transientFlag
	h.PreviousTransientFrame = transientFrame
	h.PreviousTransientPos = transientPos

	return newDetectionAllowed
}
