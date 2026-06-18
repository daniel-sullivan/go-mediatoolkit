// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/sf_estim.cpp into this parity test
// binary so the oracle bridge links the REAL FDKaacEnc_CalcFormFactor /
// FDKaacEnc_EstimateScaleFactors (which drive the entire static helper chain).
// This package owns its own copy of the needed C TUs and never imports
// libraries/aac (which would link a second copy and clash on static symbols).
#include "libfdk/libAACenc/src/sf_estim.cpp"
