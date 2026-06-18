package nativeopus

// Port of libopus/silk/NSQ.c.
//
// Noise-shaping quantizer (non-delayed-decision path) with static
// helpers silk_nsq_scale_states and silk_noise_shape_quantizer.

// silk_noise_shape_quantizer_short_prediction_c — C: NSQ.h:35-63.
// buf32[base] corresponds to C's buf32[0]; lookups go back through
// buf32[base-1..base-15].
func silk_noise_shape_quantizer_short_prediction_c(buf32 []opus_int32, base opus_int, coef16 []opus_int16, order opus_int) opus_int32 {
	// Avoids introducing a bias because silk_SMLAWB() always rounds to -inf.
	out := silk_RSHIFT(opus_int32(order), 1)
	out = silk_SMLAWB(out, buf32[base+0], opus_int32(coef16[0]))
	out = silk_SMLAWB(out, buf32[base-1], opus_int32(coef16[1]))
	out = silk_SMLAWB(out, buf32[base-2], opus_int32(coef16[2]))
	out = silk_SMLAWB(out, buf32[base-3], opus_int32(coef16[3]))
	out = silk_SMLAWB(out, buf32[base-4], opus_int32(coef16[4]))
	out = silk_SMLAWB(out, buf32[base-5], opus_int32(coef16[5]))
	out = silk_SMLAWB(out, buf32[base-6], opus_int32(coef16[6]))
	out = silk_SMLAWB(out, buf32[base-7], opus_int32(coef16[7]))
	out = silk_SMLAWB(out, buf32[base-8], opus_int32(coef16[8]))
	out = silk_SMLAWB(out, buf32[base-9], opus_int32(coef16[9]))

	if order == 16 {
		out = silk_SMLAWB(out, buf32[base-10], opus_int32(coef16[10]))
		out = silk_SMLAWB(out, buf32[base-11], opus_int32(coef16[11]))
		out = silk_SMLAWB(out, buf32[base-12], opus_int32(coef16[12]))
		out = silk_SMLAWB(out, buf32[base-13], opus_int32(coef16[13]))
		out = silk_SMLAWB(out, buf32[base-14], opus_int32(coef16[14]))
		out = silk_SMLAWB(out, buf32[base-15], opus_int32(coef16[15]))
	}
	return out
}

// silk_NSQ_noise_shape_feedback_loop_c — C: NSQ.h:67-93.
func silk_NSQ_noise_shape_feedback_loop_c(data0 []opus_int32, data1 []opus_int32, coef []opus_int16, order opus_int) opus_int32 {
	tmp2 := data0[0]
	tmp1 := data1[0]
	data1[0] = tmp2

	out := silk_RSHIFT(opus_int32(order), 1)
	out = silk_SMLAWB(out, tmp2, opus_int32(coef[0]))

	for j := opus_int(2); j < order; j += 2 {
		tmp2 = data1[j-1]
		data1[j-1] = tmp1
		out = silk_SMLAWB(out, tmp1, opus_int32(coef[j-1]))
		tmp1 = data1[j+0]
		data1[j+0] = tmp2
		out = silk_SMLAWB(out, tmp2, opus_int32(coef[j]))
	}
	data1[order-1] = tmp1
	out = silk_SMLAWB(out, tmp1, opus_int32(coef[order-1]))
	// Q11 -> Q12.
	out = silk_LSHIFT32(out, 1)
	return out
}

