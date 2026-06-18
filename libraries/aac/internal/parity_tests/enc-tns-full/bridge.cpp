// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE TNS DECISION
 * driver: FDKaacEnc_TnsDetect (libAACenc/src/aacenc_tns.cpp:766) and its
 * dependency chain
 *   FDKaacEnc_MergedAutoCorrelation (aacenc_tns.cpp:619, static)
 *     -> FDKaacEnc_ScaleUpSpectrum / FDKaacEnc_CalcAutoCorrValue /
 *        FDKaacEnc_AutoCorrNormFac (all static)
 *   CLpc_AutoToParcor (FDK_lpc.cpp:431, public)
 *   FDKaacEnc_Parcor2Index (aacenc_tns.cpp:1164, static)
 *
 * This TU #includes the GENUINE vendored aacenc_tns.cpp directly (NOT a
 * hand-twin) so the extern "C" shims below reach BOTH the public
 * FDKaacEnc_TnsDetect / FDKaacEnc_InitTnsConfiguration symbols AND the static
 * helpers (MergedAutoCorrelation, the autocorr kernels) — static symbols are
 * only reachable from inside their own translation unit, via this #include.
 * Because aacenc_tns.cpp is included here it must NOT also be compiled as a
 * separate sibling TU (that would doubly-define its public symbols). The ROM
 * tables (aacEnc_rom.cpp), the LeRoux-Gueguen ParCor analysis (FDK_lpc.cpp), the
 * fixed-point math (fixpoint_math.cpp), scale.cpp, FDK_tools_rom.cpp and
 * genericStds.cpp ARE separate sibling TUs (the support_*_cgo.cpp files)
 * supplying the symbols aacenc_tns.cpp references (CLpc_AutoToParcor /
 * CLpc_Analysis / CLpc_ParcorToLpc / fDivNorm / fPow / invSqrtNorm2 / fMultNorm
 * + the TNS ROM tables + p_FDKaacEnc_*_long_1024). oracle_kind == real_vendored
 * (the genuine FDKaacEnc_TnsDetect + CLpc_AutoToParcor are linked; the static
 * autocorrelation helpers are the genuine static functions reached via #include,
 * NOT a re-derivation of the Go port).
 *
 * This TU NEVER imports libraries/aac, so there is no cross-package
 * static-symbol clash (the same amalgamation-split reasoning the sibling
 * enc-stereo-tns / enc-psy-main oracles document). It MAY, and the test does,
 * import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL
 * / int16 FIXP_LPC Q-format. The whole TNS decision is integer arithmetic
 * (count-leading-bits, arithmetic shifts, schur division, the arm8 fixmul_DD,
 * invSqrtNorm2) with NO float and NO transcendental on the AAC-LC long-block
 * path (the only float initializer, FDKaacEnc_CalcGaussWindow, is used solely
 * for the 480/512 LD granule lengths, which this slice does not exercise — for
 * granuleLength 1024 the acfWindow is the integer ROM table acfWindowLong). So
 * the oracle asserts EXACT integer equality (the gate command still sets
 * aac_strict for consistency).
 */

#include <stdint.h>
#include <string.h>

#include "common_fix.h"
#include "psy_const.h"
#include "psy_configuration.h"
#include "aacenc_tns.h"
#include "aacEnc_rom.h"
#include "aacenc.h"

/* Pull in the genuine vendored source so its public + static symbols are all
 * visible to the shims in this same TU. */
#include "libfdk/libAACenc/src/aacenc_tns.cpp"

