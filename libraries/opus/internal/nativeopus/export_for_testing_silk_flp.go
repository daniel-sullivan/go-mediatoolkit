package nativeopus

// Thin exports for SILK float (_FLP) leaf parity tests — Phase 8 Wave 1.

// ----- scale / copy / sort / bwexpander -----

func ExportTestSilkScaleCopyVectorFLP(data_in []float32, gain float32) []float32 {
	out := make([]float32, len(data_in))
	silk_scale_copy_vector_FLP(out, data_in, gain, opus_int(len(data_in)))
	return out
}

func ExportTestSilkScaleVectorFLP(data []float32, gain float32) []float32 {
	out := append([]float32(nil), data...)
	silk_scale_vector_FLP(out, gain, opus_int(len(out)))
	return out
}

func ExportTestSilkInsertionSortDecreasingFLP(a []float32, K int) ([]float32, []int) {
	out := append([]float32(nil), a...)
	idx := make([]opus_int, K)
	silk_insertion_sort_decreasing_FLP(out, idx, opus_int(len(out)), opus_int(K))
	ri := make([]int, K)
	for i := 0; i < K; i++ {
		ri[i] = int(idx[i])
	}
	return out, ri
}

func ExportTestSilkBwexpanderFLP(ar []float32, chirp float32) []float32 {
	out := append([]float32(nil), ar...)
	silk_bwexpander_FLP(out, opus_int(len(out)), chirp)
	return out
}

// ----- inner_product / energy / apply_sine_window -----

func ExportTestSilkInnerProductFLP(a, b []float32) float64 {
	if len(a) != len(b) {
		panic("length mismatch")
	}
	return silk_inner_product_FLP(a, b, opus_int(len(a)), 0)
}
func ExportTestSilkEnergyFLP(a []float32) float64 {
	return silk_energy_FLP(a, opus_int(len(a)))
}
func ExportTestSilkApplySineWindowFLP(px []float32, win_type int) []float32 {
	out := make([]float32, len(px))
	silk_apply_sine_window_FLP(out, px, opus_int(win_type), opus_int(len(px)))
	return out
}

// ----- k2a / schur / LTP_scale_ctrl -----

func ExportTestSilkK2aFLP(rc []float32) []float32 {
	A := make([]float32, len(rc))
	silk_k2a_FLP(A, rc, opus_int32(len(rc)))
	return A
}

func ExportTestSilkSchurFLP(auto_corr []float32) (refl []float32, residual float32) {
	order := len(auto_corr) - 1
	refl = make([]float32, order)
	residual = silk_schur_FLP(refl, auto_corr, opus_int(order))
	return
}

// ExportTestSilkLTPScaleCtrlFLP exercises silk_LTP_scale_ctrl_FLP via
// a minimal silk_encoder_state_FLP / silk_encoder_control_FLP pair.
// Returns (LTP_scaleIndex, LTP_scale).
func ExportTestSilkLTPScaleCtrlFLP(
	condCoding int,
	packetLossPerc, nFramesPerPacket, SNR_dB_Q7 int,
	LBRR_flag int,
	LTPredCodGain float32,
) (int8, float32) {
	var psEnc silk_encoder_state_FLP
	var psEncCtrl silk_encoder_control_FLP
	psEnc.sCmn.PacketLoss_perc = opus_int(packetLossPerc)
	psEnc.sCmn.nFramesPerPacket = opus_int(nFramesPerPacket)
	psEnc.sCmn.SNR_dB_Q7 = opus_int(SNR_dB_Q7)
	psEnc.sCmn.LBRR_flag = opus_int8(LBRR_flag)
	psEncCtrl.LTPredCodGain = LTPredCodGain
	silk_LTP_scale_ctrl_FLP(&psEnc, &psEncCtrl, opus_int(condCoding))
	return int8(psEnc.sCmn.indices.LTP_scaleIndex), psEncCtrl.LTP_scale
}

func ExportTestCodeIndependently() int { return int(CODE_INDEPENDENTLY) }

// ----- autocorrelation / warped_autocorrelation -----

func ExportTestSilkAutocorrelationFLP(input []float32, correlationCount int) []float32 {
	results := make([]float32, correlationCount)
	silk_autocorrelation_FLP(results, input, opus_int(len(input)), opus_int(correlationCount), 0)
	return results
}

