// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libSYS/src/genericStds.cpp so FDKmemclear (called
// by FDKaacEnc_EstimateScaleFactors / calcSfbRelevantLines) resolves.
#include "libfdk/libSYS/src/genericStds.cpp"
