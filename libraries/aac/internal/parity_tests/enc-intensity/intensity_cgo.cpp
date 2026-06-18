// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored libfdk/libAACenc/src/intensity.cpp —
 * FDKaacEnc_IntensityStereoProcessing plus the static FDKaacEnc_initIsParams /
 * FDKaacEnc_prepareIntensityDecision / FDKaacEnc_finalizeIntensityDecision /
 * calcSfbMaxScale. The oracle links these GENUINE symbols (oracle_kind ==
 * real_vendored). */
#include "libfdk/libAACenc/src/intensity.cpp"