func ExportTestSilkWarpedAutocorrelationFLP(input []float32, warping float32, order int) []float32 {
	corr := make([]float32, order+1)
	silk_warped_autocorrelation_FLP_c(corr, input, warping, opus_int(len(input)), opus_int(order))
	return corr
}

// ----- LPC_inv_pred_gain / LPC_analysis_filter / LTP_analysis_filter -----

func ExportTestSilkLPCInvPredGainFLP(A []float32) float32 {
	return silk_LPC_inverse_pred_gain_FLP(A, opus_int32(len(A)))
}

func ExportTestSilkLPCAnalysisFilterFLP(PredCoef, s []float32, Order int) []float32 {
	r := make([]float32, len(s))
	silk_LPC_analysis_filter_FLP(r, PredCoef, s, opus_int(len(s)), opus_int(Order))
	return r
}

// ExportTestSilkLTPAnalysisFilterFLP runs the filter with x starting at
// offset `xOff` in the caller-provided buffer (so negative pointer
// arithmetic x_ptr - pitchL[k] stays within the backing slice).
func ExportTestSilkLTPAnalysisFilterFLP(
	x []float32, xOff int, B []float32, pitchL []int, invGains []float32,
	subfr_length, nb_subfr, pre_length int,
) []float32 {
	out := make([]float32, nb_subfr*(subfr_length+pre_length))
	p := make([]opus_int, len(pitchL))
	for i, v := range pitchL {
		p[i] = opus_int(v)
	}
	silk_LTP_analysis_filter_FLP(out, x, opus_int(xOff), B, p, invGains,
		opus_int(subfr_length), opus_int(nb_subfr), opus_int(pre_length))
	return out
}

// ----- corrMatrix / regularize / residual_energy -----

func ExportTestSilkCorrVectorFLP(x, t []float32, L, Order int) []float32 {
	Xt := make([]float32, Order)
	silk_corrVector_FLP(x, t, opus_int(L), opus_int(Order), Xt, 0)
	return Xt
}

func ExportTestSilkCorrMatrixFLP(x []float32, L, Order int) []float32 {
	XX := make([]float32, Order*Order)
	silk_corrMatrix_FLP(x, opus_int(L), opus_int(Order), XX, 0)
	return XX
}

func ExportTestSilkRegularizeCorrelationsFLP(XX, xx []float32, noise float32, D int) ([]float32, []float32) {
	XXout := append([]float32(nil), XX...)
	xxout := append([]float32(nil), xx...)
	silk_regularize_correlations_FLP(XXout, xxout, noise, opus_int(D))
	return XXout, xxout
}

func ExportTestSilkResidualEnergyCovarFLP(
	c []float32, wXX []float32, wXx []float32, wxx float32, D int,
) (float32, []float32) {
	// wXX is I/O; copy.
	wXXout := append([]float32(nil), wXX...)
	nrg := silk_residual_energy_covar_FLP(c, wXXout, wXx, wxx, opus_int(D))
	return nrg, wXXout
}

func ExportTestSilkResidualEnergyFLP(
	x []float32, a0, a1 []float32, gains []float32,
	subfr_length, nb_subfr, LPC_order int,
) []float32 {
	var a [2][MAX_LPC_ORDER]silk_float
	for i, v := range a0 {
		if i < MAX_LPC_ORDER {
			a[0][i] = v
		}
	}
	for i, v := range a1 {
		if i < MAX_LPC_ORDER {
			a[1][i] = v
		}
	}
	nrgs := make([]float32, MAX_NB_SUBFR)
	silk_residual_energy_FLP(nrgs, x, a, gains,
		opus_int(subfr_length), opus_int(nb_subfr), opus_int(LPC_order))
	return nrgs[:nb_subfr]
}

// ----- burg_modified -----

func ExportTestSilkBurgModifiedFLP(
	x []float32, minInvGain float32,
	subfr_length, nb_subfr, D int,
) (residualEnergy float32, A []float32) {
	A = make([]float32, D)
	residualEnergy = silk_burg_modified_FLP(A, x, minInvGain,
		opus_int(subfr_length), opus_int(nb_subfr), opus_int(D), 0)
	return
}
