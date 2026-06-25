// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enctnsfinish

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// goPsyConfFromC seeds a nativeaac.PsyConfiguration with the exact sfb layout the
// C side built, so the Go InitTnsConfiguration runs over an identical config.
func goPsyConfFromC(c cConfig) *nativeaac.PsyConfiguration {
	pC := new(nativeaac.PsyConfiguration)
	pC.SfbCnt = int(c.sfbCnt)
	pC.SfbActive = int(c.sfbActive)
	for i := 0; i < len(pC.SfbOffset) && i < len(c.sfbOffset); i++ {
		pC.SfbOffset[i] = c.sfbOffset[i]
	}
	return pC
}

// assertConfigEqual compares the Go TNS_CONFIG against the genuine C one,
// bit-for-bit across every field FDKaacEnc_InitTnsConfiguration writes.
func assertConfigEqual(t *testing.T, c cConfig, g *nativeaac.TNSConfig) {
	t.Helper()
	assert.Equal(t, int(c.isLowDelay), g.IsLowDelay, "isLowDelay")
	assert.Equal(t, int(c.tnsActive), g.TnsActive, "tnsActive")
	assert.Equal(t, int(c.maxOrder), g.MaxOrder, "maxOrder")
	assert.Equal(t, int(c.coefRes), g.CoefRes, "coefRes")
	assert.Equal(t, int(c.lpcStopBand), g.LpcStopBand, "lpcStopBand")
	assert.Equal(t, int(c.lpcStopLine), g.LpcStopLine, "lpcStopLine")
	assert.Equal(t, int(c.seperateFiltersAllowed), g.ConfTab.SeperateFiltersAllowed, "seperateFiltersAllowed")
	for f := 0; f < 2; f++ {
		assert.Equalf(t, int(c.filterEnabled[f]), g.ConfTab.FilterEnabled[f], "filterEnabled[%d]", f)
		assert.Equalf(t, int(c.threshOn[f]), g.ConfTab.ThreshOn[f], "threshOn[%d]", f)
		assert.Equalf(t, int(c.tnsLimitOrder[f]), g.ConfTab.TnsLimitOrder[f], "tnsLimitOrder[%d]", f)
		assert.Equalf(t, int(c.tnsFilterDirection[f]), g.ConfTab.TnsFilterDirection[f], "tnsFilterDirection[%d]", f)
		assert.Equalf(t, int(c.acfSplit[f]), g.ConfTab.AcfSplit[f], "acfSplit[%d]", f)
		assert.Equalf(t, int(c.lpcStartBand[f]), g.LpcStartBand[f], "lpcStartBand[%d]", f)
		assert.Equalf(t, int(c.lpcStartLine[f]), g.LpcStartLine[f], "lpcStartLine[%d]", f)
		for i := 0; i < acfWinSize; i++ {
			assert.Equalf(t, c.acfWindow[f][i], g.AcfWindow[f][i], "acfWindow[%d][%d]", f, i)
		}
	}
}

// makeSpectrum builds a deterministic correlated int32 (FIXP_DBL) MDCT spectrum,
// identical in construction to the enc-tns-full slice so TNS actually triggers.
func makeSpectrum(n, variant int) []int32 {
	s := make([]int32, n)
	for i := 0; i < n; i++ {
		x := float64(i)
		v := math.Sin(x*0.013*float64(variant+1)) * math.Exp(-x/512.0)
		v += 0.5 * math.Sin(x*0.041*float64(variant+2))
		v += 0.25 * math.Cos(x*0.0007*float64(variant+1)*float64(variant+1))
		q := v * 0.18 * float64(1<<30)
		if q > float64(math.MaxInt32) {
			q = float64(math.MaxInt32)
		}
		if q < float64(math.MinInt32) {
			q = float64(math.MinInt32)
		}
		s[i] = int32(q)
	}
	return s
}

