// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/pnsparam.cpp —
 * FDKaacEnc_GetPnsParam / FDKaacEnc_lookUpPnsUse plus the static PNS tuning ROM
 * (levelTable_*, pnsInfoTab*). */
#include "libfdk/libAACenc/src/pnsparam.cpp"
