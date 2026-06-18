// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE-side intensity
 * stereo processing tool (libAACenc/src/intensity.cpp):
 *
 *   - FDKaacEnc_IntensityStereoProcessing (intensity.cpp:614) -> the channel-pair
 *     tool that decides per-SFB whether to code the right channel as an intensity
 *     direction of the left, collapses the pair onto the left channel, sets
 *     isBook/isScale, updates msMask/msDigest, and switches off PNS on IS SFBs. It
 *     calls the static FDKaacEnc_initIsParams / FDKaacEnc_prepareIntensityDecision
 *     / FDKaacEnc_finalizeIntensityDecision / calcSfbMaxScale within the same TU.
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored intensity.cpp + fixpoint_math.cpp (fDivNorm) + FDK_tools_rom.cpp
 * (invCount/GetInvInt) + genericStds.cpp (FDKmemclear) + scale.cpp (out-of-line
 * scaleValue) as sibling TUs, so the oracle is the real reference, NOT a
 * hand-twin (oracle_kind == real_vendored). It NEVER imports libraries/aac, so
 * there is no cross-package static-symbol clash (each parity package compiles its
 * OWN copy of the needed fdk C TUs). It MAY, and the test does, import the pure-Go
 * internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL /
 * int16 FIXP_SGL Q-format quantity. intensity.cpp is entirely integer (fMult int64
 * products, arithmetic shifts, leading-bit counts, the table-driven sqrtFixp /
 * fDivNorm / GetInvInt kernels), bit-identical regardless of -ffp-contract or
 * vectorization. So the test asserts EXACT int equality (the gate command still
 * sets aac_strict for consistency). Only -I / -D / -Wno-* live in the in-source
 * #cgo CFLAGS (see cgo.go); the scalar FP flags come from the mise task env. */

#include <stdint.h>
#include <string.h>

#include "intensity.h"
#include "aacenc_pns.h"
#include "psy_const.h"

