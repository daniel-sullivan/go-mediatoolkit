// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

import "math"

// Preset id constants (include/lame.h enum preset_mode_e, lame.h:84-118). The
// VBR-quality presets V9..V0 map 410..500 in steps of 10; the legacy aliases sit
// at 1000+.
const (
	presetV9           = 410
	presetV8           = 420
	presetV7           = 430
	presetV6           = 440
	presetV5           = 450
	presetV4           = 460
	presetV3           = 470
	presetV2           = 480
	presetV1           = 490
	presetV0           = 500
	presetR3MIX        = 1000
	presetSTANDARD     = 1001
	presetEXTREME      = 1002
	presetINSANE       = 1003
	presetSTANDARDFAST = 1004
	presetEXTREMEFAST  = 1005
	presetMEDIUM       = 1006
	presetMEDIUMFAST   = 1007
)

// presets.go — a 1:1 Go translation of LAME 3.100's libmp3lame/presets.c
// (apply_vbr_preset / apply_abr_preset / apply_preset + the vbr_old_switch_map /
// vbr_mt_psy_switch_map / abr_switch_map tables and the get_vbr_preset selector,
// nearestBitrateFullIndex from util.c:351 for the ABR row index). lame_init_params
// (init.go) reaches apply_preset through the EncoderStages seam exactly where the C
// lame.c:995/1020/1057 calls apply_preset(gfp, 500-VBR_q*10, 0) for the VBR modes
// and apply_preset(gfp, VBR_mean_bitrate_kbps, 0) for cbr/abr.
//
// The preset installs the perceptual tuning a -V level expects into gfp (and two
// fields, minval / ATHfixpoint, directly into cfg because LAME has no gfp setter
// for them). lame_init_params then copies the gfp tuning into cfg AFTER this runs
// (init.go:751-797), so the per-frame psymodel + quantizer read the preset values:
//   - quant_comp / quant_comp_short -> cfg.QuantComp / QuantCompShort (init.go:761)
//   - ATHtype / ATHcurve / ATH_lower_db / athaa_sensitivity -> the ATH shaping
//     (init.go:705-711, 753-756)
//   - msfix / interChRatio / maskingadjust[_short] -> cfg.Msfix / InterChRatio /
//     SvQnt.MaskAdjust[Short] (init.go:654-655, 751-752)
//   - attackthre / attackthre_s (short_threshold_lrm/_s) -> psymodel_init's
//     gd.attack_threshold (psymodel.c:2126 read site; ported in psymodel_init.go:432
//     via PsyInitParams from psyInitParamsFromGfp)
//   - exp_nspsytune safejoint bit (|2) / sfb21mod (<<20) -> cfg.UseSafeJointStereo
//     / cfg.AdjustSfb21Db (init.go:766, 791-796)
//   - cfg.Minval -> psymodel_init's minval_low (psymodel.c:1895 read site;
//     psymodel_init.go:239)
//   - cfg.ATHfixpoint -> athAdjust / the ATH energy floor (quantize_pvt.c:218,605,692
//     read sites; quantize_pvt.go)
//
// FP discipline: the LERP between two switch-map rows is gfp->VBR_q_frac-weighted;
// for the integral -V levels the public encoder uses, VBR_q_frac == 0, so each LERP
// collapses to the lower row exactly (no FMA-sensitive intermediate). The
// ath_fixpoint gain adjustment uses log10(|scale|) in double precision, matching
// presets.c:208-212; for scale == 1 it is 0.

// vbrPresets is presets.c's vbr_presets_t (presets.c:67-85): one switch-map row.
type vbrPresets struct {
	vbrQ           int     // vbr_q
	quantComp      int     // quant_comp
	quantCompS     int     // quant_comp_s
	expY           int     // expY
	stLrm          float32 // st_lrm (short threshold L/R/M)
	stS            float32 // st_s   (short threshold S)
	maskingAdj     float32 // masking_adj
	maskingAdjShrt float32 // masking_adj_short
	athLower       float32 // ath_lower
	athCurve       float32 // ath_curve
	athSensitivity float32 // ath_sensitivity
	interch        float32 // interch (inter-channel ratio)
	safejoint      int     // safejoint
	sfb21mod       int     // sfb21mod
	msfix          float32 // msfix
	minval         float32 // minval
	athFixpoint    float32 // ath_fixpoint
}

