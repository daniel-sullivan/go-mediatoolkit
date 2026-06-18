// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder scale-factor estimation
// (sf_estim.cpp) DRIVER stage. The two non-static entry points
// FDKaacEnc_CalcFormFactor and FDKaacEnc_EstimateScaleFactors (linked from the
// sibling sf_estim.cpp TU compiled into this test binary) drive the ENTIRE static
// helper chain — CalcFormFactorChannel, calcSfbRelevantLines, countSingleScfBits,
// calcSingleSpecPe, countScfBitsDiff, calcSpecPeDiff, improveScf, the three
// assimilate passes and EstimateScaleFactorsChannel — so the e2e shims here
// exercise every static the Go port translates, against the real FDK code.
//
// The inline-header kernels (sqrtFixp / invSqrtNorm2 from fixpoint_math.h, the
// inline FDKaacEnc_bitCountScalefactorDelta from bit_cnt.h) are the genuine
// vendored inline definitions reached directly. No re-derivation: the oracle is
// the real FDK code, not a Go-mirroring hand-twin (oracle_kind == real_vendored).

#include "sf_estim.h"
#include "bit_cnt.h"
#include "fixpoint_math.h"
#include "qc_data.h"
#include "interface.h"

#include <stdint.h>
#include <string.h>
#include <stdlib.h>

