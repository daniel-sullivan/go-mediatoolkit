// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package dequant pins the Go port of the Fraunhofer FDK-AAC AAC-LC decode
// "dequant" stage (the nativeaac drivers readScaleFactorData /
// inverseQuantizeSpectralData / scaleSpectralData and cPnsRead — block.cpp:158/
// 487/217 + aacdec_pns.cpp:211) against verbatim twins of those vendored
// functions compiled into this test binary via cgo. The twins call the GENUINE
// vendored getSamplingRateInfo + CIcsInfo accessors (channelinfo.cpp, linked
// whole), the genuine block.h Huffman-word inlines and EvaluatePower43, and the
// genuine quant ROM (aac_rom.cpp). A fabricated AAC-LC channel context —
// scalefactor bitstream, section codebooks, and raw quantized spectrum — is fed
// to both sides; the scaled FIXP_DBL spectrum, the per-(group,band)
// scalefactors, the per-(window,sfb) and per-window block exponents AND the two
// driver return codes are compared bit-for-bit (EXACT int32 equality, no
// tolerance).
//
// This package compiles its OWN copy of the needed vendored C++ sources
// (channelinfo.cpp for getSamplingRateInfo + the CIcsInfo accessors, aac_rom.cpp
// for the InverseQuantTable / MantissaTable / ExponentTable + BOOKSCL codebook,
// FDK_bitbuffer.cpp + genericStds.cpp for the bit-buffer back-end, hcr state
// link stubs for the unused HCR dispatch table aac_rom.cpp's ROM references; one
// go-test binary per package) and NEVER imports libraries/aac — importing it
// would link a second copy of the whole FDK reference and clash on static
// symbols (the same amalgamation-split reason the sibling ics-parse / inverse-
// quant oracles document). It MAY, and does, import the pure-Go
// internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the dequant stage is a pure INTEGER / fixed-point area — FIXP_DBL
// is a Q1.31 int32, the (4/3)-power interpolation reads an integer ROM,
// fMultDiv2 is an int64 product shifted back to int32, exponents are int16, and
// every scale is an arithmetic shift. It is bit-identical regardless of
// -ffp-contract / vectorization, so no transcendental shim is needed and no
// aac_strict gate is required for FP reasons; the parity assertions run under a
// bare -tags aacfdk. See block.cpp / aacdec_pns.cpp / aac_rom.cpp.
package dequant

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/dequant). Mirrors
// the set in the sibling ics-parse / inverse-quant oracles.
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

// dequantResult is the Go mirror of fparity_dequant_result — the flattened
// dequant output the oracle returns.
type dequantResult struct {
	scaleFactor [8 * 16]int16
	sfbScale    [8 * 16]int16
	specScale   [8]int16
	readSfErr   int
	invQuantErr int
}

// cDequant runs the vendored verbatim dequant twins over the fabricated context
// and returns the flattened result plus the scaled spectrum (written back into a
// copy of rawSpectrum).
func cDequant(scaleFactorBuf []byte, sfValidBits, samplesPerFrame, samplingRateIndex,
	samplingRate uint32, globalGain uint8, flags uint32, windowSequence,
	windowGroups uint8, windowGroupLength [8]uint8, scaleFactorGrouping, maxSfBands uint8,
	codeBook [8 * 16]uint8, rawSpectrum []int32) (dequantResult, []int32) {

	spec := append([]int32(nil), rawSpectrum...)
	wgl := windowGroupLength
	cb := codeBook

	var out C.fparity_dequant_result
	var specPtr *C.int32_t
	if len(spec) > 0 {
		specPtr = (*C.int32_t)(unsafe.Pointer(&spec[0]))
	}
	C.fparity_dequant(
		(*C.uint8_t)(unsafe.Pointer(&scaleFactorBuf[0])), C.int(len(scaleFactorBuf)),
		C.uint32_t(sfValidBits), C.uint32_t(samplesPerFrame),
		C.uint32_t(samplingRateIndex), C.uint32_t(samplingRate),
		C.uint8_t(globalGain), C.uint32_t(flags), C.uint8_t(windowSequence),
		C.uint8_t(windowGroups), (*C.uint8_t)(unsafe.Pointer(&wgl[0])),
		C.uint8_t(scaleFactorGrouping), C.uint8_t(maxSfBands),
		(*C.uint8_t)(unsafe.Pointer(&cb[0])), specPtr, C.int(len(spec)), &out)

	var r dequantResult
	for i := 0; i < 8*16; i++ {
		r.scaleFactor[i] = int16(out.scaleFactor[i])
		r.sfbScale[i] = int16(out.sfbScale[i])
	}
	for i := 0; i < 8; i++ {
		r.specScale[i] = int16(out.specScale[i])
	}
	r.readSfErr = int(out.readSfErr)
	r.invQuantErr = int(out.invQuantErr)
	return r, spec
}
