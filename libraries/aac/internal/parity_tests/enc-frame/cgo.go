// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encframe pins the Go port of the self-contained integer rate-control
// driver helpers of the FDK-AAC quantizer/coder (libAACenc/src/qc_main.cpp) —
// FDKaacEnc_calcFrameLen, FDKaacEnc_framePadding, FDKaacEnc_AdjustBitrate,
// FDKaacEnc_calcMaxValueInSfb, FDKaacEnc_BitResRedistribution,
// FDKaacEnc_distributeElementDynBits, FDKaacEnc_updateUsedDynBits,
// FDKaacEnc_getTotalConsumedBits, FDKaacEnc_updateFillBits and
// FDKaacEnc_updateBitres — against the vendored fdk reference, compiled into
// this test binary via cgo.
//
// These are the leaf rate-control helpers the top-level FDKaacEnc_QCMain
// (qc_main.cpp:788) and FDKaacEnc_EncodeFrame (aacenc.cpp:769) orchestration
// calls: bitrate->frame-bytes conversion with byte padding, the bit-reservoir
// split across elements, the dynamic-bit distribution, the consumed-bit
// accounting and the fill-bit / reservoir update. Every value is an INT in the
// integer domain; results are compared exactly, bit-for-bit.
//
// This package compiles its OWN copy of the needed oracle bridge and NEVER
// imports libraries/aac (which would link a second copy of the FDK reference
// and clash). It MAY, and does, import the pure-Go internal/nativeaac.
//
// oracle_kind == verbatim_twin: most of these functions are file-local `static`
// in qc_main.cpp and the public ones would drag in the whole 12k-line encoder
// if linked from the genuine TU. So bridge.cpp carries BYTE-FOR-BYTE verbatim
// copies of each vendored body (renamed *_oracle) over the GENUINE vendored
// struct layouts (qc_data.h) and integer kernels (fMultI / fixMax / fixMin /
// fixp_abs from the real libFDK headers) — only the function bodies are
// duplicated, verbatim. See bridge.cpp for the per-function provenance.
//
// The whole AAC island is fenced behind the opt-in aacfdk build tag, so a
// default `go build ./...` links none of it; the cgo oracle additionally
// requires cgo. These are pure-integer kernels, so they assert EXACT int32
// equality regardless of -ffp-contract / vectorization.
package encframe

/*
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags come from the
// mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"); they are irrelevant
// to these integer kernels in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern int encframe_calc_frame_len(int bitRate, int sampleRate,
    int granuleLength, int mode);
extern int encframe_frame_padding(int bitRate, int sampleRate,
    int granuleLength, int *paddingRest);
extern int encframe_adjust_bitrate(int *paddingRest, int *avgTotalBits,
    int bitRate, int sampleRate, int granuleLength);
extern int encframe_calc_max_value_in_sfb(int sfbCnt, int maxSfbPerGroup,
    int sfbPerGroup, int *sfbOffset, int16_t *quantSpectrum,
    unsigned int *maxValue);
extern int encframe_bitres_redistribution(int nElements, int *relativeBits,
    int bitResTot, int bitResTotMax, int maxBitsPerFrame, int avgTotalBits,
    int *bitResLevelOut, int *maxBitResBitsOut);
extern int encframe_distribute_dyn_bits(int nElements, int *relativeBits,
    int codeBits, int *dynBitsUsed, int *grantedDynBitsOut, int *sumDynBitsOut);
extern int encframe_total_consumed_bits(int nElements, int *dynBitsUsed,
    int *staticBitsUsed, int *extBitsUsed, int globalExtBits, int globHdrBits);
extern void encframe_update_fill_bits(int bitrateMode, int minBitsPerFrame,
    int bitResTot, int bitResTotMax, int grantedDynBits, int usedDynBits,
    int staticBits, int elementExtBits, int globalExtBits,
    int *totFillBitsOut, int *totalBitsOut);
extern int encframe_update_bitres(int bitrateMode, int bitResTot,
    int maxBitsPerFrame, int bitResTotMax, int grantedDynBits, int usedDynBits,
    int totFillBits, int alignBits);
*/
import "C"

