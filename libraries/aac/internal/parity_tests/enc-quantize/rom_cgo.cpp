// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacEnc_rom.cpp
 * — the encoder ROM tables the quantizer's static helpers read
 * (FDKaacEnc_mTab_3_4, FDKaacEnc_quantTableQ/E, FDKaacEnc_mTab_4_3Elc,
 * FDKaacEnc_specExpMantTableCombElc, FDKaacEnc_specExpTableComb). Linking the
 * genuine TU keeps the oracle real_vendored (the Go side's aac_rom_quant.go is
 * verified against these). See bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libAACenc/src/aacEnc_rom.cpp"
