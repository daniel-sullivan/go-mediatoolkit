// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// env_extr.cpp's extractExtendedData references ReadPsData (PS, HE-AAC v2, out of
// HE-AAC v1 scope). Never CALLED here (no PS handle); a link-only stub avoids
// pulling in the whole PS subsystem. Never executed.
#include "psbitdec.h"

unsigned int ReadPsData(struct PS_DEC *h_ps_d, HANDLE_FDK_BITSTREAM hBs, INT nBitsLeft) {
  (void)h_ps_d; (void)hBs; (void)nBitsLeft; return 0;
}
