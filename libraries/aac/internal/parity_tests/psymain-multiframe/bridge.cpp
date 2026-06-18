// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Multi-frame psyMain carried-state parity bridge. Drives the GENUINE vendored
 * aacEncEncode (libAACenc/src/aacenc_lib.cpp) over a sequence of input frames
 * and, after EACH frame, snapshots the encoder's inter-frame carried state
 * directly out of the live handle:
 *
 *   hAacEnc->psyKernel->psyElement[0]->psyStatic[ch] : sfbThresholdnm1[],
 *       mdctScalenm1, calcPreEcho (psy_data.h:147-149) + the carried
 *       blockSwitchingControl window-sequence (block_switch.h).
 *   hAacEnc->qcKernel->hAdjThr->adjThrStateElem[0] : peLast, dynBitsLast,
 *       peCorrectionFactor_m/_e, chaosMeasureOld (adj_thr_data.h:153-160).
 *   hAacEnc->qcKernel->bitResTot (qc_data.h:280).
 *   hAacEnc->qcOut[0]->qcElement[0] : peData.pe, grantedDynBits, grantedPe
 *       (qc_data.h:216-221) — the element-level rate-control result that drives
 *       the next frame's threshold adaptation and bitstream.
 *
 * The bridge links the GENUINE vendored encoder TUs as siblings (the
 * fdk_tu_*.cpp amalgamation split) — oracle_kind == real_vendored. It NEVER
 * imports libraries/aac. mf_get_aac_enc (in fdk_tu_AACenc_aacenc_lib.cpp)
 * exposes the internal HANDLE_AAC_ENC because struct AACENCODER is private to
 * that TU.
 */

#include <stdint.h>
#include <string.h>

#include "aacenc.h"
#include "aacenc_lib.h"
#include "aacEnc_ram.h"
#include "psy_main.h"
#include "psy_data.h"
#include "qc_data.h"
#include "adj_thr_data.h"
#include "block_switch.h"

typedef struct {
  int sfbThresholdNm1[51];
  int mdctScaleNm1;
  int calcPreEcho;
  int lastWindowSequence;
  int windowShape;
  int lastWindowShape;
  int noOfGroups;
  int peLast;
  int dynBitsLast;
  int peCorrectionFactorM;
  int peCorrectionFactorE;
  int chaosMeasureOld;
  int mdctScale;
} mf_chan_state;

/* Per-channel POST-psyMain per-SFB outputs that feed peData.pe (the ld-domain
 * threshold/energy and the post-IS intensity book). The linear psyData
 * sfbEnergy/sfbThreshold scratch unions are NOT captured — they are reused
 * downstream of psyMain and are not a stable parity target. MAX_GROUPED_SFB ==
 * 60. */
typedef struct {
  int maxSfbPerGroup;
  int sfbCnt;
  int sfbPerGroup;
  int sfbThresholdLdData[60];
  int sfbEnergyLdData[60];
  int isBook[60];       /* psyOutChannel.isBook (post-IS) */
} mf_psyout_chan;

typedef struct {
  mf_chan_state ch[2];
  int bitResTot;
  int pe;
  int constPart;
  int nActiveLines;
  int grantedDynBits;
  int grantedPe;
  mf_psyout_chan psyOut[2];
  int msDigest;
  int msMask[60];
} mf_frame_state;

extern "C" void *mf_get_aac_enc(void *encoder);

