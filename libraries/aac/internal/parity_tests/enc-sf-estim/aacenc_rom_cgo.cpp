// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/aacEnc_rom.cpp so the quantizer
// mantissa tables and the FDKaacEnc_huff_ltabscf table (referenced by the inline
// FDKaacEnc_bitCountScalefactorDelta) resolve at link time.
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
