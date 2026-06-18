//go:build flac_strict

package nativeflac

import "math"

// Strict-mode float32 helpers for window.c parity.
//
// libFLAC's window generators are compiled (in the parity oracle) with
// `-ffp-contract=off`, so every `a + b*c` in the float32 polynomial
// evaluations is two separately rounded operations: a float32 multiply
// producing a rounded product, then a float32 add. Go's arm64 backend
// auto-fuses `a + b*c` into FMADDS, which would diverge in the last
// ULP. Routing each multiply through a //go:noinline helper makes the
// product an opaque function-call return value that Go's SSA cannot
// pattern-match back into a fused multiply-add. (Same technique as the
// opus port's fma_strict.go.) The f32add / f32sub helpers are likewise
// //go:noinline so each `+`/`-` is a single round-to-nearest-even
// float32 add, matching clang under -ffp-contract=off.

//go:noinline
func f32mul(a, b float32) float32 { return a * b }

//go:noinline
func f32div(a, b float32) float32 { return a / b }

//go:noinline
func f32add(a, b float32) float32 { return a + b }

//go:noinline
func f32sub(a, b float32) float32 { return a - b }

//go:noinline
func f32abs(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

// cosfStrict mirrors the parity oracle's cosf shim. The oracle defines
//
//	#define cosf(x) ((float)cos((double)(x)))
//
// so the call site cosf(2.0f * M_PI * n / N) computes its argument in
// DOUBLE precision (M_PI is a double constant, which promotes the whole
// 2.0f*M_PI*n/N chain to double), feeds that full double to the double
// cos kernel, and only narrows the RESULT to float. The argument is
// never rounded to float32 — the (double)(x) cast in the macro is a
// no-op on an already-double expression. We therefore compute
// float32(math.Cos(angle)) on the full-precision double angle; narrowing
// the angle to float32 first would drift by up to 1 ULP and break the
// cosf-based windows (hann, blackman, hamming, nuttall, flattop,
// kaiser_bessel, bartlett_hann, the tukey family).
func cosfStrict(angle float64) float32 {
	return float32(math.Cos(angle))
}

// expDouble mirrors C's double-precision exp() used by the gauss
// window (the argument and result are double; the caller casts to
// float32).
func expDouble(x float64) float64 { return math.Exp(x) }
