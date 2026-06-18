// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp
 * — supplies schur_div (out-of-line on non-x86 targets, called by
 * chaosmeasure.cpp) and the ldDataTable / invSqrtTab lookups behind the inline
 * CalcLdData / fLog2 that tonality.cpp uses. Compiling it as its own TU
 * resolves these everywhere without duplicate symbols; its only include is
 * fixpoint_math.h, so it drags in no other libfdk module. */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
