// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/fixpoint_math.cpp as its own TU: CalcLog2/f2Pow/fDivNorm/
// GetInvInt + the ld/exp/invCount tables the HF-gen + LPP paths use. See cgo.go.
#include "libfdk/libFDK/src/fixpoint_math.cpp"