// vbrOldSwitchMap is presets.c:90-103, the switch mappings for VBR mode vbr_rh.
var vbrOldSwitchMap = [...]vbrPresets{
	/*vbr_q  qcomp_l qcomp_s expY  st_lrm  st_s   mask_l mask_s ath_lwr ath_crv ath_sens interch safejoint sfb21mod msfix minval athfp */
	{0, 9, 9, 0, 5.20, 125.0, -4.2, -6.3, 4.8, 1, 0, 0, 2, 21, 0.97, 5, 100},
	{1, 9, 9, 0, 5.30, 125.0, -3.6, -5.6, 4.5, 1.5, 0, 0, 2, 21, 1.35, 5, 100},
	{2, 9, 9, 0, 5.60, 125.0, -2.2, -3.5, 2.8, 2, 0, 0, 2, 21, 1.49, 5, 100},
	{3, 9, 9, 1, 5.80, 130.0, -1.8, -2.8, 2.6, 3, -4, 0, 2, 20, 1.64, 5, 100},
	{4, 9, 9, 1, 6.00, 135.0, -0.7, -1.1, 1.1, 3.5, -8, 0, 2, 0, 1.79, 5, 100},
	{5, 9, 9, 1, 6.40, 140.0, 0.5, 0.4, -7.5, 4, -12, 0.0002, 0, 0, 1.95, 5, 100},
	{6, 9, 9, 1, 6.60, 145.0, 0.67, 0.65, -14.7, 6.5, -19, 0.0004, 0, 0, 2.30, 5, 100},
	{7, 9, 9, 1, 6.60, 145.0, 0.8, 0.75, -19.7, 8, -22, 0.0006, 0, 0, 2.70, 5, 100},
	{8, 9, 9, 1, 6.60, 145.0, 1.2, 1.15, -27.5, 10, -23, 0.0007, 0, 0, 0, 5, 100},
	{9, 9, 9, 1, 6.60, 145.0, 1.6, 1.6, -36, 11, -25, 0.0008, 0, 0, 0, 5, 100},
	{10, 9, 9, 1, 6.60, 145.0, 2.0, 2.0, -36, 12, -25, 0.0008, 0, 0, 0, 5, 100},
}

// vbrMtPsySwitchMap is presets.c:105-123, the switch mappings for VBR mode
// vbr_mtrh / vbr_mt (the -V levels). The #if-0 rows 6/7 in the C source select the
// #else variants (the active code path); those are the ones transcribed here.
var vbrMtPsySwitchMap = [...]vbrPresets{
	/*vbr_q  qcomp_l qcomp_s expY  st_lrm  st_s   mask_l mask_s ath_lwr ath_crv ath_sens interch safejoint sfb21mod msfix  minval athfp */
	{0, 9, 9, 0, 4.20, 25.0, -6.8, -6.8, 7.1, 1, 0, 0, 2, 31, 1.000, 5, 100},
	{1, 9, 9, 0, 4.20, 25.0, -4.8, -4.8, 5.4, 1.4, -1, 0, 2, 27, 1.122, 5, 98},
	{2, 9, 9, 0, 4.20, 25.0, -2.6, -2.6, 3.7, 2.0, -3, 0, 2, 23, 1.288, 5, 97},
	{3, 9, 9, 1, 4.20, 25.0, -1.6, -1.6, 2.0, 2.0, -5, 0, 2, 18, 1.479, 5, 96},
	{4, 9, 9, 1, 4.20, 25.0, -0.0, -0.0, 0.0, 2.0, -8, 0, 2, 12, 1.698, 5, 95},
	{5, 9, 9, 1, 4.20, 25.0, 1.3, 1.3, -6, 3.5, -11, 0, 2, 8, 1.950, 5, 94.2},
	{6, 9, 9, 1, 4.50, 100.0, 2.2, 2.3, -12.0, 6.0, -14, 0, 2, 4, 2.239, 3, 93.9},
	{7, 9, 9, 1, 4.80, 200.0, 2.7, 2.7, -18.0, 9.0, -17, 0, 2, 0, 2.570, 1, 93.6},
	{8, 9, 9, 1, 5.30, 300.0, 2.8, 2.8, -21.0, 10.0, -23, 0.0002, 0, 0, 2.951, 0, 93.3},
	{9, 9, 9, 1, 6.60, 300.0, 2.8, 2.8, -23.0, 11.0, -25, 0.0006, 0, 0, 3.388, 0, 93.3},
	{10, 9, 9, 1, 25.00, 300.0, 2.8, 2.8, -25.0, 12.0, -27, 0.0025, 0, 0, 3.500, 0, 93.3},
}

