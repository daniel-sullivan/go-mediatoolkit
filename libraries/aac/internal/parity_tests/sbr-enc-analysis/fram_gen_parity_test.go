// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencanalysis

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// vTuningHEAAC is v_tuningHEAAC[6] (env_est.cpp:1155), the HE-AAC v1 frame-gen
// tuning vector (segment lengths {0,2,4}, freq resolutions {0,0,0}).
var vTuningHEAAC = []int{0, 2, 4, 0, 0, 0}

// flatFrameInfo flattens a Go FrameInfoResult into the same int32 layout the C
// bridge packs (fiStride per frame).
func flatFrameInfo(rs []sbr.FrameInfoResult) []int32 {
	out := make([]int32, 0, len(rs)*fiStride)
	for _, r := range rs {
		row := make([]int32, fiStride)
		row[0] = int32(r.NEnvelopes)
		for i := 0; i < 6; i++ {
			row[1+i] = int32(r.Borders[i])
		}
		for i := 0; i < 5; i++ {
			row[7+i] = int32(r.FreqRes[i])
		}
		row[12] = int32(r.ShortEnv)
		row[13] = int32(r.NNoiseEnvelopes)
		for i := 0; i < 3; i++ {
			row[14+i] = int32(r.BordersNoise[i])
		}
		row[17] = int32(r.FrameClass)
		row[18] = int32(r.BsNumEnv)
		row[19] = int32(r.BsAbsBord)
		row[20] = int32(r.N)
		row[21] = int32(r.P)
		row[22] = int32(r.BsAbsBord0)
		row[23] = int32(r.BsAbsBord1)
		row[24] = int32(r.BsNumRel0)
		row[25] = int32(r.BsNumRel1)
		out = append(out, row...)
	}
	return out
}

func TestFrameInfoGeneratorParity(t *testing.T) {
	const timeSlots = 16 // NUMBER_TIME_SLOTS_2048 (HE-AAC v1 STD)
	freqResFixfix := []sbr.FreqRes{sbr.FreqResHigh, sbr.FreqResHigh}
	freqResFixfixC := []int{int(sbr.FreqResHigh), int(sbr.FreqResHigh)}

	rng := rand.New(rand.NewSource(0x5BAA))

	// Fixed transient-pattern sequences that drive every frame class, plus
	// randomized sequences for breadth. Each frame's transient_info is
	// {tranPos, tranFlag, _}.
	fixedSeqs := [][][3]int{
		// no transients -> FIXFIX (1 or 2 env via tranPos)
		{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}},
		// single transient -> FIXVAR then VARFIX follow-up
		{{0, 0, 0}, {5, 1, 0}, {0, 0, 0}, {0, 0, 0}},
		// tight transients -> VARVAR chain
		{{3, 1, 0}, {7, 1, 0}, {2, 1, 0}, {0, 0, 0}},
		// transient at various positions
		{{0, 1, 0}, {0, 0, 0}, {10, 1, 0}, {0, 0, 0}, {15, 1, 0}, {0, 0, 0}},
	}

	run := func(t *testing.T, seq [][3]int, allowSpread, numEnvStatic, staticFraming int) {
		nFrames := len(seq)
		tranInfos := make([][]uint8, nFrames)
		tranFlat := make([]uint8, nFrames*3)
		tranPreFlat := make([]uint8, nFrames*3)
		rbf := make([]int, nFrames)
		for i, ti := range seq {
			tranInfos[i] = []uint8{uint8(ti[0]), uint8(ti[1]), uint8(ti[2])}
			tranFlat[i*3+0] = uint8(ti[0])
			tranFlat[i*3+1] = uint8(ti[1])
			tranFlat[i*3+2] = uint8(ti[2])
		}

		g := sbr.RunFrameInfoGenerator(allowSpread, numEnvStatic, staticFraming, timeSlots, freqResFixfix, 0, 0, vTuningHEAAC, tranInfos, nil, rbf)
		gFlat := flatFrameInfo(g)
		cFlat := cFrameInfoGen(allowSpread, numEnvStatic, staticFraming, timeSlots, freqResFixfixC, 0, 0, vTuningHEAAC, tranFlat, tranPreFlat, rbf, nFrames)
		require.Equal(t, cFlat, gFlat)
	}

	for i, seq := range fixedSeqs {
		seq := seq
		t.Run("fixed_allowSpread", func(t *testing.T) { run(t, seq, 1, 1, 0) })
		t.Run("fixed_noSpread", func(t *testing.T) { run(t, seq, 0, 1, 0) })
		_ = i
	}

	// Randomized multi-frame sequences.
	for iter := 0; iter < 60; iter++ {
		n := 4 + rng.Intn(6)
		seq := make([][3]int, n)
		for f := 0; f < n; f++ {
			flag := 0
			pos := 0
			if rng.Intn(2) == 0 {
				flag = 1
				pos = rng.Intn(timeSlots)
			}
			seq[f] = [3]int{pos, flag, 0}
		}
		allowSpread := rng.Intn(2)
		run(t, seq, allowSpread, 1, 0)
	}
}
