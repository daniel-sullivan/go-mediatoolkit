// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Fixed-point multiply primitives, a 1:1 port of the generic-C variants in
// libFDK/include/fixmul.h. libfdk-aac is a fixed-point codec: FIXP_DBL is a
// signed 32-bit fraction (LONG) and FIXP_SGL a signed 16-bit fraction (SHORT)
// (common_fix.h:170-171). These are pure integer kernels — the int64
// intermediate and arithmetic shift make them bit-identical regardless of
// vectorization, so no aac_strict FP gating is required (cf. the integer-kernel
// note in nativeaac.go). Go's `>>` on a signed integer is an arithmetic shift,
// matching the C `>>` on the signed INT64/LONG used here.

// fl2fxconstDBL is the 1:1 port of the FL2FXCONST_DBL(val) macro
// (common_fix.h:191), the compile-time conversion of a real constant in
// (-1.0, 1.0] to a Q1.31 FIXP_DBL, with round-half-away-from-zero and
// saturation to ±MAXVAL_DBL. DFRACT_FIX_SCALE == (double)(MAXVAL_DBL+1) == 2^31;
// the C cast (LONG) truncates toward zero, matching Go's int32(float64). Used to
// materialise the FIXP_DBL ROM tables (e.g. ldCoeff) bit-for-bit, exactly as the
// C compiler would fold them.
//
//	#define FL2FXCONST_DBL(val) (FIXP_DBL)(
//	  (val) >= 0 ? ( val*SCALE+0.5 >= MAXVAL_DBL ? MAXVAL_DBL
//	                                             : (LONG)(val*SCALE+0.5) )
//	             : ( val*SCALE-0.5 <= MINVAL_DBL ? MINVAL_DBL
//	                                             : (LONG)(val*SCALE-0.5) ) )
func fl2fxconstDBL(val float64) int32 {
	const scale = 2147483648.0 // DFRACT_FIX_SCALE == 2^31
	const maxvalF = 2147483647.0
	const minvalF = -2147483648.0
	if val >= 0 {
		t := val*scale + 0.5
		if t >= maxvalF {
			return 0x7FFFFFFF
		}
		return int32(t)
	}
	t := val*scale - 0.5
	if t <= minvalF {
		return -0x80000000
	}
	return int32(t)
}

// fl2fxconstDBLf is fl2fxconstDBL for a single-precision (float32) source
// literal: the C FL2FXCONST_DBL(x) macro casts (double)(val), so when the call
// site writes a float literal with an `f` suffix (e.g. FL2FXCONST_DBL(0.4f) in
// channel_map.cpp / adj_thr.cpp) the constant is FIRST rounded to float, THEN
// widened to double before scaling — which differs in the low mantissa bits from
// the double literal. This helper reproduces that exactly by taking a float32
// argument and widening it, so the materialised FIXP_DBL is bit-identical to the
// f-suffixed C macro.
func fl2fxconstDBLf(val float32) int32 {
	return fl2fxconstDBL(float64(val))
}

// fl2fxconstSGL is the 1:1 port of the FL2FXCONST_SGL(val) macro
// (common_fix.h:179), the FIXP_SGL (Q1.15 int16) counterpart of fl2fxconstDBL.
// FRACT_FIX_SCALE == 2^(FRACT_BITS-1) == 2^15; (SHORT) truncates toward zero,
// matching Go's int16(float64); MAXVAL_SGL == 0x7FFF, MINVAL_SGL == -0x8000.
// Used to materialise the FIXP_SGL ROM tables (e.g. ldCoeff under LDCOEFF_16BIT)
// bit-for-bit on the aarch64 target.
func fl2fxconstSGL(val float64) int16 {
	const scale = 32768.0 // FRACT_FIX_SCALE == 2^15
	const maxvalF = 32767.0
	const minvalF = -32768.0
	if val >= 0 {
		t := val*scale + 0.5
		if t >= maxvalF {
			return 0x7FFF
		}
		return int16(t)
	}
	t := val*scale - 0.5
	if t <= minvalF {
		return -0x8000
	}
	return int16(t)
}

