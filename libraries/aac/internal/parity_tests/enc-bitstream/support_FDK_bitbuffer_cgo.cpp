// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/FDK_bitbuffer.cpp
 * — the FDK_put ring store backing FDKwriteBits, and FDK_InitBitBuffer backing
 * FDKinitBitStream. The bit WRITER itself is inline in FDK_bitstream.h; this
 * supplies the out-of-line ring-buffer primitives. Genuine vendored source ->
 * oracle stays real_vendored. */
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
