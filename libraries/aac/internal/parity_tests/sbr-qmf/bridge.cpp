// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the Fraunhofer FDK-AAC QMF filterbank (libFDK/src/qmf.cpp +
 * qmf_pcm.h: the complex exponential-modulated polyphase analysis/synthesis the
 * SBR decoder runs at no_channels==64, STD/HQ mode). This translation unit
 * provides the extern "C" entry points the Go test calls; it links the GENUINE
 * vendored qmf.cpp / dct.cpp / fft.cpp / fft_rad2.cpp / FDK_tools_rom.cpp /
 * scale.cpp / genericStds.cpp / FDK_trigFcts.cpp sibling TUs, so the oracle is
 * the real reference, not a hand-twin.
 *
 * It NEVER imports libraries/aac, so there is no cross-package static-symbol
 * clash (the amalgamation-split reasoning the sibling oracles document). It MAY
 * import internal/nativeaac on the Go side (cgo.go does).
 *
 * Integer parity: the whole QMF is fixed-point — FIXP_DBL == int32 (Q-format
 * data), FIXP_SGL == int16 (Q1.15 ROM). The polyphase FIR, the DCT-IV/DST-IV
 * modulation (int64-product>>32 fixmul/cplxMul kernels), and the saturating
 * shifts are bit-identical regardless of -ffp-contract / vectorization, with no
 * transcendental — so the oracle asserts EXACT int32 equality.
 *
 * The SBR decoder drives the 32-bit instantiations: analysis input is LONG*
 * (FIXP_QAS == FIXP_DBL, sbr_dec.cpp:282/344) and synthesis output is LONG*
 * (INT_PCM_QMFOUT == LONG, qmf.cpp:826-832). The overloaded C++ symbol resolves
 * to those by the LONG* argument types below.
 */

#include <stdint.h>
#include <string.h>

#include "qmf.h"
#include "fft.h"
#include "FDK_tools_rom.h"

