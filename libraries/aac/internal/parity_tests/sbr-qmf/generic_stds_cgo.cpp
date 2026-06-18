// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libSYS/src/genericStds.cpp (defines FDKmemclear / FDKmemmove /
// FDKmemcpy the QMF init + slot shift use), its own TU. See cgo.go.
#include "libfdk/libSYS/src/genericStds.cpp"
