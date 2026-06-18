// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point DCT/DST primitives
 * (libFDK/src/dct.cpp: dct_II / dct_III / dct_IV / dst_III / dst_IV, the
 * FFT-kernel-based DCTs the AAC-LC inverse filterbank uses). This translation
 * unit provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored dct.cpp / fft.cpp / fft_rad2.cpp / FDK_tools_rom.cpp (the sibling TUs)
 * so the oracle is the real reference, not a hand-twin.
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the same amalgamation-split reasoning the sibling fft oracle documents).
 *
 * FP-parity: dct.cpp is implemented entirely in fixed point — FIXP_DBL == int32
 * (Q-format data) and the twiddles FIXP_WTP/FIXP_STP == FIXP_SPK == packed int16
 * (Q1.15) under the active WINDOWTABLE_16BIT/SINETABLE_16BIT config. The pre/post
 * twiddle is integer adds/shifts + the int64-product>>32 fixmul/cplxMul kernels,
 * bit-identical regardless of -ffp-contract / vectorization, with no
 * transcendental. So it asserts EXACT int32 equality (no aac_strict gate).
 *
 * dct_getTables hands the kernels a twiddle (FIXP_WTP) and a sin_twiddle
 * (FIXP_STP) table plus a sin_step; dparity_get_tables runs the genuine
 * selection and copies those tables out flat (re,im int16) so the Go port can be
 * driven with the identical ROM and the sin_step asserted equal.
 */

#include <stdint.h>
#include <string.h>

#include "dct.h"
#include "FDK_tools_rom.h"

extern "C" {

/* dparity_get_tables runs the genuine dct_getTables for transform length L and
 * copies the first `count` entries of the selected twiddle (FIXP_WTP) and
 * sin_twiddle (FIXP_STP) tables out as flat [re0,im0,re1,im1,...] int16 arrays
 * (both are FIXP_SPK packed 16-bit under the active config). Returns the
 * selected sin_step. */
int dparity_get_tables(int L, int twCount, int stCount, int16_t *twiddleOut,
                       int16_t *sinTwiddleOut) {
  const FIXP_WTP *twiddle = NULL;
  const FIXP_STP *sin_twiddle = NULL;
  int sin_step = 0;
  dct_getTables(&twiddle, &sin_twiddle, &sin_step, L);
  for (int i = 0; i < twCount; i++) {
    twiddleOut[2 * i + 0] = (int16_t)twiddle[i].v.re;
    twiddleOut[2 * i + 1] = (int16_t)twiddle[i].v.im;
  }
  for (int i = 0; i < stCount; i++) {
    sinTwiddleOut[2 * i + 0] = (int16_t)sin_twiddle[i].v.re;
    sinTwiddleOut[2 * i + 1] = (int16_t)sin_twiddle[i].v.im;
  }
  return sin_step;
}

/* dparity_dct_iv runs the genuine dct_IV in place over pDat[0:L] and returns the
 * exponent delta added to *pDat_e (starting from 0). */
int dparity_dct_iv(int32_t *pDat, int L) {
  int e = 0;
  dct_IV((FIXP_DBL *)pDat, L, &e);
  return e;
}

int dparity_dst_iv(int32_t *pDat, int L) {
  int e = 0;
  dst_IV((FIXP_DBL *)pDat, L, &e);
  return e;
}

int dparity_dct_iii(int32_t *pDat, int32_t *tmp, int L) {
  int e = 0;
  dct_III((FIXP_DBL *)pDat, (FIXP_DBL *)tmp, L, &e);
  return e;
}

int dparity_dst_iii(int32_t *pDat, int32_t *tmp, int L) {
  int e = 0;
  dst_III((FIXP_DBL *)pDat, (FIXP_DBL *)tmp, L, &e);
  return e;
}

int dparity_dct_ii(int32_t *pDat, int32_t *tmp, int L) {
  int e = 0;
  dct_II((FIXP_DBL *)pDat, (FIXP_DBL *)tmp, L, &e);
  return e;
}

} /* extern "C" */
