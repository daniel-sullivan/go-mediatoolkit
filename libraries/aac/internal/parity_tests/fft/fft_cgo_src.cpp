// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC fixed-point DIT FFT
 * (libFDK/src/fft_rad2.cpp dit_fft + the libFDK/src/FDK_tools_rom.cpp
 * SineTable512 twiddle ROM the AAC-LC filterbank fft() dispatcher feeds it for
 * lengths 64/128/256/512). This translation unit provides the extern "C"
 * bridge the Go test calls.
 *
 * It compiles the GENUINE vendored sources whole — fft_rad2.cpp (which defines
 * the single non-static symbol dit_fft) and FDK_tools_rom.cpp (which defines
 * SineTable512) — so the oracle is the real reference, not a hand-twin. dit_fft
 * pulls only header-inline helpers (scramble.h, cplx_mul.h, fixmul.h, scale.h),
 * which ARE the genuine vendored headers; no other libfdk TU is linked. This
 * file NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the same amalgamation-split reasoning the sibling oracles document).
 *
 * FP-parity: the FFT is implemented entirely in fixed point (FIXP_DBL == int32,
 * Q1.31; the trig ROM in FIXP_SGL == int16, Q1.15 under SINETABLE_16BIT). The
 * butterflies are integer adds/shifts and the int64-product>>32 fixmul/cplxMul
 * kernels — bit-identical regardless of -ffp-contract / vectorization, so the
 * mise scalar flags are irrelevant to it and there is no transcendental shim.
 *
 * Active config: on this build platform (aarch64) FDK_archdef.h selects
 * ARCH_PREFER_MULT_32x16 + SINETABLE_16BIT, so FIXP_STP == FIXP_SPK (packed
 * 16-bit) and the trig multiply uses the 32x16X2 cplxMultDiv2; the 32x32X2
 * twiddle (W_PiFOURTH) uses the arm ARCH_8 inline-asm overload. The Go port
 * (nativeaac fft.go / fft_cplxmul.go) reproduces exactly this active variant.
 */

#include <stdint.h>

#include "fft_rad2.h"
#include "FDK_tools_rom.h"

extern "C" {

/* fparity_dit_fft runs the vendored dit_fft in place over x[0:2*(1<<ldn)]
 * (interleaved complex, re at even / im at odd indices) using the genuine
 * 512-point SineTable512 ROM — exactly the call fft() makes for the
 * 64/128/256/512 AAC-LC lengths. */
void fparity_dit_fft(int32_t *x, int ldn) {
  dit_fft((FIXP_DBL *)x, (INT)ldn, SineTable512, 512);
}

/* fparity_sinetable512_q15 copies the first `count` entries of the genuine
 * in-RAM SineTable512 (the narrowed FIXP_SGL re/im pairs the SINETABLE_16BIT
 * build links) so the Go side can verify its STC-narrowed ROM byte-for-byte. */
void fparity_sinetable512_q15(int16_t *outRe, int16_t *outIm, int count) {
  for (int i = 0; i < count; i++) {
    outRe[i] = (int16_t)SineTable512[i].v.re;
    outIm[i] = (int16_t)SineTable512[i].v.im;
  }
}

} /* extern "C" */
