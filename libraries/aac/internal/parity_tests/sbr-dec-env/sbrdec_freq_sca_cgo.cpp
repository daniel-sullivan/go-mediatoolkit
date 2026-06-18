// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the GENUINE vendored libSBRdec/src/sbrdec_freq_sca.cpp as its own TU:
// the master/hi/lo/noise frequency band-table builder (sbrdecUpdateFreqScale,
// resetFreqBandTables, getStartBand/getStopBand, CalcBands/cumSum/shellsort).
// Its statics stay file-local; the oracle is the real reference. See cgo.go.
#include "libfdk/libSBRdec/src/sbrdec_freq_sca.cpp"
