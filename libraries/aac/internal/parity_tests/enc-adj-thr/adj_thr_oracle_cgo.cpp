// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle for the AAC encoder threshold-adjustment DRIVER statics
// FDKaacEnc_preparePe / FDKaacEnc_calcWeighting / FDKaacEnc_calcPe /
// FDKaacEnc_initAvoidHoleFlag / FDKaacEnc_reduceThresholdsCBR /
// FDKaacEnc_calcChaosMeasure (libAACenc/src/adj_thr.cpp). These six are `static`,
// so the only way to link the GENUINE definitions is to compile adj_thr.cpp into
// THIS translation unit and expose extern "C" shims from inside it — the shims
// see the file-local statics directly. No re-derivation: the oracle is the real
// FDK code (oracle_kind == real_vendored).
//
// This package owns its own copy of the needed C TUs (adj_thr.cpp here, plus
// line_pe.cpp / fixpoint_math.cpp / aacEnc_rom.cpp / genericStds.cpp pinned by
// the sibling *_cgo.cpp) and never imports libraries/aac, which would link a
// second copy and clash on static symbols.

#include "libfdk/libAACenc/src/adj_thr.cpp"

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define MGSFB MAX_GROUPED_SFB

extern "C" {

// allocChan allocates a zeroed QC_OUT_CHANNEL/PSY_OUT_CHANNEL pair and aliases
// the psy LD-domain pointers onto the qc memory exactly as the encoder does
// (interface.h:140 "memory located in QC_OUT_CHANNEL").
static void allocChan(QC_OUT_CHANNEL **qcp, PSY_OUT_CHANNEL **psyp,
                      int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
                      const int *sfbOffset) {
  QC_OUT_CHANNEL *qc = (QC_OUT_CHANNEL *)calloc(1, sizeof(QC_OUT_CHANNEL));
  PSY_OUT_CHANNEL *psy = (PSY_OUT_CHANNEL *)calloc(1, sizeof(PSY_OUT_CHANNEL));
  psy->sfbCnt = sfbCnt;
  psy->sfbPerGroup = sfbPerGroup;
  psy->maxSfbPerGroup = maxSfbPerGroup;
  if (sfbOffset) {
    for (int i = 0; i <= sfbCnt; i++) psy->sfbOffsets[i] = sfbOffset[i];
  }
  psy->sfbEnergy = qc->sfbEnergy;
  psy->sfbThresholdLdData = qc->sfbThresholdLdData;
  psy->sfbEnergyLdData = qc->sfbEnergyLdData;
  psy->sfbMinSnrLdData = qc->sfbMinSnrLdData;
  psy->sfbSpreadEnergy = qc->sfbSpreadEnergy;
  *qcp = qc;
  *psyp = psy;
}

// aparity_prepare_pe runs the genuine FDKaacEnc_preparePe for one channel and
// copies out sfbNLines + the stamped peData->offset.
void aparity_prepare_pe(const int32_t *sfbEnergyLdData,
                        const int32_t *sfbThresholdLdData,
                        const int32_t *sfbFormFactorLdData, const int *sfbOffset,
                        int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
                        int peOffset, int32_t *sfbNLinesOut, int32_t *offsetOut) {
  QC_OUT_CHANNEL *qc;
  PSY_OUT_CHANNEL *psy;
  allocChan(&qc, &psy, sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
  memcpy(qc->sfbEnergyLdData, sfbEnergyLdData, MGSFB * sizeof(int32_t));
  memcpy(qc->sfbThresholdLdData, sfbThresholdLdData, MGSFB * sizeof(int32_t));
  memcpy(qc->sfbFormFactorLdData, sfbFormFactorLdData, MGSFB * sizeof(int32_t));

  PE_DATA pe;
  memset(&pe, 0, sizeof(pe));
  QC_OUT_CHANNEL *qcArr[2] = {qc, 0};
  PSY_OUT_CHANNEL *psyArr[2] = {psy, 0};
  FDKaacEnc_preparePe(&pe, psyArr, qcArr, 1, peOffset);

  memcpy(sfbNLinesOut, pe.peChannelData[0].sfbNLines, MGSFB * sizeof(int32_t));
  *offsetOut = pe.offset;
  free(qc);
  free(psy);
}

// aparity_calc_weighting runs the genuine FDKaacEnc_calcWeighting for nChannels.
void aparity_calc_weighting(int nChannels, const int32_t *sfbEnergyLdData,
                            const int32_t *sfbEnergy, const int32_t *sfbNLines,
                            const int *sfbOffset, const int *lastWindowSequence,
                            const int32_t *msMask, int sfbCnt, int sfbPerGroup,
                            int maxSfbPerGroup, int32_t *chaosMeasureEnFac,
                            int *lastEnFacPatch, int32_t *sfbEnFacLdOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  PE_DATA pe;
  memset(&pe, 0, sizeof(pe));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergy, sfbEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    psy[ch]->lastWindowSequence = lastWindowSequence[ch];
    memcpy(pe.peChannelData[ch].sfbNLines, sfbNLines + ch * MGSFB,
           MGSFB * sizeof(int32_t));
  }
  struct TOOLSINFO tools;
  memset(&tools, 0, sizeof(tools));
  for (int i = 0; i < MGSFB; i++) tools.msMask[i] = (UCHAR)msMask[i];

  ATS_ELEMENT ats;
  memset(&ats, 0, sizeof(ats));
  for (int ch = 0; ch < nChannels; ch++) {
    ats.chaosMeasureEnFac[ch] = chaosMeasureEnFac[ch];
    ats.lastEnFacPatch[ch] = lastEnFacPatch[ch];
  }

  FDKaacEnc_calcWeighting(&pe, psy, qc, &tools, &ats, nChannels, 1);

  for (int ch = 0; ch < nChannels; ch++) {
    chaosMeasureEnFac[ch] = ats.chaosMeasureEnFac[ch];
    lastEnFacPatch[ch] = ats.lastEnFacPatch[ch];
    memcpy(sfbEnFacLdOut + ch * MGSFB, qc[ch]->sfbEnFacLd,
           MGSFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_calc_pe runs the genuine FDKaacEnc_calcPe for nChannels.
void aparity_calc_pe(int nChannels, const int32_t *sfbWeightedEnergyLdData,
                     const int32_t *sfbThresholdLdData, const int32_t *sfbNLines,
                     const int *isBook, const int *isScale, int sfbCnt,
                     int sfbPerGroup, int maxSfbPerGroup, int peOffset,
                     int32_t *peOut, int32_t *constPartOut,
                     int32_t *nActiveLinesOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  PE_DATA pe;
  memset(&pe, 0, sizeof(pe));
  pe.offset = peOffset;
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, 0);
    memcpy(qc[ch]->sfbWeightedEnergyLdData, sfbWeightedEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++) {
      psy[ch]->isBook[i] = isBook[ch * MGSFB + i];
      psy[ch]->isScale[i] = isScale[ch * MGSFB + i];
    }
    memcpy(pe.peChannelData[ch].sfbNLines, sfbNLines + ch * MGSFB,
           MGSFB * sizeof(int32_t));
  }
  FDKaacEnc_calcPe(psy, qc, &pe, nChannels);
  *peOut = pe.pe;
  *constPartOut = pe.constPart;
  *nActiveLinesOut = pe.nActiveLines;
  for (int ch = 0; ch < nChannels; ch++) {
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_init_avoid_hole_flag runs the genuine FDKaacEnc_initAvoidHoleFlag.
void aparity_init_avoid_hole_flag(
    int nChannels, const int32_t *sfbSpreadEnergy, const int32_t *sfbEnergy,
    const int32_t *sfbEnergyLdData, const int32_t *sfbMinSnrLdData,
    const int *sfbOffset, const int *lastWindowSequence, const int32_t *msMask,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int modifyMinSnr,
    uint8_t *ahFlagOut, int32_t *sfbSpreadEnergyOut, int32_t *sfbMinSnrLdDataOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
    memcpy(qc[ch]->sfbSpreadEnergy, sfbSpreadEnergy + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergy, sfbEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbMinSnrLdData, sfbMinSnrLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    psy[ch]->lastWindowSequence = lastWindowSequence[ch];
  }
  struct TOOLSINFO tools;
  memset(&tools, 0, sizeof(tools));
  for (int i = 0; i < MGSFB; i++) tools.msMask[i] = (UCHAR)msMask[i];

  AH_PARAM ahParam;
  memset(&ahParam, 0, sizeof(ahParam));
  ahParam.modifyMinSnr = modifyMinSnr;

  UCHAR ahFlag[2][MAX_GROUPED_SFB];
  memset(ahFlag, 0, sizeof(ahFlag));

  FDKaacEnc_initAvoidHoleFlag(qc, psy, ahFlag, &tools, nChannels, &ahParam);

  for (int ch = 0; ch < nChannels; ch++) {
    for (int i = 0; i < MGSFB; i++)
      ahFlagOut[ch * MGSFB + i] = ahFlag[ch][i];
    memcpy(sfbSpreadEnergyOut + ch * MGSFB, qc[ch]->sfbSpreadEnergy,
           MGSFB * sizeof(int32_t));
    memcpy(sfbMinSnrLdDataOut + ch * MGSFB, qc[ch]->sfbMinSnrLdData,
           MGSFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_reduce_thresholds_cbr runs the genuine FDKaacEnc_reduceThresholdsCBR.
void aparity_reduce_thresholds_cbr(
    int nChannels, const int32_t *sfbWeightedEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbMinSnrLdData,
    const uint8_t *ahFlagIn, const int32_t *thrExp, int sfbCnt, int sfbPerGroup,
    int maxSfbPerGroup, int32_t redValM, int redValE,
    int32_t *sfbThresholdLdDataOut, uint8_t *ahFlagOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  UCHAR ahFlag[2][MAX_GROUPED_SFB];
  FIXP_DBL thrExpM[2][MAX_GROUPED_SFB];
  memset(ahFlag, 0, sizeof(ahFlag));
  memset(thrExpM, 0, sizeof(thrExpM));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, 0);
    memcpy(qc[ch]->sfbWeightedEnergyLdData, sfbWeightedEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbMinSnrLdData, sfbMinSnrLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++) {
      ahFlag[ch][i] = ahFlagIn[ch * MGSFB + i];
      thrExpM[ch][i] = (FIXP_DBL)thrExp[ch * MGSFB + i];
    }
  }

  FDKaacEnc_reduceThresholdsCBR(qc, psy, ahFlag, thrExpM, nChannels,
                                (FIXP_DBL)redValM, (SCHAR)redValE);

  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(sfbThresholdLdDataOut + ch * MGSFB, qc[ch]->sfbThresholdLdData,
           MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++)
      ahFlagOut[ch * MGSFB + i] = ahFlag[ch][i];
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_calc_chaos_measure runs the genuine FDKaacEnc_calcChaosMeasure for one
// channel.
int32_t aparity_calc_chaos_measure(const int32_t *sfbEnergyLdData,
                                   const int32_t *sfbThresholdLdData,
                                   const int32_t *sfbEnergy,
                                   const int32_t *sfbFormFactorLdData,
                                   const int *sfbOffset, int sfbCnt,
                                   int sfbPerGroup, int maxSfbPerGroup) {
  QC_OUT_CHANNEL *qc;
  PSY_OUT_CHANNEL *psy;
  allocChan(&qc, &psy, sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
  memcpy(qc->sfbEnergyLdData, sfbEnergyLdData, MGSFB * sizeof(int32_t));
  memcpy(qc->sfbThresholdLdData, sfbThresholdLdData, MGSFB * sizeof(int32_t));
  memcpy(qc->sfbEnergy, sfbEnergy, MGSFB * sizeof(int32_t));

  FIXP_DBL r = FDKaacEnc_calcChaosMeasure(psy, (const FIXP_DBL *)sfbFormFactorLdData);
  free(qc);
  free(psy);
  return (int32_t)r;
}

// aparity_reduce_thresholds_vbr runs the genuine static
// FDKaacEnc_reduceThresholdsVBR for nChannels (long or short block per
// lastWindowSequence). It builds qc/psy with the per-channel weighted-energy /
// threshold / minSnr / formFactor / energy rows, the avoid-hole flags and the
// thrExp matrix, threads through the in/out chaosMeasureOld, and copies out the
// reduced thresholds + updated avoid-hole flags. sfbFormFactorLdData is read off
// qc (psy aliases it). sfbOffset/groupLen/lastWindowSequence drive the
// per-window-type path.
void aparity_reduce_thresholds_vbr(
    int nChannels, const int32_t *sfbWeightedEnergyLdData,
    const int32_t *sfbThresholdLdData, const int32_t *sfbMinSnrLdData,
    const int32_t *sfbFormFactorLdData, const int32_t *sfbEnergy,
    const int32_t *sfbEnergyLdData, const uint8_t *ahFlagIn,
    const int32_t *thrExp, const int *sfbOffset,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int lastWindowSequence,
    const int *groupLen, int32_t vbrQualFactor, int32_t *chaosMeasureOldInOut,
    int32_t *sfbThresholdLdDataOut, uint8_t *ahFlagOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  UCHAR ahFlag[2][MAX_GROUPED_SFB];
  FIXP_DBL thrExpM[2][MAX_GROUPED_SFB];
  memset(ahFlag, 0, sizeof(ahFlag));
  memset(thrExpM, 0, sizeof(thrExpM));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
    psy[ch]->lastWindowSequence = lastWindowSequence;
    for (int g = 0; g < MAX_NO_OF_GROUPS; g++) psy[ch]->groupLen[g] = groupLen[g];
    memcpy(qc[ch]->sfbWeightedEnergyLdData, sfbWeightedEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbMinSnrLdData, sfbMinSnrLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbFormFactorLdData, sfbFormFactorLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergy, sfbEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++) {
      ahFlag[ch][i] = ahFlagIn[ch * MGSFB + i];
      thrExpM[ch][i] = (FIXP_DBL)thrExp[ch * MGSFB + i];
    }
  }

  FDKaacEnc_reduceThresholdsVBR(qc, psy, ahFlag, thrExpM, nChannels,
                                (FIXP_DBL)vbrQualFactor,
                                (FIXP_DBL *)chaosMeasureOldInOut);

  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(sfbThresholdLdDataOut + ch * MGSFB, qc[ch]->sfbThresholdLdData,
           MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++)
      ahFlagOut[ch * MGSFB + i] = ahFlag[ch][i];
    free(qc[ch]);
    free(psy[ch]);
  }
}

// --- A-leaves: the tiny threshold-adjustment statics -------------------------

// aparity_calc_thresh_exp runs the genuine static FDKaacEnc_calcThreshExp for
// nChannels, copying out the per-channel thrExp rows.
void aparity_calc_thresh_exp(int nChannels, const int32_t *sfbThresholdLdData,
                             const int *sfbCnt, const int *sfbPerGroup,
                             const int *maxSfbPerGroup, int32_t *thrExpOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  FIXP_DBL thrExp[2][MAX_GROUPED_SFB];
  memset(thrExp, 0, sizeof(thrExp));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt[ch], sfbPerGroup[ch], maxSfbPerGroup[ch], 0);
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MGSFB,
           MGSFB * sizeof(int32_t));
  }
  FDKaacEnc_calcThreshExp(thrExp, qc, psy, nChannels);
  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(thrExpOut + ch * MGSFB, thrExp[ch], MGSFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_adapt_min_snr runs the genuine static FDKaacEnc_adaptMinSnr for
// nChannels, copying out the updated per-channel sfbMinSnrLdData.
void aparity_adapt_min_snr(int nChannels, const int32_t *sfbEnergy,
                           const int32_t *sfbEnergyLdData,
                           const int32_t *sfbMinSnrLdData, const int *sfbCnt,
                           const int *sfbPerGroup, const int *maxSfbPerGroup,
                           int32_t maxRed, int32_t startRatio,
                           int32_t redRatioFac, int32_t redOffs,
                           int32_t *sfbMinSnrLdDataOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt[ch], sfbPerGroup[ch], maxSfbPerGroup[ch], 0);
    memcpy(qc[ch]->sfbEnergy, sfbEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbMinSnrLdData, sfbMinSnrLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
  }
  MINSNR_ADAPT_PARAM msa;
  memset(&msa, 0, sizeof(msa));
  msa.maxRed = (FIXP_DBL)maxRed;
  msa.startRatio = (FIXP_DBL)startRatio;
  msa.redRatioFac = (FIXP_DBL)redRatioFac;
  msa.redOffs = (FIXP_DBL)redOffs;
  FDKaacEnc_adaptMinSnr(qc, psy, &msa, nChannels);
  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(sfbMinSnrLdDataOut + ch * MGSFB, qc[ch]->sfbMinSnrLdData,
           MGSFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_reset_ah_flags runs the genuine static FDKaacEnc_resetAHFlags.
void aparity_reset_ah_flags(int nChannels, const uint8_t *ahFlagIn,
                            const int *sfbCnt, const int *sfbPerGroup,
                            const int *maxSfbPerGroup, uint8_t *ahFlagOut) {
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  UCHAR ahFlag[2][MAX_GROUPED_SFB];
  memset(ahFlag, 0, sizeof(ahFlag));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt[ch], sfbPerGroup[ch], maxSfbPerGroup[ch], 0);
    for (int i = 0; i < MGSFB; i++) ahFlag[ch][i] = ahFlagIn[ch * MGSFB + i];
  }
  FDKaacEnc_resetAHFlags(ahFlag, nChannels, psy);
  for (int ch = 0; ch < nChannels; ch++) {
    for (int i = 0; i < MGSFB; i++) ahFlagOut[ch * MGSFB + i] = ahFlag[ch][i];
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_calc_pe_no_ah runs the genuine static
// FDKaacEnc_FDKaacEnc_calcPeNoAH, returning (pe, constPart, nActiveLines).
void aparity_calc_pe_no_ah(int nChannels, int32_t offset, const int32_t *sfbPe,
                           const int32_t *sfbConstPart,
                           const int32_t *sfbNActiveLines, const uint8_t *ahFlagIn,
                           const int *sfbCnt, const int *sfbPerGroup,
                           const int *maxSfbPerGroup, int *peOut, int *constPartOut,
                           int *nActiveLinesOut) {
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PE_DATA pe;
  memset(&pe, 0, sizeof(pe));
  pe.offset = offset;
  UCHAR ahFlag[2][MAX_GROUPED_SFB];
  memset(ahFlag, 0, sizeof(ahFlag));
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt[ch], sfbPerGroup[ch], maxSfbPerGroup[ch], 0);
    memcpy(pe.peChannelData[ch].sfbPe, sfbPe + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(pe.peChannelData[ch].sfbConstPart, sfbConstPart + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(pe.peChannelData[ch].sfbNActiveLines, sfbNActiveLines + ch * MGSFB, MGSFB * sizeof(int32_t));
    for (int i = 0; i < MGSFB; i++) ahFlag[ch][i] = ahFlagIn[ch * MGSFB + i];
  }
  INT peO = 0, cpO = 0, nalO = 0;
  FDKaacEnc_FDKaacEnc_calcPeNoAH(&peO, &cpO, &nalO, &pe, ahFlag, psy, nChannels);
  *peOut = peO;
  *constPartOut = cpO;
  *nActiveLinesOut = nalO;
  for (int ch = 0; ch < nChannels; ch++) {
    free(qc[ch]);
    free(psy[ch]);
  }
}

// aparity_calc_bit_save runs the genuine static FDKaacEnc_calcBitSave.
int32_t aparity_calc_bit_save(int32_t fillLevel, int32_t clipLow, int32_t clipHigh,
                              int32_t minBitSave, int32_t maxBitSave,
                              int32_t bitsaveSlope) {
  return (int32_t)FDKaacEnc_calcBitSave(fillLevel, clipLow, clipHigh, minBitSave,
                                        maxBitSave, bitsaveSlope);
}

// aparity_calc_bit_spend runs the genuine static FDKaacEnc_calcBitSpend.
int32_t aparity_calc_bit_spend(int32_t fillLevel, int32_t clipLow, int32_t clipHigh,
                               int32_t minBitSpend, int32_t maxBitSpend,
                               int32_t bitspendSlope) {
  return (int32_t)FDKaacEnc_calcBitSpend(fillLevel, clipLow, clipHigh, minBitSpend,
                                         maxBitSpend, bitspendSlope);
}

// aparity_adjust_pe_min_max runs the genuine static FDKaacEnc_adjustPeMinMax.
void aparity_adjust_pe_min_max(int currPe, int *peMin, int *peMax) {
  FDKaacEnc_adjustPeMinMax(currPe, peMin, peMax);
}

// aparity_adjust_thresholds runs the GENUINE non-static FDKaacEnc_AdjustThresholds
// top entry for a single CBR AAC-LC element (SCE or CPE, INTRA bit-distribution).
// It builds the full CHANNEL_MAPPING / QC_OUT_ELEMENT / PSY_OUT_ELEMENT / QC_OUT /
// ADJ_THR_STATE state from flat inputs (seeding the ATS_ELEMENT params directly,
// as FDKaacEnc_AdjThrInit would), allocates the dynMem_* scratch the reduction
// heart aliases, runs the driver, and copies the mutated sfbThresholdLdData out.
void aparity_adjust_thresholds(
    int nChannels, int elType, const int32_t *sfbEnergy,
    const int32_t *sfbEnergyLdData, const int32_t *sfbThresholdLdData,
    const int32_t *sfbWeightedEnergyLdData, const int32_t *sfbSpreadEnergy,
    const int32_t *sfbMinSnrLdData, const int32_t *sfbFormFactorLdData,
    const int32_t *sfbEnFacLd, const int32_t *sfbPe, const int32_t *sfbConstPart,
    const int32_t *sfbNActiveLines, const int32_t *sfbNLines, const int *sfbOffset,
    const int *lastWindowSequence, const int32_t *msMask, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup, int peOffset, int modifyMinSnr,
    int startSfbL, int startSfbS, int32_t maxRed, int32_t startRatio,
    int32_t redRatioFac, int32_t redOffs, int maxIter2ndGuess, int grantedPeCorr,
    int32_t pe, int32_t constPart, int32_t nActiveLines,
    int32_t *sfbThresholdLdDataOut) {
  CHANNEL_MAPPING cm;
  memset(&cm, 0, sizeof(cm));
  cm.nElements = 1;
  cm.nChannels = nChannels;
  cm.elInfo[0].elType = (MP4_ELEMENT_ID)elType;
  cm.elInfo[0].nChannelsInEl = nChannels;
  cm.elInfo[0].ChannelIndex[0] = 0;
  if (nChannels == 2) cm.elInfo[0].ChannelIndex[1] = 1;

  QC_OUT_ELEMENT qcEl;
  memset(&qcEl, 0, sizeof(qcEl));
  PSY_OUT_ELEMENT psyEl;
  memset(&psyEl, 0, sizeof(psyEl));
  qcEl.grantedPeCorr = grantedPeCorr;
  qcEl.peData.pe = pe;
  qcEl.peData.constPart = constPart;
  qcEl.peData.nActiveLines = nActiveLines;

  QC_OUT_CHANNEL *qc[2] = {0, 0};
  PSY_OUT_CHANNEL *psy[2] = {0, 0};
  for (int ch = 0; ch < nChannels; ch++) {
    allocChan(&qc[ch], &psy[ch], sfbCnt, sfbPerGroup, maxSfbPerGroup, sfbOffset);
    memcpy(qc[ch]->sfbEnergy, sfbEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnergyLdData, sfbEnergyLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbThresholdLdData, sfbThresholdLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbWeightedEnergyLdData, sfbWeightedEnergyLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbSpreadEnergy, sfbSpreadEnergy + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbMinSnrLdData, sfbMinSnrLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbFormFactorLdData, sfbFormFactorLdData + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qc[ch]->sfbEnFacLd, sfbEnFacLd + ch * MGSFB, MGSFB * sizeof(int32_t));
    psy[ch]->lastWindowSequence = lastWindowSequence[ch];

    memcpy(qcEl.peData.peChannelData[ch].sfbPe, sfbPe + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qcEl.peData.peChannelData[ch].sfbConstPart, sfbConstPart + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qcEl.peData.peChannelData[ch].sfbNActiveLines, sfbNActiveLines + ch * MGSFB, MGSFB * sizeof(int32_t));
    memcpy(qcEl.peData.peChannelData[ch].sfbNLines, sfbNLines + ch * MGSFB, MGSFB * sizeof(int32_t));

    qcEl.qcOutChannel[ch] = qc[ch];
    psyEl.psyOutChannel[ch] = psy[ch];
  }
  for (int i = 0; i < MGSFB; i++) psyEl.toolsInfo.msMask[i] = (UCHAR)msMask[i];

  /* dynMem scratch the reduction heart aliases (indexed by elementId; element 0
   * holds the single shared buffer). Sized for [8][2][MAX_GROUPED_SFB]. */
  static UCHAR ahScratch[8][2][MAX_GROUPED_SFB];
  static FIXP_DBL thrExpScratch[8][2][MAX_GROUPED_SFB];
  static FIXP_DBL nActLinesScratch[8][2][MAX_GROUPED_SFB];
  memset(ahScratch, 0, sizeof(ahScratch));
  memset(thrExpScratch, 0, sizeof(thrExpScratch));
  memset(nActLinesScratch, 0, sizeof(nActLinesScratch));
  qcEl.dynMem_Ah_Flag = (UCHAR *)ahScratch;
  qcEl.dynMem_Thr_Exp = (UCHAR *)thrExpScratch;
  qcEl.dynMem_SfbNActiveLinesLdData = (UCHAR *)nActLinesScratch;

  QC_OUT_ELEMENT *qcElement[8] = {0};
  PSY_OUT_ELEMENT *psyOutElement[8] = {0};
  qcElement[0] = &qcEl;
  psyOutElement[0] = &psyEl;

  QC_OUT qcOut;
  memset(&qcOut, 0, sizeof(qcOut));

  ADJ_THR_STATE st;
  memset(&st, 0, sizeof(st));
  st.bitDistributionMode = AACENC_BD_MODE_INTRA_ELEMENT;
  st.maxIter2ndGuess = maxIter2ndGuess;
  ATS_ELEMENT ats;
  memset(&ats, 0, sizeof(ats));
  ats.peOffset = peOffset;
  ats.ahParam.modifyMinSnr = modifyMinSnr;
  ats.ahParam.startSfbL = startSfbL;
  ats.ahParam.startSfbS = startSfbS;
  ats.minSnrAdaptParam.maxRed = (FIXP_DBL)maxRed;
  ats.minSnrAdaptParam.startRatio = (FIXP_DBL)startRatio;
  ats.minSnrAdaptParam.redRatioFac = (FIXP_DBL)redRatioFac;
  ats.minSnrAdaptParam.redOffs = (FIXP_DBL)redOffs;
  st.adjThrStateElem[0] = &ats;

  FDKaacEnc_AdjustThresholds(&st, qcElement, &qcOut, psyOutElement, 1, &cm);

  for (int ch = 0; ch < nChannels; ch++) {
    memcpy(sfbThresholdLdDataOut + ch * MGSFB, qc[ch]->sfbThresholdLdData,
           MGSFB * sizeof(int32_t));
    free(qc[ch]);
    free(psy[ch]);
  }
}

} // extern "C"
