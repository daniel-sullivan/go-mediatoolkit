// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp
 * — the encoder Huffman code/length tables (FDKaacEnc_huff_*) the genuine
 * FDKaacEnc_codeValues / FDKaacEnc_codeScalefactorDelta read. Genuine vendored
 * source -> oracle stays real_vendored. */
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
