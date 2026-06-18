// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity bridge for the parametric-stereo parameter EXTRACTION + quantization +
 * DPCM rate-decision (libSBRenc/src/ps_encode.cpp, FDKsbrEnc_PSEncode). Creates
 * + inits a PS_ENCODE, feeds the left/right hybrid sub-band data of a frame, and
 * returns the resulting PS_OUT fields (modes, enables, per-env quantized IID/ICC
 * indices, DPCM directions, frame borders) for exact-integer comparison.
 *
 * GetRam_PsEncode / FreeRam_PsEncode (normally in sbrenc_ram.cpp, which drags in
 * the whole SBR encoder RAM map) are stubbed with malloc/free here so the TU set
 * stays minimal — the allocator only hands ps_encode.cpp a zeroed PS_ENCODE. */

#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#include "sbr_def.h"
#include "qmf.h"
#include "ps_main.h"
#include "ps_encode.h"
#include "ps_const.h"

/* malloc-backed RAM stub for the PS_ENCODE handle (sbrenc_ram.cpp surrogate).
 * sbrenc_ram.h declares these with C++ linkage, so the stubs match (no
 * extern "C"). */
HANDLE_PS_ENCODE GetRam_PsEncode(int) {
  HANDLE_PS_ENCODE h = (HANDLE_PS_ENCODE)malloc(sizeof(PS_ENCODE));
  if (h) memset(h, 0, sizeof(PS_ENCODE));
  return h;
}
void FreeRam_PsEncode(HANDLE_PS_ENCODE *ph) {
  if (ph && *ph) {
    free(*ph);
    *ph = NULL;
  }
}

extern "C" {

struct PSEXTRACT_HANDLE {
  HANDLE_PS_ENCODE h;
};

/* Create + init a PS_ENCODE for psEncMode (10 or 20). Returns an opaque handle. */
void *psextract_new(int psEncMode, int iidQuantErrorThreshold) {
  PSEXTRACT_HANDLE *wrap =
      (PSEXTRACT_HANDLE *)malloc(sizeof(PSEXTRACT_HANDLE));
  wrap->h = NULL;
  FDKsbrEnc_CreatePSEncode(&wrap->h);
  FDKsbrEnc_InitPSEncode(wrap->h, (PS_BANDS)psEncMode,
                         (FIXP_DBL)iidQuantErrorThreshold);
  return wrap;
}

void psextract_free(void *handle) {
  PSEXTRACT_HANDLE *wrap = (PSEXTRACT_HANDLE *)handle;
  if (wrap) {
    FDKsbrEnc_DestroyPSEncode(&wrap->h);
    free(wrap);
  }
}

/* Run one frame of FDKsbrEnc_PSEncode.
 *
 * hybridFlat : [HYBRID_FRAMESIZE][2 ch][2 reim][71] row-major int32
 * dynBandScale[PS_MAX_BANDS] : per-band scale (psFindBestScaling output)
 * maxEnvelopes, frameSize, sendHeader : config
 *
 * Returns the PS_OUT fields via the out-pointers; per-env arrays are flat
 * [PS_MAX_ENVELOPES][PS_MAX_BANDS] row-major.
 */
void psextract_run(void *handle, const int *hybridFlat,
                   const unsigned char *dynBandScale, int maxEnvelopes,
                   int frameSize, int sendHeader, int *enablePSHeader,
                   int *enableIID, int *iidMode, int *enableICC, int *iccMode,
                   int *frameClass, int *nEnvelopes, int *frameBorder,
                   int *deltaIID, int *deltaICC, int *iidFlat, int *iccFlat,
                   int *iidLast, int *iccLast) {
  PSEXTRACT_HANDLE *wrap = (PSEXTRACT_HANDLE *)handle;

  const int NB = 71;
  /* Build the FIXP_DBL *hybridData[HYBRID_FRAMESIZE][MAX_PS_CHANNELS][2] view
   * over a contiguous backing store, exactly as ps_main.cpp passes it. */
  static FIXP_DBL store[HYBRID_FRAMESIZE][MAX_PS_CHANNELS][2][71];
  static FIXP_DBL *hybridData[HYBRID_FRAMESIZE][MAX_PS_CHANNELS][2];
  for (int col = 0; col < HYBRID_FRAMESIZE; col++) {
    for (int ch = 0; ch < MAX_PS_CHANNELS; ch++) {
      for (int ri = 0; ri < 2; ri++) {
        int base = ((col * MAX_PS_CHANNELS + ch) * 2 + ri) * NB;
        for (int b = 0; b < NB; b++)
          store[col][ch][ri][b] = (FIXP_DBL)hybridFlat[base + b];
        hybridData[col][ch][ri] = store[col][ch][ri];
      }
    }
  }

  UCHAR dyn[PS_MAX_BANDS];
  for (int b = 0; b < PS_MAX_BANDS; b++) dyn[b] = dynBandScale[b];

  PS_OUT psOut;
  memset(&psOut, 0, sizeof(psOut));

  FDKsbrEnc_PSEncode(wrap->h, &psOut, dyn, (UINT)maxEnvelopes, hybridData,
                     frameSize, sendHeader);

  *enablePSHeader = psOut.enablePSHeader;
  *enableIID = psOut.enableIID;
  *iidMode = psOut.iidMode;
  *enableICC = psOut.enableICC;
  *iccMode = psOut.iccMode;
  *frameClass = psOut.frameClass;
  *nEnvelopes = psOut.nEnvelopes;
  for (int e = 0; e < PS_MAX_ENVELOPES; e++) {
    frameBorder[e] = psOut.frameBorder[e];
    deltaIID[e] = psOut.deltaIID[e];
    deltaICC[e] = psOut.deltaICC[e];
    for (int b = 0; b < PS_MAX_BANDS; b++) {
      iidFlat[e * PS_MAX_BANDS + b] = psOut.iid[e][b];
      iccFlat[e * PS_MAX_BANDS + b] = psOut.icc[e][b];
    }
  }
  for (int b = 0; b < PS_MAX_BANDS; b++) {
    iidLast[b] = psOut.iidLast[b];
    iccLast[b] = psOut.iccLast[b];
  }
}

} /* extern "C" */
