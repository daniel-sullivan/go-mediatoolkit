// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrencbitwrite pins the Go port of the Fraunhofer FDK-AAC SBR
// extension-payload bitstream WRITER (libSBRenc/src/bit_sbr.cpp — the
// FDKsbrEnc_WriteEnvSingleChannelElement SCE path, in internal/nativeaac/sbr)
// against the vendored C via cgo. A fully-formed SBR_ENV_DATA + grid + header is
// built identically on both sides and the emitted SBR payload bytes + bit count
// are compared byte-for-byte.
//
// Compiles its OWN copy of the needed vendored C (bit_sbr + code_env +
// sbrenc_rom + fixpoint_math + FDK_tools_rom + FDK_bitbuffer + scale +
// genericStds), NEVER imports libraries/aac. Fenced behind aacfdk. Fixed-point
// => EXACT byte equality.
package sbrencbitwrite

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

extern void bwparity_run_sce(
    int ampRes, int headerActive, int headerExtra1, int headerExtra2,
    int sbrAmpRes, int startFreq, int stopFreq, int xoverBand, int freqScale,
    int alterScale, int noiseBands, int limiterBands, int limiterGains,
    int interpolFreq, int smoothingLength, int frameClass, int bsNumEnv,
    int vf0, int bufferFrameStart, int numberTimeSlots, int noOfEnvelopes,
    int noOfnoisebands, const int *noScfBands, const int *domainVec,
    const int *domainVecNoise, const int *ienvelopeFlat,
    const signed char *noiseLevels, const int *invfMode, int addHarmonicFlag,
    int noHarmonics, const unsigned char *addHarmonic, unsigned char *outBytes,
    int *nBytes, int *nBits);
*/
import "C"

import "unsafe"

const maxFreqCoeffsC = 48 // MAX_FREQ_COEFFS

type sceScenario struct {
	ampRes, headerActive, headerExtra1, headerExtra2                      int
	sbrAmpRes, startFreq, stopFreq, xoverBand, freqScale, alterScale      int
	noiseBands, limiterBands, limiterGains, interpolFreq, smoothingLength int
	frameClass, bsNumEnv, vf0, bufferFrameStart, numberTimeSlots          int
	noOfEnvelopes, noOfnoisebands                                         int
	noScfBands, domainVec                                                 []int32
	domainVecNoise                                                        [2]int32
	ienvelopeFlat                                                         []int32 // noOfEnvelopes*maxFreqCoeffs
	noiseLevels                                                           []int8  // maxFreqCoeffs
	invfMode                                                              []int32
	addHarmonicFlag, noHarmonics                                          int
	addHarmonic                                                           []uint8 // maxFreqCoeffs
}

func cWriteSCE(s sceScenario) (payload []byte, nBits int) {
	out := make([]byte, 512)
	var nb, nbits C.int

	noScf := i32slice(s.noScfBands)
	dv := i32slice(s.domainVec)
	dvn := []C.int{C.int(s.domainVecNoise[0]), C.int(s.domainVecNoise[1])}
	ienv := i32slice(s.ienvelopeFlat)
	invf := i32slice(s.invfMode)

	C.bwparity_run_sce(C.int(s.ampRes), C.int(s.headerActive), C.int(s.headerExtra1),
		C.int(s.headerExtra2), C.int(s.sbrAmpRes), C.int(s.startFreq), C.int(s.stopFreq),
		C.int(s.xoverBand), C.int(s.freqScale), C.int(s.alterScale), C.int(s.noiseBands),
		C.int(s.limiterBands), C.int(s.limiterGains), C.int(s.interpolFreq),
		C.int(s.smoothingLength), C.int(s.frameClass), C.int(s.bsNumEnv), C.int(s.vf0),
		C.int(s.bufferFrameStart), C.int(s.numberTimeSlots), C.int(s.noOfEnvelopes),
		C.int(s.noOfnoisebands), &noScf[0], &dv[0], &dvn[0], &ienv[0],
		(*C.schar)(unsafe.Pointer(&s.noiseLevels[0])), &invf[0],
		C.int(s.addHarmonicFlag), C.int(s.noHarmonics),
		(*C.uchar)(unsafe.Pointer(&s.addHarmonic[0])),
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
