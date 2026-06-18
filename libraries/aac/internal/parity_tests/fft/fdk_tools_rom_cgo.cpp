// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the twiddle-factor ROM (SineTable512 and the rest of the
 * FDK trig/transform constant tables) dit_fft is parameterised with.
 * FDK_tools_rom.cpp's only include is FDK_tools_rom.h, so it drags in no other
 * libfdk module — pure constant data. */
#include "libfdk/libFDK/src/FDK_tools_rom.cpp"
