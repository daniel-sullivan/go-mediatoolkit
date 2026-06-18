// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package dct pins the Go port of the Fraunhofer FDK-AAC decoder's fixed-point
// DCT/DST primitives — dct_II / dct_III / dct_IV / dst_III / dst_IV
// (libFDK/src/dct.cpp), the FFT-kernel-based transforms the AAC-LC inverse
// filterbank (imdct_block) builds on top of the FFT stage — against the vendored
// C, compiled into this test binary via cgo. The genuine dct_getTables selects
// the twiddle (FIXP_WTP) and sin_twiddle (FIXP_STP) ROM + sin_step for each
// transform length; the same ROM and sin_step drive the Go port, and the in-place
// int32 (FIXP_DBL) output AND the exponent delta (the e in the (mantissa,exponent)
// pair the MDCT carries) are compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source (dct.cpp +
// the fft() dispatcher fft.cpp + dit_fft fft_rad2.cpp + the FDK_tools_rom.cpp
// twiddle/window ROM) and NEVER imports libraries/aac — importing it would link a
// second copy of the FDK reference and clash on static symbols (the same
// amalgamation-split reason the sibling parity packages document). It MAY, and
// does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the DCT/DST are pure INTEGER fixed-point kernels (FIXP_DBL Q-format
// data, FIXP_SGL Q1.15 twiddles). The pre/post twiddle is integer adds/shifts
// and the int64-product>>32 fixmul/cplxMul kernels — bit-identical regardless of
// -ffp-contract / vectorization, with no transcendental. So this slice asserts
// EXACT int32 equality unconditionally (no aac_strict gate is needed — every
// AAC kernel is fixed-point): the integer kernel matches in any build.
package dct

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/dct). Mirrors the
// sibling fft oracle.
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

extern int dparity_get_tables(int L, int twCount, int stCount, int16_t *twiddleOut, int16_t *sinTwiddleOut);
extern int dparity_dct_iv(int32_t *pDat, int L);
extern int dparity_dst_iv(int32_t *pDat, int L);
extern int dparity_dct_iii(int32_t *pDat, int32_t *tmp, int L);
extern int dparity_dst_iii(int32_t *pDat, int32_t *tmp, int L);
extern int dparity_dct_ii(int32_t *pDat, int32_t *tmp, int L);
*/
import "C"

import "unsafe"

// cGetTables runs the genuine dct_getTables for length L and returns the first
// twCount entries of the selected twiddle ROM and stCount entries of the
// sin_twiddle ROM as flat int16 [re,im,...] slices, plus the selected sin_step.
// The two counts differ because the windowSlopes twiddle table holds M entries
// while the SineTable sin_twiddle holds M+1 (over-reading either would walk past
// the genuine ROM).
func cGetTables(L, twCount, stCount int) (twiddle, sinTwiddle []int16, sinStep int) {
	twiddle = make([]int16, 2*twCount)
	sinTwiddle = make([]int16, 2*stCount)
	sinStep = int(C.dparity_get_tables(C.int(L), C.int(twCount), C.int(stCount),
		(*C.int16_t)(unsafe.Pointer(&twiddle[0])),
		(*C.int16_t)(unsafe.Pointer(&sinTwiddle[0]))))
	return
}

// cDctIV runs the vendored dct_IV over a copy of pDat and returns the result
// plus the exponent delta.
func cDctIV(pDat []int32, L int) ([]int32, int) {
	out := append([]int32(nil), pDat...)
	e := int(C.dparity_dct_iv((*C.int32_t)(unsafe.Pointer(&out[0])), C.int(L)))
	return out, e
}

func cDstIV(pDat []int32, L int) ([]int32, int) {
	out := append([]int32(nil), pDat...)
	e := int(C.dparity_dst_iv((*C.int32_t)(unsafe.Pointer(&out[0])), C.int(L)))
	return out, e
}

func cDctIII(pDat []int32, L int) ([]int32, int) {
	out := append([]int32(nil), pDat...)
	tmp := make([]int32, L)
	e := int(C.dparity_dct_iii((*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&tmp[0])), C.int(L)))
	return out, e
}

func cDstIII(pDat []int32, L int) ([]int32, int) {
	out := append([]int32(nil), pDat...)
	tmp := make([]int32, L)
	e := int(C.dparity_dst_iii((*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&tmp[0])), C.int(L)))
	return out, e
}

func cDctII(pDat []int32, L int) ([]int32, int) {
	out := append([]int32(nil), pDat...)
	tmp := make([]int32, L)
	e := int(C.dparity_dct_ii((*C.int32_t)(unsafe.Pointer(&out[0])),
		(*C.int32_t)(unsafe.Pointer(&tmp[0])), C.int(L)))
	return out, e
}
