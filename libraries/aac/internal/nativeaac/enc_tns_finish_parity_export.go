// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the "TNS finish" batch — the ENCODE-side TNS
// configuration init (enc_tns_init.go), channel-pair sync (enc_tns_sync.go),
// the in-place TNS analysis filter (enc_tns_encode.go), and the LPC lattice
// helpers it calls (fdk_lpc_analysis.go) — so the cgo parity oracle in
// internal/parity_tests/enc-tns-finish can drive them without being in-package.
// Every value is int32 FIXP_DBL / int16 FIXP_LPC fixed-point — the oracle
// asserts EXACT integer equality vs the genuine vendored FDK symbols.

// EncInitTnsConfiguration forwards to initTnsConfiguration (enc_tns_init.go):
// the AAC-LC FDKaacEnc_InitTnsConfiguration. tC is filled in place from the
// PSY_CONFIGURATION pC.
func EncInitTnsConfiguration(bitRate, sampleRate, channels, blockType, granuleLength,
	isLowDelay, ldSbrPresent int, tC *TNSConfig, pC *PsyConfiguration,
	active, useTnsPeak int) int {
	return int(initTnsConfiguration(bitRate, sampleRate, channels, blockType,
		granuleLength, isLowDelay, ldSbrPresent, tC, pC, active, useTnsPeak))
}

// EncTnsSync forwards to tnsSync (enc_tns_sync.go): the channel-pair TNS filter
// synchronisation. dest/infoDest are mutated in place.
func EncTnsSync(tnsDataDest, tnsDataSrc *TNSData, tnsInfoDest, tnsInfoSrc *TNSInfo,
	blockTypeDest, blockTypeSrc int, tC *TNSConfig) {
	tnsSync(tnsDataDest, tnsDataSrc, tnsInfoDest, tnsInfoSrc, blockTypeDest, blockTypeSrc, tC)
}

// EncTnsEncode forwards to tnsEncode (enc_tns_encode.go): the in-place TNS
// analysis filter over the MDCT spectrum. spectrum is mutated; returns the C
// return (1 if inactive, else 0).
func EncTnsEncode(tnsInfo *TNSInfo, tnsData *TNSData, numOfSfb int, tC *TNSConfig,
	lowPassLine int, spectrum []int32, subBlockNumber, blockType int) int {
	return tnsEncode(tnsInfo, tnsData, numOfSfb, tC, lowPassLine, spectrum, subBlockNumber, blockType)
}

// EncClpcParcorToLpc forwards to clpcParcorToLpc (fdk_lpc_analysis.go): reflection
// (ParCor) -> direct-form LPC. workBuffer must have length >= numOfCoeff;
// lpcCoeff is written in place. Returns the LPC exponent.
func EncClpcParcorToLpc(reflCoeff, lpcCoeff []int16, numOfCoeff int, workBuffer []int32) int {
	return clpcParcorToLpc(reflCoeff, lpcCoeff, numOfCoeff, workBuffer)
}

// EncClpcAnalysis forwards to clpcAnalysis (fdk_lpc_analysis.go): the in-place
// FIR analysis filter. signal[0:signalSize] and filtState are mutated.
// filtStateIndex is nil for the NULL path the TNS encode uses.
func EncClpcAnalysis(signal []int32, signalSize int, lpcCoeff []int16, lpcCoeffE,
	order int, filtState []int32, filtStateIndex *int) {
	clpcAnalysis(signal, signalSize, lpcCoeff, lpcCoeffE, order, filtState, filtStateIndex)
}

// EncGetTnsMaxBands forwards to getTnsMaxBands (enc_tns_rom.go).
func EncGetTnsMaxBands(sampleRate, granuleLength, isShortBlock int) int {
	return getTnsMaxBands(sampleRate, granuleLength, isShortBlock)
}
