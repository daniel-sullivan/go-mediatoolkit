// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/tonality.cpp — FDKaacEnc_CalculateFullTonality (and its
 * static FDKaacEnc_CalcSfbTonality), the chaos-measure -> tonality-index
 * kernel FDKaacEnc_psyMain calls per long-block channel (psy_main.cpp:759).
 * The oracle links this GENUINE symbol (oracle_kind == real_vendored).
 *
 * tonality.cpp includes chaosmeasure.h and calls FDKaacEnc_CalculateChaosMeasure
 * (defined in chaosmeasure.cpp -> chaosmeasure_cgo.cpp sibling TU) and the
 * inline CalcLdData (fixpoint_math.h, whose ldDataTable lives in
 * fixpoint_math.cpp -> fixpoint_math_cgo.cpp sibling TU). No other libfdk
 * module is linked. See bridge.cpp for the amalgamation-split rationale. */
#include "libfdk/libAACenc/src/tonality.cpp"
