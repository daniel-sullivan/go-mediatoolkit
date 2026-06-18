// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/sbr_ram.cpp as its own TU (the SBR working-memory
// definitions lpp_tran.cpp's sbr_ram.h include resolves against). See cgo.go.
#include "libfdk/libSBRdec/src/sbr_ram.cpp"
