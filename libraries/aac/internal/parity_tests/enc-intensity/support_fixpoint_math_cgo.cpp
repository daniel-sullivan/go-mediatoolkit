// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp —
 * fDivNorm (the out-of-line normalised division intensity.cpp calls) plus the
 * fLog2/CalcLdData/ldDataTable helpers behind its inline math. */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
