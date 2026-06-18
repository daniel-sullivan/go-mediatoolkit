// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encquantize pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE quantizer — FDKaacEnc_QuantizeSpectrum / FDKaacEnc_quantizeLines /
// FDKaacEnc_invQuantizeLines / FDKaacEnc_calcSfbDist /
// FDKaacEnc_calcSfbQuantEnergyAndDist (libAACenc/src/quantize.cpp) — against the
// vendored C, compiled into this test binary via cgo.
//
// These kernels are the scalefactor-estimation / quant-loop core of the AAC-LC
// encoder (sf_estim.cpp + adj_thr.cpp drive them): they turn the windowed MDCT
// spectrum + a per-SFB scalefactor into the quantized SHORT spectrum, the
// inverse-quantized reconstruction, and the two ld-domain cost metrics (the
// quantization distortion and the quantized energy) the scalefactor search
// minimises against the masking threshold within the bit budget. Every value is
// an int32 FIXP_DBL / int16 SHORT Q-format with carried block exponents; the
// whole result is compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (quantize.cpp + aacEnc_rom.cpp for the quantizer ROM tables + fixpoint_math.cpp
// for CalcLdData/LdDataVector) and NEVER imports libraries/aac — importing it
// would link a second copy of the FDK reference and clash on static symbols (the
// same amalgamation-split reason the sibling parity packages document). It MAY,
// and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — these are pure INTEGER kernels
// (FIXP_DBL == int32, SHORT == int16). The leading-bit counts, arithmetic-shift
// block-floating-point accumulation, int64-product fixmul kernels, and the
// table-driven ^3/4 / ^4/3 mantissa lookups + fLog2 are bit-identical regardless
// of -ffp-contract / vectorization, with no transcendental and no float. So they
// assert EXACT int32 equality. The oracle links the genuine
// FDKaacEnc_QuantizeSpectrum / ...quantizeLines / ...invQuantizeLines /
// ...calcSfbDist / ...calcSfbQuantEnergyAndDist symbols (oracle_kind ==
// real_vendored). The static helpers FDKaacEnc_quantizeLines /
// FDKaacEnc_invQuantizeLines are reached through tiny extern shims compiled
// INSIDE quantize.cpp's own TU via #include (so the genuine static functions are
// the oracle, not a hand-twin).
package encquantize

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-quantize).
// quantize.cpp pulls quantize.h + aacEnc_rom.h -> common_fix.h; aacEnc_rom.cpp
// pulls aacEnc_rom.h; fixpoint_math.cpp pulls fixpoint_math.h (libFDK + libSYS).
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to these integer kernels in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void    qparity_quantize_lines(int gain, int noOfLines,
                   const int32_t *mdctSpectrum, int16_t *quaSpectrum, int dZoneQuantEnable);
extern void    qparity_inv_quantize_lines(int gain, int noOfLines,
                   int16_t *quantSpectrum, int32_t *mdctSpectrum);
extern void    qparity_quantize_spectrum(int sfbCnt, int maxSfbPerGroup, int sfbPerGroup,
                   const int32_t *sfbOffset, const int32_t *mdctSpectrum, int globalGain,
                   const int32_t *scalefactors, int16_t *quantizedSpectrum, int dZoneQuantEnable);
extern int32_t qparity_calc_sfb_dist(const int32_t *mdctSpectrum, int16_t *quantSpectrum,
                   int noOfLines, int gain, int dZoneQuantEnable);
extern void    qparity_calc_sfb_quant_energy_and_dist(int32_t *mdctSpectrum, int16_t *quantSpectrum,
                   int noOfLines, int gain, int32_t *en, int32_t *dist);
extern void    qparity_quant_rom(int16_t *mTab34, int16_t *quantTableQ,
                   int16_t *quantTableE, int32_t *mTab43);
*/
import "C"

import "unsafe"

// i32p returns a *C.int32_t over a Go []int32 (nil for empty).
func i32p(s []int32) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&s[0]))
}

// i16p returns a *C.int16_t over a Go []int16 (nil for empty).
func i16p(s []int16) *C.int16_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int16_t)(unsafe.Pointer(&s[0]))
}

