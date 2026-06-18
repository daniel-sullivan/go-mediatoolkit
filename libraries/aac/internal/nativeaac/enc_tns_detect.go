// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// TNS (Temporal Noise Shaping) encode DECISION driver, ported 1:1 from
// libAACenc/src/aacenc_tns.cpp. This is the encode-side TNS analysis that
// decides whether TNS is applied and produces the on-wire TNS_INFO (filter
// order, direction, length, quantized coefficient indices) that feeds the TNS
// leaf filter (enc_tns.go / tns_apply.go). The chain is:
//
//	FDKaacEnc_TnsDetect (aacenc_tns.cpp:766)
//	  -> FDKaacEnc_MergedAutoCorrelation (aacenc_tns.cpp:619)
//	       -> FDKaacEnc_ScaleUpSpectrum (aacenc_tns.cpp:519)
//	       -> FDKaacEnc_CalcAutoCorrValue (aacenc_tns.cpp:553)
//	       -> FDKaacEnc_AutoCorrNormFac (aacenc_tns.cpp:587)
//	  -> CLpc_AutoToParcor (FDK_lpc.cpp:431, ported in fdk_lpc_parcor.go)
//	  -> FDKaacEnc_Parcor2Index (aacenc_tns.cpp:1164, ported in enc_tns.go)
//
// Every value is an int32 FIXP_DBL / int16 FIXP_LPC(==FIXP_SGL) Q-format
// quantity. The whole computation is pure integer fixed-point arithmetic
// (count-leading-bits, arithmetic shifts, the arm8 fixmul_DD, schur division,
// invSqrtNorm2) — no float, no transcendental — so it is bit-identical
// regardless of vectorization and is fenced only by aacfdk (no aac_strict
// split). The autocorrelation window (TNS_CONFIG.acfWindow) is the integer ROM
// table acfWindowLong/acfWindowShort for AAC-LC (aacenc_tns.cpp:113/118); the
// only float-using initializer (FDKaacEnc_CalcGaussWindow, gauss/exp) is used
// solely for the 480/512 LD granule lengths and is OUT OF SCOPE for AAC-LC —
// the driver consumes acfWindow as an input.
//
// Type mapping on the aarch64 build target: FIXP_DBL == int32, FIXP_LPC ==
// FIXP_SGL == int16, fMult(LONG,LONG) == fixmul_DD == fixmulDDarm8 (keeps bit
// 31), fPow2(LONG) == fixpow2_D == fMultDiv2DD(a,a)<<1.

// TNS encode constants, ported from aacenc_tns.h / psy_const.h.
const (
	tnsMaxOrder      = 12 // TNS_MAX_ORDER (aacenc_tns.h:126)
	maxNumOfFilters  = 2  // MAX_NUM_OF_FILTERS (aacenc_tns.h:128)
	hifilt           = 0  // HIFILT (aacenc_tns.h:130)
	lofilt           = 1  // LOFILT (aacenc_tns.h:131)
	encTransFac      = 8  // TRANS_FAC (psy_const.h:109)
	encShortWindow   = 2  // SHORT_WINDOW (psy_const.h:123)
	encAcfWindowSize = tnsMaxOrder + 3 + 1
)

// TNSParameterTabulated ports TNS_PARAMETER_TABULATED (aacenc_tns.h:133-146):
// the bitrate-dependent tabulated TNS parameters used by the decision.
type TNSParameterTabulated struct {
	FilterEnabled          [maxNumOfFilters]int
	ThreshOn               [maxNumOfFilters]int
	FilterStartFreq        [maxNumOfFilters]int
	TnsLimitOrder          [maxNumOfFilters]int
	TnsFilterDirection     [maxNumOfFilters]int
	AcfSplit               [maxNumOfFilters]int
	TnsTimeResolution      [maxNumOfFilters]int32
	SeperateFiltersAllowed int
}

