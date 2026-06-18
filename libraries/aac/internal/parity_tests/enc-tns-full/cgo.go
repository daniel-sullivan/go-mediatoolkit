// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enctnsfull pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE TNS DECISION driver — FDKaacEnc_TnsDetect
// (libAACenc/src/aacenc_tns.cpp) and its dependency chain
// (FDKaacEnc_MergedAutoCorrelation + the autocorrelation kernels, and
// CLpc_AutoToParcor from libFDK/FDK_lpc.cpp) — against the vendored C, compiled
// into this test binary via cgo.
//
// FDKaacEnc_TnsDetect is the encode-side TNS analysis/decision: it computes the
// quarter-split energy-normalised windowed autocorrelation over the MDCT
// spectrum, derives the higher (and, for long blocks, optionally a lower)
// reflection-coefficient lattice filter via the LeRoux-Gueguen/Schur recursion,
// quantizes the reflection coefficients to the on-wire 3/4-bit indices,
// truncates trailing zeros, applies the prediction-gain / sum-of-squares
// thresholds, and (for long blocks) optionally merges the two filters. The
// result is the TNS_INFO (order, direction, length, coef indices) +
// TNS_SUBBLOCK_INFO (tnsActive, predictionGain) + filtersMerged that feed the
// TNS leaf filter. Every value is an int32 FIXP_DBL / int16 FIXP_LPC Q-format
// quantity; the whole result is compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (aacenc_tns.cpp via bridge.cpp; aacEnc_rom.cpp for the ROM tables incl. the
// sfbWidth/TNS coefficient tables; FDK_lpc.cpp for CLpc_AutoToParcor;
// fixpoint_math.cpp + scale.cpp + FDK_tools_rom.cpp + genericStds.cpp for the
// math/ROM symbols aacenc_tns.cpp references) and NEVER imports libraries/aac —
// importing it would link a second copy of the FDK reference and clash on static
// symbols (the same amalgamation-split reason the sibling parity packages
// document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — the TNS decision is a pure
// INTEGER kernel (count-leading-bits, arithmetic shifts, schur division, the
// arm8 fixmul_DD, invSqrtNorm2) — bit-identical regardless of -ffp-contract /
// vectorization, with no transcendental and no float on the AAC-LC long-block
// path (the only float initializer, FDKaacEnc_CalcGaussWindow, is used solely
// for the 480/512 LD granule lengths, not exercised here; granuleLength 1024
// uses the integer ROM acfWindowLong). So it asserts EXACT integer equality. The
// oracle links the genuine FDKaacEnc_TnsDetect / CLpc_AutoToParcor symbols and
// reaches the static FDKaacEnc_MergedAutoCorrelation through the aacenc_tns.cpp
// #include (so the genuine static functions are the oracle, not a hand-twin).
// oracle_kind == real_vendored.
package enctnsfull

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-tns-full).
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

// Flat mirrors of the TNS_CONFIG / TNS_INFO POD the bridge exports. Field order
// must match bridge.cpp's tnsconf_flat / tnsinfo_flat exactly.
typedef struct {
    int32_t filterEnabled[2];
    int32_t threshOn[2];
    int32_t filterStartFreq[2];
    int32_t tnsLimitOrder[2];
    int32_t tnsFilterDirection[2];
    int32_t acfSplit[2];
    int32_t tnsTimeResolution[2];
    int32_t seperateFiltersAllowed;
    int32_t isLowDelay;
    int32_t tnsActive;
    int32_t maxOrder;
    int32_t coefRes;
    int32_t acfWindow[2][12 + 3 + 1];
    int32_t lpcStartBand[2];
    int32_t lpcStartLine[2];
    int32_t lpcStopBand;
    int32_t lpcStopLine;
} tnsconf_flat;

typedef struct {
    int32_t numOfFilters;
    int32_t coefRes;
    int32_t length[2];
    int32_t order[2];
    int32_t direction[2];
    int32_t coef[2][12];
    int32_t sbTnsActive[2];
    int32_t sbPredictionGain[2];
    int32_t filtersMerged;
} tnsinfo_flat;

extern int  tnsparity_build_config(int bitRate, int sampleRate, int channels, int active, tnsconf_flat *out);
extern int  tnsparity_detect(int bitRate, int sampleRate, int channels, int active, int sfbCnt, const int32_t *spectrum, tnsinfo_flat *out);
extern void tnsparity_autotoparcor(int32_t *acorr, int numOfCoeff, int16_t *reflCoeff, int32_t *predGainM, int32_t *predGainE);
extern void tnsparity_merged_autocorr(const int32_t *spectrum, int isLowDelay,
    const int32_t *acfWindow, const int32_t *lpcStartLine, int lpcStopLine,
    int maxOrder, const int32_t *acfSplit, int32_t *rxx1, int32_t *rxx2);
*/
import "C"

import "unsafe"

const tnsMaxOrder = 12
const acfWinSize = tnsMaxOrder + 3 + 1

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

