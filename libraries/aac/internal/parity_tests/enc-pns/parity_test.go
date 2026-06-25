// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enc_pns

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// These slices assert the pure-Go PNS detect/code chain port (nativeaac.GetPnsParam
// / InitPnsConfiguration / PnsDetect / CodePnsChannel / PreProcessPnsChannelPair /
// PostProcessPnsChannelPair) is bit-for-bit identical to the genuine vendored
// libfdk kernels. fdk-aac encode is fixed-point, so equality is EXACT int32/int16
// — no tolerance.

const maxGroupedSfb = 60

// goConfFromC builds a Go PNSConfig matching the flat cPnsConf the bridge filled,
// so the detect/code/pre/post slices run both sides against the identical config.
func goConfFromC(c cPnsConf) nativeaac.PNSConfig {
	var g nativeaac.PNSConfig
	g.UsePns = int(c.usePns)
	g.MinCorrelationEnergy = c.minCorrelationEnergy
	g.NoiseCorrelationThresh = c.noiseCorrelationThresh
	g.NoiseParams.StartSfb = int16(c.startSfb)
	g.NoiseParams.DetectionAlgorithmFlags = uint16(c.detectionAlgorithmFlags)
	g.NoiseParams.RefPower = c.refPower
	g.NoiseParams.RefTonality = c.refTonality
	g.NoiseParams.TnsGainThreshold = int(c.tnsGainThreshold)
	g.NoiseParams.TnsPNSGainThreshold = int(c.tnsPNSGainThreshold)
	g.NoiseParams.MinSfbWidth = int(c.minSfbWidth)
	for i := 0; i < maxGroupedSfb; i++ {
		g.NoiseParams.PowDistPSDcurve[i] = c.powDistPSDcurve[i]
	}
	g.NoiseParams.GapFillThr = c.gapFillThr
	return g
}

// pnsConfCase is one (bitrate, samplerate, sfbCnt, sfb width) tuple to init the
// PNS config for. The sfb layout is a uniform-width long-block layout.
type pnsConfCase struct {
	name       string
	bitrate    int
	samplerate int
	sfbCnt     int
	sfbWidth   int
	numChan    int
	isLC       int
}

// The bitrates are chosen to land in PNS-ENABLED brackets so the detect/code/
// pre/post chains actually run their decision branches:
//   - LC (levelTable_lowComplexity, per-sfb): 28000-31999 -> idx 2,
//     32000-47999 -> idx 3, 48000 -> idx 4 (sfb tables S16000..S48000 == idx).
//   - (E)LD stereo (levelTable_stereo): 29000-40999 -> rows 5..7, 41000-55999
//     -> rows 7..9; mono (levelTable_mono) similar.
var pnsConfCases = []pnsConfCase{
	{"44100_lc_mono_pns", 40000, 44100, 49, 16, 1, 1},    // LC idx3 -> enabled
	{"44100_lc_stereo_pns", 40000, 44100, 49, 16, 2, 1},  // LC idx3 -> enabled
	{"48000_lc_stereo_pns", 48000, 48000, 49, 16, 2, 1},  // LC idx4 -> enabled
	{"32000_lc_stereo_pns", 30000, 32000, 44, 16, 2, 1},  // LC idx2 -> enabled
	{"24000_lc_mono_pns", 40000, 24000, 42, 12, 1, 1},    // LC idx3 -> enabled
	{"22050_lc_stereo_pns", 35000, 22050, 42, 12, 2, 1},  // LC idx3 -> enabled
	{"16000_lc_mono_pns", 32000, 16000, 40, 12, 1, 1},    // LC idx3 -> enabled
	{"44100_lc_stereo_off", 128000, 44100, 49, 16, 2, 1}, // LC -> PNS disabled
	{"48000_eld_stereo", 45000, 48000, 49, 16, 2, 0},     // ELD stereo -> enabled
	{"44100_eld_mono", 35000, 44100, 49, 16, 1, 0},       // ELD mono -> enabled
}

