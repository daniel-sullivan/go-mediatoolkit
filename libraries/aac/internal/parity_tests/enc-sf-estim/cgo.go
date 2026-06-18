// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encsfestim pins the Go port of the Fraunhofer FDK-AAC encoder
// scale-factor estimation DRIVER stage — FDKaacEnc_CalcFormFactor /
// FDKaacEnc_EstimateScaleFactors and the whole static helper chain underneath
// (CalcFormFactorChannel, calcSfbRelevantLines, countSingleScfBits,
// calcSingleSpecPe, countScfBitsDiff, calcSpecPeDiff, improveScf, the three
// assimilate passes, EstimateScaleFactorsChannel — libAACenc/src/sf_estim.cpp)
// plus the inline-header sqrtFixp / invSqrtNorm2 / FDKaacEnc_bitCountScalefactorDelta
// kernels — against the genuine vendored C, compiled into this test binary via
// cgo.
//
// sf_estim is the rate-control/quantizer driver stage that turns the adjusted
// per-sfb thresholds + energies (ld64 log domain) and the MDCT spectrum into the
// initial integer scalefactor per band, refines them by analysis-by-synthesis,
// and emits the loop scalefactors + global gain. Every value is an int32 FIXP_DBL
// Q-format / INT with carried block exponents; the whole result (the per-sfb scf
// array, the global gain, the quantized SHORT spectrum and the zeroed MDCT
// spectrum) is compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source (sf_estim.cpp
// + quantize.cpp for the dist/energy kernels + fixpoint_math.cpp for the LD math +
// aacEnc_rom.cpp for the quantizer/huffman ROM + FDK_tools_rom.cpp for the
// invSqrtTab) and NEVER imports libraries/aac — importing it would link a second
// copy of the FDK reference and clash on static symbols (the same amalgamation-
// split reason the sibling parity packages document). It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — these are pure INTEGER
// kernels (FIXP_DBL == int32, INT == int32). The leading-bit normalisation,
// arithmetic-shift block-floating-point accumulation, int64-product fixmul
// kernels, the ROM-table inverse-log2 / inverse-sqrt and the table-driven
// quantizer are bit-identical regardless of -ffp-contract / vectorization, with
// no transcendental and no float. So they assert EXACT int32 equality. The oracle
// links the genuine FDKaacEnc_CalcFormFactor / FDKaacEnc_EstimateScaleFactors /
// sqrtFixp / invSqrtNorm2 / FDKaacEnc_bitCountScalefactorDelta symbols
// (oracle_kind == real_vendored), reached through the thin extern shims in
// bridge.cpp — no hand-twin re-derivation.
package encsfestim

/*
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

extern int32_t sfe_sqrt_fixp(int32_t op);
extern int32_t sfe_inv_sqrt_norm2(int32_t op, int32_t *shift);
extern int sfe_bit_count_scf_delta(int delta);

extern void sfe_calc_form_factor(const int32_t *mdctSpectrum,
    const int *sfbOffsets, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int32_t *sfbFormFactorLdDataOut);

extern void sfe_estimate_scale_factors(int32_t *mdctSpectrum,
    const int32_t *sfbEnergyLdData, const int32_t *sfbThresholdLdData,
    const int32_t *sfbFormFactorLdData, const int *sfbOffsets, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup, int invQuant, int dZoneQuantEnable,
    int *scfOut, int *globalGainOut, int16_t *quantSpecOut);

extern void sfe_calc_form_factor_multi(int nChannels, const int32_t *mdct,
    const int *sfbOffsets, int sfbCnt, int sfbPerGroup, int maxSfbPerGroup,
    int32_t *sfbFormFactorLdDataOut);
extern void sfe_estimate_scale_factors_multi(int nChannels, int32_t *mdct,
    const int32_t *sfbEnergyLdData, const int32_t *sfbThresholdLdData,
    const int32_t *sfbFormFactorLdData, const int *sfbOffsets, int sfbCnt,
    int sfbPerGroup, int maxSfbPerGroup, int invQuant, int dZoneQuantEnable,
    int *scfOut, int *globalGainOut, int16_t *quantSpecOut);
*/
import "C"

import "unsafe"

const maxGroupedSFB = 60

func i32p(s []int32) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&s[0]))
}

