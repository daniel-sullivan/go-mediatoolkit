package nativeopus

// Port of libopus/silk/stereo_{encode,decode,quant}_pred.c + stereo_MS_to_LR.c.
//
// silk_stereo_LR_to_MS is encoder-only (uses the full stereo_enc_state
// from structs.h) and is not in the Phase 6 shared scope. When the
// encoder integration lands, the port will live here alongside.

// stereo_dec_state — decoder-side stereo predictor state.
type stereo_dec_state struct {
	pred_prev_Q13 [2]opus_int16
	sMid          [2]opus_int16
	sSide         [2]opus_int16
}

// silk_stereo_decode_pred — Decode mid/side predictors.
func silk_stereo_decode_pred(psRangeDec *ec_dec, pred_Q13 []opus_int32) {
	var ix [2][3]opus_int
	n := ec_dec_icdf(psRangeDec, asByteSlice(silk_stereo_pred_joint_iCDF[:]), 8)
	ix[0][2] = opus_int(silk_DIV32_16(opus_int32(n), 5))
	ix[1][2] = n - 5*ix[0][2]
	for i := 0; i < 2; i++ {
		ix[i][0] = ec_dec_icdf(psRangeDec, asByteSlice(silk_uniform3_iCDF[:]), 8)
		ix[i][1] = ec_dec_icdf(psRangeDec, asByteSlice(silk_uniform5_iCDF[:]), 8)
	}

	for n := 0; n < 2; n++ {
		ix[n][0] += 3 * ix[n][2]
		low_Q13 := silk_stereo_pred_quant_Q13[ix[n][0]]
		step_Q13 := silk_SMULWB(
			opus_int32(silk_stereo_pred_quant_Q13[ix[n][0]+1])-opus_int32(low_Q13),
			SILK_FIX_CONST(0.5/STEREO_QUANT_SUB_STEPS, 16))
		pred_Q13[n] = silk_SMLABB(opus_int32(low_Q13), step_Q13, opus_int32(2*ix[n][1]+1))
	}
	pred_Q13[0] -= pred_Q13[1]
}

// silk_stereo_decode_mid_only — decode mid-only flag.
func silk_stereo_decode_mid_only(psRangeDec *ec_dec, decode_only_mid *opus_int) {
	*decode_only_mid = ec_dec_icdf(psRangeDec, asByteSlice(silk_stereo_only_code_mid_iCDF[:]), 8)
}

// silk_stereo_encode_pred — Entropy-code mid/side quantization indices.
func silk_stereo_encode_pred(psRangeEnc *ec_enc, ix [2][3]opus_int8) {
	n := 5*opus_int(ix[0][2]) + opus_int(ix[1][2])
	celt_assert(n < 25)
	ec_enc_icdf(psRangeEnc, n, asByteSlice(silk_stereo_pred_joint_iCDF[:]), 8)
	for i := 0; i < 2; i++ {
		celt_assert(ix[i][0] < 3)
		celt_assert(opus_int(ix[i][1]) < STEREO_QUANT_SUB_STEPS)
		ec_enc_icdf(psRangeEnc, opus_int(ix[i][0]), asByteSlice(silk_uniform3_iCDF[:]), 8)
		ec_enc_icdf(psRangeEnc, opus_int(ix[i][1]), asByteSlice(silk_uniform5_iCDF[:]), 8)
	}
}

// silk_stereo_encode_mid_only — code the mid-only flag.
func silk_stereo_encode_mid_only(psRangeEnc *ec_enc, mid_only_flag opus_int8) {
	ec_enc_icdf(psRangeEnc, opus_int(mid_only_flag), asByteSlice(silk_stereo_only_code_mid_iCDF[:]), 8)
}

// silk_stereo_quant_pred — quantize mid/side predictors via brute force.
func silk_stereo_quant_pred(pred_Q13 []opus_int32, ix *[2][3]opus_int8) {
	var low_Q13, step_Q13, lvl_Q13, err_min_Q13, err_Q13, quant_pred_Q13 opus_int32
	for n := 0; n < 2; n++ {
		err_min_Q13 = silk_int32_MAX
		done := false
		for i := opus_int32(0); i < STEREO_QUANT_TAB_SIZE-1 && !done; i++ {
			low_Q13 = opus_int32(silk_stereo_pred_quant_Q13[i])
			step_Q13 = silk_SMULWB(
				opus_int32(silk_stereo_pred_quant_Q13[i+1])-low_Q13,
				SILK_FIX_CONST(0.5/STEREO_QUANT_SUB_STEPS, 16))
			for j := opus_int32(0); j < STEREO_QUANT_SUB_STEPS; j++ {
				lvl_Q13 = silk_SMLABB(low_Q13, step_Q13, 2*j+1)
				err_Q13 = silk_abs(pred_Q13[n] - lvl_Q13)
				if err_Q13 < err_min_Q13 {
					err_min_Q13 = err_Q13
					quant_pred_Q13 = lvl_Q13
					ix[n][0] = opus_int8(i)
					ix[n][1] = opus_int8(j)
				} else {
					done = true
					break
				}
			}
		}
		ix[n][2] = opus_int8(silk_DIV32_16(opus_int32(ix[n][0]), 3))
		ix[n][0] -= ix[n][2] * 3
		pred_Q13[n] = quant_pred_Q13
	}
	pred_Q13[0] -= pred_Q13[1]
}

