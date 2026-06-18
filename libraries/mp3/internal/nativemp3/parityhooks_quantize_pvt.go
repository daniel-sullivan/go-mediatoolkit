// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// Exported test hooks for the quantize-pvt parity oracle
// (internal/parity_tests/quantize-pvt).
//
// The ATH shaping (athAdjust / athmdct / computeATH), the per-band allowed-
// distortion budget (calcXmin) and the quantization-noise measure (calcNoise)
// are the floating-point surface of LAME's quantizer-support TU quantize_pvt.c.
// athmdct / computeATH / calcNoiseCoreC are 1:1 translations of LAME `static`
// functions (and calcXmin/calcNoise/athAdjust of the public ones) with no place
// in the public surface; the wrappers below exist solely so the parity suite —
// which lives in its own package because it compiles the vendored C oracle — can
// assert the Go port matches the vendored C bit-for-bit under the mp3_strict
// build. They are mp3lame-gated like the slice they expose; a bare
// `go build ./...` never compiles the LGPL-fenced quantizer.
//
// Each wrapper accepts the SAME flat input arrays the C oracle's trampolines
// take and builds the LameInternalFlags / GrInfo / III_psy_ratio internally,
// mirroring oracle.c field-for-field so the two sides of every assertion operate
// on byte-identical inputs.

// FillQuantizePvtTables runs InitQuantizePvtTables (the iteration_init table
// fill) so the calcNoise pow43[]/pow20[] lookups resolve, matching the oracle's
// oracle_fill_tables. (The oracle drives the genuine iteration_init; the Go
// table fill is the same loop.)
func FillQuantizePvtTables() { InitQuantizePvtTables() }

// AthAdjust exposes athAdjust (quantize_pvt.c:555).
func AthAdjust(a, x, athFloor, athFixpoint float32) float32 {
	return athAdjust(a, x, athFloor, athFixpoint)
}

// Athmdct exposes the static ATHmdct (quantize_pvt.c:211). It builds a
// SessionConfig from the flat ATH parameters and runs the kernel; samplerate_out
// is fixed to 44100 (ATHmdct does not read it) for symmetry with oracle_athmdct.
func Athmdct(athType int, athCurve, athOffsetDb, athFixpoint, f float32) float32 {
	cfg := &SessionConfig{
		SamplerateOut: 44100,
		ATHtype:       athType,
		ATHcurve:      athCurve,
		ATHOffsetDb:   athOffsetDb,
		ATHfixpoint:   athFixpoint,
	}
	return athmdct(cfg, f)
}

// ComputeATH exposes the static compute_ath (quantize_pvt.c:231). It builds a
// LameInternalFlags from the flat cfg + scalefac_band, runs compute_ath, and
// returns the four ATH floor arrays plus ATH->floor.
func ComputeATH(samplerateOut, athType int, athCurve, athOffsetDb, athFixpoint float32, noath int,
	sbL, sbS, psfb21, psfb12 []int) (l, p21, s, p12 []float32, floor float32) {
	gfc := new(LameInternalFlags)
	gfc.ATH = new(ATH)
	gfc.Cfg.SamplerateOut = samplerateOut
	gfc.Cfg.ATHtype = athType
	gfc.Cfg.ATHcurve = athCurve
	gfc.Cfg.ATHOffsetDb = athOffsetDb
	gfc.Cfg.ATHfixpoint = athFixpoint
	gfc.Cfg.NoATH = noath
	for i := 0; i < SBMAXl+1; i++ {
		gfc.ScalefacBand.L[i] = sbL[i]
	}
	for i := 0; i < SBMAXs+1; i++ {
		gfc.ScalefacBand.S[i] = sbS[i]
	}
	for i := 0; i < PSFB21+1; i++ {
		gfc.ScalefacBand.Psfb21[i] = psfb21[i]
	}
	for i := 0; i < PSFB12+1; i++ {
		gfc.ScalefacBand.Psfb12[i] = psfb12[i]
	}
	computeATH(gfc)
	l = append([]float32(nil), gfc.ATH.L[:]...)
	p21 = append([]float32(nil), gfc.ATH.Psfb21[:]...)
	s = append([]float32(nil), gfc.ATH.S[:]...)
	p12 = append([]float32(nil), gfc.ATH.Psfb12[:]...)
	floor = gfc.ATH.Floor
	return
}

// CalcXminResult bundles calcXmin's outputs for the parity oracle.
type CalcXminResult struct {
	Xmin       []float32
	Eac        []byte
	MaxNonzero int
	AthOver    int
}