// TNSConfig ports the TNS_CONFIG fields the decision reads (aacenc_tns.h:148-163).
// acfWindow is the int32 ROM-derived autocorrelation window per filter.
type TNSConfig struct {
	ConfTab      TNSParameterTabulated
	IsLowDelay   int
	TnsActive    int
	MaxOrder     int
	CoefRes      int
	AcfWindow    [maxNumOfFilters][encAcfWindowSize]int32
	LpcStartBand [maxNumOfFilters]int
	LpcStartLine [maxNumOfFilters]int
	LpcStopBand  int
	LpcStopLine  int
}

// TNSSubblockInfo ports TNS_SUBBLOCK_INFO (aacenc_tns.h:165-168).
type TNSSubblockInfo struct {
	TnsActive      [maxNumOfFilters]int
	PredictionGain [maxNumOfFilters]int
}

// TNSInfo ports the TNS_INFO fields written by the decision (aacenc_tns.h:194-207),
// for a single subblock (the decision is invoked per subblock). The decision
// only touches subblock index `subBlockNumber`.
type TNSInfo struct {
	NumOfFilters [encTransFac]int
	CoefRes      [encTransFac]int
	Length       [encTransFac][maxNumOfFilters]int
	Order        [encTransFac][maxNumOfFilters]int
	Direction    [encTransFac][maxNumOfFilters]int
	CoefCompress [encTransFac][maxNumOfFilters]int
	Coef         [encTransFac][maxNumOfFilters][tnsMaxOrder]int
}

// TNSData ports the runtime TNS_DATA fields the decision writes
// (aacenc_tns.h:187-192): filtersMerged plus the per-subblock TNS_SUBBLOCK_INFO
// (long uses index 0; short uses subBlockNumber). The ratioMultTable arrays are
// not read by the decision and are omitted.
type TNSData struct {
	FiltersMerged int
	LongSubBlock  TNSSubblockInfo
	ShortSubBlock [encTransFac]TNSSubblockInfo
}

// scaleUpSpectrum ports the static FDKaacEnc_ScaleUpSpectrum
// (aacenc_tns.cpp:519-538): copy src[startLine:stopLine] into dest scaled up by
// the head room of the largest magnitude in that range; returns that scale.
// fixMax over FIXP_DBL; fixp_abs == fAbsDBL; CountLeadingBits == fNorm.
func scaleUpSpectrum(dest, src []int32, startLine, stopLine int) int32 {
	maxVal := int32(0)
	for i := startLine; i < stopLine; i++ {
		maxVal = fixMaxDBL(maxVal, fAbsDBL(src[i]))
	}
	scale := fNorm(maxVal) // CountLeadingBits == fixnorm_D == fNorm
	for i := startLine; i < stopLine; i++ {
		dest[i] = src[i] << uint(scale)
	}
	return scale
}

// calcAutoCorrValue ports the static FDKaacEnc_CalcAutoCorrValue
// (aacenc_tns.cpp:553-574): autocorrelation at one lag with a per-term right
// shift by `scale`. lag==0 uses fPow2 (== fixpow2_D == fMultDiv2DD(a,a)<<1);
// lag>0 uses fMult (== fixmul_DD == fixmulDDarm8, keeping bit 31).
func calcAutoCorrValue(spectrum []int32, startLine, stopLine, lag int, scale int32) int32 {
	var result int32
	if lag == 0 {
		for i := startLine; i < stopLine; i++ {
			result += (fMultDiv2DD(spectrum[i], spectrum[i]) << 1) >> uint(scale)
		}
	} else {
		for i := startLine; i < (stopLine - lag); i++ {
			result += fixmulDDarm8(spectrum[i], spectrum[i+lag]) >> uint(scale)
		}
	}
	return result
}

// hlmMinNrg is HLM_MIN_NRG == 2^-28 as a FIXP_DBL (aacenc_tns.cpp:589).
var hlmMinNrg = fl2fxconstDBL(0.0000000037252902984619140625)

