// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encqcmain pins the Go port of the Fraunhofer FDK-AAC encoder
// quantization / rate-control loop's inner-loop bit counter — FDKaacEnc_dynBitCount
// and its whole static helper chain (noiselessCounter, gmStage0/1/2,
// buildBitLookUp, findBestBook / findMinMergeBits / mergeBitLookUp / findMaxMerge
// / CalcMergeGain, getSideInfoBits, scfCount, noiseCount — libAACenc/src/dyn_bits.cpp)
// plus the per-codebook bit estimator FDKaacEnc_bitCount / FDKaacEnc_countValues and
// the seven count functions (libAACenc/src/bit_cnt.cpp) — against the genuine
// vendored C, compiled into this test binary via cgo.
//
// dynBitCount is the load-bearing core of qc_main.cpp's FDKaacEnc_QCMain: for each
// channel's quantized spectrum it groups the scalefactor bands into Huffman
// sections, picks the cheapest codebook per section by greedy bit-gain merge, and
// sums the Huffman + sectioning + scalefactor-DPCM + PNS-energy bits. That sum is
// exactly the dynamic-bit consumption the rate-control loop iterates against the
// frame budget. Every value is an INT / SHORT / UINT in the integer domain; the
// whole result (the returned bit total AND the SECTION_DATA breakdown — per-band
// huffman/side/scf/noise bits and the per-section codeBook/sfbStart/sfbCnt/
// sectionBits) is compared element-for-element, bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source (dyn_bits.cpp
// + bit_cnt.cpp for the section coder + estimator, aacEnc_rom.cpp for the packed
// Huffman length / side-info / scalefactor ROM tables) and NEVER imports
// libraries/aac — importing it would link a second copy of the FDK reference and
// clash on static symbols (the same amalgamation-split reason the sibling parity
// packages document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — these are pure INTEGER /
// table-lookup kernels (the only arithmetic is array indexing, additions, min,
// abs and the packed-table hi/lo extraction). They are bit-identical regardless
// of -ffp-contract / vectorization, with no transcendental and no float, so they
// assert EXACT int32 equality. The oracle links the genuine FDKaacEnc_dynBitCount /
// FDKaacEnc_countValues / FDKaacEnc_bitCount symbols (oracle_kind == real_vendored)
// reached through the thin extern shims in bridge.cpp — no hand-twin re-derivation.
package encqcmain

/*
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to these integer / table-lookup kernels in any case.
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

extern int qcm_dyn_bit_count(const int16_t *quantSpectrum,
    const unsigned int *maxValueInSfb, const int *scalefac, int blockType,
    int sfbCnt, int maxSfbPerGroup, int sfbPerGroup, const int *sfbOffset,
    const int *noiseNrg, const int *isBook, const int *isScale,
    unsigned int syntaxFlags, int *noOfSectionsOut, int *huffmanBitsOut,
    int *sideInfoBitsOut, int *scalefacBitsOut, int *noiseNrgBitsOut,
    int *firstScfOut, int *sectCodeBookOut, int *sectSfbStartOut,
    int *sectSfbCntOut, int *sectSectionBitsOut);

extern int qcm_count_values(int16_t *values, int width, int codeBook);

extern void qcm_bit_count(const int16_t *values, int width, int maxVal,
    int *bitCountOut);
*/
import "C"

import "unsafe"

const (
	maxSections    = 60 // MAX_SECTIONS == MAX_GROUPED_SFB
	codeBookEscNdx = 11 // CODE_BOOK_ESC_NDX
)

// cDynBitCountResult mirrors the Go DynBitCountResult so the test can compare.
type cDynBitCountResult struct {
	totalBits       int
	noOfSections    int
	huffmanBits     int
	sideInfoBits    int
	scalefacBits    int
	noiseNrgBits    int
	firstScf        int
	sectCodeBook    []int
	sectSfbStart    []int
	sectSfbCnt      []int
	sectSectionBits []int
}

func i16p(s []int16) *C.int16_t {
	if len(s) == 0 {
		return nil
	}
	return (*C.int16_t)(unsafe.Pointer(&s[0]))
}

// ip returns a *C.int over an int32 slice (C INT == 32-bit; Go int is 64-bit on
// arm64, so every INT array crossing the cgo boundary is carried as int32).
func ip(s []int32) *C.int {
	if len(s) == 0 {
		return nil
	}
	return (*C.int)(unsafe.Pointer(&s[0]))
}

func up(s []uint32) *C.uint {
	if len(s) == 0 {
		return nil
	}
	return (*C.uint)(unsafe.Pointer(&s[0]))
}

// cDynBitCount runs the genuine FDKaacEnc_dynBitCount. The INT arrays (sfbOffset,
// scalefac, noiseNrg, isBook, isScale) are int32 (C INT == 32-bit); maxValueInSfb
// is uint32 (C UINT). scalefac may be nil (C NULL => no scalefactor bits).
func cDynBitCount(quantSpectrum []int16, maxValueInSfb []uint32, scalefac []int32,
	blockType, sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int32,
	noiseNrg, isBook, isScale []int32, syntaxFlags uint) cDynBitCountResult {
	var noOfSections, huffmanBits, sideInfoBits, scalefacBits, noiseNrgBits, firstScf C.int
	sectCodeBook := make([]int32, maxSections)
	sectSfbStart := make([]int32, maxSections)
	sectSfbCnt := make([]int32, maxSections)
	sectSectionBits := make([]int32, maxSections)

	total := C.qcm_dyn_bit_count(i16p(quantSpectrum), up(maxValueInSfb), ip(scalefac),
		C.int(blockType), C.int(sfbCnt), C.int(maxSfbPerGroup), C.int(sfbPerGroup),
		ip(sfbOffset), ip(noiseNrg), ip(isBook), ip(isScale), C.uint(syntaxFlags),
		&noOfSections, &huffmanBits, &sideInfoBits, &scalefacBits, &noiseNrgBits,
		&firstScf, ip(sectCodeBook), ip(sectSfbStart), ip(sectSfbCnt), ip(sectSectionBits))

	res := cDynBitCountResult{
		totalBits:    int(total),
		noOfSections: int(noOfSections),
		huffmanBits:  int(huffmanBits),
		sideInfoBits: int(sideInfoBits),
		scalefacBits: int(scalefacBits),
		noiseNrgBits: int(noiseNrgBits),
		firstScf:     int(firstScf),
	}
	for i := 0; i < int(noOfSections); i++ {
		res.sectCodeBook = append(res.sectCodeBook, int(sectCodeBook[i]))
		res.sectSfbStart = append(res.sectSfbStart, int(sectSfbStart[i]))
		res.sectSfbCnt = append(res.sectSfbCnt, int(sectSfbCnt[i]))
		res.sectSectionBits = append(res.sectSectionBits, int(sectSectionBits[i]))
	}
	return res
}

// cCountValues runs the genuine FDKaacEnc_countValues.
func cCountValues(values []int16, width, codeBook int) int {
	return int(C.qcm_count_values(i16p(values), C.int(width), C.int(codeBook)))
}

// cBitCount runs the genuine FDKaacEnc_bitCount, returning the per-codebook bit
// cost row [0, CODE_BOOK_ESC_NDX].
func cBitCount(values []int16, width, maxVal int) []int {
	out := make([]int32, codeBookEscNdx+1)
	C.qcm_bit_count(i16p(values), C.int(width), C.int(maxVal), ip(out))
	res := make([]int, codeBookEscNdx+1)
	for i := range out {
		res[i] = int(out[i])
	}
	return res
}
