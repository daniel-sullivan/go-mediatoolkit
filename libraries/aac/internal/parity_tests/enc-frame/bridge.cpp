// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Verbatim-twin oracle bridge for the self-contained integer rate-control
// driver helpers of the FDK-AAC quantizer/coder (libAACenc/src/qc_main.cpp).
//
// The functions exercised here — FDKaacEnc_calcFrameLen, FDKaacEnc_framePadding,
// FDKaacEnc_AdjustBitrate, FDKaacEnc_calcMaxValueInSfb,
// FDKaacEnc_BitResRedistribution, FDKaacEnc_distributeElementDynBits,
// FDKaacEnc_updateUsedDynBits, FDKaacEnc_getTotalConsumedDynBits,
// FDKaacEnc_getTotalConsumedBits, FDKaacEnc_updateBitres,
// FDKaacEnc_updateFillBits — are almost all `static` (file-local) in
// qc_main.cpp and the public ones (AdjustBitrate/updateBitres/updateFillBits)
// would, if compiled from the genuine TU, drag in the entire 12k-line encoder
// (quantize / adj_thr / sf_estim / channel_map / the encoder RAM pool). Linking
// that whole subsystem is not part of this slice.
//
// Per the parity discipline (add-audio-format SKILL: "for static/inline fns
// carry a BYTE-FOR-BYTE VERBATIM copy of the vendored source (renamed _oracle)
// + link real ROM/headers"), each function below is a byte-for-byte copy of the
// vendored qc_main.cpp body, renamed *_oracle. The struct layouts
// (QC_STATE/QC_OUT/QC_OUT_ELEMENT/ELEMENT_BITS/CHANNEL_MAPPING/ELEMENT_INFO),
// the FRAME_LEN_RESULT_MODE enum and the integer kernels the bodies call
// (fMultI, fixMax/fMax, fixMin/fMin, fixp_abs/fAbs) are the GENUINE vendored
// libFDK definitions, pulled from the real headers — only the function bodies
// are duplicated, and they are duplicated verbatim. oracle_kind == verbatim_twin.
//
// These are pure-integer kernels (truncating divisions, &~7/%8 alignment masks,
// fMultI fractional multiply, abs/min/max) with no float and no transcendental,
// so they assert EXACT int32 equality against the Go port regardless of
// -ffp-contract / vectorization.

#include "qc_data.h"        // QC_STATE / QC_OUT / ELEMENT_BITS / CHANNEL_MAPPING
#include "common_fix.h"     // fixMax / fixMin / fixp_abs (== fMax/fMin/fAbs)
#include "fixpoint_math.h"  // fMultI
#include "FDK_audio.h"      // ID_SCE / ID_CPE / ID_LFE

#include <stdint.h>
#include <string.h>

// --- FRAME_LEN_RESULT_MODE: verbatim from qc_main.cpp:142-146 ---------------
typedef enum {
  FRAME_LEN_BYTES_MODULO = 1,
  FRAME_LEN_BYTES_INT = 2
} FRAME_LEN_RESULT_MODE;

#define isAudioElement(elType) \
  ((elType == ID_SCE) || (elType == ID_CPE) || (elType == ID_LFE))

// === VERBATIM TWINS (qc_main.cpp) ==========================================

// FDKaacEnc_calcFrameLen — verbatim from qc_main.cpp:179-195.
static INT FDKaacEnc_calcFrameLen_oracle(INT bitRate, INT sampleRate,
                                         INT granuleLength,
                                         FRAME_LEN_RESULT_MODE mode) {
  INT result;

  result = ((granuleLength) >> 3) * (bitRate);

  switch (mode) {
    case FRAME_LEN_BYTES_MODULO:
      result %= sampleRate;
      break;
    case FRAME_LEN_BYTES_INT:
      result /= sampleRate;
      break;
  }
  return (result);
}

