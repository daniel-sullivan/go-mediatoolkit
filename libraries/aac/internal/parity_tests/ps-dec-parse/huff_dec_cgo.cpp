// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/huff_dec.cpp as its own TU:
// DecodeHuffmanCW, the SBR Huffman code-word decoder env_extr.cpp calls. See
// cgo.go.
#include "libfdk/libSBRdec/src/huff_dec.cpp"
