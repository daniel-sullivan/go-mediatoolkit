// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Stateful PS-downmix parity bridge. Drives the genuine Fraunhofer FDK-AAC
 * parametric-stereo per-frame downmix (libSBRenc/src/ps_main.cpp:
 * FDKsbrEnc_PSEnc_ParametricStereoProcessing, which internally runs the stereo
 * analysis QMF + THREE_TO_TEN hybrid analysis, the PS parameter extraction, the
 * hybrid-history shift, and then DownmixPSQmfData) across MULTIPLE frames over a
 * persistent PS instance + analysis QMF banks + half-rate synthesis QMF bank, and
 * dumps the downmixed mono QMF core (real/imag per slot) plus the per-frame
 * downmix qmfScale.
 *
 * This mirrors EXACTLY how the SBR encoder's PS branch is wired
 * (sbr_encoder.cpp:1559-1564 QMF/PS create, 2296-2347 init, 1120-1124 per-frame
 * call) so the genuine fdk state (analysis-QMF history + hybrid filterbank +
 * qmfDelayLines) evolves identically to the native PSEncParametricStereoProcessing
 * driver and the only divergence surfaced is the downmix numerics.
 *
 * Compiles against the vendored fdk encoder TUs (qmf, FDK_hybrid, ps_main,
 * ps_encode, fixpoint_math, sbrenc_rom/ram, ...). NEVER imports libraries/aac.
 * Fixed-point => EXACT int32 equality. */

#include <stdlib.h>
#include <string.h>

#include "sbr_def.h"
#include "qmf.h"
#include "ps_main.h"
#include "ps_const.h"
#include "sbrenc_ram.h"

