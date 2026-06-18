// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the psychoacoustic DRIVER
// FDKaacEnc_psyMain (libAACenc/src/psy_main.cpp). It drives the real
// FDKaacEnc_Open + FDKaacEnc_Initialize to build a genuine AAC_ENC (the sibling
// *_cgo.cpp TUs in this package compile every vendored encoder source), then
// reproduces the FDKaacEnc_EncodeFrame pointer aliasing (psyOutChan->X =
// qcOutChan->X) and calls the genuine FDKaacEnc_psyMain over a caller-supplied
// planar int16 input. The resulting PSY_OUT_CHANNEL state is dumped into a flat
// struct the Go test compares EXACTLY (int / int32) against nativeaac.ParityPsyMain.
//
// oracle_kind == real_vendored: links the genuine FDKaacEnc_psyMain symbol, NOT
// a re-derivation of the Go port. fdk-aac encode is fixed-point; the dump is
// exact-integer.

#include "libfdk/libAACenc/src/aacenc.h"
#include "libfdk/libAACenc/src/aacEnc_ram.h" // full struct AAC_ENC definition
#include "libfdk/libAACenc/src/qc_data.h"
#include "libfdk/libAACenc/src/interface.h"
#include "libfdk/libAACenc/src/psy_main.h"
#include "libfdk/libAACenc/src/psy_const.h"
#include "libfdk/libAACenc/src/aacenc_tns.h"

#include <stdint.h>
#include <string.h>

