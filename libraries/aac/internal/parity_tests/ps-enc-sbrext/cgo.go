// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package psencsbrext pins the Go port of the HE-AAC v2 SBR-extension wrapper
// that carries ps_data() — FDKsbrEnc_WriteEnvSingleChannelElement with a
// non-nil hParametricStereo, routing through getSbrExtendedDataSize +
// encodeExtendedData + FDKsbrEnc_PSEnc_WritePSData (in internal/nativeaac/sbr) —
// against the vendored C via cgo. An identical SBR SCE + PS_OUT is built on both
// sides and the emitted SBR payload bytes + bit count compared byte-for-byte.
//
// Compiles its OWN copy of the needed vendored C (bit_sbr + code_env +
// sbrenc_rom + ps_bitenc + FDK_bitbuffer + FDK_tools_rom + fixpoint_math +
// scale + genericStds), with FDKsbrEnc_PSEnc_WritePSData stubbed in the bridge
// (ps_main.cpp surrogate). NEVER imports libraries/aac. Fenced behind aacfdk.
// Fixed-point => EXACT byte equality.
package psencsbrext

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

extern void sbrext_run(
    int ampRes, int headerActive, int startFreq, int stopFreq, int xoverBand,
    int noOfEnvelopes, int noOfnoisebands, int bsNumEnv, int numberTimeSlots,
    const int *noScfBands, const int *ienvelopeFlat, const signed char *noiseLevels,
    const int *invfMode, int psHeader, int enIID, int iidMode, int enICC,
    int iccMode, int frameClass, int psNEnv, const int *frameBorder,
    const int *deltaIID, const int *deltaICC, const int *iidFlat,
    const int *iccFlat, const int *iidLast, const int *iccLast,
    unsigned char *outBytes, int *nBytes, int *nBits);
*/
import "C"

import "unsafe"

const maxFreqCoeffsC = 48 // MAX_FREQ_COEFFS

type sbrextScenario struct {
	ampRes, headerActive, startFreq, stopFreq, xoverBand     int
	noOfEnvelopes, noOfnoisebands, bsNumEnv, numberTimeSlots int
	noScfBands                                               []int32
	ienvelopeFlat                                            []int32 // noOfEnvelopes*maxFreqCoeffs
	noiseLevels                                              []int8  // maxFreqCoeffs
	invfMode                                                 []int32

	psHeader, enIID, iidMode, enICC, iccMode, frameClass, psNEnv int
	frameBorder                                                  [4]int32
	deltaIID, deltaICC                                           [4]int32
	iidFlat, iccFlat                                             [80]int32
	iidLast, iccLast                                             [20]int32
}

func cRun(s sbrextScenario) (payload []byte, nBits int) {
	out := make([]byte, 512)
	var nb, nbits C.int

	noScf := i32slice(s.noScfBands)
	ienv := i32slice(s.ienvelopeFlat)
	invf := i32slice(s.invfMode)
	fb := i32arr(s.frameBorder[:])
	dIid := i32arr(s.deltaIID[:])
	dIcc := i32arr(s.deltaICC[:])
	iidF := i32arr(s.iidFlat[:])
	iccF := i32arr(s.iccFlat[:])
	iidL := i32arr(s.iidLast[:])
	iccL := i32arr(s.iccLast[:])

	C.sbrext_run(C.int(s.ampRes), C.int(s.headerActive), C.int(s.startFreq),
		C.int(s.stopFreq), C.int(s.xoverBand), C.int(s.noOfEnvelopes),
		C.int(s.noOfnoisebands), C.int(s.bsNumEnv), C.int(s.numberTimeSlots),
		&noScf[0], &ienv[0], (*C.schar)(unsafe.Pointer(&s.noiseLevels[0])), &invf[0],
		C.int(s.psHeader), C.int(s.enIID), C.int(s.iidMode), C.int(s.enICC),
		C.int(s.iccMode), C.int(s.frameClass), C.int(s.psNEnv), &fb[0], &dIid[0],
		&dIcc[0], &iidF[0], &iccF[0], &iidL[0], &iccL[0],
		(*C.uchar)(unsafe.Pointer(&out[0])), &nb, &nbits)

	return out[:int(nb)], int(nbits)
}

func i32slice(s []int32) []C.int {
	c := make([]C.int, len(s))
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}

func i32arr(s []int32) []C.int {
	c := make([]C.int, len(s))
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}