// buildSfbOffset builds a uniform-width sfb offset layout of sfbCnt bands plus
// the terminating offset.
func buildSfbOffset(sfbCnt, sfbWidth int) []int32 {
	off := make([]int32, sfbCnt+1)
	for i := 0; i <= sfbCnt; i++ {
		off[i] = int32(i * sfbWidth)
	}
	return off
}

func TestGetPnsParamParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-pns parity asserts under -tags aac_strict")
	}

	for _, tc := range pnsConfCases {
		t.Run(tc.name, func(t *testing.T) {
			sfbOffset := buildSfbOffset(tc.sfbCnt, tc.sfbWidth)

			cConf, cErr := cInitPnsConfiguration(tc.bitrate, tc.samplerate, 1, tc.sfbCnt,
				sfbOffset, tc.numChan, tc.isLC)

			var goConf nativeaac.PNSConfig
			goErr := nativeaac.InitPnsConfiguration(&goConf, tc.bitrate, tc.samplerate, 1,
				tc.sfbCnt, sfbOffset, tc.numChan, tc.isLC)

			require.Equal(t, cErr, goErr, "error code")
			if cErr != 0 {
				return
			}

			assert.Equal(t, cConf.usePns, int32(goConf.UsePns), "usePns")
			assert.Equal(t, cConf.minCorrelationEnergy, goConf.MinCorrelationEnergy, "minCorrelationEnergy")
			assert.Equal(t, cConf.noiseCorrelationThresh, goConf.NoiseCorrelationThresh, "noiseCorrelationThresh")
			assert.Equal(t, cConf.startSfb, int32(goConf.NoiseParams.StartSfb), "startSfb")
			assert.Equal(t, cConf.detectionAlgorithmFlags, int32(goConf.NoiseParams.DetectionAlgorithmFlags), "detectionAlgorithmFlags")
			assert.Equal(t, cConf.refPower, goConf.NoiseParams.RefPower, "refPower")
			assert.Equal(t, cConf.refTonality, goConf.NoiseParams.RefTonality, "refTonality")
			assert.Equal(t, cConf.tnsGainThreshold, int32(goConf.NoiseParams.TnsGainThreshold), "tnsGainThreshold")
			assert.Equal(t, cConf.tnsPNSGainThreshold, int32(goConf.NoiseParams.TnsPNSGainThreshold), "tnsPNSGainThreshold")
			assert.Equal(t, cConf.minSfbWidth, int32(goConf.NoiseParams.MinSfbWidth), "minSfbWidth")
			assert.Equal(t, cConf.gapFillThr, goConf.NoiseParams.GapFillThr, "gapFillThr")
			assert.Equal(t, cConf.powDistPSDcurve[:], goConf.NoiseParams.PowDistPSDcurve[:], "powDistPSDcurve")
		})
	}
}

// pseudo is a tiny deterministic LCG used to fabricate spectra / energies that
// exercise the detect/code branches without depending on the encoder front end.
type pseudo struct{ s uint32 }

func (p *pseudo) next() uint32 {
	p.s = p.s*1664525 + 1013904223
	return p.s
}

// detectInputs holds one fabricated PnsDetect input set.
type detectInputs struct {
	sfbActive          int
	maxSfbPerGroup     int
	sfbThresholdLdData []int32
	sfbOffset          []int32
	mdctSpectrum       []int32
	sfbMaxScaleSpec    []int32
	sfbtonality        []int16
	sfbEnergyLdData    []int32
}

