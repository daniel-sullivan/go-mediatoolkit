// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored Fraunhofer FDK-AAC encoder ROM
// (libAACenc/src/aacEnc_rom.cpp) as its own translation unit for the
// bitstream-encode parity oracle. The FDKaacEnc_huff_ctab* / huff_ltab* /
// huff_ctabscf / huff_ltabscf tables the Huffman emitters index resolve here.
// See libfdk/COPYING.
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
