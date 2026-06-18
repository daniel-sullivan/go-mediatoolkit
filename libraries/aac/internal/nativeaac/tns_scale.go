// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Integer scale primitives backing the TNS-filter head-room computation,
// ported 1:1 from the vendored FDK-AAC FDK library. These are pure integer
// kernels (count-leading-bits / max-exponent over a fixed-point vector) and
// are bit-identical regardless of vectorization, so they carry only the
// `aacfdk` fence with no aac_strict split.
//
// Type mapping (matches the scalar config.h baseline the cgo oracle compiles):
// FIXP_DBL/LONG/INT are 32-bit two's-complement (int32), DFRACT_BITS == 32.
//
// dfractBits (DFRACT_BITS == 32) and fixnormzD (the fixnormz_D count-leading-
// redundant-bits kernel) are shared package primitives declared by the inverse-
// quantizer area; this file reuses them rather than redeclaring.

// fMax returns the larger of a and b. Ported 1:1 from fMax(INT,INT) ->
// fixmax_I -> fixmax in common_fix.h:407 / fixminmax.h:126.
func fMax(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}

// fMin returns the smaller of a and b. Ported 1:1 from fMin(INT,INT) ->
// fixmin_I -> fixmin in common_fix.h:408 / fixminmax.h.
func fMin(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// getScalefactor returns the maximum possible scale factor (head room) for a
// FIXP_DBL input vector. Ported 1:1 from getScalefactor(const FIXP_DBL*, INT)
// in libFDK/src/scale.cpp:689-701.
//
// If all data is 0xFFFFFFFF or 0x00000000 the function returns 31. You can
// skip data normalization only if the return value is 0.
func getScalefactor(vector []int32, length int32) int32 {
	var maxVal int32
	for i := length; i != 0; i-- {
		temp := vector[length-i]
		// maxVal |= temp ^ (temp >> (DFRACT_BITS - 1))  — arithmetic right
		// shift folds negatives onto their bit-complement so |abs| head
		// room is captured for both signs.
		maxVal |= temp ^ (temp >> (dfractBits - 1))
	}
	return fMax(0, fixnormzD(maxVal)-1)
}
