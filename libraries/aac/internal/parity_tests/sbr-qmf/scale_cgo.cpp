// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/scale.cpp (defines scaleValues / scaleValuesSaturate the
// QMF inverse modulation + filter-state adapt use), its own TU. See cgo.go.
#include "libfdk/libFDK/src/scale.cpp"
