// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libSYS/src/genericStds.cpp —
 * supplies FDKmemcpy / FDKmemclear / FDKcalloc referenced by the FDK ring store
 * and bit_cnt.cpp. Genuine vendored source -> oracle stays real_vendored. */
#include "libfdk/libSYS/src/genericStds.cpp"
