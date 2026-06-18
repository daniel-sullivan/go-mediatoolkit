// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/aacenc_pns.cpp —
 * FDKaacEnc_InitPnsConfiguration / FDKaacEnc_PnsDetect / FDKaacEnc_CodePnsChannel /
 * FDKaacEnc_PreProcessPnsChannelPair / FDKaacEnc_PostProcessPnsChannelPair plus the
 * static FDKaacEnc_FDKaacEnc_noiseDetection / FDKaacEnc_CalcNoiseNrgs. The oracle
 * links these GENUINE symbols (oracle_kind == real_vendored). */
#include "libfdk/libAACenc/src/aacenc_pns.cpp"
