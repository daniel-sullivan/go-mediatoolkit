// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Encode-side intensity-stereo processing, ported 1:1 from
// libAACenc/src/intensity.cpp. This is the channel-pair tool
// FDKaacEnc_IntensityStereoProcessing that decides, per scalefactor band, whether
// to code the right channel as an intensity-stereo direction of the left, and if
// so collapses the pair onto the left channel (zeroing the right spectrum and its
// energies/thresholds), sets isBook/isScale, updates the M/S mask, and switches
// off PNS where an IS region is active.
//
// fdk-aac encode is FIXED-POINT: every value is an int32 FIXP_DBL Q-format
// quantity. The chain is pure integer fixed-point (fMult int64 products,
// arithmetic shifts, leading-bit counts, the sqrtFixp / fDivNorm / GetInvInt
// table-driven kernels) — bit-identical regardless of vectorization — so it
// carries only the aacfdk fence (no aac_strict FP split). PNS_DATA is defined in
// aacenc_pns.go and consumed here (one coherent definition). The realScale /
// hrrErr / normSfbLoudness working buffers are sized maxGroupedSfb, matching the
// C MAX_GROUPED_SFB stack arrays.

// fMult multiplies two FIXP_DBL fractions at full scale. C counterpart:
// fMult(FIXP_DBL, FIXP_DBL) == fixmul_DD (common_fix.h:241) == fixmulDDarm8 on
// the aarch64 target (fixmul_arm.h:156-191): `smull; asr #31` ==
// (int64(a)*int64(b))>>31, which KEEPS bit 31. The generic fMultDD
// (fixmuldiv2_DD(a,b)<<1) drops that LSB, so it must NOT be used here — the
// one-LSB difference flips the borderline left/right-ratio IS gate
// (intensity.cpp:264-267, the `fMult(IS_LEFT_RIGHT_RATIO_THRESH, energy)`
// comparisons) on real signals. Use the arm8 form to match fdk bit-for-bit.
func fMult(a, b int32) int32 { return fixmulDDarm8(a, b) }

// Intensity-stereo tuning constants, ported 1:1 from intensity.cpp:111-152.
// REAL_SCALE_SF / OVERALL_LOUDNESS_SF / MAX_SFB_PER_GROUP_SF / MDCT_SPEC_SF are
// the working-buffer scalefactors; the FL2FXCONST_DBL thresholds are folded at
// init time exactly as the C compiler would.
const (
	// IS_DIRECTION_DEVIATION_THRESH_SF (intensity.cpp:123).
	isDirectionDeviationThreshSF = 2
	// IS_MIN_SFBS (intensity.cpp:132): only do IS if >= 6 neighbouring SFBs.
	isMinSfbs = 6
	// REAL_SCALE_SF (intensity.cpp:142): scalefactor of realScale.
	realScaleSF = 1
	// OVERALL_LOUDNESS_SF (intensity.cpp:145).
	overallLoudnessSF = 6
	// MAX_SFB_PER_GROUP_SF (intensity.cpp:148).
	maxSfbPerGroupSF = 6
	// MDCT_SPEC_SF (intensity.cpp:151).
	mdctSpecSF = 6
)

// intensityParameters ports the file-local INTENSITY_PARAMETERS struct
// (intensity.cpp:153-179): the per-call tuning thresholds.
type intensityParameters struct {
	corrThresh               int32 // corr_thresh
	totalErrorThresh         int32 // total_error_thresh
	localErrorThresh         int32 // local_error_thresh
	directionDeviationThresh int32 // direction_deviation_thresh
	isRegionMinLoudness      int32 // is_region_min_loudness
	minIsSfbs                int   // min_is_sfbs
	leftRightRatioThreshold  int32 // left_right_ratio_threshold
}

