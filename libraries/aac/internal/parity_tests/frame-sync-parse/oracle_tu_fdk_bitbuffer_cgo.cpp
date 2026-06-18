// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored FDK bit buffer as its own
// translation unit for the ADTS frame-sync-parse parity oracle. The FDKreadBits
// / FDKgetValidBits / FDKpushBack / FDKpushFor primitives the ADTS reader and
// the lifted syncword search use resolve here. See libfdk/COPYING.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
