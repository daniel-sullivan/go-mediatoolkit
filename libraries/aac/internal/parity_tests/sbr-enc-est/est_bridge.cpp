// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the SBR-encoder inverse-filtering detector (invf_est.cpp)
 * and noise-floor estimator (nf_est.cpp). Both are driven from synthetic but
 * deterministic quota/energy/index inputs (identical to the Go side), inited via
 * the genuine init/reset funcs, run for a couple of frames (to exercise the
 * smoothing/hysteresis state), and the per-band INVF modes + quantised noise
 * levels are returned for exact-integer comparison. Fixed-point => EXACT. */

#include <stdint.h>
#include <string.h>

#include "invf_est.h"
#include "nf_est.h"
#include "sbr_def.h"
#include "fram_gen.h"

extern "C" {

/* invf detector: nEst estimates x qmfChannels quota matrix (flattened
 * row-major), nrgVector[nEst], indexVector[qmfChannels], freqBandTableDetector
 * [numDetectorBands+1]. Runs nFrames times, returns the final infVec
 * [numDetectorBands] (per call appended). */
void estparity_invf(const int32_t *quotaFlat, int nEst, int qmfChannels,
                    const int32_t *nrgVector, const signed char *indexVector,
                    const int *freqBandTableDetector, int numDetectorBands,
                    int useSpeech, int startIndex, int stopIndex,
                    const int *transientFlags, int nFrames, int *infVecOut) {
  SBR_INV_FILT_EST h;
  INT fbt[MAX_NUM_NOISE_VALUES + 1];
  for (int i = 0; i <= numDetectorBands; i++) fbt[i] = freqBandTableDetector[i];

  FDKsbrEnc_initInvFiltDetector(&h, fbt, numDetectorBands, useSpeech);

  /* build the pointer array the detector expects */
  FIXP_DBL *quota[MAX_NO_OF_ESTIMATES];
  static FIXP_DBL store[MAX_NO_OF_ESTIMATES][64];
  for (int e = 0; e < nEst; e++) {
    for (int c = 0; c < qmfChannels; c++)
      store[e][c] = (FIXP_DBL)quotaFlat[e * qmfChannels + c];
    quota[e] = store[e];
  }

  for (int f = 0; f < nFrames; f++) {
    INVF_MODE infVec[MAX_NUM_NOISE_VALUES];
    FDKsbrEnc_qmfInverseFilteringDetector(&h, quota, (FIXP_DBL *)nrgVector,
                                          (SCHAR *)indexVector, startIndex,
                                          stopIndex, transientFlags[f], infVec);
    for (int b = 0; b < numDetectorBands; b++)
      infVecOut[f * numDetectorBands + b] = (int)infVec[b];
  }
}

/* nf estimator: returns final quantised noiseLevels[nNoiseEnv*noNoiseBands] and
 * noNoiseBands. */
void estparity_nf(const int32_t *quotaFlat, int nEst, int qmfChannels,
                  const signed char *indexVector, const unsigned char *freqBandTable,
                  int nSfb, int anaMaxLevel, int noiseBands, int noiseFloorOffset,
                  int timeSlots, int useSpeech, int missingHarmonicsFlag,
                  int startIndex, int numberOfEstimatesPerFrame,
                  const int *transientFrames, const int *invfLevelsFlat,
                  int nNoiseEnvelopes, int nFrames, int32_t *noiseLevelsOut,
                  int *noNoiseBandsOut) {
  SBR_NOISE_FLOOR_ESTIMATE h;
  FDKsbrEnc_InitSbrNoiseFloorEstimate(&h, anaMaxLevel, freqBandTable, nSfb,
                                      noiseBands, noiseFloorOffset, timeSlots,
                                      useSpeech);
  *noNoiseBandsOut = h.noNoiseBands;

  FIXP_DBL *quota[MAX_NO_OF_ESTIMATES];
  static FIXP_DBL store[MAX_NO_OF_ESTIMATES][64];
  for (int e = 0; e < nEst; e++) {
    for (int c = 0; c < qmfChannels; c++)
      store[e][c] = (FIXP_DBL)quotaFlat[e * qmfChannels + c];
    quota[e] = store[e];
  }

  SBR_FRAME_INFO frameInfo;
  memset(&frameInfo, 0, sizeof(frameInfo));
  frameInfo.nNoiseEnvelopes = nNoiseEnvelopes;

  for (int f = 0; f < nFrames; f++) {
    INVF_MODE invfLevels[MAX_NUM_NOISE_VALUES];
    for (int b = 0; b < h.noNoiseBands; b++)
      invfLevels[b] = (INVF_MODE)invfLevelsFlat[f * MAX_NUM_NOISE_VALUES + b];

    FIXP_DBL noiseLevels[MAX_NUM_NOISE_VALUES];
    memset(noiseLevels, 0, sizeof(noiseLevels));
    FDKsbrEnc_sbrNoiseFloorEstimateQmf(
        &h, &frameInfo, noiseLevels, quota, (SCHAR *)indexVector,
        missingHarmonicsFlag, startIndex, numberOfEstimatesPerFrame,
        transientFrames[f], invfLevels, 0 /*syntaxFlags*/);

    if (f == nFrames - 1) {
      for (int i = 0; i < nNoiseEnvelopes * h.noNoiseBands; i++)
        noiseLevelsOut[i] = (int32_t)noiseLevels[i];
    }
  }
}

} /* extern "C" */
