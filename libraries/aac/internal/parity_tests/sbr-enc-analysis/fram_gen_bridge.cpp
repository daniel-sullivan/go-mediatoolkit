// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR-encoder frame grid generator
 * (libSBRenc/src/fram_gen.cpp). Links the GENUINE vendored fram_gen.cpp (+
 * sbr_misc.cpp). The frame generator is pure-integer, so the oracle asserts
 * EXACT int equality against the Go port. */

#include <stdint.h>
#include <string.h>

#include "fram_gen.h"

extern "C" {

/* fparity_frame_info packs one SBR_FRAME_INFO + the load-bearing SBR_GRID
 * clear-text fields into the caller's int buffer, in the order the Go snapshot
 * uses:
 *   [0] nEnvelopes
 *   [1..6]   borders[0..5]
 *   [7..11]  freqRes[0..4]
 *   [12] shortEnv
 *   [13] nNoiseEnvelopes
 *   [14..16] bordersNoise[0..2]
 *   [17] frameClass [18] bs_num_env [19] bs_abs_bord [20] n [21] p
 *   [22] bs_abs_bord_0 [23] bs_abs_bord_1 [24] bs_num_rel_0 [25] bs_num_rel_1
 */
static void packFrameInfo(const SBR_FRAME_INFO *fi, const SBR_GRID *g, int *o) {
  o[0] = fi->nEnvelopes;
  for (int i = 0; i < 6; i++) o[1 + i] = fi->borders[i];
  for (int i = 0; i < 5; i++) o[7 + i] = (int)fi->freqRes[i];
  o[12] = fi->shortEnv;
  o[13] = fi->nNoiseEnvelopes;
  for (int i = 0; i < 3; i++) o[14 + i] = fi->bordersNoise[i];
  o[17] = (int)g->frameClass;
  o[18] = g->bs_num_env;
  o[19] = g->bs_abs_bord;
  o[20] = g->n;
  o[21] = g->p;
  o[22] = g->bs_abs_bord_0;
  o[23] = g->bs_abs_bord_1;
  o[24] = g->bs_num_rel_0;
  o[25] = g->bs_num_rel_1;
}

#define FI_STRIDE 26

/* fparity_run_frame_gen inits the generator and runs nFrames frames, feeding
 * tranInfos[i*3..] / tranInfosPre[i*3..] / rightBorderFIX[i]. It writes nFrames *
 * FI_STRIDE ints to out (one packed SBR_FRAME_INFO+grid per frame). */
void fparity_run_frame_gen(int allowSpread, int numEnvStatic, int staticFraming,
                           int timeSlots, const int *freqResFixfix,
                           unsigned char fResTransIsLow, int ldGrid,
                           const int *vTuning, int nFrames,
                           const unsigned char *tranInfos,
                           const unsigned char *tranInfosPre,
                           const int *rightBorderFIX, int *out) {
  SBR_ENVELOPE_FRAME h;
  FREQ_RES frf[2];
  frf[0] = (FREQ_RES)freqResFixfix[0];
  frf[1] = (FREQ_RES)freqResFixfix[1];

  FDKsbrEnc_initFrameInfoGenerator(&h, allowSpread, numEnvStatic, staticFraming,
                                   timeSlots, frf, fResTransIsLow, ldGrid);

  for (int i = 0; i < nFrames; i++) {
    unsigned char ti[3], tip[3];
    for (int k = 0; k < 3; k++) {
      ti[k] = tranInfos[i * 3 + k];
      tip[k] = tranInfosPre[i * 3 + k];
    }
    HANDLE_SBR_FRAME_INFO fi = FDKsbrEnc_frameInfoGenerator(
        &h, ti, rightBorderFIX[i], tip, ldGrid, vTuning);
    packFrameInfo(fi, &h.SbrGrid, out + (long)i * FI_STRIDE);
  }
}

} /* extern "C" */