var cases = []struct {
	name       string
	bitRate    int
	sampleRate int
	channels   int
}{
	{"44100_128k_stereo", 128000, 44100, 2},
	{"44100_96k_mono", 96000, 44100, 1},
	{"48000_192k_stereo", 192000, 48000, 2},
	{"44100_64k_stereo", 64000, 44100, 2},
	{"32000_96k_stereo", 96000, 32000, 2},
	{"44100_12k_stereo", 12000, 44100, 2}, // bitRate < 16000 -> maxOrder-=2
}

// TestInitTnsConfigurationParity proves FDKaacEnc_InitTnsConfiguration field-for
// field for the AAC-LC long-block path.
func TestInitTnsConfigurationParity(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, sfbActive := cBuildConfig(tc.bitRate, tc.sampleRate, tc.channels, 1)
			require.GreaterOrEqual(t, sfbActive, 0, "C config build failed")
			require.Equal(t, int32(0), cfg.initRc, "C init rc must be AAC_ENC_OK")

			pC := goPsyConfFromC(cfg)
			gC := new(nativeaac.TNSConfig)
			rc := nativeaac.EncInitTnsConfiguration(tc.bitRate, tc.sampleRate, tc.channels,
				nativeaac.EncTnsLongWindow, 1024, 0, 0, gC, pC, 1, 0)
			require.Equal(t, int(cfg.initRc), rc, "init rc")
			assertConfigEqual(t, cfg, gC)
		})
	}
}

// goInfoDataFromC seeds a nativeaac.TNSInfo + TNSData (long subblock, window 0)
// from the C decision output, so the Go TnsEncode/TnsSync runs over an identical
// input.
func goInfoDataFromC(c cInfo) (*nativeaac.TNSInfo, *nativeaac.TNSData) {
	info := new(nativeaac.TNSInfo)
	data := new(nativeaac.TNSData)
	h := nativeaac.EncTnsHifilt
	l := nativeaac.EncTnsLofilt
	info.NumOfFilters[0] = int(c.numOfFilters)
	info.CoefRes[0] = int(c.coefRes)
	data.FiltersMerged = int(c.filtersMerged)
	for fi, f := range []int{h, l} {
		info.Length[0][f] = int(c.length[fi])
		info.Order[0][f] = int(c.order[fi])
		info.Direction[0][f] = int(c.direction[fi])
		info.CoefCompress[0][f] = int(c.coefCompress[fi])
		for k := 0; k < tnsMaxOrder; k++ {
			info.Coef[0][f][k] = int(c.coef[fi][k])
		}
		data.LongSubBlock.TnsActive[f] = int(c.sbTnsActive[fi])
		data.LongSubBlock.PredictionGain[f] = int(c.sbPredictionGain[fi])
	}
	return info, data
}

// TestTnsEncodeParity proves FDKaacEnc_TnsEncode rewrites the MDCT spectrum
// bit-for-bit. Both sides start from the genuine decision output for the same
// spectrum + config, then apply the analysis filter; the Go-filtered spectrum
// must equal the C-filtered spectrum exactly.
func TestTnsEncodeParity(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, sfbActive := cBuildConfig(tc.bitRate, tc.sampleRate, tc.channels, 1)
			require.GreaterOrEqual(t, sfbActive, 0)
			pC := goPsyConfFromC(cfg)
			gC := new(nativeaac.TNSConfig)
			rc := nativeaac.EncInitTnsConfiguration(tc.bitRate, tc.sampleRate, tc.channels,
				nativeaac.EncTnsLongWindow, 1024, 0, 0, gC, pC, 1, 0)
			require.Equal(t, int(cfg.initRc), rc)

			for variant := 0; variant < 6; variant++ {
				spectrum := makeSpectrum(1024, variant)

				// C: genuine detect + encode mutate this copy.
				cSpec := append([]int32(nil), spectrum...)
				cEnc := cEncode(tc.bitRate, tc.sampleRate, tc.channels, 1, sfbActive, cSpec)

				// Go: seed the genuine TNS_INFO/DATA the C decision produced, then
				// run the Go TnsEncode over a fresh spectrum copy.
				cInf, dRc := cDetect(tc.bitRate, tc.sampleRate, tc.channels, 1, sfbActive, append([]int32(nil), spectrum...))
				require.Equal(t, 0, dRc)
				gInfo, gData := goInfoDataFromC(cInf)
				gSpec := append([]int32(nil), spectrum...)
				gEnc := nativeaac.EncTnsEncode(gInfo, gData, sfbActive, gC, gC.LpcStopLine, gSpec, 0, nativeaac.EncTnsLongWindow)

				assert.Equalf(t, cEnc, gEnc, "v%d encode return", variant)
				assert.Equalf(t, cSpec, gSpec, "v%d filtered spectrum", variant)
			}
		})
	}
}

