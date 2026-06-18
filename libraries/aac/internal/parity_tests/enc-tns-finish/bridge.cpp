// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE "TNS finish"
 * batch:
 *   FDKaacEnc_InitTnsConfiguration (libAACenc/src/aacenc_tns.cpp:377, public)
 *   FDKaacEnc_TnsSync              (aacenc_tns.cpp:961, public)
 *   FDKaacEnc_TnsEncode           (aacenc_tns.cpp:1051, public)
 *   CLpc_ParcorToLpc              (libFDK/src/FDK_lpc.cpp:393, public)
 *   CLpc_Analysis                 (libFDK/src/FDK_lpc.cpp:301, public)
 * plus, to drive TnsEncode/TnsSync with a genuine TNS_INFO/TNS_DATA, the genuine
 * decision FDKaacEnc_TnsDetect (aacenc_tns.cpp:766).
 *
 * This TU #includes the GENUINE vendored aacenc_tns.cpp directly so the shims
 * reach the public symbols above AND the static helpers TnsDetect calls. Because
 * aacenc_tns.cpp is included here it must NOT also be compiled as a separate
 * sibling TU. The ROM (aacEnc_rom.cpp), FDK_lpc.cpp, fixpoint_math.cpp, scale.cpp,
 * FDK_tools_rom.cpp and genericStds.cpp ARE separate sibling TUs (support_*_cgo.cpp)
 * supplying the referenced symbols. oracle_kind == real_vendored.
 *
 * This TU NEVER imports libraries/aac. It MAY (and the test does) import the
 * pure-Go internal/nativeaac. The whole result is integer FIXP_DBL / FIXP_LPC
 * Q-format on the AAC-LC long-block path (granuleLength 1024 -> integer ROM
 * acfWindowLong; no FDKaacEnc_CalcGaussWindow, which is LD-only) so the oracle
 * asserts EXACT integer equality.
 */

#include <stdint.h>
#include <string.h>

#include "common_fix.h"
#include "psy_const.h"
#include "psy_configuration.h"
#include "aacenc_tns.h"
#include "aacEnc_rom.h"
#include "aacenc.h"

/* Pull in the genuine vendored source (public + static symbols visible here). */
#include "libfdk/libAACenc/src/aacenc_tns.cpp"

