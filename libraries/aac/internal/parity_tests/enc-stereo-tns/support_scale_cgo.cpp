// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/scale.cpp to
 * satisfy the symbols the (uncalled-but-emitted) FDKaacEnc_TnsDetect /
 * FDKaacEnc_TnsEncode / FDKaacEnc_InitTnsConfiguration / FDKaacEnc_CalcGaussWindow
 * functions in the included aacEnc_tns.cpp reference (FDK_lpc: CLpc_*; fixpoint_math:
 * fPow/fDivNorm/fMultNorm/invSqrtNorm2; FDK_tools_rom: invSqrtTab; scale: scaling
 * helpers). The parity test only drives the static TNS quantizers + ms_stereo, but
 * the whole aacEnc_tns.cpp TU must link. Genuine vendored source -> oracle stays
 * real_vendored. See bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libFDK/src/scale.cpp"