// cConfig holds the flat config fields the bridge exported, in a Go-friendly
// shape so the test can seed an identical nativeaac.TNSConfig.
type cConfig struct {
	filterEnabled          [2]int32
	threshOn               [2]int32
	filterStartFreq        [2]int32
	tnsLimitOrder          [2]int32
	tnsFilterDirection     [2]int32
	acfSplit               [2]int32
	tnsTimeResolution      [2]int32
	seperateFiltersAllowed int32
	isLowDelay             int32
	tnsActive              int32
	maxOrder               int32
	coefRes                int32
	acfWindow              [2][acfWinSize]int32
	lpcStartBand           [2]int32
	lpcStartLine           [2]int32
	lpcStopBand            int32
	lpcStopLine            int32
}

// cTnsInfo holds the flat TNS_INFO + subblock result the bridge exported.
type cTnsInfo struct {
	numOfFilters     int32
	coefRes          int32
	length           [2]int32
	order            [2]int32
	direction        [2]int32
	coef             [2][tnsMaxOrder]int32
	sbTnsActive      [2]int32
	sbPredictionGain [2]int32
	filtersMerged    int32
}

// cBuildConfig runs the genuine FDKaacEnc_InitTnsConfiguration via the bridge,
// returning the resulting config and sfbActive (or <0 on failure).
func cBuildConfig(bitRate, sampleRate, channels, active int) (cConfig, int) {
	var flat C.tnsconf_flat
	rc := int(C.tnsparity_build_config(C.int(bitRate), C.int(sampleRate),
		C.int(channels), C.int(active), &flat))
	var c cConfig
	for f := 0; f < 2; f++ {
		c.filterEnabled[f] = int32(flat.filterEnabled[f])
		c.threshOn[f] = int32(flat.threshOn[f])
		c.filterStartFreq[f] = int32(flat.filterStartFreq[f])
		c.tnsLimitOrder[f] = int32(flat.tnsLimitOrder[f])
		c.tnsFilterDirection[f] = int32(flat.tnsFilterDirection[f])
		c.acfSplit[f] = int32(flat.acfSplit[f])
		c.tnsTimeResolution[f] = int32(flat.tnsTimeResolution[f])
		c.lpcStartBand[f] = int32(flat.lpcStartBand[f])
		c.lpcStartLine[f] = int32(flat.lpcStartLine[f])
		for i := 0; i < acfWinSize; i++ {
			c.acfWindow[f][i] = int32(flat.acfWindow[f][i])
		}
	}
	c.seperateFiltersAllowed = int32(flat.seperateFiltersAllowed)
	c.isLowDelay = int32(flat.isLowDelay)
	c.tnsActive = int32(flat.tnsActive)
	c.maxOrder = int32(flat.maxOrder)
	c.coefRes = int32(flat.coefRes)
	c.lpcStopBand = int32(flat.lpcStopBand)
	c.lpcStopLine = int32(flat.lpcStopLine)
	return c, rc
}

// cDetect runs the genuine FDKaacEnc_TnsDetect over the long-block spectrum.
func cDetect(bitRate, sampleRate, channels, active, sfbCnt int, spectrum []int32) (cTnsInfo, int) {
	var flat C.tnsinfo_flat
	rc := int(C.tnsparity_detect(C.int(bitRate), C.int(sampleRate),
		C.int(channels), C.int(active), C.int(sfbCnt), i32p(spectrum), &flat))
	var t cTnsInfo
	t.numOfFilters = int32(flat.numOfFilters)
	t.coefRes = int32(flat.coefRes)
	for f := 0; f < 2; f++ {
		t.length[f] = int32(flat.length[f])
		t.order[f] = int32(flat.order[f])
		t.direction[f] = int32(flat.direction[f])
		for k := 0; k < tnsMaxOrder; k++ {
			t.coef[f][k] = int32(flat.coef[f][k])
		}
		t.sbTnsActive[f] = int32(flat.sbTnsActive[f])
		t.sbPredictionGain[f] = int32(flat.sbPredictionGain[f])
	}
	t.filtersMerged = int32(flat.filtersMerged)
	return t, rc
}

// cAutoToParcor runs the genuine CLpc_AutoToParcor (acorr mutated in place).
func cAutoToParcor(acorr []int32, numOfCoeff int) (refl []int16, gainM, gainE int32) {
	refl = make([]int16, numOfCoeff)
	var gm, ge C.int32_t
	C.tnsparity_autotoparcor(i32p(acorr), C.int(numOfCoeff), i16p(refl), &gm, &ge)
	return refl, int32(gm), int32(ge)
}

// cMergedAutoCorr runs the genuine static FDKaacEnc_MergedAutoCorrelation.
// acfWindow is the flat [2][acfWinSize] window; rxx1/rxx2 length tnsMaxOrder+1.
func cMergedAutoCorr(spectrum []int32, isLowDelay int, acfWindow []int32,
	lpcStartLine []int32, lpcStopLine, maxOrder int, acfSplit []int32) (rxx1, rxx2 []int32) {
	rxx1 = make([]int32, tnsMaxOrder+1)
	rxx2 = make([]int32, tnsMaxOrder+1)
	C.tnsparity_merged_autocorr(i32p(spectrum), C.int(isLowDelay), i32p(acfWindow),
		i32p(lpcStartLine), C.int(lpcStopLine), C.int(maxOrder), i32p(acfSplit),
		i32p(rxx1), i32p(rxx2))
	return rxx1, rxx2
}
