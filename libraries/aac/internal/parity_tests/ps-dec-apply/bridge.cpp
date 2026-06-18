// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the HE-AAC v2 PS apply / full mono->stereo synthesis
 * ("ps-dec-apply"): CreatePsDec -> ReadPsData -> DecodePs -> per-slot
 * (initSlotBasedRotation + ApplyPsSlot), exactly mirroring the sbr_dec.cpp PS
 * loop. It exercises the GENUINE hybrid filterbank, the decorrelator (allpass
 * cascade + PS ducker), and the H-matrix rotation. The Go port (PsApplyRun)
 * drives the IDENTICAL sequence over the same QMF input + ps_data payload and
 * must produce EXACTLY the same int32 left+right QMF output.
 *
 * GetRam_ps_dec / FreeRam_ps_dec are stubbed here (malloc/free) so the oracle
 * does not pull sbr_ram.cpp's full static-allocation footprint.
 *
 * Integer parity: the whole PS synthesis is fixed-point (FIXP_DBL data,
 * FIXP_SGL/FIXP_STP coefficients, the inline_fixp_cos_sin/SineTable512 rotation,
 * the sqrt/invSqrt ducker math) — bit-identical regardless of -ffp-contract /
 * vectorization. EXACT equality asserted.
 */

#include <stdint.h>
#include <string.h>
#include <stdlib.h>

#include "psdec.h"
#include "psbitdec.h"
#include "FDK_bitstream.h"
#include "FDK_trigFcts.h"
#include "FDK_decorrelate.h"

/* GetRam_ps_dec / FreeRam_ps_dec stubs (replace sbr_ram.cpp). The H_ALLOC_MEM
 * declaration in sbr_ram.h gives Get/Free C++ linkage with a default-0 int arg;
 * match it (NOT extern "C", or psdec.cpp's C++-mangled references won't resolve). */
struct PS_DEC *GetRam_ps_dec(int n) {
  (void)n;
  return (struct PS_DEC *)calloc(1, sizeof(struct PS_DEC));
}
void FreeRam_ps_dec(struct PS_DEC **p) {
  if (p != NULL) {
    free(*p);
    *p = NULL;
  }
}

#define BANDS (64)

/* expose the static rotation for the isolation probe */
extern void initSlotBasedRotation(HANDLE_PS_DEC h_ps_d, int env, int usb);

extern "C" {

/* qparity_initRot runs CreatePsDec + ReadPsData + DecodePs + initSlotBasedRotation
 * for env 0, returning the resulting H11r/H12r/H21r/H22r and DeltaHxx arrays
 * (NO_IID_GROUPS == 22 each) so the Go initSlotBasedRotation can be verified in
 * isolation. */
int qparity_initRot(const uint8_t *payload, int payloadBytes, int validBits,
                    int usb, int32_t *H11, int32_t *H12, int32_t *H21, int32_t *H22,
                    int32_t *D11, int32_t *D12, int32_t *D21, int32_t *D22) {
  HANDLE_PS_DEC h = NULL;
  if (CreatePsDec(&h, 1024)) return -1;
  h->bsLastSlot = 0; h->bsReadSlot = 0; h->processSlot = 0; h->procFrameBased = 0;
  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)payload, payloadBytes, validBits, BS_READER);
  ReadPsData(h, &bs, validBits);
  PS_DEC_COEFFICIENTS *coef = (PS_DEC_COEFFICIENTS *)calloc(1, sizeof(PS_DEC_COEFFICIENTS));
  int flag = DecodePs(h, 0, coef);
  if (flag == 1) {
    initSlotBasedRotation(h, 0, usb);
    for (int g = 0; g < NO_IID_GROUPS; g++) {
      H11[g] = coef->H11r[g]; H12[g] = coef->H12r[g];
      H21[g] = coef->H21r[g]; H22[g] = coef->H22r[g];
      D11[g] = coef->DeltaH11r[g]; D12[g] = coef->DeltaH12r[g];
      D21[g] = coef->DeltaH21r[g]; D22[g] = coef->DeltaH22r[g];
    }
  }
  free(coef);
  DeletePsDec(&h);
  return flag;
}

/* qparity_cosSin probes inline_fixp_cos_sin(x1,x2,scale,out) directly so the Go
 * InlineFixpCosSin can be verified in isolation. */
void qparity_cosSin(int32_t x1, int32_t x2, int scale, int32_t *out) {
  FIXP_DBL trig[4];
  inline_fixp_cos_sin((FIXP_DBL)x1, (FIXP_DBL)x2, scale, trig);
  for (int i = 0; i < 4; i++) out[i] = (int32_t)trig[i];
}

