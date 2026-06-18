// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_tools_rom.cpp —
 * the invCount[80] ROM behind GetInvInt (used by intensity.cpp to scale per-band
 * sums by 1/N) plus the SineTable/trig ROM the math helpers reference. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
