// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored AudioSpecificConfig / CProgramConfig
// reader as its own translation unit for the ADTS frame-sync-parse parity
// oracle. adtsRead_DecodeHeader links AudioSpecificConfig_Init and the
// CProgramConfig_* helpers from here. See libfdk/COPYING.
#include "libfdk/libMpegTPDec/src/tpdec_asc.cpp"
