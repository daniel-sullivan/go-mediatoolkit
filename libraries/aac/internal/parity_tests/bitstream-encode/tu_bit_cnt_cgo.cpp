// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored Fraunhofer FDK-AAC encode-time
// Huffman bitcounter/coder (libAACenc/src/bit_cnt.cpp) as its own translation
// unit for the bitstream-encode parity oracle. FDKaacEnc_codeValues and
// FDKaacEnc_codeScalefactorDelta resolve here. See libfdk/COPYING.
#include "libfdk/libAACenc/src/bit_cnt.cpp"
