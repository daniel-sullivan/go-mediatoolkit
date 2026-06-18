package nativeopus

// Port of libopus/silk/decode_core.c.

// silk_decode_core — core decoder. Performs inverse NSQ operation LTP + LPC.
// C: decode_core.c:38-243.
func silk_decode_core(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control,
	xq []opus_int16, pulses []opus_int16, arch int) {

	var (
		lag, start_idx, sLTP_buf_idx, NLSF_interpolation_flag, signalType                       opus_int
		A_Q12_tmp                                                                               [MAX_LPC_ORDER]opus_int16
		LTP_pred_Q13, LPC_pred_Q10, Gain_Q10, inv_gain_Q31, gain_adj_Q16, rand_seed, offset_Q10 opus_int32
	)

	silk_assert(psDec.prev_gain_Q16 != 0)

	// Reuse scratch buffers on psDec instead of allocating fresh each
	// call. Sizes are bounded by the SILK internal-rate × frame-length
	// product (see silk_decoder_state.scratch_* fields).
	sLTP := psDec.scratch_sLTP[:psDec.ltp_mem_length]
	sLTP_Q15 := psDec.scratch_sLTP_Q15[:psDec.ltp_mem_length+psDec.frame_length]
	res_Q14 := psDec.scratch_res_Q14[:psDec.subfr_length]
	sLPC_Q14 := psDec.scratch_sLPC_Q14[:psDec.subfr_length+MAX_LPC_ORDER]

	offset_Q10 = opus_int32(silk_Quantization_Offsets_Q10[psDec.indices.signalType>>1][psDec.indices.quantOffsetType])

	if psDec.indices.NLSFInterpCoef_Q2 < 1<<2 {
		NLSF_interpolation_flag = 1
	} else {
		NLSF_interpolation_flag = 0
	}

	// Decode excitation.
	rand_seed = opus_int32(psDec.indices.Seed)
	for i := opus_int(0); i < psDec.frame_length; i++ {
		rand_seed = silk_RAND(rand_seed)
		psDec.exc_Q14[i] = silk_LSHIFT(opus_int32(pulses[i]), 14)
		if psDec.exc_Q14[i] > 0 {
			psDec.exc_Q14[i] -= QUANT_LEVEL_ADJUST_Q10 << 4
		} else if psDec.exc_Q14[i] < 0 {
			psDec.exc_Q14[i] += QUANT_LEVEL_ADJUST_Q10 << 4
		}
		psDec.exc_Q14[i] += offset_Q10 << 4
		if rand_seed < 0 {
			psDec.exc_Q14[i] = -psDec.exc_Q14[i]
		}

		rand_seed = silk_ADD32_ovflw(rand_seed, opus_int32(pulses[i]))
	}

	// Copy LPC state.
	copy(sLPC_Q14[:MAX_LPC_ORDER], psDec.sLPC_Q14_buf[:MAX_LPC_ORDER])

	pexcOff := opus_int(0)
	pxqOff := opus_int(0)
	sLTP_buf_idx = psDec.ltp_mem_length

	for k := opus_int(0); k < psDec.nb_subfr; k++ {
		presUseExc := false
		A_Q12 := psDecCtrl.PredCoef_Q12[k>>1][:]
		copy(A_Q12_tmp[:psDec.LPC_order], A_Q12[:psDec.LPC_order])
		B_Q14 := psDecCtrl.LTPCoef_Q14[k*LTP_ORDER : k*LTP_ORDER+LTP_ORDER]
		signalType = opus_int(psDec.indices.signalType)

		Gain_Q10 = silk_RSHIFT(psDecCtrl.Gains_Q16[k], 6)
		inv_gain_Q31 = silk_INVERSE32_varQ(psDecCtrl.Gains_Q16[k], 47)

		// Gain adjustment factor.
		if psDecCtrl.Gains_Q16[k] != psDec.prev_gain_Q16 {
			gain_adj_Q16 = silk_DIV32_varQ(psDec.prev_gain_Q16, psDecCtrl.Gains_Q16[k], 16)
			for i := 0; i < MAX_LPC_ORDER; i++ {
				sLPC_Q14[i] = silk_SMULWW(gain_adj_Q16, sLPC_Q14[i])
			}
		} else {
			gain_adj_Q16 = opus_int32(1) << 16
		}

		silk_assert(inv_gain_Q31 != 0)
		psDec.prev_gain_Q16 = psDecCtrl.Gains_Q16[k]

		// Avoid abrupt transition from voiced PLC to unvoiced normal decoding.
		if psDec.lossCnt != 0 && psDec.prevSignalType == TYPE_VOICED &&
			psDec.indices.signalType != TYPE_VOICED && k < MAX_NB_SUBFR/2 {

			for i := opus_int(0); i < LTP_ORDER; i++ {
				B_Q14[i] = 0
			}
			B_Q14[LTP_ORDER/2] = opus_int16(SILK_FIX_CONST(0.25, 14))

			signalType = TYPE_VOICED
			psDecCtrl.pitchL[k] = psDec.lagPrev
		}

		if signalType == TYPE_VOICED {
			lag = psDecCtrl.pitchL[k]

			// Re-whitening.
			if k == 0 || (k == 2 && NLSF_interpolation_flag != 0) {
				start_idx = psDec.ltp_mem_length - lag - psDec.LPC_order - LTP_ORDER/2
				celt_assert(start_idx > 0)

				if k == 2 {
					copy(psDec.outBuf[psDec.ltp_mem_length:psDec.ltp_mem_length+2*psDec.subfr_length],
						xq[:2*psDec.subfr_length])
				}

				silk_LPC_analysis_filter(sLTP[start_idx:],
					psDec.outBuf[start_idx+k*psDec.subfr_length:],
					A_Q12, opus_int32(psDec.ltp_mem_length-start_idx), opus_int32(psDec.LPC_order), arch)

				if k == 0 {
					inv_gain_Q31 = silk_LSHIFT(silk_SMULWB(inv_gain_Q31, opus_int32(psDecCtrl.LTP_scale_Q14)), 2)
				}
				for i := opus_int(0); i < lag+LTP_ORDER/2; i++ {
					sLTP_Q15[sLTP_buf_idx-i-1] = silk_SMULWB(inv_gain_Q31, opus_int32(sLTP[psDec.ltp_mem_length-i-1]))
				}
			} else {
				if gain_adj_Q16 != opus_int32(1)<<16 {
					for i := opus_int(0); i < lag+LTP_ORDER/2; i++ {
						sLTP_Q15[sLTP_buf_idx-i-1] = silk_SMULWW(gain_adj_Q16, sLTP_Q15[sLTP_buf_idx-i-1])
					}
				}
			}
		}

		// Long-term prediction.
		if signalType == TYPE_VOICED {
			predBase := sLTP_buf_idx - lag + LTP_ORDER/2
			for i := opus_int(0); i < psDec.subfr_length; i++ {
				LTP_pred_Q13 = 2
				LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[predBase+i+0], opus_int32(B_Q14[0]))
				LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[predBase+i-1], opus_int32(B_Q14[1]))
				LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[predBase+i-2], opus_int32(B_Q14[2]))
				LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[predBase+i-3], opus_int32(B_Q14[3]))
				LTP_pred_Q13 = silk_SMLAWB(LTP_pred_Q13, sLTP_Q15[predBase+i-4], opus_int32(B_Q14[4]))

				// Generate LPC excitation.
				res_Q14[i] = silk_ADD_LSHIFT32(psDec.exc_Q14[pexcOff+i], LTP_pred_Q13, 1)

				// Update states.
				sLTP_Q15[sLTP_buf_idx] = silk_LSHIFT(res_Q14[i], 1)
				sLTP_buf_idx++
			}
		} else {
			presUseExc = true
		}

		for i := opus_int(0); i < psDec.subfr_length; i++ {
			celt_assert(psDec.LPC_order == 10 || psDec.LPC_order == 16)
			LPC_pred_Q10 = silk_RSHIFT(opus_int32(psDec.LPC_order), 1)
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-1], opus_int32(A_Q12_tmp[0]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-2], opus_int32(A_Q12_tmp[1]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-3], opus_int32(A_Q12_tmp[2]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-4], opus_int32(A_Q12_tmp[3]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-5], opus_int32(A_Q12_tmp[4]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-6], opus_int32(A_Q12_tmp[5]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-7], opus_int32(A_Q12_tmp[6]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-8], opus_int32(A_Q12_tmp[7]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-9], opus_int32(A_Q12_tmp[8]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-10], opus_int32(A_Q12_tmp[9]))
			if psDec.LPC_order == 16 {
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-11], opus_int32(A_Q12_tmp[10]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-12], opus_int32(A_Q12_tmp[11]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-13], opus_int32(A_Q12_tmp[12]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-14], opus_int32(A_Q12_tmp[13]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-15], opus_int32(A_Q12_tmp[14]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLPC_Q14[MAX_LPC_ORDER+i-16], opus_int32(A_Q12_tmp[15]))
			}

			// Add prediction to LPC excitation.
			var pres_i opus_int32
			if presUseExc {
				pres_i = psDec.exc_Q14[pexcOff+i]
			} else {
				pres_i = res_Q14[i]
			}
			sLPC_Q14[MAX_LPC_ORDER+i] = silk_ADD_SAT32(pres_i, silk_LSHIFT_SAT32(LPC_pred_Q10, 4))

			// Scale with gain.
			xq[pxqOff+i] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(silk_SMULWW(sLPC_Q14[MAX_LPC_ORDER+i], Gain_Q10), 8)))
		}

		// Update LPC filter state.
		copy(sLPC_Q14[:MAX_LPC_ORDER], sLPC_Q14[psDec.subfr_length:psDec.subfr_length+MAX_LPC_ORDER])
		pexcOff += psDec.subfr_length
		pxqOff += psDec.subfr_length
	}

	// Save LPC state.
	copy(psDec.sLPC_Q14_buf[:MAX_LPC_ORDER], sLPC_Q14[:MAX_LPC_ORDER])
}
