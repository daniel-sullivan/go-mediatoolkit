// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libSYS/src/genericStds.cpp,
 * which block_switch.cpp links for FDKmemclear / FDKmemcpy. See bridge.cpp for
 * the amalgamation-split rationale (each parity package compiles its OWN copy of
 * the needed fdk C TUs and never imports libraries/aac). */
#include "libfdk/libSYS/src/genericStds.cpp"
