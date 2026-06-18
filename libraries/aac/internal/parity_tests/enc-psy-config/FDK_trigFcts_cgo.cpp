// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_trigFcts.cpp
 * — supplies fixp_atan (FDK_trigFcts.cpp:238), which FDKaacEnc_BarcLineValue
 * calls. The TU also compiles fixp_cos/fixp_cos_sin (unused here) which
 * reference the SineTable from FDK_tools_rom.cpp, supplied as its own sibling
 * TU. */
#include "libfdk/libFDK/src/FDK_trigFcts.cpp"
