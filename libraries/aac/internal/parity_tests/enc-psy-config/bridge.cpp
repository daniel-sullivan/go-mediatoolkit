// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE psychoacoustic
 * CONFIGURATION init FDKaacEnc_InitPsyConfiguration (libAACenc/src/
 * psy_configuration.cpp:534). This is the per-element psy config init psy_main.cpp
 * calls (psy_main.cpp:336/350): it fills PSY_CONFIGURATION — the scalefactor-band
 * layout (sfbCnt/sfbActive/sfbActiveLFE/sfbOffset), the masking spreading factors
 * (sfbMaskLow/HighFactor + their SprEn variants), the per-band PCM-quant
 * thresholds, the minimum-SNR ld-data, the pre-echo ratios
 * (maxAllowedIncreaseFactor / minRemainingThresholdFactor), the lowpass lines and
 * the level-dependent clipEnergy, plus allowIS/allowMS — that the encoder's
 * psychoacoustic model reads every frame.
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored psy_configuration.cpp (+ aacEnc_rom.cpp for the SFB ROM, FDK_trigFcts.cpp
 * for fixp_atan, fixpoint_math.cpp for fDivNorm/f2Pow/fPow/fLog2/schur_div,
 * FDK_tools_rom.cpp for the trig ROM, genericStds.cpp for FDKmemclear) as sibling
 * TUs, so the oracle is the real reference, NOT a hand-twin
 * (oracle_kind == real_vendored). It NEVER imports libraries/aac, so there is no
 * cross-package static-symbol clash (each parity package compiles its OWN copy of
 * the needed fdk C TUs — the same amalgamation-split reasoning the sibling
 * enc-psy-main / enc-block-switch oracles document). It MAY, and the test does,
 * import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL /
 * int16 FIXP_SGL Q-format quantity. The init is entirely integer (fixmul int64
 * products, arithmetic shifts, leading-bit counts, the table-free fixp_atan /
 * f2Pow / fPow / fDivNorm and the table-driven fLog2/CalcLdData), bit-identical
 * regardless of -ffp-contract or vectorization, with NO transcendental. So the
 * test asserts EXACT int32/int16 equality (the gate command still sets aac_strict
 * for consistency). Only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (see
 * cgo.go); the scalar FP flags come from the mise task env (CGO_CFLAGS).
 *
 * The bridge copies the filled C PSY_CONFIGURATION field-by-field into the flat
 * output struct EPARITY_PSY_CONF so the Go side compares each field against the
 * pure-Go PsyConfiguration without depending on C/Go struct layout matching. */

#include <stdint.h>
#include <string.h>

#include "psy_configuration.h"
#include "psy_const.h"
#include "aacenc.h"

extern "C" {

/* Flat mirror of the PSY_CONFIGURATION fields InitPsyConfiguration produces, in
 * a layout the Go side can read directly (fixed MAX_SFB array sizes). The
 * tnsConf/pnsConf are exported as raw byte blobs so the Go side can assert they
 * are left zeroed (FDKmemclear, never populated by InitPsyConfiguration). */
typedef struct {
  int32_t sfbCnt;
  int32_t sfbActive;
  int32_t sfbActiveLFE;
  int32_t sfbOffset[MAX_SFB + 1];

  int32_t filterbank;

  int32_t sfbPcmQuantThreshold[MAX_SFB];

  int32_t maxAllowedIncreaseFactor;
  int16_t minRemainingThresholdFactor;

  int32_t lowpassLine;
  int32_t lowpassLineLFE;
  int32_t clipEnergy;

  int32_t sfbMaskLowFactor[MAX_SFB];
  int32_t sfbMaskHighFactor[MAX_SFB];
  int32_t sfbMaskLowFactorSprEn[MAX_SFB];
  int32_t sfbMaskHighFactorSprEn[MAX_SFB];

  int32_t sfbMinSnrLdData[MAX_SFB];

  int32_t granuleLength;
  int32_t allowIS;
  int32_t allowMS;

  /* zero-check blobs for the embedded configs the init leaves cleared */
  int32_t tnsConfAllZero;
  int32_t pnsConfAllZero;
} EPARITY_PSY_CONF;

/* eparity_init_psy_configuration runs the genuine FDKaacEnc_InitPsyConfiguration
 * for the given parameters and copies the filled PSY_CONFIGURATION out into *out.
 * Returns the AAC_ENCODER_ERROR code (0 == AAC_ENC_OK). */
int eparity_init_psy_configuration(int bitrate, int samplerate, int bandwidth,
                                   int blocktype, int granuleLength, int useIS,
                                   int useMS, int filterbank,
                                   EPARITY_PSY_CONF *out) {
  PSY_CONFIGURATION psyConf;
  AAC_ENCODER_ERROR err = FDKaacEnc_InitPsyConfiguration(
      bitrate, samplerate, bandwidth, blocktype, granuleLength, useIS, useMS,
      &psyConf, (FB_TYPE)filterbank);

  memset(out, 0, sizeof(*out));
  if (err != AAC_ENC_OK) {
    return (int)err;
  }

  out->sfbCnt = psyConf.sfbCnt;
  out->sfbActive = psyConf.sfbActive;
  out->sfbActiveLFE = psyConf.sfbActiveLFE;
  for (int i = 0; i < MAX_SFB + 1; i++) out->sfbOffset[i] = psyConf.sfbOffset[i];

  out->filterbank = psyConf.filterbank;

  for (int i = 0; i < MAX_SFB; i++) {
    out->sfbPcmQuantThreshold[i] = (int32_t)psyConf.sfbPcmQuantThreshold[i];
    out->sfbMaskLowFactor[i] = (int32_t)psyConf.sfbMaskLowFactor[i];
    out->sfbMaskHighFactor[i] = (int32_t)psyConf.sfbMaskHighFactor[i];
    out->sfbMaskLowFactorSprEn[i] = (int32_t)psyConf.sfbMaskLowFactorSprEn[i];
    out->sfbMaskHighFactorSprEn[i] = (int32_t)psyConf.sfbMaskHighFactorSprEn[i];
    out->sfbMinSnrLdData[i] = (int32_t)psyConf.sfbMinSnrLdData[i];
  }

  out->maxAllowedIncreaseFactor = psyConf.maxAllowedIncreaseFactor;
  out->minRemainingThresholdFactor = (int16_t)psyConf.minRemainingThresholdFactor;

  out->lowpassLine = psyConf.lowpassLine;
  out->lowpassLineLFE = psyConf.lowpassLineLFE;
  out->clipEnergy = (int32_t)psyConf.clipEnergy;

  out->granuleLength = psyConf.granuleLength;
  out->allowIS = psyConf.allowIS;
  out->allowMS = psyConf.allowMS;

  /* InitPsyConfiguration FDKmemclears the whole struct and never repopulates
   * tnsConf/pnsConf, so they must be all-zero. Assert that here by OR-folding
   * each blob's bytes; the Go side checks for 0. */
  {
    const unsigned char *p = (const unsigned char *)&psyConf.tnsConf;
    int32_t acc = 0;
    for (size_t i = 0; i < sizeof(psyConf.tnsConf); i++) acc |= p[i];
    out->tnsConfAllZero = acc;
  }
  {
    const unsigned char *p = (const unsigned char *)&psyConf.pnsConf;
    int32_t acc = 0;
    for (size_t i = 0; i < sizeof(psyConf.pnsConf); i++) acc |= p[i];
    out->pnsConfAllZero = acc;
  }

  return (int)err;
}

} /* extern "C" */