// silk_stereo_find_predictor — Least-squares prediction gain for y
// given x, quantized to Q13. Updates mid_res_amp_Q0[0..1] (smoothed
// mid and residual amplitudes).
func silk_stereo_find_predictor(ratio_Q14 *opus_int32, x, y []opus_int16,
	mid_res_amp_Q0 []opus_int32, length, smooth_coef_Q16 opus_int) opus_int32 {
	var nrgx, nrgy, corr, pred_Q13, pred2_Q10 opus_int32
	var scale, scale1, scale2 opus_int

	silk_sum_sqr_shift(&nrgx, &scale1, x, length)
	silk_sum_sqr_shift(&nrgy, &scale2, y, length)
	scale = silk_max_int(scale1, scale2)
	scale = scale + (scale & 1)
	nrgy = silk_RSHIFT32(nrgy, scale-scale2)
	nrgx = silk_RSHIFT32(nrgx, scale-scale1)
	if nrgx < 1 {
		nrgx = 1
	}
	corr = silk_inner_prod_aligned_scale(x, y, scale, length)
	pred_Q13 = silk_DIV32_varQ(corr, nrgx, 13)
	pred_Q13 = silk_LIMIT(pred_Q13, -(1 << 14), 1<<14)
	pred2_Q10 = silk_SMULWB(pred_Q13, pred_Q13)

	// Faster update for signals with large prediction parameters.
	sm := opus_int32(smooth_coef_Q16)
	if silk_abs(pred2_Q10) > sm {
		sm = silk_abs(pred2_Q10)
	}
	smooth_coef_Q16 = opus_int(sm)

	silk_assert(opus_int32(smooth_coef_Q16) < 32768)
	scale = opus_int(silk_RSHIFT(opus_int32(scale), 1))
	mid_res_amp_Q0[0] = silk_SMLAWB(mid_res_amp_Q0[0],
		silk_LSHIFT(silk_SQRT_APPROX(nrgx), scale)-mid_res_amp_Q0[0], opus_int32(smooth_coef_Q16))
	nrgy = silk_SUB_LSHIFT32(nrgy, silk_SMULWB(corr, pred_Q13), 3+1)
	nrgy = silk_ADD_LSHIFT32(nrgy, silk_SMULWB(nrgx, pred2_Q10), 6)
	mid_res_amp_Q0[1] = silk_SMLAWB(mid_res_amp_Q0[1],
		silk_LSHIFT(silk_SQRT_APPROX(nrgy), scale)-mid_res_amp_Q0[1], opus_int32(smooth_coef_Q16))

	denom := silk_max(mid_res_amp_Q0[0], 1)
	*ratio_Q14 = silk_DIV32_varQ(mid_res_amp_Q0[1], denom, 14)
	*ratio_Q14 = silk_LIMIT(*ratio_Q14, 0, 32767)

	return pred_Q13
}