// fl2fxconstSGLf is fl2fxconstSGL for a single-precision (float32) source
// literal: the C FL2FXCONST_SGL(x) macro casts (double)(val), so when the call
// site writes an f-suffixed float literal (e.g. FL2FXCONST_SGL(0.5011932025f) in
// sbr_rom.cpp) the constant is FIRST rounded to float, THEN widened to double
// before scaling. This helper reproduces that exactly by taking a float32
// argument and widening it.
func fl2fxconstSGLf(val float32) int16 {
	return fl2fxconstSGL(float64(val))
}

// fMultDiv2DD multiplies two FIXP_DBL fractions, returning the product scaled
// down by 2 (i.e. >> 1 relative to fMultDD). C counterpart: fixmuldiv2_DD,
// libFDK/include/fixmul.h:131; the fMultDiv2(LONG, LONG) overload
// (common_fix.h:248) forwards here.
//
//	inline LONG fixmuldiv2_DD(const LONG a, const LONG b) {
//	  return (LONG)((((INT64)a) * b) >> 32);
//	}
func fMultDiv2DD(a, b int32) int32 {
	return int32((int64(a) * int64(b)) >> 32)
}

// fMultDD is the GENERIC fixmuldiv2_DD(a,b)<<1 == ((a*b)>>32)<<1 product. NOTE:
// on the build platform (aarch64, where FDK_archdef.h:117-118 forces __arm__ and
// :165-166 forces __ARM_ARCH_8__) the C fMult(LONG,LONG) == fixmul_DD takes the
// ARMv8 override (arm/fixmul_arm.h:177-186) `smull; asr #31` == ((a*b)>>31) ==
// fixmulDDarm8 (block_switch.go), which differs from this generic form by one LSB
// on products with a set bit 31. So callers porting a literal C `fMult(a,b)` must
// use fixmulDDarm8, NOT fMultDD. fMultDD is retained as the backend for the C
// helpers that genuinely use the GENERIC path on this target — notably fPow2(a)
// == fixpow2_D(a) == fixpow2div2_D(a)<<1 (fixmul.h:282, no arm override), used as
// `fMultDD(x,x)` for a square.
func fMultDD(a, b int32) int32 {
	return fMultDiv2DD(a, b) << 1
}

// fMultDiv2DS multiplies a FIXP_DBL by a FIXP_SGL fraction, returning the
// product scaled down by 2. C counterpart: the fMultDiv2(LONG, SHORT) overload
// (common_fix.h:247) == fixmuldiv2_DS. On the build platform (aarch64 ==
// __ARM_ARCH_8__) the arm header defines FUNCTION_fixmuldiv2_SD (fixmul_arm.h:
// 157-162) but NOT FUNCTION_fixmuldiv2_DS, so fixmuldiv2_DS resolves to the
// generic form (fixmul.h:183-194): since FUNCTION_fixmuldiv2_SD IS defined it
// takes the `return fixmuldiv2_SD(b, a)` branch (fixmul.h:188), and the arm
// fixmuldiv2_SD widens the SHORT (b<<16) and calls fixmuldiv2_DD — i.e.
// fixmuldiv2_DD(b<<16, a). fixmuldiv2_DD is commutative in the int64 product,
// so this equals fMultDiv2DD(a, b<<16).
//
//	// generic fixmul.h:183
//	inline LONG fixmuldiv2_DS(const LONG a, const SHORT b)
//	  return fixmuldiv2_SD(b, a);          // FUNCTION_fixmuldiv2_SD defined
//	// arm fixmul_arm.h:159
//	inline INT fixmuldiv2_SD(const SHORT a, const INT b)
//	  return fixmuldiv2_DD((INT)(a << 16), b);
func fMultDiv2DS(a int32, b int16) int32 {
	return fMultDiv2DD(int32(b)<<16, a)
}

// fMultDS multiplies a FIXP_DBL by a FIXP_SGL fraction at full scale. C
// counterpart: the fMult(LONG, SHORT) overload (common_fix.h:240) == fixmul_DS.
// The arm header does NOT define FUNCTION_fixmul_DS/SD, so fixmul_DS resolves to
// the generic form (fixmul.h:236-243): FUNCTION_fixmul_SD is not defined, so it
// takes the `return fixmuldiv2_DS(a, b) << 1` branch (fixmul.h:242).
//
//	inline LONG fixmul_DS(const LONG a, const SHORT b) {
//	  return fixmuldiv2_DS(a, b) << 1;
//	}
func fMultDS(a int32, b int16) int32 {
	return fMultDiv2DS(a, b) << 1
}
