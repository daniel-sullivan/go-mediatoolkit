// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// This file exposes thin exported wrappers around the unexported
// inverse-quantization kernels (invquant.go) so the cgo parity oracle in
// internal/parity_tests/inverse-quant can drive them without being in-package.
// The wrappers add no logic: each forwards 1:1 to the ported kernel under test
// against its vendored C counterpart (libAACdec/src/block.cpp + block.h). They
// exist solely for the parity harness — the production decode path uses the
// unexported forms directly.

// EvaluatePower43 wraps evaluatePower43 (block.h:247): compute
// 2^(lsb/4) * value^(4/3) for the single quantized line *pValue, writing the
// mantissa back to *value and returning its exponent. lsb must be < 4.
func EvaluatePower43(value *int32, lsb uint32) int32 {
	return evaluatePower43(value, lsb)
}

// GetScaleFromValue wraps getScaleFromValue (block.h:283): determine the
// required shift scale for the given quantized value and lsb (returns 0 for a
// zero value).
func GetScaleFromValue(value int32, lsb uint32) int32 {
	return getScaleFromValue(value, lsb)
}

// MaxabsD wraps maxabs_D (block.cpp:471): the maximum absolute spectral-line
// value across the first noLines lines.
func MaxabsD(spectralCoefficient []int32, noLines int) int32 {
	return maxabsD(spectralCoefficient, noLines)
}

// InverseQuantizeBand wraps inverseQuantizeBand (block.cpp:436): inverse
// quantize one scalefactor band of noLines lines in place using the ROM rows
// for lsb (selected by the caller) and the band headroom scale. inverseQuantTab
// is the full InverseQuantTable; mantissaTab / exponentTab are the lsb-indexed
// rows of MantissaTable / ExponentTable.
func InverseQuantizeBand(spectrum []int32, inverseQuantTab []int32, mantissaTab []int32, exponentTab []int8, noLines int, scale int32) {
	inverseQuantizeBand(spectrum, inverseQuantTab, mantissaTab, exponentTab, noLines, scale)
}

// InverseQuantTableRow returns the full ported InverseQuantTable (aac_rom.cpp:109),
// the (4/3)-power ROM both EvaluatePower43 and InverseQuantizeBand index. The
// parity harness uses it to pass the same table the C oracle reads.
func InverseQuantTableRow() []int32 {
	return inverseQuantTable[:]
}

// MantissaTableRow returns row lsb (0..3) of MantissaTable (aac_rom.cpp:205),
// the Q1.31 gain mantissas for 2^(lsb/4).
func MantissaTableRow(lsb int) []int32 {
	return mantissaTable[lsb][:]
}

// ExponentTableRow returns row lsb (0..3) of ExponentTable (aac_rom.cpp:219),
// the signed exponents paired with MantissaTableRow.
func ExponentTableRow(lsb int) []int8 {
	return exponentTable[lsb][:]
}
