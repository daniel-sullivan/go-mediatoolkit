// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Sibling TU compiling the genuine vendored libSYS/src/genericStds.cpp so the
// FDKcalloc / FDKfree / FDKaalloc / FDKafree allocators (referenced by the
// aacEnc_ram.cpp RAM pools that back the unused FDKaacEnc_BCNew) and the
// FDKmemclear / FDKmemcpy primitives (referenced by FDK_bitbuffer.cpp) resolve at
// link time. Genuine vendored source -> the oracle stays real_vendored.
#include "libfdk/libSYS/src/genericStds.cpp"
