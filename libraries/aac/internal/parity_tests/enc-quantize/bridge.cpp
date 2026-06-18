// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE quantizer —
 * libAACenc/src/quantize.cpp. These kernels turn the windowed MDCT spectrum +
 * a per-SFB scalefactor into the quantized SHORT spectrum
 * (FDKaacEnc_QuantizeSpectrum quantize.cpp:278, via the static
 * FDKaacEnc_quantizeLines quantize.cpp:116), the inverse-quantized
 * reconstruction (static FDKaacEnc_invQuantizeLines quantize.cpp:180), and the
 * two ld-domain cost metrics the scalefactor search minimises
 * (FDKaacEnc_calcSfbDist quantize.cpp:312, FDKaacEnc_calcSfbQuantEnergyAndDist
 * quantize.cpp:361).
 *
 * This TU #includes the GENUINE vendored quantize.cpp directly (NOT a hand-twin)
 * so the extern "C" shims below can reach BOTH the public symbols AND the two
 * `static` helpers (FDKaacEnc_quantizeLines / FDKaacEnc_invQuantizeLines) — they
 * are static, so the only way to drive the real reference is from inside its own
 * translation unit. Because quantize.cpp is included here, it must NOT be
 * compiled as a separate sibling TU (that would doubly-define the non-static
 * public symbols and clash at link). The ROM tables (aacEnc_rom.cpp) and the
 * LD-domain log2 (fixpoint_math.cpp) ARE separate sibling TUs (rom_cgo.cpp /
 * fixpoint_math_cgo.cpp). oracle_kind == real_vendored.
 *
 * This TU NEVER imports libraries/aac, so there is no cross-package
 * static-symbol clash (the same amalgamation-split reasoning the sibling
 * enc-psy-model / enc-block-switch oracles document). It MAY, and the test does,
 * import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL
 * / int16 SHORT Q-format. These kernels are entirely integer (leading-bit
 * counts, arithmetic shifts, int64-product fixmul, table-driven ^3/4 / ^4/3
 * mantissa lookups + fLog2), bit-identical regardless of -ffp-contract or
 * vectorization, with NO transcendental and NO float. So they assert EXACT int32
 * equality (the gate command still sets aac_strict for consistency).
 */

#include <stdint.h>

#include "common_fix.h"
#include "aacEnc_rom.h"
#include "quantize.h"

/* Pull in the genuine vendored quantize.cpp so the static helpers
 * FDKaacEnc_quantizeLines / FDKaacEnc_invQuantizeLines and the public symbols
 * are all visible to the shims in this same TU. */
#include "libfdk/libAACenc/src/quantize.cpp"

extern "C" {

/* qparity_quantize_lines runs the genuine static FDKaacEnc_quantizeLines. */
void qparity_quantize_lines(int gain, int noOfLines, const int32_t *mdctSpectrum,
                            int16_t *quaSpectrum, int dZoneQuantEnable) {
  FDKaacEnc_quantizeLines((INT)gain, (INT)noOfLines,
                          (const FIXP_DBL *)mdctSpectrum, (SHORT *)quaSpectrum,
                          (INT)dZoneQuantEnable);
}

/* qparity_inv_quantize_lines runs the genuine static
 * FDKaacEnc_invQuantizeLines. */
void qparity_inv_quantize_lines(int gain, int noOfLines, int16_t *quantSpectrum,
                                int32_t *mdctSpectrum) {
  FDKaacEnc_invQuantizeLines((INT)gain, (INT)noOfLines, (SHORT *)quantSpectrum,
                             (FIXP_DBL *)mdctSpectrum);
}

/* qparity_quantize_spectrum runs the genuine FDKaacEnc_QuantizeSpectrum. */
void qparity_quantize_spectrum(int sfbCnt, int maxSfbPerGroup, int sfbPerGroup,
                               const int32_t *sfbOffset,
                               const int32_t *mdctSpectrum, int globalGain,
                               const int32_t *scalefactors,
                               int16_t *quantizedSpectrum, int dZoneQuantEnable) {
  FDKaacEnc_QuantizeSpectrum(
      (INT)sfbCnt, (INT)maxSfbPerGroup, (INT)sfbPerGroup, (const INT *)sfbOffset,
      (const FIXP_DBL *)mdctSpectrum, (INT)globalGain, (const INT *)scalefactors,
      (SHORT *)quantizedSpectrum, (INT)dZoneQuantEnable);
}

/* qparity_calc_sfb_dist runs the genuine FDKaacEnc_calcSfbDist. */
int32_t qparity_calc_sfb_dist(const int32_t *mdctSpectrum, int16_t *quantSpectrum,
                              int noOfLines, int gain, int dZoneQuantEnable) {
  return (int32_t)FDKaacEnc_calcSfbDist(
      (const FIXP_DBL *)mdctSpectrum, (SHORT *)quantSpectrum, (INT)noOfLines,
      (INT)gain, (INT)dZoneQuantEnable);
}

/* qparity_calc_sfb_quant_energy_and_dist runs the genuine
 * FDKaacEnc_calcSfbQuantEnergyAndDist. */
void qparity_calc_sfb_quant_energy_and_dist(int32_t *mdctSpectrum,
                                            int16_t *quantSpectrum, int noOfLines,
                                            int gain, int32_t *en,
                                            int32_t *dist) {
  FDKaacEnc_calcSfbQuantEnergyAndDist((FIXP_DBL *)mdctSpectrum,
                                      (SHORT *)quantSpectrum, (INT)noOfLines,
                                      (INT)gain, (FIXP_DBL *)en, (FIXP_DBL *)dist);
}

/* qparity_quant_rom publishes the genuine vendored quantizer ROM tables so the
 * test verifies the Go transcription bit-for-bit. On the build platform FIXP_QTD
 * == FIXP_SGL (int16) (ARCH_PREFER_MULT_32x16), so mTab_3_4 / quantTableQ /
 * quantTableE are int16; mTab_4_3Elc is unconditional FIXP_DBL (int32). Assert
 * the int16 assumption holds. */
void qparity_quant_rom(int16_t *mTab34, int16_t *quantTableQ,
                       int16_t *quantTableE, int32_t *mTab43) {
#ifndef ARCH_PREFER_MULT_32x16
#error "expected ARCH_PREFER_MULT_32x16 on the build platform (aarch64 -> FIXP_QTD == FIXP_SGL)"
#endif
  for (int i = 0; i < MANT_SIZE; i++) mTab34[i] = (int16_t)FDKaacEnc_mTab_3_4[i];
  for (int i = 0; i < 4; i++) quantTableQ[i] = (int16_t)FDKaacEnc_quantTableQ[i];
  for (int i = 0; i < 4; i++) quantTableE[i] = (int16_t)FDKaacEnc_quantTableE[i];
  for (int i = 0; i < 512; i++) mTab43[i] = (int32_t)FDKaacEnc_mTab_4_3Elc[i];
}

} /* extern "C" */
