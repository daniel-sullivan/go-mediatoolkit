// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacenc_tns.cpp —
 * needed only for FDKaacEnc_FreqToBandWidthRounding (aacenc_tns.cpp:339), which
 * FDKaacEnc_GetPnsParam calls to map the tuning-table start frequency to an sfb.
 * It pulls in FDK_lpc.cpp + aacEnc_rom.cpp as sibling TUs. */
#include "libfdk/libAACenc/src/aacenc_tns.cpp"