// calcSfbMaxScale is the 1:1 port of the file-static calcSfbMaxScale
// (intensity.cpp:196-211): the maximum number of left shifts the largest |line|
// over [l1, l2) tolerates (headroom). Distinct from FDKaacEnc_CalcSfbMaxScaleSpec
// (band_nrg.go) only in taking explicit l1/l2 bounds; the algorithm is identical.
//
//	maxSpc = 0;
//	for (i = l1; i < l2; i++)
//	  maxSpc = fixMax(maxSpc, fixp_abs(mdctSpectrum[i]));
//	sfbMaxScale = (maxSpc == 0) ? (DFRACT_BITS - 2) : CntLeadingZeros(maxSpc) - 1;
func calcSfbMaxScale(mdctSpectrum []int32, l1, l2 int) int {
	var maxSpc int32
	for i := l1; i < l2; i++ {
		tmp := fixabsD(mdctSpectrum[i])
		maxSpc = fixmaxD(maxSpc, tmp)
	}
	if maxSpc == 0 {
		return dfractBits - 2
	}
	return int(fixnormzD(maxSpc)) - 1
}

// initIsParams is the 1:1 port of FDKaacEnc_initIsParams (intensity.cpp:226-234):
// initialise the intensity tuning thresholds. The FL2FXCONST_DBL macros are
// materialised inline at the constant values from intensity.cpp:111-139.
func initIsParams(isParams *intensityParameters) {
	// IS_CORR_THRESH == FL2FXCONST_DBL(0.95f) (intensity.cpp:113). The `f`
	// suffix means the literal is rounded to float32 BEFORE scaling to FIXP_DBL,
	// so it must use fl2fxconstDBLf — the float64 form differs in the low
	// mantissa bits and flips the borderline IS gate / corr-threshold decision.
	isParams.corrThresh = fl2fxconstDBLf(0.95)
	// IS_TOTAL_ERROR_THRESH == FL2FXCONST_DBL(0.04f) (intensity.cpp:118).
	isParams.totalErrorThresh = fl2fxconstDBLf(0.04)
	// IS_LOCAL_ERROR_THRESH == FL2FXCONST_DBL(0.01f) (intensity.cpp:119).
	isParams.localErrorThresh = fl2fxconstDBLf(0.01)
	// IS_DIRECTION_DEVIATION_THRESH == FL2FXCONST_DBL(2.0f/(1<<SF))
	// (intensity.cpp:124-125), SF == 2 -> the literal is 2.0f/4 evaluated in
	// float (the division is a compile-time float32 expression).
	isParams.directionDeviationThresh = fl2fxconstDBLf(2.0 / float32(int(1)<<isDirectionDeviationThreshSF))
	// IS_REGION_MIN_LOUDNESS == FL2FXCONST_DBL(0.1f) (intensity.cpp:129).
	isParams.isRegionMinLoudness = fl2fxconstDBLf(0.1)
	// IS_MIN_SFBS (intensity.cpp:132).
	isParams.minIsSfbs = isMinSfbs
	// IS_LEFT_RIGHT_RATIO_THRESH == FL2FXCONST_DBL(0.7f) (intensity.cpp:139).
	isParams.leftRightRatioThreshold = fl2fxconstDBLf(0.7)
}

