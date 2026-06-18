// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libfdk/libSYS/src/genericStds.cpp so the
// FDKmemclear primitive (channel_map.cpp clears the CHANNEL_MAPPING in
// FDKaacEnc_InitChannelMapping) resolves at link time.
#include "libfdk/libSYS/src/genericStds.cpp"
