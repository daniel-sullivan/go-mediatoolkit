// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored encoder analysis filterbank
 * libfdk/libAACenc/src/transform.cpp (FDKaacEnc_Transform_Real == the forward
 * MDCT stage entry point, plus FDKaacEnc_Transform_Real_Eld which is not on the
 * AAC-LC path). See bridge.cpp for the amalgamation-split rationale (each parity
 * package compiles its OWN copy of the needed fdk C TUs and never imports
 * libraries/aac). */
#include "libfdk/libAACenc/src/transform.cpp"
