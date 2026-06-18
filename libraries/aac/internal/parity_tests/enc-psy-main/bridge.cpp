// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE psychoacoustic
 * DRIVER leaf kernels assembled by FDKaacEnc_psyMain (libAACenc/src/
 * psy_main.cpp): energy spreading (spreading.cpp:105, psy_main.cpp:950/1014),
 * pre-echo control (pre_echo_control.cpp:106/117, psy_main.cpp:987), tonality
 * (tonality.cpp:121, psy_main.cpp:759) and short-block grouping
 * (grp_data.cpp:118, psy_main.cpp:1047). Together these are the spreading /
 * tonality / pre-echo / grouping pieces of the full psy analysis that produces
 * the PsyOut SFB thresholds the quantizer targets.
 *
 * This TU provides the extern "C" bridges the Go test calls; it links the
 * GENUINE vendored spreading.cpp / pre_echo_control.cpp / tonality.cpp /
 * grp_data.cpp (+ chaosmeasure.cpp + fixpoint_math.cpp for the tonality
 * dependency chain) as sibling TUs, so the oracle is the real reference, NOT a
 * hand-twin (oracle_kind == real_vendored). It NEVER imports libraries/aac, so
 * there is no cross-package static-symbol clash (each parity package compiles
 * its OWN copy of the needed fdk C TUs — the same amalgamation-split reasoning
 * the sibling enc-block-switch / enc-analysis-filterbank / enc-psy-model
 * oracles document). It MAY, and the test does, import the pure-Go
 * internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32
 * FIXP_DBL/FIXP_SGL Q-format with carried block exponents. These kernels are
 * entirely integer (fixmul int64 products, arithmetic shifts, leading-bit
 * counts, the table-driven fLog2/chaos schur_div), bit-identical regardless of
 * -ffp-contract or vectorization, with NO transcendental. So the test asserts
 * EXACT int32 equality (the gate command still sets aac_strict for
 * consistency). Only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (see
 * cgo.go); the scalar FP flags come from the mise task env (CGO_CFLAGS).
 */

#include <stdint.h>
#include <string.h>

#include "common_fix.h"
#include "spreading.h"
#include "pre_echo_control.h"
#include "tonality.h"
#include "psy_const.h"
#include "interface.h"
#include "grp_data.h"

extern "C" {

/* eparity_spreading_max runs the genuine FDKaacEnc_SpreadingMax over an int32
 * (FIXP_DBL) spread-energy array of pbCnt bands, in place. */
void eparity_spreading_max(int pbCnt, const int32_t *maskLowFactor,
                           const int32_t *maskHighFactor,
                           int32_t *pbSpreadEnergy) {
  FDKaacEnc_SpreadingMax(pbCnt, (const FIXP_DBL *)maskLowFactor,
                         (const FIXP_DBL *)maskHighFactor,
                         (FIXP_DBL *)pbSpreadEnergy);
}

/* eparity_init_pre_echo_control runs the genuine FDKaacEnc_InitPreEchoControl
 * and returns the resulting mdctScalenm1 / calcPreEcho through out params. */
void eparity_init_pre_echo_control(int32_t *pbThresholdNm1,
                                   const int32_t *sfbPcmQuantThreshold,
                                   int numPb, int *mdctScalenm1,
                                   int *calcPreEcho) {
  /* InitPreEchoControl reads sfbPcmQuantThreshold; copy into the (non-const)
   * source buffer it expects. */
  FDKaacEnc_InitPreEchoControl((FIXP_DBL *)pbThresholdNm1, calcPreEcho, numPb,
                               (FIXP_DBL *)sfbPcmQuantThreshold, mdctScalenm1);
}

/* eparity_pre_echo_control runs the genuine FDKaacEnc_PreEchoControl, updating
 * pbThresholdNm1 (carried state) and pbThreshold in place and returning the
 * updated mdctScalenm1 through *mdctScalenm1. */
void eparity_pre_echo_control(int32_t *pbThresholdNm1, int calcPreEcho,
                              int numPb, int maxAllowedIncreaseFactor,
                              int16_t minRemainingThresholdFactor,
                              int32_t *pbThreshold, int mdctScale,
                              int *mdctScalenm1) {
  FDKaacEnc_PreEchoControl((FIXP_DBL *)pbThresholdNm1, calcPreEcho, numPb,
                           maxAllowedIncreaseFactor,
                           (FIXP_SGL)minRemainingThresholdFactor,
                           (FIXP_DBL *)pbThreshold, mdctScale, mdctScalenm1);
}

/* eparity_calculate_full_tonality runs the genuine
 * FDKaacEnc_CalculateFullTonality, writing sfbTonality[0:sfbCnt] (int16
 * FIXP_SGL). */
void eparity_calculate_full_tonality(int32_t *spectrum, int *sfbMaxScaleSpec,
                                     int32_t *sfbEnergyLD64,
                                     int16_t *sfbTonality, int sfbCnt,
                                     const int *sfbOffset, int usePns) {
  FDKaacEnc_CalculateFullTonality(
      (FIXP_DBL *)spectrum, (INT *)sfbMaxScaleSpec, (FIXP_DBL *)sfbEnergyLD64,
      (FIXP_SGL *)sfbTonality, sfbCnt, (const INT *)sfbOffset, usePns);
}

/* eparity_group_short_data runs the genuine FDKaacEnc_groupShortData. The four
 * SFB unions (threshold/energy/MS/spread) are passed as flat int32 buffers of
 * TRANS_FAC*MAX_SFB_SHORT (=120) cells each — exactly the union footprint — so
 * the Go side can hand its own flat union backing across unchanged. mdctSpectrum
 * is granuleLength cells, in-out. Returns maxSfbPerGroup. */
int eparity_group_short_data(int32_t *mdctSpectrum, int32_t *sfbThreshold,
                             int32_t *sfbEnergy, int32_t *sfbEnergyMS,
                             int32_t *sfbSpreadEnergy, int sfbCnt, int sfbActive,
                             const int *sfbOffset, const int32_t *sfbMinSnrLdData,
                             int *groupedSfbOffset,
                             int32_t *groupedSfbMinSnrLdData, int noOfGroups,
                             const int *groupLen, int granuleLength) {
  int maxSfbPerGroup = 0;
  FDKaacEnc_groupShortData(
      (FIXP_DBL *)mdctSpectrum, (SFB_THRESHOLD *)sfbThreshold,
      (SFB_ENERGY *)sfbEnergy, (SFB_ENERGY *)sfbEnergyMS,
      (SFB_ENERGY *)sfbSpreadEnergy, sfbCnt, sfbActive, (const INT *)sfbOffset,
      (const FIXP_DBL *)sfbMinSnrLdData, (INT *)groupedSfbOffset,
      &maxSfbPerGroup, (FIXP_DBL *)groupedSfbMinSnrLdData, noOfGroups,
      (const INT *)groupLen, granuleLength);
  return maxSfbPerGroup;
}

} /* extern "C" */

