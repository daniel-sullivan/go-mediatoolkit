// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/band_nrg.cpp —
 * the AAC encoder band/line-energy kernels (FDKaacEnc_CalcSfbMaxScaleSpec,
 * FDKaacEnc_CheckBandEnergyOptim, FDKaacEnc_CalcBandEnergyOptimLong /
 * ...Short, FDKaacEnc_CalcBandNrgMSOpt). The oracle links these GENUINE symbols
 * (oracle_kind == real_vendored), so the parity test compares against the real
 * reference, NOT a hand-twin.
 *
 * See bridge.cpp for the amalgamation-split rationale (each parity package
 * compiles its OWN copy of the needed fdk C TUs and never imports
 * libraries/aac). */
#include "libfdk/libAACenc/src/band_nrg.cpp"
