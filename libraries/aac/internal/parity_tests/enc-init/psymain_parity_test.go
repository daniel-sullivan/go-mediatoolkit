// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

// Exact-integer parity for the psychoacoustic DRIVER FDKaacEnc_psyMain: the
// pure-Go nativeaac.ParityPsyMain (Go Open + Initialize + psyMain over a
// deterministic planar int16 sine) vs the GENUINE vendored FDKaacEnc_psyMain
// (driven through the real FDKaacEnc_Open/Initialize + the same input in
// bridge_psymain.cpp). Every populated PSY_OUT_CHANNEL field is compared with
// require.Equal / assert.Equal (exact int / int32, no tolerance) — fdk-aac
// encode is fixed-point so psyMain is bit-identical.

package encinit

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// makePlanarSine builds channels*inputBufSize planar int16 PCM: channel ch holds
// a sine of freq*(ch+1) so the two channels differ (exercising stereo/MS/PNS).
func makePlanarSine(channels, inputBufSize, frameLength, sampleRate int, freq float64) []int16 {
	in := make([]int16, channels*inputBufSize)
	for ch := 0; ch < channels; ch++ {
		phase := 0.0
		step := 2 * math.Pi * freq * float64(ch+1) / float64(sampleRate)
		base := ch * inputBufSize
		for i := 0; i < frameLength; i++ {
			in[base+i] = int16(0.5 * 32767.0 * math.Sin(phase))
			phase += step
		}
	}
	return in
}

func TestPsyMainParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-psy-main driver parity asserts under -tags aac_strict")
	}

	cases := []struct {
		name                                  string
		channelMode, nChannels, nElements     int
		sampleRate, bitRate, aot, frameLength int
		freq                                  float64
	}{
		{"mono-44k-128k", 1, 1, 1, 44100, 128000, aotAACLC, 1024, 440},
		{"stereo-44k-128k", 2, 2, 1, 44100, 128000, aotAACLC, 1024, 440},
		{"stereo-48k-192k", 2, 2, 1, 48000, 192000, aotAACLC, 1024, 1000},
		{"mono-32k-96k", 1, 1, 1, 32000, 96000, aotAACLC, 1024, 660},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const inputBufSize = 1024
			in := makePlanarSine(c.nChannels, inputBufSize, c.frameLength, c.sampleRate, c.freq)

			rc, cst := cEpsyMainRun(c.channelMode, c.nChannels, c.sampleRate, c.bitRate,
				c.aot, c.nElements, c.frameLength, in, inputBufSize)
			require.Equal(t, 0, rc, "genuine psyMain error")

			got := nativeaac.ParityPsyMain(c.channelMode, c.nChannels, c.sampleRate,
				c.bitRate, c.aot, c.nElements, c.frameLength, in, inputBufSize)
			require.Equal(t, 0, got.ErrCode, "go psyMain error")

			assert.Equal(t, int(cst.commonWindow), got.CommonWindow, "commonWindow")
			assert.Equal(t, int(cst.msDigest), got.MsDigest, "msDigest")
			for i := 0; i < 60; i++ {
				assert.Equal(t, int(cst.msMask[i]), got.MsMask[i], "msMask[%d]", i)
			}

			for ch := 0; ch < c.nChannels; ch++ {
				assert.Equal(t, int(cst.sfbCnt[ch]), got.SfbCnt[ch], "ch%d sfbCnt", ch)
				assert.Equal(t, int(cst.sfbPerGroup[ch]), got.SfbPerGroup[ch], "ch%d sfbPerGroup", ch)
				assert.Equal(t, int(cst.maxSfbPerGroup[ch]), got.MaxSfbPerGroup[ch], "ch%d maxSfbPerGroup", ch)
				assert.Equal(t, int(cst.windowShape[ch]), got.WindowShape[ch], "ch%d windowShape", ch)
				assert.Equal(t, int(cst.lastWindowSequence[ch]), got.LastWindowSequence[ch], "ch%d lastWindowSequence", ch)
				assert.Equal(t, int(cst.groupingMask[ch]), got.GroupingMask[ch], "ch%d groupingMask", ch)
				assert.Equal(t, int(cst.mdctScale[ch]), got.MdctScale[ch], "ch%d mdctScale", ch)
				for i := 0; i < 4; i++ {
					assert.Equal(t, int(cst.groupLen[ch][i]), got.GroupLen[ch][i], "ch%d groupLen[%d]", ch, i)
				}
				// sfbOffsets: for LONG blocks the genuine psyMain memcpy's
				// (MAX_GROUPED_SFB+1)==61 INTs from a 52-INT source
				// (psy_main.cpp:1109) — a benign C struct over-read whose tail
				// cells (sfbOffsets[52..60]) are undefined adjacent-struct garbage
				// never consulted downstream (sfbCnt == sfbActive <= 51). Compare
				// only the meaningful range: SHORT blocks fill all 61 grouped
				// offsets, LONG blocks the 52 valid ones.
				offN := 60 + 1
				if got.LastWindowSequence[ch] != 2 /* not SHORT_WINDOW */ {
					offN = 52
				}
				for i := 0; i < offN; i++ {
					assert.Equal(t, int(cst.sfbOffsets[ch][i]), got.SfbOffsets[ch][i], "ch%d sfbOffsets[%d]", ch, i)
				}
				for i := 0; i < 60; i++ {
					assert.Equal(t, int(cst.noiseNrg[ch][i]), got.NoiseNrg[ch][i], "ch%d noiseNrg[%d]", ch, i)
					assert.Equal(t, int(cst.isBook[ch][i]), got.IsBook[ch][i], "ch%d isBook[%d]", ch, i)
					assert.Equal(t, int(cst.isScale[ch][i]), got.IsScale[ch][i], "ch%d isScale[%d]", ch, i)
					assert.Equal(t, int32(cst.sfbEnergy[ch][i]), got.SfbEnergy[ch][i], "ch%d sfbEnergy[%d]", ch, i)
					assert.Equal(t, int32(cst.sfbSpreadEnergy[ch][i]), got.SfbSpreadEnergy[ch][i], "ch%d sfbSpreadEnergy[%d]", ch, i)
					assert.Equal(t, int32(cst.sfbEnergyLdData[ch][i]), got.SfbEnergyLdData[ch][i], "ch%d sfbEnergyLdData[%d]", ch, i)
					assert.Equal(t, int32(cst.sfbThresholdLdData[ch][i]), got.SfbThresholdLdData[ch][i], "ch%d sfbThresholdLdData[%d]", ch, i)
					assert.Equal(t, int32(cst.sfbMinSnrLdData[ch][i]), got.SfbMinSnrLdData[ch][i], "ch%d sfbMinSnrLdData[%d]", ch, i)
				}
				for w := 0; w < 8; w++ {
					assert.Equal(t, int(cst.tnsNumOfFilters[ch][w]), got.TnsNumOfFilters[ch][w], "ch%d tnsNumOfFilters[%d]", ch, w)
					assert.Equal(t, int(cst.tnsCoefRes[ch][w]), got.TnsCoefRes[ch][w], "ch%d tnsCoefRes[%d]", ch, w)
					for f := 0; f < 2; f++ {
						assert.Equal(t, int(cst.tnsOrder[ch][w][f]), got.TnsOrder[ch][w][f], "ch%d tnsOrder[%d][%d]", ch, w, f)
					}
				}
			}
		})
	}
}
