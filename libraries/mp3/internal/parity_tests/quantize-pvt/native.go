// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package quantizepvt

import "go-mediatoolkit/libraries/mp3/internal/nativemp3"

// native.go drives the pure-Go nativemp3 port the same way oracle.c drives the
// vendored C: each wrapper forwards the identical flat inputs into the
// nativemp3 parity hook, which builds the LameInternalFlags / GrInfo /
// III_psy_ratio internally exactly as oracle.c does. Keeping the Go drivers
// beside the cgo bridge mirrors the C oracle structure so the two sides of each
// assertion are visibly symmetric. These import only internal/nativemp3 (never
// libraries/mp3).

func goFillTables(samplerateOut, athType int, athCurve, athOffsetDb, athFixpoint float32, noath int,
	sbL, sbS, psfb21, psfb12 []int, adjustLong, adjustShort []float32) {
	nativemp3.FillQuantizePvtTables()
}

func goAthAdjust(a, x, athFloor, athFixpoint float32) float32 {
	return nativemp3.AthAdjust(a, x, athFloor, athFixpoint)
}

func goAthmdct(athType int, athCurve, athOffsetDb, athFixpoint, f float32) float32 {
	return nativemp3.Athmdct(athType, athCurve, athOffsetDb, athFixpoint, f)
}

func goComputeATH(samplerateOut, athType int, athCurve, athOffsetDb, athFixpoint float32, noath int,
	sbL, sbS, psfb21, psfb12 []int) (l, p21, s, p12 []float32, floor float32) {
	return nativemp3.ComputeATH(samplerateOut, athType, athCurve, athOffsetDb, athFixpoint, noath,
		sbL, sbS, psfb21, psfb12)
}

func goCalcXmin(
	samplerateOut int, athFixpoint float32, sfb21Extra, useTemporal int,
	sbL, sbS []int,
	athAdjustFactor, athFloor float32, athL, athS []float32,
	longfact, shortfact []float32, decay float32,
	xr []float32, width []int, psyLmax, psymax, sfbSmin, blockType int,
	enL, thmL, enS, thmS []float32) xminResult {
	r := nativemp3.CalcXmin(
		samplerateOut, athFixpoint, sfb21Extra, useTemporal,
		sbL, sbS,
		athAdjustFactor, athFloor, athL, athS,
		longfact, shortfact, decay,
		xr, width, psyLmax, psymax, sfbSmin, blockType,
		enL, thmL, enS, thmS)
	return xminResult{xmin: r.Xmin, eac: r.Eac, maxNonzero: r.MaxNonzero, athOver: r.AthOver}
}

func goCalcNoise(
	xr []float32, l3enc, scalefac, width, window, subblockGain []int,
	globalGain, scalefacScale, preflag, psymax, maxNonzeroCoeff, count1, bigValues int,
	l3xmin []float32) noiseResult {
	r := nativemp3.CalcNoise(
		xr, l3enc, scalefac, width, window, subblockGain,
		globalGain, scalefacScale, preflag, psymax, maxNonzeroCoeff, count1, bigValues,
		l3xmin)
	return noiseResult{
		distort:   r.Distort,
		overNoise: r.OverNoise,
		totNoise:  r.TotNoise,
		maxNoise:  r.MaxNoise,
		overCount: r.OverCount,
		overSSD:   r.OverSSD,
		over:      r.Over,
	}
}