extern "C" {

/* Flat mirror of the TNS_CONFIG fields, matching enc-tns-full's tnsconf_flat
 * shape (so the Go test can reuse the same comparison logic) plus the
 * FDKaacEnc_InitTnsConfiguration return code. */
typedef struct {
  int32_t filterEnabled[2];
  int32_t threshOn[2];
  int32_t filterStartFreq[2];
  int32_t tnsLimitOrder[2];
  int32_t tnsFilterDirection[2];
  int32_t acfSplit[2];
  int32_t tnsTimeResolution[2];
  int32_t seperateFiltersAllowed;
  int32_t isLowDelay;
  int32_t tnsActive;
  int32_t maxOrder;
  int32_t coefRes;
  int32_t acfWindow[2][TNS_MAX_ORDER + 3 + 1];
  int32_t lpcStartBand[2];
  int32_t lpcStartLine[2];
  int32_t lpcStopBand;
  int32_t lpcStopLine;
  int32_t initRc;
  /* the genuine PSY_CONFIGURATION layout the init consumed, so the Go side runs
   * its init over an identical config */
  int32_t sfbCnt;
  int32_t sfbActive;
  int32_t sfbOffset[MAX_SFB + 1];
} tnsfin_conf_flat;

/* Flat mirror of the (long-window) TNS_INFO + TNS_SUBBLOCK_INFO the decision
 * writes, indexed by filter (0=HIFILT, 1=LOFILT). subBlock 0. */
typedef struct {
  int32_t numOfFilters;
  int32_t coefRes;
  int32_t length[2];
  int32_t order[2];
  int32_t direction[2];
  int32_t coefCompress[2];
  int32_t coef[2][TNS_MAX_ORDER];
  int32_t sbTnsActive[2];
  int32_t sbPredictionGain[2];
  int32_t filtersMerged;
} tnsfin_info_flat;

/* Build a genuine PSY_CONFIGURATION sfbOffset for an AAC-LC long block, identical
 * to enc-tns-full (GetSfBandTab cumulative-sum logic). Returns sfbActive, or -1
 * for an unseeded sample rate. */
static int build_psyconf(int sampleRate, PSY_CONFIGURATION *psyConf) {
  const SFB_PARAM_LONG *pLong = NULL;
  if (sampleRate == 44100)
    pLong = &p_FDKaacEnc_44100_long_1024;
  else if (sampleRate == 48000)
    pLong = &p_FDKaacEnc_48000_long_1024;
  else if (sampleRate == 32000)
    pLong = &p_FDKaacEnc_32000_long_1024;
  else
    return -1;

  memset(psyConf, 0, sizeof(*psyConf));
  const INT granuleLengthWindow = 1024;
  INT sfbCnt = pLong->sfbCnt;
  const UCHAR *sfbWidth = pLong->sfbWidth;
  INT specStartOffset = 0;
  INT i;
  for (i = 0; i < sfbCnt; i++) {
    psyConf->sfbOffset[i] = specStartOffset;
    specStartOffset += sfbWidth[i];
    if (specStartOffset >= granuleLengthWindow) {
      i++;
      break;
    }
  }
  sfbCnt = fixMin(i, sfbCnt);
  psyConf->sfbOffset[sfbCnt] = fixMin(specStartOffset, granuleLengthWindow);
  psyConf->sfbCnt = sfbCnt;
  psyConf->sfbActive = sfbCnt;
  return (int)psyConf->sfbActive;
}

static void export_conf(const TNS_CONFIG *tC, const PSY_CONFIGURATION *psyConf,
                        int rc, tnsfin_conf_flat *out) {
  out->sfbCnt = (int32_t)psyConf->sfbCnt;
  out->sfbActive = (int32_t)psyConf->sfbActive;
  for (int i = 0; i <= psyConf->sfbCnt && i <= MAX_SFB; i++)
    out->sfbOffset[i] = (int32_t)psyConf->sfbOffset[i];
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
    for (int i = 0; i < TNS_MAX_ORDER + 3 + 1; i++)
      out->acfWindow[f][i] = (int32_t)tC->acfWindow[f][i];
  }
  out->seperateFiltersAllowed = (int32_t)tC->confTab.seperateFiltersAllowed;
  out->isLowDelay = (int32_t)tC->isLowDelay;
  out->tnsActive = (int32_t)tC->tnsActive;
  out->maxOrder = (int32_t)tC->maxOrder;
  out->coefRes = (int32_t)tC->coefRes;
  out->lpcStopBand = (int32_t)tC->lpcStopBand;
  out->lpcStopLine = (int32_t)tC->lpcStopLine;
  out->initRc = (int32_t)rc;
}

/* tnsfin_build_config builds psyConf + runs the genuine
 * FDKaacEnc_InitTnsConfiguration for an AAC-LC long block and exports the config.
 * Returns sfbActive (>=0) or -1/-2 on failure. */
int tnsfin_build_config(int bitRate, int sampleRate, int channels, int active,
                        tnsfin_conf_flat *out) {
  PSY_CONFIGURATION psyConf;
  int sfbActive = build_psyconf(sampleRate, &psyConf);
  if (sfbActive < 0) return -1;

  TNS_CONFIG tC;
  memset(&tC, 0, sizeof(tC));
  AAC_ENCODER_ERROR err = FDKaacEnc_InitTnsConfiguration(
      bitRate, sampleRate, channels, LONG_WINDOW, 1024, 0, 0, &tC, &psyConf,
      active, 0);
  export_conf(&tC, &psyConf, (int)err, out);
  if (err != AAC_ENC_OK) return -2;
  return sfbActive;
}

/* Internal: build config + run the genuine FDKaacEnc_TnsDetect, returning the
 * TNS_CONFIG / TNS_DATA / TNS_INFO so the encode/sync shims can drive the genuine
 * filter against them. spectrum length must be >= lpcStopLine. */
