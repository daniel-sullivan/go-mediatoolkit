// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC SBR high-frequency-generation tools —
 * the HE-AAC v1 (LPP) transposer (lpp_tran.cpp), its autocorrelation
 * (autocorr2nd.cpp) and the pre-flattening gain vector (HFgen_preFlat.cpp). This
 * TU provides the extern "C" entry points the Go test calls; it links the GENUINE
 * vendored sources (compiled by the sibling *_cgo.cpp TUs), so the oracle is the
 * real reference, not a hand-twin.
 *
 * It NEVER imports libraries/aac (no cross-package static-symbol clash); the Go
 * side (cgo.go) MAY and does import internal/nativeaac/sbr.
 *
 * Integer parity: the whole HF-gen subsystem is fixed-point (FIXP_DBL == int32
 * Q-format, FIXP_SGL == int16 Q1.15). The autocorrelation MACs, the LPC
 * coefficient fDivNorm/scaleValueSaturate, the patch copy/whitening filter, and
 * the polynomial-fit Cholesky are bit-identical regardless of -ffp-contract /
 * vectorization, with no transcendental — so the oracle asserts EXACT int
 * equality.
 *
 * HBE (hbe.cpp / harmonic SBR / sbrPatchingMode==0) is USAC-only and out of
 * HE-AAC v1 scope (see the Go port's lpp_tran.go) — not linked or exercised here.
 */

#include <stdint.h>
#include <string.h>

#include "lpp_tran.h"
#include "sbr_rom.h"
#include "HFgen_preFlat.h"
#include "autocorr2nd.h"

extern "C" {

/* --- ROM table copies ----------------------------------------------------- */

/* qparity_whFactorsIndex copies the genuine FDK_sbrDecoder_sbr_whFactorsIndex
 * (USHORT) so the Go whitening ROM can be verified entry-for-entry. */
void qparity_whFactorsIndex(uint16_t *out, int count) {
  for (int i = 0; i < count; i++) out[i] = (uint16_t)FDK_sbrDecoder_sbr_whFactorsIndex[i];
}

/* qparity_whFactorsTable copies the first 5 (used) columns of each row of the
 * genuine FDK_sbrDecoder_sbr_whFactorsTable (FIXP_DBL) flattened. */
void qparity_whFactorsTable(int32_t *out, int rows) {
  for (int r = 0; r < rows; r++)
    for (int c = 0; c < 5; c++)
      out[r * 5 + c] = (int32_t)FDK_sbrDecoder_sbr_whFactorsTable[r][c];
}

/* --- autocorr2nd ---------------------------------------------------------- */

/* qparity_autoCorr2nd_real runs the genuine autoCorr2nd_real over buf (which
 * holds the two history samples at buf[base-2], buf[base-1] then `length` data
 * samples) and returns its coefficients via the out pointers + the scaling
 * return. */
int qparity_autoCorr2nd_real(const int32_t *buf, int base, int length,
                             int32_t *r11r, int32_t *r22r, int32_t *r01r,
                             int32_t *r12r, int32_t *r02r, int32_t *det,
                             int *det_scale) {
  ACORR_COEFS ac;
  memset(&ac, 0, sizeof(ac));
  int scaling = autoCorr2nd_real(&ac, (const FIXP_DBL *)buf + base, length);
  *r11r = (int32_t)ac.r11r;
  *r22r = (int32_t)ac.r22r;
  *r01r = (int32_t)ac.r01r;
  *r12r = (int32_t)ac.r12r;
  *r02r = (int32_t)ac.r02r;
  *det = (int32_t)ac.det;
  *det_scale = ac.det_scale;
  return scaling;
}

/* qparity_autoCorr2nd_cplx runs the genuine autoCorr2nd_cplx. */
int qparity_autoCorr2nd_cplx(const int32_t *re, const int32_t *im, int base,
                             int length, int32_t *r00r, int32_t *r11r,
                             int32_t *r22r, int32_t *r01r, int32_t *r12r,
                             int32_t *r01i, int32_t *r12i, int32_t *r02r,
                             int32_t *r02i, int32_t *det, int *det_scale) {
  ACORR_COEFS ac;
  memset(&ac, 0, sizeof(ac));
  int scaling = autoCorr2nd_cplx(&ac, (const FIXP_DBL *)re + base,
                                 (const FIXP_DBL *)im + base, length);
  *r00r = (int32_t)ac.r00r;
  *r11r = (int32_t)ac.r11r;
  *r22r = (int32_t)ac.r22r;
  *r01r = (int32_t)ac.r01r;
  *r12r = (int32_t)ac.r12r;
  *r01i = (int32_t)ac.r01i;
  *r12i = (int32_t)ac.r12i;
  *r02r = (int32_t)ac.r02r;
  *r02i = (int32_t)ac.r02i;
  *det = (int32_t)ac.det;
  *det_scale = ac.det_scale;
  return scaling;
}

/* --- HFgen pre-flattening ------------------------------------------------- */

/* qparity_calculateGainVec runs the genuine sbrDecoder_calculateGainVec over
 * slot-major QMF energy buffers (realFlat/imagFlat are nSlots*64), writing the
 * per-band gain mantissas (gain) and exponents (gainExp). */
void qparity_calculateGainVec(const int32_t *realFlat, const int32_t *imagFlat,
                              int nSlots, int sourceBuf_e_overlap,
                              int sourceBuf_e_current, int overlap, int numBands,
                              int startSample, int stopSample, int32_t *gain,
                              int *gainExp) {
  FIXP_DBL *re[64];
  FIXP_DBL *im[64];
  for (int i = 0; i < nSlots; i++) {
    re[i] = (FIXP_DBL *)realFlat + i * 64;
    im[i] = (FIXP_DBL *)imagFlat + i * 64;
  }
  sbrDecoder_calculateGainVec(re, im, sourceBuf_e_overlap, sourceBuf_e_current,
                              overlap, (FIXP_DBL *)gain, gainExp, numBands,
                              startSample, stopSample);
}

/* --- LPP transposer ------------------------------------------------------- */

/* qparity_resetLppTransposer runs createLppTransposer (chan 0 ->
 * resetLppTransposer) over a fresh TRANSPOSER_SETTINGS and returns the computed
 * patch layout. The patch arrays are written as MAX_NUM_PATCHES+1 == 7 entries
 * each; bwBorders is MAX_NUM_NOISE_VALUES == 10. */
int qparity_resetLppTransposer(int highBandStartSb, const uint8_t *vKMaster,
                               int numMaster, int usb, int timeSlots, int nCols,
                               const uint8_t *noiseBandTable, int noNoiseBands,
                               unsigned int fs, int overlap, int *noOfPatches,
                               int *lbStartPatching, int *lbStopPatching,
                               uint8_t *srcStart, uint8_t *srcStop,
                               uint8_t *tgtStart, uint8_t *tgtOffs,
                               uint8_t *guardStart, uint8_t *numBandsArr,
                               uint8_t *bwBorders, int32_t *whFactors) {
  SBR_LPP_TRANS hs;
  TRANSPOSER_SETTINGS st;
  memset(&hs, 0, sizeof(hs));
  memset(&st, 0, sizeof(st));

  SBR_ERROR err =
      createLppTransposer(&hs, &st, highBandStartSb, (UCHAR *)vKMaster, numMaster,
                          usb, timeSlots, nCols, (UCHAR *)noiseBandTable,
                          noNoiseBands, fs, 0, overlap);

  *noOfPatches = st.noOfPatches;
  *lbStartPatching = st.lbStartPatching;
  *lbStopPatching = st.lbStopPatching;
  for (int p = 0; p <= MAX_NUM_PATCHES; p++) {
    srcStart[p] = st.patchParam[p].sourceStartBand;
    srcStop[p] = st.patchParam[p].sourceStopBand;
    tgtStart[p] = st.patchParam[p].targetStartBand;
    tgtOffs[p] = st.patchParam[p].targetBandOffs;
    guardStart[p] = st.patchParam[p].guardStartBand;
    numBandsArr[p] = st.patchParam[p].numBandsInPatch;
  }
  for (int i = 0; i < MAX_NUM_NOISE_VALUES; i++) bwBorders[i] = st.bwBorders[i];
  whFactors[0] = (int32_t)st.whFactors.off;
  whFactors[1] = (int32_t)st.whFactors.transitionLevel;
  whFactors[2] = (int32_t)st.whFactors.lowLevel;
  whFactors[3] = (int32_t)st.whFactors.midLevel;
  whFactors[4] = (int32_t)st.whFactors.highLevel;

  return (int)err;
}

/* qparity_lppTransposer runs the full genuine lppTransposer over slot-major QMF
 * buffers (realFlat/imagFlat each nSlots*64, mutated in place). It builds the
 * patch layout via createLppTransposer first (chan 0), mirroring the Go driver.
 * invfMode/invfModePrev are per-band ints (length nInvfBands). Returns hb_scale
 * via *hbScale and writes degreeAlias[64]. */
void qparity_lppTransposer(int32_t *realFlat, int32_t *imagFlat, int nSlots,
                           int highBandStartSb, const uint8_t *vKMaster,
                           int numMaster, int usb, int timeSlots, int nCols,
                           const uint8_t *noiseBandTable, int noNoiseBands,
                           unsigned int fs, int overlap, int lbScale,
                           int ovLbScale, int useLP, int fPreWhitening,
                           int vKMaster0, int timeStep, int firstSlotOffs,
                           int lastSlotOffs, int nInvfBands, const int *invfMod,
                           const int *invfModPrev, int32_t *degreeAlias,
                           int *hbScale) {
  SBR_LPP_TRANS hs;
  TRANSPOSER_SETTINGS st;
  memset(&hs, 0, sizeof(hs));
  memset(&st, 0, sizeof(st));

  createLppTransposer(&hs, &st, highBandStartSb, (UCHAR *)vKMaster, numMaster,
                      usb, timeSlots, nCols, (UCHAR *)noiseBandTable,
                      noNoiseBands, fs, 0, overlap);

  FIXP_DBL *re[64];
  FIXP_DBL *im[64];
  for (int i = 0; i < nSlots; i++) {
    re[i] = (FIXP_DBL *)realFlat + i * 64;
    im[i] = (FIXP_DBL *)imagFlat + i * 64;
  }

  QMF_SCALE_FACTOR sf;
  memset(&sf, 0, sizeof(sf));
  sf.lb_scale = lbScale;
  sf.ov_lb_scale = ovLbScale;

  FIXP_DBL deg[64];
  memset(deg, 0, sizeof(deg));

  INVF_MODE modes[MAX_INVF_BANDS];
  INVF_MODE modesPrev[MAX_INVF_BANDS];
  for (int i = 0; i < nInvfBands; i++) {
    modes[i] = (INVF_MODE)invfMod[i];
    modesPrev[i] = (INVF_MODE)invfModPrev[i];
  }

  lppTransposer(&hs, &sf, re, deg, useLP ? NULL : im, useLP, fPreWhitening,
                vKMaster0, timeStep, firstSlotOffs, lastSlotOffs, nInvfBands,
                modes, modesPrev);

  for (int i = 0; i < 64; i++) degreeAlias[i] = (int32_t)deg[i];
  *hbScale = sf.hb_scale;
}

} /* extern "C" */