// toI32 converts a []int (band offsets / scalefactors) to []int32 for the C
// side, which uses INT == int32.
func toI32(s []int) []int32 {
	o := make([]int32, len(s))
	for i, v := range s {
		o[i] = int32(v)
	}
	return o
}

// cQuantizeLines runs the genuine FDKaacEnc_quantizeLines over a copy of the
// input, returning the quantized SHORT spectrum.
func cQuantizeLines(gain, noOfLines int, mdctSpectrum []int32, dZoneQuantEnable bool) []int16 {
	out := make([]int16, noOfLines)
	C.qparity_quantize_lines(C.int(gain), C.int(noOfLines), i32p(mdctSpectrum), i16p(out), cbool(dZoneQuantEnable))
	return out
}

// cInvQuantizeLines runs the genuine FDKaacEnc_invQuantizeLines, returning the
// reconstructed FIXP_DBL spectrum.
func cInvQuantizeLines(gain, noOfLines int, quantSpectrum []int16) []int32 {
	qs := make([]int16, len(quantSpectrum))
	copy(qs, quantSpectrum)
	out := make([]int32, noOfLines)
	C.qparity_inv_quantize_lines(C.int(gain), C.int(noOfLines), i16p(qs), i32p(out))
	return out
}

// cQuantizeSpectrum runs the genuine FDKaacEnc_QuantizeSpectrum, returning the
// quantized SHORT spectrum (length == sfbOffset[sfbCnt]).
func cQuantizeSpectrum(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int,
	mdctSpectrum []int32, globalGain int, scalefactors []int, dZoneQuantEnable bool) []int16 {
	so := toI32(sfbOffset)
	scf := toI32(scalefactors)
	out := make([]int16, sfbOffset[sfbCnt])
	C.qparity_quantize_spectrum(C.int(sfbCnt), C.int(maxSfbPerGroup), C.int(sfbPerGroup),
		i32p(so), i32p(mdctSpectrum), C.int(globalGain), i32p(scf), i16p(out), cbool(dZoneQuantEnable))
	return out
}

// cCalcSfbDist runs the genuine FDKaacEnc_calcSfbDist over a copy of the input,
// returning the ld-domain distortion.
func cCalcSfbDist(mdctSpectrum []int32, noOfLines, gain int, dZoneQuantEnable bool) int32 {
	scratch := make([]int16, noOfLines)
	return int32(C.qparity_calc_sfb_dist(i32p(mdctSpectrum), i16p(scratch),
		C.int(noOfLines), C.int(gain), cbool(dZoneQuantEnable)))
}

// cCalcSfbQuantEnergyAndDist runs the genuine
// FDKaacEnc_calcSfbQuantEnergyAndDist, returning (en, dist).
func cCalcSfbQuantEnergyAndDist(mdctSpectrum []int32, quantSpectrum []int16, noOfLines, gain int) (int32, int32) {
	md := make([]int32, len(mdctSpectrum))
	copy(md, mdctSpectrum)
	qs := make([]int16, len(quantSpectrum))
	copy(qs, quantSpectrum)
	var en, dist C.int32_t
	C.qparity_calc_sfb_quant_energy_and_dist(i32p(md), i16p(qs), C.int(noOfLines), C.int(gain),
		(*C.int32_t)(unsafe.Pointer(&en)), (*C.int32_t)(unsafe.Pointer(&dist)))
	return int32(en), int32(dist)
}

// cQuantRom returns the genuine vendored quantizer ROM tables (mTab_3_4,
// quantTableQ, quantTableE, mTab_4_3Elc), so the test verifies the Go
// transcription bit-for-bit.
func cQuantRom() (mTab34, quantTableQ, quantTableE []int16, mTab43 []int32) {
	mTab34 = make([]int16, 512)
	quantTableQ = make([]int16, 4)
	quantTableE = make([]int16, 4)
	mTab43 = make([]int32, 512)
	C.qparity_quant_rom(i16p(mTab34), i16p(quantTableQ), i16p(quantTableE), i32p(mTab43))
	return mTab34, quantTableQ, quantTableE, mTab43
}

// cbool maps a Go bool to the C int dZoneQuantEnable flag (0/1).
func cbool(b bool) C.int {
	if b {
		return 1
	}
	return 0
}
