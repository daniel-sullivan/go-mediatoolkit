// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/aacEnc_rom.cpp so the
// FDKaacEnc_huff_ltabscf table (referenced by the inline
// FDKaacEnc_bitCountScalefactorDelta in line_pe.cpp's intensity branch) resolves
// at link time.
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
