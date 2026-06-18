// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Compile the genuine vendored libSYS/src/genericStds.cpp so FDKmemclear (called
// by FDKaacEnc_calcWeighting) and the FDKcalloc/FDKfree the aacEnc_ram allocators
// use resolve at link time.
#include "libfdk/libSYS/src/genericStds.cpp"
