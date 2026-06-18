// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored fixed-point inverse-MDCT
 * (imlt_block == FrequencyToTime, imdct_gain, imdct_adapt_parameters, mdct_init,
 * imdct_drain, imdct_copy_ov_and_nr — the non-static symbols in mdct.cpp). Its
 * only includes are mdct.h, FDK_tools_rom.h (the window-slope ROM, defined in
 * the sibling FDK_tools_rom TU), dct.h (dct_IV/dct_III/dst_III/dst_IV, defined
 * in the sibling dct TU) and fixpoint_math.h (header-inline) — no other libfdk
 * module beyond header-inline cplx_mul/fixmul/scale/clz helpers. */
#include "libfdk/libFDK/src/mdct.cpp"
