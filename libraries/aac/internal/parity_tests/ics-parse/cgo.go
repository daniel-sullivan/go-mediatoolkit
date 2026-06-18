// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package ics_parse pins the Go port of the Fraunhofer FDK-AAC AAC-LC
// raw_data_block ics-parse stage (the nativeaac functions icsRead /
// icsReadMaxSfb and readSectionData — the section_data codebook layout)
// against the vendored libAACdec/src/channelinfo.cpp (IcsRead/IcsReadMaxSfb/
// getSamplingRateInfo, linked WHOLE) and a verbatim twin of
// libAACdec/src/block.cpp CBlock_ReadSectionData, compiled into this test
// binary via cgo. Random AAC-LC ics_info + section_data bit streams are
// fabricated on the Go side and parsed on both sides; the parsed window
// structure (window sequence/shape, group count, per-group lengths, max/total
// sfb, scalefactor grouping), the flat section codebook array, the section
// count, the AAC_DECODER_ERROR return code AND the post-parse bit-consumption
// position are compared bit-for-bit (EXACT int32 equality, no tolerance).
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (channelinfo.cpp for IcsRead + getSamplingRateInfo, aac_rom.cpp for the
// sfbOffsetTables ROM, FDK_bitbuffer.cpp + genericStds.cpp for the bit-buffer
// back-end, one go-test binary per package) and NEVER imports libraries/aac —
// importing it would link a second copy of the whole FDK reference and clash on
// static symbols (the same amalgamation-split reason the flac/huffman parity
// packages document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the ics-parse stage is a pure INTEGER bitstream-parse kernel — the
// window-sequence/shape reads, the scalefactor-window grouping derivation, the
// max-sfb bound check, and the section (codebook, run-length) unpacking are all
// integer/shift operations producing UCHAR/INT fields. It is therefore
// bit-identical regardless of -ffp-contract / vectorization, so no
// transcendental shim is needed and no aac_strict gate is required for FP
// reasons. The strict-gate on the Go assertion is kept only for convention (the
// area lives under the aac parity discipline); the kernel itself matches in any
// build.
package ics_parse

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/ics-parse).
// Mirrors the set in the sibling huffman-spectral-decode oracle.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACdec/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libAACenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libArithCoding/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPDec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libMpegTPEnc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libPCMutils/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libDRCdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACdec/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSACenc/include
#cgo LDFLAGS: -lm

#include "oracle_bridge.h"
*/
import "C"

import "unsafe"

// icsResult is the Go mirror of fparity_ics_result — the flattened ics-parse
// output the oracle returns.
type icsResult struct {
	windowGroupLength   [8]uint8
	windowGroups        uint8
	valid               uint8
	windowShape         uint8
	windowSequence      uint8
	maxSfBands          uint8
	scaleFactorGrouping uint8
	totalSfBands        uint8
	codeBook            [8 * 16]uint8
	numberSection       int
	errorCode           int
	bitPos              uint32
}

// cIcsParse runs the vendored getSamplingRateInfo + IcsRead + the
// CBlock_ReadSectionData twin over buf and returns the flattened result.
func cIcsParse(buf []byte, validBits, samplesPerFrame, samplingRateIndex, samplingRate uint32,
	commonWindow uint8, flags uint32) icsResult {
	var out C.fparity_ics_result
	C.fparity_ics_parse(
		(*C.uint8_t)(unsafe.Pointer(&buf[0])), C.int(len(buf)), C.uint32_t(validBits),
		C.uint32_t(samplesPerFrame), C.uint32_t(samplingRateIndex), C.uint32_t(samplingRate),
		C.uint8_t(commonWindow), C.uint32_t(flags), &out)

	var r icsResult
	for i := 0; i < 8; i++ {
		r.windowGroupLength[i] = uint8(out.windowGroupLength[i])
	}
	r.windowGroups = uint8(out.windowGroups)
	r.valid = uint8(out.valid)
	r.windowShape = uint8(out.windowShape)
	r.windowSequence = uint8(out.windowSequence)
	r.maxSfBands = uint8(out.maxSfBands)
	r.scaleFactorGrouping = uint8(out.scaleFactorGrouping)
	r.totalSfBands = uint8(out.totalSfBands)
	for i := 0; i < 8*16; i++ {
		r.codeBook[i] = uint8(out.codeBook[i])
	}
	r.numberSection = int(out.numberSection)
	r.errorCode = int(out.errorCode)
	r.bitPos = uint32(out.bitPos)
	return r
}
