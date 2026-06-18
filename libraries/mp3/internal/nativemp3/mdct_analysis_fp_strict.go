// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

// Strict-mode float32 helpers for the LAME MDCT analysis filterbank + FFT.
//
// The parity oracle compiles newmdct.c / fft.c with -ffp-contract=off
// -fno-vectorize, so every `a + b*c` in the filterbank, MDCT, and Hartley
// butterflies is two separately rounded float operations: a float32 multiply
// producing a rounded product, then a float32 add. Go's arm64 backend
// auto-fuses `a + b*c` into an FMA (FMADDS), which diverges from clang in the
// last ULP. Routing each multiply through a //go:noinline helper makes the
// product an opaque function-call return that Go's SSA cannot pattern-match
// back into a fused multiply-add; the add/sub helpers are likewise
// //go:noinline so each individual `+` / `-` is a single
// round-to-nearest-even float32 operation. (Same technique as the huffman
// dequant, opus, and flac ports.)
//
// These mirror the package-wide f32* helpers but carry mdct-specific names so
// the LGPL-fenced encoder slice keeps its own clearly-scoped FP surface.

//go:noinline
func mdctMul(a, b float32) float32 { return a * b }

//go:noinline
func mdctAdd(a, b float32) float32 { return a + b }

//go:noinline
func mdctSub(a, b float32) float32 { return a - b }

// The helpers below model C's *usual arithmetic conversions*, which are a
// SEPARATE source of last-ULP divergence from FMA contraction. newmdct.c mixes
// FLOAT (float32) variables with `double` literals — SQRT2 (util.h:79,
// 1.41421356237309504880) and the short-MDCT scale constants
// (2.069978111953089e-11, 1.907525191737280e-11 / ...281e-11, 0.5,
// 0.86602540378443870761). In C a `float * double` (or `float * double *
// double + float`) subexpression is evaluated entirely in DOUBLE precision and
// rounded to float32 only once, when the result is stored back into a FLOAT
// lvalue. -ffp-contract=off does NOT change this: the intermediate products are
// never rounded to float32. Routing each such statement through one of these
// double-bearing helpers reproduces C's single trailing round, where the plain
// float32 mdctMul chain would round every intermediate. (Same idea as
// psymodel's psMulD for `SQRT2 * gi[k]` in fft.c.)

// mdctMulD computes d*x in double precision narrowed to float32 — C's
// `xr * SQRT2` / `SQRT2 * (a-b)` (newmdct.c:559,628,...). One trailing round.
//
//go:noinline
func mdctMulD(d float64, x float32) float32 { return float32(d * float64(x)) }

// mdctMulDSub computes d*x - g in double precision narrowed to float32 — C's
// `a[23] = xr * SQRT2 - a[7]` (newmdct.c:562). The subtraction happens in
// double (g promoted), rounding only on the float32 store.
//
//go:noinline
func mdctMulDSub(d float64, x, g float32) float32 {
	return float32(d*float64(x) - float64(g))
}

// mdctMulFD computes x*d in double precision narrowed to float32 — C's
// `tc0 = (...) * 2.069978111953089e-11` (newmdct.c:849). x is a fully-rounded
// float32 subexpression; the double scale then rounds once on store.
//
//go:noinline
func mdctMulFD(x float32, d float64) float32 { return float32(float64(x) * d) }

// mdctMulFDAdd computes x*d + g in double precision narrowed to float32 — C's
// `inout[0] = tc1 * 1.907525191737280e-11 + tc0` (newmdct.c:852). Both the
// product and the add stay in double; only the store rounds.
//
//go:noinline
func mdctMulFDAdd(x float32, d float64, g float32) float32 {
	return float32(float64(x)*d + float64(g))
}

// mdctMulFDDAdd computes x*d1*d2 + g in double precision narrowed to float32 —
// C's `ts1 = ts1 * 0.5 * 1.907525191737281e-11 + ts0` (newmdct.c:856). The two
// scale multiplies and the add are a single double chain; the store rounds once.
//
//go:noinline
func mdctMulFDDAdd(x float32, d1, d2 float64, g float32) float32 {
	return float32(float64(x)*d1*d2 + float64(g))
}

// mdctMulFDDSub computes x*d1*d2 - g in double precision narrowed to float32 —
// C's `tc1 = tc1 * 0.5 * 1.907525191737281e-11 - tc0` (newmdct.c:860).
//
//go:noinline
func mdctMulFDDSub(x float32, d1, d2 float64, g float32) float32 {
	return float32(float64(x)*d1*d2 - float64(g))
}

// mdctMulFDD computes x*d1*d2 in double precision narrowed to float32 — C's
// `tc2 = tc2 * 0.86602540378443870761 * 1.907525191737281e-11`
// (newmdct.c:855). The two double scales chain in double; the store rounds once.
//
//go:noinline
func mdctMulFDD(x float32, d1, d2 float64) float32 {
	return float32(float64(x) * d1 * d2)
}
