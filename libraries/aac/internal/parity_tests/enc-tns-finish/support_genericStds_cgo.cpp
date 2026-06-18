// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libSYS/src/genericStds.cpp —
 * supplies FDKmemcpy / FDKmemclear referenced by the (uncalled-but-emitted)
 * FDKaacEnc_InitTnsConfiguration / FDKaacEnc_TnsDetect / FDKaacEnc_TnsEncode in
 * the included aacEnc_tns.cpp. Genuine vendored source -> oracle stays
 * real_vendored. */
#include "libfdk/libSYS/src/genericStds.cpp"
