// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrenccodeenv

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goCodeEnvelope mirrors ceparity_run (the C bridge) using the pure-Go port: it
// inits the envelope + noise SBR_CODE_ENVELOPE handles + huffman tables, then
// runs the multi-frame codeEnvelope scenario and returns the same outputs.
func goCodeEnvelope(ampRes, nSfbLo, nSfbHi, deltaTAcross, coupling, channel,
	headerActive, nFrames, nEnvPerFr int, freqResIn []int32, sfbNrgIn []int8,
	isNoise int) (sfbNrgOut []int8, dirVecOut []int32, prevOut []int8, upDate int) {

	var henv, hnoise sbr.SbrCodeEnvelope
	var envData sbr.SbrEnvData
	nSfb := []int{nSfbLo, nSfbHi}

	df := nativeaac.Fl2fxconstDBLf(0.3)
	sbr.InitSbrCodeEnvelope(&henv, nSfb, deltaTAcross, df, df)
	sbr.InitSbrCodeEnvelope(&hnoise, nSfb, deltaTAcross, df, df)
	sbr.InitSbrHuffmanTables(&envData, &henv, &hnoise, sbr.AmpRes(ampRes))

	h := &henv
	if isNoise != 0 {
		h = &hnoise
	}

	sfbNrgOut = make([]int8, len(sfbNrgIn))
	dirVecOut = make([]int32, nFrames*nEnvPerFr)

	frBase, inBase, outBase, dvBase := 0, 0, 0, 0
	for f := 0; f < nFrames; f++ {
		freqRes := make([]sbr.FreqRes, nEnvPerFr)
		total := 0
		for e := 0; e < nEnvPerFr; e++ {
			freqRes[e] = sbr.FreqRes(freqResIn[frBase+e])
			if freqResIn[frBase+e] == 1 {
				total += nSfbHi
			} else {
				total += nSfbLo
			}
		}
		buf := make([]int8, total)
		copy(buf, sfbNrgIn[inBase:inBase+total])
		dirvec := make([]int, nEnvPerFr)

		sbr.CodeEnvelope(buf, freqRes, h, dirvec, coupling, nEnvPerFr, channel, headerActive)

		copy(sfbNrgOut[outBase:outBase+total], buf)
		for e := 0; e < nEnvPerFr; e++ {
			dirVecOut[dvBase+e] = int32(dirvec[e])
		}
		frBase += nEnvPerFr
		inBase += total
		outBase += total
		dvBase += nEnvPerFr
	}

	prevOut = make([]int8, maxFreqCoeffsC)
	copy(prevOut, h.SfbNrgPrev[:])
	upDate = h.UpDate
	return
}

func TestCodeEnvelopeParity(t *testing.T) {
	type cfg struct {
		name                             string
		ampRes, nSfbLo, nSfbHi           int
		deltaTAcross, coupling, channel  int
		headerActive, nFrames, nEnvPerFr int
		isNoise                          int
		nrgRange                         int
	}
	cfgs := []cfg{
		{"env_amp30_mono", 1, 5, 9, 1, 0, 0, 0, 4, 2, 0, 60},
		{"env_amp15_mono", 0, 5, 9, 1, 0, 0, 0, 4, 1, 0, 60},
		{"env_amp30_hdr", 1, 6, 11, 1, 0, 0, 1, 3, 2, 0, 60},
		{"noise_mono", 1, 5, 9, 1, 0, 0, 0, 4, 2, 1, 8},
		{"env_coupling_ch0", 1, 5, 9, 1, 1, 0, 0, 4, 2, 0, 30},
		{"env_coupling_ch1", 1, 5, 9, 1, 1, 1, 0, 4, 2, 0, 12},
		{"noise_coupling_ch1", 1, 5, 9, 1, 1, 1, 0, 4, 2, 1, 12},
		{"env_singleenv_amp30", 1, 7, 13, 1, 0, 0, 0, 5, 1, 0, 60},
		{"env_deltaToff", 1, 5, 9, 0, 0, 0, 0, 4, 2, 0, 60},
	}

	for _, c := range cfgs {
		t.Run(c.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(0x5B3 + c.nSfbHi*7 + c.ampRes)))

			// build freqRes + sfbNrg input for nFrames*nEnvPerFr envelopes
			var freqRes []int32
			var sfbNrg []int8
			for f := 0; f < c.nFrames; f++ {
				for e := 0; e < c.nEnvPerFr; e++ {
					hi := int32(rng.Intn(2))
					freqRes = append(freqRes, hi)
					n := c.nSfbLo
					if hi == 1 {
						n = c.nSfbHi
					}
					for b := 0; b < n; b++ {
						// scalefactor magnitudes in [0, nrgRange]
						sfbNrg = append(sfbNrg, int8(rng.Intn(c.nrgRange+1)))
					}
				}
			}

			cOut, cDir, cPrev, cUp := cCodeEnvelope(c.ampRes, c.nSfbLo, c.nSfbHi,
				c.deltaTAcross, c.coupling, c.channel, c.headerActive, c.nFrames,
				c.nEnvPerFr, freqRes, append([]int8(nil), sfbNrg...), c.isNoise)
			gOut, gDir, gPrev, gUp := goCodeEnvelope(c.ampRes, c.nSfbLo, c.nSfbHi,
				c.deltaTAcross, c.coupling, c.channel, c.headerActive, c.nFrames,
				c.nEnvPerFr, freqRes, append([]int8(nil), sfbNrg...), c.isNoise)

			require.Equal(t, cDir, gDir, "directionVec")
			assert.Equal(t, cOut, gOut, "delta-coded sfb_nrg")
			assert.Equal(t, cPrev, gPrev, "sfb_nrg_prev")
			assert.Equal(t, cUp, gUp, "upDate")
		})
	}
}