// FDKaacEnc_framePadding — verbatim from qc_main.cpp:206-223.
static INT FDKaacEnc_framePadding_oracle(INT bitRate, INT sampleRate,
                                         INT granuleLength, INT* paddingRest) {
  INT paddingOn;
  INT difference;

  paddingOn = 0;

  difference = FDKaacEnc_calcFrameLen_oracle(bitRate, sampleRate, granuleLength,
                                             FRAME_LEN_BYTES_MODULO);
  *paddingRest -= difference;

  if (*paddingRest <= 0) {
    paddingOn = 1;
    *paddingRest += sampleRate;
  }

  return (paddingOn);
}

// FDKaacEnc_AdjustBitrate — verbatim body from qc_main.cpp:469-489 (the hQC
// argument is reduced to its only touched field, padding.paddingRest).
static INT FDKaacEnc_AdjustBitrate_oracle(INT* paddingRest, INT* avgTotalBits,
                                          INT bitRate, INT sampleRate,
                                          INT granuleLength) {
  INT paddingOn;
  INT frameLen;

  paddingOn = FDKaacEnc_framePadding_oracle(bitRate, sampleRate, granuleLength,
                                            paddingRest);

  frameLen = paddingOn + FDKaacEnc_calcFrameLen_oracle(bitRate, sampleRate,
                                                       granuleLength,
                                                       FRAME_LEN_BYTES_INT);

  *avgTotalBits = frameLen << 3;

  return 0; /* AAC_ENC_OK */
}

// FDKaacEnc_calcMaxValueInSfb — verbatim from qc_main.cpp:1219-1240.
static INT FDKaacEnc_calcMaxValueInSfb_oracle(INT sfbCnt, INT maxSfbPerGroup,
                                              INT sfbPerGroup, INT* sfbOffset,
                                              SHORT* quantSpectrum,
                                              UINT* maxValue) {
  INT sfbOffs, sfb;
  INT maxValueAll = 0;

  for (sfbOffs = 0; sfbOffs < sfbCnt; sfbOffs += sfbPerGroup)
    for (sfb = 0; sfb < maxSfbPerGroup; sfb++) {
      INT line;
      INT maxThisSfb = 0;
      for (line = sfbOffset[sfbOffs + sfb]; line < sfbOffset[sfbOffs + sfb + 1];
           line++) {
        INT tmp = fixp_abs(quantSpectrum[line]);
        maxThisSfb = fixMax(tmp, maxThisSfb);
      }

      maxValue[sfbOffs + sfb] = maxThisSfb;
      maxValueAll = fixMax(maxThisSfb, maxValueAll);
    }
  return maxValueAll;
}

// FDKaacEnc_BitResRedistribution — verbatim from qc_main.cpp:738-786.
static int FDKaacEnc_BitResRedistribution_oracle(QC_STATE* const hQC,
                                                 const CHANNEL_MAPPING* const cm,
                                                 const INT avgTotalBits) {
  if (hQC->bitResTot < 0) {
    return 0x40a0; /* AAC_ENC_BITRES_TOO_LOW */
  } else if (hQC->bitResTot > hQC->bitResTotMax) {
    return 0x40a1; /* AAC_ENC_BITRES_TOO_HIGH */
  } else {
    INT i;
    INT totalBits = 0, totalBits_max = 0;

    const int totalBitreservoir =
        fMin(hQC->bitResTot, (hQC->maxBitsPerFrame - avgTotalBits));
    const int totalBitreservoirMax =
        fMin(hQC->bitResTotMax, (hQC->maxBitsPerFrame - avgTotalBits));

    for (i = (cm->nElements - 1); i >= 0; i--) {
      if ((cm->elInfo[i].elType == ID_SCE) ||
          (cm->elInfo[i].elType == ID_CPE) ||
          (cm->elInfo[i].elType == ID_LFE)) {
        hQC->elementBits[i]->bitResLevelEl =
            fMultI(hQC->elementBits[i]->relativeBitsEl, totalBitreservoir);
        totalBits += hQC->elementBits[i]->bitResLevelEl;

        hQC->elementBits[i]->maxBitResBitsEl =
            fMultI(hQC->elementBits[i]->relativeBitsEl, totalBitreservoirMax);
        totalBits_max += hQC->elementBits[i]->maxBitResBitsEl;
      }
    }
    for (i = 0; i < cm->nElements; i++) {
      if ((cm->elInfo[i].elType == ID_SCE) ||
          (cm->elInfo[i].elType == ID_CPE) ||
          (cm->elInfo[i].elType == ID_LFE)) {
        int deltaBits = fMax(totalBitreservoir - totalBits,
                             -hQC->elementBits[i]->bitResLevelEl);
        hQC->elementBits[i]->bitResLevelEl += deltaBits;
        totalBits += deltaBits;

        deltaBits = fMax(totalBitreservoirMax - totalBits_max,
                         -hQC->elementBits[i]->maxBitResBitsEl);
        hQC->elementBits[i]->maxBitResBitsEl += deltaBits;
        totalBits_max += deltaBits;
      }
    }
  }

  return 0; /* AAC_ENC_OK */
}

