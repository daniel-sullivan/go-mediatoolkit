// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enc_psy_config pins the Go port of the Fraunhofer FDK-AAC encoder
// psychoacoustic CONFIGURATION init (nativeaac.InitPsyConfiguration) against the
// genuine vendored libAACenc/src/psy_configuration.cpp kernel
// FDKaacEnc_InitPsyConfiguration, compiled into this test binary via cgo. For a
// range of (bitrate, samplerate, bandwidth, blocktype, granuleLength,
// useIS/useMS, filterbank) parameter tuples the C kernel fills a
// PSY_CONFIGURATION and the bridge copies every field out flat; the Go port is
// compared bit-for-bit (raw int32 / int16) against it.
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (psy_configuration.cpp + aacEnc_rom.cpp + FDK_trigFcts.cpp + fixpoint_math.cpp
// + FDK_tools_rom.cpp + genericStds.cpp, one go-test binary per package) and
// NEVER imports libraries/aac — importing it would link a second copy of the
// whole FDK reference and clash on static symbols. It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
package enc_psy_config

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
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

#define EP_MAX_SFB 51 // MAX_SFB (psy_const.h:149)

typedef struct {
  int32_t sfbCnt;
  int32_t sfbActive;
  int32_t sfbActiveLFE;
  int32_t sfbOffset[EP_MAX_SFB + 1];

  int32_t filterbank;

  int32_t sfbPcmQuantThreshold[EP_MAX_SFB];

  int32_t maxAllowedIncreaseFactor;
  int16_t minRemainingThresholdFactor;

  int32_t lowpassLine;
  int32_t lowpassLineLFE;
  int32_t clipEnergy;

  int32_t sfbMaskLowFactor[EP_MAX_SFB];
  int32_t sfbMaskHighFactor[EP_MAX_SFB];
  int32_t sfbMaskLowFactorSprEn[EP_MAX_SFB];
  int32_t sfbMaskHighFactorSprEn[EP_MAX_SFB];

  int32_t sfbMinSnrLdData[EP_MAX_SFB];

  int32_t granuleLength;
  int32_t allowIS;
  int32_t allowMS;

  int32_t tnsConfAllZero;
  int32_t pnsConfAllZero;
} EPARITY_PSY_CONF;

extern int eparity_init_psy_configuration(int bitrate, int samplerate,
                                          int bandwidth, int blocktype,
                                          int granuleLength, int useIS,
                                          int useMS, int filterbank,
                                          EPARITY_PSY_CONF *out);
*/
import "C"

const epMaxSfb = 51

// cPsyConf is the Go-side mirror of the bridge's EPARITY_PSY_CONF.
type cPsyConf struct {
	sfbCnt       int32
	sfbActive    int32
	sfbActiveLFE int32
	sfbOffset    [epMaxSfb + 1]int32

	filterbank int32

	sfbPcmQuantThreshold [epMaxSfb]int32

	maxAllowedIncreaseFactor    int32
	minRemainingThresholdFactor int16

	lowpassLine    int32
	lowpassLineLFE int32
	clipEnergy     int32

	sfbMaskLowFactor       [epMaxSfb]int32
	sfbMaskHighFactor      [epMaxSfb]int32
	sfbMaskLowFactorSprEn  [epMaxSfb]int32
	sfbMaskHighFactorSprEn [epMaxSfb]int32

	sfbMinSnrLdData [epMaxSfb]int32

	granuleLength int32
	allowIS       int32
	allowMS       int32

	tnsConfAllZero int32
	pnsConfAllZero int32
}

// cInitPsyConfiguration runs the genuine FDKaacEnc_InitPsyConfiguration and
// returns its filled config plus the AAC_ENCODER_ERROR code (0 == OK).
func cInitPsyConfiguration(bitrate, samplerate, bandwidth, blocktype, granuleLength,
	useIS, useMS, filterbank int) (cPsyConf, int) {
	var out C.EPARITY_PSY_CONF
	err := C.eparity_init_psy_configuration(
		C.int(bitrate), C.int(samplerate), C.int(bandwidth), C.int(blocktype),
		C.int(granuleLength), C.int(useIS), C.int(useMS), C.int(filterbank), &out)

	var g cPsyConf
	g.sfbCnt = int32(out.sfbCnt)
	g.sfbActive = int32(out.sfbActive)
	g.sfbActiveLFE = int32(out.sfbActiveLFE)
	for i := 0; i < epMaxSfb+1; i++ {
		g.sfbOffset[i] = int32(out.sfbOffset[i])
	}
	g.filterbank = int32(out.filterbank)
	for i := 0; i < epMaxSfb; i++ {
		g.sfbPcmQuantThreshold[i] = int32(out.sfbPcmQuantThreshold[i])
		g.sfbMaskLowFactor[i] = int32(out.sfbMaskLowFactor[i])
		g.sfbMaskHighFactor[i] = int32(out.sfbMaskHighFactor[i])
		g.sfbMaskLowFactorSprEn[i] = int32(out.sfbMaskLowFactorSprEn[i])
		g.sfbMaskHighFactorSprEn[i] = int32(out.sfbMaskHighFactorSprEn[i])
		g.sfbMinSnrLdData[i] = int32(out.sfbMinSnrLdData[i])
	}
	g.maxAllowedIncreaseFactor = int32(out.maxAllowedIncreaseFactor)
	g.minRemainingThresholdFactor = int16(out.minRemainingThresholdFactor)
	g.lowpassLine = int32(out.lowpassLine)
	g.lowpassLineLFE = int32(out.lowpassLineLFE)
	g.clipEnergy = int32(out.clipEnergy)
	g.granuleLength = int32(out.granuleLength)
	g.allowIS = int32(out.allowIS)
	g.allowMS = int32(out.allowMS)
	g.tnsConfAllZero = int32(out.tnsConfAllZero)
	g.pnsConfAllZero = int32(out.pnsConfAllZero)
	return g, int(err)
}
