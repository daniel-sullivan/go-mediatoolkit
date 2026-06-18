// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_bitbuffer.cpp, its own TU: BitMask[], FDK_InitBitBuffer,
// FDK_get32/FDK_get, FDK_getValidBits, FDK_pushBack/FDK_pushForward, FDK_put —
// the byte-buffer read/write primitives the FDK bit reader/writer the SBR parse
// path uses are built on. See cgo.go.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