// FDKaacEnc_distributeElementDynBits — verbatim from qc_main.cpp:504-550.
static int FDKaacEnc_distributeElementDynBits_oracle(QC_STATE* hQC,
                                                     QC_OUT_ELEMENT* qcElement[((8))],
                                                     CHANNEL_MAPPING* cm,
                                                     INT codeBits) {
  INT i;
  INT totalBits = 0;

  for (i = (cm->nElements - 1); i >= 0; i--) {
    if (isAudioElement(cm->elInfo[i].elType)) {
      qcElement[i]->grantedDynBits =
          fMax(0, fMultI(hQC->elementBits[i]->relativeBitsEl, codeBits));
      totalBits += qcElement[i]->grantedDynBits;
    }
  }

  if (codeBits != totalBits) {
    INT elMaxBits = cm->nElements - 1;
    INT elMinBits = cm->nElements - 1;

    for (i = (cm->nElements - 1); i >= 0; i--) {
      if (isAudioElement(cm->elInfo[i].elType)) {
        if (qcElement[i]->grantedDynBits >
            qcElement[elMaxBits]->grantedDynBits) {
          elMaxBits = i;
        }
        if (qcElement[i]->grantedDynBits <
            qcElement[elMinBits]->grantedDynBits) {
          elMinBits = i;
        }
      }
    }
    if (codeBits - totalBits > 0) {
      qcElement[elMinBits]->grantedDynBits += codeBits - totalBits;
    } else {
      qcElement[elMaxBits]->grantedDynBits += codeBits - totalBits;
    }
  }

  return 0; /* AAC_ENC_OK */
}

// FDKaacEnc_updateUsedDynBits — verbatim from qc_main.cpp:677-696.
static int FDKaacEnc_updateUsedDynBits_oracle(INT* sumDynBitsConsumed,
                                              QC_OUT_ELEMENT* qcElement[((8))],
                                              CHANNEL_MAPPING* cm) {
  INT i;

  *sumDynBitsConsumed = 0;

  for (i = 0; i < cm->nElements; i++) {
    if ((cm->elInfo[i].elType == ID_SCE) ||
        (cm->elInfo[i].elType == ID_CPE) ||
        (cm->elInfo[i].elType == ID_LFE)) {
      *sumDynBitsConsumed += qcElement[i]->dynBitsUsed;
    }
  }

  return 0; /* AAC_ENC_OK */
}

// FDKaacEnc_getTotalConsumedBits — verbatim from qc_main.cpp:712-736.
static INT FDKaacEnc_getTotalConsumedBits_oracle(QC_OUT** qcOut,
                                                 QC_OUT_ELEMENT* qcElement[(1)][((8))],
                                                 CHANNEL_MAPPING* cm,
                                                 INT globHdrBits, INT nSubFrames) {
  int c, i;
  int totalUsedBits = 0;

  for (c = 0; c < nSubFrames; c++) {
    int dataBits = 0;
    for (i = 0; i < cm->nElements; i++) {
      if ((cm->elInfo[i].elType == ID_SCE) ||
          (cm->elInfo[i].elType == ID_CPE) ||
          (cm->elInfo[i].elType == ID_LFE)) {
        dataBits += qcElement[c][i]->dynBitsUsed +
                    qcElement[c][i]->staticBitsUsed +
                    qcElement[c][i]->extBitsUsed;
      }
    }
    dataBits += qcOut[c]->globalExtBits;

    totalUsedBits += (8 - (dataBits) % 8) % 8;
    totalUsedBits += dataBits + globHdrBits;
  }
  return totalUsedBits;
}