// silk_NSQ_c — main entry. C: NSQ.c:76-174.
func silk_NSQ_c(
	psEncC *silk_encoder_state,
	NSQ *silk_nsq_state,
	psIndices *SideInfoIndices,
	x16 []opus_int16,
	pulses []opus_int8,
	PredCoef_Q12 []opus_int16,
	LTPCoef_Q14 []opus_int16,
	AR_Q13 []opus_int16,
	HarmShapeGain_Q14 []opus_int,
	Tilt_Q14 []opus_int,
	LF_shp_Q14 []opus_int32,
	Gains_Q16 []opus_int32,
	pitchL []opus_int,
	Lambda_Q10 opus_int,
	LTP_scale_Q14 opus_int,
) {
	NSQ.rand_seed = opus_int32(psIndices.Seed)

	// Set unvoiced lag to the previous one, overwrite later for voiced.
	lag := NSQ.lagPrev

	offset_Q10 := opus_int32(silk_Quantization_Offsets_Q10[psIndices.signalType>>1][psIndices.quantOffsetType])

	var LSF_interpolation_flag opus_int
	if psIndices.NLSFInterpCoef_Q2 == 4 {
		LSF_interpolation_flag = 0
	} else {
		LSF_interpolation_flag = 1
	}

	sLTP_Q15 := make([]opus_int32, psEncC.ltp_mem_length+psEncC.frame_length)
	sLTP := make([]opus_int16, psEncC.ltp_mem_length+psEncC.frame_length)
	x_sc_Q10 := make([]opus_int32, psEncC.subfr_length)

	// Set up pointers to start of sub frame.
	NSQ.sLTP_shp_buf_idx = psEncC.ltp_mem_length
	NSQ.sLTP_buf_idx = psEncC.ltp_mem_length
	pxqOff := psEncC.ltp_mem_length
	x16Off := opus_int(0)
	pulsesOff := opus_int(0)

	for k := opus_int(0); k < psEncC.nb_subfr; k++ {
		A_Q12 := PredCoef_Q12[((k>>1)|(1-LSF_interpolation_flag))*MAX_LPC_ORDER:]
		B_Q14 := LTPCoef_Q14[k*LTP_ORDER:]
		AR_shp_Q13 := AR_Q13[k*MAX_SHAPE_LPC_ORDER:]

		// Noise shape parameters.
		HarmShapeFIRPacked_Q14 := silk_RSHIFT(opus_int32(HarmShapeGain_Q14[k]), 2)
		HarmShapeFIRPacked_Q14 |= silk_LSHIFT(opus_int32(silk_RSHIFT(opus_int32(HarmShapeGain_Q14[k]), 1)), 16)

		NSQ.rewhite_flag = 0
		if opus_int(psIndices.signalType) == TYPE_VOICED {
			lag = pitchL[k]

			// Re-whitening.
			if (k & (3 - (LSF_interpolation_flag << 1))) == 0 {
				// Rewhiten with new A coefs.
				start_idx := psEncC.ltp_mem_length - lag - psEncC.predictLPCOrder - LTP_ORDER/2

				silk_LPC_analysis_filter(sLTP[start_idx:], NSQ.xq[start_idx+k*psEncC.subfr_length:],
					A_Q12, opus_int32(psEncC.ltp_mem_length-start_idx), opus_int32(psEncC.predictLPCOrder), psEncC.arch)

				NSQ.rewhite_flag = 1
				NSQ.sLTP_buf_idx = psEncC.ltp_mem_length
			}
		}

		silk_nsq_scale_states(psEncC, NSQ, x16[x16Off:], x_sc_Q10, sLTP, sLTP_Q15, k, LTP_scale_Q14, Gains_Q16, pitchL, opus_int(psIndices.signalType))

		silk_noise_shape_quantizer(NSQ, opus_int(psIndices.signalType), x_sc_Q10, pulses[pulsesOff:], NSQ.xq[pxqOff:], sLTP_Q15, A_Q12, B_Q14,
			AR_shp_Q13, lag, HarmShapeFIRPacked_Q14, Tilt_Q14[k], LF_shp_Q14[k], Gains_Q16[k], Lambda_Q10,
			offset_Q10, psEncC.subfr_length, psEncC.shapingLPCOrder, psEncC.predictLPCOrder, psEncC.arch)

		x16Off += psEncC.subfr_length
		pulsesOff += psEncC.subfr_length
		pxqOff += psEncC.subfr_length
	}

	// Update lagPrev for next frame.
	NSQ.lagPrev = pitchL[psEncC.nb_subfr-1]

	// Save quantized speech and noise shaping signals.
	copy(NSQ.xq[:psEncC.ltp_mem_length], NSQ.xq[psEncC.frame_length:psEncC.frame_length+psEncC.ltp_mem_length])
	copy(NSQ.sLTP_shp_Q14[:psEncC.ltp_mem_length], NSQ.sLTP_shp_Q14[psEncC.frame_length:psEncC.frame_length+psEncC.ltp_mem_length])
}

