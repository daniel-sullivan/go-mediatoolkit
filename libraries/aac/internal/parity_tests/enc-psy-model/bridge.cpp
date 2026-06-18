// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC fixed-point ENCODE psychoacoustic
 * band/line-energy kernels — libAACenc/src/band_nrg.cpp. These produce the
 * per-scalefactor-band (SFB) MDCT energies the psy model and quantizer target:
 * the per-SFB headroom (FDKaacEnc_CalcSfbMaxScaleSpec, psy_main.cpp:644), the
 * block-floating-point SFB energies and their log2/ldData form
 * (FDKaacEnc_CheckBandEnergyOptim psy_main.cpp:659, CalcBandEnergyOptimLong/
 * Short psy_main.cpp:872-880), and the mid/side energies for M/S stereo
 * (FDKaacEnc_CalcBandNrgMSOpt, psy_main.cpp:1026). It also exposes the LD-domain
 * log2 helpers (CalcLdData / LdDataVector) band_nrg uses.
 *
 * This TU provides the extern "C" bridge the Go test calls; it links the GENUINE
 * vendored band_nrg.cpp + fixpoint_math.cpp (the sibling TUs), so the oracle is
 * the real reference, NOT a hand-twin (oracle_kind == real_vendored). It NEVER
 * imports libraries/aac, so there is no cross-package static-symbol clash (the
 * same amalgamation-split reasoning the sibling enc-block-switch /
 * enc-analysis-filterbank oracles document). It MAY, and the test does, import
 * the pure-Go internal/nativeaac.
 *
 * FP-parity: libfdk-aac ENCODE is FIXED-POINT — every value is an int32 FIXP_DBL
 * Q-format and every block carries its own exponent. These kernels are entirely
 * integer (leading-bit counts, arithmetic shifts, the int64-product fixmul
 * kernels, and the table-driven fLog2), bit-identical regardless of
 * -ffp-contract or vectorization, with NO transcendental. So they assert EXACT
 * int32 equality (the gate command still sets aac_strict for consistency).
 */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "common_fix.h"
#include "fixpoint_math.h"
#include "band_nrg.h"