// getVbrPreset is presets.c:127-137: select the switch map for the active VBR mode.
// vbr_mtrh / vbr_mt use vbr_mt_psy_switch_map; everything else uses vbr_old_switch_map.
func getVbrPreset(v int) []vbrPresets {
	switch v {
	case vbrMtrh, vbrMt:
		return vbrMtPsySwitchMap[:]
	default:
		return vbrOldSwitchMap[:]
	}
}

// abrPresets is the apply_abr_preset-local abr_presets_t (presets.c:218-232).
type abrPresets struct {
	abrKbps    int     // abr_kbps
	quantComp  int     // quant_comp
	quantCompS int     // quant_comp_s
	safejoint  int     // safejoint
	nsmsfix    float32 // nsmsfix
	stLrm      float32 // st_lrm
	stS        float32 // st_s
	scale      float32 // scale
	maskingAdj float32 // masking_adj
	athLower   float32 // ath_lower
	athCurve   float32 // ath_curve
	interch    float32 // interch
	sfscale    int     // sfscale
}

// abrSwitchMap is presets.c:240-259, the switch mappings for ABR mode.
var abrSwitchMap = [...]abrPresets{
	/* kbps quant q_s safejoint nsmsfix st_lrm st_s  scale msk ath_lwr ath_crv interch sfscale */
	{8, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -30.0, 11, 0.0012, 1},
	{16, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -25.0, 11, 0.0010, 1},
	{24, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -20.0, 11, 0.0010, 1},
	{32, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -15.0, 11, 0.0010, 1},
	{40, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -10.0, 11, 0.0009, 1},
	{48, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -10.0, 11, 0.0009, 1},
	{56, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -6.0, 11, 0.0008, 1},
	{64, 9, 9, 0, 0, 6.60, 145, 0.95, 0, -2.0, 11, 0.0008, 1},
	{80, 9, 9, 0, 0, 6.60, 145, 0.95, 0, .0, 8, 0.0007, 1},
	{96, 9, 9, 0, 2.50, 6.60, 145, 0.95, 0, 1.0, 5.5, 0.0006, 1},
	{112, 9, 9, 0, 2.25, 6.60, 145, 0.95, 0, 2.0, 4.5, 0.0005, 1},
	{128, 9, 9, 0, 1.95, 6.40, 140, 0.95, 0, 3.0, 4, 0.0002, 1},
	{160, 9, 9, 1, 1.79, 6.00, 135, 0.95, -2, 5.0, 3.5, 0, 1},
	{192, 9, 9, 1, 1.49, 5.60, 125, 0.97, -4, 7.0, 3, 0, 0},
	{224, 9, 9, 1, 1.25, 5.20, 125, 0.98, -6, 9.0, 2, 0, 0},
	{256, 9, 9, 1, 0.97, 5.20, 125, 1.00, -8, 10.0, 1, 0, 0},
	{320, 9, 9, 1, 0.90, 5.20, 125, 1.00, -10, 12.0, 0, 0, 0},
}

// nearestBitrateFullIndex is a 1:1 translation of util.c:351: pick the
// abr_switch_map row index whose full-bitrate-table value the bitrate is closest
// to (ties resolve upward).
func nearestBitrateFullIndex(bitrate int) int {
	fullBitrateTable := [...]int{8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}

	upperRangeKbps := fullBitrateTable[16]
	upperRange := 16
	lowerRangeKbps := fullBitrateTable[16]
	lowerRange := 16

	for b := 0; b < 16; b++ {
		if maxInt(bitrate, fullBitrateTable[b+1]) != bitrate {
			upperRangeKbps = fullBitrateTable[b+1]
			upperRange = b + 1
			lowerRangeKbps = fullBitrateTable[b]
			lowerRange = b
			break
		}
	}

	if (upperRangeKbps - bitrate) > (bitrate - lowerRangeKbps) {
		return lowerRange
	}
	return upperRange
}

