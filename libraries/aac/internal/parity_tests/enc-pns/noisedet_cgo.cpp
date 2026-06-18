// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/noisedet.cpp —
 * FDKaacEnc_noiseDetect (the power-distribution + psych-tonality noise detector
 * the PnsDetect chain calls) plus the static FDKaacEnc_fuzzyIsSmaller. */
#include "libfdk/libAACenc/src/noisedet.cpp"