// FDKaacEnc_updateFillBits — verbatim from qc_main.cpp:1168-1209.
static int FDKaacEnc_updateFillBits_oracle(QC_STATE* qcKernel, QC_OUT** qcOut) {
  switch (qcKernel->bitrateMode) {
    case QCDATA_BR_MODE_SFR:
      break;
    case QCDATA_BR_MODE_FF:
      break;
    case QCDATA_BR_MODE_VBR_1:
    case QCDATA_BR_MODE_VBR_2:
    case QCDATA_BR_MODE_VBR_3:
    case QCDATA_BR_MODE_VBR_4:
    case QCDATA_BR_MODE_VBR_5:
      qcOut[0]->totFillBits =
          (qcOut[0]->grantedDynBits - qcOut[0]->usedDynBits) & 7;
      qcOut[0]->totalBits = qcOut[0]->staticBits + qcOut[0]->usedDynBits +
                            qcOut[0]->totFillBits + qcOut[0]->elementExtBits +
                            qcOut[0]->globalExtBits;
      qcOut[0]->totFillBits +=
          (fixMax(0, qcKernel->minBitsPerFrame - qcOut[0]->totalBits) + 7) & ~7;
      break;
    case QCDATA_BR_MODE_CBR:
    case QCDATA_BR_MODE_INVALID:
    default: {
      INT bitResSpace = qcKernel->bitResTotMax - qcKernel->bitResTot;
      INT deltaBitRes = qcOut[0]->grantedDynBits - qcOut[0]->usedDynBits;
      qcOut[0]->totFillBits = fixMax(
          (deltaBitRes & 7), (deltaBitRes - (fixMax(0, bitResSpace - 7) & ~7)));
      qcOut[0]->totalBits = qcOut[0]->staticBits + qcOut[0]->usedDynBits +
                            qcOut[0]->totFillBits + qcOut[0]->elementExtBits +
                            qcOut[0]->globalExtBits;
      qcOut[0]->totFillBits +=
          (fixMax(0, qcKernel->minBitsPerFrame - qcOut[0]->totalBits) + 7) & ~7;
    } break;
  }

  return 0; /* AAC_ENC_OK */
}

// FDKaacEnc_updateBitres — verbatim from qc_main.cpp:1249-1274.
static void FDKaacEnc_updateBitres_oracle(QC_STATE* qcKernel, QC_OUT** qcOut) {
  switch (qcKernel->bitrateMode) {
    case QCDATA_BR_MODE_VBR_1:
    case QCDATA_BR_MODE_VBR_2:
    case QCDATA_BR_MODE_VBR_3:
    case QCDATA_BR_MODE_VBR_4:
    case QCDATA_BR_MODE_VBR_5:
      qcKernel->bitResTot =
          fMin(qcKernel->maxBitsPerFrame, qcKernel->bitResTotMax);
      break;
    case QCDATA_BR_MODE_CBR:
    case QCDATA_BR_MODE_SFR:
    case QCDATA_BR_MODE_INVALID:
    default: {
      int c = 0;
      {
        qcKernel->bitResTot += qcOut[c]->grantedDynBits -
                               (qcOut[c]->usedDynBits + qcOut[c]->totFillBits +
                                qcOut[c]->alignBits);
      }
      break;
    }
  }
}

// === C-ABI SHIMS for the Go test ===========================================

