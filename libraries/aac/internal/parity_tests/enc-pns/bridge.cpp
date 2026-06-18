// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE-side Perceptual
 * Noise Substitution (PNS) detect/code chain (libAACenc/src/aacenc_pns.cpp):
 *
 *   - FDKaacEnc_InitPnsConfiguration (aacenc_pns.cpp:137) -> fills PNS_CONFIG via
 *     FDKaacEnc_GetPnsParam (pnsparam.cpp:501) + FDKaacEnc_FreqToBandWidthRounding
 *     (aacenc_tns.cpp:339).
 *   - FDKaacEnc_PnsDetect (aacenc_pns.cpp:173) -> runs FDKaacEnc_noiseDetect
 *     (noisedet.cpp:150) and the fuzzy/threshold decision, fills pnsFlag/noiseNrg.
 *   - FDKaacEnc_CodePnsChannel (aacenc_pns.cpp:381) -> finalises noiseNrg.
 *   - FDKaacEnc_PreProcessPnsChannelPair (aacenc_pns.cpp:441) -> noiseEnergyCorrelation.
 *   - FDKaacEnc_PostProcessPnsChannelPair (aacenc_pns.cpp:498) -> msMask/pnsFlag.
 *
 * This TU provides the extern "C" bridges the Go test calls; it links the GENUINE
 * vendored aacenc_pns.cpp + noisedet.cpp + pnsparam.cpp + aacenc_tns.cpp (for
 * FreqToBandWidthRounding) + FDK_lpc.cpp + aacEnc_rom.cpp + fixpoint_math.cpp +
 * FDK_tools_rom.cpp + scale.cpp + genericStds.cpp as sibling TUs, so the oracle is
 * the real reference, NOT a hand-twin (oracle_kind == real_vendored). It NEVER
 * imports libraries/aac, so there is no cross-package static-symbol clash (each
 * parity package compiles its OWN copy of the needed fdk C TUs). It MAY, and the
 * test does, import the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL /
 * int16 FIXP_SGL Q-format quantity. The chain is entirely integer (fMult int64
 * products, arithmetic shifts, leading-bit counts, table-driven CalcLdData /
 * CalcInvLdData), bit-identical regardless of -ffp-contract or vectorization. So
 * the test asserts EXACT int32/int16 equality (the gate command still sets
 * aac_strict for consistency). Only -I / -D / -Wno-* live in the in-source #cgo
 * CFLAGS (see cgo.go); the scalar FP flags come from the mise task env. */

#include <stdint.h>
#include <string.h>

#include "aacenc_pns.h"
#include "pns_func.h"
#include "pnsparam.h"
#include "psy_const.h"
#include "aacenc.h"

