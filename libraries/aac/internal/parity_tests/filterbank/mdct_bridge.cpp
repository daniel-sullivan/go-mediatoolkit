// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point inverse-MDCT synthesis
 * filterbank (libFDK/src/mdct.cpp: imlt_block == FrequencyToTime, plus
 * imdct_gain / mdct_init it builds on, and the AAC-LC FrequencyToTime output
 * tail scaleValuesSaturate). This translation unit provides the extern "C"
 * bridge the Go test calls; it links the GENUINE vendored mdct.cpp / dct.cpp /
 * fft.cpp / fft_rad2.cpp / FDK_tools_rom.cpp / scale.cpp / genericStds.cpp
 * (the sibling TUs) so the oracle is the real reference, not a hand-twin.
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the same amalgamation-split reasoning the sibling dct/fft oracles
 * document). It MAY, and the test does, import the pure-Go internal/nativeaac.
 *
 * FP-parity: mdct.cpp is implemented entirely in fixed point — FIXP_DBL == int32
 * (Q-format data) and the window slopes FIXP_WTP == FIXP_SPK == packed int16
 * (Q1.15) under the active WINDOWTABLE_16BIT config. The fold is integer
 * adds/shifts + the int64-product>>32 cplxMultDiv2 / fMultDiv2 kernels, the inner
 * dct_IV is the same integer transform, and the saturation/scaleValue paths are
 * integer — bit-identical regardless of -ffp-contract / vectorization, with no
 * transcendental. So it asserts EXACT int32 equality (no aac_strict gate).
 *
 * The AAC-LC inverse filterbank (block.cpp:1227 CBlock_FrequencyToTime, the
 * last_core_mode != LPD branch) calls imlt_block with currAliasingSymmetry == 0
 * (so flags == 0), gain == 0, and the FDKgetWindowSlope(fl/fr, shape) window
 * slopes; the bridge reproduces exactly that call against a persistent mdct_t.
 */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "mdct.h"
#include "dct.h"
#include "FDK_tools_rom.h"
#include "scale.h"

extern "C" {

/* mparity_window_slope copies the first `count` entries of the genuine
 * FDKgetWindowSlope(length, shape) FIXP_WTP table out as a flat
 * [re0,im0,re1,im1,...] int16 array (FIXP_WTP == FIXP_SPK packed 16-bit under
 * the active config). */
void mparity_window_slope(int length, int shape, int count, int16_t *out) {
  const FIXP_WTP *w = FDKgetWindowSlope(length, shape);
  for (int i = 0; i < count; i++) {
    out[2 * i + 0] = (int16_t)w[i].v.re;
    out[2 * i + 1] = (int16_t)w[i].v.im;
  }
}

/* mparity_dct_tables runs the genuine dct_getTables for transform length tl and
 * copies the first twCount entries of the selected twiddle (FIXP_WTP) and
 * stCount entries of the sin_twiddle (FIXP_STP) tables out flat. Returns the
 * selected sin_step. Mirrors the sibling dct oracle so the Go imlt_block port is
 * driven with the same ROM dct_IV uses internally. */
int mparity_dct_tables(int tl, int twCount, int stCount, int16_t *twiddleOut,
                       int16_t *sinTwiddleOut) {
  const FIXP_WTP *twiddle = NULL;
  const FIXP_STP *sin_twiddle = NULL;
  int sin_step = 0;
  dct_getTables(&twiddle, &sin_twiddle, &sin_step, tl);
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

/* mparity_state holds a persistent mdct_t + its overlap buffer so the test can
 * drive a sequence of imlt_block calls (the IMDCT is stateful: 50% overlap-add).
 * ovSize == OverlapBufferSize (768) for AAC-LC. */
typedef struct {
  mdct_t h;
  FIXP_DBL overlap[768];
} mparity_state;

/* mparity_init allocates+zeroes a state and runs the genuine mdct_init, mirroring
 * the decoder's FDKmemclear(overlap)+mdct_init per channel. Returns the opaque
 * handle. */
void *mparity_init(void) {
  mparity_state *s = (mparity_state *)malloc(sizeof(mparity_state));
  memset(s->overlap, 0, sizeof(s->overlap));
  mdct_init(&s->h, s->overlap, 768);
  return s;
}

void mparity_free(void *st) { free(st); }

/* mparity_imlt_block runs the genuine imlt_block over a copy of spectrum
 * (length nSpec*tl) into output, returning the number of output samples. The
 * window slopes wls/wrs (flat int16 re/im) are supplied by the caller so both
 * sides use the identical FDKgetWindowSlope ROM. scalefactor is the per-spectrum
 * input exponent array. The scratch spectrum is freed after imlt_block has
 * copied the overlap out (mdct.cpp:723-724 FDKmemcpy) — the pSpec+tl/2-1 overlap
 * source pointer it sets is only consumed within the call. */
int mparity_imlt_block(void *st, int32_t *output, const int32_t *spectrum,
                       const int16_t *scalefactor, int nSpec, int noOutSamples,
                       int tl, const int16_t *wls, int fl, const int16_t *wrs,
                       int fr, int32_t gain, int flags) {
  mparity_state *s = (mparity_state *)st;
  int n = nSpec * tl;
  FIXP_DBL *spec = (FIXP_DBL *)malloc(n * sizeof(FIXP_DBL));
  memcpy(spec, spectrum, n * sizeof(FIXP_DBL));
  int r = imlt_block(&s->h, (FIXP_DBL *)output, spec, (const SHORT *)scalefactor,
                     nSpec, noOutSamples, tl, (const FIXP_WTP *)wls, fl,
                     (const FIXP_WTP *)wrs, fr, (FIXP_DBL)gain, flags);
  free(spec);
  return r;
}

/* mparity_scale_out runs the genuine scaleValuesSaturate(dst,src,len,scale) —
 * the AAC-LC FrequencyToTime output tail (block.cpp:1240). */
void mparity_scale_out(int32_t *dst, const int32_t *src, int len, int scale) {
  scaleValuesSaturate((FIXP_DBL *)dst, (const FIXP_DBL *)src, len, scale);
}

} /* extern "C" */