// setOption mirrors the SET_OPTION/SET__OPTION macros (presets.c:34-42): when
// enforce, install val unconditionally; otherwise install val only if the current
// gfp field still holds the documented default sentinel def (|cur-def| not > 0,
// i.e. cur == def). cur/def are the float-valued gfp getter values. Returns the
// resulting value to assign back to the gfp field.
func setOption(cur, val, def float32, enforce bool) float32 {
	if enforce {
		return val
	}
	if !(absFloat32(cur-def) > 0) {
		return val
	}
	return cur
}

// setOptionInt is setOption for the int-valued options (quant_comp /
// quant_comp_short); the C macro applies fabs on the int-converted-to-double, so
// the comparison is exact-integer.
func setOptionInt(cur, val, def int, enforce bool) int {
	if enforce {
		return val
	}
	if !(absInt(cur-def) > 0) {
		return val
	}
	return cur
}

// absFloat32 is the C fabs() narrowed to the float32 the SET_OPTION comparison
// operates on.
func absFloat32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

// lerpF is presets.c's LERP macro (presets.c:140): p + x*(q-p) in single
// precision. For VBR_q_frac == 0 (the integral -V levels) this is exactly p.
func lerpF(p, q, x float32) float32 {
	return p + x*(q-p)
}

// applyVbrPreset is a 1:1 translation of presets.c:142-213. a is the switch-map
// row index (= the V level); enforce is 0 for lame_init_params. It writes the
// per-V tuning into gfp (and minval / ATHfixpoint directly into cfg).
func (gfc *LameInternalFlags) applyVbrPreset(gfp *LameGlobalFlags, a int, enforce bool) {
	vbrPreset := getVbrPreset(gfp.VBR)
	x := gfp.VBRqFrac
	p := vbrPreset[a]
	q := vbrPreset[a+1]
	set := p // vbrPresets_t const *set = &p (presets.c:149)

	// NOOP(vbr_q/quant_comp/quant_comp_s/expY) / NOOP(safejoint) keep p's values;
	// the LERP'd fields blend toward q by x (presets.c:151-167).
	set.stLrm = lerpF(p.stLrm, q.stLrm, x)
	set.stS = lerpF(p.stS, q.stS, x)
	set.maskingAdj = lerpF(p.maskingAdj, q.maskingAdj, x)
	set.maskingAdjShrt = lerpF(p.maskingAdjShrt, q.maskingAdjShrt, x)
	set.athLower = lerpF(p.athLower, q.athLower, x)
	set.athCurve = lerpF(p.athCurve, q.athCurve, x)
	set.athSensitivity = lerpF(p.athSensitivity, q.athSensitivity, x)
	set.interch = lerpF(p.interch, q.interch, x)
	set.sfb21mod = int(lerpF(float32(p.sfb21mod), float32(q.sfb21mod), x))
	set.msfix = lerpF(p.msfix, q.msfix, x)
	set.minval = lerpF(p.minval, q.minval, x)
	set.athFixpoint = lerpF(p.athFixpoint, q.athFixpoint, x)

	// lame_set_VBR_q(set->vbr_q) (presets.c:169).
	gfp.VBRq = set.vbrQ
	// quant_comp / quant_comp_short, def -1 (presets.c:170-171).
	gfp.QuantComp = setOptionInt(gfp.QuantComp, set.quantComp, -1, enforce)
	gfp.QuantCompShort = setOptionInt(gfp.QuantCompShort, set.quantCompS, -1, enforce)
	if set.expY != 0 { // lame_set_experimentalY (presets.c:172-174).
		gfp.ExperimentalY = set.expY
	}
	// short_threshold_lrm/_s -> attackthre/attackthre_s, def -1 (presets.c:175-176).
	gfp.Attackthre = setOption(gfp.Attackthre, set.stLrm, -1, enforce)
	gfp.AttackthreS = setOption(gfp.AttackthreS, set.stS, -1, enforce)
	// maskingadjust[_short], def 0 (presets.c:177-178).
	gfp.Maskingadjust = setOption(gfp.Maskingadjust, set.maskingAdj, 0, enforce)
	gfp.MaskingadjustShort = setOption(gfp.MaskingadjustShort, set.maskingAdjShrt, 0, enforce)
	// ATHtype = 5 for vbr_mt/vbr_mtrh (presets.c:179-181).
	if gfp.VBR == vbrMt || gfp.VBR == vbrMtrh {
		gfp.ATHtype = 5
	}
	// ATHlower -> ATH_lower_db, def 0; ATHcurve, def -1; athaa_sensitivity, def 0
	// (presets.c:182-184).
	gfp.ATHLowerDb = setOption(gfp.ATHLowerDb, set.athLower, 0, enforce)
	gfp.ATHcurve = setOption(gfp.ATHcurve, set.athCurve, -1, enforce)
	gfp.AthaaSensitivity = setOption(gfp.AthaaSensitivity, set.athSensitivity, 0, enforce)
	if set.interch > 0 { // interChRatio, def -1 (presets.c:185-187).
		gfp.InterChRatio = setOption(gfp.InterChRatio, set.interch, -1, enforce)
	}

	// parameters for which there is no proper set/get interface (presets.c:189-200).
	if set.safejoint > 0 {
		gfp.ExpNspsytune = gfp.ExpNspsytune | 2
	}
	if set.sfb21mod > 0 {
		nsp := gfp.ExpNspsytune
		val := (nsp >> 20) & 63
		if val == 0 {
			sf21mod := (set.sfb21mod << 20) | nsp
			gfp.ExpNspsytune = sf21mod
		}
	}
	// msfix, def -1 (SET__OPTION, presets.c:201).
	gfp.Msfix = setOption(gfp.Msfix, set.msfix, -1, enforce)

	if !enforce { // presets.c:203-206.
		gfp.VBRq = a
		gfp.VBRqFrac = x
	}
	// minval / ATHfixpoint have no gfp setter — written straight to cfg
	// (presets.c:207-212). The gain adjustment uses double-precision log10 of the
	// absolute output scale, exactly as the C.
	gfc.Cfg.Minval = set.minval
	{
		sx := math.Abs(float64(gfp.Scale))
		var y float64
		if sx > 0.0 {
			y = 10.0 * math.Log10(sx)
		}
		gfc.Cfg.ATHfixpoint = set.athFixpoint - float32(y)
	}
}

