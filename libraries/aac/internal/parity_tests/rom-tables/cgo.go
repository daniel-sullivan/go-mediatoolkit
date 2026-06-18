// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package rom_tables pins the Go port of the Fraunhofer FDK-AAC sfb-offset /
// sampling-rate-info ROM (the nativeaac sfbOffsetTables + the per-resolution
// sfb_* offset arrays, indexed via getSamplingRateInfo) against the vendored
// libAACdec/src/aac_rom.cpp + channelinfo.cpp, compiled into this test binary
// via cgo. For every (frame length, sampling-rate index, rate) combination the
// C reference getSamplingRateInfo is run and its result — the error code, the
// resolved long/short band counts, the sampling-rate index/rate, and the FULL
// resolved offset tables copied [0 .. count] inclusive — is compared
// field-for-field and value-for-value against the nativeaac port.
//
// This package compiles its OWN copy of the needed vendored C++ source
// (aac_rom.cpp for the sfbOffsetTables ROM + the static sfb_* offset arrays,
// plus the HCR-state link stubs aac_rom.cpp's unused dispatch table demands;
// one go-test binary per package) and NEVER imports libraries/aac — importing
// it would link a second copy of the whole FDK reference and clash on static
// symbols (the same amalgamation-split reason the flac parity packages
// document). It MAY, and does, import the pure-Go internal/nativeaac.
//
// getSamplingRateInfo itself lives in channelinfo.cpp, which drags in the whole
// per-channel decoder struct at link time, so per the add-audio-format parity
// discipline its body is copied VERBATIM into the oracle TU
// (oracle_romtables_cgo.cpp) typed against the genuine vendored sfbOffsetTables
// ROM — the real reference code, decoupled from the rest of the decoder.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: this slice is a pure INTEGER ROM lookup — the sfb offsets are
// int16 ROM values and getSamplingRateInfo is integer comparisons / a switch /
// pointer selects. It is bit-identical regardless of -ffp-contract /
// vectorization, and there is no float anywhere in this path, so it is fenced
// by aacfdk alone with NO aac_strict gate (no FP split exists for it) and no
// transcendental shim. See aac_rom.cpp / channelinfo.cpp.
package rom_tables

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at libraries/aac/internal/parity_tests/rom-tables).
// Mirrors the set in libraries/aac/aac_cgo.go and the sibling inverse-quant /
// huffman-spectral-decode oracles.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer ROM lookup in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
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

#include <stdint.h>

// fparity_sri_result and fparity_get_sampling_rate_info are declared in
// oracle_bridge.h, shared verbatim with the oracle TU oracle_romtables_cgo.cpp
// so the struct layout is identical on both sides.
#include "oracle_bridge.h"
*/
import "C"

// sriResult is the Go-side mirror of the C fparity_sri_result, holding the
// reference getSamplingRateInfo result for one (frame length, sampling-rate
// index, rate) combination.
type sriResult struct {
	err              int
	numberOfSfbLong  uint8
	numberOfSfbShort uint8
	samplingRateIdx  uint32
	samplingRate     uint32
	longIsNull       bool
	shortIsNull      bool
	longOffsets      []int16 // copied [0 .. numberOfSfbLong] inclusive
	shortOffsets     []int16 // copied [0 .. numberOfSfbShort] inclusive
}

// cGetSamplingRateInfo runs the vendored getSamplingRateInfo (copied verbatim
// in the oracle TU against the genuine sfbOffsetTables ROM) for the given frame
// length / sampling-rate index / rate and returns the reference result with the
// resolved offset tables materialised by value.
func cGetSamplingRateInfo(samplesPerFrame, samplingRateIndex, samplingRate uint32) sriResult {
	var r C.fparity_sri_result
	C.fparity_get_sampling_rate_info(
		C.uint(samplesPerFrame), C.uint(samplingRateIndex),
		C.uint(samplingRate), &r)

	out := sriResult{
		err:              int(r.err),
		numberOfSfbLong:  uint8(r.number_of_sfb_long),
		numberOfSfbShort: uint8(r.number_of_sfb_short),
		samplingRateIdx:  uint32(r.sampling_rate_index),
		samplingRate:     uint32(r.sampling_rate),
		longIsNull:       r.long_is_null != 0,
		shortIsNull:      r.short_is_null != 0,
	}
	if !out.longIsNull {
		n := int(out.numberOfSfbLong) + 1
		out.longOffsets = make([]int16, n)
		for k := 0; k < n; k++ {
			out.longOffsets[k] = int16(r.long_offsets[k])
		}
	}
	if !out.shortIsNull {
		n := int(out.numberOfSfbShort) + 1
		out.shortOffsets = make([]int16, n)
		for k := 0; k < n; k++ {
			out.shortOffsets[k] = int16(r.short_offsets[k])
		}
	}
	return out
}