// ip returns a *C.int over an int32 slice. Go's `int` is 64-bit on arm64 while C
// `int` is 32-bit, so every INT array crossing the cgo boundary is carried as
// int32 (the C type is int == 32-bit) and the *C.int aliases it bit-for-bit.
func ip(s []int32) *C.int {
	if len(s) == 0 {
		return nil
	}
	return (*C.int)(unsafe.Pointer(&s[0]))
}

// cSqrtFixp runs the genuine sqrtFixp.
func cSqrtFixp(op int32) int32 { return int32(C.sfe_sqrt_fixp(C.int32_t(op))) }

// cInvSqrtNorm2 runs the genuine invSqrtNorm2, returning (mantissa, shift).
func cInvSqrtNorm2(op int32) (int32, int32) {
	var s C.int32_t
	m := C.sfe_inv_sqrt_norm2(C.int32_t(op), &s)
	return int32(m), int32(s)
}

// cBitCountScfDelta runs the genuine FDKaacEnc_bitCountScalefactorDelta.
func cBitCountScfDelta(delta int) int { return int(C.sfe_bit_count_scf_delta(C.int(delta))) }

// cCalcFormFactor runs the genuine FDKaacEnc_CalcFormFactor and returns the
// per-sfb form factor (length maxGroupedSFB). sfbOffsets is an int32 array (C
// INT == 32-bit).
func cCalcFormFactor(mdctSpectrum []int32, sfbOffsets []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	out := make([]int32, maxGroupedSFB)
	C.sfe_calc_form_factor(i32p(mdctSpectrum), ip(sfbOffsets), C.int(sfbCnt),
		C.int(sfbPerGroup), C.int(maxSfbPerGroup), i32p(out))
	return out
}

// cEstimateScaleFactors runs the genuine FDKaacEnc_EstimateScaleFactors. The
// passed-in mdctSpectrum is mutated in place (empty bands zeroed). sfbOffsets is
// int32 (C INT == 32-bit); the returned scf is int32 for the same reason.
// Returns (scf, globalGain, quantSpec).
func cEstimateScaleFactors(mdctSpectrum []int32,
	sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32,
	sfbOffsets []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup, invQuant int,
	dZoneQuantEnable bool) (scf []int32, globalGain int, quantSpec []int16) {
	scf = make([]int32, maxGroupedSFB)
	quantSpec = make([]int16, 1024)
	var gg C.int
	dz := 0
	if dZoneQuantEnable {
		dz = 1
	}
	C.sfe_estimate_scale_factors(i32p(mdctSpectrum),
		i32p(sfbEnergyLdData), i32p(sfbThresholdLdData), i32p(sfbFormFactorLdData),
		ip(sfbOffsets), C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		C.int(invQuant), C.int(dz), ip(scf), &gg,
		(*C.int16_t)(unsafe.Pointer(&quantSpec[0])))
	return scf, int(gg), quantSpec
}

// cCalcFormFactorMulti runs the genuine FDKaacEnc_CalcFormFactor over nChannels
// and returns the per-channel form factor (length nChannels*maxGroupedSFB).
func cCalcFormFactorMulti(nChannels int, mdct []int32, sfbOffsets []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int) []int32 {
	out := make([]int32, nChannels*maxGroupedSFB)
	C.sfe_calc_form_factor_multi(C.int(nChannels), i32p(mdct), ip(sfbOffsets),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup), i32p(out))
	return out
}

// cEstimateScaleFactorsMulti runs the genuine FDKaacEnc_EstimateScaleFactors over
// nChannels. mdct is mutated in place. Returns per-channel scf
// (nChannels*maxGroupedSFB), globalGain (nChannels) and quantSpec (nChannels*1024).
func cEstimateScaleFactorsMulti(nChannels int, mdct []int32,
	sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32, sfbOffsets []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup, invQuant int, dZoneQuantEnable bool) (
	scf []int32, globalGain []int32, quantSpec []int16) {
	scf = make([]int32, nChannels*maxGroupedSFB)
	globalGain = make([]int32, nChannels)
	quantSpec = make([]int16, nChannels*1024)
	dz := 0
	if dZoneQuantEnable {
		dz = 1
	}
	C.sfe_estimate_scale_factors_multi(C.int(nChannels), i32p(mdct),
		i32p(sfbEnergyLdData), i32p(sfbThresholdLdData), i32p(sfbFormFactorLdData),
		ip(sfbOffsets), C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup),
		C.int(invQuant), C.int(dz), ip(scf), ip(globalGain),
		(*C.int16_t)(unsafe.Pointer(&quantSpec[0])))
	return scf, globalGain, quantSpec
}