// CalcXmin exposes calcXmin (quantize_pvt.c:590), building the LameInternalFlags
// / GrInfo / III_psy_ratio from the flat inputs (the same fields oracle_calc_xmin
// populates) and returning the pxmin budget, the energy_above_cutoff flags,
// max_nonzero_coeff and the ath_over count.
func CalcXmin(
	samplerateOut int, athFixpoint float32, sfb21Extra, useTemporal int,
	sbL, sbS []int,
	athAdjustFactor, athFloor float32, athL, athS []float32,
	longfact, shortfact []float32, decay float32,
	xr []float32, width []int, psyLmax, psymax, sfbSmin, blockType int,
	enL, thmL, enS, thmS []float32) CalcXminResult {

	gfc := new(LameInternalFlags)
	gfc.ATH = new(ATH)
	gfc.CdPsy = new(PsyConst)
	gfc.Cfg.SamplerateOut = samplerateOut
	gfc.Cfg.ATHfixpoint = athFixpoint
	gfc.SvQnt.Sfb21Extra = sfb21Extra
	gfc.Cfg.UseTemporalMaskingEffect = useTemporal
	for i := 0; i < SBMAXl+1; i++ {
		gfc.ScalefacBand.L[i] = sbL[i]
	}
	for i := 0; i < SBMAXs+1; i++ {
		gfc.ScalefacBand.S[i] = sbS[i]
	}
	gfc.ATH.AdjustFactor = athAdjustFactor
	gfc.ATH.Floor = athFloor
	for i := 0; i < SBMAXl; i++ {
		gfc.ATH.L[i] = athL[i]
		gfc.SvQnt.Longfact[i] = longfact[i]
	}
	for i := 0; i < SBMAXs; i++ {
		gfc.ATH.S[i] = athS[i]
		gfc.SvQnt.Shortfact[i] = shortfact[i]
	}
	gfc.CdPsy.Decay = decay

	var gi GrInfo
	copy(gi.Xr[:], xr)
	for i := 0; i < SFBMAX; i++ {
		gi.Width[i] = width[i]
	}
	gi.PsyLmax = psyLmax
	gi.Psymax = psymax
	gi.SfbSmin = sfbSmin
	gi.BlockType = blockType

	var ratio III_psy_ratio
	for i := 0; i < SBMAXl; i++ {
		ratio.En.L[i] = enL[i]
		ratio.Thm.L[i] = thmL[i]
	}
	for i := 0; i < SBMAXs; i++ {
		for b := 0; b < 3; b++ {
			ratio.En.S[i][b] = enS[i*3+b]
			ratio.Thm.S[i][b] = thmS[i*3+b]
		}
	}

	pxmin := make([]float32, SFBMAX)
	athOver := calcXmin(gfc, &ratio, &gi, pxmin)

	eac := make([]byte, SFBMAX)
	for i := 0; i < SFBMAX; i++ {
		eac[i] = gi.EnergyAboveCutoff[i]
	}
	return CalcXminResult{
		Xmin:       pxmin,
		Eac:        eac,
		MaxNonzero: gi.MaxNonzeroCoeff,
		AthOver:    athOver,
	}
}

// CalcNoiseResultOut bundles calcNoise's outputs for the parity oracle.
type CalcNoiseResultOut struct {
	Distort   []float32
	OverNoise float32
	TotNoise  float32
	MaxNoise  float32
	OverCount int
	OverSSD   int
	Over      int
}

// CalcNoise exposes calcNoise (quantize_pvt.c:816) with prevNoise == nil,
// building the GrInfo from the flat side-info + xr + l3_enc and running over the
// supplied l3_xmin (the same surface oracle_calc_noise drives).
func CalcNoise(
	xr []float32, l3enc, scalefac, width, window, subblockGain []int,
	globalGain, scalefacScale, preflag, psymax, maxNonzeroCoeff, count1, bigValues int,
	l3xmin []float32) CalcNoiseResultOut {

	var gi GrInfo
	copy(gi.Xr[:], xr)
	for i := 0; i < 576; i++ {
		gi.L3Enc[i] = l3enc[i]
	}
	for i := 0; i < SFBMAX; i++ {
		gi.Scalefac[i] = scalefac[i]
		gi.Width[i] = width[i]
		gi.Window[i] = window[i]
	}
	for i := 0; i < 4; i++ {
		gi.SubblockGain[i] = subblockGain[i]
	}
	gi.GlobalGain = globalGain
	gi.ScalefacScale = scalefacScale
	gi.Preflag = preflag
	gi.Psymax = psymax
	gi.MaxNonzeroCoeff = maxNonzeroCoeff
	gi.Count1 = count1
	gi.BigValues = bigValues

	distort := make([]float32, SFBMAX)
	var res CalcNoiseResult
	over := calcNoise(&gi, l3xmin, distort, &res, nil)

	return CalcNoiseResultOut{
		Distort:   distort,
		OverNoise: res.OverNoise,
		TotNoise:  res.TotNoise,
		MaxNoise:  res.MaxNoise,
		OverCount: res.OverCount,
		OverSSD:   res.OverSSD,
		Over:      over,
	}
}
