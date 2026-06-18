// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the HE-AAC v2 PS hybrid filterbank ("ps-dec-hybrid"):
 * FDKhybridAnalysisApply -> FDKhybridSynthesisApply (FDK_hybrid.cpp). It inits
 * the GENUINE analysis filterbank over the pHybridAnaStatesLFdmx-sized LF memory
 * (no HF memory, THREE_TO_TEN, NO_QMF_BANDS_HYBRID20 bands) and one synthesis
 * filterbank (THREE_TO_TEN, 64 bands), then for each input timeslot splits the 3
 * complex QMF inputs into 12 sub-subbands and recombines them, returning the 64
 * QMF outputs per slot. The Go port (PsHybridRun) drives the IDENTICAL sequence
 * and must produce EXACTLY the same int32 output.
 *
 * Integer parity: the hybrid filterbank is pure fixed-point (FIXP_DBL data,
 * FIXP_SGL/FIXP_SPK coefficients, the inline fft_8) — bit-identical regardless of
 * -ffp-contract / vectorization. EXACT equality asserted.
 */

#include <stdint.h>
#include <string.h>

#include "FDK_hybrid.h"

#define NO_QMF_BANDS_HYBRID20 (3)
#define NO_QMF_CHANNELS (64)
#define NO_HYBRID_DATA_BANDS (71)

extern "C" {

void qparity_psHybrid(int nSlots, const int32_t *qmfRe, const int32_t *qmfImg,
                      int32_t *outRe, int32_t *outImg) {
  FDK_ANA_HYB_FILTER ana;
  FDK_SYN_HYB_FILTER syn;
  FIXP_DBL lfMem[2 * 13 * NO_QMF_BANDS_HYBRID20];

  memset(&ana, 0, sizeof(ana));
  memset(&syn, 0, sizeof(syn));

  FDKhybridAnalysisOpen(&ana, lfMem, sizeof(lfMem), NULL, 0);
  FDKhybridAnalysisInit(&ana, THREE_TO_TEN, NO_QMF_BANDS_HYBRID20,
                        NO_QMF_BANDS_HYBRID20, 1);
  FDKhybridSynthesisInit(&syn, THREE_TO_TEN, NO_QMF_CHANNELS, NO_QMF_CHANNELS);

  for (int s = 0; s < nSlots; s++) {
    FIXP_DBL qInRe[NO_QMF_BANDS_HYBRID20];
    FIXP_DBL qInImg[NO_QMF_BANDS_HYBRID20];
    for (int i = 0; i < NO_QMF_BANDS_HYBRID20; i++) {
      qInRe[i] = qmfRe[s * NO_QMF_BANDS_HYBRID20 + i];
      qInImg[i] = qmfImg[s * NO_QMF_BANDS_HYBRID20 + i];
    }

    FIXP_DBL hybRe[NO_HYBRID_DATA_BANDS];
    FIXP_DBL hybImg[NO_HYBRID_DATA_BANDS];
    memset(hybRe, 0, sizeof(hybRe));
    memset(hybImg, 0, sizeof(hybImg));

    FDKhybridAnalysisApply(&ana, qInRe, qInImg, hybRe, hybImg);

    FDKhybridSynthesisApply(&syn, hybRe, hybImg, &outRe[s * NO_QMF_CHANNELS],
                            &outImg[s * NO_QMF_CHANNELS]);
  }
}

} /* extern "C" */
