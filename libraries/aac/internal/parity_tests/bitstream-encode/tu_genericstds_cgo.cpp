// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Per-TU cgo wrapper compiling the vendored libSYS generic standard-library
// shims (libSYS/src/genericStds.cpp: FDKmemcpy / FDKmemclear / FDKcalloc, …)
// as its own translation unit for the bitstream-encode parity oracle. See
// libfdk/COPYING.
#include "libfdk/libSYS/src/genericStds.cpp"
