// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_tools_rom.cpp
 * — supplies the SineTable / trig ROM that FDK_trigFcts.cpp's fixp_cos /
 * fixp_cos_sin (compiled but unused here) reference. Compiled as its own TU so
 * the FDK_trigFcts TU links cleanly without duplicate symbols. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
