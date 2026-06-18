// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Complex multiply + narrowing primitives the fixed-point FFT (dit_fft, fft.cpp)
// is built on, a 1:1 port of the generic-C / aarch64 variants the vendored
// build selects on this platform. libfdk-aac is a fixed-point codec: FIXP_DBL is
// a signed 32-bit Q1.31 fraction and FIXP_SGL a signed 16-bit Q1.15 fraction
// (common_fix.h:170-171). These are pure integer kernels — bit-identical
// regardless of vectorization — so no aac_strict FP gating is required.
//
// Active config note (FX parity convention)
// ------------------------------------------
// On the build platform (darwin/arm64 == __aarch64__) FDK_archdef.h:117 defines
// __arm__ and :166 defines __ARM_ARCH_8__, selecting the
// `__arm__ && __ARM_ARCH_8__` branch (FDK_archdef.h:182-187): ARCH_PREFER_MULT_
// 32x16 and SINETABLE_16BIT. Therefore:
//
//   - FIXP_STP == FIXP_SPK, i.e. the trig ROM holds *16-bit* packed
//     (FIXP_SGL re, FIXP_SGL im) pairs and STC(a) == FX_DBL2FXCONST_SGL(a)
//     narrows the Q31 hex constant to Q15 (FDK_archdef.h:248-251).
//   - The cplxMultDiv2 overload that dit_fft's trig multiply resolves to is the
//     32x16X2 generic (cplx_mul.h:124-129): the arm header only overrides the
//     32x32X2 form for ARM_ARCH_8 (arm/cplx_mul_arm.h:106-114), leaving 32x16X2
//     to the generic fallback.
//   - The 32x32X2 overload (block 2's STC(0x5a82799a) DBL multiply) IS the arm
//     ARCH_8 inline-asm body (arm/cplx_mul_arm.h:116-148): it accumulates the
//     two products in a 64-bit register and shifts >>32 ONCE, which is
//     bit-identical to fMultDiv2(DBL,DBL) of each product summed because each
//     product already fits the high 32 bits — see cplxMultDiv2DBL below for the
//     faithful per-product form, which the parity oracle (the genuine C) agrees
//     with bit-for-bit.
//
// The oracle compiles the genuine vendored C under these same defines, so the Go
// side must reproduce exactly this active variant.

// fixSTP is the in-RAM trig-ROM element: a packed pair of FIXP_SGL (Q1.15)
// re/im values. C counterpart: FIXP_SPK (common_fix.h:419-431), the type
// FIXP_STP aliases under SINETABLE_16BIT (FDK_archdef.h:250).
type fixSTP struct {
	re int16
	im int16
}

// stcNarrow narrows a Q1.31 hex constant to a FIXP_SGL (Q1.15) exactly as the
// C macro FX_DBL2FXCONST_SGL (common_fix.h:160-166), the body of STC(a) under
// SINETABLE_16BIT (FDK_archdef.h:251). DFRACT_BITS==32, FRACT_BITS==16, so the
// shift is (32-16-1)==15 and the overflow threshold is (1<<16)-1==0xFFFF; the
// saturation value is (1<<15)-1==0x7FFF.
//
//	#define FX_DBL2FXCONST_SGL(val)                                  \
//	  ((((((val) >> 15) + 1) > 0xFFFF) && ((LONG)(val) > 0))         \
//	     ? (FIXP_SGL)(SHORT)0x7FFF                                   \
//	     : (FIXP_SGL)(SHORT)((((val) >> 15) + 1) >> 1))
func stcNarrow(val int32) int16 {
	t := (val >> 15) + 1
	if t > 0xFFFF && val > 0 {
		return int16(0x7FFF)
	}
	return int16(t >> 1)
}