extern "C" {

// psymain_State mirrors the deterministic PSY_OUT_CHANNEL state FDKaacEnc_psyMain
// fills (plus the per-element common-window / MS digest). Flat fields + fixed
// arrays so cgo can carry them out by pointer. Sizes use the genuine macros.
typedef struct {
  int errCode;

  int commonWindow;
  int msDigest;
  int msMask[MAX_GROUPED_SFB];

  int sfbCnt[2];
  int sfbPerGroup[2];
  int maxSfbPerGroup[2];
  int windowShape[2];
  int lastWindowSequence[2];
  int groupingMask[2];
  int mdctScale[2];
  int groupLen[2][MAX_NO_OF_GROUPS];
  int sfbOffsets[2][MAX_GROUPED_SFB + 1];
  int noiseNrg[2][MAX_GROUPED_SFB];
  int isBook[2][MAX_GROUPED_SFB];
  int isScale[2][MAX_GROUPED_SFB];

  int32_t sfbEnergy[2][MAX_GROUPED_SFB];
  int32_t sfbSpreadEnergy[2][MAX_GROUPED_SFB];
  int32_t sfbEnergyLdData[2][MAX_GROUPED_SFB];
  int32_t sfbThresholdLdData[2][MAX_GROUPED_SFB];
  int32_t sfbMinSnrLdData[2][MAX_GROUPED_SFB];

  int tnsNumOfFilters[2][TRANS_FAC];
  int tnsCoefRes[2][TRANS_FAC];
  int tnsOrder[2][TRANS_FAC][MAX_NUM_OF_FILTERS];
} psymain_State;

// epsymain_run drives the genuine init + FDKaacEnc_psyMain over the planar int16
// input (channel ch at input[ch*inputBufSize : (ch+1)*inputBufSize]) for element
// 0, and writes the PSY_OUT dump into *out. Returns the FDKaacEnc_psyMain error.
int epsymain_run(int channelMode, int nChannels, int sampleRate, int bitRate,
                 int audioObjectType, int nElements, int frameLength,
                 const short *input, int inputBufSize, psymain_State *out) {
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
    out->errCode = (int)err;
    return (int)err;
  }
  err = FDKaacEnc_Initialize(hAacEnc, &config, NULL, /*initFlags=*/1);
  if (err != AAC_ENC_OK) {
    out->errCode = (int)err;
    FDKaacEnc_Close(&hAacEnc);
    return (int)err;
  }

  CHANNEL_MAPPING *cm = &hAacEnc->channelMapping;
  PSY_OUT *psyOut = hAacEnc->psyOut[0];
  QC_OUT *qcOut = hAacEnc->qcOut[0];
  const int el = 0;
  ELEMENT_INFO elInfo = cm->elInfo[el];

  // reproduce FDKaacEnc_EncodeFrame pointer aliasing (aacenc.cpp:798-809)
  for (int ch = 0; ch < elInfo.nChannelsInEl; ch++) {
    PSY_OUT_CHANNEL *psyOutChan = psyOut->psyOutElement[el]->psyOutChannel[ch];
    QC_OUT_CHANNEL *qcOutChan = qcOut->qcElement[el]->qcOutChannel[ch];
    psyOutChan->mdctSpectrum = qcOutChan->mdctSpectrum;
    psyOutChan->sfbSpreadEnergy = qcOutChan->sfbSpreadEnergy;
    psyOutChan->sfbEnergy = qcOutChan->sfbEnergy;
    psyOutChan->sfbEnergyLdData = qcOutChan->sfbEnergyLdData;
    psyOutChan->sfbMinSnrLdData = qcOutChan->sfbMinSnrLdData;
    psyOutChan->sfbThresholdLdData = qcOutChan->sfbThresholdLdData;
  }

  AAC_ENCODER_ERROR ec = FDKaacEnc_psyMain(
      elInfo.nChannelsInEl, hAacEnc->psyKernel->psyElement[el],
      hAacEnc->psyKernel->psyDynamic, hAacEnc->psyKernel->psyConf,
      psyOut->psyOutElement[el], (INT_PCM *)input, (UINT)inputBufSize,
      cm->elInfo[el].ChannelIndex, cm->nChannels);

  out->errCode = (int)ec;
  if (ec != AAC_ENC_OK) {
    FDKaacEnc_Close(&hAacEnc);
    return (int)ec;
  }

  PSY_OUT_ELEMENT *poe = psyOut->psyOutElement[el];
  out->commonWindow = poe->commonWindow;
  out->msDigest = poe->toolsInfo.msDigest;
  for (int i = 0; i < MAX_GROUPED_SFB; i++) out->msMask[i] = poe->toolsInfo.msMask[i];

  for (int ch = 0; ch < elInfo.nChannelsInEl; ch++) {
    PSY_OUT_CHANNEL *poc = poe->psyOutChannel[ch];
    out->sfbCnt[ch] = poc->sfbCnt;
    out->sfbPerGroup[ch] = poc->sfbPerGroup;
    out->maxSfbPerGroup[ch] = poc->maxSfbPerGroup;
    out->windowShape[ch] = poc->windowShape;
    out->lastWindowSequence[ch] = poc->lastWindowSequence;
    out->groupingMask[ch] = poc->groupingMask;
    out->mdctScale[ch] = poc->mdctScale;
    for (int i = 0; i < MAX_NO_OF_GROUPS; i++) out->groupLen[ch][i] = poc->groupLen[i];
    for (int i = 0; i < MAX_GROUPED_SFB + 1; i++) out->sfbOffsets[ch][i] = poc->sfbOffsets[i];
    for (int i = 0; i < MAX_GROUPED_SFB; i++) {
      out->noiseNrg[ch][i] = poc->noiseNrg[i];
      out->isBook[ch][i] = poc->isBook[i];
      out->isScale[ch][i] = poc->isScale[i];
      out->sfbEnergy[ch][i] = (int32_t)poc->sfbEnergy[i];
      out->sfbSpreadEnergy[ch][i] = (int32_t)poc->sfbSpreadEnergy[i];
      out->sfbEnergyLdData[ch][i] = (int32_t)poc->sfbEnergyLdData[i];
      out->sfbThresholdLdData[ch][i] = (int32_t)poc->sfbThresholdLdData[i];
      out->sfbMinSnrLdData[ch][i] = (int32_t)poc->sfbMinSnrLdData[i];
    }
    for (int w = 0; w < TRANS_FAC; w++) {
      out->tnsNumOfFilters[ch][w] = poc->tnsInfo.numOfFilters[w];
      out->tnsCoefRes[ch][w] = poc->tnsInfo.coefRes[w];
      for (int f = 0; f < MAX_NUM_OF_FILTERS; f++)
        out->tnsOrder[ch][w][f] = poc->tnsInfo.order[w][f];
    }
  }

  FDKaacEnc_Close(&hAacEnc);
  return (int)ec;
}

} // extern "C"
