// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_tools_rom.cpp —
 * the SineTable/trig ROM FDK_lpc.cpp / aacenc_tns.cpp may reference. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