// silk_noise_shape_quantizer — C: NSQ.c:183-366.
func silk_noise_shape_quantizer(
	NSQ *silk_nsq_state,
	signalType opus_int,
	x_sc_Q10 []opus_int32,
	pulses []opus_int8,
	xq []opus_int16,
	sLTP_Q15 []opus_int32,
	a_Q12 []opus_int16,
	b_Q14 []opus_int16,
	AR_shp_Q13 []opus_int16,
	lag opus_int,
	HarmShapeFIRPacked_Q14 opus_int32,
	Tilt_Q14 opus_int,
	LF_shp_Q14 opus_int32,
	Gain_Q16 opus_int32,
	Lambda_Q10 opus_int,
	offset_Q10 opus_int32,
	length opus_int,
	shapingLPCOrder opus_int,
	predictLPCOrder opus_int,
	arch int,
) {
	// shp_base and pred_base correspond to C's shp_lag_ptr[0] / pred_lag_ptr[0]
	// indices into the underlying arrays; both are incremented per iteration.
	shp_base := NSQ.sLTP_shp_buf_idx - lag + HARM_SHAPE_FIR_TAPS/2
	pred_base := NSQ.sLTP_buf_idx - lag + LTP_ORDER/2
	Gain_Q10 := silk_RSHIFT(Gain_Q16, 6)

	// Set up short term AR state. In C: psLPC_Q14 = &NSQ->sLPC_Q14[NSQ_LPC_BUF_LENGTH - 1];
	// the loop body then does psLPC_Q14++ and *psLPC_Q14 = xq_Q14, so the
	// effective base advances each iteration.
	psLPC_base := opus_int(NSQ_LPC_BUF_LENGTH - 1)

	for i := opus_int(0); i < length; i++ {
		// Generate dither.
		NSQ.rand_seed = silk_RAND(NSQ.rand_seed)

		// Short-term prediction. The C uses a sliding buffer pointer
		// that advances once per iteration.
		LPC_pred_Q10 := silk_noise_shape_quantizer_short_prediction_c(NSQ.sLPC_Q14[:], psLPC_base, a_Q12, predictLPCOrder)

		var LTP_pred_Q13 opus_int32
		if signalType == TYPE_VOICED {
			// Unrolled loop. Avoids introducing a bias because silk_SMLAWB() always rounds to -inf.
			LTP_pred_Q13 = 2
			LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[pred_base+0], opus_int32(b_Q14[0]))
			LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[pred_base-1], opus_int32(b_Q14[1]))
			LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[pred_base-2], opus_int32(b_Q14[2]))
			LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[pred_base-3], opus_int32(b_Q14[3]))
			LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[pred_base-4], opus_int32(b_Q14[4]))
			pred_base++
		} else {
			LTP_pred_Q13 = 0
		}

		// Noise shape feedback. In C: n_AR_Q12 = silk_NSQ_noise_shape_feedback_loop
		// (&NSQ->sDiff_shp_Q14, NSQ->sAR2_Q14, AR_shp_Q13, shapingLPCOrder, arch).
		n_AR_Q12 := silk_NSQ_noise_shape_feedback_loop_inplace(&NSQ.sDiff_shp_Q14, NSQ.sAR2_Q14[:], AR_shp_Q13, shapingLPCOrder)

		n_AR_Q12 = silk_SMLAWB(n_AR_Q12, NSQ.sLF_AR_shp_Q14, opus_int32(Tilt_Q14))

		n_LF_Q12 := silk_SMULWB(NSQ.sLTP_shp_Q14[NSQ.sLTP_shp_buf_idx-1], LF_shp_Q14)
		n_LF_Q12 = silk_SMLAWT(n_LF_Q12, NSQ.sLF_AR_shp_Q14, LF_shp_Q14)

		// Combine prediction and noise shaping signals.
		tmp1 := silk_SUB32_ovflw(silk_LSHIFT32(LPC_pred_Q10, 2), n_AR_Q12) // Q12
		tmp1 = silk_SUB32_ovflw(tmp1, n_LF_Q12)                            // Q12

		var n_LTP_Q13 opus_int32
		if lag > 0 {
			// Symmetric, packed FIR coefficients.
			n_LTP_Q13 = silk_SMULWB(silk_ADD_SAT32(NSQ.sLTP_shp_Q14[shp_base+0], NSQ.sLTP_shp_Q14[shp_base-2]), HarmShapeFIRPacked_Q14)
			n_LTP_Q13 = silk_SMLAWT(n_LTP_Q13, NSQ.sLTP_shp_Q14[shp_base-1], HarmShapeFIRPacked_Q14)
			n_LTP_Q13 = silk_LSHIFT(n_LTP_Q13, 1)
			shp_base++

			tmp2 := silk_SUB32(LTP_pred_Q13, n_LTP_Q13)           // Q13
			tmp1 = silk_ADD32_ovflw(tmp2, silk_LSHIFT32(tmp1, 1)) // Q13
			tmp1 = silk_RSHIFT_ROUND(tmp1, 3)                     // Q10
		} else {
			tmp1 = silk_RSHIFT_ROUND(tmp1, 2) // Q10
		}

		r_Q10 := silk_SUB32(x_sc_Q10[i], tmp1) // residual error Q10

		// Flip sign depending on dither.
		if NSQ.rand_seed < 0 {
			r_Q10 = -r_Q10
		}
		r_Q10 = silk_LIMIT_32(r_Q10, -(31 << 10), 30<<10)

		// Find two quantization level candidates and measure their rate-distortion.
		q1_Q10 := silk_SUB32(r_Q10, offset_Q10)
		q1_Q0 := silk_RSHIFT(q1_Q10, 10)
		if Lambda_Q10 > 2048 {
			rdo_offset := opus_int32(Lambda_Q10)/2 - 512
			if q1_Q10 > rdo_offset {
				q1_Q0 = silk_RSHIFT(q1_Q10-rdo_offset, 10)
			} else if q1_Q10 < -rdo_offset {
				q1_Q0 = silk_RSHIFT(q1_Q10+rdo_offset, 10)
			} else if q1_Q10 < 0 {
				q1_Q0 = -1
			} else {
				q1_Q0 = 0
			}
		}

		var q2_Q10, rd1_Q20, rd2_Q20 opus_int32
		if q1_Q0 > 0 {
			q1_Q10 = silk_SUB32(silk_LSHIFT(q1_Q0, 10), QUANT_LEVEL_ADJUST_Q10)
			q1_Q10 = silk_ADD32(q1_Q10, offset_Q10)
			q2_Q10 = silk_ADD32(q1_Q10, 1024)
			rd1_Q20 = silk_SMULBB(q1_Q10, opus_int32(Lambda_Q10))
			rd2_Q20 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
		} else if q1_Q0 == 0 {
			q1_Q10 = offset_Q10
			q2_Q10 = silk_ADD32(q1_Q10, 1024-QUANT_LEVEL_ADJUST_Q10)
			rd1_Q20 = silk_SMULBB(q1_Q10, opus_int32(Lambda_Q10))
			rd2_Q20 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
		} else if q1_Q0 == -1 {
			q2_Q10 = offset_Q10
			q1_Q10 = silk_SUB32(q2_Q10, 1024-QUANT_LEVEL_ADJUST_Q10)
			rd1_Q20 = silk_SMULBB(-q1_Q10, opus_int32(Lambda_Q10))
			rd2_Q20 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
		} else { // q1_Q0 < -1
			q1_Q10 = silk_ADD32(silk_LSHIFT(q1_Q0, 10), QUANT_LEVEL_ADJUST_Q10)
			q1_Q10 = silk_ADD32(q1_Q10, offset_Q10)
			q2_Q10 = silk_ADD32(q1_Q10, 1024)
			rd1_Q20 = silk_SMULBB(-q1_Q10, opus_int32(Lambda_Q10))
			rd2_Q20 = silk_SMULBB(-q2_Q10, opus_int32(Lambda_Q10))
		}
		rr_Q10 := silk_SUB32(r_Q10, q1_Q10)
		rd1_Q20 = silk_SMLABB(rd1_Q20, rr_Q10, rr_Q10)
		rr_Q10 = silk_SUB32(r_Q10, q2_Q10)
		rd2_Q20 = silk_SMLABB(rd2_Q20, rr_Q10, rr_Q10)

		if rd2_Q20 < rd1_Q20 {
			q1_Q10 = q2_Q10
		}

		pulses[i] = opus_int8(silk_RSHIFT_ROUND(q1_Q10, 10))

		// Excitation.
		exc_Q14 := silk_LSHIFT(q1_Q10, 4)
		if NSQ.rand_seed < 0 {
			exc_Q14 = -exc_Q14
		}

		// Add predictions.
		LPC_exc_Q14 := silk_ADD_LSHIFT32(exc_Q14, LTP_pred_Q13, 1)
		xq_Q14 := silk_ADD32_ovflw(LPC_exc_Q14, silk_LSHIFT32(LPC_pred_Q10, 4))

		// Scale XQ back to normal level before saving.
		xq[i] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(silk_SMULWW(xq_Q14, Gain_Q10), 8)))

		// Update states.
		psLPC_base++
		NSQ.sLPC_Q14[psLPC_base] = xq_Q14
		NSQ.sDiff_shp_Q14 = silk_SUB32_ovflw(xq_Q14, silk_LSHIFT32(x_sc_Q10[i], 4))
		sLF_AR_shp_Q14 := silk_SUB32_ovflw(NSQ.sDiff_shp_Q14, silk_LSHIFT32(n_AR_Q12, 2))
		NSQ.sLF_AR_shp_Q14 = sLF_AR_shp_Q14

		NSQ.sLTP_shp_Q14[NSQ.sLTP_shp_buf_idx] = silk_SUB32_ovflw(sLF_AR_shp_Q14, silk_LSHIFT32(n_LF_Q12, 2))
		sLTP_Q15[NSQ.sLTP_buf_idx] = silk_LSHIFT(LPC_exc_Q14, 1)
		NSQ.sLTP_shp_buf_idx++
		NSQ.sLTP_buf_idx++

		// Make dither dependent on quantized signal.
		NSQ.rand_seed = silk_ADD32_ovflw(NSQ.rand_seed, opus_int32(pulses[i]))
	}

	// Update LPC synth buffer: shift sLPC_Q14[length..length+NSQ_LPC_BUF_LENGTH-1] into [0..NSQ_LPC_BUF_LENGTH-1].
	copy(NSQ.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], NSQ.sLPC_Q14[length:length+NSQ_LPC_BUF_LENGTH])
}

