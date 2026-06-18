// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDKaacEnc_TnsEncode, ported 1:1 from libAACenc/src/aacenc_tns.cpp
// (:1051-1091): apply the TNS analysis (residual) filter to the MDCT spectrum in
// place using the quantized coefficients the decision (FDKaacEnc_TnsDetect)
// produced. For each active filter it dequantizes the on-wire coefficient
// indices to ParCor (FDKaacEnc_Index2Parcor / fdkaacEncIndex2Parcor), converts
// ParCor to direct-form LPC (CLpc_ParcorToLpc / clpcParcorToLpc), then runs the
// FIR analysis filter (CLpc_Analysis / clpcAnalysis) over the [startLine,
// stopLine) range — rewriting spectrum[] in place exactly as the C does.
//
// Pure integer fixed-point (int32 FIXP_DBL spectrum, int16 FIXP_LPC coeffs),
// aacfdk-fenced, no aac_strict split.

// tnsEncode ports FDKaacEnc_TnsEncode(TNS_INFO *tnsInfo, TNS_DATA *tnsData, INT
// numOfSfb, const TNS_CONFIG *tC, INT lowPassLine, FIXP_DBL *spectrum, INT
// subBlockNumber, INT blockType). spectrum is mutated in place over the filter
// ranges. Returns 1 when TNS is inactive for this subblock (no filtering), else
// 0. numOfSfb / lowPassLine are accepted for signature parity (the C does not
// read them in this path).
func tnsEncode(tnsInfo *TNSInfo, tnsData *TNSData, numOfSfb int, tC *TNSConfig,
	lowPassLine int, spectrum []int32, subBlockNumber, blockType int) int {

	_ = numOfSfb
	_ = lowPassLine

	// if the higher filter is inactive for this subblock, do nothing
	if (blockType == encShortWindow && tnsData.ShortSubBlock[subBlockNumber].TnsActive[hifilt] == 0) ||
		(blockType != encShortWindow && tnsData.LongSubBlock.TnsActive[hifilt] == 0) {
		return 1
	}

	var startLine, stopLine int
	if tnsData.FiltersMerged != 0 {
		startLine = tC.LpcStartLine[lofilt]
	} else {
		startLine = tC.LpcStartLine[hifilt]
	}
	stopLine = tC.LpcStopLine

	for i := 0; i < tnsInfo.NumOfFilters[subBlockNumber]; i++ {
		var lpcCoeff [tnsMaxOrder]int16
		var workBuffer [tnsMaxOrder]int32
		var parcorTmp [tnsMaxOrder]int16

		order := tnsInfo.Order[subBlockNumber][i]

		// FDKaacEnc_Index2Parcor(coef[subBlock][i], parcor_tmp, order, coefRes)
		fdkaacEncIndex2Parcor(tnsInfo.Coef[subBlockNumber][i][:], parcorTmp[:], order, tC.CoefRes)

		// lpcGainFactor = CLpc_ParcorToLpc(parcor_tmp, LpcCoeff, order, workBuffer)
		lpcGainFactor := clpcParcorToLpc(parcorTmp[:], lpcCoeff[:], order, workBuffer[:])

		// FDKmemclear(workBuffer, TNS_MAX_ORDER * sizeof(FIXP_DBL));
		for k := range workBuffer {
			workBuffer[k] = 0
		}
		// CLpc_Analysis(&spectrum[startLine], stopLine - startLine, LpcCoeff,
		//               lpcGainFactor, order, workBuffer, NULL);
		clpcAnalysis(spectrum[startLine:], stopLine-startLine, lpcCoeff[:],
			lpcGainFactor, order, workBuffer[:], nil)

		// update for second filter
		startLine = tC.LpcStartLine[lofilt]
		stopLine = tC.LpcStartLine[hifilt]
	}

	return 0
}
