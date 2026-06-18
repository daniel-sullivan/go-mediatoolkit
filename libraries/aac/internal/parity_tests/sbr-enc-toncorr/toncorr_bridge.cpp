// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the SBR-encoder tonality-correction parameter extraction
 * (ton_corr.cpp). Two isolated, deterministic sub-drivers:
 *
 *   toncorr_quotas  — drives FDKsbrEnc_CalculateTonalityQuotas. The scalar LPC
 *                     params (lpcLength/stepSize/nextSample/move/...) that
 *                     FDKsbrEnc_InitTonCorrParamExtr would set are supplied
 *                     directly (identical to the Go side); the quota/sign matrix
 *                     rows point into local static storage (no RAM pool needed).
 *                     Identical complex QMF buffers are fed and the resulting
 *                     quotaMatrix / signMatrix / nrgVector / nrgVectorFreq are
 *                     returned for exact-integer comparison.
 *
 *   toncorr_patch   — drives the static resetPatch via its public entry point
 *                     FDKsbrEnc_ResetTonCorrParamExtr-equivalent inlined logic:
 *                     since resetPatch is file-static, we reach it through the
 *                     Init path with a hand-built v_k_master + scalar config and
 *                     return the patchParam table + indexVector.
 *
 * fdk-aac SBR is fixed-point => EXACT integer equality. */

#include <stdint.h>
#include <string.h>

#include "ton_corr.h"
#include "sbr_def.h"
#include "fram_gen.h"

/* resetPatch is file-static in ton_corr.cpp; ton_corr_tu_cgo.cpp #includes the
 * TU and exposes this tap so the bridge can drive it in isolation. */
extern "C" int toncorr_reset_patch_tap(SBR_TON_CORR_EST *h, int xposctrl,
                                       int highBandStartSb, unsigned char *vk,
                                       int numMaster, int fs, int noChannels);

extern "C" {

/* CalculateTonalityQuotas driver. quotaIn/signIn seed the matrices (nEst rows of
 * 64); srcReal/srcImag are buffLen rows of (usb+NUM_V_COMBINE) bands (flattened
 * row-major). Returns the full post-call quota/sign/nrg state. */
void toncorr_quotas(int lpcLen0, int lpcLen1, int stepSize, int nextSample,
                    int move, int startIndexMatrix, int numberOfEstimates,
                    int numberOfEstimatesPerFrame, int noQmfChannels, int buffLen,
                    int usb, int qmfScale, int srcStride,
                    const int32_t *quotaIn, const int32_t *signIn,
                    const int32_t *nrgIn,
                    const int32_t *srcReal, const int32_t *srcImag,
                    int32_t *quotaOut, int32_t *signOut, int32_t *nrgOut,
                    int32_t *nrgFreqOut) {
  static SBR_TON_CORR_EST h;
  memset(&h, 0, sizeof(h));

  static FIXP_DBL quotaStore[MAX_NO_OF_ESTIMATES][64];
  static INT signStore[MAX_NO_OF_ESTIMATES][64];
  for (int e = 0; e < MAX_NO_OF_ESTIMATES; e++) {
    h.quotaMatrix[e] = quotaStore[e];
    h.signMatrix[e] = signStore[e];
    for (int c = 0; c < 64; c++) {
      quotaStore[e][c] = (FIXP_DBL)quotaIn[e * 64 + c];
      signStore[e][c] = (INT)signIn[e * 64 + c];
    }
  }
  for (int e = 0; e < MAX_NO_OF_ESTIMATES; e++) h.nrgVector[e] = (FIXP_DBL)nrgIn[e];

  h.lpcLength[0] = lpcLen0;
  h.lpcLength[1] = lpcLen1;
  h.stepSize = stepSize;
  h.nextSample = nextSample;
  h.move = move;
  h.startIndexMatrix = startIndexMatrix;
  h.numberOfEstimates = numberOfEstimates;
  h.numberOfEstimatesPerFrame = numberOfEstimatesPerFrame;
  h.noQmfChannels = noQmfChannels;
  h.bufferLength = buffLen;

  /* Build the source pointer arrays (buffLen rows of srcStride bands). */
  static FIXP_DBL reStore[64][64];
  static FIXP_DBL imStore[64][64];
  FIXP_DBL *pRe[64];
  FIXP_DBL *pIm[64];
  for (int i = 0; i < buffLen; i++) {
    for (int b = 0; b < srcStride; b++) {
      reStore[i][b] = (FIXP_DBL)srcReal[i * srcStride + b];
      imStore[i][b] = (FIXP_DBL)srcImag[i * srcStride + b];
    }
    pRe[i] = reStore[i];
    pIm[i] = imStore[i];
  }

  FDKsbrEnc_CalculateTonalityQuotas(&h, pRe, pIm, usb, qmfScale);

  for (int e = 0; e < MAX_NO_OF_ESTIMATES; e++) {
    for (int c = 0; c < 64; c++) {
      quotaOut[e * 64 + c] = (int32_t)h.quotaMatrix[e][c];
      signOut[e * 64 + c] = (int32_t)h.signMatrix[e][c];
    }
    nrgOut[e] = (int32_t)h.nrgVector[e];
  }
  for (int c = 0; c < 64; c++) nrgFreqOut[c] = (int32_t)h.nrgVectorFreq[c];
}

/* resetPatch driver via FDKsbrEnc_ResetTonCorrParamExtr. The detector resets it
 * also performs need a prior noise-floor/invf init (the freqBandTableQmf), so we
 * init the noise-floor + invf detectors with a minimal valid low-res band table
 * first, then call Reset and read back the patch table + index vector. */
void toncorr_patch(int xposctrl, int highBandStartSb,
                   const unsigned char *vKMaster, int numMaster, int fs,
                   int noChannels, int guard, int shiftStartSb,
                   int *patchOut /* 6*6 ints */, signed char *indexOut /* 64 */,
                   int *noOfPatchesOut) {
  static SBR_TON_CORR_EST h;
  memset(&h, 0, sizeof(h));

  UCHAR vk[MAX_FREQ_COEFFS + 1];
  for (int i = 0; i <= numMaster; i++) vk[i] = vKMaster[i];

  h.guard = guard;
  h.shiftStartSb = shiftStartSb;

  toncorr_reset_patch_tap(&h, xposctrl, highBandStartSb, vk, numMaster, fs,
                          noChannels);

  for (int p = 0; p < MAX_NUM_PATCHES; p++) {
    patchOut[p * 6 + 0] = h.patchParam[p].sourceStartBand;
    patchOut[p * 6 + 1] = h.patchParam[p].sourceStopBand;
    patchOut[p * 6 + 2] = h.patchParam[p].guardStartBand;
    patchOut[p * 6 + 3] = h.patchParam[p].targetStartBand;
    patchOut[p * 6 + 4] = h.patchParam[p].targetBandOffs;
    patchOut[p * 6 + 5] = h.patchParam[p].numBandsInPatch;
  }
  for (int i = 0; i < 64; i++) indexOut[i] = h.indexVector[i];
  *noOfPatchesOut = h.noOfPatches;
}

} /* extern "C" */
