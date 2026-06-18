// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Genuine libFDK/src/FDK_bitbuffer.cpp: the byte-buffer read primitives the FDK
// bit reader (pulled in by env_extr.cpp's sbrGetHeaderData/ChannelElement) needs.
// Those parse paths are never CALLED by this oracle (env_calc is driven directly),
// only linked through the env_extr TU.
#include "libfdk/libFDK/src/FDK_bitbuffer.cpp"
