// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/sbr_rom.cpp as its own translation
// unit for the sbr-dec-env parity oracle (defines FDK_sbrDecoder_* tables: the
// start-freq tables, envAdj gain/smooth/randomPhase tables, frame_info defaults,
// Huffman codebooks, invTable). See cgo.go.
#include "libfdk/libSBRdec/src/sbr_rom.cpp"
