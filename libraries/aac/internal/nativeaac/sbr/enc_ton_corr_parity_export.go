// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parity-export shims for the SBR-encoder tonality-correction batch (ton_corr):
// thin views the sbr-enc-toncorr oracle uses to drive the file-static resetPatch
// and to set the scalar LPC params CalculateTonalityQuotas reads. No logic.
package sbr

// ResetPatchForParity drives the (unexported) resetPatch on a fresh
// SbrTonCorrEst seeded with the given guard / shiftStartSb, and returns the
// patch table (flattened 6×6: sourceStart, sourceStop, guardStart, targetStart,
// targetOffs, numBands), the index vector and noOfPatches — for exact-integer
// comparison against the vendored C.
func ResetPatchForParity(xposctrl, highBandStartSb int, vKMaster []uint8,
	numMaster, fs, noChannels, guard, shiftStartSb int) (patch []int32, index []int8, noOfPatches int) {

	var h SbrTonCorrEst
	h.Guard = guard
	h.ShiftStartSb = shiftStartSb
	h.resetPatch(xposctrl, highBandStartSb, vKMaster, numMaster, fs, noChannels)

	patch = make([]int32, 6*6)
	for p := 0; p < maxNumPatches; p++ {
		patch[p*6+0] = int32(h.PatchParam[p].SourceStartBand)
		patch[p*6+1] = int32(h.PatchParam[p].SourceStopBand)
		patch[p*6+2] = int32(h.PatchParam[p].GuardStartBand)
		patch[p*6+3] = int32(h.PatchParam[p].TargetStartBand)
		patch[p*6+4] = int32(h.PatchParam[p].TargetBandOffs)
		patch[p*6+5] = int32(h.PatchParam[p].NumBandsInPatch)
	}
	index = make([]int8, 64)
	copy(index, h.IndexVector[:])
	return patch, index, h.NoOfPatches
}

// CalculateTonalityQuotasForParity seeds a SbrTonCorrEst with the scalar LPC
// params (the ones InitTonCorrParamExtr would set) + the quota/sign/nrg state,
// runs CalculateTonalityQuotas over the given complex QMF source, and returns
// the full post-call quota/sign matrices (4×64 flattened), nrgVector (4) and
// nrgVectorFreq (64).
func CalculateTonalityQuotasForParity(lpcLen0, lpcLen1, stepSize, nextSample, move,
	startIndexMatrix, numberOfEstimates, numberOfEstimatesPerFrame, noQmfChannels,
	buffLen, usb, qmfScale, srcStride int,
	quotaIn, signIn, nrgIn, srcReal, srcImag []int32) (quotaOut, signOut, nrgOut, nrgFreqOut []int32) {

	var h SbrTonCorrEst
	h.LpcLength[0] = lpcLen0
	h.LpcLength[1] = lpcLen1
	h.StepSize = stepSize
	h.NextSample = nextSample
	h.Move = move
	h.StartIndexMatrix = startIndexMatrix
	h.NumberOfEstimates = numberOfEstimates
	h.NumberOfEstimatesPerFrame = numberOfEstimatesPerFrame
	h.NoQmfChannels = noQmfChannels
	h.BufferLength = buffLen

	for e := 0; e < maxNoOfEstimates; e++ {
		h.QuotaMatrix[e] = make([]int32, 64)
		h.SignMatrix[e] = make([]int32, 64)
		copy(h.QuotaMatrix[e], quotaIn[e*64:e*64+64])
		copy(h.SignMatrix[e], signIn[e*64:e*64+64])
	}
	copy(h.NrgVector[:], nrgIn)

	src := func(flat []int32) [][]int32 {
		rows := make([][]int32, buffLen)
		for i := 0; i < buffLen; i++ {
			rows[i] = flat[i*srcStride : i*srcStride+srcStride]
		}
		return rows
	}

	h.CalculateTonalityQuotas(src(srcReal), src(srcImag), usb, qmfScale)

	quotaOut = make([]int32, 4*64)
	signOut = make([]int32, 4*64)
	nrgOut = make([]int32, 4)
	nrgFreqOut = make([]int32, 64)
	for e := 0; e < maxNoOfEstimates; e++ {
		copy(quotaOut[e*64:e*64+64], h.QuotaMatrix[e])
		copy(signOut[e*64:e*64+64], h.SignMatrix[e])
		nrgOut[e] = h.NrgVector[e]
	}
	copy(nrgFreqOut, h.NrgVectorFreq[:])
	return
}
