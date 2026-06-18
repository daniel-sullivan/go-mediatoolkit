// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encpsymodel pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE psychoacoustic band/line-energy kernels — FDKaacEnc_CalcSfbMaxScaleSpec
// / FDKaacEnc_CheckBandEnergyOptim / FDKaacEnc_CalcBandEnergyOptimLong /
// ...Short / FDKaacEnc_CalcBandNrgMSOpt (libAACenc/src/band_nrg.cpp), plus the
// LD-domain log2 helpers CalcLdData / LdDataVector (libFDK fixpoint_math) they
// invoke — against the vendored C, compiled into this test binary via cgo.
//
// These kernels are the SFB-energy core of the psy model (psy_main.cpp): they
// turn the windowed MDCT spectrum into the per-SFB energies, their log2/ldData
// form, and the M/S mid/side energies that the masking-threshold computation and
// the quantizer ultimately target. Every value is an int32 FIXP_DBL Q-format
// with a carried per-block exponent (sfbMaxScaleSpec); the whole result is
// compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (band_nrg.cpp + fixpoint_math.cpp) and NEVER imports libraries/aac — importing
// it would link a second copy of the FDK reference and clash on static symbols
// (the same amalgamation-split reason the sibling parity packages document). It
// MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: these are pure INTEGER fixed-point kernels (FIXP_DBL == int32). The
// leading-bit counts, arithmetic-shift block-floating-point energy
// accumulation, the int32 fixmul int64-product>>32 kernels, and the
// table-driven fLog2 are bit-identical regardless of -ffp-contract /
// vectorization, with no transcendental. So they assert EXACT int32 equality
// unconditionally. The oracle is the genuine FDKaacEnc_* / LdDataVector /
// CalcLdData symbols (oracle_kind == real_vendored).
package encpsymodel

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-psy-model).
// band_nrg.cpp pulls band_nrg.h -> common_fix.h; fixpoint_math.cpp pulls
// fixpoint_math.h (libFDK + libSYS).
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
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void    eparity_calc_sfb_max_scale_spec(const int32_t *mdctSpectrum,
                   const int32_t *bandOffset, int32_t *sfbMaxScaleSpec, int numBands);
extern int32_t eparity_check_band_energy_optim(const int32_t *mdctSpectrum,
                   const int32_t *sfbMaxScaleSpec, const int32_t *bandOffset, int numBands,
                   int32_t *bandEnergy, int32_t *bandEnergyLdData, int minSpecShift);
extern int     eparity_calc_band_energy_optim_long(const int32_t *mdctSpectrum,
                   int32_t *sfbMaxScaleSpec, const int32_t *bandOffset, int numBands,
                   int32_t *bandEnergy, int32_t *bandEnergyLdData);
extern void    eparity_calc_band_energy_optim_short(const int32_t *mdctSpectrum,
                   int32_t *sfbMaxScaleSpec, const int32_t *bandOffset, int numBands,
                   int32_t *bandEnergy);
extern void    eparity_calc_band_nrg_ms_opt(const int32_t *mdctSpectrumLeft,
                   const int32_t *mdctSpectrumRight, int32_t *sfbMaxScaleSpecLeft,
                   int32_t *sfbMaxScaleSpecRight, const int32_t *bandOffset, int numBands,
                   int32_t *bandEnergyMid, int32_t *bandEnergySide, int calcLdData,
                   int32_t *bandEnergyMidLdData, int32_t *bandEnergySideLdData);
extern int32_t eparity_calc_ld_data(int32_t op);
extern void    eparity_ld_data_vector(const int32_t *src, int32_t *dst, int n);
extern void    eparity_ld_consts(int32_t *out);
extern void    eparity_ld_coeff(int16_t *out);
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

// toI32 converts a []int (band offsets / sfb max-scale spec) to []int32 for the
// C side, which uses INT == int32.
func toI32(s []int) []int32 {
	o := make([]int32, len(s))
	for i, v := range s {
		o[i] = int32(v)
	}
	return o
}

// cCalcSfbMaxScaleSpec runs the genuine FDKaacEnc_CalcSfbMaxScaleSpec, returning
// the per-band headroom as []int.
func cCalcSfbMaxScaleSpec(mdctSpectrum []int32, bandOffset []int, numBands int) []int {
	bo := toI32(bandOffset)
	out := make([]int32, numBands)
	C.eparity_calc_sfb_max_scale_spec(i32p(mdctSpectrum), i32p(bo), i32p(out), C.int(numBands))
	r := make([]int, numBands)
	for i, v := range out {
		r[i] = int(v)
	}
	return r
}

