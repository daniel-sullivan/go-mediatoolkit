// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enc_intensity pins the Go port of the Fraunhofer FDK-AAC encoder
// intensity-stereo processing tool (nativeaac.IntensityStereoProcessing) against
// the genuine vendored libAACenc/src/intensity.cpp
// FDKaacEnc_IntensityStereoProcessing kernel, compiled into this test binary via
// cgo. For a range of fabricated (energy, mdct spectrum, threshold, ld-data,
// sfb-layout, allowIS, pns-present) configs the C kernel fills the modified
// spectrum/energies/thresholds + isBook/isScale/msMask/msDigest/pnsFlag and the
// bridge copies every field out flat; the Go port is compared bit-for-bit (raw
// int32 / int) against it.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (intensity.cpp + fixpoint_math.cpp + FDK_tools_rom.cpp + genericStds.cpp +
// scale.cpp, one go-test binary per package) and NEVER imports libraries/aac —
// importing it would link a second copy of the whole FDK reference and clash on
// static symbols. It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
package enc_intensity

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

extern void eparity_intensity_stereo_processing(
    const int32_t *sfbEnergyLeftIn, const int32_t *sfbEnergyRightIn,
    const int32_t *mdctSpectrumLeftIn, const int32_t *mdctSpectrumRightIn,
    const int32_t *sfbThresholdLeftIn, const int32_t *sfbThresholdRightIn,
    const int32_t *sfbThresholdLdDataRightIn, const int32_t *sfbSpreadEnLeftIn,
    const int32_t *sfbSpreadEnRightIn, const int32_t *sfbEnergyLdDataLeftIn,
    const int32_t *sfbEnergyLdDataRightIn, int msDigestIn, const int *msMaskIn,
    int sfbCnt, int sfbPerGroup, int maxSfbPerGroup, const int *sfbOffset,
    int allowIS, int pnsPresent, const int *pnsFlagLIn, const int *pnsFlagRIn,
    int specLen,
    int32_t *mdctSpectrumLeftOut, int32_t *mdctSpectrumRightOut,
    int32_t *sfbEnergyRightOut, int32_t *sfbThresholdRightOut,
    int32_t *sfbThresholdLdDataRightOut, int32_t *sfbSpreadEnRightOut,
    int *isBookOut, int *isScaleOut, int *msMaskOut, int *msDigestOut,
    int *pnsFlagLOut, int *pnsFlagROut);
*/
import "C"

const epMaxGroupedSfb = 60

// isResult holds the modified outputs of one IntensityStereoProcessing call.
type isResult struct {
	mdctLeft       []int32 // mutated left spectrum (specLen)
	mdctRight      []int32 // mutated right spectrum (specLen)
	sfbEnergyRight []int32 // zeroed where IS (maxGroupedSfb)
	sfbThrRight    []int32 // zeroed where IS
	sfbThrLdRight  []int32 // -0.515625 where IS
	sfbSpreadRight []int32 // zeroed where IS
	isBook         []int32
	isScale        []int32
	msMask         []int32
	msDigest       int
	pnsFlagL       []int32
	pnsFlagR       []int32
}