// silk_NSQ_noise_shape_feedback_loop_inplace is the scalar reference
// variant where data0 is a single scalar supplied by pointer (mirrors
// the C call pattern `&NSQ->sDiff_shp_Q14`).
func silk_NSQ_noise_shape_feedback_loop_inplace(data0 *opus_int32, data1 []opus_int32, coef []opus_int16, order opus_int) opus_int32 {
	tmp2 := *data0
	tmp1 := data1[0]
	data1[0] = tmp2

	out := silk_RSHIFT(opus_int32(order), 1)
	out = silk_SMLAWB(out, tmp2, opus_int32(coef[0]))

	for j := opus_int(2); j < order; j += 2 {
		tmp2 = data1[j-1]
		data1[j-1] = tmp1
		out = silk_SMLAWB(out, tmp1, opus_int32(coef[j-1]))
		tmp1 = data1[j+0]
		data1[j+0] = tmp2
		out = silk_SMLAWB(out, tmp2, opus_int32(coef[j]))
	}
	data1[order-1] = tmp1
	out = silk_SMLAWB(out, tmp1, opus_int32(coef[order-1]))
	out = silk_LSHIFT32(out, 1)
	return out
}

// silk_nsq_scale_states — C: NSQ.c:368-437.
func silk_nsq_scale_states(
	psEncC *silk_encoder_state,
	NSQ *silk_nsq_state,
	x16 []opus_int16,
	x_sc_Q10 []opus_int32,
	sLTP []opus_int16,
	sLTP_Q15 []opus_int32,
	subfr opus_int,
	LTP_scale_Q14 opus_int,
	Gains_Q16 []opus_int32,
	pitchL []opus_int,
	signal_type opus_int,
) {
	lag := pitchL[subfr]
	inv_gain_Q31 := silk_INVERSE32_varQ(silk_max(Gains_Q16[subfr], 1), 47)

	// Scale input.
	inv_gain_Q26 := silk_RSHIFT_ROUND(inv_gain_Q31, 5)
	for i := opus_int(0); i < psEncC.subfr_length; i++ {
		x_sc_Q10[i] = silk_SMULWW(opus_int32(x16[i]), inv_gain_Q26)
	}

	// After rewhitening the LTP state is un-scaled, so scale with inv_gain_Q16.
	if NSQ.rewhite_flag != 0 {
		if subfr == 0 {
			inv_gain_Q31 = silk_LSHIFT(silk_SMULWB(inv_gain_Q31, opus_int32(LTP_scale_Q14)), 2)
		}
		for i := NSQ.sLTP_buf_idx - lag - LTP_ORDER/2; i < NSQ.sLTP_buf_idx; i++ {
			sLTP_Q15[i] = silk_SMULWB(inv_gain_Q31, opus_int32(sLTP[i]))
		}
	}

	// Adjust for changing gain.
	if Gains_Q16[subfr] != NSQ.prev_gain_Q16 {
		gain_adj_Q16 := silk_DIV32_varQ(NSQ.prev_gain_Q16, Gains_Q16[subfr], 16)

		// Scale long-term shaping state.
		for i := NSQ.sLTP_shp_buf_idx - psEncC.ltp_mem_length; i < NSQ.sLTP_shp_buf_idx; i++ {
			NSQ.sLTP_shp_Q14[i] = silk_SMULWW(gain_adj_Q16, NSQ.sLTP_shp_Q14[i])
		}

		// Scale long-term prediction state.
		if signal_type == TYPE_VOICED && NSQ.rewhite_flag == 0 {
			for i := NSQ.sLTP_buf_idx - lag - LTP_ORDER/2; i < NSQ.sLTP_buf_idx; i++ {
				sLTP_Q15[i] = silk_SMULWW(gain_adj_Q16, sLTP_Q15[i])
			}
		}

		NSQ.sLF_AR_shp_Q14 = silk_SMULWW(gain_adj_Q16, NSQ.sLF_AR_shp_Q14)
		NSQ.sDiff_shp_Q14 = silk_SMULWW(gain_adj_Q16, NSQ.sDiff_shp_Q14)

		// Scale short-term prediction and shaping states.
		for i := 0; i < NSQ_LPC_BUF_LENGTH; i++ {
			NSQ.sLPC_Q14[i] = silk_SMULWW(gain_adj_Q16, NSQ.sLPC_Q14[i])
		}
		for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
			NSQ.sAR2_Q14[i] = silk_SMULWW(gain_adj_Q16, NSQ.sAR2_Q14[i])
		}

		// Save inverse gain.
		NSQ.prev_gain_Q16 = Gains_Q16[subfr]
	}
}
