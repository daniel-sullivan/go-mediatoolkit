// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package inverse_quant pins the Go port of the Fraunhofer FDK-AAC
// inverse-quantization kernels (the nativeaac functions evaluatePower43 /
// getScaleFromValue / maxabsD / inverseQuantizeBand) against the vendored
// libAACdec/src/block.cpp + block.h, compiled into this test binary via cgo.
// These kernels map a quantized spectral line back to its rescaled fixed-point
// magnitude — spectrum[i] = Sign(spectrum[i]) * Mantissa(spectrum[i])^(4/3) *
// 2^(lsb/4) — and the per-band headroom scale used to align the sfb. The Go
// side and the C oracle are driven with identical fabricated quantized
// spectra; the rescaled int32 / FIXP_DBL output and the returned exponents /
// scales are compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (aac_rom.cpp for the InverseQuantTable / MantissaTable / ExponentTable ROM,
// plus HCR-state link stubs aac_rom.cpp's unused dispatch table demands) and
// NEVER imports libraries/aac — importing it would link a second copy of the
// whole FDK reference and clash on static symbols (the same amalgamation-split
// reason the flac parity packages document). It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the inverse-quantization area is a pure INTEGER / fixed-point
// kernel — FIXP_DBL is a Q1.31 value carried in an int32, the (4/3)-power
// interpolation reads an integer ROM, fMultDiv2 is an int64 product shifted
// back to int32, and the gain is applied by arithmetic shifts. It is therefore
// bit-identical regardless of -ffp-contract / vectorization, so no
// transcendental shim is needed. The strict-gate on the Go assertions is kept
// for convention (the area lives under the aac_strict parity discipline); the
// kernel itself matches in any build. See block.cpp / aac_rom.cpp.
package inverse_quant

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at
// libraries/aac/internal/parity_tests/inverse-quant). Mirrors the set in
// libraries/aac/aac_cgo.go and the sibling huffman-spectral-decode oracle.
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
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_evaluate_power43 runs the genuine FDK_INLINE EvaluatePower43
// (block.h:247) over *value for the given lsb (0..3), writing the rescaled
// mantissa back to *value and returning its exponent.
extern int fparity_evaluate_power43(int32_t *value, unsigned lsb);

// fparity_get_scale_from_value runs the genuine FDK_INLINE GetScaleFromValue
// (block.h:283) for value/lsb, returning the required shift scale.
extern int fparity_get_scale_from_value(int32_t value, unsigned lsb);

// fparity_maxabs_d runs the verbatim maxabs_D (block.cpp:471) copy over the
// first noLines lines of spectrum.
extern int32_t fparity_maxabs_d(const int32_t *spectrum, int noLines);

// fparity_inverse_quantize_band runs the verbatim InverseQuantizeBand
// (block.cpp:436) copy over the first noLines lines of spectrum in place, using
// the vendored InverseQuantTable and the lsb-indexed MantissaTable /
// ExponentTable rows with the given band headroom scale.
extern void fparity_inverse_quantize_band(int32_t *spectrum, int lsb,
                                          int noLines, int scale);
*/
import "C"

import "unsafe"

// cEvaluatePower43 runs the vendored EvaluatePower43 over value for lsb,
// returning the rescaled mantissa and the result exponent.
func cEvaluatePower43(value int32, lsb uint32) (int32, int) {
	v := C.int32_t(value)
	exp := C.fparity_evaluate_power43(&v, C.uint(lsb))
	return int32(v), int(exp)
}

// cGetScaleFromValue runs the vendored GetScaleFromValue over value for lsb.
func cGetScaleFromValue(value int32, lsb uint32) int {
	return int(C.fparity_get_scale_from_value(C.int32_t(value), C.uint(lsb)))
}

// cMaxabsD runs the vendored maxabs_D over the first noLines lines of spectrum.
func cMaxabsD(spectrum []int32, noLines int) int32 {
	if noLines == 0 {
		return 0
	}
	return int32(C.fparity_maxabs_d(
		(*C.int32_t)(unsafe.Pointer(&spectrum[0])), C.int(noLines)))
}

// cInverseQuantizeBand runs the vendored InverseQuantizeBand in place over the
// first noLines lines of spectrum for lsb and the band headroom scale, and
// returns the rescaled spectrum.
func cInverseQuantizeBand(spectrum []int32, lsb int, noLines int, scale int32) []int32 {
	out := append([]int32(nil), spectrum...)
	if noLines > 0 {
		C.fparity_inverse_quantize_band(
			(*C.int32_t)(unsafe.Pointer(&out[0])), C.int(lsb),
			C.int(noLines), C.int(scale))
	}
	return out
}
