// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the AAC spectral Huffman ROM: AACcodeBookDescriptionTable
 * and the HuffmanCodeBook_* decode trees the oracle walks. aac_rom.cpp's only
 * include is aac_rom.h, so this drags in no other libfdk module. */
#include "libfdk/libAACdec/src/aac_rom.cpp"