// TestTnsSyncParity proves FDKaacEnc_TnsSync. It drives two channels' decisions
// (the same spectrum perturbed) to manufacture realistic dest/src TNS_INFO, then
// compares the synchronised dest.
func TestTnsSyncParity(t *testing.T) {
	tc := cases[0] // 44100 stereo, TNS active
	cfg, sfbActive := cBuildConfig(tc.bitRate, tc.sampleRate, tc.channels, 1)
	require.GreaterOrEqual(t, sfbActive, 0)
	pC := goPsyConfFromC(cfg)
	gC := new(nativeaac.TNSConfig)
	nativeaac.EncInitTnsConfiguration(tc.bitRate, tc.sampleRate, tc.channels,
		nativeaac.EncTnsLongWindow, 1024, 0, 0, gC, pC, 1, 0)
	maxOrder := int(cfg.maxOrder)

	// Build several dest/src info pairs: identical-spectrum (should sync), and
	// nearby-spectrum (may or may not sync, exercises the absDiff thresholds).
	for variant := 0; variant < 6; variant++ {
		dInfo, _ := cDetect(tc.bitRate, tc.sampleRate, tc.channels, 1, sfbActive, makeSpectrum(1024, variant))
		sInfo, _ := cDetect(tc.bitRate, tc.sampleRate, tc.channels, 1, sfbActive, makeSpectrum(1024, variant+1))

		// C sync.
		cOut := cSync(maxOrder, dInfo, sInfo)

		// Go sync over the same seeded dest/src.
		gDestInfo, gDestData := goInfoDataFromC(dInfo)
		gSrcInfo, gSrcData := goInfoDataFromC(sInfo)
		nativeaac.EncTnsSync(gDestData, gSrcData, gDestInfo, gSrcInfo,
			nativeaac.EncTnsLongWindow, nativeaac.EncTnsLongWindow, gC)

		assertSyncEqual(t, variant, cOut, gDestInfo, gDestData)

		// Also exercise the identical-pair (guaranteed sync path).
		cOut2 := cSync(maxOrder, dInfo, dInfo)
		gDi, gDd := goInfoDataFromC(dInfo)
		gSi, gSd := goInfoDataFromC(dInfo)
		nativeaac.EncTnsSync(gDd, gSd, gDi, gSi,
			nativeaac.EncTnsLongWindow, nativeaac.EncTnsLongWindow, gC)
		assertSyncEqual(t, variant+100, cOut2, gDi, gDd)
	}
}

