// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored FDK bit buffer
// (libFDK/src/FDK_bitbuffer.cpp) as its own translation unit for the
// bitstream-encode parity oracle. FDK_put (the MSB-first ring store the inline
// FDKwriteBits / FDKbyteAlign flush through) and FDK_InitBitBuffer resolve
// here. See libfdk/COPYING.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
