// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp
 * — supplies fDivNorm / f2Pow / fPow / fLog2 / schur_div and the ldDataTable
 * lookups behind the inline CalcLdData / fLog2 that psy_configuration.cpp's
 * initMinSnr / getMaskFactor use. Compiling it as its own TU resolves these
 * everywhere without duplicate symbols; its only include is fixpoint_math.h, so
 * it drags in no other libfdk module. */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
