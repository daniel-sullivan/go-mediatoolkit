// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// Exported driver for the sbr-enc-analysis missing-harmonics-detector parity
// slice: it inits the detector and runs N frames with the supplied quota/sign
// matrices, frame infos and transient infos, returning the per-frame decisions
// (addHarmFlag, addHarmonicsScaleFactorBands, envelopeCompensation) so the
// oracle can assert EXACT equality against the vendored
// FDKsbrEnc_SbrMissingHarmonicsDetectorQmf across the cross-frame guide state.

// MHResult is one frame's missing-harmonics decision.
type MHResult struct {
	AddHarmFlag int
	AddHarmSfb  []uint8
	EnvComp     []uint8
}

// RunMissingHarmonicsDetector inits the detector then runs nFrames frames.
// quotaFlat[f] is totNoEst*qmfChannels FIXP_DBL; signFlat likewise; frameInfos[f]
// the per-frame SbrFrameInfo; tranInfos[f] the 3-byte transient info; nrgFlat[f]
// qmfChannels energies. indexVector and freqBandTable are shared across frames.
func RunMissingHarmonicsDetector(lowDelay bool, sampleFreq, frameSize, nSfb, qmfChannels, totNoEst, move, noEstPerFrame, nFrames int, quotaFlat, signFlat [][]int32, indexVector []int8, frameInfos []*SbrFrameInfo, tranInfos [][]uint8, nrgFlat [][]int32, freqBandTable []uint8) []MHResult {
	var det SbrMissingHarmonicsDetector
	InitSbrMissingHarmonicsDetector(&det, lowDelay, sampleFreq, frameSize, nSfb, qmfChannels, totNoEst, move, noEstPerFrame)

	out := make([]MHResult, 0, nFrames)
	for f := 0; f < nFrames; f++ {
		quota := make([][]int32, totNoEst)
		sign := make([][]int32, totNoEst)
		for e := 0; e < totNoEst; e++ {
			quota[e] = quotaFlat[f][e*qmfChannels : e*qmfChannels+qmfChannels]
			sign[e] = signFlat[f][e*qmfChannels : e*qmfChannels+qmfChannels]
		}

		addHarmFlag := 0
		addHarmSfb := make([]uint8, nSfb)
		envComp := make([]uint8, nSfb)

		SbrMissingHarmonicsDetectorQmf(&det, quota, sign, indexVector, frameInfos[f], tranInfos[f], &addHarmFlag, addHarmSfb, freqBandTable, nSfb, envComp, nrgFlat[f])

		out = append(out, MHResult{AddHarmFlag: addHarmFlag, AddHarmSfb: addHarmSfb, EnvComp: envComp})
	}
	return out
}
