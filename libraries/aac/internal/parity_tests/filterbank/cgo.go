// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package filterbank pins the Go port of the Fraunhofer FDK-AAC decoder's
// fixed-point inverse-MDCT synthesis filterbank — imlt_block (FrequencyToTime),
// the AAC-LC stage that turns a dequantized/TNS'd MDCT spectrum into time
// samples with a windowed 50% overlap-add (libFDK/src/mdct.cpp) — against the
// vendored C, compiled into this test binary via cgo. The inner DCT-IV, the
// window slopes (FDKgetWindowSlope), the 2/N+output gain exponent fold
// (imdct_gain), and the overlap carry are all exercised: the in-place int32
// (FIXP_DBL) time output AND the resulting sample count are compared bit-for-bit
// across a stateful sequence of blocks (the IMDCT carries overlap frame to
// frame).
//
// This package compiles its OWN copy of the needed vendored C source (mdct.cpp +
// the dct.cpp/fft.cpp/fft_rad2.cpp transform stack + FDK_tools_rom.cpp twiddle/
// window ROM + scale.cpp saturating scaler + genericStds.cpp FDKmemcpy) and
// NEVER imports libraries/aac — importing it would link a second copy of the FDK
// reference and clash on static symbols (the same amalgamation-split reason the
// sibling dct/fft parity packages document). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the inverse MDCT is a pure INTEGER fixed-point kernel (FIXP_DBL
// Q-format data, FIXP_WTP/FIXP_STP Q1.15 int16 twiddles/window slopes). The
// fold/de-scale/overlap is integer adds/shifts + the int64-product>>32
// cplxMultDiv2/fMultDiv2 and the saturating scaleValue paths — bit-identical
// regardless of -ffp-contract / vectorization, with no transcendental. So this
// slice asserts EXACT int32 equality unconditionally (no aac_strict gate is
// needed — every AAC kernel is fixed-point): the integer kernel matches in any
// build.
package filterbank

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/filterbank).
// Mirrors the sibling dct oracle, plus libSYS/src for genericStds internals.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>
#include <stdlib.h>

extern void  mparity_window_slope(int length, int shape, int count, int16_t *out);
extern int   mparity_dct_tables(int tl, int twCount, int stCount, int16_t *twiddleOut, int16_t *sinTwiddleOut);
extern void *mparity_init(void);
extern void  mparity_free(void *st);
extern int   mparity_imlt_block(void *st, int32_t *output, const int32_t *spectrum,
                                const int16_t *scalefactor, int nSpec, int noOutSamples,
                                int tl, const int16_t *wls, int fl, const int16_t *wrs,
                                int fr, int32_t gain, int flags);
extern void  mparity_scale_out(int32_t *dst, const int32_t *src, int len, int scale);
*/
import "C"

import "unsafe"

// cWindowSlope returns the first `count` entries of the genuine
// FDKgetWindowSlope(length, shape) FIXP_WTP table as a flat int16 [re,im,...].
func cWindowSlope(length, shape, count int) []int16 {
	out := make([]int16, 2*count)
	C.mparity_window_slope(C.int(length), C.int(shape), C.int(count),
		(*C.int16_t)(unsafe.Pointer(&out[0])))
	return out
}

// cDctTables runs the genuine dct_getTables for length tl and returns the first
// twCount twiddle + stCount sin_twiddle entries (flat int16 re/im) plus sin_step.
func cDctTables(tl, twCount, stCount int) (twiddle, sinTwiddle []int16, sinStep int) {
	twiddle = make([]int16, 2*twCount)
	sinTwiddle = make([]int16, 2*stCount)
	sinStep = int(C.mparity_dct_tables(C.int(tl), C.int(twCount), C.int(stCount),
		(*C.int16_t)(unsafe.Pointer(&twiddle[0])),
		(*C.int16_t)(unsafe.Pointer(&sinTwiddle[0]))))
	return
}

// cState wraps the opaque persistent mdct_t handle the C side allocates.
type cState struct{ p unsafe.Pointer }

// cNewState allocates+zeroes the overlap buffer and runs mdct_init.
func cNewState() *cState { return &cState{p: C.mparity_init()} }

// free releases the C state.
func (s *cState) free() { C.mparity_free(s.p) }

// cImltBlock runs the genuine imlt_block over a copy of spectrum into a fresh
// output buffer of length noOutSamples and returns (output, nrSamples).
func (s *cState) cImltBlock(spectrum []int32, scalefactor []int16, nSpec, noOutSamples, tl int,
	wls []int16, fl int, wrs []int16, fr int, gain int32, flags int) ([]int32, int) {
	out := make([]int32, noOutSamples)
	n := int(C.mparity_imlt_block(s.p,
		(*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&spectrum[0])),
		(*C.int16_t)(unsafe.Pointer(&scalefactor[0])),
		C.int(nSpec), C.int(noOutSamples), C.int(tl),
		(*C.int16_t)(unsafe.Pointer(&wls[0])), C.int(fl),
		(*C.int16_t)(unsafe.Pointer(&wrs[0])), C.int(fr),
		C.int32_t(gain), C.int(flags)))
	return out, n
}

// cScaleOut runs the genuine scaleValuesSaturate(dst,src,len,scale) — the
// AAC-LC FrequencyToTime output tail (block.cpp:1240).
func cScaleOut(src []int32, length int, scale int) []int32 {
	dst := make([]int32, length)
	C.mparity_scale_out((*C.int32_t)(unsafe.Pointer(&dst[0])),
		(*C.int32_t)(unsafe.Pointer(&src[0])), C.int(length), C.int(scale))
	return dst
}