extern "C" {

// --- inline-header leaf kernels (genuine vendored) --------------------------

int32_t sfe_sqrt_fixp(int32_t op) { return (int32_t)sqrtFixp((FIXP_DBL)op); }

int32_t sfe_inv_sqrt_norm2(int32_t op, int32_t *shift) {
  INT s = 0;
  FIXP_DBL r = invSqrtNorm2((FIXP_DBL)op, &s);
  *shift = (int32_t)s;
  return (int32_t)r;
}

int sfe_bit_count_scf_delta(int delta) {
  return FDKaacEnc_bitCountScalefactorDelta(delta);
}

// --- full-chain e2e oracles (genuine vendored entry points) -----------------

// sfe_calc_form_factor runs the genuine FDKaacEnc_CalcFormFactor over one channel
// and copies out the resulting QC_OUT_CHANNEL.sfbFormFactorLdData
// (MAX_GROUPED_SFB cells).
void sfe_calc_form_factor(const int32_t *mdctSpectrum, const int *sfbOffsets,
                          int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
                          int32_t *sfbFormFactorLdDataOut) {
  QC_OUT_CHANNEL *qc = (QC_OUT_CHANNEL *)calloc(1, sizeof(QC_OUT_CHANNEL));
  PSY_OUT_CHANNEL *psy = (PSY_OUT_CHANNEL *)calloc(1, sizeof(PSY_OUT_CHANNEL));

  memcpy(qc->mdctSpectrum, mdctSpectrum, 1024 * sizeof(FIXP_DBL));
  psy->sfbCnt = sfbCnt;
  psy->sfbPerGroup = sfbPerGroup;
  psy->maxSfbPerGroup = maxSfbPerGroup;
  for (int i = 0; i < MAX_GROUPED_SFB + 1; i++) psy->sfbOffsets[i] = sfbOffsets[i];
  // PSY_OUT_CHANNEL.mdctSpectrum aliases QC_OUT_CHANNEL.mdctSpectrum.
  psy->mdctSpectrum = qc->mdctSpectrum;

  QC_OUT_CHANNEL *qcArr[2] = {qc, 0};
  PSY_OUT_CHANNEL *psyArr[2] = {psy, 0};
  FDKaacEnc_CalcFormFactor(qcArr, psyArr, 1);

  memcpy(sfbFormFactorLdDataOut, qc->sfbFormFactorLdData,
         MAX_GROUPED_SFB * sizeof(int32_t));
  free(qc);
  free(psy);
}

// sfe_estimate_scale_factors runs the genuine FDKaacEnc_EstimateScaleFactors over
// one channel. mdctSpectrum is mutated in place (empty bands zeroed). The
// per-sfb sfbEnergyLdData / sfbThresholdLdData / sfbFormFactorLdData inputs are
// seeded; the resulting scf (MAX_GROUPED_SFB), globalGain and quantSpec (1024)
// are copied out.
void sfe_estimate_scale_factors(int32_t *mdctSpectrum,
                                const int32_t *sfbEnergyLdData,
                                const int32_t *sfbThresholdLdData,
                                const int32_t *sfbFormFactorLdData,
                                const int *sfbOffsets, int sfbCnt,
                                int sfbPerGroup, int maxSfbPerGroup,
                                int invQuant, int dZoneQuantEnable,
                                int *scfOut, int *globalGainOut,
                                int16_t *quantSpecOut) {
  QC_OUT_CHANNEL *qc = (QC_OUT_CHANNEL *)calloc(1, sizeof(QC_OUT_CHANNEL));
  PSY_OUT_CHANNEL *psy = (PSY_OUT_CHANNEL *)calloc(1, sizeof(PSY_OUT_CHANNEL));

  memcpy(qc->mdctSpectrum, mdctSpectrum, 1024 * sizeof(FIXP_DBL));
  memcpy(qc->sfbEnergyLdData, sfbEnergyLdData, MAX_GROUPED_SFB * sizeof(int32_t));
  memcpy(qc->sfbThresholdLdData, sfbThresholdLdData,
         MAX_GROUPED_SFB * sizeof(int32_t));
  memcpy(qc->sfbFormFactorLdData, sfbFormFactorLdData,
         MAX_GROUPED_SFB * sizeof(int32_t));

  psy->sfbCnt = sfbCnt;
  psy->sfbPerGroup = sfbPerGroup;
  psy->maxSfbPerGroup = maxSfbPerGroup;
  for (int i = 0; i < MAX_GROUPED_SFB + 1; i++) psy->sfbOffsets[i] = sfbOffsets[i];
  psy->mdctSpectrum = qc->mdctSpectrum;
  psy->sfbThresholdLdData = qc->sfbThresholdLdData;
  psy->sfbEnergyLdData = qc->sfbEnergyLdData;

  QC_OUT_CHANNEL *qcArr[2] = {qc, 0};
  PSY_OUT_CHANNEL *psyArr[2] = {psy, 0};
  FDKaacEnc_EstimateScaleFactors(psyArr, qcArr, invQuant, dZoneQuantEnable, 1);

  for (int i = 0; i < MAX_GROUPED_SFB; i++) scfOut[i] = qc->scf[i];
  *globalGainOut = qc->globalGain;
  for (int i = 0; i < 1024; i++) quantSpecOut[i] = qc->quantSpec[i];
  // copy back the (possibly zeroed) mdct spectrum
  memcpy(mdctSpectrum, qc->mdctSpectrum, 1024 * sizeof(FIXP_DBL));

  free(qc);
  free(psy);
}

// sfe_calc_form_factor_multi runs the genuine FDKaacEnc_CalcFormFactor over
// nChannels (shared band layout, per-channel mdct at mdct[ch*1024]). Copies out
// the per-channel sfbFormFactorLdData. Exercises the channel loop of the driver.
void sfe_calc_form_factor_multi(int nChannels, const int32_t *mdct,
                                const int *sfbOffsets, int sfbCnt, int sfbPerGroup,
                                int maxSfbPerGroup, int32_t *sfbFormFactorLdDataOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  for (int ch = 0; ch < nChannels; ch++) {
    qc[ch] = (QC_OUT_CHANNEL *)calloc(1, sizeof(QC_OUT_CHANNEL));
    psy[ch] = (PSY_OUT_CHANNEL *)calloc(1, sizeof(PSY_OUT_CHANNEL));
    memcpy(qc[ch]->mdctSpectrum, mdct + ch * 1024, 1024 * sizeof(FIXP_DBL));
    psy[ch]->sfbCnt = sfbCnt;
    psy[ch]->sfbPerGroup = sfbPerGroup;
    psy[ch]->maxSfbPerGroup = maxSfbPerGroup;
    for (int i = 0; i < MAX_GROUPED_SFB + 1; i++)
      psy[ch]->sfbOffsets[i] = sfbOffsets[i];
    psy[ch]->mdctSpectrum = qc[ch]->mdctSpectrum;
  }
  FDKaacEnc_CalcFormFactor(qc, psy, nChannels);
  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(sfbFormFactorLdDataOut + ch * MAX_GROUPED_SFB,
           qc[ch]->sfbFormFactorLdData, MAX_GROUPED_SFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

// sfe_estimate_scale_factors_multi runs the genuine FDKaacEnc_EstimateScaleFactors
// over nChannels. Per-channel mdct (mutated), ld inputs, scf/globalGain/quantSpec
// outputs. Exercises the channel loop of the driver.
void sfe_estimate_scale_factors_multi(int nChannels, int32_t *mdct,
                                      const int32_t *sfbEnergyLdData,
                                      const int32_t *sfbThresholdLdData,
                                      const int32_t *sfbFormFactorLdData,
                                      const int *sfbOffsets, int sfbCnt,
                                      int sfbPerGroup, int maxSfbPerGroup,
                                      int invQuant, int dZoneQuantEnable,
                                      int *scfOut, int *globalGainOut,
                                      int16_t *quantSpecOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  for (int ch = 0; ch < nChannels; ch++) {
    qc[ch] = (QC_OUT_CHANNEL *)calloc(1, sizeof(QC_OUT_CHANNEL));
    psy[ch] = (PSY_OUT_CHANNEL *)calloc(1, sizeof(PSY_OUT_CHANNEL));
    memcpy(qc[ch]->mdctSpectrum, mdct + ch * 1024, 1024 * sizeof(FIXP_DBL));
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MAX_GROUPED_SFB,
           MAX_GROUPED_SFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MAX_GROUPED_SFB,
           MAX_GROUPED_SFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbFormFactorLdData, sfbFormFactorLdData + ch * MAX_GROUPED_SFB,
           MAX_GROUPED_SFB * sizeof(int32_t));
    psy[ch]->sfbCnt = sfbCnt;
    psy[ch]->sfbPerGroup = sfbPerGroup;
    psy[ch]->maxSfbPerGroup = maxSfbPerGroup;
    for (int i = 0; i < MAX_GROUPED_SFB + 1; i++)
      psy[ch]->sfbOffsets[i] = sfbOffsets[i];
    psy[ch]->mdctSpectrum = qc[ch]->mdctSpectrum;
    psy[ch]->sfbThresholdLdData = qc[ch]->sfbThresholdLdData;
    psy[ch]->sfbEnergyLdData = qc[ch]->sfbEnergyLdData;
  }
  FDKaacEnc_EstimateScaleFactors(psy, qc, invQuant, dZoneQuantEnable, nChannels);
  for (int ch = 0; ch < nChannels; ch++) {
    for (int i = 0; i < MAX_GROUPED_SFB; i++)
      scfOut[ch * MAX_GROUPED_SFB + i] = qc[ch]->scf[i];
    globalGainOut[ch] = qc[ch]->globalGain;
    for (int i = 0; i < 1024; i++)
      quantSpecOut[ch * 1024 + i] = qc[ch]->quantSpec[i];
    memcpy(mdct + ch * 1024, qc[ch]->mdctSpectrum, 1024 * sizeof(FIXP_DBL));
    free(qc[ch]);
    free(psy[ch]);
  }
}

} // extern "C"
