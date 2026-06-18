// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC-LC encoder rate-control DRIVER tier
// (libAACenc/src/qc_main.cpp): FDKaacEnc_QCNew / FDKaacEnc_QCInit /
// FDKaacEnc_QCOutNew / FDKaacEnc_QCOutInit / FDKaacEnc_QCMainPrepare /
// FDKaacEnc_QCMain. The single non-static QCMain entry point drives the whole
// static chain — prepareBitDistribution / reduceBitConsumption / crashRecovery
// and the convergence loop — calling the GENUINE leaf kernels (CalcFormFactor,
// peCalculation, AdjustThresholds, EstimateScaleFactors, QuantizeSpectrum,
// dynBitCount, ChannelElementWrite, DistributeBits, BitResRedistribution) linked
// from the sibling vendored TUs compiled into this binary (cgo.go).
//
// oracle_kind == real_vendored: this is the genuine vendored FDK code end-to-end,
// not a Go-mirroring twin. The oracle constructs the QC_STATE / PSY_OUT / QC_OUT
// struct graph via the genuine GetRam_* RAM pool (QCNew/QCOutNew), seeds it with
// the SAME raw input the Go port receives (mdct spectrum, sfb layout, the per-sfb
// threshold/energy/minSnr/spread LD-data, noiseNrg/isBook/isScale), runs
// QCMainPrepare then QCMain, and copies out the quantized spectrum, scalefactors,
// global gain and the full bit accounting so the test can assert EXACT integer
// equality against the Go nativeaac port.
//
// AAC-LC CBR, single SCE/CPE element, single sub frame. transportEnc_* symbols
// referenced by the linked bitenc.cpp are satisfied by never-reached stubs (the
// QCMain path passes hTpEnc == NULL throughout).

#include "qc_data.h"
#include "qc_main.h"
#include "interface.h"
#include "channel_map.h"
#include "aacEnc_ram.h"
#include "tpenc_lib.h"

#include <stdint.h>
#include <string.h>
#include <stdlib.h>

// QCM_SFB is the transport-struct per-sfb array length. It is a fixed 120 (the
// Go nativeaac MaxGroupedSFB / MaxSections bound) so the flat qcm_in / qcm_out
// memory layout matches the Go cgo mirror exactly for the unsafe.Pointer cast.
// Only the genuine MAX_GROUPED_SFB (= 60) leading entries are forwarded into the
// real FDK structs.
#define QCM_SFB 120

// Mirrors the flat input layout the Go test marshals (see cgo.go QcMainInput).
struct qcm_in {
  // config / QC_INIT scalars
  int nChannels;     // 1 (SCE) or 2 (CPE)
  int bitrate;       // total bitrate
  int sampleRate;
  int maxBits;       // maxBitsPerFrame
  int minBits;
  int bitRes;
  int averageBits;
  int staticBits;    // globHdrBits (transport overhead)
  int meanPe;
  int maxIterations;
  int invQuant;
  int maxBitFac;     // FIXP_DBL
  int avgTotalBits;  // argument to QCMain

  // per-channel sfb layout (shared L/R for CPE)
  int sfbCnt;
  int sfbPerGroup;
  int maxSfbPerGroup;
  int lastWindowSequence;
  int sfbOffsets[1025];

  // per-channel raw spectral + psy LD-data (channel 0 then channel 1)
  int mdctSpectrum[2][1024];        // FIXP_DBL
  int sfbThresholdLdData[2][QCM_SFB];
  int sfbEnergyLdData[2][QCM_SFB];
  int sfbEnergy[2][QCM_SFB];
  int sfbMinSnrLdData[2][QCM_SFB];
  int sfbSpreadEnergy[2][QCM_SFB];
  int noiseNrg[2][QCM_SFB];
  int isBook[2][QCM_SFB];
  int isScale[2][QCM_SFB];
};

struct qcm_out {
  int errCode;
  // per-channel quantized output
  int16_t quantSpec[2][1024];
  int scf[2][QCM_SFB];
  int globalGain[2];
  unsigned int maxValueInSfb[2][QCM_SFB];
  // per-element bit accounting
  int staticBitsUsed;
  int dynBitsUsed;
  int grantedDynBits;
  int grantedPe;
  int grantedPeCorr;
  // per-AU bit accounting
  int usedDynBits;
  int auGrantedDynBits;
  int maxDynBits;
  int totalGrantedPeCorr;
  // section data (channel 0)
  int noOfSections0;
  int huffmanBits0;
  int sideInfoBits0;
  int scalefacBits0;
};

