// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package nativemp3

// iteration_init — the one-time quantizer setup, a 1:1 Go translation of LAME
// 3.100's iteration_init (libmp3lame/quantize_pvt.c:338). lame_init_params
// (init.go) reaches it through the EncoderStages seam; it runs once (latched by
// iteration_init_init) to: zero main_data_begin, build the per-band ATH energy
// floors (compute_ath), fill the pow43 / adj43 / ipow20 / pow20 precompute
// tables, install the Huffman bit-length tables + bv_scf split table
// (huffman_init), and fill the nspsytune longfact / shortfact per-band masking
// adjustments from the bass/alto/treble/sfb21 dB tuning. init_xrpow_core_init
// only selects an SSE fast path in the C (the vendored config builds the scalar
// baseline), so it is a no-op here.
//
// # Floating-point
//
// This is one-time setup, not the FMA-sensitive per-frame path. pow43 / adj43 /
// ipow20 / pow20 use double pow narrowed to float32 exactly as the C (and as
// InitQuantizePvtTables, quantize_pvt.go, which this reuses). The longfact /
// shortfact adjustments are `powf(10, db*0.1f)` — the shimmed double-narrowed
// powf (psPowf) of a float32 db, matching the cgo oracle's cosf/powf shim. So
// they are computed with the same psPowf the rest of the encoder uses; no
// *_fp_strict split is needed because the values are computed once and stored.

// payloadLong / payloadShort are quantize_pvt.c:325/329's static const
// payload_long[2][4] / payload_short[2][4]: the per-region dB tuning added to
// the nspsytune bass/alto/treble/sfb21 adjustments. Index [sel][region]; the
// vendored iteration_init always uses sel = 1 (the "all modes like vbr-new"
// branch, quantize_pvt.c:372).
var (
	payloadLong = [2][4]float32{
		{-0.000, -0.000, -0.000, +0.000},
		{-0.500, -0.250, -0.025, +0.500},
	}
	payloadShort = [2][4]float32{
		{-0.000, -0.000, -0.000, +0.000},
		{-2.000, -1.000, -0.050, +0.500},
	}
)

// iterationInit is LAME's iteration_init (quantize_pvt.c:338).
func (gfc *LameInternalFlags) iterationInit() {
	cfg := &gfc.Cfg
	l3Side := &gfc.L3Side

	if gfc.IterationInitInit != 0 {
		return
	}
	gfc.IterationInitInit = 1

	l3Side.MainDataBegin = 0
	computeATH(gfc)

	// pow43 / adj43 / ipow20 / pow20 — same double-pow table fill as the C's
	// non-TAKEHIRO_IEEE754_HACK branch (quantize_pvt.c:351-367); reuse the
	// already-ported fill so the tables are identical to calc_noise's.
	InitQuantizePvtTables()
	// adj43asm — the TAKEHIRO_IEEE754_HACK rounding table the VBR k_34_4
	// (vqHackQuantize) reads. The vendored config enables TAKEHIRO_IEEE754_HACK,
	// so iteration_init fills adj43asm in the SAME init block as pow43/ipow20/pow20
	// (quantize_pvt.c:355-358). Without this the VBR scalefactor noise search
	// quantizes against a zero adj43asm and the -V global_gain / scalefactors
	// diverge from LAME. (The parity hooks installed it for the isolated vbrquantize
	// slices, masking the omission in the real encode path.)
	InitVbrQuantizeTables()

	gfc.huffmanInit()
	// init_xrpow_core_init selects an SSE kernel in the C; the vendored config
	// builds the scalar baseline, so the pure-Go quantizeLinesXrpow path stands.

	const sel = 1 // RH: all modes like vbr-new

	// long
	db := psAdd(cfg.AdjustBassDb, payloadLong[sel][0])
	adjust := psPowf(10.0, psMul(db, 0.1))
	i := 0
	for ; i <= 6; i++ {
		gfc.SvQnt.Longfact[i] = adjust
	}
	db = psAdd(cfg.AdjustAltoDb, payloadLong[sel][1])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i <= 13; i++ {
		gfc.SvQnt.Longfact[i] = adjust
	}
	db = psAdd(cfg.AdjustTrebleDb, payloadLong[sel][2])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i <= 20; i++ {
		gfc.SvQnt.Longfact[i] = adjust
	}
	db = psAdd(cfg.AdjustSfb21Db, payloadLong[sel][3])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i < SBMAXl; i++ {
		gfc.SvQnt.Longfact[i] = adjust
	}

	// short
	db = psAdd(cfg.AdjustBassDb, payloadShort[sel][0])
	adjust = psPowf(10.0, psMul(db, 0.1))
	i = 0
	for ; i <= 2; i++ {
		gfc.SvQnt.Shortfact[i] = adjust
	}
	db = psAdd(cfg.AdjustAltoDb, payloadShort[sel][1])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i <= 6; i++ {
		gfc.SvQnt.Shortfact[i] = adjust
	}
	db = psAdd(cfg.AdjustTrebleDb, payloadShort[sel][2])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i <= 11; i++ {
		gfc.SvQnt.Shortfact[i] = adjust
	}
	db = psAdd(cfg.AdjustSfb21Db, payloadShort[sel][3])
	adjust = psPowf(10.0, psMul(db, 0.1))
	for ; i < SBMAXs; i++ {
		gfc.SvQnt.Shortfact[i] = adjust
	}
}

// psyInitParamsFromGfp bridges the init driver's LameGlobalFlags to the
// PsyInitParams the native psymodel initialiser (InitPsyModel) takes, mapping
// the gfp fields psymodel_init reads (experimentalZ, attackthre[_s], VBR_q[_frac]).
func psyInitParamsFromGfp(gfp *LameGlobalFlags) *PsyInitParams {
	return &PsyInitParams{
		ExperimentalZ: gfp.ExperimentalZ,
		Attackthre:    gfp.Attackthre,
		AttackthreS:   gfp.AttackthreS,
		VBRq:          gfp.VBRq,
		VBRqFrac:      gfp.VBRqFrac,
	}
}
