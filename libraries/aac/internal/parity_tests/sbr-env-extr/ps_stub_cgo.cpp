// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// env_extr.cpp's extractExtendedData references ReadPsData (psbitdec.cpp) in the
// Parametric Stereo (EXTENSION_ID_PS_CODING) branch — but only when a PS decoder
// handle is attached (hParametricStereoDec != NULL). The oracle always passes
// NULL (PS == HE-AAC v2, out of this HE-AAC v1 batch's scope), so the symbol is
// never CALLED, only referenced for linking. A link-only stub avoids pulling in
// the whole PS subsystem (psbitdec/psdec/pvc_dec). It mirrors the sbr-dec-env
// oracle's sbrdec_mapToStdSampleRate stub reasoning. Never executed.

#include "psbitdec.h" /* for the exact ReadPsData prototype */

unsigned int ReadPsData(struct PS_DEC *h_ps_d, HANDLE_FDK_BITSTREAM hBs,
                        INT nBitsLeft) {
  (void)h_ps_d;
  (void)hBs;
  (void)nBitsLeft;
  return 0;
}
