// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enctnsfull

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// goConfigFromC builds a nativeaac.TNSConfig with the exact field values the
// genuine FDKaacEnc_InitTnsConfiguration produced, so the Go TNS decision runs
// over an identical config (the parity claim is on FDKaacEnc_TnsDetect, not on
// the config init — both sides consume the same C-built config).
func goConfigFromC(c cConfig) *nativeaac.TNSConfig {
	tC := new(nativeaac.TNSConfig)
	tC.IsLowDelay = int(c.isLowDelay)
	tC.TnsActive = int(c.tnsActive)
	tC.MaxOrder = int(c.maxOrder)
	tC.CoefRes = int(c.coefRes)
	tC.LpcStopBand = int(c.lpcStopBand)
	tC.LpcStopLine = int(c.lpcStopLine)
	for f := 0; f < 2; f++ {
		tC.ConfTab.FilterEnabled[f] = int(c.filterEnabled[f])
		tC.ConfTab.ThreshOn[f] = int(c.threshOn[f])
		tC.ConfTab.FilterStartFreq[f] = int(c.filterStartFreq[f])
		tC.ConfTab.TnsLimitOrder[f] = int(c.tnsLimitOrder[f])
		tC.ConfTab.TnsFilterDirection[f] = int(c.tnsFilterDirection[f])
		tC.ConfTab.AcfSplit[f] = int(c.acfSplit[f])
		tC.ConfTab.TnsTimeResolution[f] = c.tnsTimeResolution[f]
		tC.LpcStartBand[f] = int(c.lpcStartBand[f])
		tC.LpcStartLine[f] = int(c.lpcStartLine[f])
		for i := 0; i < acfWinSize; i++ {
			tC.AcfWindow[f][i] = c.acfWindow[f][i]
		}
	}
	tC.ConfTab.SeperateFiltersAllowed = int(c.seperateFiltersAllowed)
	return tC
}

// makeSpectrum builds a deterministic int32 (FIXP_DBL Q-format) MDCT-style
// long-block spectrum of `n` lines designed to exercise TNS: a sum of decaying
// sinusoids in the line domain (which yields a correlated spectrum the
// LeRoux-Gueguen analysis can predict), scaled into the FIXP_DBL range. The
// `variant` perturbs the content so several cases run.
func makeSpectrum(n, variant int) []int32 {
	s := make([]int32, n)
	for i := 0; i < n; i++ {
		x := float64(i)
		// A few partials with line-domain envelope -> correlated spectrum.
		v := math.Sin(x*0.013*float64(variant+1)) * math.Exp(-x/512.0)
		v += 0.5 * math.Sin(x*0.041*float64(variant+2))
		v += 0.25 * math.Cos(x*0.0007*float64(variant+1)*float64(variant+1))
		// Scale to a comfortable fraction of full-scale Q1.31.
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

// TestTnsDetectParity is the headline slice: the full FDKaacEnc_TnsDetect
// decision, bit-for-bit, over several spectra and sample rates.
func TestTnsDetectParity(t *testing.T) {
	// Pure integer kernel — no FP, so it runs and asserts EXACT equality in
	// both the default and the aac_strict build (no StrictMode skip needed).
	cases := []struct {
		name       string
		bitRate    int
		sampleRate int
		channels   int
	}{
		{"44100_128k_stereo", 128000, 44100, 2},
		{"44100_96k_mono", 96000, 44100, 1},
		{"48000_192k_stereo", 192000, 48000, 2},
		{"44100_64k_stereo", 64000, 44100, 2},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, sfbActive := cBuildConfig(tc.bitRate, tc.sampleRate, tc.channels, 1)
			require.GreaterOrEqual(t, sfbActive, 0, "config build failed rc=%d", sfbActive)
			require.Equal(t, int32(1), cfg.tnsActive, "tns should be active for AAC-LC long")
			require.Greater(t, cfg.lpcStopLine, int32(0))

			goCfg := goConfigFromC(cfg)
			scratch := make([]int32, 1024)

			for variant := 0; variant < 5; variant++ {
				spectrum := makeSpectrum(1024, variant)

				cInfo, cRc := cDetect(tc.bitRate, tc.sampleRate, tc.channels, 1, sfbActive, spectrum)
				require.Equal(t, 0, cRc)

				// Go side: fresh data/info, identical spectrum + config.
				goData := new(nativeaac.TNSData)
				goInfo := new(nativeaac.TNSInfo)
				// nativeaac.EncTnsDetect mutates spectrum (scaleUp writes scratch only;
				// CLpc mutates rxx copies, not spectrum) — pass a copy for safety since
				// the C side consumed its own copy.
				goSpec := make([]int32, len(spectrum))
				copy(goSpec, spectrum)
				rc := nativeaac.EncTnsDetect(goData, goCfg, goInfo, sfbActive, goSpec, 0, nativeaac.EncTnsLongWindow, scratch)
				_ = rc

				assertTnsEqual(t, variant, cInfo, goData, goInfo)
			}
		})
	}
}

