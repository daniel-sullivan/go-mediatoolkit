// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored fft() dispatcher (fft.cpp), which
 * dct_IV/dst_IV/dct_III/dst_III/dct_II call for the inner complex FFT. For the
 * AAC-LC lengths it routes to dit_fft (the sibling fft_rad2 TU) with the
 * SineTable512 ROM (the sibling FDK_tools_rom TU); the static mixed-radix
 * helpers it also defines are unused here. fft.cpp's only includes are
 * fft_rad2.h, FDK_tools_rom.h, fft.h — no other libfdk module. */
#include "libfdk/libFDK/src/fft.cpp"
