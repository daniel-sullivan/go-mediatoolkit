// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC AAC-LC raw_data_block ics-parse
 * stage (libAACdec/src/channelinfo.cpp IcsRead/IcsReadMaxSfb and
 * libAACdec/src/block.cpp CBlock_ReadSectionData). This translation unit
 * provides the extern "C" bridge the Go test calls. It uses the GENUINE
 * vendored code where the function is self-contained, and a VERBATIM twin
 * where the genuine function is bolted to a struct too large to fabricate:
 *
 *   - getSamplingRateInfo, IcsRead, IcsReadMaxSfb are the ONLY functions
 *     channelinfo.cpp defines (it includes only channelinfo.h / aac_rom.h /
 *     aac_ram.h / FDK_bitstream.h). channelinfo.cpp is compiled as a sibling TU
 *     (channelinfo_cgo.cpp) and these are linked WHOLE — the genuine reference.
 *     IcsRead takes only CIcsInfo* + SamplingRateInfo* + flags, both small
 *     structs we fabricate here, so no twin is needed.
 *
 *   - CBlock_ReadSectionData (block.cpp:326) reads out of the giant
 *     CAacDecoderChannelInfo (pDynData->aCodeBook, icsInfo, RawDataInfo) and
 *     compiling block.cpp whole would drag the entire decoder (HCR/TNS/PNS/
 *     arith/iMDCT) at link time — the cross-module drag the per-package oracle
 *     discipline forbids. The oracle instead carries a VERBATIM copy of its body
 *     below (block.cpp:326-423, byte-for-byte the vendored source — only the
 *     symbol name is suffixed _oracle and the field accesses retargeted to the
 *     fabricated minimal twin). The control flow, bit reads, escape loop, line
 *     limits, codebook-legality checks and the codebook-stamp loop are the
 *     genuine reference. For AAC-LC (flags == 0) the HCR side-info branch is
 *     never entered.
 *
 *   - The AAC ROM (sfbOffsetTables that getSamplingRateInfo selects) comes from
 *     aac_rom.cpp, compiled as a sibling TU (aac_rom_cgo.cpp). The FDK bit
 *     buffer back-end (FDK_get32 …) comes from FDK_bitbuffer.cpp + genericStds
 *     sibling TUs.
 *
 * The whole AAC island is fenced behind aacfdk; the oracle additionally
 * requires cgo. See libfdk/COPYING for the Fraunhofer FDK-AAC license.
 */

#include "channelinfo.h"
#include "aac_rom.h"
#include "FDK_bitstream.h"

#include "oracle_bridge.h"

/* ---- VERBATIM twin of CBlock_ReadSectionData (block.cpp:326-423) ----
 *
 * Retargeted from CAacDecoderChannelInfo* to its three load-bearing pieces
 * (the icsInfo, the flat codebook array, and CommonWindow). The HCR side-info
 * collection (flags & AC_ER_HCR) writes to local scratch since AAC-LC never
 * enters it; numberSection is reported back. Body otherwise byte-for-byte. */