// assertTnsEqual compares the C TNS_INFO/TNS_SUBBLOCK_INFO against the Go result.
func assertTnsEqual(t *testing.T, variant int, c cTnsInfo, goData *nativeaac.TNSData, goInfo *nativeaac.TNSInfo) {
	t.Helper()
	h := nativeaac.EncTnsHifilt
	l := nativeaac.EncTnsLofilt

	assert.Equal(t, int(c.numOfFilters), goInfo.NumOfFilters[0], "v%d numOfFilters", variant)
	assert.Equal(t, int(c.coefRes), goInfo.CoefRes[0], "v%d coefRes", variant)
	assert.Equal(t, int(c.filtersMerged), goData.FiltersMerged, "v%d filtersMerged", variant)

	for fi, f := range []int{h, l} {
		assert.Equalf(t, int(c.length[fi]), goInfo.Length[0][f], "v%d length[%d]", variant, f)
		assert.Equalf(t, int(c.order[fi]), goInfo.Order[0][f], "v%d order[%d]", variant, f)
		assert.Equalf(t, int(c.direction[fi]), goInfo.Direction[0][f], "v%d direction[%d]", variant, f)
		assert.Equalf(t, int(c.sbTnsActive[fi]), goData.LongSubBlock.TnsActive[f], "v%d tnsActive[%d]", variant, f)
		assert.Equalf(t, int(c.sbPredictionGain[fi]), goData.LongSubBlock.PredictionGain[f], "v%d predictionGain[%d]", variant, f)
		for k := 0; k < tnsMaxOrder; k++ {
			assert.Equalf(t, int(c.coef[fi][k]), goInfo.Coef[0][f][k], "v%d coef[%d][%d]", variant, f, k)
		}
	}
}

// TestAutoToParcorParity isolates the LeRoux-Gueguen/Schur ParCor analysis
// (CLpc_AutoToParcor) — the load-bearing LPC step inside the decision.
func TestAutoToParcorParity(t *testing.T) {
	rng := newLCG(0x1234abcd)
	for trial := 0; trial < 40; trial++ {
		order := 4 + trial%9 // 4..12
		acorr := make([]int32, order+1)
		// acorr[0] is the energy (largest, positive); the rest decay.
		acorr[0] = int32(0x40000000) + int32(rng.next()%0x20000000)
		for i := 1; i <= order; i++ {
			// Random lags strictly smaller in magnitude than acorr[0].
			v := int32(rng.next()%0x30000000) - int32(0x18000000)
			acorr[i] = v
		}

		// C mutates acorr in place; give each its own copy.
		cIn := append([]int32(nil), acorr...)
		goIn := append([]int32(nil), acorr...)

		cRefl, cGm, cGe := cAutoToParcor(cIn, order)
		goRefl, goGm, goGe := nativeaac.EncClpcAutoToParcor(goIn, order)

		assert.Equalf(t, cGm, goGm, "trial%d predGain mantissa", trial)
		assert.Equalf(t, cGe, goGe, "trial%d predGain exponent", trial)
		assert.Equalf(t, cRefl, goRefl, "trial%d reflCoeff", trial)
		// The in-place-mutated acorr must also match bit-for-bit.
		assert.Equalf(t, cIn, goIn, "trial%d acorr mutation", trial)
	}
}

// TestAutoToParcorZeroEnergy exercises the autoCorr_0 == 0 branch.
func TestAutoToParcorZeroEnergy(t *testing.T) {
	order := 8
	acorr := make([]int32, order+1) // all zeros
	cIn := append([]int32(nil), acorr...)
	goIn := append([]int32(nil), acorr...)
	cRefl, cGm, cGe := cAutoToParcor(cIn, order)
	goRefl, goGm, goGe := nativeaac.EncClpcAutoToParcor(goIn, order)
	assert.Equal(t, cGm, goGm)
	assert.Equal(t, cGe, goGe)
	assert.Equal(t, cRefl, goRefl)
}

// TestMergedAutoCorrParity isolates the static FDKaacEnc_MergedAutoCorrelation.
func TestMergedAutoCorrParity(t *testing.T) {
	cfg, sfbActive := cBuildConfig(128000, 44100, 2, 1)
	require.GreaterOrEqual(t, sfbActive, 0)
	goCfg := goConfigFromC(cfg)

	// Flatten acfWindow [2][acfWinSize] for the C bridge.
	acfFlat := make([]int32, 2*acfWinSize)
	for f := 0; f < 2; f++ {
		for i := 0; i < acfWinSize; i++ {
			acfFlat[f*acfWinSize+i] = goCfg.AcfWindow[f][i]
		}
	}
	lpcStartLine := []int32{int32(goCfg.LpcStartLine[0]), int32(goCfg.LpcStartLine[1])}
	acfSplit := []int32{int32(goCfg.ConfTab.AcfSplit[0]), int32(goCfg.ConfTab.AcfSplit[1])}

	scratch := make([]int32, 1024)
	for variant := 0; variant < 6; variant++ {
		spectrum := makeSpectrum(1024, variant)

		cRxx1, cRxx2 := cMergedAutoCorr(spectrum, goCfg.IsLowDelay, acfFlat,
			lpcStartLine, goCfg.LpcStopLine, goCfg.MaxOrder, acfSplit)

		var goRxx1 [tnsMaxOrder + 1]int32
		var goRxx2 [tnsMaxOrder + 1]int32
		goSpec := append([]int32(nil), spectrum...)
		var acfWin [2][acfWinSize]int32
		acfWin = goCfg.AcfWindow
		var startLine [2]int
		startLine = goCfg.LpcStartLine
		var split [2]int
		split = goCfg.ConfTab.AcfSplit
		nativeaac.EncMergedAutoCorrelation(goSpec, goCfg.IsLowDelay, &acfWin,
			&startLine, goCfg.LpcStopLine, goCfg.MaxOrder, &split,
			goRxx1[:], goRxx2[:], scratch)

		assert.Equalf(t, cRxx1, goRxx1[:], "v%d rxx1", variant)
		assert.Equalf(t, cRxx2, goRxx2[:], "v%d rxx2", variant)
	}
}

// lcg is a tiny deterministic PRNG so the trials are reproducible.
type lcg struct{ state uint64 }

func newLCG(seed uint64) *lcg { return &lcg{state: seed} }
func (r *lcg) next() int32 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return int32(r.state >> 33)
}
