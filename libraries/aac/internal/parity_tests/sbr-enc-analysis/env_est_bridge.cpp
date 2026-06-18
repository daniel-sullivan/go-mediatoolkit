// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the QMF-energy / noise-floor / envelope LEAF kernels of the
 * Fraunhofer FDK-AAC SBR-encoder envelope estimator (libSBRenc/src/env_est.cpp).
 *
 * Those kernels are file-static in env_est.cpp, so this TU #includes the GENUINE
 * vendored env_est.cpp and wraps the statics in extern "C" entry points compiled
 * in the same translation unit (the standard way to pin a static against its 1:1
 * port without a hand-twin). The oracle therefore exercises the real reference.
 *
 * env_est.cpp's *non-static* orchestrators (FDKsbrEnc_extractSbrEnvelope1/2) are
 * compiled too and reference a handful of encoder symbols that are OUT of this
 * batch's scope (codeEnvelope / TonCorrParamExtr / huffman / bitstream writers).
 * Those orchestrators are NEVER called by the parity test; we provide trap stubs
 * for the otherwise-undefined symbols so the TU links. (transientDetect /
 * frameSplitter / fastTransientDetect / frameInfoGenerator are NOT stubbed here —
 * the genuine tran_det.cpp / fram_gen.cpp TUs in this package define them.)
 *
 * Fixed-point => EXACT int parity. */

#include <stdint.h>
#include <string.h>

#include "env_est.cpp"

/* --- trap stubs for the unreferenced extractSbrEnvelope1/2 dependency edges ---
 * Signatures copied verbatim from code_env.h / ton_corr.h / bit_sbr.h so they
 * match the already-included declarations exactly. Never called. */
void FDKsbrEnc_codeEnvelope(SCHAR *, const FREQ_RES *, SBR_CODE_ENVELOPE *,
                            INT *, INT, INT, INT, INT) {}
void FDKsbrEnc_TonCorrParamExtr(HANDLE_SBR_TON_CORR_EST, INVF_MODE *, FIXP_DBL *,
                                INT *, UCHAR *, UCHAR *, const SBR_FRAME_INFO *,
                                UCHAR *, UCHAR *, INT, XPOS_MODE, UINT) {}
INT FDKsbrEnc_InitSbrHuffmanTables(struct SBR_ENV_DATA *,
                                   HANDLE_SBR_CODE_ENVELOPE,
                                   HANDLE_SBR_CODE_ENVELOPE, AMP_RES) {
  return 0;
}
void FDKsbrEnc_CalculateTonalityQuotas(HANDLE_SBR_TON_CORR_EST, FIXP_DBL **,
                                       FIXP_DBL **, INT, INT) {}
INT FDKsbrEnc_CountSbrChannelPairElement(
    struct SBR_HEADER_DATA *, struct T_PARAMETRIC_STEREO *,
    struct SBR_BITSTREAM_DATA *, struct SBR_ENV_DATA *, struct SBR_ENV_DATA *,
    struct COMMON_DATA *, UINT) {
  return 0;
}
INT FDKsbrEnc_WriteEnvChannelPairElement(
    struct SBR_HEADER_DATA *, struct T_PARAMETRIC_STEREO *,
    struct SBR_BITSTREAM_DATA *, struct SBR_ENV_DATA *, struct SBR_ENV_DATA *,
    struct COMMON_DATA *, UINT) {
  return 0;
}
INT FDKsbrEnc_WriteEnvSingleChannelElement(
    struct SBR_HEADER_DATA *, struct T_PARAMETRIC_STEREO *,
    struct SBR_BITSTREAM_DATA *, struct SBR_ENV_DATA *, struct COMMON_DATA *,
    UINT) {
  return 0;
}

