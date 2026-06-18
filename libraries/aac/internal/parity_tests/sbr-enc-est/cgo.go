// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Package sbrencest pins the Go ports of the SBR-encoder inverse-filtering
// detector (invf_est.cpp) and noise-floor estimator (nf_est.cpp), in
// internal/nativeaac/sbr, against the vendored C via cgo. Synthetic but
// deterministic quota/energy/index inputs are fed to both the genuine and Go
// funcs (inited identically, run across frames to exercise the smoothing +
// hysteresis state) and the per-band INVF modes + quantised noise levels are
// compared bit-for-bit.
//
// Compiles its OWN copy of invf_est + nf_est + sbr_misc + fixpoint_math +
// FDK_tools_rom + scale + genericStds; NEVER imports libraries/aac. aacfdk
// fenced; fixed-point => EXACT int equality.
package sbrencest

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

extern void estparity_invf(const int32_t *quotaFlat, int nEst, int qmfChannels,
                           const int32_t *nrgVector, const signed char *indexVector,
                           const int *freqBandTableDetector, int numDetectorBands,
                           int useSpeech, int startIndex, int stopIndex,
                           const int *transientFlags, int nFrames, int *infVecOut);

extern void estparity_nf(const int32_t *quotaFlat, int nEst, int qmfChannels,
                         const signed char *indexVector, const unsigned char *freqBandTable,
                         int nSfb, int anaMaxLevel, int noiseBands, int noiseFloorOffset,
                         int timeSlots, int useSpeech, int missingHarmonicsFlag,
                         int startIndex, int numberOfEstimatesPerFrame,
                         const int *transientFrames, const int *invfLevelsFlat,
                         int nNoiseEnvelopes, int nFrames, int32_t *noiseLevelsOut,
                         int *noNoiseBandsOut);
*/
import "C"

import "unsafe"

const maxNumNoiseValuesC = 10 // MAX_NUM_NOISE_VALUES (sbr_def.h)

func cInvf(quotaFlat []int32, nEst, qmfChannels int, nrgVector []int32,
	indexVector []int8, freqBandTableDetector []int32, numDetectorBands, useSpeech,
	startIndex, stopIndex int, transientFlags []int32, nFrames int) []int32 {

	out := make([]int32, nFrames*numDetectorBands)
	fbt := i32cints(freqBandTableDetector)
	tf := i32cints(transientFlags)
	C.estparity_invf((*C.int32_t)(unsafe.Pointer(&quotaFlat[0])), C.int(nEst),
		C.int(qmfChannels), (*C.int32_t)(unsafe.Pointer(&nrgVector[0])),
		(*C.schar)(unsafe.Pointer(&indexVector[0])), &fbt[0], C.int(numDetectorBands),
		C.int(useSpeech), C.int(startIndex), C.int(stopIndex), &tf[0], C.int(nFrames),
		(*C.int)(unsafe.Pointer(&out[0])))
	return out
}

func cNf(quotaFlat []int32, nEst, qmfChannels int, indexVector []int8,
	freqBandTable []uint8, nSfb, anaMaxLevel, noiseBands, noiseFloorOffset, timeSlots,
	useSpeech, missingHarmonicsFlag, startIndex, numEst int, transientFrames []int32,
	invfLevelsFlat []int32, nNoiseEnvelopes, nFrames int) (noiseLevels []int32, noNoiseBands int) {

	out := make([]int32, maxNumNoiseValuesC)
	var nnb C.int
	tf := i32cints(transientFrames)
	inv := i32cints(invfLevelsFlat)
	C.estparity_nf((*C.int32_t)(unsafe.Pointer(&quotaFlat[0])), C.int(nEst),
		C.int(qmfChannels), (*C.schar)(unsafe.Pointer(&indexVector[0])),
		(*C.uchar)(unsafe.Pointer(&freqBandTable[0])), C.int(nSfb), C.int(anaMaxLevel),
		C.int(noiseBands), C.int(noiseFloorOffset), C.int(timeSlots), C.int(useSpeech),
		C.int(missingHarmonicsFlag), C.int(startIndex), C.int(numEst), &tf[0], &inv[0],
		C.int(nNoiseEnvelopes), C.int(nFrames), (*C.int32_t)(unsafe.Pointer(&out[0])), &nnb)
	noNoiseBands = int(nnb)
	return out[:nNoiseEnvelopes*noNoiseBands], noNoiseBands
}

func i32cints(s []int32) []C.int {
	c := make([]C.int, len(s))
	for i, v := range s {
		c[i] = C.int(v)
	}
	return c
}
