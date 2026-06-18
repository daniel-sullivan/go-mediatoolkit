// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE analysis
 * filterbank — the forward (analysis) MDCT, libAACenc/src/transform.cpp:117
 * FDKaacEnc_Transform_Real, which folds+windows a block of INT_PCM time samples
 * and runs the shared inner dct_IV to produce the FIXP_DBL MDCT spectrum (one
 * long spectrum or eight short spectra) plus the per-block exponent. This is the
 * encoder's first DSP stage (psy_main.cpp:573 calls it per channel against a
 * persistent mdct_t).
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored transform.cpp / mdct.cpp / dct.cpp / fft.cpp / fft_rad2.cpp /
 * FDK_tools_rom.cpp / scale.cpp / genericStds.cpp / aacEnc_rom.cpp (the sibling
 * TUs) so the oracle is the real reference, NOT a hand-twin (oracle_kind ==
 * real_vendored: the test calls the genuine FDKaacEnc_Transform_Real symbol).
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the same amalgamation-split reasoning the sibling decode filterbank
 * oracle documents). It MAY, and the test does, import the pure-Go
 * internal/nativeaac.
 *
 * FP-parity: the forward MDCT is implemented entirely in fixed point — FIXP_PCM
 * == FIXP_SGL == int16 (SAMPLE_BITS == 16), FIXP_DBL == int32 Q-format data, and
 * the window slopes FIXP_WTP == FIXP_SPK == packed int16 (Q1.15) under the active
 * WINDOWTABLE_16BIT config. The fold is integer shifts (DFRACT_BITS-SAMPLE_BITS-1
 * == 15) plus the int32 fixmuldiv2_SS products and the int64-product>>32 dct_IV
 * kernel — bit-identical regardless of -ffp-contract / vectorization, with no
 * transcendental. So it asserts EXACT int32 equality (no aac_strict gate). */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "psy_const.h"
#include "transform.h"
#include "mdct.h"
#include "dct.h"
#include "FDK_tools_rom.h"

extern "C" {

/* eparity_window_slope copies the first `count` entries of the genuine
 * FDKgetWindowSlope(length, shape) FIXP_WTP table out as a flat
 * [re0,im0,re1,im1,...] int16 array (FIXP_WTP == FIXP_SPK packed 16-bit). The
 * encoder analysis MDCT selects its right window slope this way
 * (transform.cpp:155); the Go side is driven with the same ROM. */
void eparity_window_slope(int length, int shape, int count, int16_t *out) {
  const FIXP_WTP *w = FDKgetWindowSlope(length, shape);
  for (int i = 0; i < count; i++) {
    out[2 * i + 0] = (int16_t)w[i].v.re;
    out[2 * i + 1] = (int16_t)w[i].v.im;
  }
}

/* eparity_state holds a persistent mdct_t so the test can drive a sequence of
 * forward-MDCT blocks (the analysis MDCT is stateful: each block's left window
 * slope/length is the previous block's right slope/length — prev_wrs/prev_fr).
 * The encoder inits with mdct_init(&mdctPers, NULL, 0) (psy_main.cpp:270) — the
 * forward mdct_block never touches the overlap buffer, only prev_*. */
typedef struct {
  mdct_t h;
} eparity_state;

void *eparity_init(void) {
  eparity_state *s = (eparity_state *)malloc(sizeof(eparity_state));
  memset(&s->h, 0, sizeof(s->h));
  mdct_init(&s->h, NULL, 0);
  return s;
}

void eparity_free(void *st) { free(st); }

/* eparity_transform_real runs the genuine FDKaacEnc_Transform_Real over
 * pTimeData (INT_PCM == int16, length frameLength) into mdctData (frameLength
 * FIXP_DBL lines), against the persistent state. blockType/windowShape are the
 * WINDOW_TYPE / WINDOW_SHAPE enums; *prevWindowShape is updated in place;
 * *mdctData_e receives the published block exponent. filterType is the non-ELD
 * filterbank id (ignored on the AAC-LC path). Returns the rc (0 ok, -1 fail). */
int eparity_transform_real(void *st, const int16_t *pTimeData, int32_t *mdctData,
                           int blockType, int windowShape, int *prevWindowShape,
                           int frameLength, int *mdctData_e, int filterType) {
  eparity_state *s = (eparity_state *)st;
  return FDKaacEnc_Transform_Real((const INT_PCM *)pTimeData,
                                  (FIXP_DBL *)mdctData, blockType, windowShape,
                                  prevWindowShape, &s->h, frameLength,
                                  mdctData_e, filterType);
}

} /* extern "C" */
