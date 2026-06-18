// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC AAC-LC decode "dequant" stage
 * (libAACdec/src/block.cpp CBlock_ReadScaleFactorData / CBlock_ScaleSpectralData
 * / CBlock_InverseQuantizeSpectralData, plus libAACdec/src/aacdec_pns.cpp
 * CPns_Read). This translation unit provides the extern "C" bridge the Go test
 * calls. It uses the GENUINE vendored code where the function is self-contained,
 * and a VERBATIM twin where the genuine function is bolted to the giant
 * CAacDecoderChannelInfo struct, mirroring the ics-parse oracle's discipline:
 *
 *   - getSamplingRateInfo + the CIcsInfo accessor inlines (GetWindowGroups,
 *     GetWindowGroupLength, GetScaleFactorBandsTransmitted,
 *     GetScaleFactorBandOffsets, GetScaleFactorBandsTotal, IsLongBlock) come from
 *     channelinfo.cpp / channelinfo.h, linked WHOLE (channelinfo.cpp is compiled
 *     as a sibling TU). They take only CIcsInfo* / SamplingRateInfo*, both small
 *     structs fabricated here.
 *   - EvaluatePower43 (block.h:247) and GetScaleFromValue is NOT needed here;
 *     the inverse-quantize driver derives the scale inline exactly as block.cpp
 *     does (EvaluatePower43 + CntLeadingZeros). EvaluatePower43 is the genuine
 *     FDK_INLINE pulled in by including block.h.
 *   - CBlock_DecodeHuffmanWord / CBlock_DecodeHuffmanWordCB (block.h:300/327) are
 *     genuine FDK inlines from block.h, used by the scalefactor read and CPns_Read.
 *   - The three drivers + CPns_Read + the static InverseQuantizeBand / maxabs_D
 *     (and getScalefactor for the TNS branch) read out of CAacDecoderChannelInfo
 *     / pDynData; compiling block.cpp or aacdec_pns.cpp whole would drag the
 *     entire decoder (HCR/TNS/PNS-noise/arith/iMDCT) at link time — the
 *     cross-module drag the per-package oracle discipline forbids. The oracle
 *     instead carries VERBATIM copies of their bodies below (byte-for-byte the
 *     vendored source — only the symbol name is suffixed _oracle and the
 *     CAacDecoderChannelInfo* field accesses retargeted to the fabricated minimal
 *     twin: a CIcsInfo, the flat aCodeBook/aScaleFactor/aSfbScale arrays, the
 *     specScale array, the spectrum, the global gain, and an inactive CTnsData).
 *     The control flow, bit reads, delta accumulators, band loops, scale
 *     derivation and down-shift are the genuine reference.
 *   - The quant ROM (InverseQuantTable / MantissaTable / ExponentTable) and the
 *     BOOKSCL Huffman codebook come from aac_rom.cpp (sibling TU). The FDK bit
 *     buffer back-end comes from FDK_bitbuffer.cpp + genericStds.cpp siblings.
 *
 * This file NEVER imports libraries/aac (which would link a second copy of the
 * whole reference); it stands alone alongside nativeaac.
 *
 * FP-parity: the entire dequant stage is INTEGER / fixed-point — FIXP_DBL is a
 * Q1.31 int32, the (4/3)-power interpolation reads an integer ROM, fMultDiv2 is
 * an int64 product shifted back to int32, exponents are int16, and every scale is
 * an arithmetic shift. It is bit-identical regardless of -ffp-contract /
 * vectorization, so no transcendental shim and no aac_strict gate are needed for
 * FP reasons. See block.cpp / aacdec_pns.cpp / aac_rom.cpp.
 *
 * The whole AAC island is fenced behind aacfdk; the oracle additionally requires
 * cgo. See libfdk/COPYING for the Fraunhofer FDK-AAC license.
 */

#include <stdint.h>
#include <string.h>

#include "block.h"
#include "channelinfo.h"
#include "aac_rom.h"
#include "FDK_bitstream.h"

#include "oracle_bridge.h"

