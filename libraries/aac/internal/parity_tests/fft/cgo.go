// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package fft pins the Go port of the Fraunhofer FDK-AAC decoder's fixed-point
// DIT FFT — dit_fft (libFDK/src/fft_rad2.cpp) plus the SineTable512 twiddle ROM
// (libFDK/src/FDK_tools_rom.cpp) the AAC-LC filterbank fft() dispatcher feeds it
// for lengths 64/128/256/512 — against the vendored C, compiled into this test
// binary via cgo. Random Q1.31 interleaved-complex spectra are FFT'd on both
// sides and the in-place int32 (FIXP_DBL) output is compared bit-for-bit; the
// narrowed Q1.15 trig ROM is verified entry-for-entry too.
//
// This package compiles its OWN copy of the needed vendored C source and NEVER
// imports libraries/aac — importing it would link a second copy of the whole
// FDK reference and clash on static symbols (the same amalgamation-split reason
// the sibling parity packages document). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the FFT is a pure INTEGER fixed-point kernel (FIXP_DBL Q1.31 data,
// FIXP_SGL Q1.15 twiddles). The butterflies are integer adds/shifts and the
// int64-product>>32 fixmul/cplxMul kernels — bit-identical regardless of
// -ffp-contract / vectorization, with no transcendental. So this slice asserts
// EXACT int32 equality unconditionally (no aac_strict gate is needed — every
// AAC kernel is fixed-point): the integer kernel matches in any build.
package fft

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/fft). Mirrors the
// set in the sibling tns-decode / huffman-spectral-decode oracles.
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

extern void fparity_dit_fft(int32_t *x, int ldn);
extern void fparity_sinetable512_q15(int16_t *outRe, int16_t *outIm, int count);
*/
import "C"

import "unsafe"

// cDitFFT runs the vendored dit_fft over a copy of x (interleaved complex,
// length 2*(1<<ldn)) and returns the in-place result.
func cDitFFT(x []int32, ldn int) []int32 {
	out := append([]int32(nil), x...)
	C.fparity_dit_fft((*C.int32_t)(unsafe.Pointer(&out[0])), C.int(ldn))
	return out
}

// cSineTable512Q15 returns the first count entries of the genuine in-RAM
// SineTable512 as (re,im) int16 pairs.
func cSineTable512Q15(count int) [][2]int16 {
	re := make([]int16, count)
	im := make([]int16, count)
	C.fparity_sinetable512_q15(
		(*C.int16_t)(unsafe.Pointer(&re[0])),
		(*C.int16_t)(unsafe.Pointer(&im[0])),
		C.int(count))
	out := make([][2]int16, count)
	for i := 0; i < count; i++ {
		out[i] = [2]int16{re[i], im[i]}
	}
	return out
}