// silk_stereo_MS_to_LR — Convert adaptive Mid/Side representation to Left/Right.
func silk_stereo_MS_to_LR(state *stereo_dec_state, x1, x2 []opus_int16,
	pred_Q13 []opus_int32, fs_kHz, frame_length opus_int) {

	// Buffering.
	copy(x1[:2], state.sMid[:])
	copy(x2[:2], state.sSide[:])
	copy(state.sMid[:], x1[frame_length:frame_length+2])
	copy(state.sSide[:], x2[frame_length:frame_length+2])

	pred0_Q13 := opus_int32(state.pred_prev_Q13[0])
	pred1_Q13 := opus_int32(state.pred_prev_Q13[1])
	denom_Q16 := silk_DIV32_16(opus_int32(1)<<16, opus_int32(STEREO_INTERP_LEN_MS*fs_kHz))
	delta0_Q13 := silk_RSHIFT_ROUND(silk_SMULBB(pred_Q13[0]-opus_int32(state.pred_prev_Q13[0]), denom_Q16), 16)
	delta1_Q13 := silk_RSHIFT_ROUND(silk_SMULBB(pred_Q13[1]-opus_int32(state.pred_prev_Q13[1]), denom_Q16), 16)

	for n := opus_int(0); n < STEREO_INTERP_LEN_MS*fs_kHz; n++ {
		pred0_Q13 += delta0_Q13
		pred1_Q13 += delta1_Q13
		sum := silk_LSHIFT(silk_ADD_LSHIFT32(opus_int32(x1[n])+opus_int32(x1[n+2]), opus_int32(x1[n+1]), 1), 9)
		sum = silk_SMLAWB(silk_LSHIFT(opus_int32(x2[n+1]), 8), sum, pred0_Q13)
		sum = silk_SMLAWB(sum, silk_LSHIFT(opus_int32(x1[n+1]), 11), pred1_Q13)
		x2[n+1] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(sum, 8)))
	}
	pred0_Q13 = pred_Q13[0]
	pred1_Q13 = pred_Q13[1]
	for n := STEREO_INTERP_LEN_MS * fs_kHz; n < frame_length; n++ {
		sum := silk_LSHIFT(silk_ADD_LSHIFT32(opus_int32(x1[n])+opus_int32(x1[n+2]), opus_int32(x1[n+1]), 1), 9)
		sum = silk_SMLAWB(silk_LSHIFT(opus_int32(x2[n+1]), 8), sum, pred0_Q13)
		sum = silk_SMLAWB(sum, silk_LSHIFT(opus_int32(x1[n+1]), 11), pred1_Q13)
		x2[n+1] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(sum, 8)))
	}
	state.pred_prev_Q13[0] = opus_int16(pred_Q13[0])
	state.pred_prev_Q13[1] = opus_int16(pred_Q13[1])

	// Convert to L/R.
	for n := opus_int(0); n < frame_length; n++ {
		sum := opus_int32(x1[n+1]) + opus_int32(x2[n+1])
		diff := opus_int32(x1[n+1]) - opus_int32(x2[n+1])
		x1[n+1] = opus_int16(silk_SAT16(sum))
		x2[n+1] = opus_int16(silk_SAT16(diff))
	}
}

