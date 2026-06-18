// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

/*
#include <stdint.h>
extern void mhparity_run(int lowDelay, int sampleFreq, int frameSize, int nSfb,
                         int qmfChannels, int totNoEst, int move, int noEstPerFrame,
                         int nFrames, const int32_t *quotaFlat, const int32_t *signFlat,
                         const signed char *indexVector, const int *frameInfoPacked,
                         const unsigned char *tranInfo, const int32_t *nrgFlat,
                         const unsigned char *freqBandTable, int *addHarmFlagOut,
                         unsigned char *addHarmSfbOut, unsigned char *envCompOut);
*/
import "C"

import "unsafe"

// cMHDetector runs the genuine missing-harmonics detector over nFrames frames
// and returns per-frame addHarmFlag, addHarmSfb (nSfb each), envComp (nSfb each).
func cMHDetector(lowDelay int, sampleFreq, frameSize, nSfb, qmfChannels, totNoEst, move, noEstPerFrame, nFrames int, quotaFlat, signFlat []int32, indexVector []int8, frameInfoPacked []int32, tranInfo []uint8, nrgFlat []int32, freqBandTable []uint8) (flags []int32, addHarmSfb, envComp []uint8) {
	idx := make([]C.schar, len(indexVector))
	for i := range indexVector {
		idx[i] = C.schar(indexVector[i])
	}
	fip := make([]C.int, len(frameInfoPacked))
	for i := range frameInfoPacked {
		fip[i] = C.int(frameInfoPacked[i])
	}
	ti := make([]C.uchar, len(tranInfo))
	for i := range tranInfo {
		ti[i] = C.uchar(tranInfo[i])
	}
	fbt := make([]C.uchar, len(freqBandTable))
	for i := range freqBandTable {
		fbt[i] = C.uchar(freqBandTable[i])
	}

	flagsC := make([]C.int, nFrames)
	ahsC := make([]C.uchar, nFrames*nSfb)
	ecC := make([]C.uchar, nFrames*nSfb)

	C.mhparity_run(C.int(lowDelay), C.int(sampleFreq), C.int(frameSize), C.int(nSfb),
		C.int(qmfChannels), C.int(totNoEst), C.int(move), C.int(noEstPerFrame),
		C.int(nFrames),
		(*C.int32_t)(unsafe.Pointer(&quotaFlat[0])),
		(*C.int32_t)(unsafe.Pointer(&signFlat[0])),
		&idx[0], &fip[0], &ti[0],
		(*C.int32_t)(unsafe.Pointer(&nrgFlat[0])), &fbt[0],
		&flagsC[0], &ahsC[0], &ecC[0])

	flags = make([]int32, nFrames)
	for i := range flagsC {
		flags[i] = int32(flagsC[i])
	}
	addHarmSfb = make([]uint8, nFrames*nSfb)
	for i := range ahsC {
		addHarmSfb[i] = uint8(ahsC[i])
	}
	envComp = make([]uint8, nFrames*nSfb)
	for i := range ecC {
		envComp[i] = uint8(ecC[i])
	}
	return flags, addHarmSfb, envComp
}