extern "C" {

int qcmain_e2e(const struct qcm_in *in, struct qcm_out *out) {
  memset(out, 0, sizeof(*out));

  // --- channel mapping (single SCE or CPE) ---------------------------------
  CHANNEL_MAPPING cm;
  memset(&cm, 0, sizeof(cm));
  cm.nChannels = in->nChannels;
  cm.nChannelsEff = in->nChannels;
  cm.nElements = 1;
  if (in->nChannels == 2) {
    cm.encMode = MODE_2;
    cm.elInfo[0].elType = ID_CPE;
    cm.elInfo[0].nChannelsInEl = 2;
    cm.elInfo[0].relativeBits = (FIXP_DBL)MAXVAL_DBL;
  } else {
    cm.encMode = MODE_1;
    cm.elInfo[0].elType = ID_SCE;
    cm.elInfo[0].nChannelsInEl = 1;
    cm.elInfo[0].relativeBits = (FIXP_DBL)MAXVAL_DBL;
  }
  cm.elInfo[0].instanceTag = 0;

  // --- allocate state via the genuine RAM pool -----------------------------
  static unsigned char dynRam[(1) * 1 * (10240)]; // generous DYN buffer
  QC_STATE *qcKernel = NULL;
  QC_OUT *qcOut[1] = {NULL};

  if (FDKaacEnc_QCNew(&qcKernel, 1, dynRam) != AAC_ENC_OK) {
    out->errCode = -100;
    return out->errCode;
  }
  if (FDKaacEnc_QCOutNew(qcOut, 1, in->nChannels, 1, dynRam) != AAC_ENC_OK) {
    out->errCode = -101;
    return out->errCode;
  }

  // --- QC_INIT -------------------------------------------------------------
  struct QC_INIT qcInit;
  memset(&qcInit, 0, sizeof(qcInit));
  qcInit.channelMapping = &cm;
  qcInit.maxBits = in->maxBits;
  qcInit.averageBits = in->averageBits;
  qcInit.bitRes = in->bitRes;
  qcInit.sampleRate = in->sampleRate;
  qcInit.isLowDelay = 0;
  qcInit.staticBits = in->staticBits;
  qcInit.bitrateMode = QCDATA_BR_MODE_CBR;
  qcInit.meanPe = in->meanPe;
  qcInit.chBitrate = in->bitrate / in->nChannels;
  qcInit.invQuant = in->invQuant;
  qcInit.maxIterations = in->maxIterations;
  qcInit.maxBitFac = (FIXP_DBL)in->maxBitFac;
  qcInit.bitrate = in->bitrate;
  qcInit.nSubFrames = 1;
  qcInit.minBits = in->minBits;
  qcInit.bitResMode = AACENC_BR_MODE_FULL;
  qcInit.bitDistributionMode = 0;
  qcInit.padding.paddingRest = in->sampleRate;

  if (FDKaacEnc_QCInit(qcKernel, &qcInit, 1) != AAC_ENC_OK) {
    out->errCode = -102;
    return out->errCode;
  }
  if (FDKaacEnc_QCOutInit(qcOut, 1, &cm) != AAC_ENC_OK) {
    out->errCode = -103;
    return out->errCode;
  }

  // --- build PSY_OUT graph -------------------------------------------------
  PSY_OUT psyOutStorage;
  PSY_OUT_ELEMENT psyElStorage;
  PSY_OUT_CHANNEL psyChStorage[2];
  memset(&psyOutStorage, 0, sizeof(psyOutStorage));
  memset(&psyElStorage, 0, sizeof(psyElStorage));
  memset(psyChStorage, 0, sizeof(psyChStorage));

  PSY_OUT *psyOut[1] = {&psyOutStorage};
  psyOut[0]->psyOutElement[0] = &psyElStorage;
  psyElStorage.commonWindow = (in->nChannels == 2) ? 1 : 0;

  // QC_OUT_ELEMENT holds the per-channel spectral / LD-data storage; the
  // PSY_OUT_CHANNEL fields sfbEnergy / sfbThresholdLdData / sfbEnergyLdData /
  // sfbMinSnrLdData / sfbSpreadEnergy / mdctSpectrum are POINTERS aliasing that
  // storage (interface.h "memory located in QC_OUT_CHANNEL"; the aliasing is
  // wired in FDKaacEnc_EncodeFrame, aacenc.cpp:803-808). Reproduce it here.
  QC_OUT_ELEMENT *qcEl = qcOut[0]->qcElement[0];
  for (int ch = 0; ch < in->nChannels; ch++) {
    PSY_OUT_CHANNEL *p = &psyChStorage[ch];
    QC_OUT_CHANNEL *q = qcEl->qcOutChannel[ch];
    psyElStorage.psyOutChannel[ch] = p;
    p->sfbCnt = in->sfbCnt;
    p->sfbPerGroup = in->sfbPerGroup;
    p->maxSfbPerGroup = in->maxSfbPerGroup;
    p->lastWindowSequence = in->lastWindowSequence;
    p->windowShape = 0;
    for (int i = 0; i <= in->sfbCnt; i++) p->sfbOffsets[i] = in->sfbOffsets[i];
    for (int s = 0; s < MAX_GROUPED_SFB; s++) {
      p->noiseNrg[s] = in->noiseNrg[ch][s];
      p->isBook[s] = in->isBook[ch][s];
      p->isScale[s] = in->isScale[ch][s];
    }
    // alias the QC_OUT_CHANNEL storage (aacenc.cpp:803-808)
    p->mdctSpectrum = q->mdctSpectrum;
    p->sfbSpreadEnergy = q->sfbSpreadEnergy;
    p->sfbEnergy = q->sfbEnergy;
    p->sfbEnergyLdData = q->sfbEnergyLdData;
    p->sfbMinSnrLdData = q->sfbMinSnrLdData;
    p->sfbThresholdLdData = q->sfbThresholdLdData;

    // seed the QC_OUT_CHANNEL storage with the raw input
    for (int i = 0; i < 1024; i++) q->mdctSpectrum[i] = (FIXP_DBL)in->mdctSpectrum[ch][i];
    for (int s = 0; s < MAX_GROUPED_SFB; s++) {
      q->sfbThresholdLdData[s] = (FIXP_DBL)in->sfbThresholdLdData[ch][s];
      q->sfbEnergyLdData[s] = (FIXP_DBL)in->sfbEnergyLdData[ch][s];
      q->sfbEnergy[s] = (FIXP_DBL)in->sfbEnergy[ch][s];
      q->sfbMinSnrLdData[s] = (FIXP_DBL)in->sfbMinSnrLdData[ch][s];
      q->sfbSpreadEnergy[s] = (FIXP_DBL)in->sfbSpreadEnergy[ch][s];
    }
  }

  // --- QCMainPrepare (per element) -----------------------------------------
  AAC_ENCODER_ERROR err = FDKaacEnc_QCMainPrepare(
      &cm.elInfo[0], qcKernel->hAdjThr->adjThrStateElem[0],
      psyOut[0]->psyOutElement[0], qcEl, AOT_AAC_LC, 0, -1);
  if (err != AAC_ENC_OK) {
    out->errCode = (int)err;
    return out->errCode;
  }
  qcOut[0]->staticBits = qcEl->staticBitsUsed;
  qcOut[0]->totalNoRedPe = qcEl->peData.pe;

  // --- QCMain --------------------------------------------------------------
  err = FDKaacEnc_QCMain(qcKernel, psyOut, qcOut, in->avgTotalBits, &cm,
                         AOT_AAC_LC, 0, -1);
  out->errCode = (int)err;
  if (err != AAC_ENC_OK) return out->errCode;

  // --- copy out ------------------------------------------------------------
  for (int ch = 0; ch < in->nChannels; ch++) {
    QC_OUT_CHANNEL *q = qcEl->qcOutChannel[ch];
    for (int i = 0; i < 1024; i++) out->quantSpec[ch][i] = q->quantSpec[i];
    for (int s = 0; s < MAX_GROUPED_SFB; s++) {
      out->scf[ch][s] = q->scf[s];
      out->maxValueInSfb[ch][s] = q->maxValueInSfb[s];
    }
    out->globalGain[ch] = q->globalGain;
  }
  out->staticBitsUsed = qcEl->staticBitsUsed;
  out->dynBitsUsed = qcEl->dynBitsUsed;
  out->grantedDynBits = qcEl->grantedDynBits;
  out->grantedPe = qcEl->grantedPe;
  out->grantedPeCorr = qcEl->grantedPeCorr;
  out->usedDynBits = qcOut[0]->usedDynBits;
  out->auGrantedDynBits = qcOut[0]->grantedDynBits;
  out->maxDynBits = qcOut[0]->maxDynBits;
  out->totalGrantedPeCorr = qcOut[0]->totalGrantedPeCorr;
  out->noOfSections0 = qcEl->qcOutChannel[0]->sectionData.noOfSections;
  out->huffmanBits0 = qcEl->qcOutChannel[0]->sectionData.huffmanBits;
  out->sideInfoBits0 = qcEl->qcOutChannel[0]->sectionData.sideInfoBits;
  out->scalefacBits0 = qcEl->qcOutChannel[0]->sectionData.scalefacBits;

  FDKaacEnc_QCClose(&qcKernel, qcOut);
  return out->errCode;
}

} // extern "C"

// ---- never-reached transportEnc_* stubs ------------------------------------
// The linked bitenc.cpp references these; the QCMain path passes hTpEnc == NULL,
// so they are never executed. Declared with C++ linkage to match tpenc_lib.h.
HANDLE_FDK_BITSTREAM transportEnc_GetBitstream(HANDLE_TRANSPORTENC hTp) {
  (void)hTp;
  return NULL;
}
int transportEnc_CrcStartReg(HANDLE_TRANSPORTENC hTpEnc, int mBits) {
  (void)hTpEnc;
  (void)mBits;
  return 0;
}
void transportEnc_CrcEndReg(HANDLE_TRANSPORTENC hTpEnc, int reg) {
  (void)hTpEnc;
  (void)reg;
}
TRANSPORTENC_ERROR transportEnc_EndAccessUnit(HANDLE_TRANSPORTENC hTp,
                                              int *bits) {
  (void)hTp;
  (void)bits;
  return TRANSPORTENC_OK;
}
INT transportEnc_GetStaticBits(HANDLE_TRANSPORTENC hTp, int auBits) {
  (void)hTp;
  (void)auBits;
  return 0;
}
