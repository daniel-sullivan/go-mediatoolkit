// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psencbitwrite pins the Go port of the Fraunhofer FDK-AAC parametric
// stereo ps_data() bitstream WRITER (libSBRenc/src/ps_bitenc.cpp —
// FDKsbrEnc_WritePSBitstream, in internal/nativeaac/sbr) against the vendored C
// via cgo. An identical PS_OUT is built on both sides and the emitted ps_data()
// bytes + bit count are compared byte-for-byte.
//
// Compiles its OWN copy of the needed vendored C (ps_bitenc + ps_encode +
// FDK_bitbuffer + genericStds), NEVER imports libraries/aac. Fenced behind
// aacfdk. Fixed-point => EXACT byte equality.
package psencbitwrite

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

extern void psbw_run(
    int enablePSHeader, int enableIID, int iidMode, int enableICC, int iccMode,
    int frameClass, int nEnvelopes, const int *frameBorder, const int *deltaIID,
    const int *deltaICC, const int *iidFlat, const int *iccFlat,
    const int *iidLast, const int *iccLast, unsigned char *outBytes,
    int *nBytes, int *nBits);
*/
import "C"

import "unsafe"

type psOutScenario struct {
	enablePSHeader, enableIID, iidMode, enableICC, iccMode int
	frameClass, nEnvelopes                                 int
	frameBorder                                            [4]int32
	deltaIID, deltaICC                                     [4]int32
	iidFlat, iccFlat                                       [80]int32 // 4*20
	iidLast, iccLast                                       [20]int32
}

func cWritePS(s psOutScenario) (payload []byte, nBits int) {
	out := make([]byte, 512)
	var nb, nbits C.int

	fb := i32arr4(s.frameBorder)
	dIid := i32arr4(s.deltaIID)
	dIcc := i32arr4(s.deltaICC)
	iidF := i32arr80(s.iidFlat)
	iccF := i32arr80(s.iccFlat)
	iidL := i32arr20(s.iidLast)
	iccL := i32arr20(s.iccLast)

	C.psbw_run(C.int(s.enablePSHeader), C.int(s.enableIID), C.int(s.iidMode),
		C.int(s.enableICC), C.int(s.iccMode), C.int(s.frameClass),
		C.int(s.nEnvelopes), &fb[0], &dIid[0], &dIcc[0], &iidF[0], &iccF[0],
		&iidL[0], &iccL[0], (*C.uchar)(unsafe.Pointer(&out[0])), &nb, &nbits)

	return out[:int(nb)], int(nbits)
}

func i32arr4(s [4]int32) []C.int {
	c := make([]C.int, 4)
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}

func i32arr20(s [20]int32) []C.int {
	c := make([]C.int, 20)
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}

func i32arr80(s [80]int32) []C.int {
	c := make([]C.int, 80)
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}
