// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libSYS/src/genericStds.cpp,
 * which psy_configuration.cpp links for FDKmemclear (the PSY_CONFIGURATION
 * zero-init at the top of FDKaacEnc_InitPsyConfiguration). See bridge.cpp for
 * the amalgamation-split rationale. */
#include "libfdk/libSYS/src/genericStds.cpp"
