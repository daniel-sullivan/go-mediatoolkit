// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package ms_stereo_decode pins the Go port of the Fraunhofer FDK-AAC M/S
// (mid/side) joint-stereo decode tools (the nativeaac functions
// generateMSOutput and the non-complex-prediction "MS stereo" branch of
// ApplyMS) against the vendored libAACdec/src/stereo.cpp, compiled into this
// test binary via cgo. Random L/R quantized MDCT spectra, per-window SFB scale
// exponents, band offsets, a window-group structure and an MsUsed flag array
// are fabricated on the Go side and transformed in place on BOTH sides; the
// post-upmix L/R spectra, the L/R SFB scales and the (possibly cleared) MsUsed
// array are compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C kernel
// (CJointStereo_GenerateMSOutput, carried verbatim in ms_stereo_cgo_src.cpp —
// it is `static inline` in stereo.cpp and so cannot be linked from the whole
// TU without dragging the entire decoder + complex-prediction filterbank) and
// NEVER imports libraries/aac — importing it would link a second copy of the
// whole FDK reference and clash on static symbols (the same amalgamation-split
// reason the flac parity packages document). It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island (vendored FDK source + nativeaac) is fenced behind the
// opt-in aacfdk build tag, so a default `go build ./...` links none of it. The
// cgo oracle additionally requires cgo. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: the M/S stereo path is a pure INTEGER kernel — FIXP_DBL (int32)
// arithmetic right-shifts to a common scale, then integer add/sub for the
// mid/side sum and difference. It is therefore bit-identical regardless of
// -ffp-contract / vectorization, so no transcendental shim is needed. The
// strict-gate on the Go assertion is kept only for convention (the area lives
// under the aac_strict parity discipline); the kernel itself matches in any
// build.
package ms_stereo_decode

/*
// Include search paths for the vendored libfdk tree, rooted three levels up
// (this package lives at
// libraries/aac/internal/parity_tests/ms-stereo-decode). The M/S kernel needs
// only common_fix.h (the FIXP_DBL fixed-point inlines + fMin + DFRACT_BITS),
// so only the libFDK / libSYS include dirs (and the module root, for the
// "libfdk/..." prefix style other oracles use) are required.
//
// Only -I / -D / -Wno-* belong in-source. The scalar FP flags
// (-ffp-contract=off -fno-vectorize -fno-slp-vectorize -fno-unroll-loops) come
// from the mise task env (CGO_CFLAGS, with CGO_CFLAGS_ALLOW=".*"), not here —
// Go's cgo flag allowlist rejects -ffp-contract=off in source. They are
// irrelevant to this integer kernel in any case.
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

// fparity_apply_ms runs the genuine non-cplx-pred M/S branch of
// CJointStereo_ApplyMS over the fabricated flat arrays, transforming the L/R
// spectra and L/R SFB scales in place and clearing msUsed when
// msMaskPresent==2. Argument layout mirrors nativeaac.ApplyMS exactly.
extern void fparity_apply_ms(uint8_t msMaskPresent, uint8_t *msUsed,
                             int32_t *spectrumL, int32_t *spectrumR,
                             int16_t *sfbLeftScale, int16_t *sfbRightScale,
                             const int16_t *sfbOffsets,
                             const uint8_t *windowGroupLength, int windowGroups,
                             int max_sfb_ste_outside,
                             int scaleFactorBandsTransmittedL,
                             int scaleFactorBandsTransmittedR,
                             int granuleLength);
*/
import "C"

import "unsafe"

// cApplyMSResult is the post-transform state the C oracle leaves behind, so the
// Go side can compare every mutated array bit-for-bit.
type cApplyMSResult struct {
	spectrumL  []int32
	spectrumR  []int32
	leftScale  []int16
	rightScale []int16
	msUsed     []uint8
}

// cApplyMS runs the vendored M/S apply branch over independent copies of the
// fabricated inputs and returns the mutated arrays. The inputs are copied so
// the caller can run the Go port over the originals.
func cApplyMS(
	msMaskPresent uint8,
	msUsed []uint8,
	spectrumL, spectrumR []int32,
	sfbLeftScale, sfbRightScale []int16,
	sfbOffsets []int16,
	windowGroupLength []uint8,
	windowGroups int,
	maxSfbSteOutside int,
	scaleFactorBandsTransmittedL int,
	scaleFactorBandsTransmittedR int,
	granuleLength int,
) cApplyMSResult {
	out := cApplyMSResult{
		spectrumL:  append([]int32(nil), spectrumL...),
		spectrumR:  append([]int32(nil), spectrumR...),
		leftScale:  append([]int16(nil), sfbLeftScale...),
		rightScale: append([]int16(nil), sfbRightScale...),
		msUsed:     append([]uint8(nil), msUsed...),
	}

	C.fparity_apply_ms(
		C.uint8_t(msMaskPresent),
		(*C.uint8_t)(unsafe.Pointer(&out.msUsed[0])),
		(*C.int32_t)(unsafe.Pointer(&out.spectrumL[0])),
		(*C.int32_t)(unsafe.Pointer(&out.spectrumR[0])),
		(*C.int16_t)(unsafe.Pointer(&out.leftScale[0])),
		(*C.int16_t)(unsafe.Pointer(&out.rightScale[0])),
		(*C.int16_t)(unsafe.Pointer(&sfbOffsets[0])),
		(*C.uint8_t)(unsafe.Pointer(&windowGroupLength[0])),
		C.int(windowGroups),
		C.int(maxSfbSteOutside),
		C.int(scaleFactorBandsTransmittedL),
		C.int(scaleFactorBandsTransmittedR),
		C.int(granuleLength),
	)

	return out
}
