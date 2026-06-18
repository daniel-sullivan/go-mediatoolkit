// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the vendored ton_corr.cpp locally, then expose a tap to its
// file-static resetPatch so the parity bridge can drive it in isolation
// (without the mh-detector RAM that the public Reset entry would need).
#include "libfdk/libSBRenc/src/ton_corr.cpp"

extern "C" int toncorr_reset_patch_tap(SBR_TON_CORR_EST *h, int xposctrl,
                                       int highBandStartSb, unsigned char *vk,
                                       int numMaster, int fs, int noChannels) {
  return resetPatch(h, xposctrl, highBandStartSb, vk, numMaster, fs, noChannels);
}
