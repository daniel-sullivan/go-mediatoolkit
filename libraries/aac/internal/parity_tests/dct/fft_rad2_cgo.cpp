// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored DIT FFT (dit_fft, the single
 * non-static symbol in fft_rad2.cpp) that the fft() dispatcher calls for the
 * AAC-LC lengths. It pulls only header-inline helpers (scramble.h, cplx_mul.h,
 * fixmul.h, scale.h) plus the SineTable512 symbol from the FDK_tools_rom TU —
 * no other libfdk module. */
#include "libfdk/libFDK/src/fft_rad2.cpp"