static int detect_into(int bitRate, int sampleRate, int channels, int active,
                       int sfbCnt, const int32_t *spectrum, TNS_CONFIG *tC,
                       TNS_DATA *tnsData, TNS_INFO *tnsInfo) {
  PSY_CONFIGURATION psyConf;
  if (build_psyconf(sampleRate, &psyConf) < 0) return -1;
  memset(tC, 0, sizeof(*tC));
  if (FDKaacEnc_InitTnsConfiguration(bitRate, sampleRate, channels, LONG_WINDOW,
                                     1024, 0, 0, tC, &psyConf, active,
                                     0) != AAC_ENC_OK)
    return -2;
  memset(tnsData, 0, sizeof(*tnsData));
  memset(tnsInfo, 0, sizeof(*tnsInfo));
  return (int)FDKaacEnc_TnsDetect(tnsData, tC, tnsInfo, sfbCnt,
                                  (const FIXP_DBL *)spectrum, 0, LONG_WINDOW);
}

static void export_info(const TNS_DATA *tnsData, const TNS_INFO *tnsInfo,
                        tnsfin_info_flat *out) {
  out->numOfFilters = (int32_t)tnsInfo->numOfFilters[0];
  out->coefRes = (int32_t)tnsInfo->coefRes[0];
  for (int f = 0; f < 2; f++) {
    out->length[f] = (int32_t)tnsInfo->length[0][f];
    out->order[f] = (int32_t)tnsInfo->order[0][f];
    out->direction[f] = (int32_t)tnsInfo->direction[0][f];
    out->coefCompress[f] = (int32_t)tnsInfo->coefCompress[0][f];
    for (int k = 0; k < TNS_MAX_ORDER; k++)
      out->coef[f][k] = (int32_t)tnsInfo->coef[0][f][k];
    out->sbTnsActive[f] =
        (int32_t)tnsData->dataRaw.Long.subBlockInfo.tnsActive[f];
    out->sbPredictionGain[f] =
        (int32_t)tnsData->dataRaw.Long.subBlockInfo.predictionGain[f];
  }
  out->filtersMerged = (int32_t)tnsData->filtersMerged;
}

/* tnsfin_detect runs the genuine decision and exports the resulting TNS_INFO +
 * subblock info (so the Go side can seed an identical TNS_INFO/TNS_DATA to drive
 * its own TnsEncode/TnsSync). Returns the FDKaacEnc_TnsDetect return (0). */
int tnsfin_detect(int bitRate, int sampleRate, int channels, int active,
                  int sfbCnt, const int32_t *spectrum, tnsfin_info_flat *out) {
  TNS_CONFIG tC;
  TNS_DATA tnsData;
  TNS_INFO tnsInfo;
  int rc = detect_into(bitRate, sampleRate, channels, active, sfbCnt, spectrum,
                       &tC, &tnsData, &tnsInfo);
  if (rc < 0) return rc;
  export_info(&tnsData, &tnsInfo, out);
  return 0;
}

/* tnsfin_encode runs the genuine decision then the genuine FDKaacEnc_TnsEncode,
 * rewriting `spectrum` in place. Returns the TnsEncode return (1 if inactive,
 * else 0); -1/-2 on config failure. */
int tnsfin_encode(int bitRate, int sampleRate, int channels, int active,
                  int sfbCnt, int32_t *spectrum) {
  TNS_CONFIG tC;
  TNS_DATA tnsData;
  TNS_INFO tnsInfo;
  /* The decision reads spectrum but does NOT mutate it (scaleUp writes scratch);
   * run detect on a copy is unnecessary — the C decode does the same: detect
   * then encode on the same array. */
  int rc = detect_into(bitRate, sampleRate, channels, active, sfbCnt, spectrum,
                       &tC, &tnsData, &tnsInfo);
  if (rc < 0) return rc;
  int enc = (int)FDKaacEnc_TnsEncode(&tnsInfo, &tnsData, sfbCnt, &tC,
                                     tC.lpcStopLine, (FIXP_DBL *)spectrum, 0,
                                     LONG_WINDOW);
  return enc;
}

