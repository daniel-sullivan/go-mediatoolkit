// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/bit_cnt.cpp: dyn_bits.cpp's
// FDKaacEnc_buildBitLookUp calls FDKaacEnc_bitCount from here, and crash recovery
// calls FDKaacEnc_countValues from here. The seven static per-codebook count
// functions and the countFuncTable dispatch all live in this TU, so linking it
// exercises the real bit estimator the Go port mirrors.
#include "libfdk/libAACenc/src/bit_cnt.cpp"
