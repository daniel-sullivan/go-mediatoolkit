// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enctnsfinish pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE "TNS finish" batch — FDKaacEnc_InitTnsConfiguration, FDKaacEnc_TnsSync,
// FDKaacEnc_TnsEncode (libAACenc/src/aacenc_tns.cpp) and the LPC lattice helpers
// they call, CLpc_ParcorToLpc / CLpc_Analysis (libFDK/src/FDK_lpc.cpp) — against
// the vendored C, compiled into this test binary via cgo.
//
// FDKaacEnc_InitTnsConfiguration fills a TNS_CONFIG (filter orders, thresholds,
// LPC start/stop bands+lines, the integer autocorrelation window ROM) for an
// AAC-LC long block. FDKaacEnc_TnsEncode dequantizes the decision's coefficient
// indices to ParCor (Index2Parcor), converts ParCor -> direct-form LPC
// (CLpc_ParcorToLpc), then runs the FIR analysis filter (CLpc_Analysis) to
// rewrite the MDCT spectrum in place. FDKaacEnc_TnsSync synchronises the higher
// filter between a channel pair. Every value is int32 FIXP_DBL / int16 FIXP_LPC
// Q-format; the whole result is compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C (aacenc_tns.cpp via
// bridge.cpp — which also reaches the genuine static helpers + FDKaacEnc_TnsDetect
// used to MANUFACTURE a real TNS_INFO/TNS_DATA input for encode/sync; aacEnc_rom.cpp;
// FDK_lpc.cpp; fixpoint_math.cpp + scale.cpp + FDK_tools_rom.cpp + genericStds.cpp)
// and NEVER imports libraries/aac (which would link a second copy of the FDK
// reference and clash on static symbols). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island is fenced behind aacfdk, so a default `go build ./...`
// links none of it. Integer parity: ENCODE is FIXED-POINT; the AAC-LC long-block
// TNS finish path is pure integer arithmetic (no FDKaacEnc_CalcGaussWindow, which
// is LD-only; granuleLength 1024 uses the integer ROM acfWindowLong), so the
// oracle asserts EXACT integer equality. oracle_kind == real_vendored.
package enctnsfinish

/*
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

// Field order must match bridge.cpp's tnsfin_conf_flat / tnsfin_info_flat.
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
    int32_t initRc;
    int32_t sfbCnt;
    int32_t sfbActive;
    int32_t sfbOffset[51 + 1];
} tnsfin_conf_flat;

typedef struct {
    int32_t numOfFilters;
    int32_t coefRes;
    int32_t length[2];
    int32_t order[2];
    int32_t direction[2];
    int32_t coefCompress[2];
    int32_t coef[2][12];
    int32_t sbTnsActive[2];
    int32_t sbPredictionGain[2];
    int32_t filtersMerged;
} tnsfin_info_flat;

extern int  tnsfin_build_config(int bitRate, int sampleRate, int channels, int active, tnsfin_conf_flat *out);
extern int  tnsfin_detect(int bitRate, int sampleRate, int channels, int active, int sfbCnt, const int32_t *spectrum, tnsfin_info_flat *out);
extern int  tnsfin_encode(int bitRate, int sampleRate, int channels, int active, int sfbCnt, int32_t *spectrum);
extern void tnsfin_sync(int maxOrder, const tnsfin_info_flat *destIn, const tnsfin_info_flat *srcIn, tnsfin_info_flat *destOut);
extern int  tnsfin_get_max_bands(int sampleRate, int granuleLength, int isShortBlock);
extern int  tnsfin_parcor2lpc(const int16_t *reflCoeff, int16_t *lpcCoeff, int numOfCoeff, int32_t *workBuffer);
extern void tnsfin_analysis(int32_t *signal, int signalSize, const int16_t *lpcCoeff, int lpcCoeffE, int order, int32_t *filtState);
*/
import "C"

import "unsafe"

const tnsMaxOrder = 12
const acfWinSize = tnsMaxOrder + 3 + 1

func i32p(s []int32) *C.int32_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int32_t)(unsafe.Pointer(&s[0]))
}

func i16p(s []int16) *C.int16_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int16_t)(unsafe.Pointer(&s[0]))
}

// cConfig mirrors the flat TNS_CONFIG fields plus the init return code.
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
	initRc                 int32
	sfbCnt                 int32
	sfbActive              int32
	sfbOffset              [52]int32
}

// cInfo mirrors the flat (long) TNS_INFO + subblock result.
type cInfo struct {
	numOfFilters     int32
	coefRes          int32
	length           [2]int32
	order            [2]int32
	direction        [2]int32
	coefCompress     [2]int32
	coef             [2][tnsMaxOrder]int32
	sbTnsActive      [2]int32
	sbPredictionGain [2]int32
	filtersMerged    int32
}

