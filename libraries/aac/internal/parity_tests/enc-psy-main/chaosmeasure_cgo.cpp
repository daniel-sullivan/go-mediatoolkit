// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Sibling TU compiling the genuine vendored
 * libfdk/libAACenc/src/chaosmeasure.cpp — FDKaacEnc_CalculateChaosMeasure,
 * which tonality.cpp calls. Linking the genuine TU keeps the tonality oracle
 * real_vendored. chaosmeasure.cpp calls schur_div (out-of-line in
 * fixpoint_math.cpp on non-x86 targets, supplied by the fixpoint_math_cgo.cpp
 * sibling TU) and the inline FDK fixed-point primitives; no other libfdk
 * module is dragged in. */
#include "libfdk/libAACenc/src/chaosmeasure.cpp"