/* NOISE_OFFSET (aacdec_pns.cpp:113), per ISO 14496-3 p. 175. */
#define NOISE_OFFSET_ORACLE 90

/* Minimal twin of the load-bearing CAacDecoderChannelInfo / pDynData pieces the
 * three drivers + CPns_Read read. Field names mirror the C so the verbatim
 * bodies retarget by a single struct-member swap. */
typedef struct {
  CIcsInfo icsInfo;
  uint8_t aCodeBook[8 * 16];    /* pDynData->aCodeBook */
  int16_t aScaleFactor[8 * 16]; /* pDynData->aScaleFactor */
  int16_t aSfbScale[8 * 16];    /* pDynData->aSfbScale */
  int16_t specScale[8];         /* specScale */
  int32_t *spectrum;            /* SPEC(pSpectralCoefficient, window, gl) base */
  int granuleLength;            /* SPEC stride */
  uint8_t globalGain;           /* RawDataInfo.GlobalGain */
  /* PNS running state (CPnsData subset) */
  int pnsCurrentEnergy;
  uint8_t pnsActive;
} DequantTwin;

/* ---- Verbatim twin of CPns_Read (aacdec_pns.cpp:211) ----
 * Retargeted: PnsData fields read from the twin's running state; the genuine
 * CBlock_DecodeHuffmanWord (block.h) and FDKreadBits are used. */
static void CPns_Read_oracle(DequantTwin *t, HANDLE_FDK_BITSTREAM bs,
                             const CodeBookDescription *hcb, SHORT *pScaleFactor,
                             UCHAR global_gain, int band, int group) {
  int delta;
  UINT pns_band = group * 16 + band;

  if (t->pnsActive) {
    /* Next PNS band case */
    delta = CBlock_DecodeHuffmanWord(bs, hcb) - 60;
  } else {
    /* First PNS band case */
    int noiseStartValue = FDKreadBits(bs, 9);

    delta = noiseStartValue - 256;
    t->pnsActive = 1;
    t->pnsCurrentEnergy = global_gain - NOISE_OFFSET_ORACLE;
  }

  t->pnsCurrentEnergy += delta;
  pScaleFactor[pns_band] = t->pnsCurrentEnergy;
}

/* ---- Verbatim twin of CBlock_ReadScaleFactorData (block.cpp:158) ----
 * Retargeted from CAacDecoderChannelInfo* to the twin. The pCodeBook +=16 /
 * pScaleFactor +=16 per-group advance is preserved. */
static AAC_DECODER_ERROR CBlock_ReadScaleFactorData_oracle(
    DequantTwin *t, HANDLE_FDK_BITSTREAM bs, UINT flags) {
  int temp;
  int band;
  int group;
  int position = 0; /* accu for intensity delta coding */
  int factor = t->globalGain; /* accu for scale factor delta coding */
  UCHAR *pCodeBook = t->aCodeBook;
  SHORT *pScaleFactor = t->aScaleFactor;
  const CodeBookDescription *hcb = &AACcodeBookDescriptionTable[BOOKSCL];

  const USHORT(*CodeBook)[HuffmanEntries] = hcb->CodeBook;

  int ScaleFactorBandsTransmitted =
      GetScaleFactorBandsTransmitted(&t->icsInfo);
  for (group = 0; group < GetWindowGroups(&t->icsInfo); group++) {
    for (band = 0; band < ScaleFactorBandsTransmitted; band++) {
      switch (pCodeBook[band]) {
        case ZERO_HCB: /* zero book */
          pScaleFactor[band] = 0;
          break;

        default: /* decode scale factor */
          if (!((flags & (AC_USAC | AC_RSVD50 | AC_RSV603DA)) && band == 0 &&
                group == 0)) {
            temp = CBlock_DecodeHuffmanWordCB(bs, CodeBook);
            factor += temp - 60; /* MIDFAC 1.5 dB */
          }
          pScaleFactor[band] = factor - 100;
          break;

        case INTENSITY_HCB: /* intensity steering */
        case INTENSITY_HCB2:
          temp = CBlock_DecodeHuffmanWordCB(bs, CodeBook);
          position += temp - 60;
          pScaleFactor[band] = position - 100;
          break;

        case NOISE_HCB: /* PNS */
          if (flags & (AC_MPEGD_RES | AC_USAC | AC_RSVD50 | AC_RSV603DA)) {
            return AAC_DEC_PARSE_ERROR;
          }
          CPns_Read_oracle(t, bs, hcb, t->aScaleFactor, t->globalGain, band,
                           group);
          break;
      }
    }
    pCodeBook += 16;
    pScaleFactor += 16;
  }

  return AAC_DEC_OK;
}

