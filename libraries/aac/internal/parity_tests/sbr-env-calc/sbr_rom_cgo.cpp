// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/sbr_rom.cpp as its own TU: the FDK_sbrDecoder_* tables
// (limGains, smoothFilter, randomPhase, limiterBandsPerOctaveDiv4, invTable, ...).
#include "libfdk/libSBRdec/src/sbr_rom.cpp"