// autoCorrNormFac ports the static FDKaacEnc_AutoCorrNormFac
// (aacenc_tns.cpp:587-617): 1/energy normalisation factor for an
// autocorrelation, returning the factor and accumulating the exponent into sc.
// invSqrtNorm2 is the ported fixed-point reciprocal-sqrt; fMult ==
// fixmulDDarm8.
func autoCorrNormFac(value, scale, sc int32) (retValue, scOut int32) {
	scOut = sc
	var a, b int32
	if scale >= 0 {
		a = value
		b = hlmMinNrg >> uint(fMin(dfractBits-1, scale))
	} else {
		a = value >> uint(fMin(dfractBits-1, -scale))
		b = hlmMinNrg
	}

	if a > b {
		tmp, shift := invSqrtNorm2(value)
		retValue = fixmulDDarm8(tmp, tmp)
		scOut += 2 * shift
	} else {
		retValue = int32(0x7FFFFFFF) // MAXVAL_DBL
		scOut += scale + 28
	}
	return retValue, scOut
}

// mergedAutoCorrelation ports the static FDKaacEnc_MergedAutoCorrelation
// (aacenc_tns.cpp:619-752): split the [lpcStartLine..lpcStopLine] MDCT range
// into quarters, scale each up, compute energy-normalised+windowed
// autocorrelation, and merge quarters 2/3/4 into rxx2 (lower part stays as
// rxx1). scaleValue == the non-saturating scale.go shift; fMult == fixmulDDarm8.
//
// pScratch is a caller-supplied scratch of length >= 1024 (the
// C_ALLOC_SCRATCH_START pSpectrum[1024]); rxx1/rxx2 are length tnsMaxOrder+1.
func mergedAutoCorrelation(
	spectrum []int32, isLowDelay int,
	acfWindow *[maxNumOfFilters][encAcfWindowSize]int32,
	lpcStartLine *[maxNumOfFilters]int, lpcStopLine, maxOrder int,
	acfSplit *[maxNumOfFilters]int, rxx1, rxx2, pScratch []int32) {

	var idx0, idx1, idx2, idx3, idx4, i int

	if (acfSplit[lofilt] == -1) || (acfSplit[hifilt] == -1) {
		idx0 = lpcStartLine[lofilt]
		i = lpcStopLine - lpcStartLine[lofilt]
		idx1 = idx0 + i/4
		idx2 = idx0 + i/2
		idx3 = idx0 + i*3/4
		idx4 = lpcStopLine
	} else {
		// FDK_ASSERT acfSplit[LOFILT]==1, acfSplit[HIFILT]==3
		i = (lpcStopLine - lpcStartLine[hifilt]) / 3
		idx0 = lpcStartLine[lofilt]
		idx1 = lpcStartLine[hifilt]
		idx2 = idx1 + i
		idx3 = idx2 + i
		idx4 = lpcStopLine
	}

	pSpectrum := pScratch

	sc1 := scaleUpSpectrum(pSpectrum, spectrum, idx0, idx1)
	sc2 := scaleUpSpectrum(pSpectrum, spectrum, idx1, idx2)
	sc3 := scaleUpSpectrum(pSpectrum, spectrum, idx2, idx3)
	sc4 := scaleUpSpectrum(pSpectrum, spectrum, idx3, idx4)

	var nsc1, nsc2, nsc3, nsc4 int32
	for nsc1 = 1; (1 << uint(nsc1)) < (idx1 - idx0); nsc1++ {
	}
	for nsc2 = 1; (1 << uint(nsc2)) < (idx2 - idx1); nsc2++ {
	}
	for nsc3 = 1; (1 << uint(nsc3)) < (idx3 - idx2); nsc3++ {
	}
	for nsc4 = 1; (1 << uint(nsc4)) < (idx4 - idx3); nsc4++ {
	}

	rxx1_0 := calcAutoCorrValue(pSpectrum, idx0, idx1, 0, nsc1)
	rxx2_0 := calcAutoCorrValue(pSpectrum, idx1, idx2, 0, nsc2)
	rxx3_0 := calcAutoCorrValue(pSpectrum, idx2, idx3, 0, nsc3)
	rxx4_0 := calcAutoCorrValue(pSpectrum, idx3, idx4, 0, nsc4)

	if rxx1_0 != 0 {
		scFac1 := int32(-1)
		fac1, sf := autoCorrNormFac(rxx1_0, (-2*sc1)+nsc1, scFac1)
		scFac1 = sf
		rxx1[0] = scaleValue(fixmulDDarm8(rxx1_0, fac1), scFac1)

		if isLowDelay != 0 {
			for lag := 1; lag <= maxOrder; lag++ {
				x1 := calcAutoCorrValue(pSpectrum, idx0, idx1, lag, nsc1)
				rxx1[lag] = fixmulDDarm8(scaleValue(fixmulDDarm8(x1, fac1), scFac1), acfWindow[lofilt][lag])
			}
		} else {
			for lag := 1; lag <= maxOrder; lag++ {
				if (3 * lag) <= maxOrder+3 {
					x1 := calcAutoCorrValue(pSpectrum, idx0, idx1, lag, nsc1)
					rxx1[lag] = fixmulDDarm8(scaleValue(fixmulDDarm8(x1, fac1), scFac1), acfWindow[lofilt][3*lag])
				}
			}
		}
	}

	if !((rxx2_0 == 0) && (rxx3_0 == 0) && (rxx4_0 == 0)) {
		var fac2, fac3, fac4 int32
		var scFac2, scFac3, scFac4 int32

		if rxx2_0 != 0 {
			fac2, scFac2 = autoCorrNormFac(rxx2_0, (-2*sc2)+nsc2, scFac2)
			scFac2 -= 2
		}
		if rxx3_0 != 0 {
			fac3, scFac3 = autoCorrNormFac(rxx3_0, (-2*sc3)+nsc3, scFac3)
			scFac3 -= 2
		}
		if rxx4_0 != 0 {
			fac4, scFac4 = autoCorrNormFac(rxx4_0, (-2*sc4)+nsc4, scFac4)
			scFac4 -= 2
		}

		rxx2[0] = scaleValue(fixmulDDarm8(rxx2_0, fac2), scFac2) +
			scaleValue(fixmulDDarm8(rxx3_0, fac3), scFac3) +
			scaleValue(fixmulDDarm8(rxx4_0, fac4), scFac4)

		for lag := 1; lag <= maxOrder; lag++ {
			x2 := scaleValue(fixmulDDarm8(calcAutoCorrValue(pSpectrum, idx1, idx2, lag, nsc2), fac2), scFac2) +
				scaleValue(fixmulDDarm8(calcAutoCorrValue(pSpectrum, idx2, idx3, lag, nsc3), fac3), scFac3) +
				scaleValue(fixmulDDarm8(calcAutoCorrValue(pSpectrum, idx3, idx4, lag, nsc4), fac4), scFac4)

			rxx2[lag] = fixmulDDarm8(x2, acfWindow[hifilt][lag])
		}
	}
}

