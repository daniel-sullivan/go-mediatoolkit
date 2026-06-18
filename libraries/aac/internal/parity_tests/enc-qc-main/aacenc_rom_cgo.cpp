// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/aacEnc_rom.cpp so the packed Huffman
// length tables (FDKaacEnc_huff_ltab1_2 .. FDKaacEnc_huff_ltab11), the
// FDKaacEnc_huff_ltabscf scalefactor table (used by the inline
// FDKaacEnc_bitCountScalefactorDelta) and the FDKaacEnc_sideInfoTabLong/Short
// section side-info tables resolve at link time.
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
