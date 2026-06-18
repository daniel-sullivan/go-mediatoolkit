// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/block_switch.cpp — the AAC encoder block-switch decision
 * kernel (FDKaacEnc_InitBlockSwitching / FDKaacEnc_BlockSwitching /
 * FDKaacEnc_SyncBlockSwitching, plus the static FDKaacEnc_CalcWindowEnergy /
 * FDKaacEnc_GetWindowEnergy they call). The oracle links these GENUINE symbols
 * (oracle_kind == real_vendored), so the parity test compares against the real
 * reference, NOT a hand-twin.
 *
 * See bridge.cpp for the amalgamation-split rationale (each parity package
 * compiles its OWN copy of the needed fdk C TUs and never imports
 * libraries/aac). */
#include "libfdk/libAACenc/src/block_switch.cpp"