// fMultNorm5 ports the inline fMultNorm(f1_m, f1_e, f2_m, f2_e, result_e)
// (fixpoint_math.h:486-496): a normalised multiply re-expressed at a fixed
// result exponent, with saturation.
//
//	m = fMultNorm(f1_m, f2_m, &e);
//	m = scaleValueSaturate(m, e + f1_e + f2_e - result_e);
func fMultNorm5(f1m, f1e, f2m, f2e, resultE int32) int32 {
	m, e := fMultNorm(f1m, f2m)
	return scaleValueSaturate(m, e+f1e+f2e-resultE)
}

// fdkaacEncTnsDetect ports FDKaacEnc_TnsDetect (aacenc_tns.cpp:766-945): the TNS
// decision. It clears the per-subblock TNS_INFO/TNS_SUBBLOCK_INFO, runs the
// merged autocorrelation, derives the higher (and possibly lower) lattice
// filter, quantizes the reflection coefficients, truncates trailing zeros,
// applies the prediction-gain / sum-of-squares thresholds, and (for long
// blocks) optionally merges the two filters. Returns 0 (matching the C).
//
// tnsData / tnsInfo are mutated in place at subblock subBlockNumber. spectrum is
// the MDCT line array (length >= lpcStopLine). pScratch is scratch of length
// >= 1024.
func fdkaacEncTnsDetect(
	tnsData *TNSData, tC *TNSConfig, tnsInfo *TNSInfo,
	sfbCnt int, spectrum []int32, subBlockNumber, blockType int,
	pScratch []int32) int {

	var rxx1 [tnsMaxOrder + 1]int32
	var rxx2 [tnsMaxOrder + 1]int32
	var parcorTmp [tnsMaxOrder]int16

	var tsbi *TNSSubblockInfo
	if blockType == encShortWindow {
		tsbi = &tnsData.ShortSubBlock[subBlockNumber]
	} else {
		tsbi = &tnsData.LongSubBlock
	}

	tnsData.FiltersMerged = 0 // FALSE

	tsbi.TnsActive[hifilt] = 0 // FALSE
	tsbi.PredictionGain[hifilt] = 1000
	tsbi.TnsActive[lofilt] = 0 // FALSE
	tsbi.PredictionGain[lofilt] = 1000

	tnsInfo.NumOfFilters[subBlockNumber] = 0
	tnsInfo.CoefRes[subBlockNumber] = tC.CoefRes
	for i := 0; i < tC.MaxOrder; i++ {
		tnsInfo.Coef[subBlockNumber][hifilt][i] = 0
		tnsInfo.Coef[subBlockNumber][lofilt][i] = 0
	}

	tnsInfo.Length[subBlockNumber][hifilt] = 0
	tnsInfo.Length[subBlockNumber][lofilt] = 0
	tnsInfo.Order[subBlockNumber][hifilt] = 0
	tnsInfo.Order[subBlockNumber][lofilt] = 0

	if (tC.TnsActive != 0) && (tC.MaxOrder > 0) {
		var sumSqrCoef int

		mergedAutoCorrelation(
			spectrum, tC.IsLowDelay, &tC.AcfWindow, &tC.LpcStartLine,
			tC.LpcStopLine, tC.MaxOrder, &tC.ConfTab.AcfSplit, rxx1[:], rxx2[:], pScratch)

		// Higher TNS filter (ParCor) via LeRoux-Gueguen/Schur.
		{
			predictionGainM, predictionGainE := clpcAutoToParcor(rxx2[:], parcorTmp[:], tC.ConfTab.TnsLimitOrder[hifilt])
			tsbi.PredictionGain[hifilt] = int(fMultNorm5(predictionGainM, predictionGainE, 1000, 31, 31))
		}

		// Non-linear quantization.
		idxBuf := tnsInfo.Coef[subBlockNumber][hifilt][:]
		fdkaacEncParcor2Index(parcorTmp[:], idxBuf, tC.ConfTab.TnsLimitOrder[hifilt], tC.CoefRes)

		// Reduce filter order by truncating trailing zeros.
		var i int
		for i = tC.ConfTab.TnsLimitOrder[hifilt] - 1; i >= 0; i-- {
			if tnsInfo.Coef[subBlockNumber][hifilt][i] != 0 {
				break
			}
		}

		tnsInfo.Order[subBlockNumber][hifilt] = i + 1

		sumSqrCoef = 0
		for ; i >= 0; i-- {
			c := tnsInfo.Coef[subBlockNumber][hifilt][i]
			sumSqrCoef += c * c
		}

		tnsInfo.Direction[subBlockNumber][hifilt] = tC.ConfTab.TnsFilterDirection[hifilt]
		tnsInfo.Length[subBlockNumber][hifilt] = sfbCnt - tC.LpcStartBand[hifilt]

		if (tsbi.PredictionGain[hifilt] > tC.ConfTab.ThreshOn[hifilt]) ||
			(sumSqrCoef > (tC.ConfTab.TnsLimitOrder[hifilt]/2 + 2)) {
			tsbi.TnsActive[hifilt] = 1 // TRUE
			tnsInfo.NumOfFilters[subBlockNumber]++

			// Second filter for lower quarter; long windows only.
			if (blockType != encShortWindow) && (tC.ConfTab.FilterEnabled[lofilt] != 0) &&
				(tC.ConfTab.SeperateFiltersAllowed != 0) {

				var predGain int
				{
					predictionGainM, predictionGainE := clpcAutoToParcor(rxx1[:], parcorTmp[:], tC.ConfTab.TnsLimitOrder[lofilt])
					predGain = int(fMultNorm5(predictionGainM, predictionGainE, 1000, 31, 31))
				}

				idxBufLo := tnsInfo.Coef[subBlockNumber][lofilt][:]
				fdkaacEncParcor2Index(parcorTmp[:], idxBufLo, tC.ConfTab.TnsLimitOrder[lofilt], tC.CoefRes)

				for i = tC.ConfTab.TnsLimitOrder[lofilt] - 1; i >= 0; i-- {
					if tnsInfo.Coef[subBlockNumber][lofilt][i] != 0 {
						break
					}
				}
				tnsInfo.Order[subBlockNumber][lofilt] = i + 1

				sumSqrCoef = 0
				for ; i >= 0; i-- {
					c := tnsInfo.Coef[subBlockNumber][lofilt][i]
					sumSqrCoef += c * c
				}

				tnsInfo.Direction[subBlockNumber][lofilt] = tC.ConfTab.TnsFilterDirection[lofilt]
				tnsInfo.Length[subBlockNumber][lofilt] = tC.LpcStartBand[hifilt] - tC.LpcStartBand[lofilt]

				if ((predGain > tC.ConfTab.ThreshOn[lofilt]) &&
					(predGain < (16000 * tC.ConfTab.TnsLimitOrder[lofilt]))) ||
					((sumSqrCoef > 9) &&
						(sumSqrCoef < 22*tC.ConfTab.TnsLimitOrder[lofilt])) {

					tsbi.TnsActive[lofilt] = 1 // TRUE
					sumSqrCoef = 0
					for i = 0; i < tC.ConfTab.TnsLimitOrder[lofilt]; i++ {
						sumSqrCoef += fixpAbsInt(tnsInfo.Coef[subBlockNumber][hifilt][i] -
							tnsInfo.Coef[subBlockNumber][lofilt][i])
					}
					if (sumSqrCoef < 2) &&
						(tnsInfo.Direction[subBlockNumber][lofilt] ==
							tnsInfo.Direction[subBlockNumber][hifilt]) {
						tnsData.FiltersMerged = 1 // TRUE
						tnsInfo.Length[subBlockNumber][hifilt] = sfbCnt - tC.LpcStartBand[lofilt]
						for ; i < tnsInfo.Order[subBlockNumber][hifilt]; i++ {
							if fixpAbsInt(tnsInfo.Coef[subBlockNumber][hifilt][i]) > 1 {
								break
							}
						}
						for i--; i >= 0; i-- {
							if tnsInfo.Coef[subBlockNumber][hifilt][i] != 0 {
								break
							}
						}
						if i < tnsInfo.Order[subBlockNumber][hifilt] {
							tnsInfo.Order[subBlockNumber][hifilt] = i + 1
						}
					} else {
						tnsInfo.NumOfFilters[subBlockNumber]++
					}
				} // filter lower part
				tsbi.PredictionGain[lofilt] = predGain

			} // second filter allowed
		} // predictionGain threshold
	} // maxOrder>0 && tnsActive

	return 0
}
