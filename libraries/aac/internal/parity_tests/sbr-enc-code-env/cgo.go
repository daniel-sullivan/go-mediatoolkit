// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrenccodeenv pins the Go port of the Fraunhofer FDK-AAC SBR-ENCODER
// DPCM envelope/noise coder (libSBRenc/src/code_env.cpp — FDKsbrEnc_codeEnvelope
// / InitSbrCodeEnvelope / InitSbrHuffmanTables, in internal/nativeaac/sbr)
// against the vendored C, compiled into this test binary via cgo. The delta-
// coded scalefactor output, per-envelope direction vector, and persistent
// sfb_nrg_prev / upDate state are compared bit-for-bit across a multi-frame
// scenario (so the time-coding upDate path is exercised).
//
// This package compiles its OWN copy of the needed vendored C source (code_env
// plus its sbrenc_rom / fixpoint_math / FDK_tools_rom / scale / genericStds
// siblings) and NEVER imports libraries/aac. It MAY import the pure-Go
// internal/nativeaac/sbr port. Fenced behind the opt-in aacfdk build tag.
//
// Integer parity: the SBR encoder is FIXED-POINT — EXACT int equality, no
// aac_strict gate.
package sbrenccodeenv

/*
#cgo CXXFLAGS: -std=c++11 -w
#cgo CFLAGS:   -w
#cgo CPPFLAGS: -I${SRCDIR}/../../..
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/src
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSBRenc/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libFDK/include
#cgo CPPFLAGS: -I${SRCDIR}/../../../libfdk/libSYS/include
#cgo LDFLAGS: -lm

#include <stdint.h>

extern void ceparity_run(int ampRes, int nSfbLo, int nSfbHi, int deltaTAcross,
                         int coupling, int channel, int headerActive, int nFrames,
                         int nEnvPerFr, const int *freqResIn,
                         const signed char *sfbNrgIn, int isNoise,
                         signed char *sfbNrgOut, int *dirVecOut,
                         signed char *prevOut, int *upDateOut);
*/
import "C"

import "unsafe"

const maxFreqCoeffsC = 48 // MAX_FREQ_COEFFS

// cCodeEnvelope drives the genuine multi-frame codeEnvelope scenario and returns
// the delta-coded output, direction vector, final sfb_nrg_prev, and final upDate.
func cCodeEnvelope(ampRes, nSfbLo, nSfbHi, deltaTAcross, coupling, channel,
	headerActive, nFrames, nEnvPerFr int, freqResIn []int32, sfbNrgIn []int8,
	isNoise int) (sfbNrgOut []int8, dirVecOut []int32, prevOut []int8, upDate int) {

	sfbNrgOut = make([]int8, len(sfbNrgIn))
	dirVecOut = make([]int32, nFrames*nEnvPerFr)
	prevOut = make([]int8, maxFreqCoeffsC)
	var up C.int

	fr := make([]C.int, len(freqResIn))
	for i, v := range freqResIn {
		fr[i] = C.int(v)
	}

	C.ceparity_run(C.int(ampRes), C.int(nSfbLo), C.int(nSfbHi), C.int(deltaTAcross),
		C.int(coupling), C.int(channel), C.int(headerActive), C.int(nFrames),
		C.int(nEnvPerFr), &fr[0],
		(*C.schar)(unsafe.Pointer(&sfbNrgIn[0])), C.int(isNoise),
		(*C.schar)(unsafe.Pointer(&sfbNrgOut[0])),
		(*C.int)(unsafe.Pointer(&dirVecOut[0])),
		(*C.schar)(unsafe.Pointer(&prevOut[0])), &up)
	upDate = int(up)
	return
}
