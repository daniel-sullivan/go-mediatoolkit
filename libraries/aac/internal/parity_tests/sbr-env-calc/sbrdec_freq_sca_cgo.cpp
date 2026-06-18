// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSBRdec/src/sbrdec_freq_sca.cpp as its own TU: the master/hi/lo/noise
// frequency band-table builder resetFreqBandTables drives + shellsort.
#include "libfdk/libSBRdec/src/sbrdec_freq_sca.cpp"