extern "C" {

/* eparity_energy runs FDKsbrEnc_getEnergyFromCplxQmfData (timeslot-pair) over
 * numberCols rows (stride numberBands) of real/imag (mutated in place), writing
 * the energy matrix (numberCols/2 rows) and the updated qmf/energy scales. */
void eparity_energy(int32_t *realFlat, int32_t *imagFlat, int numberBands,
                    int numberCols, int qmfScaleIn, int32_t *energyOut,
                    int *qmfScaleOut, int *energyScaleOut) {
  FIXP_DBL *real[64], *imag[64], *energy[32];
  for (int k = 0; k < numberCols; k++) {
    real[k] = (FIXP_DBL *)realFlat + (long)k * numberBands;
    imag[k] = (FIXP_DBL *)imagFlat + (long)k * numberBands;
  }
  for (int k = 0; k < numberCols / 2; k++)
    energy[k] = (FIXP_DBL *)energyOut + (long)k * numberBands;

  int qmfScale = qmfScaleIn, energyScale = 0;
  FDKsbrEnc_getEnergyFromCplxQmfData(energy, real, imag, numberBands, numberCols,
                                     &qmfScale, &energyScale);
  *qmfScaleOut = qmfScale;
  *energyScaleOut = energyScale;
}

/* eparity_energy_full runs the per-timeslot FDKsbrEnc_getEnergyFromCplxQmfDataFull. */
void eparity_energy_full(int32_t *realFlat, int32_t *imagFlat, int numberBands,
                         int numberCols, int qmfScaleIn, int32_t *energyOut,
                         int *qmfScaleOut, int *energyScaleOut) {
  FIXP_DBL *real[16], *imag[16], *energy[16];
  for (int k = 0; k < numberCols; k++) {
    real[k] = (FIXP_DBL *)realFlat + (long)k * numberBands;
    imag[k] = (FIXP_DBL *)imagFlat + (long)k * numberBands;
    energy[k] = (FIXP_DBL *)energyOut + (long)k * numberBands;
  }
  int qmfScale = qmfScaleIn, energyScale = 0;
  FDKsbrEnc_getEnergyFromCplxQmfDataFull(energy, real, imag, numberBands,
                                         numberCols, &qmfScale, &energyScale);
  *qmfScaleOut = qmfScale;
  *energyScaleOut = energyScale;
}

/* eparity_tonality runs FDKsbrEnc_GetTonality. */
int32_t eparity_tonality(const int32_t *quotaFlat, int totEst, int qmfChannels,
                         const int32_t *energyFlat, int numberCols,
                         int noEstPerFrame, int startIndex, int startBand,
                         int stopBand) {
  const FIXP_DBL *quota[4];
  const FIXP_DBL *energy[16];
  for (int e = 0; e < totEst; e++)
    quota[e] = (const FIXP_DBL *)quotaFlat + (long)e * qmfChannels;
  for (int k = 0; k < numberCols; k++)
    energy[k] = (const FIXP_DBL *)energyFlat + (long)k * qmfChannels;
  return (int32_t)FDKsbrEnc_GetTonality(quota, noEstPerFrame, startIndex, energy,
                                        (UCHAR)startBand, stopBand, numberCols);
}

/* eparity_map_panorama runs mapPanorama. */
int eparity_map_panorama(int nrgVal, int ampRes, int *quantError) {
  return mapPanorama(nrgVal, ampRes, quantError);
}

/* eparity_noise_quant runs sbrNoiseFloorLevelsQuantisation. */
void eparity_noise_quant(const int32_t *noiseLevels, int coupling,
                         signed char *out) {
  sbrNoiseFloorLevelsQuantisation((SCHAR *)out, (FIXP_DBL *)noiseLevels,
                                  coupling);
}

/* eparity_couple_noise runs coupleNoiseFloor (mutates left/right in place). */
void eparity_couple_noise(int32_t *left, int32_t *right) {
  coupleNoiseFloor((FIXP_DBL *)left, (FIXP_DBL *)right);
}

/* eparity_env_sfb_energy runs getEnvSfbEnergy over a flat YBuffer. */
int32_t eparity_env_sfb_energy(int li, int ui, int startPos, int stopPos,
                               int borderPos, int32_t *yFlat, int numYRows,
                               int qmfChannels, int yBufferSzShift, int scaleNrg0,
                               int scaleNrg1) {
  FIXP_DBL *y[64];
  for (int r = 0; r < numYRows; r++)
    y[r] = (FIXP_DBL *)yFlat + (long)r * qmfChannels;
  return (int32_t)getEnvSfbEnergy(li, ui, startPos, stopPos, borderPos, y,
                                  yBufferSzShift, scaleNrg0, scaleNrg1);
}

/* eparity_mh_lowering / eparity_nmh_lowering run the compensation leaves. */
int32_t eparity_mh_lowering(int32_t nrg, int M) {
  return (int32_t)mhLoweringEnergy(nrg, M);
}
int32_t eparity_nmh_lowering(int32_t nrg, int32_t nrgSum, int nrgSumScale,
                             int M) {
  return (int32_t)nmhLoweringEnergy(nrg, nrgSum, nrgSumScale, M);
}

} /* extern "C" */
