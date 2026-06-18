// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libSBRdec/src/sbr_rom.cpp as its own TU: the PS Huffman
// codebooks (aBookPsIid*/aBookPsIcc*) plus FDK_sbrDecoder_aFixNoEnvDecode /
// aNoIidBins / aNoIccBins the PS parse indexes. See cgo.go.
#include "libfdk/libSBRdec/src/sbr_rom.cpp"
