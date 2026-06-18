// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE stereo+TNS stage:
 *
 *   - FDKaacEnc_MsStereoProcessing (libAACenc/src/ms_stereo.cpp:109) — the M/S
 *     stereo decision: per-band ld-domain pe comparison of L/R vs mid/side, the
 *     in-place L/R->M/S rewrite of the MDCT spectrum, the energy/threshold
 *     copy-down, the msMask, and the frame-level msDigest (SI_MS_MASK_*).
 *   - the static TNS-encode reflection-coefficient quantizers
 *     FDKaacEnc_Parcor2Index / FDKaacEnc_Index2Parcor (and the
 *     FDKaacEnc_Search3/Search4 they call), aacEnc_tns.cpp:1141-1191.
 *
 * This TU #includes the GENUINE vendored ms_stereo.cpp AND aacEnc_tns.cpp
 * directly (NOT hand-twins) so the extern "C" shims below reach BOTH the public
 * FDKaacEnc_MsStereoProcessing symbol AND the four `static` TNS helpers — they
 * are static, so the only way to drive the real reference is from inside their
 * own translation unit. Because ms_stereo.cpp / aacEnc_tns.cpp are included
 * here, they must NOT also be compiled as separate sibling TUs (that would
 * doubly-define their public symbols). The ROM tables (aacEnc_rom.cpp) and the
 * fixed-point math (fixpoint_math.cpp) ARE separate sibling TUs (rom_cgo.cpp /
 * fixpoint_math_cgo.cpp) supplying the symbols aacEnc_tns.cpp references
 * (FDKaacEnc_tnsEncCoeff3/4 + tnsCoeff3/4Borders + the FDK_lpc helpers).
 * oracle_kind == real_vendored (the genuine FDKaacEnc_MsStereoProcessing is
 * linked; the static TNS quantizers are the genuine static functions reached via
 * #include, NOT a re-derivation).
 *
 * This TU NEVER imports libraries/aac, so there is no cross-package
 * static-symbol clash (the same amalgamation-split reasoning the sibling
 * enc-quantize / enc-psy-model oracles document). It MAY, and the test does,
 * import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL
 * / int16 FIXP_SGL Q-format. ms_stereo is fixMin/fixMax + arithmetic shifts; the
 * TNS quantizers are integer border comparisons + table indexing. Both are
 * bit-identical regardless of -ffp-contract or vectorization, with NO
 * transcendental and NO float. So they assert EXACT integer equality (the gate
 * command still sets aac_strict for consistency).
 */

#include <stdint.h>
#include <string.h>

#include "common_fix.h"
#include "psy_const.h"
#include "psy_data.h"
#include "interface.h"

/* Pull in the genuine vendored sources so their public + static symbols are all
 * visible to the shims in this same TU. */
#include "libfdk/libAACenc/src/ms_stereo.cpp"
#include "libfdk/libAACenc/src/aacEnc_tns.cpp"

extern "C" {

/* ---- M/S stereo decision --------------------------------------------------
 *
 * msparity_ms_stereo seeds two genuine PSY_DATA + two genuine PSY_OUT_CHANNEL
 * structs from the flat int32 input arrays, runs the genuine
 * FDKaacEnc_MsStereoProcessing, then copies every mutated field back out. The
 * per-band arrays are all length sfbCnt; the MDCT spectra are length
 * sfbOffset[sfbCnt]. Every "io" buffer is read in and written back so the test
 * compares the in-place mutation element-for-element.
 *
 * Layout note: PSY_DATA carries the .Long arrays inline (sfbEnergy.Long,
 * sfbThreshold.Long, sfbEnergyMS.Long, sfbSpreadEnergy.Long, sfbEnergyMSLdData,
 * mdctSpectrum pointer). PSY_OUT_CHANNEL carries sfbEnergyLdData /
 * sfbThresholdLdData as pointers (memory located in QC_OUT_CHANNEL); we point
 * them at caller-provided buffers.
 */
int msparity_ms_stereo(
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int allowMS,
    const int32_t *isBook, /* NULL allowed */
    /* io energies / thresholds / spreads (length sfbCnt) */
    int32_t *sfbEnergyLeft, int32_t *sfbEnergyRight,
    const int32_t *sfbEnergyMid, const int32_t *sfbEnergySide,
    int32_t *sfbThresholdLeft, int32_t *sfbThresholdRight,
    int32_t *sfbSpreadEnLeft, int32_t *sfbSpreadEnRight,
    int32_t *sfbEnergyLeftLd, int32_t *sfbEnergyRightLd,
    const int32_t *sfbEnergyMidLd, const int32_t *sfbEnergySideLd,
    int32_t *sfbThresholdLeftLd, int32_t *sfbThresholdRightLd,
    /* io MDCT spectra (length sfbOffset[sfbCnt]) */
    int32_t *mdctSpectrumLeft, int32_t *mdctSpectrumRight,
    const int32_t *sfbOffset,
    /* io msMask (length sfbCnt) */
    int32_t *msMask) {

  /* Genuine FDK structs, zero-initialised. */
  static PSY_DATA psyDataL, psyDataR;
  static PSY_OUT_CHANNEL psyOutL, psyOutR;
  memset(&psyDataL, 0, sizeof(psyDataL));
  memset(&psyDataR, 0, sizeof(psyDataR));
  memset(&psyOutL, 0, sizeof(psyOutL));
  memset(&psyOutR, 0, sizeof(psyOutR));

  /* PSY_OUT_CHANNEL ldData pointers + mdctSpectrum pointers live in the genuine
   * structs as pointers; point them at the caller buffers. */
  psyOutL.sfbEnergyLdData = (FIXP_DBL *)sfbEnergyLeftLd;
  psyOutR.sfbEnergyLdData = (FIXP_DBL *)sfbEnergyRightLd;
  psyOutL.sfbThresholdLdData = (FIXP_DBL *)sfbThresholdLeftLd;
  psyOutR.sfbThresholdLdData = (FIXP_DBL *)sfbThresholdRightLd;
  psyDataL.mdctSpectrum = (FIXP_DBL *)mdctSpectrumLeft;
  psyDataR.mdctSpectrum = (FIXP_DBL *)mdctSpectrumRight;

  /* Copy the inline .Long arrays in (length sfbCnt). */
  for (int i = 0; i < sfbCnt; i++) {
    psyDataL.sfbEnergy.Long[i] = (FIXP_DBL)sfbEnergyLeft[i];
    psyDataR.sfbEnergy.Long[i] = (FIXP_DBL)sfbEnergyRight[i];
    psyDataL.sfbEnergyMS.Long[i] = (FIXP_DBL)sfbEnergyMid[i];
    psyDataR.sfbEnergyMS.Long[i] = (FIXP_DBL)sfbEnergySide[i];
    psyDataL.sfbThreshold.Long[i] = (FIXP_DBL)sfbThresholdLeft[i];
    psyDataR.sfbThreshold.Long[i] = (FIXP_DBL)sfbThresholdRight[i];
    psyDataL.sfbSpreadEnergy.Long[i] = (FIXP_DBL)sfbSpreadEnLeft[i];
    psyDataR.sfbSpreadEnergy.Long[i] = (FIXP_DBL)sfbSpreadEnRight[i];
    psyDataL.sfbEnergyMSLdData[i] = (FIXP_DBL)sfbEnergyMidLd[i];
    psyDataR.sfbEnergyMSLdData[i] = (FIXP_DBL)sfbEnergySideLd[i];
  }

  PSY_DATA *psyData[2] = {&psyDataL, &psyDataR};
  PSY_OUT_CHANNEL *psyOutChannel[2] = {&psyOutL, &psyOutR};

  INT msDigest = 0;
  INT msMaskTmp[MAX_GROUPED_SFB];
  for (int i = 0; i < sfbCnt; i++) msMaskTmp[i] = (INT)msMask[i];

  FDKaacEnc_MsStereoProcessing(psyData, psyOutChannel, (const INT *)isBook,
                               &msDigest, msMaskTmp, (INT)allowMS, (INT)sfbCnt,
                               (INT)sfbPerGroup, (INT)maxSfbPerGroup,
                               (const INT *)sfbOffset);

  /* Copy every mutated inline / pointer field back out (length sfbCnt). The
   * ldData + mdctSpectrum buffers were mutated in place via the pointers. */
  for (int i = 0; i < sfbCnt; i++) {
    sfbEnergyLeft[i] = (int32_t)psyDataL.sfbEnergy.Long[i];
    sfbEnergyRight[i] = (int32_t)psyDataR.sfbEnergy.Long[i];
    sfbThresholdLeft[i] = (int32_t)psyDataL.sfbThreshold.Long[i];
    sfbThresholdRight[i] = (int32_t)psyDataR.sfbThreshold.Long[i];
    sfbSpreadEnLeft[i] = (int32_t)psyDataL.sfbSpreadEnergy.Long[i];
    sfbSpreadEnRight[i] = (int32_t)psyDataR.sfbSpreadEnergy.Long[i];
    msMask[i] = (int32_t)msMaskTmp[i];
  }

  return (int)msDigest;
}

/* ---- TNS-encode reflection-coefficient quantizers -------------------------
 *
 * msparity_parcor2index runs the genuine static FDKaacEnc_Parcor2Index over
 * `order` FIXP_LPC (int16) ParCor coefficients, returning the signed indices.
 * msparity_index2parcor runs the genuine static FDKaacEnc_Index2Parcor.
 */
void msparity_parcor2index(const int16_t *parcor, int32_t *index, int order,
                           int bitsPerCoeff) {
  /* FIXP_LPC == FIXP_SGL (int16) on the build target; INT == int32. */
  FDKaacEnc_Parcor2Index((const FIXP_LPC *)parcor, (INT *)index, (INT)order,
                         (INT)bitsPerCoeff);
}

void msparity_index2parcor(const int32_t *index, int16_t *parcor, int order,
                           int bitsPerCoeff) {
  FDKaacEnc_Index2Parcor((const INT *)index, (FIXP_LPC *)parcor, (INT)order,
                         (INT)bitsPerCoeff);
}

/* msparity_tns_rom publishes the genuine vendored TNS-encode ROM tables so the
 * test verifies the Go int16-narrowed transcription bit-for-bit. FIXP_LPC ==
 * FIXP_SGL (int16) on the build target; assert that assumption holds. */
void msparity_tns_rom(int16_t *encCoeff3, int16_t *coeff3Borders,
                      int16_t *encCoeff4, int16_t *coeff4Borders) {
  for (int i = 0; i < 8; i++) {
    encCoeff3[i] = (int16_t)FDKaacEnc_tnsEncCoeff3[i];
    coeff3Borders[i] = (int16_t)FDKaacEnc_tnsCoeff3Borders[i];
  }
  for (int i = 0; i < 16; i++) {
    encCoeff4[i] = (int16_t)FDKaacEnc_tnsEncCoeff4[i];
    coeff4Borders[i] = (int16_t)FDKaacEnc_tnsCoeff4Borders[i];
  }
}

} /* extern "C" */
