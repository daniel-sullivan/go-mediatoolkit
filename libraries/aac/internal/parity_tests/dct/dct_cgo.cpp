// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored fixed-point DCT/DST primitives
 * (dct_II/dct_III/dct_IV/dst_III/dst_IV + dct_getTables, the non-static symbols
 * in dct.cpp). It pulls only FDK_tools_rom.h (for the FIXP_WTP/FIXP_STP twiddle
 * ROM, defined in the sibling FDK_tools_rom TU) and fft.h (the fft() dispatcher,
 * defined in the sibling fft TU) plus header-inline cplx_mul/fixmul/clz helpers
 * — no other libfdk module. */
#include "libfdk/libFDK/src/dct.cpp"
