/* SPDX-License-Identifier: FDK-AAC */
/* Shared bridge declarations for the AAC-LC ics-parse parity oracle.
 * Included by both the cgo preamble in cgo.go and the oracle TU
 * oracle_ics_parse_cgo.cpp so the fparity_ics_result layout is identical on
 * both sides. Fields mirror CIcsInfo (channelinfo.h:167) plus the section-data
 * codebook array (pDynData->aCodeBook), the numberSection counter, the final
 * AAC_DECODER_ERROR, and the post-parse bit-consumption position. */
#ifndef FPARITY_ICS_BRIDGE_H
#define FPARITY_ICS_BRIDGE_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Flattened result of running IcsRead followed by CBlock_ReadSectionData over a
 * fabricated bit buffer, exactly as CChannelElement_Read sequences the ics_info
 * and section_data raw-data-block items. All integer; compared bit-for-bit. */
typedef struct {
  uint8_t windowGroupLength[8]; /* CIcsInfo.WindowGroupLength[8] */
  uint8_t windowGroups;         /* CIcsInfo.WindowGroups */
  uint8_t valid;                /* CIcsInfo.Valid */
  uint8_t windowShape;          /* CIcsInfo.WindowShape */
  uint8_t windowSequence;       /* CIcsInfo.WindowSequence (BLOCK_TYPE) */
  uint8_t maxSfBands;           /* CIcsInfo.MaxSfBands */
  uint8_t scaleFactorGrouping;  /* CIcsInfo.ScaleFactorGrouping */
  uint8_t totalSfBands;         /* CIcsInfo.TotalSfBands */
  uint8_t codeBook[8 * 16];     /* pDynData->aCodeBook */
  int32_t numberSection;        /* specificTo.aac.numberSection */
  int32_t errorCode;            /* AAC_DECODER_ERROR */
  uint32_t bitPos;              /* FDKgetBitCnt after the parse */
} fparity_ics_result;

/* fparity_ics_parse runs the GENUINE vendored getSamplingRateInfo + IcsRead
 * (from channelinfo.cpp) then a verbatim twin of CBlock_ReadSectionData over a
 * bit buffer initialised from buf[0:bufSize] with validBits valid bits, and
 * fills *out. samplesPerFrame / samplingRateIndex / samplingRate select the
 * scalefactor-band ROM; commonWindow gates the intensity-codebook legality
 * check; flags is the AC_* bitmask (0 for AAC-LC). */
void fparity_ics_parse(const uint8_t *buf, int bufSize, uint32_t validBits,
                       uint32_t samplesPerFrame, uint32_t samplingRateIndex,
                       uint32_t samplingRate, uint8_t commonWindow,
                       uint32_t flags, fparity_ics_result *out);

#ifdef __cplusplus
}
#endif

#endif /* FPARITY_ICS_BRIDGE_H */
