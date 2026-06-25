// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// packFrameInfoForMH packs the SbrFrameInfo into the 26-int layout the bridge's
// mhparity_run reads (only the first 17 fields matter to the detector, but the
// stride mirrors fram_gen's packing so the same helper is reusable).
func packFrameInfoForMH(fi *sbr.SbrFrameInfo) []int32 {
	row := make([]int32, fiStride)
	row[0] = int32(fi.NEnvelopes)
	for i := 0; i < 6; i++ {
		row[1+i] = int32(fi.Borders[i])
	}
	for i := 0; i < 5; i++ {
		row[7+i] = int32(fi.FreqRes[i])
	}
	row[12] = int32(fi.ShortEnv)
	row[13] = int32(fi.NNoiseEnvelopes)
	for i := 0; i < 3; i++ {
		row[14+i] = int32(fi.BordersNoise[i])
	}
	return row
}

func TestMissingHarmonicsDetectorParity(t *testing.T) {
	// HE-AAC v1, NUMBER_TIME_SLOTS_2048: totNoEst=4, noEstPerFrame=2, move=2
	// (ton_corr.cpp:744-766). qmfChannels covers the SBR band range.
	const (
		sampleFreq    = 44100
		frameSize     = 2048
		qmfChannels   = 32
		totNoEst      = 4
		move          = 2
		noEstPerFrame = 2
		nSfb          = 6
	)

	rng := rand.New(rand.NewSource(0xBEEF))

	// freqBandTable: nSfb+1 monotonically increasing QMF band edges within
	// qmfChannels, starting above a lowband offset.
	freqBandTable := make([]uint8, nSfb+1)
	freqBandTable[0] = 8
	for i := 1; i <= nSfb; i++ {
		freqBandTable[i] = freqBandTable[i-1] + uint8(2+rng.Intn(2))
	}
	hiBand := int(freqBandTable[nSfb])
	require.Less(t, hiBand+2, qmfChannels, "freqBandTable must fit with +2 lookahead")

	// indexVector: SBR band k maps back to a source (lowband) band; keep it a
	// valid index < qmfChannels (a simple downward patch).
	indexVector := make([]int8, qmfChannels)
	for k := 0; k < qmfChannels; k++ {
		src := k - 6
		if src < 0 {
			src = k
		}
		indexVector[k] = int8(src)
	}

	const nFrames = 8

	// Build per-frame quota/sign/nrg + frame infos + transient infos.
	quotaPerFrame := make([][]int32, nFrames)
	signPerFrame := make([][]int32, nFrames)
	nrgPerFrame := make([][]int32, nFrames)
	frameInfos := make([]*sbr.SbrFrameInfo, nFrames)
	tranInfos := make([][]uint8, nFrames)

	quotaFlat := make([]int32, 0, nFrames*totNoEst*qmfChannels)
	signFlat := make([]int32, 0, nFrames*totNoEst*qmfChannels)
	nrgFlat := make([]int32, 0, nFrames*qmfChannels)
	frameInfoPacked := make([]int32, 0, nFrames*fiStride)
	tranFlat := make([]uint8, 0, nFrames*3)

	for f := 0; f < nFrames; f++ {
		q := make([]int32, totNoEst*qmfChannels)
		s := make([]int32, totNoEst*qmfChannels)
		for i := range q {
			// Tonality values are non-negative FIXP_DBL; vary magnitude so some
			// exceed the detection thresholds (RELAXATION-scaled, small).
			q[i] = int32(rng.Uint32() >> uint(3+rng.Intn(20)))
			if rng.Intn(2) == 0 {
				s[i] = 1
			} else {
				s[i] = -1
			}
		}
		nrg := make([]int32, qmfChannels)
		for i := range nrg {
			nrg[i] = int32(rng.Uint32() >> uint(2+rng.Intn(8)))
		}

		// Frame info: a simple FIXFIX-style grid (nEnvelopes=1 or 2), borders
		// inside [0, timeSlots=16].
		fi := new(sbr.SbrFrameInfo)
		if rng.Intn(2) == 0 {
			fi.NEnvelopes = 1
			fi.Borders = [6]int{0, 16}
		} else {
			fi.NEnvelopes = 2
			fi.Borders = [6]int{0, 8, 16}
		}
		fi.NNoiseEnvelopes = 1

		// Transient info {pos, flag, _}.
		flag := 0
		pos := 0
		if rng.Intn(3) == 0 {
			flag = 1
			pos = rng.Intn(14)
		}
		ti := []uint8{uint8(pos), uint8(flag), 0}

		quotaPerFrame[f] = q
		signPerFrame[f] = s
		nrgPerFrame[f] = nrg
		frameInfos[f] = fi
		tranInfos[f] = ti

		quotaFlat = append(quotaFlat, q...)
		signFlat = append(signFlat, s...)
		nrgFlat = append(nrgFlat, nrg...)
		frameInfoPacked = append(frameInfoPacked, packFrameInfoForMH(fi)...)
		tranFlat = append(tranFlat, ti...)
	}

	g := sbr.RunMissingHarmonicsDetector(false, sampleFreq, frameSize, nSfb, qmfChannels, totNoEst, move, noEstPerFrame, nFrames, quotaPerFrame, signPerFrame, indexVector, frameInfos, tranInfos, nrgPerFrame, freqBandTable)

	cFlags, cAddHarm, cEnvComp := cMHDetector(0, sampleFreq, frameSize, nSfb, qmfChannels, totNoEst, move, noEstPerFrame, nFrames, quotaFlat, signFlat, indexVector, frameInfoPacked, tranFlat, nrgFlat, freqBandTable)

	for f := 0; f < nFrames; f++ {
		require.Equal(t, int(cFlags[f]), g[f].AddHarmFlag, "frame %d addHarmFlag", f)
		require.Equal(t, cAddHarm[f*nSfb:f*nSfb+nSfb], g[f].AddHarmSfb, "frame %d addHarmSfb", f)
		require.Equal(t, cEnvComp[f*nSfb:f*nSfb+nSfb], g[f].EnvComp, "frame %d envComp", f)
	}
}
