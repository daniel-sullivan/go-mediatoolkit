// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored libSYS generic standard-library
// shims (FDKmemcpy / FDKmemclear / FDKcalloc, …) as its own translation unit
// for the ADTS frame-sync-parse parity oracle. See libfdk/COPYING.
#include "libfdk/libSYS/src/genericStds.cpp"