// cIntensityStereoProcessing runs the genuine FDKaacEnc_IntensityStereoProcessing
// and returns its modified outputs.
func cIntensityStereoProcessing(
	sfbEnergyLeft, sfbEnergyRight []int32,
	mdctLeft, mdctRight []int32,
	sfbThrLeft, sfbThrRight []int32,
	sfbThrLdRight []int32,
	sfbSpreadLeft, sfbSpreadRight []int32,
	sfbEnergyLdLeft, sfbEnergyLdRight []int32,
	msDigest int, msMask []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int, sfbOffset []int32, allowIS int,
	pnsPresent int, pnsFlagL, pnsFlagR []int32) isResult {

	specLen := len(mdctLeft)

	cEnL := i32slice(sfbEnergyLeft)
	cEnR := i32slice(sfbEnergyRight)
	cMdL := i32slice(mdctLeft)
	cMdR := i32slice(mdctRight)
	cThrL := i32slice(sfbThrLeft)
	cThrR := i32slice(sfbThrRight)
	cThrLdR := i32slice(sfbThrLdRight)
	cSprL := i32slice(sfbSpreadLeft)
	cSprR := i32slice(sfbSpreadRight)
	cEnLdL := i32slice(sfbEnergyLdLeft)
	cEnLdR := i32slice(sfbEnergyLdRight)
	cMsMask := islice32(msMask)
	cOff := islice32(sfbOffset)
	cPnsL := islice32(pnsFlagL)
	cPnsR := islice32(pnsFlagR)

	mdLOut := make([]C.int32_t, specLen)
	mdROut := make([]C.int32_t, specLen)
	enROut := make([]C.int32_t, epMaxGroupedSfb)
	thrROut := make([]C.int32_t, epMaxGroupedSfb)
	thrLdROut := make([]C.int32_t, epMaxGroupedSfb)
	sprROut := make([]C.int32_t, epMaxGroupedSfb)
	isBookOut := make([]C.int, epMaxGroupedSfb)
	isScaleOut := make([]C.int, epMaxGroupedSfb)
	msMaskOut := make([]C.int, epMaxGroupedSfb)
	pnsLOut := make([]C.int, epMaxGroupedSfb)
	pnsROut := make([]C.int, epMaxGroupedSfb)
	var dig C.int

	C.eparity_intensity_stereo_processing(
		ptr32(cEnL), ptr32(cEnR), ptr32(cMdL), ptr32(cMdR),
		ptr32(cThrL), ptr32(cThrR), ptr32(cThrLdR), ptr32(cSprL), ptr32(cSprR),
		ptr32(cEnLdL), ptr32(cEnLdR), C.int(msDigest), ptrInt(cMsMask),
		C.int(sfbCnt), C.int(sfbPerGroup), C.int(maxSfbPerGroup), ptrInt(cOff),
		C.int(allowIS), C.int(pnsPresent), ptrInt(cPnsL), ptrInt(cPnsR),
		C.int(specLen),
		&mdLOut[0], &mdROut[0], &enROut[0], &thrROut[0], &thrLdROut[0],
		&sprROut[0], &isBookOut[0], &isScaleOut[0], &msMaskOut[0], &dig,
		&pnsLOut[0], &pnsROut[0])

	r := isResult{
		mdctLeft:       make([]int32, specLen),
		mdctRight:      make([]int32, specLen),
		sfbEnergyRight: make([]int32, epMaxGroupedSfb),
		sfbThrRight:    make([]int32, epMaxGroupedSfb),
		sfbThrLdRight:  make([]int32, epMaxGroupedSfb),
		sfbSpreadRight: make([]int32, epMaxGroupedSfb),
		isBook:         make([]int32, epMaxGroupedSfb),
		isScale:        make([]int32, epMaxGroupedSfb),
		msMask:         make([]int32, epMaxGroupedSfb),
		pnsFlagL:       make([]int32, epMaxGroupedSfb),
		pnsFlagR:       make([]int32, epMaxGroupedSfb),
		msDigest:       int(dig),
	}
	for i := 0; i < specLen; i++ {
		r.mdctLeft[i] = int32(mdLOut[i])
		r.mdctRight[i] = int32(mdROut[i])
	}
	for i := 0; i < epMaxGroupedSfb; i++ {
		r.sfbEnergyRight[i] = int32(enROut[i])
		r.sfbThrRight[i] = int32(thrROut[i])
		r.sfbThrLdRight[i] = int32(thrLdROut[i])
		r.sfbSpreadRight[i] = int32(sprROut[i])
		r.isBook[i] = int32(isBookOut[i])
		r.isScale[i] = int32(isScaleOut[i])
		r.msMask[i] = int32(msMaskOut[i])
		r.pnsFlagL[i] = int32(pnsLOut[i])
		r.pnsFlagR[i] = int32(pnsROut[i])
	}
	return r
}

// ---- small slice->C conversion helpers (kept local to this test package) ----

func i32slice(s []int32) []C.int32_t {
	out := make([]C.int32_t, len(s))
	for i, v := range s {
		out[i] = C.int32_t(v)
	}
	return out
}

func islice32(s []int32) []C.int {
	out := make([]C.int, len(s))
	for i, v := range s {
		out[i] = C.int(v)
	}
	return out
}

func ptr32(s []C.int32_t) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}

func ptrInt(s []C.int) *C.int {
	if len(s) == 0 {
		return nil
	}
	return &s[0]
}
