// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encbitstream pins the Go port of the Fraunhofer FDK-AAC fixed-point
// ENCODE bitstream stage — the raw_data_block syntax serializers in
// libAACenc/src/bitenc.cpp (FDKaacEnc_encodeIcsInfo / encodeGlobalGain /
// encodeSectionData / encodeScaleFactorData / encodeMSInfo /
// encodeTnsDataPresent / encodeTnsData / encodeSpectralData) — against the
// vendored C, compiled into this test binary via cgo. For a range of fabricated
// ics-info / section / scalefactor / spectral / TNS / MS inputs the genuine C
// reference is run and its produced access-unit bytes plus the static-bit count
// it reports are compared bit-for-bit against the nativeaac port.
//
// These eight serializers turn the quantizer/coder output (SECTION_DATA, the
// quantized spectrum, scalefactors, PNS/intensity scales) and the
// psychoacoustic output (block type, window shape, grouping, TNS_INFO, msMask)
// into the AAC individual_channel_stream syntax — ics_info, section_data,
// scale_factor_data, spectral_data and the TNS/MS side info. Every value is an
// int32/int16 in Q-format-irrelevant integer space (bit fields, codebook
// indices, DPCM deltas); the whole result is compared byte-for-byte.
//
// This package compiles its OWN copy of the needed vendored C++ source
// (bitenc.cpp via bridge.cpp; bit_cnt.cpp for the genuine Huffman emitters,
// aacEnc_rom.cpp for the Huffman tables, FDK_tools_rom.cpp for
// getBitstreamElementList, FDK_bitbuffer.cpp for the FDK_put ring store,
// genericStds.cpp for the libSYS shims) and NEVER imports libraries/aac —
// importing it would link a second copy of the whole FDK reference and clash on
// static symbols (the same amalgamation-split reason the sibling enc-quantize /
// enc-stereo-tns oracles document). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// # Oracle fidelity
//
// The eight encode* serializers are all `static` in bitenc.cpp; bridge.cpp
// #includes the genuine vendored bitenc.cpp so the extern "C" shims reach the
// REAL static functions (not a hand-twin). FDKaacEnc_codeValues /
// FDKaacEnc_codeScalefactorDelta (called by the spectral / scalefactor
// serializers) are the genuine vendored functions; the FDK bit WRITER is the
// genuine inline reference driven through FDKinitBitStream. oracle_kind ==
// real_vendored.
//
// Integer parity: the whole bitstream-encode area is a pure INTEGER kernel (bit
// shifts, masks, table lookups), bit-identical regardless of -ffp-contract /
// vectorization, with no transcendental and no float — so the assertions are
// EXACT-byte equality. The strict gate on the Go side is the area convention,
// not a numerical necessity.
package encbitstream

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/enc-bitstream).
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
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

extern int ebparity_ics_info(int blockType, int windowShape, int groupingMask,
                             int maxSfbPerGroup, unsigned int syntaxFlags,
                             unsigned char *out, int bufBytes, int *statBits);
extern int ebparity_global_gain(int globalGain, int scalefac, int mdctScale,
                                unsigned char *out, int bufBytes, int *statBits);
extern int ebparity_section_data(int blockType, int noOfSections,
                                 const int *codeBook, const int *sfbStart,
                                 const int *sfbCnt, unsigned int useVCB11,
                                 unsigned char *out, int bufBytes, int *siBits);
extern int ebparity_scalefactor_data(
    int blockType, int firstScf, int noOfSections, const int *codeBook,
    const int *sfbStart, const int *sfbCnt, const unsigned int *maxValueInSfb,
    const int *scalefac, const int *noiseNrg, const int *isScale,
    int globalGain, unsigned char *out, int bufBytes, int *sfBits);
extern int ebparity_ms_info(int sfbCnt, int grpSfb, int maxSfb, int msDigest,
                            const int *jsFlags, unsigned char *out, int bufBytes,
                            int *msBits);
extern int ebparity_tns_data_present(
    int blockType, int numOfWindows, const int *coefRes,
    const int *numOfFilters, const int *length, const int *order,
    const int *direction, const int *coef, unsigned char *out, int bufBytes,
    int *statBits);
extern int ebparity_tns_data(int blockType, int numOfWindows, const int *coefRes,
                             const int *numOfFilters, const int *length,
                             const int *order, const int *direction,
                             const int *coef, unsigned char *out, int bufBytes,
                             int *tnsBits);
extern int ebparity_spectral_data(int blockType, int noOfSections,
                                  const int *codeBook, const int *sfbStart,
                                  const int *sfbCnt, int *sfbOffset,
                                  short *quantSpectrum, unsigned char *out,
                                  int bufBytes, int *specBits);
*/
import "C"

import "unsafe"

