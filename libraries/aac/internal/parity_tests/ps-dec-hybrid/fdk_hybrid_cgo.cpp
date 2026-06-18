// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine vendored libFDK/src/FDK_hybrid.cpp as its own TU: the hybrid
// analysis/synthesis filterbank (FDKhybridAnalysisInit/Apply,
// FDKhybridSynthesisInit/Apply) the PS tool uses to split the lowest 3 QMF
// subbands into 12 sub-subbands and recombine them. fft_8 is inline in fft.h.
// See cgo.go.
#include "libfdk/libFDK/src/FDK_hybrid.cpp"
