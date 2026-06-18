// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libFDK/src/fixpoint_math.cpp into this parity
// test binary so bandwidth.cpp's low-delay interpolation branch links the real
// fDivNorm / f2Pow / the fLog2 + InvLdData ROM tables. This package owns its own
// copy of the needed C TUs and never imports libraries/aac.
#include "libfdk/libFDK/src/fixpoint_math.cpp"