// prepareIntensityDecision is the 1:1 port of FDKaacEnc_prepareIntensityDecision
// (intensity.cpp:258-472): compute per-SFB the left/right ratio gate, normalized
// loudness, Pearson channel correlation, the loudness-weighted hrrErr, and the
// preliminary isMask. Outputs hrrErr / isMask / realScale / normSfbLoudness.
// The local working buffers (channelCorr, overallLoudness) are sized exactly as
// the C stack arrays. inv_n / s / scaling follow the C comments verbatim.
func prepareIntensityDecision(
	sfbEnergyLeft, sfbEnergyRight []int32,
	sfbEnergyLdDataLeft, sfbEnergyLdDataRight []int32,
	mdctSpectrumLeft, mdctSpectrumRight []int32,
	isParams *intensityParameters, hrrErr []int32, isMask []int,
	realScale, normSfbLoudness []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	sfbOffset []int32) {

	// temporary variables to compute loudness
	var overallLoudness [maxNoOfGroups]int32
	// temporary variables to compute correlation
	var channelCorr [maxGroupedSfb]int32
	var ml, mr int32
	var prodLr int32
	var squareL, squareR int32
	var tmpL, tmpR int32
	var invN int32

	// FDKmemclear(channelCorr ...), normSfbLoudness, overallLoudness, realScale
	for i := range channelCorr {
		channelCorr[i] = 0
	}
	for i := 0; i < maxGroupedSfb; i++ {
		normSfbLoudness[i] = 0
	}
	for i := range overallLoudness {
		overallLoudness[i] = 0
	}
	for i := 0; i < maxGroupedSfb; i++ {
		realScale[i] = 0
	}

	grpCounter := 0
	for sfboffs := 0; sfboffs < sfbCnt; sfboffs, grpCounter = sfboffs+sfbPerGroup, grpCounter+1 {
		overallLoudness[grpCounter] = 0
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			isValue := sfbEnergyLdDataLeft[sfb+sfboffs] - sfbEnergyLdDataRight[sfb+sfboffs]

			// delimitate intensity scale value to representable range
			// FL2FXCONST_DBL(±60.f/(1<<(REAL_SCALE_SF+LD_DATA_SHIFT)))
			realScale[sfb+sfboffs] = fixMinDBL(
				fl2fxconstDBL(60.0/float64(int(1)<<(realScaleSF+ldDataShift))),
				fixMaxDBL(fl2fxconstDBL(-60.0/float64(int(1)<<(realScaleSF+ldDataShift))),
					isValue))

			sL := fixMax(0, int(fixnormzD(sfbEnergyLeft[sfb+sfboffs]))-1)
			sR := fixMax(0, int(fixnormzD(sfbEnergyRight[sfb+sfboffs]))-1)
			s := (fixMin(sL, sR) >> 2) << 2
			normSfbLoudness[sfb+sfboffs] = sqrtFixp(sqrtFixp(
				((sfbEnergyLeft[sfb+sfboffs]<<uint(s))>>1)+
					((sfbEnergyRight[sfb+sfboffs]<<uint(s))>>1))) >> uint(s>>2)

			overallLoudness[grpCounter] += normSfbLoudness[sfb+sfboffs] >> overallLoudnessSF

			// don't do intensity if panning angle too close to middle, one
			// channel non-existent, or dual mono.
			if (sfbEnergyLeft[sfb+sfboffs] >=
				fMult(isParams.leftRightRatioThreshold, sfbEnergyRight[sfb+sfboffs])) &&
				(fMult(isParams.leftRightRatioThreshold, sfbEnergyLeft[sfb+sfboffs]) <=
					sfbEnergyRight[sfb+sfboffs]) {
				// prevent post processing from considering this SFB for merging
				hrrErr[sfb+sfboffs] = fl2fxconstDBL(1.0 / 8.0)
			}
		}
	}

	grpCounter = 0
	for sfboffs := 0; sfboffs < sfbCnt; sfboffs, grpCounter = sfboffs+sfbPerGroup, grpCounter+1 {
		var invOverallLoudnessSF int
		var invOverallLoudness int32

		if overallLoudness[grpCounter] == 0 {
			invOverallLoudness = 0
			invOverallLoudnessSF = 0
		} else {
			var e int32
			invOverallLoudness, e = fDivNorm(maxvalDBL, overallLoudness[grpCounter])
			// +1: compensate fMultDiv2() in subsequent loop
			invOverallLoudnessSF = int(e) - overallLoudnessSF + 1
		}
		invOverallLoudnessSF = fixMin(fixMax(invOverallLoudnessSF, -(dfractBits-1)), dfractBits-1)

		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			tmp := fMultDiv2((normSfbLoudness[sfb+sfboffs]>>overallLoudnessSF)<<overallLoudnessSF,
				invOverallLoudness)

			normSfbLoudness[sfb+sfboffs] = scaleValue(tmp, int32(invOverallLoudnessSF))

			channelCorr[sfb+sfboffs] = 0

			// inv_n scaled with factor 2 to compensate fMultDiv2() below
			invN = getInvInt(int((sfbOffset[sfb+sfboffs+1] - sfbOffset[sfb+sfboffs]) >> 1))

			if invN > 0 {
				// correlation := Pearson's product-moment coefficient
				ml = 0
				mr = 0
				prodLr = 0
				squareL = 0
				squareR = 0

				sL := calcSfbMaxScale(mdctSpectrumLeft, int(sfbOffset[sfb+sfboffs]), int(sfbOffset[sfb+sfboffs+1]))
				sR := calcSfbMaxScale(mdctSpectrumRight, int(sfbOffset[sfb+sfboffs]), int(sfbOffset[sfb+sfboffs+1]))
				s := fixMin(sL, sR)

				for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
					ml += fMultDiv2(mdctSpectrumLeft[j]<<uint(s), invN)
					mr += fMultDiv2(mdctSpectrumRight[j]<<uint(s), invN)
				}
				ml = fMultDiv2(ml, invN)
				mr = fMultDiv2(mr, invN)

				for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
					tmpL = fMultDiv2(mdctSpectrumLeft[j]<<uint(s), invN) - ml
					tmpR = fMultDiv2(mdctSpectrumRight[j]<<uint(s), invN) - mr

					prodLr += fMultDiv2(tmpL, tmpR)
					squareL += fPow2Div2(tmpL)
					squareR += fPow2Div2(tmpR)
				}
				prodLr = prodLr << 1
				squareL = squareL << 1
				squareR = squareR << 1

				if squareL > 0 && squareR > 0 {
					channelCorrSF := 0

					sL = fixMax(0, int(fixnormzD(squareL))-1)
					sR = fixMax(0, int(fixnormzD(squareR))-1)
					s = ((sL + sR) >> 1) << 1
					sL = fixMin(sL, s)
					sR = s - sL
					tmp = fMult(squareL<<uint(sL), squareR<<uint(sR))
					tmp = sqrtFixp(tmp)

					// numerator and denominator have the same scaling
					if prodLr < 0 {
						var e int32
						channelCorr[sfb+sfboffs], e = fDivNorm(-prodLr, tmp)
						channelCorr[sfb+sfboffs] = -channelCorr[sfb+sfboffs]
						channelCorrSF = int(e)
					} else {
						var e int32
						channelCorr[sfb+sfboffs], e = fDivNorm(prodLr, tmp)
						channelCorrSF = int(e)
					}
					channelCorrSF = fixMin(
						fixMax(channelCorrSF+((sL+sR)>>1), -(dfractBits-1)),
						dfractBits-1)

					if channelCorrSF < 0 {
						channelCorr[sfb+sfboffs] = channelCorr[sfb+sfboffs] >> uint(-channelCorrSF)
					} else {
						// avoid overflows due to limited computational accuracy
						if fAbsDBL(channelCorr[sfb+sfboffs]) > (maxvalDBL >> uint(channelCorrSF)) {
							if channelCorr[sfb+sfboffs] < 0 {
								channelCorr[sfb+sfboffs] = -maxvalDBL
							} else {
								channelCorr[sfb+sfboffs] = maxvalDBL
							}
						} else {
							channelCorr[sfb+sfboffs] = channelCorr[sfb+sfboffs] << uint(channelCorrSF)
						}
					}
				}
			}

			// hrrErr is the (too-little) correlation error weighted with SFB
			// loudness; SFBs with small hrrErr can be merged.
			if hrrErr[sfb+sfboffs] == fl2fxconstDBL(1.0/8.0) {
				continue
			}

			hrrErr[sfb+sfboffs] = fMultDiv2(
				fl2fxconstDBL(0.25)-(channelCorr[sfb+sfboffs]>>2),
				normSfbLoudness[sfb+sfboffs])

			// set IS mask/vector to 1, if correlation is high enough
			if fAbsDBL(channelCorr[sfb+sfboffs]) >= isParams.corrThresh {
				isMask[sfb+sfboffs] = 1
			}
		}
	}
}

