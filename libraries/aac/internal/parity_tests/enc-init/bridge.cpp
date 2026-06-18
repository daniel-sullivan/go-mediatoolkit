// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder init/config tier
// (libAACenc/src/aacenc.cpp FDKaacEnc_AacInitDefaultConfig / FDKaacEnc_Open /
// FDKaacEnc_Initialize and, transitively, psy_main.cpp FDKaacEnc_psyMainInit +
// qc_main.cpp FDKaacEnc_QCInit/QCOutInit + adj_thr.cpp FDKaacEnc_AdjThrInit +
// channel_map.cpp + bandwidth.cpp + psy_configuration.cpp + aacenc_tns.cpp +
// aacenc_pns.cpp). The sibling *_cgo.cpp TUs in this package each compile one
// genuine vendored source as its own translation unit (so file-static helpers
// never collide), and this bridge links the real FDKaacEnc_Open /
// FDKaacEnc_Initialize symbols (oracle_kind == real_vendored) — NOT a re-derived
// hand-twin of the Go port.
//
// The only piece not part of the AAC encoder core is the transport-encoder
// static-bit demand transportEnc_GetStaticBits(hTpEnc, auBits). For the AAC-LC
// CBR / raw (TT_MP4_RAW) path the genuine function returns 0 (no header bits, no
// PCE at frame 0), so this bridge supplies a stub that returns 0 and passes
// hTpEnc == NULL to FDKaacEnc_Initialize — pinning the transport static bits to
// the deterministic raw value the genuine code would produce, exactly as the Go
// port models it (a nil StaticBitsProvider -> 0). This keeps the oracle
// self-contained (no transport library link) while still driving the genuine
// init arithmetic.
//
// All fixed-point (FIXP_DBL == int32); the asserted init state is exact-integer.

#include "libfdk/libAACenc/src/aacenc.h"
#include "libfdk/libAACenc/src/aacEnc_ram.h" // full struct AAC_ENC definition
#include "libfdk/libAACenc/src/qc_data.h"
#include "libfdk/libAACenc/src/adj_thr_data.h"
#include "libfdk/libAACenc/src/psy_configuration.h"
#include "libfdk/libAACenc/src/channel_map.h"
#include "libfdk/libAACenc/src/psy_main.h"
#include "libfdk/libMpegTPEnc/include/tpenc_lib.h" // HANDLE_TRANSPORTENC

#include <stdint.h>
#include <string.h>

// transportEnc_GetStaticBits stub: deterministic raw/ADIF path (== 0). The
// genuine declaration (tpenc_lib.h) has C++ linkage and takes HANDLE_TRANSPORTENC.
// On the AAC-LC CBR / raw path the genuine function returns 0; this stub supplies
// that value so the oracle stays self-contained (no transport library link). The
// AAC core only ever forwards the hTpEnc it was given (NULL here) and the auBits.
INT transportEnc_GetStaticBits(HANDLE_TRANSPORTENC hTp, int auBits) {
  (void)hTp;
  (void)auBits;
  return 0;
}

// The transport-encoder write-path symbols below are referenced only by
// FDKaacEnc_EncodeFrame / FDKaacEnc_WriteBitstream / FDKaacEnc_writeExtensionData
// (bitenc.cpp + aacenc.cpp), which the init oracle NEVER calls — it drives only
// FDKaacEnc_Open + FDKaacEnc_Initialize. They exist purely to satisfy the linker
// (compiling those TUs as one amalgamation pulls the references in). They are
// never executed; trivial definitions suffice.
HANDLE_FDK_BITSTREAM transportEnc_GetBitstream(HANDLE_TRANSPORTENC hTp) {
  (void)hTp;
  return 0;
}
TRANSPORTENC_ERROR transportEnc_WriteAccessUnit(HANDLE_TRANSPORTENC hTp, INT a, INT b, INT c) {
  (void)hTp; (void)a; (void)b; (void)c;
  return TRANSPORTENC_OK;
}
TRANSPORTENC_ERROR transportEnc_EndAccessUnit(HANDLE_TRANSPORTENC hTp, INT *a) {
  (void)hTp; (void)a;
  return TRANSPORTENC_OK;
}
TRANSPORTENC_ERROR transportEnc_GetFrame(HANDLE_TRANSPORTENC hTp, INT *a) {
  (void)hTp; (void)a;
  return TRANSPORTENC_OK;
}
int transportEnc_CrcStartReg(HANDLE_TRANSPORTENC hTp, int mBits) {
  (void)hTp; (void)mBits;
  return 0;
}
void transportEnc_CrcEndReg(HANDLE_TRANSPORTENC hTp, int reg) {
  (void)hTp; (void)reg;
}

