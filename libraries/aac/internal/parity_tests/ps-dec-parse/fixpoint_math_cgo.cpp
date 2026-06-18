// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/fixpoint_math.cpp (defines CalcLdInt + the ld/log tables
// that FDK_getNumOctavesDiv8 — used by the freq-scale builder — needs). Its own
// TU. See cgo.go.
#include "libfdk/libFDK/src/fixpoint_math.cpp"