// finalizeIntensityDecision is the 1:1 port of FDKaacEnc_finalizeIntensityDecision
// (intensity.cpp:490-579): expand/contract IS regions in each group based on the
// hrrErr/threshold criteria, enforce the minimum region size and minimum loudness,
// and reject SFBs whose IS direction deviates too far. Modifies isMask in place.
func finalizeIntensityDecision(hrrErr []int32, isMask []int, realIsScale, normSfbLoudness []int32,
	isParams *intensityParameters, sfbCnt, sfbPerGroup, maxSfbPerGroup int) {

	isScaleLast := int32(0)
	isStartValueFound := 0

	for sfboffs := 0; sfboffs < sfbCnt; sfboffs += sfbPerGroup {
		startIsSfb := 0
		inIsBlock := 0
		currentIsSfbCount := 0
		overallHrrError := int32(0)
		isRegionLoudness := int32(0)

		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			if isMask[sfboffs+sfb] == 1 {
				if currentIsSfbCount == 0 {
					startIsSfb = sfboffs + sfb
				}
				if isStartValueFound == 0 {
					isScaleLast = realIsScale[sfboffs+sfb]
					isStartValueFound = 1
				}
				inIsBlock = 1
				currentIsSfbCount++
				overallHrrError += hrrErr[sfboffs+sfb] >> (maxSfbPerGroupSF - 3)
				isRegionLoudness += normSfbLoudness[sfboffs+sfb] >> maxSfbPerGroupSF
			} else {
				// based on correlation, IS should not be used -> use it anyway if
				// overall error is below threshold and local error does not exceed
				// threshold; otherwise check if there are enough IS SFBs.
				if inIsBlock != 0 {
					overallHrrError += hrrErr[sfboffs+sfb] >> (maxSfbPerGroupSF - 3)
					isRegionLoudness += normSfbLoudness[sfboffs+sfb] >> maxSfbPerGroupSF

					if (hrrErr[sfboffs+sfb] < (isParams.localErrorThresh >> 3)) &&
						(overallHrrError < (isParams.totalErrorThresh >> maxSfbPerGroupSF)) {
						currentIsSfbCount++
						// overwrite correlation based decision
						isMask[sfboffs+sfb] = 1
					} else {
						inIsBlock = 0
					}
				}
			}
			// check for large direction deviation
			if inIsBlock != 0 {
				if fAbsDBL(isScaleLast-realIsScale[sfboffs+sfb]) <
					(isParams.directionDeviationThresh >>
						(realScaleSF + ldDataShift - isDirectionDeviationThreshSF)) {
					isScaleLast = realIsScale[sfboffs+sfb]
				} else {
					isMask[sfboffs+sfb] = 0
					inIsBlock = 0
					currentIsSfbCount--
				}
			}

			if currentIsSfbCount > 0 && (inIsBlock == 0 || sfb == maxSfbPerGroup-1) {
				// not enough SFBs -> do not use IS
				if currentIsSfbCount < isParams.minIsSfbs ||
					(isRegionLoudness < isParams.isRegionMinLoudness>>maxSfbPerGroupSF) {
					for j := startIsSfb; j <= sfboffs+sfb; j++ {
						isMask[j] = 0
					}
					isScaleLast = 0
					isStartValueFound = 0
					for j := 0; j < startIsSfb; j++ {
						if isMask[j] != 0 {
							isScaleLast = realIsScale[j]
							isStartValueFound = 1
						}
					}
				}
				currentIsSfbCount = 0
				overallHrrError = 0
				isRegionLoudness = 0
			}
		}
	}
}