extern "C" int mf_encode(int sampleRate, int channels, int bitrate,
                         short *pcm, int framesIn, int frameLen,
                         mf_frame_state *states) {
  HANDLE_AACENCODER enc;
  if (aacEncOpen(&enc, 0, (UINT)channels) != AACENC_OK) return -1;
  int channelMode = (channels == 1) ? MODE_1 : MODE_2;
  if (aacEncoder_SetParam(enc, AACENC_AOT, 2) != AACENC_OK ||
      aacEncoder_SetParam(enc, AACENC_SAMPLERATE, (UINT)sampleRate) != AACENC_OK ||
      aacEncoder_SetParam(enc, AACENC_CHANNELMODE, (UINT)channelMode) != AACENC_OK ||
      aacEncoder_SetParam(enc, AACENC_BITRATE, (UINT)bitrate) != AACENC_OK ||
      aacEncoder_SetParam(enc, AACENC_BITRATEMODE, 0) != AACENC_OK ||
      aacEncoder_SetParam(enc, AACENC_TRANSMUX, 0) != AACENC_OK) {
    aacEncClose(&enc);
    return -2;
  }
  if (aacEncEncode(enc, NULL, NULL, NULL, NULL) != AACENC_OK) {
    aacEncClose(&enc);
    return -3;
  }

  HANDLE_AAC_ENC hAacEnc = (HANDLE_AAC_ENC)mf_get_aac_enc((void *)enc);

  unsigned char auBuf[8192];
  int per = frameLen * channels;

  for (int f = 0; f < framesIn; f++) {
    AACENC_BufDesc inDesc;   memset(&inDesc, 0, sizeof(inDesc));
    AACENC_BufDesc outDesc;  memset(&outDesc, 0, sizeof(outDesc));
    AACENC_InArgs inArgs;    memset(&inArgs, 0, sizeof(inArgs));
    AACENC_OutArgs outArgs;  memset(&outArgs, 0, sizeof(outArgs));

    void *inPtr = pcm + (size_t)f * per;
    INT inId = IN_AUDIO_DATA, inSize = per * (INT)sizeof(short), inElem = (INT)sizeof(short);
    inDesc.numBufs = 1; inDesc.bufs = &inPtr; inDesc.bufferIdentifiers = &inId;
    inDesc.bufSizes = &inSize; inDesc.bufElSizes = &inElem;

    void *outPtr = auBuf;
    INT outId = OUT_BITSTREAM_DATA, outSize = (INT)sizeof(auBuf), outElem = 1;
    outDesc.numBufs = 1; outDesc.bufs = &outPtr; outDesc.bufferIdentifiers = &outId;
    outDesc.bufSizes = &outSize; outDesc.bufElSizes = &outElem;

    inArgs.numInSamples = per;
    AACENC_ERROR e = aacEncEncode(enc, &inDesc, &outDesc, &inArgs, &outArgs);
    if (e != AACENC_OK) { aacEncClose(&enc); return -10 - (int)e; }

    /* snapshot carried state AFTER this frame */
    mf_frame_state *st = &states[f];
    memset(st, 0, sizeof(*st));
    st->bitResTot = hAacEnc->qcKernel->bitResTot;
    {
      QC_OUT_ELEMENT *qe = hAacEnc->qcOut[0]->qcElement[0];
      st->pe = qe->peData.pe;
      st->constPart = qe->peData.constPart;
      st->nActiveLines = qe->peData.nActiveLines;
      st->grantedDynBits = qe->grantedDynBits;
      st->grantedPe = qe->grantedPe;
    }
    ATS_ELEMENT *ats = hAacEnc->qcKernel->hAdjThr->adjThrStateElem[0];
    for (int ch = 0; ch < channels; ch++) {
      PSY_STATIC *ps = hAacEnc->psyKernel->psyElement[0]->psyStatic[ch];
      mf_chan_state *cs = &st->ch[ch];
      for (int i = 0; i < 51; i++) cs->sfbThresholdNm1[i] = (int)ps->sfbThresholdnm1[i];
      cs->mdctScaleNm1 = ps->mdctScalenm1;
      cs->calcPreEcho = ps->calcPreEcho;
      cs->lastWindowSequence = ps->blockSwitchingControl.lastWindowSequence;
      cs->windowShape = ps->blockSwitchingControl.windowShape;
      cs->lastWindowShape = ps->blockSwitchingControl.lastWindowShape;
      cs->noOfGroups = ps->blockSwitchingControl.noOfGroups;
      cs->peLast = ats->peLast;
      cs->dynBitsLast = ats->dynBitsLast;
      cs->peCorrectionFactorM = (int)ats->peCorrectionFactor_m;
      cs->peCorrectionFactorE = ats->peCorrectionFactor_e;
      cs->chaosMeasureOld = (int)ats->chaosMeasureOld;
      cs->mdctScale = hAacEnc->psyKernel->psyDynamic->psyData[ch].mdctScale;
    }

    /* POST-psyMain per-SFB outputs for this frame (single subframe c==0). */
    {
      PSY_OUT_ELEMENT *poe = hAacEnc->psyOut[0]->psyOutElement[0];
      st->msDigest = poe->toolsInfo.msDigest;
      for (int i = 0; i < 60; i++) st->msMask[i] = poe->toolsInfo.msMask[i];
      for (int ch = 0; ch < channels; ch++) {
        PSY_OUT_CHANNEL *poc = poe->psyOutChannel[ch];
        mf_psyout_chan *po = &st->psyOut[ch];
        po->maxSfbPerGroup = poc->maxSfbPerGroup;
        po->sfbCnt = poc->sfbCnt;
        po->sfbPerGroup = poc->sfbPerGroup;
        for (int i = 0; i < 60; i++) {
          po->sfbThresholdLdData[i] = (int)poc->sfbThresholdLdData[i];
          po->sfbEnergyLdData[i] = (int)poc->sfbEnergyLdData[i];
          po->isBook[i] = poc->isBook[i];
        }
      }
    }
  }

  aacEncClose(&enc);
  return 0;
}
