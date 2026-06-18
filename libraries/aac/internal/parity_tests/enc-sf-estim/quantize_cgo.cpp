// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/quantize.cpp: sf_estim.cpp's
// analysis-by-synthesis passes call FDKaacEnc_calcSfbDist /
// FDKaacEnc_calcSfbQuantEnergyAndDist from here.
#include "libfdk/libAACenc/src/quantize.cpp"