extern "C" {

// einit_State mirrors the deterministic init-populated state the Go parity test
// compares. Flat integer fields + fixed-size arrays so cgo can carry them out by
// pointer.
typedef struct {
  // --- channel mapping (CHANNEL_MAPPING) ---
  int cm_encMode;
  int cm_nChannels;
  int cm_nChannelsEff;
  int cm_nElements;
  int cm_elType[8];
  int cm_elInstanceTag[8];
  int cm_elNChannelsInEl[8];
  int cm_elChIndex[8];

  // --- AAC_ENC lifetime / config-derived ---
  int aot;
  int bitrateMode;
  int bandwidth90dB;
  unsigned int maxAncBytesPerAU;
  int cfg_bandWidth;

  // --- QC_STATE ---
  int qc_globHdrBits;
  int qc_maxBitsPerFrame;
  int qc_minBitsPerFrame;
  int qc_nElements;
  int qc_bitrateMode;
  int qc_bitResMode;
  int qc_bitResTot;
  int qc_bitResTotMax;
  int qc_maxIterations;
  int qc_invQuant;
  int qc_vbrQualFactor;
  int qc_maxBitFac;
  int qc_paddingRest;
  int qc_dZoneQuantEnable;
  // per-element ELEMENT_BITS
  int qc_eb_chBitrateEl[8];
  int qc_eb_maxBitsEl[8];
  int qc_eb_bitResLevelEl[8];
  int qc_eb_maxBitResBitsEl[8];
  int qc_eb_relativeBitsEl[8];

  // --- ADJ_THR_STATE ---
  int at_bitDistributionMode;
  int at_maxIter2ndGuess;
  // bresParamLong / bresParamShort
  int at_bpL[8]; // clipSaveLow,clipSaveHigh,minBitSave,maxBitSave,clipSpendLow,clipSpendHigh,minBitSpend,maxBitSpend
  int at_bpS[8];
  // per-element ATS_ELEMENT (assert nElements entries)
  int at_peMin[8];
  int at_peMax[8];
  int at_peOffset[8];
  int at_bits2PeFactor_m[8];
  int at_bits2PeFactor_e[8];
  int at_ah_modifyMinSnr[8];
  int at_ah_startSfbL[8];
  int at_ah_startSfbS[8];
  int at_msa_maxRed[8];
  int at_msa_startRatio[8];
  int at_msa_maxRatio[8];
  int at_msa_redRatioFac[8];
  int at_msa_redOffs[8];
  int at_peLast[8];
  int at_dynBitsLast[8];
  int at_peCorr_m[8];
  int at_peCorr_e[8];
  int at_vbrQualFactor[8];
  int at_chaosMeasureOld[8];

  // --- PSY_CONFIGURATION[0] (LONG) and [1] (SHORT) ---
  // header scalars
  int pc_sfbCnt[2];
  int pc_sfbActive[2];
  int pc_sfbActiveLFE[2];
  int pc_filterbank[2];
  int pc_maxAllowedIncreaseFactor[2];
  int pc_minRemainingThresholdFactor[2];
  int pc_lowpassLine[2];
  int pc_lowpassLineLFE[2];
  int pc_clipEnergy[2];
  int pc_granuleLength[2];
  int pc_allowIS[2];
  int pc_allowMS[2];
  // arrays (MAX_SFB == 51, +1 for offset)
  int pc_sfbOffset[2][52];
  int pc_sfbPcmQuantThreshold[2][51];
  int pc_sfbMaskLowFactor[2][51];
  int pc_sfbMaskHighFactor[2][51];
  int pc_sfbMaskLowFactorSprEn[2][51];
  int pc_sfbMaskHighFactorSprEn[2][51];
  int pc_sfbMinSnrLdData[2][51];
  // pnsConf
  int pc_pns_usePns[2];
  int pc_pns_minCorrelationEnergy[2];
  int pc_pns_noiseCorrelationThresh[2];
  // tnsConf scalars (the deterministic init footprint)
  int pc_tns_isLowDelay[2];
  int pc_tns_tnsActive[2];
  int pc_tns_maxOrder[2];
  int pc_tns_coefRes[2];
  int pc_tns_lpcStartBand[2][2];
  int pc_tns_lpcStartLine[2][2];
  int pc_tns_lpcStopBand[2];
  int pc_tns_lpcStopLine[2];
} einit_State;

static void dumpBresParam(const BRES_PARAM *bp, int out[8]) {
  out[0] = (int)bp->clipSaveLow;
  out[1] = (int)bp->clipSaveHigh;
  out[2] = (int)bp->minBitSave;
  out[3] = (int)bp->maxBitSave;
  out[4] = (int)bp->clipSpendLow;
  out[5] = (int)bp->clipSpendHigh;
  out[6] = (int)bp->minBitSpend;
  out[7] = (int)bp->maxBitSpend;
}

static void dumpPsyConf(const PSY_CONFIGURATION *pc, int idx, einit_State *s) {
  s->pc_sfbCnt[idx] = pc->sfbCnt;
  s->pc_sfbActive[idx] = pc->sfbActive;
  s->pc_sfbActiveLFE[idx] = pc->sfbActiveLFE;
  s->pc_filterbank[idx] = pc->filterbank;
  s->pc_maxAllowedIncreaseFactor[idx] = pc->maxAllowedIncreaseFactor;
  s->pc_minRemainingThresholdFactor[idx] = (int)pc->minRemainingThresholdFactor;
  s->pc_lowpassLine[idx] = pc->lowpassLine;
  s->pc_lowpassLineLFE[idx] = pc->lowpassLineLFE;
  s->pc_clipEnergy[idx] = (int)pc->clipEnergy;
  s->pc_granuleLength[idx] = pc->granuleLength;
  s->pc_allowIS[idx] = pc->allowIS;
  s->pc_allowMS[idx] = pc->allowMS;
  for (int i = 0; i < 52; i++) s->pc_sfbOffset[idx][i] = pc->sfbOffset[i];
  for (int i = 0; i < 51; i++) {
    s->pc_sfbPcmQuantThreshold[idx][i] = (int)pc->sfbPcmQuantThreshold[i];
    s->pc_sfbMaskLowFactor[idx][i] = (int)pc->sfbMaskLowFactor[i];
    s->pc_sfbMaskHighFactor[idx][i] = (int)pc->sfbMaskHighFactor[i];
    s->pc_sfbMaskLowFactorSprEn[idx][i] = (int)pc->sfbMaskLowFactorSprEn[i];
    s->pc_sfbMaskHighFactorSprEn[idx][i] = (int)pc->sfbMaskHighFactorSprEn[i];
    s->pc_sfbMinSnrLdData[idx][i] = (int)pc->sfbMinSnrLdData[i];
  }
  s->pc_pns_usePns[idx] = pc->pnsConf.usePns;
  s->pc_pns_minCorrelationEnergy[idx] = (int)pc->pnsConf.minCorrelationEnergy;
  s->pc_pns_noiseCorrelationThresh[idx] = (int)pc->pnsConf.noiseCorrelationThresh;
  s->pc_tns_isLowDelay[idx] = pc->tnsConf.isLowDelay;
  s->pc_tns_tnsActive[idx] = pc->tnsConf.tnsActive;
  s->pc_tns_maxOrder[idx] = pc->tnsConf.maxOrder;
  s->pc_tns_coefRes[idx] = pc->tnsConf.coefRes;
  for (int i = 0; i < 2; i++) {
    s->pc_tns_lpcStartBand[idx][i] = pc->tnsConf.lpcStartBand[i];
    s->pc_tns_lpcStartLine[idx][i] = pc->tnsConf.lpcStartLine[i];
  }
  s->pc_tns_lpcStopBand[idx] = pc->tnsConf.lpcStopBand;
  s->pc_tns_lpcStopLine[idx] = pc->tnsConf.lpcStopLine;
}

// einit_run drives the genuine init for a default AAC-LC CBR config and writes
// the resulting deterministic state into *out. Returns the FDKaacEnc_Initialize
// error code (0 == AAC_ENC_OK). channels/sampleRate/bitRate select the config;
// channelMode is the resolved CHANNEL_MODE (MODE_2 for stereo).
int einit_run(int channelMode, int nChannels, int sampleRate, int bitRate,
              int audioObjectType, int nElements, int frameLength,
              einit_State *out) {
  memset(out, 0, sizeof(*out));

  AACENC_CONFIG config;
  FDKaacEnc_AacInitDefaultConfig(&config);
  config.audioObjectType = (AUDIO_OBJECT_TYPE)audioObjectType;
  config.nChannels = nChannels;
  config.channelMode = (CHANNEL_MODE)channelMode;
  config.sampleRate = sampleRate;
  config.bitRate = bitRate;
  config.framelength = frameLength;

  HANDLE_AAC_ENC hAacEnc = NULL;
  AAC_ENCODER_ERROR err =
      FDKaacEnc_Open(&hAacEnc, nElements, nChannels, config.nSubFrames);
  if (err != AAC_ENC_OK || hAacEnc == NULL) {
    return (int)err;
  }

  // hTpEnc == NULL -> our transportEnc_GetStaticBits stub returns 0.
  err = FDKaacEnc_Initialize(hAacEnc, &config, NULL, /*initFlags=*/1);
  if (err != AAC_ENC_OK) {
    FDKaacEnc_Close(&hAacEnc);
    return (int)err;
  }

  CHANNEL_MAPPING *cm = &hAacEnc->channelMapping;
  out->cm_encMode = cm->encMode;
  out->cm_nChannels = cm->nChannels;
  out->cm_nChannelsEff = cm->nChannelsEff;
  out->cm_nElements = cm->nElements;
  for (int i = 0; i < cm->nElements && i < 8; i++) {
    out->cm_elType[i] = cm->elInfo[i].elType;
    out->cm_elInstanceTag[i] = cm->elInfo[i].instanceTag;
    out->cm_elNChannelsInEl[i] = cm->elInfo[i].nChannelsInEl;
    out->cm_elChIndex[i] = cm->elInfo[i].ChannelIndex[0];
  }

  out->aot = hAacEnc->aot;
  out->bitrateMode = hAacEnc->bitrateMode;
  out->bandwidth90dB = hAacEnc->bandwidth90dB;
  out->maxAncBytesPerAU = config.maxAncBytesPerAU;
  out->cfg_bandWidth = config.bandWidth;

  QC_STATE *qc = hAacEnc->qcKernel;
  out->qc_globHdrBits = qc->globHdrBits;
  out->qc_maxBitsPerFrame = qc->maxBitsPerFrame;
  out->qc_minBitsPerFrame = qc->minBitsPerFrame;
  out->qc_nElements = qc->nElements;
  out->qc_bitrateMode = qc->bitrateMode;
  out->qc_bitResMode = qc->bitResMode;
  out->qc_bitResTot = qc->bitResTot;
  out->qc_bitResTotMax = qc->bitResTotMax;
  out->qc_maxIterations = qc->maxIterations;
  out->qc_invQuant = qc->invQuant;
  out->qc_vbrQualFactor = (int)qc->vbrQualFactor;
  out->qc_maxBitFac = (int)qc->maxBitFac;
  out->qc_paddingRest = qc->padding.paddingRest;
  out->qc_dZoneQuantEnable = qc->dZoneQuantEnable;
  for (int i = 0; i < cm->nElements && i < 8; i++) {
    ELEMENT_BITS *eb = qc->elementBits[i];
    out->qc_eb_chBitrateEl[i] = eb->chBitrateEl;
    out->qc_eb_maxBitsEl[i] = eb->maxBitsEl;
    out->qc_eb_bitResLevelEl[i] = eb->bitResLevelEl;
    out->qc_eb_maxBitResBitsEl[i] = eb->maxBitResBitsEl;
    out->qc_eb_relativeBitsEl[i] = (int)eb->relativeBitsEl;
  }

  ADJ_THR_STATE *at = qc->hAdjThr;
  out->at_bitDistributionMode = at->bitDistributionMode;
  out->at_maxIter2ndGuess = at->maxIter2ndGuess;
  dumpBresParam(&at->bresParamLong, out->at_bpL);
  dumpBresParam(&at->bresParamShort, out->at_bpS);
  for (int i = 0; i < cm->nElements && i < 8; i++) {
    ATS_ELEMENT *e = at->adjThrStateElem[i];
    out->at_peMin[i] = e->peMin;
    out->at_peMax[i] = e->peMax;
    out->at_peOffset[i] = e->peOffset;
    out->at_bits2PeFactor_m[i] = (int)e->bits2PeFactor_m;
    out->at_bits2PeFactor_e[i] = e->bits2PeFactor_e;
    out->at_ah_modifyMinSnr[i] = e->ahParam.modifyMinSnr;
    out->at_ah_startSfbL[i] = e->ahParam.startSfbL;
    out->at_ah_startSfbS[i] = e->ahParam.startSfbS;
    out->at_msa_maxRed[i] = (int)e->minSnrAdaptParam.maxRed;
    out->at_msa_startRatio[i] = (int)e->minSnrAdaptParam.startRatio;
    out->at_msa_maxRatio[i] = (int)e->minSnrAdaptParam.maxRatio;
    out->at_msa_redRatioFac[i] = (int)e->minSnrAdaptParam.redRatioFac;
    out->at_msa_redOffs[i] = (int)e->minSnrAdaptParam.redOffs;
    out->at_peLast[i] = e->peLast;
    out->at_dynBitsLast[i] = e->dynBitsLast;
    out->at_peCorr_m[i] = (int)e->peCorrectionFactor_m;
    out->at_peCorr_e[i] = e->peCorrectionFactor_e;
    out->at_vbrQualFactor[i] = (int)e->vbrQualFactor;
    out->at_chaosMeasureOld[i] = (int)e->chaosMeasureOld;
  }

  PSY_INTERNAL *psy = hAacEnc->psyKernel;
  dumpPsyConf(&psy->psyConf[0], 0, out);
  dumpPsyConf(&psy->psyConf[1], 1, out);

  FDKaacEnc_Close(&hAacEnc);
  return (int)AAC_ENC_OK;
}

} // extern "C"
