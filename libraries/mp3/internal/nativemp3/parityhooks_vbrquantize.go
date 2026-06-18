// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the vbrquantize-leaf parity oracle
// (internal/parity_tests/vbrquantize-leaf).
//
// The VBR scalefactor-search leaf kernels (vbrquantize.go: vecMaxC,
// findLowestScalefac, k344, calcSfbNoiseX34, triCalcSfbNoiseX34, calcScalefac,
// guessScalefacX34, findScalefacX34) are unexported 1:1 translations of LAME's
// `static`/`inline static` functions and have no place in the public surface.
// The wrappers below exist solely so the parity suite — which lives in its own
// package because it compiles the vendored vbrquantize.c oracle — can assert the
// Go port matches the genuine C bit-for-bit under the mp3_strict build. They are
// mp3lame-gated like the slice they expose; a bare `go build ./...` never
// compiles the LGPL-fenced VBR quantizer.

// FillVbrQuantizeTables runs InitQuantizePvtTables (pow43 / pow20 / ipow20 /
// adj43) then InitVbrQuantizeTables (adj43asm, the TAKEHIRO_IEEE754_HACK branch)
// so the leaf kernels' table lookups resolve, matching the oracle's
// oracle_fill_tables (which drives the genuine iteration_init table fill).
func FillVbrQuantizeTables() {
	InitQuantizePvtTables()
	InitVbrQuantizeTables()
}

// VecMaxC exposes vecMaxC (vbrquantize.c:116).
func VecMaxC(xr34 []float32, bw int) float32 { return vecMaxC(xr34, uint(bw)) }

// FindLowestScalefac exposes find_lowest_scalefac (vbrquantize.c:148).
func FindLowestScalefac(xr34 float32) uint8 { return findLowestScalefac(xr34) }

// K344 exposes k_34_4 (vbrquantize.c:169). x is quantized in place to l3.
func K344(x [4]float64) [4]int {
	var xx [4]float64 = x
	var l3 [4]int
	k344(&xx, &l3)
	return l3
}

// CalcSfbNoiseX34 exposes calc_sfb_noise_x34 (vbrquantize.c:218).
func CalcSfbNoiseX34(xr, xr34 []float32, bw int, sf uint8) float32 {
	return calcSfbNoiseX34(xr, xr34, uint(bw), sf)
}

// TriCalcSfbNoiseX34 exposes tri_calc_sfb_noise_x34 (vbrquantize.c:278). It
// seeds a fresh 256-entry cache so the result depends only on the inputs.
func TriCalcSfbNoiseX34(xr, xr34 []float32, l3Xmin float32, bw int, sf uint8) uint8 {
	var didIt [256]calcNoiseCache
	return triCalcSfbNoiseX34(xr, xr34, l3Xmin, uint(bw), sf, didIt[:])
}

// CalcScalefac exposes calc_scalefac (vbrquantize.c:317).
func CalcScalefac(l3Xmin float32, bw int) int { return calcScalefac(l3Xmin, bw) }

// GuessScalefacX34 exposes guess_scalefac_x34 (vbrquantize.c:324).
func GuessScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	return guessScalefacX34(xr, xr34, l3Xmin, uint(bw), sfMin)
}

// FindScalefacX34 exposes find_scalefac_x34 (vbrquantize.c:347).
func FindScalefacX34(xr, xr34 []float32, l3Xmin float32, bw int, sfMin uint8) uint8 {
	return findScalefacX34(xr, xr34, l3Xmin, uint(bw), sfMin)
}
