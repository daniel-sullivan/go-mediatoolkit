// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/bit_cnt.cpp —
 * supplies the GENUINE FDKaacEnc_codeValues / FDKaacEnc_codeScalefactorDelta
 * Huffman emitters that the spectral_data / scale_factor_data serializers in the
 * included bitenc.cpp call (and the bit-count helpers). Genuine vendored source
 * -> oracle stays real_vendored. See bridge.cpp for the amalgamation-split
 * rationale. */
#include "libfdk/libAACenc/src/bit_cnt.cpp"
