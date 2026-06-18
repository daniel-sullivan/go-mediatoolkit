// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder perceptual-entropy
// (line_pe.cpp) DRIVER stage. Each shim allocates the real PE_CHANNEL_DATA and
// calls the GENUINE vendored FDKaacEnc_prepareSfbPe / FDKaacEnc_calcSfbPe
// (linked from the sibling line_pe.cpp TU compiled into this test binary), then
// copies the result into flat int32 buffers for the Go side. The LD-domain
// helper shims call the genuine inline header / fixpoint_math.cpp kernels
// (CalcInvLdData, CalcLdInt, fMultNorm, fMultI). No re-derivation: the oracle is
// the real FDK code, not a Go-mirroring hand-twin.

#include "line_pe.h"
#include "fixpoint_math.h"

#include <stdint.h>
#include <string.h>

extern "C" {

// eparity_prepare_sfb_pe runs the genuine FDKaacEnc_prepareSfbPe and copies
// PE_CHANNEL_DATA.sfbNLines (MAX_GROUPED_SFB cells) into sfbNLinesOut.
void eparity_prepare_sfb_pe(const int32_t *sfbEnergyLdData,
                            const int32_t *sfbThresholdLdData,
                            const int32_t *sfbFormFactorLdData,
                            const int32_t *sfbOffset, int sfbCnt,
                            int sfbPerGroup, int maxSfbPerGroup,
                            int32_t *sfbNLinesOut) {
  PE_CHANNEL_DATA pc;
  memset(&pc, 0, sizeof(pc));
  FDKaacEnc_prepareSfbPe(&pc, (const FIXP_DBL *)sfbEnergyLdData,
                         (const FIXP_DBL *)sfbThresholdLdData,
                         (const FIXP_DBL *)sfbFormFactorLdData,
                         (const INT *)sfbOffset, sfbCnt, sfbPerGroup,
                         maxSfbPerGroup);
  memcpy(sfbNLinesOut, pc.sfbNLines, MAX_GROUPED_SFB * sizeof(int32_t));
}

// eparity_calc_sfb_pe seeds PE_CHANNEL_DATA.sfbNLines and runs the genuine
// FDKaacEnc_calcSfbPe, copying out sfbPe/sfbConstPart/sfbNActiveLines
// (MAX_GROUPED_SFB cells each) and the channel sums pe/constPart/nActiveLines.
void eparity_calc_sfb_pe(const int32_t *sfbNLines, const int32_t *sfbEnergyLdData,
                         const int32_t *sfbThresholdLdData, int sfbCnt,
                         int sfbPerGroup, int maxSfbPerGroup,
                         const int32_t *isBook, const int32_t *isScale,
                         int32_t *sfbPeOut, int32_t *sfbConstPartOut,
                         int32_t *sfbNActiveLinesOut, int32_t *peOut,
                         int32_t *constPartOut, int32_t *nActiveLinesOut) {
  PE_CHANNEL_DATA pc;
  memset(&pc, 0, sizeof(pc));
  memcpy(pc.sfbNLines, sfbNLines, MAX_GROUPED_SFB * sizeof(int32_t));
  FDKaacEnc_calcSfbPe(&pc, (const FIXP_DBL *)sfbEnergyLdData,
                      (const FIXP_DBL *)sfbThresholdLdData, sfbCnt, sfbPerGroup,
                      maxSfbPerGroup, (const INT *)isBook,
                      (const INT *)isScale);
  memcpy(sfbPeOut, pc.sfbPe, MAX_GROUPED_SFB * sizeof(int32_t));
  memcpy(sfbConstPartOut, pc.sfbConstPart, MAX_GROUPED_SFB * sizeof(int32_t));
  memcpy(sfbNActiveLinesOut, pc.sfbNActiveLines,
         MAX_GROUPED_SFB * sizeof(int32_t));
  *peOut = pc.pe;
  *constPartOut = pc.constPart;
  *nActiveLinesOut = pc.nActiveLines;
}

// LD-domain helper oracles (genuine vendored kernels).
int32_t eparity_calc_inv_ld_data(int32_t x) {
  return (int32_t)CalcInvLdData((FIXP_DBL)x);
}
int32_t eparity_calc_ld_int(int32_t i) { return (int32_t)CalcLdInt((INT)i); }
int32_t eparity_fmult_norm(int32_t f1, int32_t f2, int32_t *result_e) {
  INT e = 0;
  FIXP_DBL m = fMultNorm((FIXP_DBL)f1, (FIXP_DBL)f2, &e);
  *result_e = (int32_t)e;
  return (int32_t)m;
}
int32_t eparity_fmult_i(int32_t a, int32_t b) {
  return (int32_t)fMultI((FIXP_DBL)a, (INT)b);
}

} // extern "C"