// IntensityStereoProcessing is the 1:1 port of FDKaacEnc_IntensityStereoProcessing
// (intensity.cpp:614-817): the encode-side intensity-stereo tool. When allowIS,
// it prepares + finalizes the per-SFB IS decision, then for each IS SFB collapses
// the channel pair onto the left channel (in/out of phase), zeroes the right
// spectrum/energies/thresholds, sets isBook/isScale, updates msMask/msDigest, and
// disables PNS on those SFBs. pnsData is the [L,R] pair (a nil pnsData[0] means
// "no PNS active", matching the C `if (pnsData[0])` guard). All inputs/outputs are
// FIXP_DBL/INT slices sized for the grouped-SFB layout.
func IntensityStereoProcessing(
	sfbEnergyLeft, sfbEnergyRight []int32,
	mdctSpectrumLeft, mdctSpectrumRight []int32,
	sfbThresholdLeft, sfbThresholdRight []int32,
	sfbThresholdLdDataRight []int32,
	sfbSpreadEnLeft, sfbSpreadEnRight []int32,
	sfbEnergyLdDataLeft, sfbEnergyLdDataRight []int32,
	msDigest *int, msMask []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int, sfbOffset []int32, allowIS int,
	isBook, isScale []int, pnsData [2]*PNSData) {

	var scale int32
	var lr int32
	var hrrErr [maxGroupedSfb]int32
	var normSfbLoudness [maxGroupedSfb]int32
	var realIsScale [maxGroupedSfb]int32
	var isParams intensityParameters
	var isMask [maxGroupedSfb]int

	// FDKmemclear isBook / isMask / realIsScale / isScale / hrrErr (sfbCnt entries)
	for i := 0; i < sfbCnt; i++ {
		isBook[i] = 0
		isMask[i] = 0
		realIsScale[i] = 0
		isScale[i] = 0
		hrrErr[i] = 0
	}

	if allowIS == 0 {
		return
	}

	initIsParams(&isParams)

	prepareIntensityDecision(
		sfbEnergyLeft, sfbEnergyRight, sfbEnergyLdDataLeft, sfbEnergyLdDataRight,
		mdctSpectrumLeft, mdctSpectrumRight, &isParams, hrrErr[:], isMask[:],
		realIsScale[:], normSfbLoudness[:], sfbCnt, sfbPerGroup, maxSfbPerGroup,
		sfbOffset)

	finalizeIntensityDecision(hrrErr[:], isMask[:], realIsScale[:],
		normSfbLoudness[:], &isParams, sfbCnt, sfbPerGroup, maxSfbPerGroup)

	for sfb := 0; sfb < sfbCnt; sfb += sfbPerGroup {
		for sfboffs := 0; sfboffs < maxSfbPerGroup; sfboffs++ {
			mdctSpecSf := mdctSpecSF

			msMask[sfb+sfboffs] = 0
			if isMask[sfb+sfboffs] == 0 {
				continue
			}

			// FL2FXCONST_DBL(1.0f / 1.5f) (intensity.cpp:672): the `1.0f/1.5f`
			// quotient is a float32 expression (0.6666667f) before the macro
			// widens to double, so it must use fl2fxconstDBLf to match fdk's
			// constant bit-for-bit.
			if (sfbEnergyLeft[sfb+sfboffs] < sfbThresholdLeft[sfb+sfboffs]) &&
				(fMult(fl2fxconstDBLf(float32(1.0)/float32(1.5)), sfbEnergyRight[sfb+sfboffs]) >
					sfbThresholdRight[sfb+sfboffs]) {
				continue
			}
			// NEW: if there is a big-enough IS region, switch off PNS
			if pnsData[0] != nil {
				if pnsData[0].PnsFlag[sfb+sfboffs] != 0 {
					pnsData[0].PnsFlag[sfb+sfboffs] = 0
				}
				if pnsData[1].PnsFlag[sfb+sfboffs] != 0 {
					pnsData[1].PnsFlag[sfb+sfboffs] = 0
				}
			}

			if int(sfbOffset[sfb+sfboffs+1]-sfbOffset[sfb+sfboffs]) > 1<<mdctSpecSf {
				// rare cases where the number of bins in a band is > 64
				mdctSpecSf++
			}

			// scaled with 2 to compensate fMultDiv2() in subsequent loop
			invN := getInvInt(int((sfbOffset[sfb+sfboffs+1] - sfbOffset[sfb+sfboffs]) >> 1))
			sL := calcSfbMaxScale(mdctSpectrumLeft, int(sfbOffset[sfb+sfboffs]), int(sfbOffset[sfb+sfboffs+1]))
			sR := calcSfbMaxScale(mdctSpectrumRight, int(sfbOffset[sfb+sfboffs]), int(sfbOffset[sfb+sfboffs+1]))

			lr = 0
			for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
				lr += fMultDiv2(
					fMultDiv2(mdctSpectrumLeft[j]<<uint(sL), mdctSpectrumRight[j]<<uint(sR)),
					invN)
			}
			lr = lr << 1

			if lr < 0 {
				// OUT OF phase intensity stereo
				s0 := fixMin(sL, sR)
				ed := int32(0)
				for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
					d := ((mdctSpectrumLeft[j] << uint(s0)) >> 1) -
						((mdctSpectrumRight[j] << uint(s0)) >> 1)
					ed += fMultDiv2(d, d) >> uint(mdctSpecSf-1)
				}
				msMask[sfb+sfboffs] = 1
				tmp, s1 := fDivNorm(sfbEnergyLeft[sfb+sfboffs], ed)
				s2 := int(s1) + (2 * s0) - 2 - mdctSpecSf
				if s2&1 != 0 {
					tmp = tmp >> 1
					s2 = s2 + 1
				}
				s2 = (s2 >> 1) + 1 // +1 compensate fMultDiv2() in subsequent loop
				s2 = fixMin(fixMax(s2, -(dfractBits-1)), dfractBits-1)
				scale = sqrtFixp(tmp)
				if s2 < 0 {
					s2 = -s2
					for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
						mdctSpectrumLeft[j] = (fMultDiv2(mdctSpectrumLeft[j], scale) -
							fMultDiv2(mdctSpectrumRight[j], scale)) >> uint(s2)
						mdctSpectrumRight[j] = 0
					}
				} else {
					for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
						mdctSpectrumLeft[j] = (fMultDiv2(mdctSpectrumLeft[j], scale) -
							fMultDiv2(mdctSpectrumRight[j], scale)) << uint(s2)
						mdctSpectrumRight[j] = 0
					}
				}
			} else {
				// IN phase intensity stereo
				s0 := fixMin(sL, sR)
				es := int32(0)
				for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
					s := ((mdctSpectrumLeft[j] << uint(s0)) >> 1) +
						((mdctSpectrumRight[j] << uint(s0)) >> 1)
					es += fMultDiv2(s, s) >> uint(mdctSpecSf-1)
				}
				msMask[sfb+sfboffs] = 0
				tmp, s1 := fDivNorm(sfbEnergyLeft[sfb+sfboffs], es)
				s2 := int(s1) + (2 * s0) - 2 - mdctSpecSf
				if s2&1 != 0 {
					tmp = tmp >> 1
					s2 = s2 + 1
				}
				s2 = (s2 >> 1) + 1 // +1 compensate fMultDiv2() in subsequent loop
				s2 = fixMin(fixMax(s2, -(dfractBits-1)), dfractBits-1)
				scale = sqrtFixp(tmp)
				if s2 < 0 {
					s2 = -s2
					for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
						mdctSpectrumLeft[j] = (fMultDiv2(mdctSpectrumLeft[j], scale) +
							fMultDiv2(mdctSpectrumRight[j], scale)) >> uint(s2)
						mdctSpectrumRight[j] = 0
					}
				} else {
					for j := int(sfbOffset[sfb+sfboffs]); j < int(sfbOffset[sfb+sfboffs+1]); j++ {
						mdctSpectrumLeft[j] = (fMultDiv2(mdctSpectrumLeft[j], scale) +
							fMultDiv2(mdctSpectrumRight[j], scale)) << uint(s2)
						mdctSpectrumRight[j] = 0
					}
				}
			}

			isBook[sfb+sfboffs] = codeBookIsInPhaseNo

			if realIsScale[sfb+sfboffs] < 0 {
				isScale[sfb+sfboffs] = int((((realIsScale[sfb+sfboffs] >> 1) -
					fl2fxconstDBL(0.5/float64(int(1)<<(realScaleSF+ldDataShift+1)))) >>
					uint(dfractBits-1-realScaleSF-ldDataShift-1))) + 1
			} else {
				isScale[sfb+sfboffs] = int((((realIsScale[sfb+sfboffs] >> 1) +
					fl2fxconstDBL(0.5/float64(int(1)<<(realScaleSF+ldDataShift+1)))) >>
					uint(dfractBits-1-realScaleSF-ldDataShift-1)))
			}

			sfbEnergyRight[sfb+sfboffs] = 0
			sfbEnergyLdDataRight[sfb+sfboffs] = fl2fxconstDBL(-1.0)
			sfbThresholdRight[sfb+sfboffs] = 0
			sfbThresholdLdDataRight[sfb+sfboffs] = fl2fxconstDBL(-0.515625)
			sfbSpreadEnRight[sfb+sfboffs] = 0

			*msDigest = MsSome
		}
	}
}
