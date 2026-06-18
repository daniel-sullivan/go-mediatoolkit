// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/psy_configuration.cpp — FDKaacEnc_InitPsyConfiguration
 * (psy_configuration.cpp:534) plus its static helpers (initSfbTable,
 * BarcLineValue, InitMinPCMResolution, getMaskFactor, initSpreading,
 * initBarcValues, initMinSnr). The oracle links this GENUINE symbol
 * (oracle_kind == real_vendored), so the parity test compares against the real
 * reference, NOT a hand-twin. See bridge.cpp for the amalgamation-split
 * rationale. */
#include "libfdk/libAACenc/src/psy_configuration.cpp"