extern "C" {

/* Flat mirror of the TNS_CONFIG fields the decision reads, plus the resulting
 * TNS_INFO + TNS_SUBBLOCK_INFO the decision writes. Pointer-free POD so cgo can
 * read every field. Mirrors nativeaac.TNSConfig / TNSInfo / TNSSubblockInfo. */
typedef struct {
  /* confTab */
  int32_t filterEnabled[2];
  int32_t threshOn[2];
  int32_t filterStartFreq[2];
  int32_t tnsLimitOrder[2];
  int32_t tnsFilterDirection[2];
  int32_t acfSplit[2];
  int32_t tnsTimeResolution[2];
  int32_t seperateFiltersAllowed;
  /* top-level */
  int32_t isLowDelay;
  int32_t tnsActive;
  int32_t maxOrder;
  int32_t coefRes;
  int32_t acfWindow[2][TNS_MAX_ORDER + 3 + 1];
  int32_t lpcStartBand[2];
  int32_t lpcStartLine[2];
  int32_t lpcStopBand;
  int32_t lpcStopLine;
} tnsconf_flat;

typedef struct {
  int32_t numOfFilters;
  int32_t coefRes;
  int32_t length[2];
  int32_t order[2];
  int32_t direction[2];
  int32_t coef[2][TNS_MAX_ORDER];
  /* subblock info */
  int32_t sbTnsActive[2];
  int32_t sbPredictionGain[2];
  int32_t filtersMerged;
} tnsinfo_flat;

static void export_conf(const TNS_CONFIG *tC, tnsconf_flat *out) {
  for (int f = 0; f < 2; f++) {
    out->filterEnabled[f] = (int32_t)tC->confTab.filterEnabled[f];
    out->threshOn[f] = (int32_t)tC->confTab.threshOn[f];
    out->filterStartFreq[f] = (int32_t)tC->confTab.filterStartFreq[f];
    out->tnsLimitOrder[f] = (int32_t)tC->confTab.tnsLimitOrder[f];
    out->tnsFilterDirection[f] = (int32_t)tC->confTab.tnsFilterDirection[f];
    out->acfSplit[f] = (int32_t)tC->confTab.acfSplit[f];
    out->tnsTimeResolution[f] = (int32_t)tC->confTab.tnsTimeResolution[f];
    out->lpcStartBand[f] = (int32_t)tC->lpcStartBand[f];
    out->lpcStartLine[f] = (int32_t)tC->lpcStartLine[f];
    for (int i = 0; i < TNS_MAX_ORDER + 3 + 1; i++) {
      out->acfWindow[f][i] = (int32_t)tC->acfWindow[f][i];
    }
  }
  out->seperateFiltersAllowed = (int32_t)tC->confTab.seperateFiltersAllowed;
  out->isLowDelay = (int32_t)tC->isLowDelay;
  out->tnsActive = (int32_t)tC->tnsActive;
  out->maxOrder = (int32_t)tC->maxOrder;
  out->coefRes = (int32_t)tC->coefRes;
  out->lpcStopBand = (int32_t)tC->lpcStopBand;
  out->lpcStopLine = (int32_t)tC->lpcStopLine;
}

/* tnsparity_build_config builds a genuine TNS_CONFIG for an AAC-LC long block at
 * the given bitRate/sampleRate/channels by:
 *   1. building a genuine PSY_CONFIGURATION sfbOffset from the real ROM
 *      p_FDKaacEnc_44100_long_1024 / p_FDKaacEnc_48000_long_1024 sfbWidth via
 *      the exact GetSfBandTab cumulative-sum logic (psy_configuration.cpp:246),
 *   2. calling the genuine FDKaacEnc_InitTnsConfiguration.
 * It returns sfbCnt (== psyConf.sfbActive) and exports the config; -1 on a
 * sample rate this test does not seed. */
int tnsparity_build_config(int bitRate, int sampleRate, int channels,
                           int active, tnsconf_flat *out) {
  const SFB_PARAM_LONG *pLong = NULL;
  if (sampleRate == 44100)
    pLong = &p_FDKaacEnc_44100_long_1024;
  else if (sampleRate == 48000)
    pLong = &p_FDKaacEnc_48000_long_1024;
  else
    return -1;

  PSY_CONFIGURATION psyConf;
  memset(&psyConf, 0, sizeof(psyConf));

  /* GetSfBandTab cumulative-offset logic (psy_configuration.cpp:246-259),
   * granuleLength 1024 long window. */
  const INT granuleLengthWindow = 1024;
  INT sfbCnt = pLong->sfbCnt;
  const UCHAR *sfbWidth = pLong->sfbWidth;
  INT specStartOffset = 0;
  INT i;
  for (i = 0; i < sfbCnt; i++) {
    psyConf.sfbOffset[i] = specStartOffset;
    specStartOffset += sfbWidth[i];
    if (specStartOffset >= granuleLengthWindow) {
      i++;
      break;
    }
  }
  sfbCnt = fixMin(i, sfbCnt);
  psyConf.sfbOffset[sfbCnt] = fixMin(specStartOffset, granuleLengthWindow);

  psyConf.sfbCnt = sfbCnt;
  psyConf.sfbActive = sfbCnt; /* full bandwidth */

  TNS_CONFIG tC;
  memset(&tC, 0, sizeof(tC));

  AAC_ENCODER_ERROR err = FDKaacEnc_InitTnsConfiguration(
      bitRate, sampleRate, channels, LONG_WINDOW /*blockType*/,
      granuleLengthWindow, 0 /*isLowDelay*/, 0 /*ldSbrPresent*/, &tC, &psyConf,
      active, 0 /*useTnsPeak*/);
  if (err != AAC_ENC_OK) return -2;

  export_conf(&tC, out);
  return (int)psyConf.sfbActive;
}

/* tnsparity_detect runs the genuine FDKaacEnc_TnsDetect over the long-block
 * spectrum for the given config (rebuilt identically to tnsparity_build_config
 * so the config the Go side compares against is the same one the detect uses),
 * then exports the resulting TNS_INFO + the (long) TNS_SUBBLOCK_INFO +
 * filtersMerged. spectrum length must be >= lpcStopLine. Returns 0 (the C
 * return), -1/-2 on config build failure. */
int tnsparity_detect(int bitRate, int sampleRate, int channels, int active,
                     int sfbCnt, const int32_t *spectrum, tnsinfo_flat *out) {
  const SFB_PARAM_LONG *pLong = NULL;
  if (sampleRate == 44100)
    pLong = &p_FDKaacEnc_44100_long_1024;
  else if (sampleRate == 48000)
    pLong = &p_FDKaacEnc_48000_long_1024;
  else
    return -1;

  PSY_CONFIGURATION psyConf;
  memset(&psyConf, 0, sizeof(psyConf));
  const INT granuleLengthWindow = 1024;
  INT cnt = pLong->sfbCnt;
  const UCHAR *sfbWidth = pLong->sfbWidth;
  INT specStartOffset = 0;
  INT i;
  for (i = 0; i < cnt; i++) {
    psyConf.sfbOffset[i] = specStartOffset;
    specStartOffset += sfbWidth[i];
    if (specStartOffset >= granuleLengthWindow) {
      i++;
      break;
    }
  }
  cnt = fixMin(i, cnt);
  psyConf.sfbOffset[cnt] = fixMin(specStartOffset, granuleLengthWindow);
  psyConf.sfbCnt = cnt;
  psyConf.sfbActive = cnt;

  TNS_CONFIG tC;
  memset(&tC, 0, sizeof(tC));
  AAC_ENCODER_ERROR err = FDKaacEnc_InitTnsConfiguration(
      bitRate, sampleRate, channels, LONG_WINDOW, granuleLengthWindow, 0, 0, &tC,
      &psyConf, active, 0);
  if (err != AAC_ENC_OK) return -2;

  TNS_DATA tnsData;
  TNS_INFO tnsInfo;
  memset(&tnsData, 0, sizeof(tnsData));
  memset(&tnsInfo, 0, sizeof(tnsInfo));

  int rc = (int)FDKaacEnc_TnsDetect(&tnsData, &tC, &tnsInfo, sfbCnt,
                                    (const FIXP_DBL *)spectrum, 0 /*subBlock*/,
                                    LONG_WINDOW);

  /* export subblock 0 of the long-window TNS_INFO + the long subBlockInfo */
  out->numOfFilters = (int32_t)tnsInfo.numOfFilters[0];
  out->coefRes = (int32_t)tnsInfo.coefRes[0];
  for (int f = 0; f < 2; f++) {
    out->length[f] = (int32_t)tnsInfo.length[0][f];
    out->order[f] = (int32_t)tnsInfo.order[0][f];
    out->direction[f] = (int32_t)tnsInfo.direction[0][f];
    for (int k = 0; k < TNS_MAX_ORDER; k++) {
      out->coef[f][k] = (int32_t)tnsInfo.coef[0][f][k];
    }
    out->sbTnsActive[f] =
        (int32_t)tnsData.dataRaw.Long.subBlockInfo.tnsActive[f];
    out->sbPredictionGain[f] =
        (int32_t)tnsData.dataRaw.Long.subBlockInfo.predictionGain[f];
  }
  out->filtersMerged = (int32_t)tnsData.filtersMerged;

  (void)rc;
  return rc;
}

/* tnsparity_autotoparcor runs the genuine CLpc_AutoToParcor over `numOfCoeff`
 * autocorrelation values (mutated in place, matching the C), returning the
 * int16 reflection coefficients and the prediction gain mantissa/exponent. */
void tnsparity_autotoparcor(int32_t *acorr, int numOfCoeff, int16_t *reflCoeff,
                            int32_t *predGainM, int32_t *predGainE) {
  FIXP_DBL gainM = (FIXP_DBL)0;
  INT gainE = 0;
  CLpc_AutoToParcor((FIXP_DBL *)acorr, 0 /*acorr_e*/, (FIXP_LPC *)reflCoeff,
                    (INT)numOfCoeff, &gainM, &gainE);
  *predGainM = (int32_t)gainM;
  *predGainE = (int32_t)gainE;
}

/* tnsparity_merged_autocorr runs the genuine static
 * FDKaacEnc_MergedAutoCorrelation (reached via the aacenc_tns.cpp #include) over
 * a long-block spectrum, writing rxx1/rxx2 (length TNS_MAX_ORDER+1). The
 * acfWindow / lpcStartLine / lpcStopLine / maxOrder / acfSplit are passed in
 * flat so the Go side drives its port with identical inputs. */
void tnsparity_merged_autocorr(const int32_t *spectrum, int isLowDelay,
                               const int32_t *acfWindow /*[2][16]*/,
                               const int32_t *lpcStartLine /*[2]*/,
                               int lpcStopLine, int maxOrder,
                               const int32_t *acfSplit /*[2]*/, int32_t *rxx1,
                               int32_t *rxx2) {
  FIXP_DBL acf[MAX_NUM_OF_FILTERS][TNS_MAX_ORDER + 3 + 1];
  INT startLine[MAX_NUM_OF_FILTERS];
  INT split[MAX_NUM_OF_FILTERS];
  for (int f = 0; f < 2; f++) {
    startLine[f] = (INT)lpcStartLine[f];
    split[f] = (INT)acfSplit[f];
    for (int i = 0; i < TNS_MAX_ORDER + 3 + 1; i++) {
      acf[f][i] = (FIXP_DBL)acfWindow[f * (TNS_MAX_ORDER + 3 + 1) + i];
    }
  }
  FIXP_DBL r1[TNS_MAX_ORDER + 1];
  FIXP_DBL r2[TNS_MAX_ORDER + 1];
  memset(r1, 0, sizeof(r1));
  memset(r2, 0, sizeof(r2));

  FDKaacEnc_MergedAutoCorrelation((const FIXP_DBL *)spectrum, (INT)isLowDelay,
                                  acf, startLine, (INT)lpcStopLine,
                                  (INT)maxOrder, split, r1, r2);

  for (int i = 0; i < TNS_MAX_ORDER + 1; i++) {
    rxx1[i] = (int32_t)r1[i];
    rxx2[i] = (int32_t)r2[i];
  }
}

} /* extern "C" */
