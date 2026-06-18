// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encstereotns pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE stereo+TNS stage — FDKaacEnc_MsStereoProcessing
// (libAACenc/src/ms_stereo.cpp) and the static TNS-encode reflection-coefficient
// quantizers FDKaacEnc_Parcor2Index / FDKaacEnc_Index2Parcor
// (libAACenc/src/aacEnc_tns.cpp) — against the vendored C, compiled into this
// test binary via cgo.
//
// FDKaacEnc_MsStereoProcessing is the M/S stereo DECISION on the encode side:
// for every non-intensity scalefactor band it compares the ld-domain perceptual
// cost of L/R vs mid/side coding, and where M/S wins rewrites the L/R MDCT
// spectrum in place to mid/side, copies the mid/side energies + min threshold
// into the L/R slots, sets msMask, and folds the result into the frame-level
// msDigest (SI_MS_MASK_NONE/SOME/ALL). The TNS quantizers turn the
// LeRoux-Gueguen ParCor (reflection) coefficients into the on-wire 3/4-bit TNS
// coefficient indices and back. Every value is an int32 FIXP_DBL / int16
// FIXP_SGL Q-format; the whole result is compared element-for-element,
// bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (ms_stereo.cpp + aacEnc_tns.cpp via bridge.cpp; aacEnc_rom.cpp for the ROM
// tables; fixpoint_math.cpp + FDK_lpc.cpp + the libFDK/libSYS support TUs for the
// symbols aacEnc_tns.cpp references) and NEVER imports libraries/aac — importing
// it would link a second copy of the FDK reference and clash on static symbols
// (the same amalgamation-split reason the sibling parity packages document). It
// MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — these are pure INTEGER kernels
// (FIXP_DBL == int32, FIXP_SGL == int16). ms_stereo is fixMin/fixMax +
// arithmetic shifts; the TNS quantizers are integer border comparisons + table
// indexing — bit-identical regardless of -ffp-contract / vectorization, with no
// transcendental and no float. So they assert EXACT integer equality. The oracle
// links the genuine FDKaacEnc_MsStereoProcessing symbol and reaches the static
// FDKaacEnc_Parcor2Index / FDKaacEnc_Index2Parcor through extern "C" shims
// compiled INSIDE aacEnc_tns.cpp's own TU via #include (so the genuine static
// functions are the oracle, not a hand-twin). oracle_kind == real_vendored.
package encstereotns

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-stereo-tns).
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

extern int  msparity_ms_stereo(
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, int allowMS,
    const int32_t *isBook,
    int32_t *sfbEnergyLeft, int32_t *sfbEnergyRight,
    const int32_t *sfbEnergyMid, const int32_t *sfbEnergySide,
    int32_t *sfbThresholdLeft, int32_t *sfbThresholdRight,
    int32_t *sfbSpreadEnLeft, int32_t *sfbSpreadEnRight,
    int32_t *sfbEnergyLeftLd, int32_t *sfbEnergyRightLd,
    const int32_t *sfbEnergyMidLd, const int32_t *sfbEnergySideLd,
    int32_t *sfbThresholdLeftLd, int32_t *sfbThresholdRightLd,
    int32_t *mdctSpectrumLeft, int32_t *mdctSpectrumRight,
    const int32_t *sfbOffset, int32_t *msMask);
extern void msparity_parcor2index(const int16_t *parcor, int32_t *index, int order, int bitsPerCoeff);
extern void msparity_index2parcor(const int32_t *index, int16_t *parcor, int order, int bitsPerCoeff);
extern void msparity_tns_rom(int16_t *encCoeff3, int16_t *coeff3Borders, int16_t *encCoeff4, int16_t *coeff4Borders);
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

// msIO bundles the io arrays for the M/S oracle so the test can seed them, run
// the genuine C, and compare every mutated array element-for-element. Field
// order/names mirror nativeaac.MsStereoArrays.
type msIO struct {
	sfbEnergyLeft, sfbEnergyRight           []int32
	sfbEnergyMid, sfbEnergySide             []int32
	sfbThresholdLeft, sfbThresholdRight     []int32
	sfbSpreadEnLeft, sfbSpreadEnRight       []int32
	sfbEnergyLeftLd, sfbEnergyRightLd       []int32
	sfbEnergyMidLd, sfbEnergySideLd         []int32
	sfbThresholdLeftLd, sfbThresholdRightLd []int32
	mdctSpectrumLeft, mdctSpectrumRight     []int32
	msMask                                  []int32
}

// cMsStereo runs the genuine FDKaacEnc_MsStereoProcessing over the io arrays in
// `io` (mutated in place) and returns msDigest. isBook may be nil.
func cMsStereo(io *msIO, isBook, sfbOffset []int32, allowMS, sfbCnt, sfbPerGroup, maxSfbPerGroup int) int {
	return int(C.msparity_ms_stereo(
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup), C.int(allowMS),
		i32p(isBook),
		i32p(io.sfbEnergyLeft), i32p(io.sfbEnergyRight),
		i32p(io.sfbEnergyMid), i32p(io.sfbEnergySide),
		i32p(io.sfbThresholdLeft), i32p(io.sfbThresholdRight),
		i32p(io.sfbSpreadEnLeft), i32p(io.sfbSpreadEnRight),
		i32p(io.sfbEnergyLeftLd), i32p(io.sfbEnergyRightLd),
		i32p(io.sfbEnergyMidLd), i32p(io.sfbEnergySideLd),
		i32p(io.sfbThresholdLeftLd), i32p(io.sfbThresholdRightLd),
		i32p(io.mdctSpectrumLeft), i32p(io.mdctSpectrumRight),
		i32p(sfbOffset), i32p(io.msMask)))
}

// cParcor2Index runs the genuine static FDKaacEnc_Parcor2Index.
func cParcor2Index(parcor []int16, order, bitsPerCoeff int) []int32 {
	out := make([]int32, order)
	C.msparity_parcor2index(i16p(parcor), i32p(out), C.int(order), C.int(bitsPerCoeff))
	return out
}

// cIndex2Parcor runs the genuine static FDKaacEnc_Index2Parcor.
func cIndex2Parcor(index []int32, order, bitsPerCoeff int) []int16 {
	out := make([]int16, order)
	C.msparity_index2parcor(i32p(index), i16p(out), C.int(order), C.int(bitsPerCoeff))
	return out
}

// cTnsRom returns the genuine vendored TNS-encode ROM tables so the test
// verifies the Go int16-narrowed transcription bit-for-bit.
func cTnsRom() (encCoeff3, coeff3Borders, encCoeff4, coeff4Borders []int16) {
	encCoeff3 = make([]int16, 8)
	coeff3Borders = make([]int16, 8)
	encCoeff4 = make([]int16, 16)
	coeff4Borders = make([]int16, 16)
	C.msparity_tns_rom(i16p(encCoeff3), i16p(coeff3Borders), i16p(encCoeff4), i16p(coeff4Borders))
	return
}