func confFromFlat(flat *C.tnsfin_conf_flat) cConfig {
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
	c.initRc = int32(flat.initRc)
	c.sfbCnt = int32(flat.sfbCnt)
	c.sfbActive = int32(flat.sfbActive)
	for i := 0; i < 52; i++ {
		c.sfbOffset[i] = int32(flat.sfbOffset[i])
	}
	return c
}

func infoFromFlat(flat *C.tnsfin_info_flat) cInfo {
	var t cInfo
	t.numOfFilters = int32(flat.numOfFilters)
	t.coefRes = int32(flat.coefRes)
	for f := 0; f < 2; f++ {
		t.length[f] = int32(flat.length[f])
		t.order[f] = int32(flat.order[f])
		t.direction[f] = int32(flat.direction[f])
		t.coefCompress[f] = int32(flat.coefCompress[f])
		for k := 0; k < tnsMaxOrder; k++ {
			t.coef[f][k] = int32(flat.coef[f][k])
		}
		t.sbTnsActive[f] = int32(flat.sbTnsActive[f])
		t.sbPredictionGain[f] = int32(flat.sbPredictionGain[f])
	}
	t.filtersMerged = int32(flat.filtersMerged)
	return t
}

func infoToFlat(t cInfo) C.tnsfin_info_flat {
	var flat C.tnsfin_info_flat
	flat.numOfFilters = C.int32_t(t.numOfFilters)
	flat.coefRes = C.int32_t(t.coefRes)
	for f := 0; f < 2; f++ {
		flat.length[f] = C.int32_t(t.length[f])
		flat.order[f] = C.int32_t(t.order[f])
		flat.direction[f] = C.int32_t(t.direction[f])
		flat.coefCompress[f] = C.int32_t(t.coefCompress[f])
		for k := 0; k < tnsMaxOrder; k++ {
			flat.coef[f][k] = C.int32_t(t.coef[f][k])
		}
		flat.sbTnsActive[f] = C.int32_t(t.sbTnsActive[f])
		flat.sbPredictionGain[f] = C.int32_t(t.sbPredictionGain[f])
	}
	flat.filtersMerged = C.int32_t(t.filtersMerged)
	return flat
}

// cBuildConfig runs the genuine FDKaacEnc_InitTnsConfiguration.
func cBuildConfig(bitRate, sampleRate, channels, active int) (cConfig, int) {
	var flat C.tnsfin_conf_flat
	rc := int(C.tnsfin_build_config(C.int(bitRate), C.int(sampleRate),
		C.int(channels), C.int(active), &flat))
	return confFromFlat(&flat), rc
}

// cDetect runs the genuine decision and returns the TNS_INFO it produced.
func cDetect(bitRate, sampleRate, channels, active, sfbCnt int, spectrum []int32) (cInfo, int) {
	var flat C.tnsfin_info_flat
	rc := int(C.tnsfin_detect(C.int(bitRate), C.int(sampleRate), C.int(channels),
		C.int(active), C.int(sfbCnt), i32p(spectrum), &flat))
	return infoFromFlat(&flat), rc
}

// cEncode runs the genuine decision + FDKaacEnc_TnsEncode, mutating spectrum.
func cEncode(bitRate, sampleRate, channels, active, sfbCnt int, spectrum []int32) int {
	return int(C.tnsfin_encode(C.int(bitRate), C.int(sampleRate), C.int(channels),
		C.int(active), C.int(sfbCnt), i32p(spectrum)))
}

// cSync runs the genuine FDKaacEnc_TnsSync over the given dest/src info.
func cSync(maxOrder int, dest, src cInfo) cInfo {
	destFlat := infoToFlat(dest)
	srcFlat := infoToFlat(src)
	var out C.tnsfin_info_flat
	C.tnsfin_sync(C.int(maxOrder), &destFlat, &srcFlat, &out)
	return infoFromFlat(&out)
}

// cParcorToLpc runs the genuine CLpc_ParcorToLpc (lpcCoeff/workBuffer mutated).
func cParcorToLpc(reflCoeff []int16, numOfCoeff int) (lpcCoeff []int16, workBuffer []int32, expo int) {
	lpcCoeff = make([]int16, numOfCoeff)
	workBuffer = make([]int32, numOfCoeff)
	expo = int(C.tnsfin_parcor2lpc(i16p(reflCoeff), i16p(lpcCoeff),
		C.int(numOfCoeff), i32p(workBuffer)))
	return
}

// cAnalysis runs the genuine CLpc_Analysis (signal/filtState mutated in place).
func cAnalysis(signal []int32, lpcCoeff []int16, lpcCoeffE, order int, filtState []int32) {
	C.tnsfin_analysis(i32p(signal), C.int(len(signal)), i16p(lpcCoeff),
		C.int(lpcCoeffE), C.int(order), i32p(filtState))
}

// cGetMaxBands runs the genuine static getTnsMaxBands.
func cGetMaxBands(sampleRate, granuleLength, isShortBlock int) int {
	return int(C.tnsfin_get_max_bands(C.int(sampleRate), C.int(granuleLength), C.int(isShortBlock)))
}
