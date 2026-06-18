// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/fft.cpp (the fft() dispatcher + hard-coded fft_16/fft_32
// the QMF L==64 DCT routes through at M==32), its own TU. See cgo.go.
#include "libfdk/libFDK/src/fft.cpp"