/* ---- Verbatim copy of InverseQuantizeBand (block.cpp:436), renamed _oracle. */
static inline void InverseQuantizeBand_oracle(
    FIXP_DBL *RESTRICT spectrum, const FIXP_DBL *RESTRICT InverseQuantTabler,
    const FIXP_DBL *RESTRICT MantissaTabler,
    const SCHAR *RESTRICT ExponentTabler, INT noLines, INT scale) {
  scale = scale + 1; /* +1 to compensate fMultDiv2 shift-right in loop */

  FIXP_DBL *RESTRICT ptr = spectrum;
  FIXP_DBL signedValue;

  for (INT i = noLines; i--;) {
    if ((signedValue = *ptr++) != FL2FXCONST_DBL(0)) {
      FIXP_DBL value = fAbs(signedValue);
      UINT freeBits = CntLeadingZeros(value);
      UINT exponent = 32 - freeBits;

      UINT x = (UINT)(LONG)value << (INT)freeBits;
      x <<= 1; /* shift out sign bit to avoid masking later on */
      UINT tableIndex = x >> 24;
      x = (x >> 20) & 0x0F;

      UINT r0 = (UINT)(LONG)InverseQuantTabler[tableIndex + 0];
      UINT r1 = (UINT)(LONG)InverseQuantTabler[tableIndex + 1];
      UINT temp = (r1 - r0) * x + (r0 << 4);

      value = fMultDiv2((FIXP_DBL)temp, MantissaTabler[exponent]);

      /* + 1 compensates fMultDiv2() */
      scaleValueInPlace(&value, scale + ExponentTabler[exponent]);

      signedValue = (signedValue < (FIXP_DBL)0) ? -value : value;
      ptr[-1] = signedValue;
    }
  }
}

/* ---- Verbatim copy of maxabs_D (block.cpp:471), renamed _oracle. */
static inline FIXP_DBL maxabs_D_oracle(const FIXP_DBL *pSpectralCoefficient,
                                       const int noLines) {
  FIXP_DBL locMax = (FIXP_DBL)0;
  int i;
  DWORD_ALIGNED(pSpectralCoefficient);
  for (i = noLines; i-- > 0;) {
    locMax = fMax(fixp_abs(pSpectralCoefficient[i]), locMax);
  }
  return locMax;
}

/* ---- Verbatim twin of CBlock_InverseQuantizeSpectralData (block.cpp:487) ----
 * Retargeted to the twin. The active_band_search path is fed 0 / NULL (AAC-LC
 * decode); EvaluatePower43 is the genuine block.h inline. SPEC() is the
 * window-major stride spectrum + BandOffsets[band]. */
