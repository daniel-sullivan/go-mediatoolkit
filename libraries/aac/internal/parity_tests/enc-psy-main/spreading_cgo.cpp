// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/spreading.cpp — FDKaacEnc_SpreadingMax, the energy
 * spreading kernel FDKaacEnc_psyMain applies to the masking thresholds and the
 * spread-energy estimate (psy_main.cpp:950, 1014). The oracle links this
 * GENUINE symbol (oracle_kind == real_vendored), so the parity test compares
 * against the real reference, NOT a hand-twin. See bridge.cpp for the
 * amalgamation-split rationale. */
#include "libfdk/libAACenc/src/spreading.cpp"
