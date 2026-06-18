// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package enc_psy_main pins the Go port of the Fraunhofer FDK-AAC encoder
// psychoacoustic DRIVER leaf kernels — energy spreading
// (nativeaac.SpreadingMax), pre-echo control (nativeaac.InitPreEchoControl /
// PreEchoControl), tonality (nativeaac.CalculateFullTonality) and short-block
// grouping (nativeaac.GroupShortData) — against the genuine vendored
// libAACenc/src kernels FDKaacEnc_psyMain assembles (spreading.cpp,
// pre_echo_control.cpp, tonality.cpp, grp_data.cpp), compiled into this test
// binary via cgo. For a range of fabricated inputs the C kernels are computed
// and compared bit-for-bit (raw int32 / int16) against the nativeaac ports.
//
// This package compiles its OWN copy of the needed vendored C++ sources (the
// four leaf TUs + chaosmeasure.cpp + fixpoint_math.cpp for the tonality
// dependency chain, one go-test binary per package) and NEVER imports
// libraries/aac — importing it would link a second copy of the whole FDK
// reference and clash on static symbols. It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
package enc_psy_main

/*
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to these integer kernels in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void eparity_spreading_max(int pbCnt, const int32_t *maskLowFactor,
                                  const int32_t *maskHighFactor,
                                  int32_t *pbSpreadEnergy);
extern void eparity_init_pre_echo_control(int32_t *pbThresholdNm1,
                                          const int32_t *sfbPcmQuantThreshold,
                                          int numPb, int *mdctScalenm1,
                                          int *calcPreEcho);
extern void eparity_pre_echo_control(int32_t *pbThresholdNm1, int calcPreEcho,
                                     int numPb, int maxAllowedIncreaseFactor,
                                     int16_t minRemainingThresholdFactor,
                                     int32_t *pbThreshold, int mdctScale,
                                     int *mdctScalenm1);
extern void eparity_calculate_full_tonality(int32_t *spectrum,
                                            int *sfbMaxScaleSpec,
                                            int32_t *sfbEnergyLD64,
                                            int16_t *sfbTonality, int sfbCnt,
                                            const int *sfbOffset, int usePns);
extern int eparity_group_short_data(
    int32_t *mdctSpectrum, int32_t *sfbThreshold, int32_t *sfbEnergy,
    int32_t *sfbEnergyMS, int32_t *sfbSpreadEnergy, int sfbCnt, int sfbActive,
    const int *sfbOffset, const int32_t *sfbMinSnrLdData, int *groupedSfbOffset,
    int32_t *groupedSfbMinSnrLdData, int noOfGroups, const int *groupLen,
    int granuleLength);
*/
import "C"

import "unsafe"

// cSpreadingMax runs the genuine FDKaacEnc_SpreadingMax over a copy of energy,
// returning the spread result (len(energy) bands).
func cSpreadingMax(maskLow, maskHigh, energy []int32) []int32 {
	out := append([]int32(nil), energy...)
	if len(out) == 0 {
		return out
	}
	C.eparity_spreading_max(C.int(len(out)),
		(*C.int32_t)(unsafe.Pointer(&maskLow[0])),
		(*C.int32_t)(unsafe.Pointer(&maskHigh[0])),
		(*C.int32_t)(unsafe.Pointer(&out[0])))
	return out
}

// cInitPreEchoControl runs the genuine FDKaacEnc_InitPreEchoControl, returning
// the filled pbThresholdNm1 (len numPb) and (mdctScalenm1, calcPreEcho).
func cInitPreEchoControl(sfbPcmQuantThreshold []int32, numPb int) (nm1 []int32, mdctScalenm1, calcPreEcho int) {
	nm1 = make([]int32, numPb)
	var ms, cpe C.int
	C.eparity_init_pre_echo_control(
		(*C.int32_t)(unsafe.Pointer(&nm1[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbPcmQuantThreshold[0])),
		C.int(numPb), &ms, &cpe)
	return nm1, int(ms), int(cpe)
}

// cPreEchoControl runs the genuine FDKaacEnc_PreEchoControl, mutating nm1 and
// threshold in place and returning the updated mdctScalenm1.
func cPreEchoControl(nm1 []int32, calcPreEcho, numPb, maxInc int, minRemain int16,
	threshold []int32, mdctScale, mdctScalenm1 int) int {
	ms := C.int(mdctScalenm1)
	C.eparity_pre_echo_control(
		(*C.int32_t)(unsafe.Pointer(&nm1[0])), C.int(calcPreEcho), C.int(numPb),
		C.int(maxInc), C.int16_t(minRemain),
		(*C.int32_t)(unsafe.Pointer(&threshold[0])), C.int(mdctScale), &ms)
	return int(ms)
}

// cCalculateFullTonality runs the genuine FDKaacEnc_CalculateFullTonality,
// returning sfbTonality (len sfbCnt int16).
func cCalculateFullTonality(spectrum []int32, sfbMaxScaleSpec []int32,
	sfbEnergyLD64 []int32, sfbCnt int, sfbOffset []int32, usePns int) []int16 {
	tonality := make([]int16, sfbCnt)
	C.eparity_calculate_full_tonality(
		(*C.int32_t)(unsafe.Pointer(&spectrum[0])),
		(*C.int)(unsafe.Pointer(&sfbMaxScaleSpec[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbEnergyLD64[0])),
		(*C.int16_t)(unsafe.Pointer(&tonality[0])),
		C.int(sfbCnt),
		(*C.int)(unsafe.Pointer(&sfbOffset[0])),
		C.int(usePns))
	return tonality
}

// cGroupShortData runs the genuine FDKaacEnc_groupShortData. The four SFB
// unions are flat int32 buffers of 120 cells each (in-out); mdctSpectrum is
// granuleLength cells (in-out). groupedSfbOffset/groupedSfbMinSnr are outputs.
// Returns maxSfbPerGroup. All slices are mutated in place to match the Go port.
func cGroupShortData(mdctSpectrum, sfbThreshold, sfbEnergy, sfbEnergyMS, sfbSpreadEnergy []int32,
	sfbCnt, sfbActive int, sfbOffset []int32, sfbMinSnr []int32,
	groupedSfbOffset []int32, groupedSfbMinSnr []int32,
	noOfGroups int, groupLen []int32, granuleLength int) int {
	r := C.eparity_group_short_data(
		(*C.int32_t)(unsafe.Pointer(&mdctSpectrum[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbThreshold[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbEnergy[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbEnergyMS[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbSpreadEnergy[0])),
		C.int(sfbCnt), C.int(sfbActive),
		(*C.int)(unsafe.Pointer(&sfbOffset[0])),
		(*C.int32_t)(unsafe.Pointer(&sfbMinSnr[0])),
		(*C.int)(unsafe.Pointer(&groupedSfbOffset[0])),
		(*C.int32_t)(unsafe.Pointer(&groupedSfbMinSnr[0])),
		C.int(noOfGroups),
		(*C.int)(unsafe.Pointer(&groupLen[0])),
		C.int(granuleLength))
	return int(r)
}
