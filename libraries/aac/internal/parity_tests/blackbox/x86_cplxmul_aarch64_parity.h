/* x86_cplxmul_aarch64_parity.h — black-box parity shim, force-included into the
 * upstream libfdk-aac build ON x86_64 ONLY (see run.sh).
 *
 * WHY THIS EXISTS
 * ---------------
 * The pure-Go nativeaac port is a 1:1 port of libFDK's AArch64 fixed-point
 * path. libFDK's fixed-point arithmetic is NOT bit-identical across CPU
 * architectures: AArch64 ships hand-written kernels (libFDK/include/arm/ headers,
 * gated on __ARM_ARCH_8__) whose rounding differs by up to 1 LSB from the
 * generic C fallback that x86_64 uses (x86_64 has NO cplx_mul/fixmul/fixmadd
 * header upstream). TWO primitives diverge and both feed the AAC-LC transform
 * (libFDK/src/{mdct,fft,dct}.cpp), used by the decoder synthesis filterbank AND
 * the encoder analysis filterbank:
 *
 * 1) cplxMultDiv2 (arm/cplx_mul_arm.h): AArch64 accumulates the two 64-bit
 *    products and arithmetic-shifts ONCE, after the add/sub
 *    (smull/smsubl/smaddl ; asr #32):
 *      AArch64 :  c_Re = ( (INT64)a_Re*b_Re - (INT64)a_Im*b_Im ) >> 32
 *    The generic C truncates EACH product separately, then adds/subtracts:
 *      genericC:  c_Re = ( (INT64)a_Re*b_Re >> 32 ) - ( (INT64)a_Im*b_Im >> 32 )
 *    The two discarded low halves can carry → ±1 LSB. This dominates the
 *    DECODER divergence (IMDCT/FFT synthesis).
 *
 * 2) fixmul_DD / fMult (arm/fixmul_arm.h): AArch64 keeps one extra low bit by
 *    shifting the 64-bit product right by 31 (smull ; asr #31):
 *      AArch64 :  fixmul_DD(a,b) = ( (INT64)a*b ) >> 31
 *    The generic C shifts right 32 then left 1, DISCARDING bit 31:
 *      genericC:  fixmul_DD(a,b) = ( ( (INT64)a*b ) >> 32 ) << 1
 *    → ±1 LSB whenever bit 31 of the product is set. fMult is used throughout
 *    the ENCODER (windowing/gain in mdct.cpp, the cplxMult twiddles in
 *    dct.cpp), which is why a cplxMultDiv2-only shim fixes decode but leaves a
 *    few encoder AUs (the M/S-stereo and broadband configs) 1 quantizer-bit off.
 *    (The *BitExact* fMult variants already match across arches — AArch64 maps
 *    them to the >>32<<1 form too — so only the plain fixmul_DD needs aligning.)
 *
 * 3) sqrtFixp / invSqrtNorm2 / invFixp / schur_div
 *    (libFDK/include/x86/fixpoint_math_x86.h): the x86 header reimplements these
 *    with FLOATING-POINT <math.h> sqrt, whereas AArch64 (no arm/fixpoint_math
 *    header exists) uses the integer, table-based generic fixpoint_math.h. The
 *    encoder's scalefactor / psychoacoustic path uses these, so they account
 *    for the last couple of encoder configs (48 kHz stereo, broadband pink)
 *    that (1) and (2) alone do not fix.
 *
 * All three divergences are deterministic per-arch and independent of -O level
 * (verified: -O0/-O1/-O2 are byte-identical to each other on x86_64).
 *
 * This shim makes the x86_64 libFDK build compute the EXACT AArch64 arithmetic
 * for these two primitives, in portable C, so the black-box reference is the
 * same fixed-point arithmetic the Go port targets and the byte-exact encode /
 * integer-exact decode assertions hold WITHOUT being weakened. It is a faithful
 * translation of the AArch64 inline asm to C — not a tolerance, and not an edit
 * to the upstream tree (force-included via -include; the tracked sources stay
 * pristine).
 *
 * No-op on non-x86_64 targets (the native AArch64 path is already correct).
 */
#ifndef X86_CPLXMUL_AARCH64_PARITY_H
#define X86_CPLXMUL_AARCH64_PARITY_H

#if defined(__x86_64__) || defined(__amd64__)

/* --- fixmul_DD (encoder fMult path) -------------------------------------- *
 * x86_64 has its OWN intrinsic header libFDK/include/x86/fixmul_x86.h whose
 * fixmul_DD is `imul ; =d(result) ; shl $1` == ((INT64)a*b >> 32) << 1, i.e. it
 * DISCARDS bit 31 — diverging from AArch64's `smull ; asr #31` == (INT64)a*b >>
 * 31. We replace it with the AArch64 form. x86/fixmul_x86.h defines its inline
 * UNCONDITIONALLY (it does not honour FUNCTION_fixmul_DD), so suppressing the
 * generic guard alone is not enough — we also pre-set its include guard
 * (FIXMUL_X86_H) so its body is skipped entirely. The other things it would
 * have provided (fixmuldiv2_DD == (a*b)>>32, and the *BitExact* aliases) then
 * fall back to the generic C in fixmul.h, which is ARITHMETICALLY IDENTICAL to
 * the x86 asm versions, so only fixmul_DD changes behaviour.
 *
 * Order matters: machine_type.h gives us INT/INT64, and our fixmul_DD must be
 * defined BEFORE common_fix.h's fMult(LONG,LONG) wrapper (which calls
 * fixmul_DD) is parsed. */