extern "C" {

int encframe_calc_frame_len(int bitRate, int sampleRate, int granuleLength,
                            int mode) {
  return FDKaacEnc_calcFrameLen_oracle(bitRate, sampleRate, granuleLength,
                                       (FRAME_LEN_RESULT_MODE)mode);
}

int encframe_frame_padding(int bitRate, int sampleRate, int granuleLength,
                           int* paddingRest) {
  return FDKaacEnc_framePadding_oracle(bitRate, sampleRate, granuleLength,
                                       paddingRest);
}

int encframe_adjust_bitrate(int* paddingRest, int* avgTotalBits, int bitRate,
                            int sampleRate, int granuleLength) {
  return FDKaacEnc_AdjustBitrate_oracle(paddingRest, avgTotalBits, bitRate,
                                        sampleRate, granuleLength);
}

int encframe_calc_max_value_in_sfb(int sfbCnt, int maxSfbPerGroup,
                                   int sfbPerGroup, int* sfbOffset,
                                   int16_t* quantSpectrum, unsigned int* maxValue) {
  return FDKaacEnc_calcMaxValueInSfb_oracle(sfbCnt, maxSfbPerGroup, sfbPerGroup,
                                            sfbOffset, (SHORT*)quantSpectrum,
                                            (UINT*)maxValue);
}

// encframe_bitres_redistribution drives the verbatim BitResRedistribution over
// a CHANNEL_MAPPING of nElements SCE elements, with per-element relativeBits and
// the QC reservoir levels supplied flat. It copies the resulting per-element
// bitResLevelEl / maxBitResBitsEl back out.
int encframe_bitres_redistribution(int nElements, int* relativeBits,
                                   int bitResTot, int bitResTotMax,
                                   int maxBitsPerFrame, int avgTotalBits,
                                   int* bitResLevelOut, int* maxBitResBitsOut) {
  QC_STATE qc;
  CHANNEL_MAPPING cm;
  ELEMENT_BITS eb[8];
  ELEMENT_BITS* ebp[8];
  memset(&qc, 0, sizeof(qc));
  memset(&cm, 0, sizeof(cm));
  memset(eb, 0, sizeof(eb));

  qc.bitResTot = bitResTot;
  qc.bitResTotMax = bitResTotMax;
  qc.maxBitsPerFrame = maxBitsPerFrame;
  cm.nElements = nElements;
  for (int i = 0; i < nElements; i++) {
    cm.elInfo[i].elType = ID_SCE;
    eb[i].relativeBitsEl = relativeBits[i];
    ebp[i] = &eb[i];
    qc.elementBits[i] = &eb[i];
  }

  int err = FDKaacEnc_BitResRedistribution_oracle(&qc, &cm, avgTotalBits);

  for (int i = 0; i < nElements; i++) {
    bitResLevelOut[i] = eb[i].bitResLevelEl;
    maxBitResBitsOut[i] = eb[i].maxBitResBitsEl;
  }
  return err;
}

// encframe_distribute_dyn_bits drives the verbatim distributeElementDynBits and
// updateUsedDynBits over nElements SCE elements, returning grantedDynBits[] and
// the summed usedDynBits (computed from the supplied per-element dynBitsUsed[]).
int encframe_distribute_dyn_bits(int nElements, int* relativeBits, int codeBits,
                                 int* dynBitsUsed, int* grantedDynBitsOut,
                                 int* sumDynBitsOut) {
  QC_STATE qc;
  CHANNEL_MAPPING cm;
  ELEMENT_BITS eb[8];
  QC_OUT_ELEMENT els[8];
  QC_OUT_ELEMENT* elp[8];
  memset(&qc, 0, sizeof(qc));
  memset(&cm, 0, sizeof(cm));
  memset(eb, 0, sizeof(eb));
  memset(els, 0, sizeof(els));

  cm.nElements = nElements;
  for (int i = 0; i < nElements; i++) {
    cm.elInfo[i].elType = ID_SCE;
    eb[i].relativeBitsEl = relativeBits[i];
    qc.elementBits[i] = &eb[i];
    els[i].dynBitsUsed = dynBitsUsed[i];
    elp[i] = &els[i];
  }

  int err = FDKaacEnc_distributeElementDynBits_oracle(&qc, elp, &cm, codeBits);

  int sumDyn = 0;
  FDKaacEnc_updateUsedDynBits_oracle(&sumDyn, elp, &cm);
  *sumDynBitsOut = sumDyn;

  for (int i = 0; i < nElements; i++) {
    grantedDynBitsOut[i] = els[i].grantedDynBits;
  }
  return err;
}

// encframe_total_consumed_bits drives the verbatim getTotalConsumedBits over a
// single sub frame of nElements SCE elements.
int encframe_total_consumed_bits(int nElements, int* dynBitsUsed,
                                 int* staticBitsUsed, int* extBitsUsed,
                                 int globalExtBits, int globHdrBits) {
  CHANNEL_MAPPING cm;
  QC_OUT qo;
  QC_OUT* qop[1];
  QC_OUT_ELEMENT els[8];
  QC_OUT_ELEMENT* grid[1][8];
  memset(&cm, 0, sizeof(cm));
  memset(&qo, 0, sizeof(qo));
  memset(els, 0, sizeof(els));

  cm.nElements = nElements;
  qo.globalExtBits = globalExtBits;
  qop[0] = &qo;
  for (int i = 0; i < nElements; i++) {
    cm.elInfo[i].elType = ID_SCE;
    els[i].dynBitsUsed = dynBitsUsed[i];
    els[i].staticBitsUsed = staticBitsUsed[i];
    els[i].extBitsUsed = extBitsUsed[i];
    grid[0][i] = &els[i];
  }

  return FDKaacEnc_getTotalConsumedBits_oracle(qop, grid, &cm, globHdrBits, 1);
}

// encframe_update_fill_bits drives the verbatim updateFillBits over qcOut[0].
// The relevant QC_STATE / QC_OUT fields are supplied flat and the resulting
// totFillBits / totalBits copied back.
void encframe_update_fill_bits(int bitrateMode, int minBitsPerFrame,
                               int bitResTot, int bitResTotMax,
                               int grantedDynBits, int usedDynBits,
                               int staticBits, int elementExtBits,
                               int globalExtBits, int* totFillBitsOut,
                               int* totalBitsOut) {
  QC_STATE qc;
  QC_OUT qo;
  QC_OUT* qop[1];
  memset(&qc, 0, sizeof(qc));
  memset(&qo, 0, sizeof(qo));

  qc.bitrateMode = (QCDATA_BR_MODE)bitrateMode;
  qc.minBitsPerFrame = minBitsPerFrame;
  qc.bitResTot = bitResTot;
  qc.bitResTotMax = bitResTotMax;
  qo.grantedDynBits = grantedDynBits;
  qo.usedDynBits = usedDynBits;
  qo.staticBits = staticBits;
  qo.elementExtBits = elementExtBits;
  qo.globalExtBits = globalExtBits;
  qop[0] = &qo;

  FDKaacEnc_updateFillBits_oracle(&qc, qop);

  *totFillBitsOut = qo.totFillBits;
  *totalBitsOut = qo.totalBits;
}

// encframe_update_bitres drives the verbatim updateBitres over qcOut[0],
// returning the new bitResTot.
int encframe_update_bitres(int bitrateMode, int bitResTot, int maxBitsPerFrame,
                           int bitResTotMax, int grantedDynBits, int usedDynBits,
                           int totFillBits, int alignBits) {
  QC_STATE qc;
  QC_OUT qo;
  QC_OUT* qop[1];
  memset(&qc, 0, sizeof(qc));
  memset(&qo, 0, sizeof(qo));

  qc.bitrateMode = (QCDATA_BR_MODE)bitrateMode;
  qc.bitResTot = bitResTot;
  qc.maxBitsPerFrame = maxBitsPerFrame;
  qc.bitResTotMax = bitResTotMax;
  qo.grantedDynBits = grantedDynBits;
  qo.usedDynBits = usedDynBits;
  qo.totFillBits = totFillBits;
  qo.alignBits = alignBits;
  qop[0] = &qo;

  FDKaacEnc_updateBitres_oracle(&qc, qop);

  return qc.bitResTot;
}

} // extern "C"
