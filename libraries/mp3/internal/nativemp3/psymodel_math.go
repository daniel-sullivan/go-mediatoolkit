// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// Math helpers the psychoacoustic model pulls from LAME's util.c and util.h.
// These are part of the encoder support surface, translated 1:1 so the model
// computes identical values to the C. Each names its C counterpart.

// freq2bark converts a frequency in Hz to the bark scale (util.c:281,
// freq2bark). The C body is
//
//	freq = freq * 0.001;
//	return 13.0 * atan(.76 * freq) + 3.5 * atan(freq * freq / (7.5 * 7.5));
//
// where 0.001/.76/13.0/3.5/7.5 are DOUBLE literals and atan is the double libm
// call, so the WHOLE right-hand side evaluates in double and narrows to FLOAT
// only on the return. The sole FLOAT-precision step is `freq * freq` (both
// operands FLOAT -> a single-rounded float32 product) before the `/ (7.5*7.5)`
// promotes it to double. The earlier port computed the bark sum in float32,
// which diverges bval by enough to shift the s3 spreading table by hundreds of
// ULP and drift every multi-granule masking threshold. Mirror the C precision:
// keep the body in double, with freq and freq*freq narrowed to float32 exactly
// where the C does.
func freq2bark(freq float32) float32 {
	// input: freq in hz  output: barks
	if freq < 0 {
		freq = 0
	}
	// freq = freq * 0.001: 0.001 is a DOUBLE literal, so freq promotes to double,
	// the product is double, and the assignment back to FLOAT freq narrows once.
	// Multiplying by a float32 0.001 instead is off by a ULP and shifts every bark
	// value (and thus the s3 spreading table) — match the C's double multiply.
	freq = float32(float64(freq) * 0.001)
	ff := float64(freq)
	ffSq := float64(psMul(freq, freq)) // freq*freq is a FLOAT product in the C
	bark := 13.0*math.Atan(0.76*ff) + 3.5*math.Atan(ffSq/(7.5*7.5))
	return float32(bark)
}

// athFormulaGB is LAME's ATHformula_GB (util.c, the Painter & Spanias / Bouvigne
// threshold-of-hearing formula). value tunes the HF tail. The body computes the
// sum in DOUBLE exactly as the C (pow/exp return double, the literals are
// double), narrowing once to the FLOAT result. The C oracle compiles with
// -ffp-contract=off, so the double products/sums round separately: the four
// terms and their left-to-right accumulation ((t1 - t2) + t3) + t4 are routed
// through the //go:noinline ps*D64 helpers so Go's arm64 backend cannot fuse a
// double `a +/- b*c` into a single-rounded FMA, matching the oracle bit-for-bit.
// The f/=1000 and the Max/Min clamps are float32, mirroring `FLOAT f`.
func athFormulaGB(f, value, fMin, fMax float32) float32 {
	// the following Hack allows to ask for the lowest value
	if f < -0.3 {
		f = 3410
	}
	f /= 1000 // convert to khz
	f = maxF32(fMin, f)
	f = minF32(fMax, f)

	ff := float64(f)
	t1 := psMulD64(3.640, math.Pow(ff, -0.8))
	t2 := psMulD64(6.800, math.Exp(psMulD64(-0.6, math.Pow(psSubD64(ff, 3.4), 2.0))))
	t3 := psMulD64(6.000, math.Exp(psMulD64(-0.15, math.Pow(psSubD64(ff, 8.7), 2.0))))
	t4 := psMulD64(psMulD64(psAddD64(0.6, psMulD64(0.04, float64(value))), 0.001), math.Pow(ff, 4.0))
	ath := psAddD64(psAddD64(psSubD64(t1, t2), t3), t4)
	return float32(ath)
}

// athFormula is LAME's ATHformula (util.c:250): dispatches on cfg.ATHtype to a
// parameterisation of athFormulaGB. Mirrors the C switch verbatim, default and
// all cases.
func athFormula(cfg *SessionConfig, f float32) float32 {
	var ath float32
	switch cfg.ATHtype {
	case 0:
		ath = athFormulaGB(f, 9, 0.1, 24.0)
	case 1:
		ath = athFormulaGB(f, -1, 0.1, 24.0) // over sensitive, should probably be removed
	case 2:
		ath = athFormulaGB(f, 0, 0.1, 24.0)
	case 3:
		ath = athFormulaGB(f, 1, 0.1, 24.0) + 6 // modification of GB formula by Roel
	case 4:
		ath = athFormulaGB(f, cfg.ATHcurve, 0.1, 24.0)
	case 5:
		ath = athFormulaGB(f, cfg.ATHcurve, 3.41, 16.1)
	default:
		ath = athFormulaGB(f, 0, 0.1, 24.0)
	}
	return ath
}

// fastLog10 is LAME's FAST_LOG10 macro in its non-USE_FAST_LOG form
// (util.h:101, #define FAST_LOG10(x) log10(x)). The vendored build does not
// define USE_FAST_LOG, so this is plain log10. LAME computes it in float32
// (FLOAT) with a double-returning libm call.
func fastLog10(x float32) float32 {
	return float32(math.Log10(float64(x)))
}

// fastLog10X is LAME's FAST_LOG10_X macro, non-USE_FAST_LOG form (util.h:103,
// #define FAST_LOG10_X(x,y) (log10(x)*(y))). The C `log10` is the DOUBLE libm
// call (x promotes to double, result double) and `*(y)` promotes y to double, so
// the whole product is evaluated in double and only narrowed to FLOAT on use.
// vbrpsy_mask_add's sole caller does `(int)(FAST_LOG10_X(ratio, 16.0f))`, so the
// product must stay double until the truncation: rounding log10 to float32 first
// can flip the integer table2 index at a boundary and diverge the masking
// threshold. Keep the multiply in double (psMulD64), narrowing only at return.
func fastLog10X(x, y float32) float32 {
	return float32(psMulD64(math.Log10(float64(x)), float64(y)))
}

// log10Var is LAME's LOG10 macro used as a runtime FLOAT (e.g. 10.0f*LOG10 in
// pecalc_*). util.h:72 defines LOG10 as 2.30258509299404568402.
const log10Var = float32(log10Const)

// minF32 / maxF32 mirror LAME's Min/Max macros (util.h:91) for float32 — the
// model uses them on FLOAT operands throughout. Kept as the C ternary
// (strict less-than / greater-than) so NaN/tie behaviour matches.
func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxF32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}
