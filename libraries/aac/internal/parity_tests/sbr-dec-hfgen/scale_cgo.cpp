// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/scale.cpp as its own TU (scaleValues/scaleValuesSaturate/
// getScalefactor). See cgo.go.
#include "libfdk/libFDK/src/scale.cpp"
