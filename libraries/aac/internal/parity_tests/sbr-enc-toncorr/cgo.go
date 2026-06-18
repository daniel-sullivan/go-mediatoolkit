// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrenctoncorr pins the Go port of the SBR-encoder tonality-correction
// parameter extraction (internal/nativeaac/sbr/enc_ton_corr.go) against the
// vendored Fraunhofer FDK-AAC C (ton_corr.cpp) via cgo. Two isolated drivers:
// FDKsbrEnc_CalculateTonalityQuotas (the 2nd-order-LPC quota estimation) and the
// file-static resetPatch (the high-band patching + index vector). Deterministic
// inputs are fed to both the genuine and Go funcs and the full output state is
// compared bit-for-bit.
//
// Compiles its OWN copy of ton_corr + invf_est + nf_est + mh_det + sbr_misc +
// sbrenc_ram + the libFDK kernels (autocorr2nd / fixpoint_math / scale /
// FDK_tools_rom) + genericStds; NEVER imports libraries/aac. aacfdk fenced;
// fixed-point => EXACT int equality.
package sbrenctoncorr

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

extern void toncorr_quotas(int lpcLen0, int lpcLen1, int stepSize, int nextSample,
                           int move, int startIndexMatrix, int numberOfEstimates,
                           int numberOfEstimatesPerFrame, int noQmfChannels, int buffLen,
                           int usb, int qmfScale, int srcStride,
                           const int32_t *quotaIn, const int32_t *signIn,
                           const int32_t *nrgIn,
                           const int32_t *srcReal, const int32_t *srcImag,
                           int32_t *quotaOut, int32_t *signOut, int32_t *nrgOut,
                           int32_t *nrgFreqOut);

extern void toncorr_patch(int xposctrl, int highBandStartSb,
                          const unsigned char *vKMaster, int numMaster, int fs,
                          int noChannels, int guard, int shiftStartSb,
                          int *patchOut, signed char *indexOut, int *noOfPatchesOut);
*/
import "C"

import "unsafe"

// cQuotas drives FDKsbrEnc_CalculateTonalityQuotas with the given seeded state +
// QMF source and returns the full post-call quota/sign matrices (4*64),
// nrgVector (4) and nrgVectorFreq (64).
func cQuotas(lpcLen0, lpcLen1, stepSize, nextSample, move, startIndexMatrix,
	numberOfEstimates, numberOfEstimatesPerFrame, noQmfChannels, buffLen, usb, qmfScale, srcStride int,
	quotaIn, signIn, nrgIn, srcReal, srcImag []int32) (quotaOut, signOut, nrgOut, nrgFreqOut []int32) {

	quotaOut = make([]int32, 4*64)
	signOut = make([]int32, 4*64)
	nrgOut = make([]int32, 4)
	nrgFreqOut = make([]int32, 64)

	C.toncorr_quotas(C.int(lpcLen0), C.int(lpcLen1), C.int(stepSize), C.int(nextSample),
		C.int(move), C.int(startIndexMatrix), C.int(numberOfEstimates),
		C.int(numberOfEstimatesPerFrame), C.int(noQmfChannels), C.int(buffLen),
		C.int(usb), C.int(qmfScale), C.int(srcStride),
		(*C.int32_t)(unsafe.Pointer(&quotaIn[0])), (*C.int32_t)(unsafe.Pointer(&signIn[0])),
		(*C.int32_t)(unsafe.Pointer(&nrgIn[0])),
		(*C.int32_t)(unsafe.Pointer(&srcReal[0])), (*C.int32_t)(unsafe.Pointer(&srcImag[0])),
		(*C.int32_t)(unsafe.Pointer(&quotaOut[0])), (*C.int32_t)(unsafe.Pointer(&signOut[0])),
		(*C.int32_t)(unsafe.Pointer(&nrgOut[0])), (*C.int32_t)(unsafe.Pointer(&nrgFreqOut[0])))
	return
}

// cPatch drives the file-static resetPatch (via the TU tap) and returns the
// patchParam table (6 patches × 6 ints), the index vector (64) and noOfPatches.
func cPatch(xposctrl, highBandStartSb int, vKMaster []uint8, numMaster, fs, noChannels, guard, shiftStartSb int) (patch []int32, index []int8, noOfPatches int) {
	patchC := make([]C.int, 6*6)
	idxC := make([]C.schar, 64)
	var noP C.int
	C.toncorr_patch(C.int(xposctrl), C.int(highBandStartSb),
		(*C.uchar)(unsafe.Pointer(&vKMaster[0])), C.int(numMaster), C.int(fs),
		C.int(noChannels), C.int(guard), C.int(shiftStartSb),
		&patchC[0], &idxC[0], &noP)

	patch = make([]int32, 6*6)
	for i := range patchC {
		patch[i] = int32(patchC[i])
	}
	index = make([]int8, 64)
	for i := range idxC {
		index[i] = int8(idxC[i])
	}
	return patch, index, int(noP)
}
