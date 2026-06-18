// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame && mp3_strict

package nativemp3

import "math"

// Strict-mode float32 helpers for LAME's psychoacoustic model.
//
// In the parity oracle psymodel.c / fft.c are compiled with
// -ffp-contract=off, so every `a + b*c` is two separately rounded float32
// operations (a rounded product, then a rounded add). Go's backend auto-fuses
// `a + b*c` into a single-rounded FMA on arm64, which diverges in the last
// ULP. Routing each float32 multiply through a //go:noinline helper makes the
// product an opaque function-call return that Go's SSA cannot pattern-match
// back into a fused multiply-add. The +/-/÷ helpers are likewise //go:noinline
// so each individual operation is a single round-to-nearest-even float32 step,
// matching clang under -ffp-contract=off -fno-vectorize. Same technique as the
// opus and flac ports and the huffman slice in this package (the names here
// carry a `ps` prefix to coexist with huffman's f32* helpers).

//go:noinline
func psMul(a, b float32) float32 { return a * b }

//go:noinline
func psAdd(a, b float32) float32 { return a + b }

//go:noinline
func psSub(a, b float32) float32 { return a - b }

//go:noinline
func psDiv(a, b float32) float32 { return a / b }

// psMulD computes d*x where d is a double-precision scalar (e.g. LAME's SQRT2,
// a double literal) and x is a float32. C's usual arithmetic conversions
// promote x to double, multiply in DOUBLE precision, then narrow the result to
// float32 on assignment to a FLOAT lvalue — so this is a single double-rounded
// product, NOT a float32 multiply. (fft.c:95-96, `f3 = SQRT2 * gi[k3]`.) This
// is orthogonal to FMA contraction: it must round in double regardless of
// -ffp-contract, so the same helper serves the strict path.
//
//go:noinline
func psMulD(d float64, x float32) float32 { return float32(d * float64(x)) }

// psFma computes a + b*c as two separately rounded float32 operations,
// matching -ffp-contract=off. The multiply goes through psMul (opaque to the
// fuser) and the add through psAdd; the strict build therefore rounds the
// product before adding, never emitting a fused FMADDS.
func psFma(a, b, c float32) float32 { return psAdd(a, psMul(b, c)) }

// psFmaSub computes a - b*c as two separately rounded float32 operations.
func psFmaSub(a, b, c float32) float32 { return psSub(a, psMul(b, c)) }

// psCosf is the single-precision cosine shim. The platform's cosf is neither
// correctly-rounded nor portable, so both the strict Go build and the cgo
// oracle compute it as the double kernel narrowed to float32
// (float32(math.Cos(float64(x)))); the oracle #defines cosf(x) to
// ((float)cos((double)(x))). See the SKILL "Transcendentals" rule.
func psCosf(x float32) float32 { return float32(math.Cos(float64(x))) }

// psPowf is the single-precision power shim used by quantize_pvt.c's ATHmdct /
// athAdjust (powf(10.f, ...)). The platform's powf is neither correctly-rounded
// nor portable, so both the strict Go build and the cgo oracle compute it as
// the double kernel narrowed to float32 (float32(math.Pow(float64(x),
// float64(y)))); the oracle #defines powf(x,y) to ((float)pow((double)x,
// (double)y)). See the SKILL "Transcendentals" rule. It is //go:noinline only
// for symmetry with the other ps* helpers; the narrowing is the contract.
//
//go:noinline
func psPowf(x, y float32) float32 { return float32(math.Pow(float64(x), float64(y))) }

// psMulD64 / psAddD64 / psSubD64 are //go:noinline DOUBLE-precision arithmetic
// helpers. Go's arm64 backend fuses `a + b*c` / `a - b*c` into a single-rounded
// FMA for float64 too, but the cgo oracle compiles util.c's ATHformula_GB with
// -ffp-contract=off so each double product/sum rounds separately. Routing the
// double terms through these opaque helpers keeps the strict build's float64
// arithmetic unfused, matching the oracle bit-for-bit before the final narrow
// to float32.
//
//go:noinline
func psMulD64(a, b float64) float64 { return a * b }

//go:noinline
func psAddD64(a, b float64) float64 { return a + b }

//go:noinline
func psSubD64(a, b float64) float64 { return a - b }
