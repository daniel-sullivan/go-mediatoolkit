// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/fixpoint_math.cpp (CalcLdInt + ld/log tables used by the
// freq-scale builder and ResetLimiterBands' fLog2).
#include "libfdk/libFDK/src/fixpoint_math.cpp"
