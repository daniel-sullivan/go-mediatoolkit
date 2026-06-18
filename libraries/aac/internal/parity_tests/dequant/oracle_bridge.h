/* SPDX-License-Identifier: FDK-AAC */
/* Shared bridge declarations for the AAC-LC dequant parity oracle.
 * Included by both the cgo preamble in cgo.go and the oracle TU
 * oracle_dequant_cgo.cpp so the fparity_dequant_result layout is identical on
 * both sides. The dequant stage is CBlock_ReadScaleFactorData (block.cpp:158) →
 * CBlock_InverseQuantizeSpectralData (block.cpp:487) → CBlock_ScaleSpectralData
 * (block.cpp:217): it turns the parsed section codebooks + raw quantized
 * spectrum into scaled FIXP_DBL spectral coefficients with per-band and
 * per-window block exponents, for one channel. All outputs are integer and
 * compared bit-for-bit (EXACT int32 equality, no tolerance). */
#ifndef FPARITY_DEQUANT_BRIDGE_H
#define FPARITY_DEQUANT_BRIDGE_H

#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Flattened result of running the three dequant drivers over a fabricated
 * channel context. spectrum is filled by the caller (its length is passed
 * separately); the per-(group,band) scalefactors, per-(window,sfb) and
 * per-window block exponents and the two driver return codes are reported. */
typedef struct {
  int16_t scaleFactor[8 * 16]; /* pDynData->aScaleFactor[group*16+band] */
  int16_t sfbScale[8 * 16];    /* pDynData->aSfbScale[window*16+band] */
  int16_t specScale[8];        /* pAacDecoderChannelInfo->specScale[window] */
  int32_t readSfErr;           /* CBlock_ReadScaleFactorData return code */
  int32_t invQuantErr;         /* CBlock_InverseQuantizeSpectralData return */
} fparity_dequant_result;

/* fparity_dequant runs the GENUINE vendored getSamplingRateInfo (channelinfo.cpp)
 * to resolve the scalefactor-band ROM, then verbatim twins of the three dequant
 * drivers (and CPns_Read) over the fabricated context:
 *
 *   - scaleFactorBuf[0:sfBufSize] / sfValidBits — the bitstream the scalefactor
 *     read consumes (a power-of-two buffer).
 *   - the window/grouping fields mirror the already-parsed CIcsInfo.
 *   - codeBook[8*16] is the flat section codebook layout.
 *   - rawSpectrum[0:specLen] is the quantized MDCT buffer, window-major with the
 *     long/short granule stride; it is inverse-quantized in place and the scaled
 *     result is written back into rawSpectrum.
 *
 * TNS is inactive (no-TNS path). flags is the AC_* bitmask (0 for AAC-LC). */
void fparity_dequant(const uint8_t *scaleFactorBuf, int sfBufSize,
                     uint32_t sfValidBits, uint32_t samplesPerFrame,
                     uint32_t samplingRateIndex, uint32_t samplingRate,
                     uint8_t globalGain, uint32_t flags, uint8_t windowSequence,
                     uint8_t windowGroups, const uint8_t *windowGroupLength,
                     uint8_t scaleFactorGrouping, uint8_t maxSfBands,
                     const uint8_t *codeBook, int32_t *rawSpectrum, int specLen,
                     fparity_dequant_result *out);

#ifdef __cplusplus
}
#endif

#endif /* FPARITY_DEQUANT_BRIDGE_H */
