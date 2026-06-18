// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psychoacoustics_encoder pins the Go port of the Fraunhofer FDK-AAC
// encoder chaos (tonality) measure (nativeaac.calculateChaosMeasure) against
// the vendored libAACenc/src/chaosmeasure.cpp, compiled into this test binary
// via cgo. For a range of fabricated MDCT magnitude arrays the C chaos
// measure is computed and compared bit-for-bit (raw int32) against the
// nativeaac port.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (chaosmeasure.cpp + fixpoint_math.cpp for schur_div, one go-test binary per
// package) and NEVER imports libraries/aac — importing it would link a second
// copy of the whole FDK reference and clash on static symbols (the same
// amalgamation-split reason the flac parity packages document). It MAY, and
// does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
package psychoacoustics_encoder

/*
// Include search paths for the vendored libfdk tree, mirroring the set in
// libraries/aac/aac_cgo.go but rooted three levels up (this package lives at
// libraries/aac/internal/parity_tests/psychoacoustics-encoder). Only the
// chaos-measure path actually needs libAACenc/src (for chaosmeasure.h /
// psy_const.h) and the libFDK includes; the rest are listed for parity with
// the main backend so any sibling psy TU added later compiles unchanged.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_chaos_measure runs FDKaacEnc_CalculateChaosMeasure over an int32
// (FIXP_DBL) MDCT magnitude array of numberOfLines entries, writing the chaos
// measure into chaosMeasure[0:numberOfLines]. Defined in
// chaosmeasure_cgo_src.cpp.
extern void fparity_chaos_measure(int *paMDCTDataNM0, int numberOfLines,
                                  int *chaosMeasure);
*/
import "C"

import "unsafe"

// cChaosMeasure computes the libFDK chaos measure for the int32 MDCT
// magnitudes in mdct, returning a fresh int32 slice of the same length. mdct
// must hold at least 4 lines (the peak filter primes four taps).
func cChaosMeasure(mdct []int32) []int32 {
	out := make([]int32, len(mdct))
	if len(mdct) == 0 {
		return out
	}
	C.fparity_chaos_measure(
		(*C.int)(unsafe.Pointer(&mdct[0])),
		C.int(len(mdct)),
		(*C.int)(unsafe.Pointer(&out[0])),
	)
	return out
}
