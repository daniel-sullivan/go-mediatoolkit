// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine-vendored oracle bridge for the AAC encoder TNS Gauss window
// (aacenc_tns.cpp). The vendored TU is #included directly here so the static
// FDKaacEnc_CalcGaussWindow is visible to the extern "C" shim that wraps it. The
// shim calls the REAL FDK function and copies out win[] — no re-derivation
// (oracle_kind == real_vendored).

// Compile the genuine vendored aacenc_tns.cpp into this TU so the static
// FDKaacEnc_CalcGaussWindow can be called by the shim below. aacenc_tns.cpp
// pulls in channel_map.h / aacenc_tns.h transitively.
#include "aacenc_tns.cpp"

#include <stdint.h>

extern "C" {

// gparity_calc_gauss_window runs the genuine static FDKaacEnc_CalcGaussWindow.
void gparity_calc_gauss_window(int32_t *win, int winSize, int samplingRate,
                               int transformResolution, int32_t timeResolution,
                               int timeResolutionE) {
  FDKaacEnc_CalcGaussWindow((FIXP_DBL *)win, winSize, samplingRate,
                            transformResolution, (FIXP_DBL)timeResolution,
                            timeResolutionE);
}

} // extern "C"