extern "C" {

/* eparity_intensity_stereo_processing runs the genuine
 * FDKaacEnc_IntensityStereoProcessing on the given inputs (all FIXP_DBL/INT
 * arrays). The per-SFB arrays are MAX_GROUPED_SFB wide; the spectrum arrays are
 * specLen wide. Arrays that the kernel modifies in place are passed as separate
 * In/Out buffers so the Go side can compare. pnsPresent != 0 supplies a [L,R]
 * PNS_DATA pair (driven by pnsFlagLIn/pnsFlagRIn); pnsPresent == 0 passes a NULL
 * pnsData[0] (the C `if (pnsData[0])` guard). */
void eparity_intensity_stereo_processing(
    const int32_t *sfbEnergyLeftIn, const int32_t *sfbEnergyRightIn,
    const int32_t *mdctSpectrumLeftIn, const int32_t *mdctSpectrumRightIn,
    const int32_t *sfbThresholdLeftIn, const int32_t *sfbThresholdRightIn,
    const int32_t *sfbThresholdLdDataRightIn, const int32_t *sfbSpreadEnLeftIn,
    const int32_t *sfbSpreadEnRightIn, const int32_t *sfbEnergyLdDataLeftIn,
    const int32_t *sfbEnergyLdDataRightIn, int msDigestIn, const int *msMaskIn,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, const int *sfbOffset,
    int allowIS, int pnsPresent, const int *pnsFlagLIn, const int *pnsFlagRIn,
    int specLen,
    /* outputs */
    int32_t *mdctSpectrumLeftOut, int32_t *mdctSpectrumRightOut,
    int32_t *sfbEnergyRightOut, int32_t *sfbThresholdRightOut,
    int32_t *sfbThresholdLdDataRightOut, int32_t *sfbSpreadEnRightOut,
    int *isBookOut, int *isScaleOut, int *msMaskOut, int *msDigestOut,
    int *pnsFlagLOut, int *pnsFlagROut) {

  /* working copies of the in/out arrays the kernel mutates */
  FIXP_DBL sfbEnergyLeft[MAX_GROUPED_SFB];
  FIXP_DBL sfbEnergyRight[MAX_GROUPED_SFB];
  FIXP_DBL sfbThresholdLeft[MAX_GROUPED_SFB];
  FIXP_DBL sfbThresholdRight[MAX_GROUPED_SFB];
  FIXP_DBL sfbThresholdLdDataRight[MAX_GROUPED_SFB];
  FIXP_DBL sfbSpreadEnLeft[MAX_GROUPED_SFB];
  FIXP_DBL sfbSpreadEnRight[MAX_GROUPED_SFB];
  FIXP_DBL sfbEnergyLdDataLeft[MAX_GROUPED_SFB];
  FIXP_DBL sfbEnergyLdDataRight[MAX_GROUPED_SFB];
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    sfbEnergyLeft[i] = (FIXP_DBL)sfbEnergyLeftIn[i];
    sfbEnergyRight[i] = (FIXP_DBL)sfbEnergyRightIn[i];
    sfbThresholdLeft[i] = (FIXP_DBL)sfbThresholdLeftIn[i];
    sfbThresholdRight[i] = (FIXP_DBL)sfbThresholdRightIn[i];
    sfbThresholdLdDataRight[i] = (FIXP_DBL)sfbThresholdLdDataRightIn[i];
    sfbSpreadEnLeft[i] = (FIXP_DBL)sfbSpreadEnLeftIn[i];
    sfbSpreadEnRight[i] = (FIXP_DBL)sfbSpreadEnRightIn[i];
    sfbEnergyLdDataLeft[i] = (FIXP_DBL)sfbEnergyLdDataLeftIn[i];
    sfbEnergyLdDataRight[i] = (FIXP_DBL)sfbEnergyLdDataRightIn[i];
  }

  /* spectrum working copies (specLen wide, capped at the test maximum 1024) */
  static FIXP_DBL mdctSpectrumLeft[1024];
  static FIXP_DBL mdctSpectrumRight[1024];
  for (int i = 0; i < specLen; i++) {
    mdctSpectrumLeft[i] = (FIXP_DBL)mdctSpectrumLeftIn[i];
    mdctSpectrumRight[i] = (FIXP_DBL)mdctSpectrumRightIn[i];
  }

  INT msMask[MAX_GROUPED_SFB];
  INT isBook[MAX_GROUPED_SFB];
  INT isScale[MAX_GROUPED_SFB];
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    msMask[i] = msMaskIn[i];
    isBook[i] = 0;
    isScale[i] = 0;
  }
  INT msDigest = msDigestIn;

  PNS_DATA pnsDataL, pnsDataR;
  memset(&pnsDataL, 0, sizeof(pnsDataL));
  memset(&pnsDataR, 0, sizeof(pnsDataR));
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    pnsDataL.pnsFlag[i] = pnsFlagLIn[i];
    pnsDataR.pnsFlag[i] = pnsFlagRIn[i];
  }
  PNS_DATA *pnsData[2];
  if (pnsPresent) {
    pnsData[0] = &pnsDataL;
    pnsData[1] = &pnsDataR;
  } else {
    pnsData[0] = NULL;
    pnsData[1] = NULL;
  }

  FDKaacEnc_IntensityStereoProcessing(
      sfbEnergyLeft, sfbEnergyRight, mdctSpectrumLeft, mdctSpectrumRight,
      sfbThresholdLeft, sfbThresholdRight, sfbThresholdLdDataRight,
      sfbSpreadEnLeft, sfbSpreadEnRight, sfbEnergyLdDataLeft,
      sfbEnergyLdDataRight, &msDigest, msMask, sfbCnt, sfbPerGroup,
      maxSfbPerGroup, sfbOffset, allowIS, isBook, isScale, pnsData);

  for (int i = 0; i < specLen; i++) {
    mdctSpectrumLeftOut[i] = (int32_t)mdctSpectrumLeft[i];
    mdctSpectrumRightOut[i] = (int32_t)mdctSpectrumRight[i];
  }
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    sfbEnergyRightOut[i] = (int32_t)sfbEnergyRight[i];
    sfbThresholdRightOut[i] = (int32_t)sfbThresholdRight[i];
    sfbThresholdLdDataRightOut[i] = (int32_t)sfbThresholdLdDataRight[i];
    sfbSpreadEnRightOut[i] = (int32_t)sfbSpreadEnRight[i];
    isBookOut[i] = isBook[i];
    isScaleOut[i] = isScale[i];
    msMaskOut[i] = msMask[i];
    pnsFlagLOut[i] = pnsDataL.pnsFlag[i];
    pnsFlagROut[i] = pnsDataR.pnsFlag[i];
  }
  *msDigestOut = msDigest;
}

} /* extern "C" */
