// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the ENCODE quantizer kernels (quantize.go) so
// the cgo parity oracle in internal/parity_tests/enc-quantize can drive them
// without being in-package. They forward 1:1. These are the scalefactor-
// estimation / quant-loop core: quantize a band for a trial QSS
// (QuantizeSpectrumForParity / QuantizeLinesForParity), inverse-quantize it
// (InvQuantizeLinesForParity), and the two ld-domain cost functions the
// scalefactor search minimises (CalcSfbDistForParity /
// CalcSfbQuantEnergyAndDistForParity). Every value is an int32 FIXP_DBL / int16
// SHORT in Q-format; the parity test compares element-for-element, bit-for-bit.

// QuantizeLinesForParity forwards to fdkaacEncQuantizeLines (quantize.go).
func QuantizeLinesForParity(gain, noOfLines int, mdctSpectrum []int32, quaSpectrum []int16, dZoneQuantEnable bool) {
	fdkaacEncQuantizeLines(gain, noOfLines, mdctSpectrum, quaSpectrum, dZoneQuantEnable)
}

// InvQuantizeLinesForParity forwards to fdkaacEncInvQuantizeLines (quantize.go).
func InvQuantizeLinesForParity(gain, noOfLines int, quantSpectrum []int16, mdctSpectrum []int32) {
	fdkaacEncInvQuantizeLines(gain, noOfLines, quantSpectrum, mdctSpectrum)
}

// QuantizeSpectrumForParity forwards to fdkaacEncQuantizeSpectrum (quantize.go).
func QuantizeSpectrumForParity(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int,
	mdctSpectrum []int32, globalGain int, scalefactors []int, quantizedSpectrum []int16,
	dZoneQuantEnable bool) {
	fdkaacEncQuantizeSpectrum(sfbCnt, maxSfbPerGroup, sfbPerGroup, sfbOffset,
		mdctSpectrum, globalGain, scalefactors, quantizedSpectrum, dZoneQuantEnable)
}

// CalcSfbDistForParity forwards to fdkaacEncCalcSfbDist (quantize.go).
func CalcSfbDistForParity(mdctSpectrum []int32, quantSpectrum []int16, noOfLines, gain int, dZoneQuantEnable bool) int32 {
	return fdkaacEncCalcSfbDist(mdctSpectrum, quantSpectrum, noOfLines, gain, dZoneQuantEnable)
}

// CalcSfbQuantEnergyAndDistForParity forwards to
// fdkaacEncCalcSfbQuantEnergyAndDist (quantize.go), returning (en, dist).
func CalcSfbQuantEnergyAndDistForParity(mdctSpectrum []int32, quantSpectrum []int16, noOfLines, gain int) (en, dist int32) {
	return fdkaacEncCalcSfbQuantEnergyAndDist(mdctSpectrum, quantSpectrum, noOfLines, gain)
}

// QuantRomForParity returns the four narrowed quantizer ROM tables (quantize's
// aacEnc_rom subset) so the oracle can verify the Go transcription bit-for-bit
// against the genuine vendored tables.
func QuantRomForParity() (mTab34, quantTableQ, quantTableE []int16, mTab43 []int32) {
	mTab34 = make([]int16, len(fdkaacEncMTab34))
	copy(mTab34, fdkaacEncMTab34[:])
	quantTableQ = make([]int16, len(fdkaacEncQuantTableQ))
	copy(quantTableQ, fdkaacEncQuantTableQ[:])
	quantTableE = make([]int16, len(fdkaacEncQuantTableE))
	copy(quantTableE, fdkaacEncQuantTableE[:])
	mTab43 = make([]int32, len(fdkaacEncMTab43Elc))
	copy(mTab43, fdkaacEncMTab43Elc[:])
	return mTab34, quantTableQ, quantTableE, mTab43
}