extern "C" {

/* qparity_fft runs the genuine fft() dispatcher in place over x[0:2*length]
 * (interleaved complex) and returns the scalefactor it accumulates. For
 * length==32 this exercises the hard-coded fft_32 the QMF L==64 DCT routes
 * through; length 16/64/etc. are also reachable for cross-checking. */
int qparity_fft(int length, int32_t *x) {
  int sc = 0;
  fft(length, (FIXP_DBL *)x, &sc);
  return sc;
}

/* qparity_qmf_analysis runs a full HQ STD analysis: it inits a 64-band analysis
 * bank over freshly-cleared FIXP_DBL filter states, then runs qmfAnalysisFiltering
 * over noCol slots of the int32 (LONG / FIXP_QAS) time input. The complex subband
 * output is written interleaved per slot into qmfReal/qmfImag (noCol*64 each).
 * Returns scaleFactor.lb_scale via *pLbScale. */
void qparity_qmf_analysis(const int32_t *timeIn, int noCol, int lsb, int usb,
                          int timeInE, int stride, int32_t *qmfRealFlat,
                          int32_t *qmfImagFlat, int *pLbScale) {
  QMF_FILTER_BANK fb;
  /* Analysis states are allocated 10*no_channels == 10*64 (FDK_qmf_domain.cpp:127,
   * C_AALLOC_MEM2(AnaQmfStates, FIXP_DBL, 10*QMF_DOMAIN_MAX_ANALYSIS_QMF_BANDS)):
   * the slot feed writes into offset=(2*QMF_NO_POLY-1)*64 .. +64 and the FIR reads
   * sta_1 from 2*QMF_NO_POLY*64-1 == 639, so 9*64 would overrun by 64. */
  FIXP_DBL states[10 * 64];
  memset(states, 0, sizeof(states));
  memset(&fb, 0, sizeof(fb));

  qmfInitAnalysisFilterBank(&fb, (FIXP_DBL *)states, noCol, lsb, usb, 64, 0);

  FIXP_DBL *qmfReal[64];
  FIXP_DBL *qmfImag[64];
  for (int i = 0; i < noCol; i++) {
    qmfReal[i] = (FIXP_DBL *)qmfRealFlat + i * 64;
    qmfImag[i] = (FIXP_DBL *)qmfImagFlat + i * 64;
  }

  QMF_SCALE_FACTOR sf;
  memset(&sf, 0, sizeof(sf));

  FIXP_DBL workBuffer[2 * 64];

  qmfAnalysisFiltering(&fb, qmfReal, qmfImag, &sf, (const LONG *)timeIn, timeInE,
                       stride, workBuffer);

  *pLbScale = sf.lb_scale;
}

/* qparity_qmf_analysis32 runs a full HQ STD 32-band analysis (the dual-rate SBR
 * analysis filter bank): no_channels==32, non-downsampled. Output is noCol*32
 * complex subband values per slot. */
void qparity_qmf_analysis32(const int32_t *timeIn, int noCol, int lsb, int usb,
                            int timeInE, int stride, int32_t *qmfRealFlat,
                            int32_t *qmfImagFlat, int *pLbScale) {
  QMF_FILTER_BANK fb;
  FIXP_DBL states[10 * 32];
  memset(states, 0, sizeof(states));
  memset(&fb, 0, sizeof(fb));

  qmfInitAnalysisFilterBank(&fb, (FIXP_DBL *)states, noCol, lsb, usb, 32, 0);

  FIXP_DBL *qmfReal[64];
  FIXP_DBL *qmfImag[64];
  for (int i = 0; i < noCol; i++) {
    qmfReal[i] = (FIXP_DBL *)qmfRealFlat + i * 32;
    qmfImag[i] = (FIXP_DBL *)qmfImagFlat + i * 32;
  }

  QMF_SCALE_FACTOR sf;
  memset(&sf, 0, sizeof(sf));
  FIXP_DBL workBuffer[2 * 64];
  qmfAnalysisFiltering(&fb, qmfReal, qmfImag, &sf, (const LONG *)timeIn, timeInE,
                       stride, workBuffer);
  *pLbScale = sf.lb_scale;
}

/* qparity_qmf_synthesis runs a full HQ STD synthesis: it inits a 64-band
 * synthesis bank over freshly-cleared FIXP_QSS filter states, applies the
 * requested output scalefactor, then runs qmfSynthesisFiltering over noCol slots
 * of the complex subband input (qmfReal/qmfImag, noCol*64 each), writing
 * noCol*64 int32 (LONG) time samples to timeOut at the given stride. lbScale /
 * hbScale / ovLbScale / ovHbScale seed the QMF_SCALE_FACTOR. */
void qparity_qmf_synthesis(const int32_t *qmfRealFlat, const int32_t *qmfImagFlat,
                           int noCol, int lsb, int usb, int outScalefactor,
                           int lbScale, int hbScale, int ovLbScale, int ovHbScale,
                           int ovLen, int stride, int32_t *timeOut) {
  QMF_FILTER_BANK fb;
  FIXP_DBL states[9 * 64];
  memset(states, 0, sizeof(states));
  memset(&fb, 0, sizeof(fb));

  qmfInitSynthesisFilterBank(&fb, (FIXP_DBL *)states, noCol, lsb, usb, 64, 0);
  qmfChangeOutScalefactor(&fb, outScalefactor);

  FIXP_DBL *qmfReal[64];
  FIXP_DBL *qmfImag[64];
  for (int i = 0; i < noCol; i++) {
    qmfReal[i] = (FIXP_DBL *)qmfRealFlat + i * 64;
    qmfImag[i] = (FIXP_DBL *)qmfImagFlat + i * 64;
  }

  QMF_SCALE_FACTOR sf;
  memset(&sf, 0, sizeof(sf));
  sf.lb_scale = lbScale;
  sf.hb_scale = hbScale;
  sf.ov_lb_scale = ovLbScale;
  sf.ov_hb_scale = ovHbScale;

  FIXP_DBL workBuffer[2 * 64];

  qmfSynthesisFiltering(&fb, qmfReal, qmfImag, &sf, ovLen, (LONG *)timeOut,
                        stride, workBuffer);
}

/* qparity_pfilt640 copies the genuine in-RAM qmf_pfilt640 (FIXP_PFT == narrowed
 * FIXP_SGL) so the Go port's QFC-narrowed ROM can be verified entry-for-entry. */
void qparity_pfilt640(int16_t *out, int count) {
  for (int i = 0; i < count; i++) out[i] = (int16_t)qmf_pfilt640[i];
}

/* qparity_phaseshift64 copies the genuine qmf_phaseshift_cos64 / _sin64
 * (FIXP_QTW == FIXP_SGL) for byte-for-byte verification. */
void qparity_phaseshift64(int16_t *cosOut, int16_t *sinOut, int count) {
  for (int i = 0; i < count; i++) {
    cosOut[i] = (int16_t)qmf_phaseshift_cos64[i];
    sinOut[i] = (int16_t)qmf_phaseshift_sin64[i];
  }
}

/* qparity_sinewindow64 copies the genuine SineWindow64 (FIXP_WTP == FIXP_SPK
 * packed 16-bit) as flat [re0,im0,...] so the Go port's WTCP-narrowed slope ROM
 * can be verified entry-for-entry. */
void qparity_sinewindow64(int16_t *out, int pairCount) {
  for (int i = 0; i < pairCount; i++) {
    out[2 * i + 0] = (int16_t)SineWindow64[i].v.re;
    out[2 * i + 1] = (int16_t)SineWindow64[i].v.im;
  }
}

} /* extern "C" */
