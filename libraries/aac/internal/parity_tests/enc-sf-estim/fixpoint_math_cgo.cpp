// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libFDK/src/fixpoint_math.cpp (CalcLdData /
// CalcInvLdData + the fLog2 / InvLdData ROM tables) the sf_estim / quantize
// kernels link.
#include "libfdk/libFDK/src/fixpoint_math.cpp"