/* tnsfin_sync seeds two long-window TNS_DATA/TNS_INFO from the flat info the
 * caller supplies (dest, src), runs the genuine FDKaacEnc_TnsSync, and exports
 * the mutated dest data+info. maxOrder is read from a freshly built config for
 * the given rate. */
void tnsfin_sync(int maxOrder, const tnsfin_info_flat *destIn,
                 const tnsfin_info_flat *srcIn, tnsfin_info_flat *destOut) {
  TNS_DATA dDest, dSrc;
  TNS_INFO iDest, iSrc;
  memset(&dDest, 0, sizeof(dDest));
  memset(&dSrc, 0, sizeof(dSrc));
  memset(&iDest, 0, sizeof(iDest));
  memset(&iSrc, 0, sizeof(iSrc));

  TNS_CONFIG tC;
  memset(&tC, 0, sizeof(tC));
  tC.maxOrder = maxOrder;

  /* seed dest */
  iDest.numOfFilters[0] = destIn->numOfFilters;
  dDest.filtersMerged = destIn->filtersMerged;
  for (int f = 0; f < 2; f++) {
    iDest.length[0][f] = destIn->length[f];
    iDest.order[0][f] = destIn->order[f];
    iDest.direction[0][f] = destIn->direction[f];
    iDest.coefCompress[0][f] = destIn->coefCompress[f];
    for (int k = 0; k < TNS_MAX_ORDER; k++)
      iDest.coef[0][f][k] = destIn->coef[f][k];
    dDest.dataRaw.Long.subBlockInfo.tnsActive[f] = destIn->sbTnsActive[f];
  }
  /* seed src */
  iSrc.numOfFilters[0] = srcIn->numOfFilters;
  dSrc.filtersMerged = srcIn->filtersMerged;
  for (int f = 0; f < 2; f++) {
    iSrc.length[0][f] = srcIn->length[f];
    iSrc.order[0][f] = srcIn->order[f];
    iSrc.direction[0][f] = srcIn->direction[f];
    iSrc.coefCompress[0][f] = srcIn->coefCompress[f];
    for (int k = 0; k < TNS_MAX_ORDER; k++)
      iSrc.coef[0][f][k] = srcIn->coef[f][k];
    dSrc.dataRaw.Long.subBlockInfo.tnsActive[f] = srcIn->sbTnsActive[f];
  }

  FDKaacEnc_TnsSync(&dDest, &dSrc, &iDest, &iSrc, LONG_WINDOW, LONG_WINDOW, &tC);

  export_info(&dDest, &iDest, destOut);
}

/* tnsfin_get_max_bands reaches the genuine static getTnsMaxBands (via the
 * aacenc_tns.cpp #include) so the Go ROM scan is verified against the real C. */
int tnsfin_get_max_bands(int sampleRate, int granuleLength, int isShortBlock) {
  return getTnsMaxBands(sampleRate, granuleLength, isShortBlock);
}

/* tnsfin_parcor2lpc runs the genuine CLpc_ParcorToLpc. workBuffer length >=
 * numOfCoeff; lpcCoeff written in place. Returns the LPC exponent. */
int tnsfin_parcor2lpc(const int16_t *reflCoeff, int16_t *lpcCoeff,
                      int numOfCoeff, int32_t *workBuffer) {
  return (int)CLpc_ParcorToLpc((const FIXP_LPC *)reflCoeff, (FIXP_LPC *)lpcCoeff,
                               (INT)numOfCoeff, (FIXP_DBL *)workBuffer);
}

/* tnsfin_analysis runs the genuine CLpc_Analysis (NULL filtStateIndex). signal
 * (length signalSize) and filtState (length order) mutated in place. */
void tnsfin_analysis(int32_t *signal, int signalSize, const int16_t *lpcCoeff,
                     int lpcCoeffE, int order, int32_t *filtState) {
  CLpc_Analysis((FIXP_DBL *)signal, (INT)signalSize, (const FIXP_LPC *)lpcCoeff,
                (INT)lpcCoeffE, (INT)order, (FIXP_DBL *)filtState, NULL);
}

} /* extern "C" */
