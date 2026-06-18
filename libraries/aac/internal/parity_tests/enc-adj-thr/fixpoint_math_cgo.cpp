// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libFDK/src/fixpoint_math.cpp (CalcLdInt /
// fMultNorm / schur_div / f2Pow / the fLog2 + InvLdData ROM tables) into this
// parity test binary; line_pe.cpp's PE math and the LD-domain helper oracles
// link these real kernels.
#include "libfdk/libFDK/src/fixpoint_math.cpp"