extern "C" {

#define EP_MAX_GROUPED_SFB MAX_GROUPED_SFB

/* ---- GetPnsParam / InitPnsConfiguration ------------------------------------ */

/* Flat mirror of the NOISEPARAMS + correlation fields InitPnsConfiguration fills. */
typedef struct {
  int32_t usePns; /* (possibly cleared) usePns returned via pnsConf */
  int32_t minCorrelationEnergy;
  int32_t noiseCorrelationThresh;
  /* NOISEPARAMS np */
  int32_t startSfb;
  int32_t detectionAlgorithmFlags;
  int32_t refPower;
  int32_t refTonality;
  int32_t tnsGainThreshold;
  int32_t tnsPNSGainThreshold;
  int32_t minSfbWidth;
  int16_t powDistPSDcurve[EP_MAX_GROUPED_SFB];
  int16_t gapFillThr;
} EP_PNS_CONF;

/* eparity_init_pns_configuration runs the genuine FDKaacEnc_InitPnsConfiguration
 * for the given parameters and copies the filled PNS_CONFIG out into *out.
 * Returns the AAC_ENCODER_ERROR code (0 == AAC_ENC_OK). */
int eparity_init_pns_configuration(int bitRate, int sampleRate, int usePns,
                                   int sfbCnt, const int *sfbOffset, int numChan,
                                   int isLC, EP_PNS_CONF *out) {
  PNS_CONFIG pnsConf;
  memset(&pnsConf, 0, sizeof(pnsConf));

  AAC_ENCODER_ERROR err = FDKaacEnc_InitPnsConfiguration(
      &pnsConf, bitRate, sampleRate, usePns, sfbCnt, sfbOffset, numChan, isLC);

  memset(out, 0, sizeof(*out));
  if (err != AAC_ENC_OK) {
    return (int)err;
  }

  out->usePns = pnsConf.usePns;
  out->minCorrelationEnergy = (int32_t)pnsConf.minCorrelationEnergy;
  out->noiseCorrelationThresh = (int32_t)pnsConf.noiseCorrelationThresh;
  out->startSfb = pnsConf.np.startSfb;
  out->detectionAlgorithmFlags = pnsConf.np.detectionAlgorithmFlags;
  out->refPower = (int32_t)pnsConf.np.refPower;
  out->refTonality = (int32_t)pnsConf.np.refTonality;
  out->tnsGainThreshold = pnsConf.np.tnsGainThreshold;
  out->tnsPNSGainThreshold = pnsConf.np.tnsPNSGainThreshold;
  out->minSfbWidth = pnsConf.np.minSfbWidth;
  for (int i = 0; i < EP_MAX_GROUPED_SFB; i++)
    out->powDistPSDcurve[i] = (int16_t)pnsConf.np.powDistPSDcurve[i];
  out->gapFillThr = (int16_t)pnsConf.np.gapFillThr;

  return (int)err;
}

/* Build a PNS_CONFIG from the flat EP_PNS_CONF (used to drive the detect/code
 * bridges with a config identical to the Go side's). */
static void ep_build_pns_conf(PNS_CONFIG *pnsConf, const EP_PNS_CONF *in) {
  memset(pnsConf, 0, sizeof(*pnsConf));
  pnsConf->usePns = in->usePns;
  pnsConf->minCorrelationEnergy = (FIXP_DBL)in->minCorrelationEnergy;
  pnsConf->noiseCorrelationThresh = (FIXP_DBL)in->noiseCorrelationThresh;
  pnsConf->np.startSfb = (SHORT)in->startSfb;
  pnsConf->np.detectionAlgorithmFlags = (USHORT)in->detectionAlgorithmFlags;
  pnsConf->np.refPower = (FIXP_DBL)in->refPower;
  pnsConf->np.refTonality = (FIXP_DBL)in->refTonality;
  pnsConf->np.tnsGainThreshold = in->tnsGainThreshold;
  pnsConf->np.tnsPNSGainThreshold = in->tnsPNSGainThreshold;
  pnsConf->np.minSfbWidth = in->minSfbWidth;
  for (int i = 0; i < EP_MAX_GROUPED_SFB; i++)
    pnsConf->np.powDistPSDcurve[i] = (FIXP_SGL)in->powDistPSDcurve[i];
  pnsConf->np.gapFillThr = (FIXP_SGL)in->gapFillThr;
}

/* ---- PnsDetect ------------------------------------------------------------- */

/* eparity_pns_detect runs the genuine FDKaacEnc_PnsDetect on the given inputs and
 * copies out pnsFlag/noiseNrg + the noiseFuzzyMeasure produced by noise detection.
 * conf is the flat PNS_CONFIG to run against (built via ep_build_pns_conf). */
void eparity_pns_detect(const EP_PNS_CONF *conf, int lastWindowSequence,
                        int sfbActive, int maxSfbPerGroup,
                        const int32_t *sfbThresholdLdData, const int *sfbOffset,
                        const int32_t *mdctSpectrum, const int *sfbMaxScaleSpec,
                        const int16_t *sfbtonality, int tnsOrder,
                        int tnsPredictionGain, int tnsActive,
                        const int32_t *sfbEnergyLdData, int specLen,
                        int *pnsFlagOut, int *noiseNrgOut,
                        int16_t *noiseFuzzyOut) {
  PNS_CONFIG pnsConf;
  ep_build_pns_conf(&pnsConf, conf);

  PNS_DATA pnsData;
  memset(&pnsData, 0, sizeof(pnsData));

  /* Working copies (PnsDetect modifies sfbThresholdLdData? No — it only reads it.
   * noiseNrg is sized MAX_GROUPED_SFB.) */
  int noiseNrg[MAX_GROUPED_SFB];

  FDKaacEnc_PnsDetect(
      &pnsConf, &pnsData, lastWindowSequence, sfbActive, maxSfbPerGroup,
      (FIXP_DBL *)sfbThresholdLdData, sfbOffset, (FIXP_DBL *)mdctSpectrum,
      (INT *)sfbMaxScaleSpec, (FIXP_SGL *)sfbtonality, tnsOrder,
      tnsPredictionGain, tnsActive, (FIXP_DBL *)sfbEnergyLdData, noiseNrg);

  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    pnsFlagOut[i] = pnsData.pnsFlag[i];
    noiseNrgOut[i] = noiseNrg[i];
    noiseFuzzyOut[i] = (int16_t)pnsData.noiseFuzzyMeasure[i];
  }
  (void)specLen;
}

/* ---- CodePnsChannel -------------------------------------------------------- */

/* eparity_code_pns_channel runs the genuine FDKaacEnc_CodePnsChannel. pnsFlag and
 * noiseNrgInOut/sfbThresholdInOut are read+modified in place. */
