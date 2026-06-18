// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Thin exported wrappers around the ENCODE band/line-energy kernels
// (band_nrg.go) and the fixed-point log2 helpers (fixpoint_log2.go) so the cgo
// parity oracle in internal/parity_tests/enc-psy-model can drive them without
// being in-package. They forward 1:1, converting the oracle's []int32/[]int
// slices into the in-package call. These are the SFB-energy core of the psy
// model: the per-SFB headroom (CalcSfbMaxScaleSpec), the block-floating-point
// SFB energies and their ldData form (CheckBandEnergyOptim / CalcBandEnergy-
// OptimLong / CalcBandEnergyOptimShort), and the M/S mid/side energies
// (CalcBandNrgMSOpt) — all int32 FIXP_DBL with carried block exponents.

// CalcSfbMaxScaleSpec forwards to FDKaacEnc_CalcSfbMaxScaleSpec (band_nrg.go).
func CalcSfbMaxScaleSpec(mdctSpectrum []int32, bandOffset, sfbMaxScaleSpec []int, numBands int) {
	fdkaacEncCalcSfbMaxScaleSpec(mdctSpectrum, bandOffset, sfbMaxScaleSpec, numBands)
}

// CheckBandEnergyOptim forwards to FDKaacEnc_CheckBandEnergyOptim (band_nrg.go)
// and returns the rescaled maxNrg.
func CheckBandEnergyOptim(mdctSpectrum, bandEnergy, bandEnergyLdData []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands, minSpecShift int) int32 {
	return fdkaacEncCheckBandEnergyOptim(mdctSpectrum, bandEnergy, bandEnergyLdData,
		sfbMaxScaleSpec, bandOffset, numBands, minSpecShift)
}

// CalcBandEnergyOptimLong forwards to FDKaacEnc_CalcBandEnergyOptimLong
// (band_nrg.go) and returns the applied shiftBits.
func CalcBandEnergyOptimLong(mdctSpectrum, bandEnergy, bandEnergyLdData []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands int) int {
	return fdkaacEncCalcBandEnergyOptimLong(mdctSpectrum, bandEnergy, bandEnergyLdData,
		sfbMaxScaleSpec, bandOffset, numBands)
}

// CalcBandEnergyOptimShort forwards to FDKaacEnc_CalcBandEnergyOptimShort
// (band_nrg.go).
func CalcBandEnergyOptimShort(mdctSpectrum, bandEnergy []int32,
	sfbMaxScaleSpec, bandOffset []int, numBands int) {
	fdkaacEncCalcBandEnergyOptimShort(mdctSpectrum, bandEnergy, sfbMaxScaleSpec, bandOffset, numBands)
}

// CalcBandNrgMSOpt forwards to FDKaacEnc_CalcBandNrgMSOpt (band_nrg.go).
func CalcBandNrgMSOpt(mdctSpectrumLeft, mdctSpectrumRight []int32,
	sfbMaxScaleSpecLeft, sfbMaxScaleSpecRight, bandOffset []int, numBands int,
	bandEnergyMid, bandEnergySide []int32,
	calcLdDataFlag int, bandEnergyMidLdData, bandEnergySideLdData []int32) {
	fdkaacEncCalcBandNrgMSOpt(mdctSpectrumLeft, mdctSpectrumRight,
		sfbMaxScaleSpecLeft, sfbMaxScaleSpecRight, bandOffset, numBands,
		bandEnergyMid, bandEnergySide, calcLdDataFlag, bandEnergyMidLdData, bandEnergySideLdData)
}

// CalcLdData forwards to the fLog2-based CalcLdData (fixpoint_log2.go); exported
// so the oracle can cross-check the log2 helper in isolation.
func CalcLdData(op int32) int32 { return calcLdData(op) }

// LdDataVector forwards to ldDataVector (fixpoint_log2.go).
func LdDataVector(src, dst []int32, n int) { ldDataVector(src, dst, n) }

// Fl2fxconstDBLForParity forwards to fl2fxconstDBL (fixmul.go) so the oracle can
// cross-check the FL2FXCONST_DBL compile-time folding against the genuine C
// macro.
func Fl2fxconstDBLForParity(val float64) int32 { return fl2fxconstDBL(val) }

// LdCoeffForParity returns the Go-embedded ldCoeff ROM (fixpoint_log2.go) so the
// oracle can verify it bit-for-bit against the genuine ldCoeff[]. On the aarch64
// target ldCoeff is the FIXP_SGL (int16) variant (LDCOEFF_16BIT).
func LdCoeffForParity() []int16 {
	out := make([]int16, len(ldCoeff))
	copy(out, ldCoeff[:])
	return out
}

// Fl2fxconstSGLForParity forwards to fl2fxconstSGL (fixmul.go) so the oracle can
// cross-check the FL2FXCONST_SGL folding against the genuine C macro.
func Fl2fxconstSGLForParity(val float64) int16 { return fl2fxconstSGL(val) }
