// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU compiling the genuine vendored libFDK/src/FDK_bitbuffer.cpp so that
// FDKaacEnc_codeValues (linked in from bit_cnt.cpp, which this slice does not
// call but which shares the bit_cnt.cpp TU with FDKaacEnc_countValues /
// FDKaacEnc_bitCount that it does call) resolves its FDK_put bit-writer
// reference. Genuine vendored source -> the oracle stays real_vendored.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
