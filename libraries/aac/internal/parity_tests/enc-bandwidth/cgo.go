// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package encbandwidth pins the Go port of the Fraunhofer FDK-AAC encoder
// bandwidth expert — FDKaacEnc_DetermineBandWidth and its static helper
// GetBandwidthEntry (libAACenc/src/bandwidth.cpp) — against the genuine vendored
// C, compiled into this test binary via cgo.
//
// DetermineBandWidth maps (proposed bandwidth, bitrate, bitrate mode, sample
// rate, frame length, channel mode) onto the coded audio bandwidth. On the
// AAC-LC CBR path GetBandwidthEntry takes the plain bandWidthTable lookup for
// frame length 1024/960; the low-delay interpolation branch (fDivNorm/fMult/
// scaleValue) is exercised too by feeding the LD frame lengths/sample rates, so
// the ROM tables and the fixed-point interpolation are both covered. Every value
// is INT; the result and the AAC_ENCODER_ERROR code are compared bit-for-bit.
//
// This package compiles its OWN copy of the needed vendored C source
// (bandwidth.cpp via the bridge + fixpoint_math.cpp for the LD-interpolation
// kernels) and NEVER imports libraries/aac — importing it would link a second
// copy of the FDK reference and clash on static symbols (the same amalgamation
// reason the sibling parity packages document). It MAY, and does, import the
// pure-Go internal/nativeaac.
//
// The whole AAC island is fenced behind the opt-in aacfdk build tag; a default
// `go build ./...` links none of it. See libfdk/COPYING for the Fraunhofer
// FDK-AAC license.
//
// Integer parity: libfdk-aac ENCODE is FIXED-POINT — DetermineBandWidth is
// integer/table logic with a fixed-point fDivNorm interpolation, bit-identical
// regardless of -ffp-contract / vectorization, no float, no transcendental. So
// it asserts EXACT int equality. The oracle links the genuine
// FDKaacEnc_DetermineBandWidth + GetBandwidthEntry (oracle_kind == real_vendored)
// through the thin extern shims in bridge.cpp — no hand-twin re-derivation.
package encbandwidth

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

extern int bparity_determine_bandwidth(int proposedBandWidth, int bitrate,
    int bitrateMode, int sampleRate, int frameLength, int nChannelsEff,
    int encoderMode, int *bandWidthOut);
extern int bparity_get_bandwidth_entry(int frameLength, int sampleRate,
    int chanBitRate, int entryNo);
*/
import "C"

// cDetermineBandWidth runs the genuine FDKaacEnc_DetermineBandWidth, returning
// (bandWidth, errorCode).
func cDetermineBandWidth(proposedBandWidth, bitrate, bitrateMode, sampleRate,
	frameLength, nChannelsEff, encoderMode int) (int, int) {
	var bw C.int
	err := C.bparity_determine_bandwidth(C.int(proposedBandWidth), C.int(bitrate),
		C.int(bitrateMode), C.int(sampleRate), C.int(frameLength), C.int(nChannelsEff),
		C.int(encoderMode), &bw)
	return int(bw), int(err)
}

// cGetBandwidthEntry runs the genuine static GetBandwidthEntry.
func cGetBandwidthEntry(frameLength, sampleRate, chanBitRate, entryNo int) int {
	return int(C.bparity_get_bandwidth_entry(C.int(frameLength), C.int(sampleRate),
		C.int(chanBitRate), C.int(entryNo)))
}
