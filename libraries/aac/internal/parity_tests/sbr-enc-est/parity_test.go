// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencest

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goInvf mirrors estparity_invf using the pure-Go port.
func goInvf(quotaFlat []int32, nEst, qmfChannels int, nrgVector []int32,
	indexVector []int8, freqBandTableDetector []int32, numDetectorBands, useSpeech,
	startIndex, stopIndex int, transientFlags []int32, nFrames int) []int32 {

	var h sbr.SbrInvFiltEst
	fbt := make([]int, numDetectorBands+1)
	for i := range fbt {
		fbt[i] = int(freqBandTableDetector[i])
	}
	sbr.InitInvFiltDetector(&h, fbt, numDetectorBands, uint(useSpeech))

	quota := make([][]int32, nEst)
	for e := 0; e < nEst; e++ {
		quota[e] = quotaFlat[e*qmfChannels : e*qmfChannels+qmfChannels]
	}

	out := make([]int32, nFrames*numDetectorBands)
	for f := 0; f < nFrames; f++ {
		infVec := make([]sbr.InvfMode, sbr.MaxNumNoiseValues())
		sbr.QmfInverseFilteringDetector(&h, quota, nrgVector, indexVector, startIndex,
			stopIndex, int(transientFlags[f]), infVec)
		for b := 0; b < numDetectorBands; b++ {
			out[f*numDetectorBands+b] = int32(infVec[b])
		}
	}
	return out
}

// goNf mirrors estparity_nf using the pure-Go port.
func goNf(quotaFlat []int32, nEst, qmfChannels int, indexVector []int8,
	freqBandTable []uint8, nSfb, anaMaxLevel, noiseBands, noiseFloorOffset, timeSlots,
	useSpeech, missingHarmonicsFlag, startIndex, numEst int, transientFrames []int32,
	invfLevelsFlat []int32, nNoiseEnvelopes, nFrames int) (noiseLevels []int32, noNoiseBands int) {

	var h sbr.SbrNoiseFloorEstimate
	sbr.InitSbrNoiseFloorEstimate(&h, anaMaxLevel, freqBandTable, nSfb, noiseBands,
		noiseFloorOffset, timeSlots, uint(useSpeech))
	noNoiseBands = h.NoNoiseBands

	quota := make([][]int32, nEst)
	for e := 0; e < nEst; e++ {
		quota[e] = quotaFlat[e*qmfChannels : e*qmfChannels+qmfChannels]
	}

	var frameInfo sbr.SbrFrameInfo
	frameInfo.NNoiseEnvelopes = nNoiseEnvelopes

	noiseLevels = make([]int32, maxNumNoiseValuesC)
	for f := 0; f < nFrames; f++ {
		invfLevels := make([]sbr.InvfMode, sbr.MaxNumNoiseValues())
		for b := 0; b < noNoiseBands; b++ {
			invfLevels[b] = sbr.InvfMode(invfLevelsFlat[f*maxNumNoiseValuesC+b])
		}
		nl := make([]int32, maxNumNoiseValuesC)
		sbr.SbrNoiseFloorEstimateQmf(&h, &frameInfo, nl, quota, indexVector,
			missingHarmonicsFlag, startIndex, uint(numEst), int(transientFrames[f]),
			invfLevels, 0)
		if f == nFrames-1 {
			noiseLevels = nl
		}
	}
	return noiseLevels[:nNoiseEnvelopes*noNoiseBands], noNoiseBands
}

func buildInputs(rng *rand.Rand, nEst, qmfChannels int) (quota []int32, nrg []int32, index []int8) {
	quota = make([]int32, nEst*qmfChannels)
	for i := range quota {
		// tonality quota in [0, 0.95) Q31
		quota[i] = int32(rng.Float64() * 0.95 * float64(1<<31))
	}
	nrg = make([]int32, nEst)
	for i := range nrg {
		nrg[i] = int32(rng.Float64() * 0.5 * float64(1<<31))
	}
	index = make([]int8, qmfChannels)
	for i := range index {
		// map highband channel to a lowband source (a few -1 guards)
		if i < 2 {
			index[i] = -1
		} else {
			index[i] = int8(rng.Intn(qmfChannels / 2))
		}
	}
	return
}