/* qparity_decorr isolates FDKdecorrelateOpen/Init(DECORR_PS) -> per-slot
 * FDKdecorrelateApply. inRe/inImg are nSlots rows of 71 hybrid bands; the apply
 * runs in-place semantics: left (inRe/inImg) is the direct signal, the right
 * (outRe/outImg) is the decorrelated output. Returns left+right after nSlots. */
void qparity_decorr(int nSlots, const int32_t *inRe, const int32_t *inImg,
                    int32_t *leftRe, int32_t *leftImg, int32_t *rightRe,
                    int32_t *rightImg) {
  DECORR_DEC dec;
  memset(&dec, 0, sizeof(dec));
  FIXP_DBL buf[2 * ((825) + (373))];
  FDKdecorrelateOpen(&dec, buf, 2 * ((825) + (373)));
  FDKdecorrelateInit(&dec, 71, DECORR_PS, DUCKER_AUTOMATIC, 0, 0, 0, 0, 1, 1);

  for (int s = 0; s < nSlots; s++) {
    FIXP_DBL lRe[71], lImg[71], rRe[71], rImg[71];
    for (int b = 0; b < 71; b++) {
      lRe[b] = inRe[s * 71 + b];
      lImg[b] = inImg[s * 71 + b];
    }
    FDKdecorrelateApply(&dec, lRe, lImg, rRe, rImg, 0);
    for (int b = 0; b < 71; b++) {
      leftRe[s * 71 + b] = lRe[b];
      leftImg[s * 71 + b] = lImg[b];
      rightRe[s * 71 + b] = rRe[b];
      rightImg[s * 71 + b] = rImg[b];
    }
  }
}

/* qparity_psApply drives the full PS synthesis. lowBandReal/Imag are
 * (noCol+HYBRID_FILTER_DELAY) rows of 64 QMF bands (flattened) for the left/mono
 * input; outLeft* receive the in-place-modified left rows (noCol*64) and
 * outRight* the synthesised right rows. Returns the DecodePs process flag. */
int qparity_psApply(int aacSamplesPerFrame, const uint8_t *payload, int payloadBytes,
                    int validBits, int noCol, int lsb, int usb,
                    int scaleFactorLowBandNoOv, int scaleFactorLowBand,
                    int scaleFactorHighBand, int highSubband,
                    int32_t *lowBandReal, int32_t *lowBandImag,
                    int32_t *outLeftRe, int32_t *outLeftImg,
                    int32_t *outRightRe, int32_t *outRightImg) {
  HANDLE_PS_DEC h = NULL;
  if (CreatePsDec(&h, aacSamplesPerFrame)) {
    return -1;
  }
  h->bsLastSlot = 0;
  h->bsReadSlot = 0;
  h->processSlot = 0;
  h->procFrameBased = 0; /* slot-based directly (no frame->slot warm-up) */

  FDK_BITSTREAM bs;
  FDKinitBitStream(&bs, (UCHAR *)payload, payloadBytes, validBits, BS_READER);
  ReadPsData(h, &bs, validBits);

  PS_DEC_COEFFICIENTS *coef =
      (PS_DEC_COEFFICIENTS *)calloc(1, sizeof(PS_DEC_COEFFICIENTS));
  int psProcess = DecodePs(h, 0, coef);
  h->psDecodedPrv = (UCHAR)psProcess;

  int totalRows = noCol + HYBRID_FILTER_DELAY;

  /* [row][band] pointer view of the left QMF buffer. */
  FIXP_DBL **left = (FIXP_DBL **)malloc(totalRows * sizeof(FIXP_DBL *));
  FIXP_DBL **leftImg = (FIXP_DBL **)malloc(totalRows * sizeof(FIXP_DBL *));
  for (int r = 0; r < totalRows; r++) {
    left[r] = &lowBandReal[r * BANDS];
    leftImg[r] = &lowBandImag[r * BANDS];
  }

  int env = 0;
  for (int i = 0; i < noCol; i++) {
    if (psProcess) {
      if (i == h->bsData[h->processSlot].mpeg.aEnvStartStop[env]) {
        initSlotBasedRotation(h, env, highSubband);
        env++;
      }
      ApplyPsSlot(h, &left[i], &leftImg[i], &outRightRe[i * BANDS],
                  &outRightImg[i * BANDS], scaleFactorLowBandNoOv,
                  scaleFactorLowBand, scaleFactorHighBand, lsb, usb);
    }
  }

  for (int r = 0; r < noCol; r++) {
    memcpy(&outLeftRe[r * BANDS], left[r], BANDS * sizeof(int32_t));
    memcpy(&outLeftImg[r * BANDS], leftImg[r], BANDS * sizeof(int32_t));
  }

  free(left);
  free(leftImg);
  free(coef);
  DeletePsDec(&h);

  return psProcess;
}

} /* extern "C" */
