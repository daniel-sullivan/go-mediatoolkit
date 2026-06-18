// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libAACenc/src/dyn_bits.cpp into this parity test
// binary so the oracle bridge links the REAL FDKaacEnc_dynBitCount (which drives
// the entire static helper chain: noiselessCounter, gmStage0/1/2, buildBitLookUp,
// findBestBook/findMinMergeBits/mergeBitLookUp/findMaxMerge/CalcMergeGain,
// getSideInfoBits, scfCount, noiseCount). This package owns its own copy of the
// needed C TUs and never imports libraries/aac (which would link a second copy
// and clash on static symbols).
#include "libfdk/libAACenc/src/dyn_bits.cpp"
