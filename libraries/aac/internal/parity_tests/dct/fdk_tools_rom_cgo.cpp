// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the FDK trig/transform constant ROM the DCT/DST
 * primitives + dit_fft are parameterised with: the SineTableXXX twiddle ROM,
 * the SineWindow/KBDWindow windowSlopes ROM, and SineTable512. FDK_tools_rom.cpp's
 * only include is FDK_tools_rom.h, so it drags in no other libfdk module — pure
 * constant data. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
