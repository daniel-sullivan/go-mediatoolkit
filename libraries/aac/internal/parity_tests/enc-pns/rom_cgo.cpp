// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp —
 * the SFB ROM + TNS tables aacenc_tns.cpp references. */
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
