// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && !mp3_strict

package nativemp3

// Default-mode float32 helpers for the LAME MDCT analysis filterbank + FFT.
//
// The production build inlines the plain float32 operators and lets the
// backend fuse multiply-adds (FMADDS) and vectorize freely. The analysis
// spectrum it produces is within PSNR noise of the reference but is not
// guaranteed bit-exact in every ULP; the strict build
// (mdct_analysis_fp_strict.go) is what the parity suite asserts against.

func mdctMul(a, b float32) float32 { return a * b }

func mdctAdd(a, b float32) float32 { return a + b }

func mdctSub(a, b float32) float32 { return a - b }

// The double-bearing helpers below model C's usual arithmetic conversions for
// the newmdct.c statements that mix FLOAT (float32) variables with `double`
// literals (SQRT2 and the short-MDCT scale constants): the products stay in
// double and round to float32 only on the final store. Like psymodel's psMulD,
// the default build keeps the same double-rounded semantics — the double
// multiply IS the meaning of the C expression, not a strict-only parity
// contrivance — so these bodies are identical to the strict file's (minus the
// //go:noinline, which only matters for the FMA-prone float32 mdctMul chain).

// mdctMulD computes d*x in double precision narrowed to float32 (`xr * SQRT2`).
func mdctMulD(d float64, x float32) float32 { return float32(d * float64(x)) }

// mdctMulDSub computes d*x - g in double precision narrowed to float32
// (`xr * SQRT2 - a[7]`).
func mdctMulDSub(d float64, x, g float32) float32 {
	return float32(d*float64(x) - float64(g))
}

// mdctMulFD computes x*d in double precision narrowed to float32
// (`(...) * 2.069978111953089e-11`).
func mdctMulFD(x float32, d float64) float32 { return float32(float64(x) * d) }

// mdctMulFDAdd computes x*d + g in double precision narrowed to float32
// (`tc1 * 1.907525191737280e-11 + tc0`).
func mdctMulFDAdd(x float32, d float64, g float32) float32 {
	return float32(float64(x)*d + float64(g))
}

// mdctMulFDDAdd computes x*d1*d2 + g in double precision narrowed to float32
// (`ts1 * 0.5 * 1.907525191737281e-11 + ts0`).
func mdctMulFDDAdd(x float32, d1, d2 float64, g float32) float32 {
	return float32(float64(x)*d1*d2 + float64(g))
}

// mdctMulFDDSub computes x*d1*d2 - g in double precision narrowed to float32
// (`tc1 * 0.5 * 1.907525191737281e-11 - tc0`).
func mdctMulFDDSub(x float32, d1, d2 float64, g float32) float32 {
	return float32(float64(x)*d1*d2 - float64(g))
}

// mdctMulFDD computes x*d1*d2 in double precision narrowed to float32
// (`tc2 * 0.86602540378443870761 * 1.907525191737281e-11`).
func mdctMulFDD(x float32, d1, d2 float64) float32 {
	return float32(float64(x) * d1 * d2)
}