func assertSyncEqual(t *testing.T, variant int, c cInfo, gInfo *nativeaac.TNSInfo, gData *nativeaac.TNSData) {
	t.Helper()
	h := nativeaac.EncTnsHifilt
	l := nativeaac.EncTnsLofilt
	assert.Equalf(t, int(c.numOfFilters), gInfo.NumOfFilters[0], "v%d numOfFilters", variant)
	assert.Equalf(t, int(c.filtersMerged), gData.FiltersMerged, "v%d filtersMerged", variant)
	for fi, f := range []int{h, l} {
		assert.Equalf(t, int(c.length[fi]), gInfo.Length[0][f], "v%d length[%d]", variant, f)
		assert.Equalf(t, int(c.order[fi]), gInfo.Order[0][f], "v%d order[%d]", variant, f)
		assert.Equalf(t, int(c.direction[fi]), gInfo.Direction[0][f], "v%d direction[%d]", variant, f)
		assert.Equalf(t, int(c.coefCompress[fi]), gInfo.CoefCompress[0][f], "v%d coefCompress[%d]", variant, f)
		assert.Equalf(t, int(c.sbTnsActive[fi]), gData.LongSubBlock.TnsActive[f], "v%d tnsActive[%d]", variant, f)
		for k := 0; k < tnsMaxOrder; k++ {
			assert.Equalf(t, int(c.coef[fi][k]), gInfo.Coef[0][f][k], "v%d coef[%d][%d]", variant, f, k)
		}
	}
}

// TestParcorToLpcParity isolates CLpc_ParcorToLpc over random reflection
// coefficients (the dequantized ParCor inputs TnsEncode feeds it).
func TestParcorToLpcParity(t *testing.T) {
	rng := newLCG(0xC0FFEE01)
	for trial := 0; trial < 60; trial++ {
		order := 1 + trial%12 // 1..12
		refl := make([]int16, order)
		for i := range refl {
			refl[i] = int16(rng.next())
		}
		cLpc, cWB, cExp := cParcorToLpc(refl, order)

		goLpc := make([]int16, order)
		goWB := make([]int32, order)
		goExp := nativeaac.EncClpcParcorToLpc(append([]int16(nil), refl...), goLpc, order, goWB)

		assert.Equalf(t, cExp, goExp, "trial%d exponent", trial)
		assert.Equalf(t, cLpc, goLpc, "trial%d lpcCoeff", trial)
		assert.Equalf(t, cWB, goWB, "trial%d workBuffer", trial)
	}
}

// TestAnalysisParity isolates CLpc_Analysis (the FIR residual filter) over a
// random signal + LPC coefficients, NULL filtStateIndex, asserting the in-place
// rewrite of signal[] and the final filtState[] are bit-identical.
func TestAnalysisParity(t *testing.T) {
	rng := newLCG(0xA5A5F00D)
	for trial := 0; trial < 40; trial++ {
		order := 1 + trial%12 // 1..12
		lpcCoeffE := trial % 4
		size := 64 + trial*7

		signal := make([]int32, size)
		for i := range signal {
			signal[i] = rng.next()
		}
		lpc := make([]int16, order)
		for i := range lpc {
			lpc[i] = int16(rng.next())
		}

		cSig := append([]int32(nil), signal...)
		cState := make([]int32, order)
		cAnalysis(cSig, lpc, lpcCoeffE, order, cState)

		goSig := append([]int32(nil), signal...)
		goState := make([]int32, order)
		nativeaac.EncClpcAnalysis(goSig, len(goSig), append([]int16(nil), lpc...), lpcCoeffE, order, goState, nil)

		assert.Equalf(t, cSig, goSig, "trial%d signal", trial)
		assert.Equalf(t, cState, goState, "trial%d filtState", trial)
	}
}

// TestGetTnsMaxBandsParity proves the encoder getTnsMaxBands ROM table scan over
// every seeded sample rate and both block columns.
func TestGetTnsMaxBandsParity(t *testing.T) {
	rates := []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000, 100000, 7000}
	for _, sr := range rates {
		for _, short := range []int{0, 1} {
			for _, gl := range []int{1024, 960} {
				got := nativeaac.EncGetTnsMaxBands(sr, gl, short)
				want := cGetMaxBands(sr, gl, short)
				assert.Equalf(t, want, got, "getTnsMaxBands(sr=%d,gl=%d,short=%d)", sr, gl, short)
			}
		}
	}
}

// lcg is a tiny deterministic PRNG.
type lcg struct{ state uint64 }

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }
func (r *lcg) next() int32 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return int32(r.state >> 33)
}
