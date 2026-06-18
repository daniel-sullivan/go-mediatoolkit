// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compiles the genuine vendored libFDK fixed-point math TU into this parity
// test binary. The verbatim twins in bridge.cpp call fMultI (inline in
// fixpoint_math.h), which in turn calls the out-of-line fMultNorm defined here.
// This pulls in the real fixed-point kernel — no re-derivation. Compiled into
// its OWN translation unit so its file-local statics never clash with a sibling
// parity package (the same amalgamation-split discipline the other slices use).

#include "src/fixpoint_math.cpp"
