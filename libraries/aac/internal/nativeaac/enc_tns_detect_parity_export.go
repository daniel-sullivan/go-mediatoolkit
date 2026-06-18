// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the ENCODE TNS decision driver
// (enc_tns_detect.go) and the LeRoux-Gueguen ParCor analysis
// (fdk_lpc_parcor.go) so the cgo parity oracle in
// internal/parity_tests/enc-tns-full can drive them without being in-package.
// Every value is int32 FIXP_DBL / int16 FIXP_LPC fixed-point — no float — so the
// oracle asserts EXACT integer equality vs the genuine vendored FDK kernels.

// EncTnsDetect forwards to fdkaacEncTnsDetect (enc_tns_detect.go): the TNS
// decision. tnsData / tnsInfo are mutated in place at subBlockNumber; spectrum
// is the MDCT line array; pScratch is scratch of length >= 1024. Returns 0 (as
// the C FDKaacEnc_TnsDetect). Re-exported only for the parity harness.
func EncTnsDetect(tnsData *TNSData, tC *TNSConfig, tnsInfo *TNSInfo,
	sfbCnt int, spectrum []int32, subBlockNumber, blockType int, pScratch []int32) int {
	return fdkaacEncTnsDetect(tnsData, tC, tnsInfo, sfbCnt, spectrum, subBlockNumber, blockType, pScratch)
}

// EncClpcAutoToParcor forwards to clpcAutoToParcor (fdk_lpc_parcor.go): the
// LeRoux-Gueguen/Schur reflection-coefficient analysis. acorr is mutated in
// place (matching the C); returns the reflection coefficients plus the
// prediction gain (mantissa, exponent).
func EncClpcAutoToParcor(acorr []int32, numOfCoeff int) (reflCoeff []int16, predictionGainM, predictionGainE int32) {
	reflCoeff = make([]int16, numOfCoeff)
	predictionGainM, predictionGainE = clpcAutoToParcor(acorr, reflCoeff, numOfCoeff)
	return
}

// EncMergedAutoCorrelation forwards to mergedAutoCorrelation
// (enc_tns_detect.go): the quarter-split, energy-normalised, windowed
// autocorrelation feeding the TNS decision. rxx1/rxx2 (length tnsMaxOrder+1) are
// written in place; pScratch is scratch of length >= 1024.
func EncMergedAutoCorrelation(
	spectrum []int32, isLowDelay int,
	acfWindow *[maxNumOfFilters][encAcfWindowSize]int32,
	lpcStartLine *[maxNumOfFilters]int, lpcStopLine, maxOrder int,
	acfSplit *[maxNumOfFilters]int, rxx1, rxx2, pScratch []int32) {
	mergedAutoCorrelation(spectrum, isLowDelay, acfWindow, lpcStartLine,
		lpcStopLine, maxOrder, acfSplit, rxx1, rxx2, pScratch)
}

// EncTnsMaxOrder / EncTnsAcfWindowSize / EncTnsMaxNumOfFilters publish the TNS
// dimension constants so the parity package can size its fixed arrays to match.
const (
	EncTnsMaxOrder      = tnsMaxOrder
	EncTnsAcfWindowSize = encAcfWindowSize
	EncTnsMaxNumFilters = maxNumOfFilters
	EncTnsTransFac      = encTransFac
	EncTnsShortWindow   = encShortWindow
	EncTnsLongWindow    = 0 // LONG_WINDOW (psy_const.h:121)
	EncTnsHifilt        = hifilt
	EncTnsLofilt        = lofilt
)