static AAC_DECODER_ERROR CBlock_InverseQuantizeSpectralData_oracle(
    DequantTwin *t, SamplingRateInfo *pSamplingRateInfo, UCHAR *band_is_noise,
    UCHAR active_band_search) {
  int window, group, groupwin, band;
  int ScaleFactorBandsTransmitted =
      GetScaleFactorBandsTransmitted(&t->icsInfo);
  UCHAR *RESTRICT pCodeBook = t->aCodeBook;
  SHORT *RESTRICT pSfbScale = t->aSfbScale;
  SHORT *RESTRICT pScaleFactor = t->aScaleFactor;
  const SHORT *RESTRICT BandOffsets =
      GetScaleFactorBandOffsets(&t->icsInfo, pSamplingRateInfo);
  const SHORT total_bands = GetScaleFactorBandsTotal(&t->icsInfo);

  FDKmemclear(t->aSfbScale, (8 * 16) * sizeof(SHORT));

  for (window = 0, group = 0; group < GetWindowGroups(&t->icsInfo); group++) {
    for (groupwin = 0;
         groupwin < GetWindowGroupLength(&t->icsInfo, group);
         groupwin++, window++) {
      for (band = 0; band < ScaleFactorBandsTransmitted; band++) {
        FIXP_DBL *pSpectralCoefficient =
            (t->spectrum + window * t->granuleLength) + BandOffsets[band];
        FIXP_DBL locMax;

        const int noLines = BandOffsets[band + 1] - BandOffsets[band];
        const int bnds = group * 16 + band;

        if ((pCodeBook[bnds] == ZERO_HCB) ||
            (pCodeBook[bnds] == INTENSITY_HCB) ||
            (pCodeBook[bnds] == INTENSITY_HCB2))
          continue;

        if (pCodeBook[bnds] == NOISE_HCB) {
          pSfbScale[window * 16 + band] = (pScaleFactor[bnds] >> 2) + 1;
          continue;
        }

        locMax = maxabs_D_oracle(pSpectralCoefficient, noLines);

        if (active_band_search) {
          if (locMax != FIXP_DBL(0)) {
            band_is_noise[group * 16 + band] = 0;
          }
        }

        if (fixp_abs(locMax) > (FIXP_DBL)MAX_QUANTIZED_VALUE) {
          return AAC_DEC_PARSE_ERROR;
        }

        int msb = pScaleFactor[bnds] >> 2;

        if (locMax != FIXP_DBL(0)) {
          int lsb = pScaleFactor[bnds] & 0x03;

          int scale = EvaluatePower43(&locMax, lsb);

          scale = CntLeadingZeros(locMax) - scale - 2;

          pSfbScale[window * 16 + band] = msb - scale;
          InverseQuantizeBand_oracle(pSpectralCoefficient, InverseQuantTable,
                                     MantissaTable[lsb], ExponentTable[lsb],
                                     noLines, scale);
        } else {
          pSfbScale[window * 16 + band] = msb;
        }
      } /* for band */

      SHORT start_clear = BandOffsets[ScaleFactorBandsTransmitted];
      SHORT end_clear = BandOffsets[total_bands];
      int diff_clear = (int)(end_clear - start_clear);
      FIXP_DBL *pSpectralCoefficient =
          (t->spectrum + window * t->granuleLength) + start_clear;
      FDKmemclear(pSpectralCoefficient, diff_clear * sizeof(FIXP_DBL));
    }
  }

  return AAC_DEC_OK;
}

/* ---- Verbatim twin of CBlock_ScaleSpectralData (block.cpp:217) ----
 * Retargeted to the twin. TNS is inactive (no CTnsData), so the TNS-headroom
 * branch (block.cpp:247-294) is never entered — it is therefore omitted, exactly
 * matching the no-TNS Go path under test (scaleSpectralData with an inactive
 * CTnsData). The window/group loop, the per-window max-sfb reduce, the specScale
 * store and the per-band down-shift are byte-for-byte the vendored source. */