void eparity_code_pns_channel(const EP_PNS_CONF *conf, int sfbActive,
                              const int *pnsFlag, const int32_t *sfbEnergyLdData,
                              const int *noiseNrgIn, const int32_t *sfbThresholdIn,
                              int *noiseNrgOut, int32_t *sfbThresholdOut) {
  PNS_CONFIG pnsConf;
  ep_build_pns_conf(&pnsConf, conf);

  int pnsFlagLocal[MAX_GROUPED_SFB];
  int noiseNrg[MAX_GROUPED_SFB];
  int32_t sfbThreshold[MAX_GROUPED_SFB];
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    pnsFlagLocal[i] = pnsFlag[i];
    noiseNrg[i] = noiseNrgIn[i];
    sfbThreshold[i] = sfbThresholdIn[i];
  }

  FDKaacEnc_CodePnsChannel(sfbActive, &pnsConf, pnsFlagLocal,
                           (FIXP_DBL *)sfbEnergyLdData, noiseNrg,
                           (FIXP_DBL *)sfbThreshold);

  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    noiseNrgOut[i] = noiseNrg[i];
    sfbThresholdOut[i] = sfbThreshold[i];
  }
}

/* ---- PreProcessPnsChannelPair ---------------------------------------------- */

/* eparity_pre_process runs the genuine FDKaacEnc_PreProcessPnsChannelPair and
 * copies out the L channel's noiseEnergyCorrelation (R is identical). */
void eparity_pre_process(const EP_PNS_CONF *conf, int sfbActive,
                         const int32_t *sfbEnergyLeft, const int32_t *sfbEnergyRight,
                         const int32_t *sfbEnergyLeftLD, const int32_t *sfbEnergyRightLD,
                         const int32_t *sfbEnergyMid, int32_t *corrOut) {
  PNS_CONFIG pnsConf;
  ep_build_pns_conf(&pnsConf, conf);

  PNS_DATA pnsDataLeft, pnsDataRight;
  memset(&pnsDataLeft, 0, sizeof(pnsDataLeft));
  memset(&pnsDataRight, 0, sizeof(pnsDataRight));

  FDKaacEnc_PreProcessPnsChannelPair(
      sfbActive, (FIXP_DBL *)sfbEnergyLeft, (FIXP_DBL *)sfbEnergyRight,
      (FIXP_DBL *)sfbEnergyLeftLD, (FIXP_DBL *)sfbEnergyRightLD,
      (FIXP_DBL *)sfbEnergyMid, &pnsConf, &pnsDataLeft, &pnsDataRight);

  for (int i = 0; i < MAX_GROUPED_SFB; i++)
    corrOut[i] = (int32_t)pnsDataLeft.noiseEnergyCorrelation[i];
}

/* ---- PostProcessPnsChannelPair --------------------------------------------- */

/* eparity_post_process runs the genuine FDKaacEnc_PostProcessPnsChannelPair on
 * explicit pnsFlag/noiseEnergyCorrelation/msMask inputs and copies out the
 * modified pnsFlagL/pnsFlagR/msMask + msDigest. */
void eparity_post_process(const EP_PNS_CONF *conf, int sfbActive,
                          const int *pnsFlagL, const int *pnsFlagR,
                          const int32_t *corrL, const int32_t *corrR,
                          const int *msMaskIn, int msDigestIn, int *pnsFlagLOut,
                          int *pnsFlagROut, int *msMaskOut, int *msDigestOut) {
  PNS_CONFIG pnsConf;
  ep_build_pns_conf(&pnsConf, conf);

  PNS_DATA pnsDataLeft, pnsDataRight;
  memset(&pnsDataLeft, 0, sizeof(pnsDataLeft));
  memset(&pnsDataRight, 0, sizeof(pnsDataRight));
  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    pnsDataLeft.pnsFlag[i] = pnsFlagL[i];
    pnsDataRight.pnsFlag[i] = pnsFlagR[i];
    pnsDataLeft.noiseEnergyCorrelation[i] = (FIXP_DBL)corrL[i];
    pnsDataRight.noiseEnergyCorrelation[i] = (FIXP_DBL)corrR[i];
  }

  int msMask[MAX_GROUPED_SFB];
  for (int i = 0; i < MAX_GROUPED_SFB; i++) msMask[i] = msMaskIn[i];
  int msDigest = msDigestIn;

  FDKaacEnc_PostProcessPnsChannelPair(sfbActive, &pnsConf, &pnsDataLeft,
                                      &pnsDataRight, msMask, &msDigest);

  for (int i = 0; i < MAX_GROUPED_SFB; i++) {
    pnsFlagLOut[i] = pnsDataLeft.pnsFlag[i];
    pnsFlagROut[i] = pnsDataRight.pnsFlag[i];
    msMaskOut[i] = msMask[i];
  }
  *msDigestOut = msDigest;
}

} /* extern "C" */