func makeDetectInputs(seed uint32, sfbCnt, sfbWidth int) detectInputs {
	p := pseudo{s: seed}
	sfbOffset := buildSfbOffset(sfbCnt, sfbWidth)
	specLen := int(sfbOffset[sfbCnt])

	spec := make([]int32, specLen)
	for i := range spec {
		// small-ish FIXP_DBL values; keep the <<leadingBits shift from overflowing
		// in noiseDetect (mdctSpectrum[i] << (sfbMaxScaleSpec-3)).
		v := int32(p.next()) >> 14
		spec[i] = v
	}

	maxScale := make([]int32, sfbCnt)
	for i := range maxScale {
		maxScale[i] = int32(p.next() % 5) // 0..4 leadingBits headroom
	}

	tonality := make([]int16, maxGroupedSfb)
	for i := 0; i < sfbCnt; i++ {
		tonality[i] = int16(p.next() & 0x7FFF)
	}

	// ld-domain threshold/energy values in a plausible negative range.
	thr := make([]int32, maxGroupedSfb)
	en := make([]int32, maxGroupedSfb)
	for i := 0; i < sfbCnt; i++ {
		thr[i] = -int32(p.next()%(1<<28)) - (1 << 27)
		en[i] = -int32(p.next()%(1<<28)) - (1 << 26)
	}

	return detectInputs{
		sfbActive:          sfbCnt,
		maxSfbPerGroup:     sfbCnt,
		sfbThresholdLdData: thr,
		sfbOffset:          sfbOffset,
		mdctSpectrum:       spec,
		sfbMaxScaleSpec:    maxScale,
		sfbtonality:        tonality,
		sfbEnergyLdData:    en,
	}
}

func TestPnsDetectParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-pns parity asserts under -tags aac_strict")
	}

	for _, tc := range pnsConfCases {
		if tc.isLC == 0 {
			continue // (E)LD uses a different block-type gate; LC is the target
		}
		t.Run(tc.name, func(t *testing.T) {
			sfbOffset := buildSfbOffset(tc.sfbCnt, tc.sfbWidth)
			cConf, cErr := cInitPnsConfiguration(tc.bitrate, tc.samplerate, 1, tc.sfbCnt,
				sfbOffset, tc.numChan, tc.isLC)
			require.Equal(t, 0, cErr)
			if cConf.usePns == 0 {
				t.Skip("PNS disabled for this config")
			}
			goConf := goConfFromC(cConf)

			// Several TNS-activity scenarios drive the noiseDetection branch.
			tnsScenarios := []struct {
				name              string
				tnsOrder          int
				tnsPredictionGain int
				tnsActive         int
			}{
				{"no_tns", 0, 0, 0},
				{"low_gain", 2, 500, 0},
				{"high_gain_inactive", 5, 2000, 0},
				{"high_gain_active", 5, 2000, 1},
			}

			for si, sc := range tnsScenarios {
				t.Run(sc.name, func(t *testing.T) {
					in := makeDetectInputs(uint32(0x1234+si)*7919, tc.sfbCnt, tc.sfbWidth)

					cFlag, cNrg, cFuzzy := cPnsDetect(cConf, 0 /*LONG*/, in.sfbActive,
						in.maxSfbPerGroup, in.sfbThresholdLdData, in.sfbOffset, in.mdctSpectrum,
						in.sfbMaxScaleSpec, in.sfbtonality, sc.tnsOrder, sc.tnsPredictionGain,
						sc.tnsActive, in.sfbEnergyLdData)

					var pnsData nativeaac.PNSData
					noiseNrg := make([]int, maxGroupedSfb)
					gConf := goConf
					nativeaac.PnsDetect(&gConf, &pnsData, 0 /*LONG*/, in.sfbActive,
						in.maxSfbPerGroup, in.sfbThresholdLdData, in.sfbOffset, in.mdctSpectrum,
						in.sfbMaxScaleSpec, in.sfbtonality, sc.tnsOrder, sc.tnsPredictionGain,
						sc.tnsActive, in.sfbEnergyLdData, noiseNrg)

					goFlag := make([]int32, maxGroupedSfb)
					goNrg := make([]int32, maxGroupedSfb)
					goFuzzy := make([]int16, maxGroupedSfb)
					for i := 0; i < maxGroupedSfb; i++ {
						goFlag[i] = int32(pnsData.PnsFlag[i])
						goNrg[i] = int32(noiseNrg[i])
						goFuzzy[i] = pnsData.NoiseFuzzyMeasure[i]
					}

					assert.Equal(t, cFuzzy, goFuzzy, "noiseFuzzyMeasure")
					assert.Equal(t, cFlag, goFlag, "pnsFlag")
					assert.Equal(t, cNrg, goNrg, "noiseNrg")
				})
			}
		})
	}
}

func TestCodePnsChannelParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-pns parity asserts under -tags aac_strict")
	}

	for _, tc := range pnsConfCases {
		if tc.isLC == 0 {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			sfbOffset := buildSfbOffset(tc.sfbCnt, tc.sfbWidth)
			cConf, cErr := cInitPnsConfiguration(tc.bitrate, tc.samplerate, 1, tc.sfbCnt,
				sfbOffset, tc.numChan, tc.isLC)
			require.Equal(t, 0, cErr)
			if cConf.usePns == 0 {
				t.Skip("PNS disabled")
			}
			goConf := goConfFromC(cConf)

			p := pseudo{s: 0xC0DE}
			pnsFlag := make([]int32, maxGroupedSfb)
			noiseNrgIn := make([]int32, maxGroupedSfb)
			thrIn := make([]int32, maxGroupedSfb)
			enLd := make([]int32, maxGroupedSfb)
			for i := 0; i < tc.sfbCnt; i++ {
				if p.next()%3 != 0 {
					pnsFlag[i] = 1
				}
				// a spread of noiseNrg values to exercise the delta-LAV clamp.
				noiseNrgIn[i] = int32(p.next()%400) - 200
				thrIn[i] = -int32(p.next() % (1 << 28))
				enLd[i] = -int32(p.next() % (1 << 28))
			}

			cNrg, cThr := cCodePnsChannel(cConf, tc.sfbCnt, pnsFlag, enLd, noiseNrgIn, thrIn)

			noiseNrg := make([]int, maxGroupedSfb)
			thr := make([]int32, maxGroupedSfb)
			pf := make([]int, maxGroupedSfb)
			for i := 0; i < maxGroupedSfb; i++ {
				noiseNrg[i] = int(noiseNrgIn[i])
				thr[i] = thrIn[i]
				pf[i] = int(pnsFlag[i])
			}
			gConf := goConf
			nativeaac.CodePnsChannel(tc.sfbCnt, &gConf, pf, enLd, noiseNrg, thr)

			goNrg := make([]int32, maxGroupedSfb)
			for i := 0; i < maxGroupedSfb; i++ {
				goNrg[i] = int32(noiseNrg[i])
			}
			assert.Equal(t, cNrg, goNrg, "noiseNrg")
			assert.Equal(t, cThr, thr, "sfbThresholdLdData")
		})
	}
}

func TestPreProcessPnsChannelPairParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-pns parity asserts under -tags aac_strict")
	}

	for _, tc := range pnsConfCases {
		if tc.numChan < 2 {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			sfbOffset := buildSfbOffset(tc.sfbCnt, tc.sfbWidth)
			cConf, cErr := cInitPnsConfiguration(tc.bitrate, tc.samplerate, 1, tc.sfbCnt,
				sfbOffset, tc.numChan, tc.isLC)
			require.Equal(t, 0, cErr)
			if cConf.usePns == 0 {
				t.Skip("PNS disabled")
			}
			goConf := goConfFromC(cConf)

			p := pseudo{s: 0xBEEF}
			enL := make([]int32, maxGroupedSfb)
			enR := make([]int32, maxGroupedSfb)
			enLLD := make([]int32, maxGroupedSfb)
			enRLD := make([]int32, maxGroupedSfb)
			enMid := make([]int32, maxGroupedSfb)
			for i := 0; i < tc.sfbCnt; i++ {
				enL[i] = int32(p.next() >> 2)
				enR[i] = int32(p.next() >> 2)
				enMid[i] = int32(p.next() >> 2)
				enLLD[i] = -int32(p.next() % (1 << 28))
				enRLD[i] = -int32(p.next() % (1 << 28))
			}

			cCorr := cPreProcess(cConf, tc.sfbCnt, enL, enR, enLLD, enRLD, enMid)

			var pnsL, pnsR nativeaac.PNSData
			gConf := goConf
			nativeaac.PreProcessPnsChannelPair(tc.sfbCnt, enL, enR, enLLD, enRLD, enMid,
				&gConf, &pnsL, &pnsR)

			goCorr := make([]int32, maxGroupedSfb)
			for i := 0; i < maxGroupedSfb; i++ {
				goCorr[i] = pnsL.NoiseEnergyCorrelation[i]
			}
			assert.Equal(t, cCorr, goCorr, "noiseEnergyCorrelation")
			// L and R must be identical (both written to ccf).
			for i := 0; i < maxGroupedSfb; i++ {
				assert.Equal(t, pnsL.NoiseEnergyCorrelation[i], pnsR.NoiseEnergyCorrelation[i], "L==R corr[%d]", i)
			}
		})
	}
}

func TestPostProcessPnsChannelPairParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc-pns parity asserts under -tags aac_strict")
	}

	for _, tc := range pnsConfCases {
		if tc.numChan < 2 {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			sfbOffset := buildSfbOffset(tc.sfbCnt, tc.sfbWidth)
			cConf, cErr := cInitPnsConfiguration(tc.bitrate, tc.samplerate, 1, tc.sfbCnt,
				sfbOffset, tc.numChan, tc.isLC)
			require.Equal(t, 0, cErr)
			if cConf.usePns == 0 {
				t.Skip("PNS disabled")
			}
			goConf := goConfFromC(cConf)

			thresh := goConf.NoiseCorrelationThresh
			p := pseudo{s: 0xFACE}
			flagL := make([]int32, maxGroupedSfb)
			flagR := make([]int32, maxGroupedSfb)
			corrL := make([]int32, maxGroupedSfb)
			corrR := make([]int32, maxGroupedSfb)
			msMask := make([]int32, maxGroupedSfb)
			for i := 0; i < tc.sfbCnt; i++ {
				if p.next()%2 == 0 {
					flagL[i] = 1
				}
				if p.next()%2 == 0 {
					flagR[i] = 1
				}
				if p.next()%2 == 0 {
					msMask[i] = 1
				}
				// straddle the correlation threshold both ways.
				if p.next()%2 == 0 {
					corrL[i] = thresh + int32(p.next()%1000)
				} else {
					corrL[i] = thresh - int32(p.next()%1000) - 1
				}
				corrR[i] = corrL[i]
			}

			cFL, cFR, cMM, cDig := cPostProcess(cConf, tc.sfbCnt, flagL, flagR, corrL, corrR, msMask, 0)

			var pnsL, pnsR nativeaac.PNSData
			for i := 0; i < maxGroupedSfb; i++ {
				pnsL.PnsFlag[i] = int(flagL[i])
				pnsR.PnsFlag[i] = int(flagR[i])
				pnsL.NoiseEnergyCorrelation[i] = corrL[i]
				pnsR.NoiseEnergyCorrelation[i] = corrR[i]
			}
			goMask := make([]int, maxGroupedSfb)
			for i := 0; i < maxGroupedSfb; i++ {
				goMask[i] = int(msMask[i])
			}
			msDigest := 0
			gConf := goConf
			nativeaac.PostProcessPnsChannelPair(tc.sfbCnt, &gConf, &pnsL, &pnsR, goMask, &msDigest)

			goFL := make([]int32, maxGroupedSfb)
			goFR := make([]int32, maxGroupedSfb)
			goMM := make([]int32, maxGroupedSfb)
			for i := 0; i < maxGroupedSfb; i++ {
				goFL[i] = int32(pnsL.PnsFlag[i])
				goFR[i] = int32(pnsR.PnsFlag[i])
				goMM[i] = int32(goMask[i])
			}
			assert.Equal(t, cFL, goFL, "pnsFlagL")
			assert.Equal(t, cFR, goFR, "pnsFlagR")
			assert.Equal(t, cMM, goMM, "msMask")
			assert.Equal(t, cDig, msDigest, "msDigest")
		})
	}
}