#include "machine_type.h"

#define FIXMUL_X86_H   /* skip libFDK/include/x86/fixmul_x86.h entirely */
#define FUNCTION_fixmul_DD /* skip the generic fixmul_DD in fixmul.h */
inline INT fixmul_DD(const INT a, const INT b) {
  return (INT)(((INT64)a * b) >> 31); /* AArch64: smull ; asr #31 */
}

/* --- fixed-point math (encoder psy / quantizer path) --------------------- *
 * libFDK/include/x86/fixpoint_math_x86.h reimplements sqrtFixp / invSqrtNorm2 /
 * invFixp / schur_div for x86 using FLOATING-POINT <math.h> sqrt — diverging
 * from the integer, table-based generic implementations in fixpoint_math.h that
 * AArch64 uses (there is NO arm/fixpoint_math header, so AArch64 falls back to
 * the generic integer math). The encoder's scalefactor estimation /
 * psychoacoustic model lean on these, which is why the cplxMultDiv2 + fixmul_DD
 * fixes alone still leave a couple of the harder configs (48 kHz stereo,
 * broadband pink) 1 quantizer-bit off. Skip the x86 header (pre-set its include
 * guard) so x86_64 uses the same generic integer math as AArch64 and the Go
 * port. The generic schur_div is defined out-of-line in fixpoint_math.cpp and
 * linked normally; everything else is inline integer. */
#define FIXPOINT_MATH_X86_H

/* Suppress the generic cplxMultDiv2 fallbacks in cplx_mul.h for the three Div2
 * variants the AArch64 header replaces (x86_64 has no x86/cplx_mul header), then
 * supply matching implementations below. */
#define FUNCTION_cplxMultDiv2_32x32X2
#define FUNCTION_cplxMultDiv2_32x16X2
#define FUNCTION_cplxMultDiv2_32x16

/* Bring in the fixed-point types (FIXP_DBL/FIXP_SGL/FIXP_DPK/FIXP_SPK) and the
 * conversion helpers used below. common_fix.h includes fixmul.h + cplx_mul.h,
 * which now skip the suppressed variants and the x86 fixmul header. */
#include "common_fix.h"

/* AArch64 cplxMultDiv2(FIXP_DBL,FIXP_DBL) — accumulate in 64 bits, shift once.
 * Mirrors arm/cplx_mul_arm.h __ARM_ARCH_8__:
 *   smull/smull, smsubl/smaddl, asr #32. */
inline void cplxMultDiv2(FIXP_DBL *c_Re, FIXP_DBL *c_Im, const FIXP_DBL a_Re,
                         const FIXP_DBL a_Im, const FIXP_DBL b_Re,
                         const FIXP_DBL b_Im) {
  *c_Re = (FIXP_DBL)(((INT64)a_Re * b_Re - (INT64)a_Im * b_Im) >> 32);
  *c_Im = (FIXP_DBL)(((INT64)a_Re * b_Im + (INT64)a_Im * b_Re) >> 32);
}

/* 32x16 packed-twiddle (FIXP_SPK): AArch64 widens the 16-bit twiddle to 32 bits
 * (FX_SGL2FX_DBL == <<16) and dispatches to the 32x32 form above. */
inline void cplxMultDiv2(FIXP_DBL *c_Re, FIXP_DBL *c_Im, const FIXP_DBL a_Re,
                         const FIXP_DBL a_Im, FIXP_SPK wpk) {
  FIXP_DBL b_Re = FX_SGL2FX_DBL(wpk.v.re);
  FIXP_DBL b_Im = FX_SGL2FX_DBL(wpk.v.im);
  cplxMultDiv2(c_Re, c_Im, a_Re, a_Im, b_Re, b_Im);
}

/* 32x16 two-scalar (FIXP_SGL,FIXP_SGL): AArch64 widens each and dispatches to
 * the 32x32 form above. */
inline void cplxMultDiv2(FIXP_DBL *c_Re, FIXP_DBL *c_Im, const FIXP_DBL a_Re,
                         const FIXP_DBL a_Im, const FIXP_SGL b_Re,
                         const FIXP_SGL b_Im) {
  FIXP_DBL b_re = FX_SGL2FX_DBL(b_Re);
  FIXP_DBL b_im = FX_SGL2FX_DBL(b_Im);
  cplxMultDiv2(c_Re, c_Im, a_Re, a_Im, b_re, b_im);
}

#endif /* __x86_64__ || __amd64__ */

#endif /* X86_CPLXMUL_AARCH64_PARITY_H */