// cplxMultDiv2SGL is the 32x16X2 complex multiply: (a_Re,a_Im) FIXP_DBL times
// (b_Re,b_Im) FIXP_SGL, each product scaled down by 2. On the build platform
// (aarch64 == __ARM_ARCH_8__) this is the arm overload
// (arm/cplx_mul_arm.h:174-197), which under __ARM_ARCH_8__ WIDENS the FIXP_SGL
// twiddles to FIXP_DBL via FX_SGL2FX_DBL (b<<16, common_fix.h:218-219) and then
// delegates to the 32x32X2 DBL form (accumulate-in-64 then a single >>32):
//
//	FIXP_DBL b_re = FX_SGL2FX_DBL(b_Re);   // b_Re << 16
//	FIXP_DBL b_im = FX_SGL2FX_DBL(b_Im);   // b_Im << 16
//	cplxMultDiv2(c_Re, c_Im, a_Re, a_Im, b_re, b_im);  // 32x32X2
//
// This is NOT the generic 32x16X2 (two separate fMultDiv2(DBL,SGL) per output)
// — the combined 64-bit accumulate then single >>32 can differ by one ULP — so
// the port reproduces the ACTIVE arm-ARCH_8 variant, matching the genuine C the
// oracle compiles on this platform.
func cplxMultDiv2SGL(aRe, aIm int32, bRe, bIm int16) (cRe, cIm int32) {
	return cplxMultDiv2DBL(aRe, aIm, int32(bRe)<<16, int32(bIm)<<16)
}

// cplxMultDiv2DBL is the 32x32X2 complex multiply: (a_Re,a_Im) times
// (b_Re,b_Im), both FIXP_DBL, each product scaled down by 2. On aarch64 (the
// build platform) this is the inline-asm overload (arm/cplx_mul_arm.h:116-148),
// which under __ARM_ARCH_8__ accumulates the two int64 products in a single
// 64-bit register and shifts >>32 ONCE:
//
//	smull  tmp1 = a_Re * b_Re       (64-bit)
//	smsubl tmp1 -= a_Im * b_Im      (64-bit accumulate)
//	asr    tmp1 >>= 32
//	smull  tmp2 = a_Re * b_Im
//	smaddl tmp2 += a_Im * b_Re
//	asr    tmp2 >>= 32
//
// This is NOT the same as the generic cplx_mul.h:187-192 per-product form
// (shift each >>32 then add/sub) — the borrow/carry out of the discarded low 32
// bits can differ by one ULP — so the port reproduces the ACTIVE asm variant
// (accumulate-in-64 then single >>32), matching the genuine C the oracle
// compiles on this platform.
func cplxMultDiv2DBL(aRe, aIm, bRe, bIm int32) (cRe, cIm int32) {
	cRe = int32((int64(aRe)*int64(bRe) - int64(aIm)*int64(bIm)) >> 32)
	cIm = int32((int64(aRe)*int64(bIm) + int64(aIm)*int64(bRe)) >> 32)
	return
}

// cplxMultSGL is the full-scale 32x16X2 complex multiply: (a_Re,a_Im) FIXP_DBL
// times (b_Re,b_Im) FIXP_SGL, no Div2 scaling. C counterpart: the
// cplxMult(FIXP_DBL*, FIXP_DBL*, FIXP_DBL, FIXP_DBL, FIXP_SGL, FIXP_SGL)
// 32x16X2 overload (cplx_mul.h:221-226). Unlike cplxMultDiv2, the arm header
// (cplx_mul_arm.h) overrides ONLY the Div2 forms — it defines no full-scale
// cplxMult — so cplxMult resolves to this generic body on every platform:
//
//	*c_Re = fMult(a_Re, b_Re) - fMult(a_Im, b_Im);
//	*c_Im = fMult(a_Re, b_Im) + fMult(a_Im, b_Re);
//
// where fMult(LONG, SHORT) == fixmul_DS == fMultDS. The FIXP_STP/FIXP_WTP
// (SPK) overloads cplxMult(...,w) (cplx_mul.h:238) forward here with
// w.v.re/w.v.im, so dct_IV's twiddle/sin_twiddle multiplies land here.
func cplxMultSGL(aRe, aIm int32, bRe, bIm int16) (cRe, cIm int32) {
	cRe = fMultDS(aRe, bRe) - fMultDS(aIm, bIm)
	cIm = fMultDS(aRe, bIm) + fMultDS(aIm, bRe)
	return
}