extern "C" {

/* psdmx_run drives nFrames frames of the genuine PS downmix.
 *
 *   pcmInterleaved : interleaved STEREO int16 (L,R,L,R,...), length
 *                    nFrames*noQmfSlots*noQmfBands*2.
 *   noQmfSlots/noQmfBands : core SBR QMF grid (32 / 64 on the dual-rate path).
 *   nStereoBands/maxEnvelopes/iidQuantErrorThreshold : per-bitrate PS tuning.
 *
 * Outputs (caller-allocated):
 *   mixRealFlat/mixImagFlat : nFrames * noQmfSlots * noQmfBands int32.
 *   downFlat                : nFrames * noQmfSlots * (noQmfBands>>1) int16 — the
 *                             downsampled MONO time signal (the AAC-core input).
 *   qmfScales               : nFrames int.
 *
 * Returns 0 on success, negative on failure. */
int psdmx_run(const short *pcmInterleaved, int nFrames, int noQmfSlots,
              int noQmfBands, int nStereoBands, int maxEnvelopes,
              int iidQuantErrorThreshold, int *mixRealFlat, int *mixImagFlat,
              short *downFlat, int *qmfScales) {
  const int halfBands = noQmfBands >> 1;
  const int perFrame = noQmfSlots * noQmfBands; /* mono samples per channel */

  /* --- two 64-band analysis QMF banks over cleared FIXP_QAS states --- */
  QMF_FILTER_BANK qmfAna[MAX_PS_CHANNELS];
  FIXP_QAS *anaStates[MAX_PS_CHANNELS];
  QMF_FILTER_BANK *hQmfAnalysis[MAX_PS_CHANNELS];
  for (int ch = 0; ch < MAX_PS_CHANNELS; ch++) {
    memset(&qmfAna[ch], 0, sizeof(QMF_FILTER_BANK));
    /* analysis polyphase delay line: 10*no_channels (FDK_qmf_domain) */
    anaStates[ch] = (FIXP_QAS *)calloc((size_t)(10 * noQmfBands), sizeof(FIXP_QAS));
    if (qmfInitAnalysisFilterBank(&qmfAna[ch], anaStates[ch], noQmfSlots,
                                  noQmfBands, noQmfBands, noQmfBands, 0) != 0) {
      return -1;
    }
    hQmfAnalysis[ch] = &qmfAna[ch];
  }

  /* --- half-band synthesis QMF bank over cleared FIXP_QSS states --- */
  QMF_FILTER_BANK sbrSynthQmf;
  memset(&sbrSynthQmf, 0, sizeof(QMF_FILTER_BANK));
  FIXP_QSS *synStates =
      (FIXP_QSS *)calloc((size_t)((2 * QMF_NO_POLY - 1) * halfBands), sizeof(FIXP_QSS));
  if (qmfInitSynthesisFilterBank(&sbrSynthQmf, synStates, noQmfSlots, halfBands,
                                 halfBands, halfBands, 0) != 0) {
    return -2;
  }

  /* --- PS instance + dynamic RAM that backs the hybrid current-frame window --- */
  HANDLE_PARAMETRIC_STEREO hPs = NULL;
  if (PSEnc_Create(&hPs) != PSENC_OK) return -3;

  UCHAR *dynamicRam = (UCHAR *)calloc((size_t)SBR_ENC_DYN_RAM_SIZE, 1);

  PSENC_CONFIG cfg;
  memset(&cfg, 0, sizeof(cfg));
  cfg.frameSize = noQmfSlots;
  cfg.qmfFilterMode = 0;
  cfg.sbrPsDelay = 0;
  cfg.nStereoBands = (PSENC_STEREO_BANDS_CONFIG)nStereoBands;
  cfg.maxEnvelopes = (PSENC_NENV_CONFIG)maxEnvelopes;
  cfg.iidQuantErrorThreshold = (FIXP_DBL)iidQuantErrorThreshold;
  if (PSEnc_Init(hPs, &cfg, noQmfSlots, noQmfBands, dynamicRam) != PSENC_OK) {
    return -4;
  }

  /* per-frame planar channel buffers + downmix output buffers */
  INT_PCM *pSamples[MAX_PS_CHANNELS];
  for (int ch = 0; ch < MAX_PS_CHANNELS; ch++)
    pSamples[ch] = (INT_PCM *)malloc((size_t)perFrame * sizeof(INT_PCM));

  FIXP_DBL *dmxRealStore = (FIXP_DBL *)malloc((size_t)noQmfSlots * 64 * sizeof(FIXP_DBL));
  FIXP_DBL *dmxImagStore = (FIXP_DBL *)malloc((size_t)noQmfSlots * 64 * sizeof(FIXP_DBL));
  FIXP_DBL *dmxReal[64], *dmxImag[64];
  INT_PCM *down = (INT_PCM *)malloc((size_t)noQmfSlots * halfBands * sizeof(INT_PCM));

  int rc = 0;
  for (int f = 0; f < nFrames; f++) {
    /* de-interleave this frame */
    long base = (long)f * perFrame * 2;
    for (int i = 0; i < perFrame; i++) {
      pSamples[0][i] = (INT_PCM)pcmInterleaved[base + i * 2 + 0];
      pSamples[1][i] = (INT_PCM)pcmInterleaved[base + i * 2 + 1];
    }
    for (int s = 0; s < noQmfSlots; s++) {
      dmxReal[s] = &dmxRealStore[s * 64];
      dmxImag[s] = &dmxImagStore[s * 64];
    }
    memset(dmxRealStore, 0, (size_t)noQmfSlots * 64 * sizeof(FIXP_DBL));
    memset(dmxImagStore, 0, (size_t)noQmfSlots * 64 * sizeof(FIXP_DBL));
    memset(down, 0, (size_t)noQmfSlots * halfBands * sizeof(INT_PCM));

    SCHAR qmfScale = 0;
    FDK_PSENC_ERROR e = FDKsbrEnc_PSEnc_ParametricStereoProcessing(
        hPs, pSamples, (UINT)perFrame, hQmfAnalysis, dmxReal, dmxImag, down,
        &sbrSynthQmf, &qmfScale, /*sendHeader*/ 0);
    if (e != PSENC_OK) {
      rc = -10;
      break;
    }

    for (int s = 0; s < noQmfSlots; s++) {
      for (int b = 0; b < noQmfBands; b++) {
        mixRealFlat[(f * noQmfSlots + s) * noQmfBands + b] = (int)dmxReal[s][b];
        mixImagFlat[(f * noQmfSlots + s) * noQmfBands + b] = (int)dmxImag[s][b];
      }
    }
    for (int i = 0; i < noQmfSlots * halfBands; i++) {
      downFlat[(long)f * noQmfSlots * halfBands + i] = down[i];
    }
    qmfScales[f] = (int)qmfScale;
  }

  for (int ch = 0; ch < MAX_PS_CHANNELS; ch++) {
    free(anaStates[ch]);
    free(pSamples[ch]);
  }
  free(synStates);
  free(dynamicRam);
  free(dmxRealStore);
  free(dmxImagStore);
  free(down);
  PSEnc_Destroy(&hPs);
  return rc;
}

} /* extern "C" */
