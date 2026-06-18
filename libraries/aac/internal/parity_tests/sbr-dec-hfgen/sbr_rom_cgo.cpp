// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/sbr_rom.cpp as its own TU: defines
// FDK_sbrDecoder_sbr_whFactorsIndex / _whFactorsTable (the LPP whitening ROM) and
// the other SBR tables lpp_tran.cpp references. See cgo.go.
#include "libfdk/libSBRdec/src/sbr_rom.cpp"