static AAC_DECODER_ERROR ReadSectionData_oracle(
    HANDLE_FDK_BITSTREAM bs, CIcsInfo *pIcsInfo,
    const SamplingRateInfo *pSamplingRateInfo, UCHAR commonWindow,
    const UINT flags, UCHAR *pCodeBook /* [8*16] */, int *pNumberSection) {
  int top, band;
  int sect_len, sect_len_incr;
  int group;
  UCHAR sect_cb;
  /* HCR input (long) — local scratch, AAC-LC never collects */
  SHORT aNumLinesInSec[MAX_SFB_HCR];
  SHORT *pNumLinesInSec = aNumLinesInSec;
  int numLinesInSecIdx = 0;
  UCHAR aHcrCodeBook[MAX_SFB_HCR];
  UCHAR *pHcrCodeBook = aHcrCodeBook;
  const SHORT *BandOffsets =
      GetScaleFactorBandOffsets(pIcsInfo, pSamplingRateInfo);
  *pNumberSection = 0;
  AAC_DECODER_ERROR ErrorStatus = AAC_DEC_OK;

  FDKmemclear(pCodeBook, sizeof(UCHAR) * (8 * 16));

  const int nbits = (IsLongBlock(pIcsInfo) == 1) ? 5 : 3;

  int sect_esc_val = (1 << nbits) - 1;

  UCHAR ScaleFactorBandsTransmitted =
      GetScaleFactorBandsTransmitted(pIcsInfo);
  for (group = 0; group < GetWindowGroups(pIcsInfo); group++) {
    for (band = 0; band < ScaleFactorBandsTransmitted;) {
      sect_len = 0;
      if (flags & AC_ER_VCB11) {
        sect_cb = (UCHAR)FDKreadBits(bs, 5);
      } else
        sect_cb = (UCHAR)FDKreadBits(bs, 4);

      if (((flags & AC_ER_VCB11) == 0) || (sect_cb < 11) ||
          ((sect_cb > 11) && (sect_cb < 16))) {
        sect_len_incr = FDKreadBits(bs, nbits);
        while (sect_len_incr == sect_esc_val) {
          sect_len += sect_esc_val;
          sect_len_incr = FDKreadBits(bs, nbits);
        }
      } else {
        sect_len_incr = 1;
      }

      sect_len += sect_len_incr;

      top = band + sect_len;

      if (flags & AC_ER_HCR) {
        /* HCR input (long) -- collecting sideinfo (for HCR-_long_ only) */
        if (numLinesInSecIdx >= MAX_SFB_HCR) {
          return AAC_DEC_PARSE_ERROR;
        }
        if (top > (int)GetNumberOfScaleFactorBands(pIcsInfo,
                                                   pSamplingRateInfo)) {
          return AAC_DEC_PARSE_ERROR;
        }
        pNumLinesInSec[numLinesInSecIdx] = BandOffsets[top] - BandOffsets[band];
        numLinesInSecIdx++;
        if (sect_cb == BOOKSCL) {
          return AAC_DEC_INVALID_CODE_BOOK;
        } else {
          *pHcrCodeBook++ = sect_cb;
        }
        (*pNumberSection)++;
      }

      /* Check spectral line limits */
      if (IsLongBlock(pIcsInfo)) {
        if (top > 64) {
          return AAC_DEC_DECODE_FRAME_ERROR;
        }
      } else { /* short block */
        if (top + group * 16 > (8 * 16)) {
          return AAC_DEC_DECODE_FRAME_ERROR;
        }
      }

      /* Check if decoded codebook index is feasible */
      if ((sect_cb == BOOKSCL) ||
          ((sect_cb == INTENSITY_HCB || sect_cb == INTENSITY_HCB2) &&
           commonWindow == 0)) {
        return AAC_DEC_INVALID_CODE_BOOK;
      }

      /* Store codebook index */
      for (; band < top; band++) {
        pCodeBook[group * 16 + band] = sect_cb;
      }
    }
  }

  return ErrorStatus;
}

extern "C" void fparity_ics_parse(const uint8_t *buf, int bufSize,
                                  uint32_t validBits, uint32_t samplesPerFrame,
                                  uint32_t samplingRateIndex,
                                  uint32_t samplingRate, uint8_t commonWindow,
                                  uint32_t flags, fparity_ics_result *out) {
  FDK_BITSTREAM bsStruct;
  HANDLE_FDK_BITSTREAM bs = &bsStruct;
  FDKinitBitStream(bs, (UCHAR *)buf, (UINT)bufSize, validBits, BS_READER);

  SamplingRateInfo sri;
  FDKmemclear(&sri, sizeof(sri));
  getSamplingRateInfo(&sri, samplesPerFrame, samplingRateIndex, samplingRate);

  CIcsInfo ics;
  FDKmemclear(&ics, sizeof(ics));

  AAC_DECODER_ERROR err = IcsRead(bs, &ics, &sri, flags);

  UCHAR codeBook[8 * 16];
  FDKmemclear(codeBook, sizeof(codeBook));
  int numberSection = 0;

  if (err == AAC_DEC_OK) {
    err = ReadSectionData_oracle(bs, &ics, &sri, commonWindow, flags, codeBook,
                                 &numberSection);
  }

  for (int i = 0; i < 8; i++) out->windowGroupLength[i] = ics.WindowGroupLength[i];
  out->windowGroups = ics.WindowGroups;
  out->valid = ics.Valid;
  out->windowShape = ics.WindowShape;
  out->windowSequence = (uint8_t)ics.WindowSequence;
  out->maxSfBands = ics.MaxSfBands;
  out->scaleFactorGrouping = ics.ScaleFactorGrouping;
  out->totalSfBands = ics.TotalSfBands;
  for (int i = 0; i < 8 * 16; i++) out->codeBook[i] = codeBook[i];
  out->numberSection = numberSection;
  out->errorCode = (int32_t)err;
  /* FDKgetBitCnt == BitNdx - BitsInCache (the unconsumed cache bits), matching
   * the Go side's bitNdx - bitsInCache. */
  out->bitPos = bs->hBitBuf.BitNdx - bs->BitsInCache;
}
