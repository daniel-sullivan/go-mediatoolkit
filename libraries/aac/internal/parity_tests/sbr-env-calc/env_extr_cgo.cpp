// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/env_extr.cpp as its own TU: resetFreqBandTables (which
// env_calc's setup needs to build the freq band tables) + sbrdec_mapToStdSampleRate.
#include "libfdk/libSBRdec/src/env_extr.cpp"
