// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/fft_rad2.cpp (defines dit_fft, used by the 64/128/256/512
// dct paths), its own TU. See cgo.go.
#include "libfdk/libFDK/src/fft_rad2.cpp"