// ip returns a *C.int over a Go []int32 (nil for empty). The Go side keeps
// section / offset arrays as int32 so the pointer aliases C int (4 bytes).
func ip(s []int32) *C.int {
	if len(s) == 0 {
		return nil
	}
	return (*C.int)(unsafe.Pointer(&s[0]))
}

// up returns a *C.uint over a Go []uint32 (nil for empty).
func up(s []uint32) *C.uint {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint)(unsafe.Pointer(&s[0]))
}

// cIcsInfo runs the genuine FDKaacEnc_encodeIcsInfo and returns the produced
// bytes plus the static-bit count.
func cIcsInfo(blockType, windowShape, groupingMask, maxSfbPerGroup, bufBytes int,
	syntaxFlags uint32) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_ics_info(C.int(blockType), C.int(windowShape),
		C.int(groupingMask), C.int(maxSfbPerGroup), C.uint(syntaxFlags),
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cGlobalGain runs the genuine FDKaacEnc_encodeGlobalGain.
func cGlobalGain(globalGain, scalefac, mdctScale, bufBytes int) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_global_gain(C.int(globalGain), C.int(scalefac),
		C.int(mdctScale), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cSectionData runs the genuine FDKaacEnc_encodeSectionData. codeBook / sfbStart
// / sfbCnt are length noOfSections.
func cSectionData(blockType int, codeBook, sfbStart, sfbCnt []int32,
	useVCB11 bool, bufBytes int) (out []byte, siBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	v := C.uint(0)
	if useVCB11 {
		v = 1
	}
	n := C.ebparity_section_data(C.int(blockType), C.int(len(codeBook)),
		ip(codeBook), ip(sfbStart), ip(sfbCnt), v,
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cScaleFactorData runs the genuine FDKaacEnc_encodeScaleFactorData.
func cScaleFactorData(blockType, firstScf int, codeBook, sfbStart, sfbCnt []int32,
	maxValueInSfb []uint32, scalefac, noiseNrg, isScale []int32, globalGain,
	bufBytes int) (out []byte, sfBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_scalefactor_data(C.int(blockType), C.int(firstScf),
		C.int(len(codeBook)), ip(codeBook), ip(sfbStart), ip(sfbCnt),
		up(maxValueInSfb), ip(scalefac), ip(noiseNrg), ip(isScale),
		C.int(globalGain), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cMSInfo runs the genuine FDKaacEnc_encodeMSInfo. jsFlags is length sfbCnt.
func cMSInfo(sfbCnt, grpSfb, maxSfb, msDigest int, jsFlags []int32,
	bufBytes int) (out []byte, msBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_ms_info(C.int(sfbCnt), C.int(grpSfb), C.int(maxSfb),
		C.int(msDigest), ip(jsFlags), (*C.uchar)(unsafe.Pointer(&buf[0])),
		C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cTnsDataPresent runs the genuine FDKaacEnc_encodeTnsDataPresent. The flat
// per-window/filter arrays match bridge.cpp fillTnsInfo's layout: coefRes /
// numOfFilters are length numOfWindows; length / order / direction are length
// numOfWindows*MAX_NUM_OF_FILTERS (==2); coef is length
// numOfWindows*MAX_NUM_OF_FILTERS*TNS_MAX_ORDER (==12).
func cTnsDataPresent(blockType, numOfWindows int, coefRes, numOfFilters, length,
	order, direction, coef []int32, bufBytes int) (out []byte, statBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_tns_data_present(C.int(blockType), C.int(numOfWindows),
		ip(coefRes), ip(numOfFilters), ip(length), ip(order), ip(direction),
		ip(coef), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cTnsData runs the genuine FDKaacEnc_encodeTnsData.
func cTnsData(blockType, numOfWindows int, coefRes, numOfFilters, length,
	order, direction, coef []int32, bufBytes int) (out []byte, tnsBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	n := C.ebparity_tns_data(C.int(blockType), C.int(numOfWindows),
		ip(coefRes), ip(numOfFilters), ip(length), ip(order), ip(direction),
		ip(coef), (*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}

// cSpectralData runs the genuine FDKaacEnc_encodeSpectralData. sfbOffset is
// length >= max(sfbStart+sfbCnt)+1; quantSpectrum holds the quantized lines.
func cSpectralData(blockType int, codeBook, sfbStart, sfbCnt, sfbOffset []int32,
	quantSpectrum []int16, bufBytes int) (out []byte, specBits int) {
	buf := make([]byte, bufBytes)
	var sb C.int
	var qp *C.short
	if len(quantSpectrum) > 0 {
		qp = (*C.short)(unsafe.Pointer(&quantSpectrum[0]))
	}
	n := C.ebparity_spectral_data(C.int(blockType), C.int(len(codeBook)),
		ip(codeBook), ip(sfbStart), ip(sfbCnt), ip(sfbOffset), qp,
		(*C.uchar)(unsafe.Pointer(&buf[0])), C.int(bufBytes), &sb)
	return buf[:int(n)], int(sb)
}
