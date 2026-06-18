// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU supplying the FDK bit-buffer back-end (FDK_get32, FDK_byteAlign,
 * FDK_pushBack/For, FDK_InitBitBuffer, …) that the inline FDK_bitstream.h
 * readers in the oracle call into. */
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
