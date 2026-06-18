// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp
 * — band_nrg.cpp calls the extern LdDataVector (fixpoint_math.cpp:117) and the
 * inline fLog2 / CalcLdData (fixpoint_math.h, the LD-domain log2 used to convert
 * SFB energies to their ldData form). Linking the genuine TU keeps the oracle
 * real_vendored (the Go side's fixpoint_log2.go is verified against this). */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