// cCheckBandEnergyOptim runs the genuine FDKaacEnc_CheckBandEnergyOptim,
// returning (maxNrg, bandEnergy, bandEnergyLdData).
func cCheckBandEnergyOptim(mdctSpectrum []int32, sfbMaxScaleSpec, bandOffset []int,
	numBands, minSpecShift int) (int32, []int32, []int32) {
	ms := toI32(sfbMaxScaleSpec)
	bo := toI32(bandOffset)
	be := make([]int32, numBands)
	beLd := make([]int32, numBands)
	r := C.eparity_check_band_energy_optim(i32p(mdctSpectrum), i32p(ms), i32p(bo),
		C.int(numBands), i32p(be), i32p(beLd), C.int(minSpecShift))
	return int32(r), be, beLd
}

// cCalcBandEnergyOptimLong runs the genuine FDKaacEnc_CalcBandEnergyOptimLong,
// returning (shiftBits, bandEnergy, bandEnergyLdData).
func cCalcBandEnergyOptimLong(mdctSpectrum []int32, sfbMaxScaleSpec, bandOffset []int,
	numBands int) (int, []int32, []int32) {
	ms := toI32(sfbMaxScaleSpec)
	bo := toI32(bandOffset)
	be := make([]int32, numBands)
	beLd := make([]int32, numBands)
	r := C.eparity_calc_band_energy_optim_long(i32p(mdctSpectrum), i32p(ms), i32p(bo),
		C.int(numBands), i32p(be), i32p(beLd))
	return int(r), be, beLd
}

// cCalcBandEnergyOptimShort runs the genuine FDKaacEnc_CalcBandEnergyOptimShort,
// returning bandEnergy.
func cCalcBandEnergyOptimShort(mdctSpectrum []int32, sfbMaxScaleSpec, bandOffset []int,
	numBands int) []int32 {
	ms := toI32(sfbMaxScaleSpec)
	bo := toI32(bandOffset)
	be := make([]int32, numBands)
	C.eparity_calc_band_energy_optim_short(i32p(mdctSpectrum), i32p(ms), i32p(bo),
		C.int(numBands), i32p(be))
	return be
}

// cCalcBandNrgMSOpt runs the genuine FDKaacEnc_CalcBandNrgMSOpt, returning
// (mid, side, midLd, sideLd).
func cCalcBandNrgMSOpt(mdctL, mdctR []int32, msLeft, msRight, bandOffset []int,
	numBands, calcLdData int) (mid, side, midLd, sideLd []int32) {
	msl := toI32(msLeft)
	msr := toI32(msRight)
	bo := toI32(bandOffset)
	mid = make([]int32, numBands)
	side = make([]int32, numBands)
	midLd = make([]int32, numBands)
	sideLd = make([]int32, numBands)
	C.eparity_calc_band_nrg_ms_opt(i32p(mdctL), i32p(mdctR), i32p(msl), i32p(msr),
		i32p(bo), C.int(numBands), i32p(mid), i32p(side), C.int(calcLdData),
		i32p(midLd), i32p(sideLd))
	return
}

// cCalcLdData runs the genuine CalcLdData(op).
func cCalcLdData(op int32) int32 { return int32(C.eparity_calc_ld_data(C.int32_t(op))) }

// cLdDataVector runs the genuine LdDataVector over n entries of src.
func cLdDataVector(src []int32, n int) []int32 {
	dst := make([]int32, n)
	C.eparity_ld_data_vector(i32p(src), i32p(dst), C.int(n))
	return dst
}

// cLdConsts returns the four FL2FXCONST_DBL constants the Go port embeds.
func cLdConsts() [4]int32 {
	var out [4]C.int32_t
	C.eparity_ld_consts(&out[0])
	var r [4]int32
	for i := 0; i < 4; i++ {
		r[i] = int32(out[i])
	}
	return r
}

// cLdCoeff returns the genuine ldCoeff[10] ROM (FIXP_SGL int16 on aarch64).
func cLdCoeff() [10]int16 {
	var out [10]C.int16_t
	C.eparity_ld_coeff(&out[0])
	var r [10]int16
	for i := 0; i < 10; i++ {
		r[i] = int16(out[i])
	}
	return r
}
