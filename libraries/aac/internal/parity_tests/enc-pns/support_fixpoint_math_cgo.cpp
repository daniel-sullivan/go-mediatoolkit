// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp —
 * fPow / fLog2 / CalcLdData / CalcInvLdData / ldDataTable behind the inline
 * helpers pnsparam.cpp and aacenc_pns.cpp use. */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
