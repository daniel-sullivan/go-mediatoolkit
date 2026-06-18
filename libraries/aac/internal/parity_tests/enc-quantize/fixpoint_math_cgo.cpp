// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/fixpoint_math.cpp
 * — calcSfbDist / calcSfbQuantEnergyAndDist call CalcLdData (the LD-domain log2
 * used to convert the block-floating-point distortion/energy sums to their
 * ldData form). CalcLdData is inline in fixpoint_math.h, but linking the genuine
 * TU also satisfies the extern LdDataVector / InitLdInt symbols it references and
 * keeps the oracle real_vendored. */
#include "libfdk/libFDK/src/fixpoint_math.cpp"
