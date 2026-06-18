// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

/* Parity oracle for the Fraunhofer FDK-AAC encoder chaos (tonality) measure
 * (libAACenc/src/chaosmeasure.cpp). This translation unit compiles the
 * vendored chaosmeasure.cpp verbatim (via #include of the real source, the
 * same per-TU technique the libraries/aac fdk_tu_*.cpp wrappers use) and then
 * exposes a thin extern "C" bridge, fparity_chaos_measure, that the Go test
 * calls to obtain the reference output. The Go side
 * (nativeaac.calculateChaosMeasure, ported 1:1) is asserted bit-for-bit
 * against this.
 *
 * Why this slice is a clean oracle: FDKaacEnc_CalculateChaosMeasure is a
 * non-static, self-contained function over a FIXP_DBL (int32) MDCT array and
 * a FIXP_DBL output array. Its only external dependencies are the inline FDK
 * fixed-point primitives (fMult, CntLeadingZeros) plus schur_div; the latter
 * is provided generically by fixpoint_math.cpp on non-x86 targets, so that
 * source is compiled as its own sibling TU (chaosmeasure_cgo_schur.cpp) — no
 * other libfdk module is linked, so there is no cross-package static-symbol
 * clash. This file NEVER imports libraries/aac (which would link a second
 * copy of the whole reference); it stands alone alongside nativeaac.
 *
 * FP-parity: chaos measure is a pure fixed-point INTEGER kernel — every
 * operation is on signed 32-bit fractions with arithmetic shifts and an
 * int64-intermediate multiply. It is therefore bit-identical regardless of
 * -ffp-contract / vectorization, so no transcendental shim is needed here.
 * The strict-gate on the Go assertion is kept only for convention (the area
 * lives under the aac_strict parity discipline); the kernel itself matches in
 * any build.
 *
 * Build flags: only -I / -D / -Wno-* live in the in-source #cgo CFLAGS (see
 * cgo.go). The scalar FP flags (-ffp-contract=off -fno-vectorize
 * -fno-slp-vectorize -fno-unroll-loops) come from the mise task env
 * (CGO_CFLAGS), not here — but they are irrelevant to this integer kernel.
 */

#include "libfdk/libAACenc/src/chaosmeasure.cpp"

extern "C" void fparity_chaos_measure(int *paMDCTDataNM0, int numberOfLines,
                                      int *chaosMeasure) {
  FDKaacEnc_CalculateChaosMeasure((FIXP_DBL *)paMDCTDataNM0, (INT)numberOfLines,
                                  (FIXP_DBL *)chaosMeasure);
}