// applyAbrPreset is a 1:1 translation of presets.c:215-315. preset is the target
// ABR/CBR bitrate in kbps; enforce is 0 for lame_init_params. Returns preset.
func (gfc *LameInternalFlags) applyAbrPreset(gfp *LameGlobalFlags, preset int, enforce bool) int {
	actualBitrate := preset

	r := nearestBitrateFullIndex(preset)

	gfp.VBR = vbrAbr // lame_set_VBR(vbr_abr) (presets.c:269).
	gfp.VBRMeanBitrate = actualBitrate
	gfp.VBRMeanBitrate = minInt(gfp.VBRMeanBitrate, 320)
	gfp.VBRMeanBitrate = maxInt(gfp.VBRMeanBitrate, 8)
	gfp.Brate = gfp.VBRMeanBitrate // lame_set_brate (presets.c:273).

	m := abrSwitchMap[r]

	// parameters for which there is no proper set/get interface (presets.c:276-281).
	if m.safejoint > 0 {
		gfp.ExpNspsytune = gfp.ExpNspsytune | 2
	}
	if m.sfscale > 0 {
		// lame_set_sfscale(gfp, 1) sets gfp->noise_shaping = 2 (set_get.c:1762).
		gfp.NoiseShaping = 2
	}

	// quant_comp / quant_comp_short, def -1 (presets.c:284-285).
	gfp.QuantComp = setOptionInt(gfp.QuantComp, m.quantComp, -1, enforce)
	gfp.QuantCompShort = setOptionInt(gfp.QuantCompShort, m.quantCompS, -1, enforce)

	// msfix, def -1 (SET__OPTION, presets.c:287).
	gfp.Msfix = setOption(gfp.Msfix, m.nsmsfix, -1, enforce)

	// short_threshold_lrm/_s -> attackthre/attackthre_s, def -1 (presets.c:289-290).
	gfp.Attackthre = setOption(gfp.Attackthre, m.stLrm, -1, enforce)
	gfp.AttackthreS = setOption(gfp.AttackthreS, m.stS, -1, enforce)

	// lame_set_scale(gfp, lame_get_scale(gfp) * scale) (presets.c:294).
	gfp.Scale = gfp.Scale * m.scale

	// maskingadjust, def 0; maskingadjust_short = masking_adj * .9 / *1.1
	// (presets.c:296-302).
	gfp.Maskingadjust = setOption(gfp.Maskingadjust, m.maskingAdj, 0, enforce)
	if m.maskingAdj > 0 {
		gfp.MaskingadjustShort = setOption(gfp.MaskingadjustShort, m.maskingAdj*0.9, 0, enforce)
	} else {
		gfp.MaskingadjustShort = setOption(gfp.MaskingadjustShort, m.maskingAdj*1.1, 0, enforce)
	}

	// ATHlower -> ATH_lower_db, def 0; ATHcurve, def -1 (presets.c:305-306).
	gfp.ATHLowerDb = setOption(gfp.ATHLowerDb, m.athLower, 0, enforce)
	gfp.ATHcurve = setOption(gfp.ATHcurve, m.athCurve, -1, enforce)

	// interChRatio, def -1 (presets.c:308).
	gfp.InterChRatio = setOption(gfp.InterChRatio, m.interch, -1, enforce)

	// cfg.minval = 5. * (abr_kbps / 320.) (presets.c:312).
	gfc.Cfg.Minval = 5.0 * (float32(m.abrKbps) / 320.0)

	return preset
}

