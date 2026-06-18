// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libFDK/src/dct.cpp that the
 * FDKaacEnc_Transform_Real (forward MDCT) oracle links. See bridge.cpp for the
 * amalgamation-split rationale (each parity package compiles its OWN copy of the
 * needed fdk C TUs and never imports libraries/aac). */
#include "libfdk/libFDK/src/dct.cpp"