static void CBlock_ScaleSpectralData_oracle(DequantTwin *t, UCHAR maxSfbs,
                                            SamplingRateInfo *pSamplingRateInfo) {
  int band;
  int window;
  const SHORT *RESTRICT pSfbScale = t->aSfbScale;
  SHORT *RESTRICT pSpecScale = t->specScale;
  int groupwin, group;
  const SHORT *RESTRICT BandOffsets =
      GetScaleFactorBandOffsets(&t->icsInfo, pSamplingRateInfo);

  FDKmemclear(pSpecScale, 8 * sizeof(SHORT));

  for (window = 0, group = 0; group < GetWindowGroups(&t->icsInfo); group++) {
    for (groupwin = 0;
         groupwin < GetWindowGroupLength(&t->icsInfo, group);
         groupwin++, window++) {
      int SpecScale_window = pSpecScale[window];
      FIXP_DBL *pSpectrum = t->spectrum + window * t->granuleLength;

      /* find scaling for current window */
      for (band = 0; band < maxSfbs; band++) {
        SpecScale_window =
            fMax(SpecScale_window, (int)pSfbScale[window * 16 + band]);
      }

      /* (TNS branch omitted — inactive on this path; see header note) */

      /* store scaling of current window */
      pSpecScale[window] = SpecScale_window;

      for (band = 0; band < maxSfbs; band++) {
        int scale = fMin(DFRACT_BITS - 1,
                         SpecScale_window - pSfbScale[window * 16 + band]);
        if (scale) {
          int max_index = BandOffsets[band + 1];
          for (int index = BandOffsets[band]; index < max_index; index++) {
            pSpectrum[index] >>= scale;
          }
        }
      }
    }
  }
}

extern "C" void fparity_dequant(
    const uint8_t *scaleFactorBuf, int sfBufSize, uint32_t sfValidBits,
    uint32_t samplesPerFrame, uint32_t samplingRateIndex, uint32_t samplingRate,
    uint8_t globalGain, uint32_t flags, uint8_t windowSequence,
    uint8_t windowGroups, const uint8_t *windowGroupLength,
    uint8_t scaleFactorGrouping, uint8_t maxSfBands, const uint8_t *codeBook,
    int32_t *rawSpectrum, int specLen, fparity_dequant_result *out) {
  FDK_BITSTREAM bsStruct;
  HANDLE_FDK_BITSTREAM bs = &bsStruct;
  FDKinitBitStream(bs, (UCHAR *)scaleFactorBuf, (UINT)sfBufSize, sfValidBits,
                   BS_READER);

  SamplingRateInfo sri;
  FDKmemclear(&sri, sizeof(sri));
  getSamplingRateInfo(&sri, samplesPerFrame, samplingRateIndex, samplingRate);

  DequantTwin t;
  memset(&t, 0, sizeof(t));

  /* Rebuild the CIcsInfo from the already-parsed window/grouping fields. */
  for (int i = 0; i < 8; i++) t.icsInfo.WindowGroupLength[i] = windowGroupLength[i];
  t.icsInfo.WindowGroups = windowGroups;
  t.icsInfo.Valid = 1;
  t.icsInfo.WindowSequence = (BLOCK_TYPE)windowSequence;
  t.icsInfo.MaxSfBands = maxSfBands;
  t.icsInfo.ScaleFactorGrouping = scaleFactorGrouping;
  if (IsLongBlock(&t.icsInfo)) {
    t.icsInfo.TotalSfBands = sri.NumberOfScaleFactorBands_Long;
  } else {
    t.icsInfo.TotalSfBands = sri.NumberOfScaleFactorBands_Short;
  }

  for (int i = 0; i < 8 * 16; i++) t.aCodeBook[i] = codeBook[i];
  t.spectrum = rawSpectrum;
  t.granuleLength =
      ((BLOCK_TYPE)windowSequence == BLOCK_SHORT) ? (int)(samplesPerFrame / 8)
                                                  : (int)samplesPerFrame;
  t.globalGain = globalGain;

  AAC_DECODER_ERROR readErr =
      CBlock_ReadScaleFactorData_oracle(&t, bs, flags);

  AAC_DECODER_ERROR invErr =
      CBlock_InverseQuantizeSpectralData_oracle(&t, &sri, NULL, 0);

  CBlock_ScaleSpectralData_oracle(&t, t.icsInfo.MaxSfBands, &sri);

  for (int i = 0; i < 8 * 16; i++) {
    out->scaleFactor[i] = t.aScaleFactor[i];
    out->sfbScale[i] = t.aSfbScale[i];
  }
  for (int i = 0; i < 8; i++) out->specScale[i] = t.specScale[i];
  out->readSfErr = (int32_t)readErr;
  out->invQuantErr = (int32_t)invErr;
  (void)specLen;
}