// applyPreset is a 1:1 translation of presets.c:319-404. It translates the legacy
// preset ids, then dispatches V9..V0 to applyVbrPreset and 8..320 to
// applyAbrPreset.
func (gfc *LameInternalFlags) applyPreset(gfp *LameGlobalFlags, preset int, enforce bool) int {
	// translate legacy presets (presets.c:322-359).
	switch preset {
	case presetR3MIX:
		preset = presetV3
		gfp.VBR = vbrMtrh
	case presetMEDIUM, presetMEDIUMFAST:
		preset = presetV4
		gfp.VBR = vbrMtrh
	case presetSTANDARD, presetSTANDARDFAST:
		preset = presetV2
		gfp.VBR = vbrMtrh
	case presetEXTREME, presetEXTREMEFAST:
		preset = presetV0
		gfp.VBR = vbrMtrh
	case presetINSANE:
		preset = 320
		gfp.Preset = preset
		gfc.applyAbrPreset(gfp, preset, enforce)
		gfp.VBR = vbrOff
		return preset
	}

	gfp.Preset = preset
	switch preset {
	case presetV9:
		gfc.applyVbrPreset(gfp, 9, enforce)
		return preset
	case presetV8:
		gfc.applyVbrPreset(gfp, 8, enforce)
		return preset
	case presetV7:
		gfc.applyVbrPreset(gfp, 7, enforce)
		return preset
	case presetV6:
		gfc.applyVbrPreset(gfp, 6, enforce)
		return preset
	case presetV5:
		gfc.applyVbrPreset(gfp, 5, enforce)
		return preset
	case presetV4:
		gfc.applyVbrPreset(gfp, 4, enforce)
		return preset
	case presetV3:
		gfc.applyVbrPreset(gfp, 3, enforce)
		return preset
	case presetV2:
		gfc.applyVbrPreset(gfp, 2, enforce)
		return preset
	case presetV1:
		gfc.applyVbrPreset(gfp, 1, enforce)
		return preset
	case presetV0:
		gfc.applyVbrPreset(gfp, 0, enforce)
		return preset
	}

	if 8 <= preset && preset <= 320 {
		return gfc.applyAbrPreset(gfp, preset, enforce)
	}

	gfp.Preset = 0 // no corresponding preset found (presets.c:402).
	return preset
}