import "unsafe"

func ip(s []int32) *C.int {
	if len(s) == 0 {
		return nil
	}
	return (*C.int)(unsafe.Pointer(&s[0]))
}

func i16p(s []int16) *C.int16_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int16_t)(unsafe.Pointer(&s[0]))
}

func up(s []uint32) *C.uint {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint)(unsafe.Pointer(&s[0]))
}

func cCalcFrameLen(bitRate, sampleRate, granuleLength, mode int) int {
	return int(C.encframe_calc_frame_len(C.int(bitRate), C.int(sampleRate),
		C.int(granuleLength), C.int(mode)))
}

func cFramePadding(bitRate, sampleRate, granuleLength, paddingRest int) (int, int) {
	pr := C.int(paddingRest)
	on := C.encframe_frame_padding(C.int(bitRate), C.int(sampleRate),
		C.int(granuleLength), &pr)
	return int(on), int(pr)
}

func cAdjustBitrate(paddingRest, bitRate, sampleRate, granuleLength int) (int, int) {
	pr := C.int(paddingRest)
	var avg C.int
	C.encframe_adjust_bitrate(&pr, &avg, C.int(bitRate), C.int(sampleRate),
		C.int(granuleLength))
	return int(avg), int(pr)
}

func cCalcMaxValueInSfb(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int32, quantSpectrum []int16, maxValue []uint32) int {
	return int(C.encframe_calc_max_value_in_sfb(C.int(sfbCnt), C.int(maxSfbPerGroup),
		C.int(sfbPerGroup), ip(sfbOffset), i16p(quantSpectrum), up(maxValue)))
}

func cBitResRedistribution(relativeBits []int32, bitResTot, bitResTotMax, maxBitsPerFrame, avgTotalBits int) (int, []int32, []int32) {
	n := len(relativeBits)
	lvl := make([]int32, n)
	maxb := make([]int32, n)
	err := C.encframe_bitres_redistribution(C.int(n), ip(relativeBits),
		C.int(bitResTot), C.int(bitResTotMax), C.int(maxBitsPerFrame),
		C.int(avgTotalBits), ip(lvl), ip(maxb))
	return int(err), lvl, maxb
}

func cDistributeDynBits(relativeBits []int32, codeBits int, dynBitsUsed []int32) (int, []int32, int) {
	n := len(relativeBits)
	granted := make([]int32, n)
	var sum C.int
	err := C.encframe_distribute_dyn_bits(C.int(n), ip(relativeBits),
		C.int(codeBits), ip(dynBitsUsed), ip(granted), &sum)
	return int(err), granted, int(sum)
}

func cTotalConsumedBits(dynBitsUsed, staticBitsUsed, extBitsUsed []int32, globalExtBits, globHdrBits int) int {
	return int(C.encframe_total_consumed_bits(C.int(len(dynBitsUsed)),
		ip(dynBitsUsed), ip(staticBitsUsed), ip(extBitsUsed),
		C.int(globalExtBits), C.int(globHdrBits)))
}

func cUpdateFillBits(bitrateMode, minBitsPerFrame, bitResTot, bitResTotMax, grantedDynBits, usedDynBits, staticBits, elementExtBits, globalExtBits int) (int, int) {
	var totFill, total C.int
	C.encframe_update_fill_bits(C.int(bitrateMode), C.int(minBitsPerFrame),
		C.int(bitResTot), C.int(bitResTotMax), C.int(grantedDynBits),
		C.int(usedDynBits), C.int(staticBits), C.int(elementExtBits),
		C.int(globalExtBits), &totFill, &total)
	return int(totFill), int(total)
}

func cUpdateBitres(bitrateMode, bitResTot, maxBitsPerFrame, bitResTotMax, grantedDynBits, usedDynBits, totFillBits, alignBits int) int {
	return int(C.encframe_update_bitres(C.int(bitrateMode), C.int(bitResTot),
		C.int(maxBitsPerFrame), C.int(bitResTotMax), C.int(grantedDynBits),
		C.int(usedDynBits), C.int(totFillBits), C.int(alignBits)))
}