func TestInvfDetectorParity(t *testing.T) {
	type tc struct {
		name                                      string
		nEst, qmfChannels, numDetectorBands       int
		useSpeech, startIndex, stopIndex, nFrames int
	}
	tcs := []tc{
		{"aac_2bands", 4, 32, 2, 0, 0, 4, 3},
		{"speech_2bands", 4, 32, 2, 1, 0, 4, 3},
		{"aac_3bands", 4, 40, 3, 0, 0, 4, 4},
		{"aac_1band", 2, 24, 1, 0, 0, 2, 2},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(0xA11 + c.numDetectorBands)))
			quota, nrg, index := buildInputs(rng, c.nEst, c.qmfChannels)
			// detector band table spanning [low, qmfChannels)
			fbt := make([]int32, c.numDetectorBands+1)
			lo := 4
			for i := 0; i <= c.numDetectorBands; i++ {
				fbt[i] = int32(lo + i*((c.qmfChannels-lo)/c.numDetectorBands))
			}
			fbt[c.numDetectorBands] = int32(c.qmfChannels)
			tf := make([]int32, c.nFrames)
			for i := range tf {
				tf[i] = int32(rng.Intn(2))
			}
			cOut := cInvf(quota, c.nEst, c.qmfChannels, nrg, index, fbt, c.numDetectorBands,
				c.useSpeech, c.startIndex, c.stopIndex, tf, c.nFrames)
			gOut := goInvf(quota, c.nEst, c.qmfChannels, nrg, index, fbt, c.numDetectorBands,
				c.useSpeech, c.startIndex, c.stopIndex, tf, c.nFrames)
			require.Equal(t, cOut, gOut, "infVec across frames")
		})
	}
}

func TestNoiseFloorParity(t *testing.T) {
	type tc struct {
		name                                                               string
		nEst, qmfChannels, nSfb, anaMaxLevel, noiseBands, noiseFloorOffset int
		timeSlots, useSpeech, missingHarm, startIndex, numEst              int
		nNoiseEnv, nFrames                                                 int
	}
	tcs := []tc{
		{"aac_1env", 4, 32, 10, 6, 1, 0, 16, 0, 0, 0, 2, 1, 3},
		{"aac_2env", 4, 32, 10, 6, 2, 0, 16, 0, 0, 0, 2, 2, 4},
		{"speech_missingharm", 4, 32, 10, 3, 1, 0, 16, 1, 1, 0, 2, 1, 3},
		{"aac_2band_offset", 4, 40, 12, 6, 2, 0, 16, 0, 0, 0, 2, 2, 3},
	}
	for _, c := range tcs {
		t.Run(c.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(0xB22 + c.nSfb)))
			quota, _, index := buildInputs(rng, c.nEst, c.qmfChannels)
			// freqBandTable[0..nSfb] over the highband range
			fbt := make([]uint8, c.nSfb+1)
			lo := 4
			for i := 0; i <= c.nSfb; i++ {
				v := lo + i*((c.qmfChannels-lo)/c.nSfb)
				if v >= c.qmfChannels {
					v = c.qmfChannels - 1
				}
				fbt[i] = uint8(v)
			}
			fbt[c.nSfb] = uint8(c.qmfChannels)
			tf := make([]int32, c.nFrames)
			inv := make([]int32, c.nFrames*maxNumNoiseValuesC)
			for f := 0; f < c.nFrames; f++ {
				tf[f] = int32(rng.Intn(2))
				for b := 0; b < maxNumNoiseValuesC; b++ {
					inv[f*maxNumNoiseValuesC+b] = int32(rng.Intn(4))
				}
			}
			cLvls, cNB := cNf(quota, c.nEst, c.qmfChannels, index, fbt, c.nSfb, c.anaMaxLevel,
				c.noiseBands, c.noiseFloorOffset, c.timeSlots, c.useSpeech, c.missingHarm,
				c.startIndex, c.numEst, tf, inv, c.nNoiseEnv, c.nFrames)
			gLvls, gNB := goNf(quota, c.nEst, c.qmfChannels, index, fbt, c.nSfb, c.anaMaxLevel,
				c.noiseBands, c.noiseFloorOffset, c.timeSlots, c.useSpeech, c.missingHarm,
				c.startIndex, c.numEst, tf, inv, c.nNoiseEnv, c.nFrames)
			require.Equal(t, cNB, gNB, "noNoiseBands")
			assert.Equal(t, cLvls, gLvls, "quantised noise levels")
		})
	}
}
