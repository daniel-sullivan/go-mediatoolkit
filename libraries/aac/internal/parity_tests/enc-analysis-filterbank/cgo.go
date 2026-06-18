// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encanalysisfilterbank pins the Go port of the Fraunhofer FDK-AAC
// fixed-point ENCODE analysis filterbank — the forward (analysis) MDCT,
// FDKaacEnc_Transform_Real (libAACenc/src/transform.cpp), which folds+windows a
// block of INT_PCM time samples and runs the shared inner dct_IV to produce the
// FIXP_DBL MDCT spectrum (one long spectrum of frameLen lines or eight short
// spectra of frameLen/8 lines) plus the per-block exponent — against the
// vendored C, compiled into this test binary via cgo. The fold (overlap-aware,
// using the previous block's right window slope as this block's left slope), the
// per-spectrum block exponent, and the in-place int32 (FIXP_DBL) spectrum are
// compared bit-for-bit across a stateful sequence of blocks.
//
// This package compiles its OWN copy of the needed vendored C source
// (transform.cpp + mdct.cpp + the dct.cpp/fft.cpp/fft_rad2.cpp transform stack +
// FDK_tools_rom.cpp twiddle/window ROM + scale.cpp + genericStds.cpp +
// aacEnc_rom.cpp, which transform.cpp's ELD path references) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols (the same amalgamation-split reason the sibling decode
// filterbank parity package documents). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the forward MDCT is a pure INTEGER fixed-point kernel (FIXP_PCM ==
// int16 time samples since SAMPLE_BITS == 16, FIXP_DBL Q-format spectrum,
// FIXP_WTP Q1.15 int16 window slopes). The fold is integer shifts + the int32
// fixmuldiv2_SS products and the int64-product>>32 dct_IV — bit-identical
// regardless of -ffp-contract / vectorization, with no transcendental. So this
// slice asserts EXACT int32 equality unconditionally (no aac_strict gate is
// needed — every AAC kernel is fixed-point): the integer kernel matches in any
// build. The oracle is the
// genuine FDKaacEnc_Transform_Real symbol (oracle_kind == real_vendored).
package encanalysisfilterbank

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-analysis-filterbank).
// Mirrors the sibling decode filterbank oracle, plus libAACenc/src for the
// encoder transform.cpp / psy_const.h / transform.h / aacEnc_rom.h headers.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>
#include <stdlib.h>

extern void  eparity_window_slope(int length, int shape, int count, int16_t *out);
extern void *eparity_init(void);
extern void  eparity_free(void *st);
extern int   eparity_transform_real(void *st, const int16_t *pTimeData, int32_t *mdctData,
                                    int blockType, int windowShape, int *prevWindowShape,
                                    int frameLength, int *mdctData_e, int filterType);
*/
import "C"

import "unsafe"

// cWindowSlope returns the first `count` entries of the genuine
// FDKgetWindowSlope(length, shape) FIXP_WTP table as a flat int16 [re,im,...].
func cWindowSlope(length, shape, count int) []int16 {
	out := make([]int16, 2*count)
	C.eparity_window_slope(C.int(length), C.int(shape), C.int(count),
		(*C.int16_t)(unsafe.Pointer(&out[0])))
	return out
}

// cEncState wraps the opaque persistent mdct_t handle the C side allocates.
type cEncState struct{ p unsafe.Pointer }

// cNewEncState allocates+zeroes the state and runs mdct_init(NULL, 0) (the
// encoder per-channel init; the forward MDCT only uses prev_*).
func cNewEncState() *cEncState { return &cEncState{p: C.eparity_init()} }

// free releases the C state.
func (s *cEncState) free() { C.eparity_free(s.p) }

// cTransformReal runs the genuine FDKaacEnc_Transform_Real over pTimeData into a
// fresh mdctData buffer of length frameLength, returning (mdctData, rc,
// mdctDataE, prevWindowShape). prevWindowShape is updated by the C in place.
func (s *cEncState) cTransformReal(pTimeData []int16, blockType, windowShape, prevWindowShape,
	frameLength, filterType int) (mdctData []int32, rc, mdctDataE, newPrevWindowShape int) {
	mdctData = make([]int32, frameLength)
	pws := C.int(prevWindowShape)
	e := C.int(0)
	rc = int(C.eparity_transform_real(s.p,
		(*C.int16_t)(unsafe.Pointer(&pTimeData[0])),
		(*C.int32_t)(unsafe.Pointer(&mdctData[0])),
		C.int(blockType), C.int(windowShape), &pws,
		C.int(frameLength), &e, C.int(filterType)))
	return mdctData, rc, int(e), int(pws)
}