extern "C" {

/* eparity_calc_sfb_max_scale_spec runs the genuine
 * FDKaacEnc_CalcSfbMaxScaleSpec over mdctSpectrum, writing the per-band headroom
 * into sfbMaxScaleSpec[0..numBands). bandOffset has numBands+1 entries. */
void eparity_calc_sfb_max_scale_spec(const int32_t *mdctSpectrum,
                                     const int32_t *bandOffset,
                                     int32_t *sfbMaxScaleSpec, int numBands) {
  FDKaacEnc_CalcSfbMaxScaleSpec((const FIXP_DBL *)mdctSpectrum,
                                (const INT *)bandOffset, (INT *)sfbMaxScaleSpec,
                                numBands);
}

/* eparity_check_band_energy_optim runs the genuine
 * FDKaacEnc_CheckBandEnergyOptim and returns the rescaled maxNrg; bandEnergy /
 * bandEnergyLdData are written for all numBands. */
int32_t eparity_check_band_energy_optim(const int32_t *mdctSpectrum,
                                        const int32_t *sfbMaxScaleSpec,
                                        const int32_t *bandOffset, int numBands,
                                        int32_t *bandEnergy,
                                        int32_t *bandEnergyLdData,
                                        int minSpecShift) {
  return (int32_t)FDKaacEnc_CheckBandEnergyOptim(
      (const FIXP_DBL *)mdctSpectrum, (const INT *)sfbMaxScaleSpec,
      (const INT *)bandOffset, numBands, (FIXP_DBL *)bandEnergy,
      (FIXP_DBL *)bandEnergyLdData, minSpecShift);
}

/* eparity_calc_band_energy_optim_long runs the genuine
 * FDKaacEnc_CalcBandEnergyOptimLong and returns the applied shiftBits. */
int eparity_calc_band_energy_optim_long(const int32_t *mdctSpectrum,
                                        int32_t *sfbMaxScaleSpec,
                                        const int32_t *bandOffset, int numBands,
                                        int32_t *bandEnergy,
                                        int32_t *bandEnergyLdData) {
  return FDKaacEnc_CalcBandEnergyOptimLong(
      (const FIXP_DBL *)mdctSpectrum, (INT *)sfbMaxScaleSpec,
      (const INT *)bandOffset, numBands, (FIXP_DBL *)bandEnergy,
      (FIXP_DBL *)bandEnergyLdData);
}

/* eparity_calc_band_energy_optim_short runs the genuine
 * FDKaacEnc_CalcBandEnergyOptimShort. */
void eparity_calc_band_energy_optim_short(const int32_t *mdctSpectrum,
                                          int32_t *sfbMaxScaleSpec,
                                          const int32_t *bandOffset,
                                          int numBands, int32_t *bandEnergy) {
  FDKaacEnc_CalcBandEnergyOptimShort(
      (const FIXP_DBL *)mdctSpectrum, (INT *)sfbMaxScaleSpec,
      (const INT *)bandOffset, numBands, (FIXP_DBL *)bandEnergy);
}

/* eparity_calc_band_nrg_ms_opt runs the genuine FDKaacEnc_CalcBandNrgMSOpt. */
void eparity_calc_band_nrg_ms_opt(
    const int32_t *mdctSpectrumLeft, const int32_t *mdctSpectrumRight,
    int32_t *sfbMaxScaleSpecLeft, int32_t *sfbMaxScaleSpecRight,
    const int32_t *bandOffset, int numBands, int32_t *bandEnergyMid,
    int32_t *bandEnergySide, int calcLdData, int32_t *bandEnergyMidLdData,
    int32_t *bandEnergySideLdData) {
  FDKaacEnc_CalcBandNrgMSOpt(
      (const FIXP_DBL *)mdctSpectrumLeft, (const FIXP_DBL *)mdctSpectrumRight,
      (INT *)sfbMaxScaleSpecLeft, (INT *)sfbMaxScaleSpecRight,
      (const INT *)bandOffset, numBands, (FIXP_DBL *)bandEnergyMid,
      (FIXP_DBL *)bandEnergySide, calcLdData, (FIXP_DBL *)bandEnergyMidLdData,
      (FIXP_DBL *)bandEnergySideLdData);
}

/* eparity_calc_ld_data runs the genuine CalcLdData(op) (== fLog2(op,0)). */
int32_t eparity_calc_ld_data(int32_t op) {
  return (int32_t)CalcLdData((FIXP_DBL)op);
}

/* eparity_ld_data_vector runs the genuine LdDataVector over n entries. */
void eparity_ld_data_vector(const int32_t *src, int32_t *dst, int n) {
  /* LdDataVector takes a non-const src; copy is unnecessary since it only
   * reads. Cast away const to match the vendored signature. */
  LdDataVector((FIXP_DBL *)src, (FIXP_DBL *)dst, n);
}

/* eparity_ld_consts publishes the FL2FXCONST_DBL compile-time constants the Go
 * port's band_nrg / fixpoint_log2 embed, so the test cross-checks the genuine
 * macro folding against the Go fl2fxconstDBL helper:
 *   [0] FL2FXCONST_DBL(-1.0)      [1] FL2FXCONST_DBL(2.0/64)
 *   [2] FL2FXCONST_DBL(1.0/64)    [3] FL2FXCONST_DBL(0.0) */
void eparity_ld_consts(int32_t *out) {
  out[0] = (int32_t)FL2FXCONST_DBL(-1.0);
  out[1] = (int32_t)FL2FXCONST_DBL(2.0 / 64);
  out[2] = (int32_t)FL2FXCONST_DBL(1.0 / 64);
  out[3] = (int32_t)FL2FXCONST_DBL(0.0);
}

/* eparity_ld_coeff publishes the genuine ldCoeff[] ROM (the Taylor coefficients
 * fLog2 uses), so the test verifies the Go port's transcription bit-for-bit.
 * ldCoeff is static in fixpoint_math.h; on the aarch64 build LDCOEFF_16BIT is
 * defined so it is the FIXP_SGL (int16) variant — we recompute the identical
 * FL2FXCONST_SGL literals here from the same source expressions, asserting
 * LDCOEFF_16BIT is actually on. */
void eparity_ld_coeff(int16_t *out) {
#ifndef LDCOEFF_16BIT
#error "expected LDCOEFF_16BIT on the build platform (aarch64 -> FIXP_SGL ldCoeff)"
#endif
  out[0] = (int16_t)FL2FXCONST_SGL(-1.0);
  out[1] = (int16_t)FL2FXCONST_SGL(-1.0 / 2.0);
  out[2] = (int16_t)FL2FXCONST_SGL(-1.0 / 3.0);
  out[3] = (int16_t)FL2FXCONST_SGL(-1.0 / 4.0);
  out[4] = (int16_t)FL2FXCONST_SGL(-1.0 / 5.0);
  out[5] = (int16_t)FL2FXCONST_SGL(-1.0 / 6.0);
  out[6] = (int16_t)FL2FXCONST_SGL(-1.0 / 7.0);
  out[7] = (int16_t)FL2FXCONST_SGL(-1.0 / 8.0);
  out[8] = (int16_t)FL2FXCONST_SGL(-1.0 / 9.0);
  out[9] = (int16_t)FL2FXCONST_SGL(-1.0 / 10.0);
}

} /* extern "C" */