// silk_stereo_LR_to_MS — Convert L/R stereo to adaptive M/S.
// C: stereo_LR_to_MS.c:36-229.
//
// The C code takes raw pointers x1, x2 "pointing at the first frame
// sample" with two valid history samples at [-2], [-1]. In Go we pass
// the whole caller buffer slices x1Buf, x2Buf where index 0 is two
// samples before the frame start (i.e. x1Buf[2] is the first frame
// sample). All internal indexing then uses the same offsets as the
// C after rewriting &x1[-2] to x1Buf[0].
func silk_stereo_LR_to_MS(
	state *stereo_enc_state,
	x1Buf, x2Buf []opus_int16,
	ix *[2][3]opus_int8,
	mid_only_flag *opus_int8,
	mid_side_rates_bps []opus_int32,
	total_rate_bps opus_int32,
	prev_speech_act_Q8 opus_int,
	toMono opus_int,
	fs_kHz, frame_length opus_int,
) {
	side := make([]opus_int16, frame_length+2)
	// mid aliases x1Buf (mirrors C: opus_int16 *mid = &x1[-2]).
	mid := x1Buf

	// Basic M/S sums. C loop runs n=0..frame_length+1 reading x1[n-2], x2[n-2]
	// which in our buffer is x1Buf[n], x2Buf[n].
	for n := opus_int(0); n < frame_length+2; n++ {
		sum := opus_int32(x1Buf[n]) + opus_int32(x2Buf[n])
		diff := opus_int32(x1Buf[n]) - opus_int32(x2Buf[n])
		mid[n] = opus_int16(silk_RSHIFT_ROUND(sum, 1))
		side[n] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(diff, 1)))
	}

	// Buffering: restore history from state, then save new history.
	copy(mid[:2], state.sMid[:])
	copy(side[:2], state.sSide[:])
	copy(state.sMid[:], mid[frame_length:frame_length+2])
	copy(state.sSide[:], side[frame_length:frame_length+2])

	// LP/HP filter the mid signal.
	LP_mid := make([]opus_int16, frame_length)
	HP_mid := make([]opus_int16, frame_length)
	for n := opus_int(0); n < frame_length; n++ {
		sum := silk_RSHIFT_ROUND(
			silk_ADD_LSHIFT32(opus_int32(mid[n])+opus_int32(mid[n+2]), opus_int32(mid[n+1]), 1),
			2)
		LP_mid[n] = opus_int16(sum)
		HP_mid[n] = opus_int16(opus_int32(mid[n+1]) - sum)
	}

	// LP/HP filter the side signal.
	LP_side := make([]opus_int16, frame_length)
	HP_side := make([]opus_int16, frame_length)
	for n := opus_int(0); n < frame_length; n++ {
		sum := silk_RSHIFT_ROUND(
			silk_ADD_LSHIFT32(opus_int32(side[n])+opus_int32(side[n+2]), opus_int32(side[n+1]), 1),
			2)
		LP_side[n] = opus_int16(sum)
		HP_side[n] = opus_int16(opus_int32(side[n+1]) - sum)
	}

	is10msFrame := frame_length == 10*fs_kHz
	var smooth_coef_Q16 opus_int32
	if is10msFrame {
		smooth_coef_Q16 = SILK_FIX_CONST(STEREO_RATIO_SMOOTH_COEF/2, 16)
	} else {
		smooth_coef_Q16 = SILK_FIX_CONST(STEREO_RATIO_SMOOTH_COEF, 16)
	}
	smooth_coef_Q16 = silk_SMULWB(
		silk_SMULBB(opus_int32(prev_speech_act_Q8), opus_int32(prev_speech_act_Q8)),
		smooth_coef_Q16)

	var pred_Q13 [2]opus_int32
	var LP_ratio_Q14, HP_ratio_Q14 opus_int32
	pred_Q13[0] = silk_stereo_find_predictor(&LP_ratio_Q14, LP_mid, LP_side, state.mid_side_amp_Q0[0:], frame_length, opus_int(smooth_coef_Q16))
	pred_Q13[1] = silk_stereo_find_predictor(&HP_ratio_Q14, HP_mid, HP_side, state.mid_side_amp_Q0[2:], frame_length, opus_int(smooth_coef_Q16))
	// Ratio of residual to mid signal norm.
	frac_Q16 := silk_SMLABB(HP_ratio_Q14, LP_ratio_Q14, 3)
	frac_Q16 = silk_min(frac_Q16, SILK_FIX_CONST(1, 16))

	// Bitrate split.
	if is10msFrame {
		total_rate_bps -= 1200
	} else {
		total_rate_bps -= 600
	}
	if total_rate_bps < 1 {
		total_rate_bps = 1
	}
	min_mid_rate_bps := silk_SMLABB(2000, opus_int32(fs_kHz), 600)
	silk_assert(min_mid_rate_bps < 32767)

	frac_3_Q16 := silk_MUL(3, frac_Q16)
	mid_side_rates_bps[0] = silk_DIV32_varQ(total_rate_bps,
		SILK_FIX_CONST(8+5, 16)+frac_3_Q16, 16+3)
	var width_Q14 opus_int32
	if mid_side_rates_bps[0] < min_mid_rate_bps {
		mid_side_rates_bps[0] = min_mid_rate_bps
		mid_side_rates_bps[1] = total_rate_bps - mid_side_rates_bps[0]
		width_Q14 = silk_DIV32_varQ(
			silk_LSHIFT(mid_side_rates_bps[1], 1)-min_mid_rate_bps,
			silk_SMULWB(SILK_FIX_CONST(1, 16)+frac_3_Q16, min_mid_rate_bps),
			14+2)
		width_Q14 = silk_LIMIT(width_Q14, 0, SILK_FIX_CONST(1, 14))
	} else {
		mid_side_rates_bps[1] = total_rate_bps - mid_side_rates_bps[0]
		width_Q14 = SILK_FIX_CONST(1, 14)
	}

	// Smoother.
	state.smth_width_Q14 = opus_int16(silk_SMLAWB(opus_int32(state.smth_width_Q14),
		width_Q14-opus_int32(state.smth_width_Q14), smooth_coef_Q16))

	*mid_only_flag = 0
	if toMono != 0 {
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
		silk_stereo_quant_pred(pred_Q13[:], ix)
	} else if state.width_prev_Q14 == 0 &&
		(8*total_rate_bps < 13*min_mid_rate_bps ||
			silk_SMULWB(frac_Q16, opus_int32(state.smth_width_Q14)) < SILK_FIX_CONST(0.05, 14)) {
		// Code as panned-mono; previous frame already had zero width.
		pred_Q13[0] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[0]), 14)
		pred_Q13[1] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[1]), 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
		mid_side_rates_bps[0] = total_rate_bps
		mid_side_rates_bps[1] = 0
		*mid_only_flag = 1
	} else if state.width_prev_Q14 != 0 &&
		(8*total_rate_bps < 11*min_mid_rate_bps ||
			silk_SMULWB(frac_Q16, opus_int32(state.smth_width_Q14)) < SILK_FIX_CONST(0.02, 14)) {
		// Transition to zero-width stereo.
		pred_Q13[0] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[0]), 14)
		pred_Q13[1] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[1]), 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = 0
		pred_Q13[0] = 0
		pred_Q13[1] = 0
	} else if state.smth_width_Q14 > opus_int16(SILK_FIX_CONST(0.95, 14)) {
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = SILK_FIX_CONST(1, 14)
	} else {
		pred_Q13[0] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[0]), 14)
		pred_Q13[1] = silk_RSHIFT(silk_SMULBB(opus_int32(state.smth_width_Q14), pred_Q13[1]), 14)
		silk_stereo_quant_pred(pred_Q13[:], ix)
		width_Q14 = opus_int32(state.smth_width_Q14)
	}

	if *mid_only_flag == 1 {
		state.silent_side_len += opus_int16(frame_length - STEREO_INTERP_LEN_MS*fs_kHz)
		if state.silent_side_len < opus_int16(LA_SHAPE_MS*fs_kHz) {
			*mid_only_flag = 0
		} else {
			state.silent_side_len = 10000
		}
	} else {
		state.silent_side_len = 0
	}

	if *mid_only_flag == 0 && mid_side_rates_bps[1] < 1 {
		mid_side_rates_bps[1] = 1
		mid_side_rates_bps[0] = silk_max(1, total_rate_bps-mid_side_rates_bps[1])
	}

	// Interpolate predictors, subtract prediction from side channel.
	pred0_Q13 := -opus_int32(state.pred_prev_Q13[0])
	pred1_Q13 := -opus_int32(state.pred_prev_Q13[1])
	w_Q24 := silk_LSHIFT(opus_int32(state.width_prev_Q14), 10)
	denom_Q16 := silk_DIV32_16(opus_int32(1)<<16, opus_int32(STEREO_INTERP_LEN_MS*fs_kHz))
	delta0_Q13 := -silk_RSHIFT_ROUND(silk_SMULBB(pred_Q13[0]-opus_int32(state.pred_prev_Q13[0]), denom_Q16), 16)
	delta1_Q13 := -silk_RSHIFT_ROUND(silk_SMULBB(pred_Q13[1]-opus_int32(state.pred_prev_Q13[1]), denom_Q16), 16)
	deltaw_Q24 := silk_LSHIFT(silk_SMULWB(width_Q14-opus_int32(state.width_prev_Q14), denom_Q16), 10)

	// In C x2[n-1] writes into the caller's buffer at (frame_start-1+n).
	// Our x2Buf starts 2 samples before frame_start so x2[n-1] == x2Buf[n+1].
	for n := opus_int(0); n < STEREO_INTERP_LEN_MS*fs_kHz; n++ {
		pred0_Q13 += delta0_Q13
		pred1_Q13 += delta1_Q13
		w_Q24 += deltaw_Q24
		sum := silk_LSHIFT(silk_ADD_LSHIFT32(opus_int32(mid[n])+opus_int32(mid[n+2]), opus_int32(mid[n+1]), 1), 9)
		sum = silk_SMLAWB(silk_SMULWB(w_Q24, opus_int32(side[n+1])), sum, pred0_Q13)
		sum = silk_SMLAWB(sum, silk_LSHIFT(opus_int32(mid[n+1]), 11), pred1_Q13)
		x2Buf[n+1] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(sum, 8)))
	}

	pred0_Q13 = -pred_Q13[0]
	pred1_Q13 = -pred_Q13[1]
	w_Q24 = silk_LSHIFT(width_Q14, 10)
	for n := STEREO_INTERP_LEN_MS * fs_kHz; n < frame_length; n++ {
		sum := silk_LSHIFT(silk_ADD_LSHIFT32(opus_int32(mid[n])+opus_int32(mid[n+2]), opus_int32(mid[n+1]), 1), 9)
		sum = silk_SMLAWB(silk_SMULWB(w_Q24, opus_int32(side[n+1])), sum, pred0_Q13)
		sum = silk_SMLAWB(sum, silk_LSHIFT(opus_int32(mid[n+1]), 11), pred1_Q13)
		x2Buf[n+1] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(sum, 8)))
	}
	state.pred_prev_Q13[0] = opus_int16(pred_Q13[0])
	state.pred_prev_Q13[1] = opus_int16(pred_Q13[1])
	state.width_prev_Q14 = opus_int16(width_Q14)
}
