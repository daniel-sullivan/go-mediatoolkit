package nativeopus

// Port of libopus/silk/PLC.c and PLC.h (the decoder-side constants).

// PLC.h constants.
const (
	BWE_COEF                     = 0.99
	V_PITCH_GAIN_START_MIN_Q14   = 11469 // 0.7 in Q14
	V_PITCH_GAIN_START_MAX_Q14   = 15565 // 0.95 in Q14
	MAX_PITCH_LAG_MS             = 18
	RAND_BUF_SIZE                = 128
	RAND_BUF_MASK                = RAND_BUF_SIZE - 1
	LOG2_INV_LPC_GAIN_HIGH_THRES = 3   // 2^3 = 8 dB LPC gain
	LOG2_INV_LPC_GAIN_LOW_THRES  = 8   // 2^8 = 24 dB LPC gain
	PITCH_DRIFT_FAC_Q16          = 655 // 0.01 in Q16
)

// NB_ATT PLC attenuation tables.
var silk_PLC_HARM_ATT_Q15 = [2]opus_int16{32440, 31130}          // 0.99, 0.95
var silk_PLC_RAND_ATTENUATE_V_Q15 = [2]opus_int16{31130, 26214}  // 0.95, 0.8
var silk_PLC_RAND_ATTENUATE_UV_Q15 = [2]opus_int16{32440, 29491} // 0.99, 0.9

// silk_PLC_Reset — reset PLC state to defaults.
// C: PLC.c:61-70.
func silk_PLC_Reset(psDec *silk_decoder_state) {
	psDec.sPLC.pitchL_Q8 = silk_LSHIFT(opus_int32(psDec.frame_length), 8-1)
	psDec.sPLC.prevGain_Q16[0] = SILK_FIX_CONST(1, 16)
	psDec.sPLC.prevGain_Q16[1] = SILK_FIX_CONST(1, 16)
	psDec.sPLC.subfr_length = 20
	psDec.sPLC.nb_subfr = 2
}

// silk_PLC — PLC control: dispatches to update (good frame) or conceal (lost).
// C: PLC.c:72-114.
func silk_PLC(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control,
	frame []opus_int16, lost opus_int, arch int) {

	if psDec.fs_kHz != psDec.sPLC.fs_kHz {
		silk_PLC_Reset(psDec)
		psDec.sPLC.fs_kHz = psDec.fs_kHz
	}

	if lost != 0 {
		silk_PLC_conceal(psDec, psDecCtrl, frame, arch)
		psDec.lossCnt++
	} else {
		silk_PLC_update(psDec, psDecCtrl)
	}
}

// silk_PLC_update — update PLC state after a good frame.
// C: PLC.c:119-190 (static).
func silk_PLC_update(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control) {
	var LTP_Gain_Q14, temp_LTP_Gain_Q14 opus_int32
	psPLC := &psDec.sPLC

	psDec.prevSignalType = opus_int(psDec.indices.signalType)
	LTP_Gain_Q14 = 0
	if psDec.indices.signalType == TYPE_VOICED {
		// Find the parameters for the last subframe which contains a pitch pulse.
		for j := opus_int(0); j*psDec.subfr_length < psDecCtrl.pitchL[psDec.nb_subfr-1]; j++ {
			if j == psDec.nb_subfr {
				break
			}
			temp_LTP_Gain_Q14 = 0
			for i := opus_int(0); i < LTP_ORDER; i++ {
				temp_LTP_Gain_Q14 += opus_int32(psDecCtrl.LTPCoef_Q14[(psDec.nb_subfr-1-j)*LTP_ORDER+i])
			}
			if temp_LTP_Gain_Q14 > LTP_Gain_Q14 {
				LTP_Gain_Q14 = temp_LTP_Gain_Q14
				base := opus_int(silk_SMULBB(opus_int32(psDec.nb_subfr-1-j), LTP_ORDER))
				copy(psPLC.LTPCoef_Q14[:LTP_ORDER], psDecCtrl.LTPCoef_Q14[base:base+LTP_ORDER])

				psPLC.pitchL_Q8 = silk_LSHIFT(opus_int32(psDecCtrl.pitchL[psDec.nb_subfr-1-j]), 8)
			}
		}

		for i := opus_int(0); i < LTP_ORDER; i++ {
			psPLC.LTPCoef_Q14[i] = 0
		}
		psPLC.LTPCoef_Q14[LTP_ORDER/2] = opus_int16(LTP_Gain_Q14)

		// Limit LT coefs.
		if LTP_Gain_Q14 < V_PITCH_GAIN_START_MIN_Q14 {
			tmp := silk_LSHIFT(V_PITCH_GAIN_START_MIN_Q14, 10)
			scale_Q10 := silk_DIV32(tmp, silk_max_32(LTP_Gain_Q14, 1))
			for i := opus_int(0); i < LTP_ORDER; i++ {
				psPLC.LTPCoef_Q14[i] = opus_int16(silk_RSHIFT(
					silk_SMULBB(opus_int32(psPLC.LTPCoef_Q14[i]), scale_Q10), 10))
			}
		} else if LTP_Gain_Q14 > V_PITCH_GAIN_START_MAX_Q14 {
			tmp := silk_LSHIFT(V_PITCH_GAIN_START_MAX_Q14, 14)
			scale_Q14 := silk_DIV32(tmp, silk_max_32(LTP_Gain_Q14, 1))
			for i := opus_int(0); i < LTP_ORDER; i++ {
				psPLC.LTPCoef_Q14[i] = opus_int16(silk_RSHIFT(
					silk_SMULBB(opus_int32(psPLC.LTPCoef_Q14[i]), scale_Q14), 14))
			}
		}
	} else {
		psPLC.pitchL_Q8 = silk_LSHIFT(silk_SMULBB(opus_int32(psDec.fs_kHz), 18), 8)
		for i := opus_int(0); i < LTP_ORDER; i++ {
			psPLC.LTPCoef_Q14[i] = 0
		}
	}

	// Save LPC coefficients.
	copy(psPLC.prevLPC_Q12[:psDec.LPC_order], psDecCtrl.PredCoef_Q12[1][:psDec.LPC_order])
	psPLC.prevLTP_scale_Q14 = opus_int16(psDecCtrl.LTP_scale_Q14)

	// Save last two gains.
	psPLC.prevGain_Q16[0] = psDecCtrl.Gains_Q16[psDec.nb_subfr-2]
	psPLC.prevGain_Q16[1] = psDecCtrl.Gains_Q16[psDec.nb_subfr-1]

	psPLC.subfr_length = psDec.subfr_length
	psPLC.nb_subfr = psDec.nb_subfr
}

// silk_PLC_energy — compute per-sub-frame energies of the scaled excitation.
// C: PLC.c:192-214 (static).
func silk_PLC_energy(energy1 *opus_int32, shift1 *opus_int,
	energy2 *opus_int32, shift2 *opus_int,
	exc_Q14 []opus_int32, prevGain_Q10 []opus_int32,
	subfr_length opus_int, nb_subfr opus_int) {

	exc_buf := make([]opus_int16, 2*subfr_length)
	for k := opus_int(0); k < 2; k++ {
		for i := opus_int(0); i < subfr_length; i++ {
			exc_buf[k*subfr_length+i] = opus_int16(silk_SAT16(silk_RSHIFT(
				silk_SMULWW(exc_Q14[i+(k+nb_subfr-2)*subfr_length], prevGain_Q10[k]), 8)))
		}
	}
	silk_sum_sqr_shift(energy1, shift1, exc_buf[:subfr_length], subfr_length)
	silk_sum_sqr_shift(energy2, shift2, exc_buf[subfr_length:], subfr_length)
}

// silk_PLC_conceal — generate concealed frame content.
// C: PLC.c:216-430 (static).
func silk_PLC_conceal(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control,
	frame []opus_int16, arch int) {

	var (
		lag, idx, sLTP_buf_idx, shift1, shift2                opus_int
		rand_seed, harm_Gain_Q15, rand_Gain_Q15, inv_gain_Q30 opus_int32
		energy1, energy2                                      opus_int32
		LPC_pred_Q10, LTP_pred_Q12                            opus_int32
		rand_scale_Q14                                        opus_int16
		A_Q12                                                 [MAX_LPC_ORDER]opus_int16
		prevGain_Q10                                          [2]opus_int32
	)
	psPLC := &psDec.sPLC

	sLTP_Q14 := make([]opus_int32, psDec.ltp_mem_length+psDec.frame_length)
	sLTP := make([]opus_int16, psDec.ltp_mem_length)

	prevGain_Q10[0] = silk_RSHIFT(psPLC.prevGain_Q16[0], 6)
	prevGain_Q10[1] = silk_RSHIFT(psPLC.prevGain_Q16[1], 6)

	if psDec.first_frame_after_reset != 0 {
		for i := range psPLC.prevLPC_Q12 {
			psPLC.prevLPC_Q12[i] = 0
		}
	}

	silk_PLC_energy(&energy1, &shift1, &energy2, &shift2,
		psDec.exc_Q14[:], prevGain_Q10[:], psDec.subfr_length, psDec.nb_subfr)

	var rand_ptr []opus_int32
	if silk_RSHIFT(energy1, opus_int(shift2)) < silk_RSHIFT(energy2, opus_int(shift1)) {
		// First sub-frame has lowest energy.
		off := silk_max_int(0, (psPLC.nb_subfr-1)*psPLC.subfr_length-RAND_BUF_SIZE)
		rand_ptr = psDec.exc_Q14[off:]
	} else {
		// Second sub-frame has lowest energy.
		off := silk_max_int(0, psPLC.nb_subfr*psPLC.subfr_length-RAND_BUF_SIZE)
		rand_ptr = psDec.exc_Q14[off:]
	}

	// Set up Gain to random noise component.
	B_Q14 := psPLC.LTPCoef_Q14[:]
	rand_scale_Q14 = psPLC.randScale_Q14

	// Set up attenuation gains.
	harm_Gain_Q15 = opus_int32(silk_PLC_HARM_ATT_Q15[silk_min_int(2-1, psDec.lossCnt)])
	if psDec.prevSignalType == TYPE_VOICED {
		rand_Gain_Q15 = opus_int32(silk_PLC_RAND_ATTENUATE_V_Q15[silk_min_int(2-1, psDec.lossCnt)])
	} else {
		rand_Gain_Q15 = opus_int32(silk_PLC_RAND_ATTENUATE_UV_Q15[silk_min_int(2-1, psDec.lossCnt)])
	}

	// LPC concealment. Apply BWE to previous LPC.
	silk_bwexpander(psPLC.prevLPC_Q12[:], psDec.LPC_order, SILK_FIX_CONST(BWE_COEF, 16))

	copy(A_Q12[:psDec.LPC_order], psPLC.prevLPC_Q12[:psDec.LPC_order])

	// First lost frame.
	if psDec.lossCnt == 0 {
		rand_scale_Q14 = 1 << 14

		if psDec.prevSignalType == TYPE_VOICED {
			for i := opus_int(0); i < LTP_ORDER; i++ {
				rand_scale_Q14 -= B_Q14[i]
			}
			rand_scale_Q14 = silk_max_16(3277, rand_scale_Q14) // 0.2
			rand_scale_Q14 = opus_int16(silk_RSHIFT(
				silk_SMULBB(opus_int32(rand_scale_Q14), opus_int32(psPLC.prevLTP_scale_Q14)), 14))
		} else {
			invGain_Q30 := silk_LPC_inverse_pred_gain_c(psPLC.prevLPC_Q12[:], psDec.LPC_order)

			down_scale_Q30 := silk_min_32(
				silk_RSHIFT(opus_int32(1)<<30, LOG2_INV_LPC_GAIN_HIGH_THRES),
				invGain_Q30)
			down_scale_Q30 = silk_max_32(
				silk_RSHIFT(opus_int32(1)<<30, LOG2_INV_LPC_GAIN_LOW_THRES),
				down_scale_Q30)
			down_scale_Q30 = silk_LSHIFT(down_scale_Q30, LOG2_INV_LPC_GAIN_HIGH_THRES)

			rand_Gain_Q15 = silk_RSHIFT(silk_SMULWB(down_scale_Q30, rand_Gain_Q15), 14)
		}
	}

	rand_seed = psPLC.rand_seed
	lag = opus_int(silk_RSHIFT_ROUND(psPLC.pitchL_Q8, 8))
	sLTP_buf_idx = psDec.ltp_mem_length

	// Rewhiten LTP state.
	idx = psDec.ltp_mem_length - lag - psDec.LPC_order - LTP_ORDER/2
	celt_assert(idx > 0)
	silk_LPC_analysis_filter(sLTP[idx:], psDec.outBuf[idx:], A_Q12[:],
		opus_int32(psDec.ltp_mem_length-idx), opus_int32(psDec.LPC_order), arch)
	// Scale LTP state.
	inv_gain_Q30 = silk_INVERSE32_varQ(psPLC.prevGain_Q16[1], 46)
	inv_gain_Q30 = silk_min_32(inv_gain_Q30, silk_int32_MAX>>1)
	for i := idx + psDec.LPC_order; i < psDec.ltp_mem_length; i++ {
		sLTP_Q14[i] = silk_SMULWB(inv_gain_Q30, opus_int32(sLTP[i]))
	}

	// LTP synthesis filtering.
	for k := opus_int(0); k < psDec.nb_subfr; k++ {
		predBase := sLTP_buf_idx - lag + LTP_ORDER/2
		for i := opus_int(0); i < psDec.subfr_length; i++ {
			LTP_pred_Q12 = 2
			LTP_pred_Q12 = silk_SMLAWB(LTP_pred_Q12, sLTP_Q14[predBase+i+0], opus_int32(B_Q14[0]))
			LTP_pred_Q12 = silk_SMLAWB(LTP_pred_Q12, sLTP_Q14[predBase+i-1], opus_int32(B_Q14[1]))
			LTP_pred_Q12 = silk_SMLAWB(LTP_pred_Q12, sLTP_Q14[predBase+i-2], opus_int32(B_Q14[2]))
			LTP_pred_Q12 = silk_SMLAWB(LTP_pred_Q12, sLTP_Q14[predBase+i-3], opus_int32(B_Q14[3]))
			LTP_pred_Q12 = silk_SMLAWB(LTP_pred_Q12, sLTP_Q14[predBase+i-4], opus_int32(B_Q14[4]))

			// Generate LPC excitation.
			rand_seed = silk_RAND(rand_seed)
			idx = opus_int(silk_RSHIFT(rand_seed, 25)) & RAND_BUF_MASK
			sLTP_Q14[sLTP_buf_idx] = silk_LSHIFT32(silk_SMLAWB(LTP_pred_Q12, rand_ptr[idx], opus_int32(rand_scale_Q14)), 2)
			sLTP_buf_idx++
		}

		// Gradually reduce LTP gain.
		for j := opus_int(0); j < LTP_ORDER; j++ {
			B_Q14[j] = opus_int16(silk_RSHIFT(silk_SMULBB(harm_Gain_Q15, opus_int32(B_Q14[j])), 15))
		}
		// Gradually reduce excitation gain.
		rand_scale_Q14 = opus_int16(silk_RSHIFT(silk_SMULBB(opus_int32(rand_scale_Q14), rand_Gain_Q15), 15))

		// Slowly increase pitch lag.
		psPLC.pitchL_Q8 = silk_SMLAWB(psPLC.pitchL_Q8, psPLC.pitchL_Q8, PITCH_DRIFT_FAC_Q16)
		psPLC.pitchL_Q8 = silk_min_32(psPLC.pitchL_Q8,
			silk_LSHIFT(silk_SMULBB(MAX_PITCH_LAG_MS, opus_int32(psDec.fs_kHz)), 8))
		lag = opus_int(silk_RSHIFT_ROUND(psPLC.pitchL_Q8, 8))
	}

	// LPC synthesis filtering.
	sLPC_Q14_base := psDec.ltp_mem_length - MAX_LPC_ORDER

	// Copy LPC state.
	copy(sLTP_Q14[sLPC_Q14_base:sLPC_Q14_base+MAX_LPC_ORDER], psDec.sLPC_Q14_buf[:MAX_LPC_ORDER])

	celt_assert(psDec.LPC_order >= 10)
	for i := opus_int(0); i < psDec.frame_length; i++ {
		LPC_pred_Q10 = silk_RSHIFT(opus_int32(psDec.LPC_order), 1)
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-1], opus_int32(A_Q12[0]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-2], opus_int32(A_Q12[1]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-3], opus_int32(A_Q12[2]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-4], opus_int32(A_Q12[3]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-5], opus_int32(A_Q12[4]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-6], opus_int32(A_Q12[5]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-7], opus_int32(A_Q12[6]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-8], opus_int32(A_Q12[7]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-9], opus_int32(A_Q12[8]))
		LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-10], opus_int32(A_Q12[9]))
		for j := opus_int(10); j < psDec.LPC_order; j++ {
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i-j-1], opus_int32(A_Q12[j]))
		}

		// Add prediction to LPC excitation.
		sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i] = silk_ADD_SAT32(
			sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i],
			silk_LSHIFT_SAT32(LPC_pred_Q10, 4))

		// Scale with gain.
		frame[i] = opus_int16(silk_SAT16(silk_SAT16(silk_RSHIFT_ROUND(
			silk_SMULWW(sLTP_Q14[sLPC_Q14_base+MAX_LPC_ORDER+i], prevGain_Q10[1]), 8))))
	}

	// Save LPC state. C: silk_memcpy(psDec->sLPC_Q14_buf,
	// &sLPC_Q14_ptr[psDec->frame_length], MAX_LPC_ORDER*sizeof(opus_int32));
	// sLPC_Q14_ptr = &sLTP_Q14[ltp_mem_length - MAX_LPC_ORDER] = sLPC_Q14_base,
	// so the read starts at sLPC_Q14_base + frame_length (NOT + MAX_LPC_ORDER +
	// frame_length — that was an extraneous offset that ran past the buffer
	// tail by MAX_LPC_ORDER words for any frame where ltp_mem+frame filled
	// the allocation exactly, e.g. 48 kHz 20 ms mono where
	// ltp_mem_length+frame_length == len(sLTP_Q14)).
	copy(psDec.sLPC_Q14_buf[:MAX_LPC_ORDER],
		sLTP_Q14[sLPC_Q14_base+psDec.frame_length:sLPC_Q14_base+psDec.frame_length+MAX_LPC_ORDER])

	// Update states.
	psPLC.rand_seed = rand_seed
	psPLC.randScale_Q14 = rand_scale_Q14
	for i := opus_int(0); i < MAX_NB_SUBFR; i++ {
		psDecCtrl.pitchL[i] = lag
	}
}

// silk_PLC_glue_frames — smooth transition between concealed and good frames.
// C: PLC.c:432-493.
func silk_PLC_glue_frames(psDec *silk_decoder_state, frame []opus_int16, length opus_int) {
	var energy opus_int32
	var energy_shift opus_int
	psPLC := &psDec.sPLC

	if psDec.lossCnt != 0 {
		silk_sum_sqr_shift(&psPLC.conc_energy, &psPLC.conc_energy_shift, frame, length)
		psPLC.last_frame_lost = 1
	} else {
		if psDec.sPLC.last_frame_lost != 0 {
			silk_sum_sqr_shift(&energy, &energy_shift, frame, length)

			if energy_shift > psPLC.conc_energy_shift {
				psPLC.conc_energy = silk_RSHIFT(psPLC.conc_energy, energy_shift-psPLC.conc_energy_shift)
			} else if energy_shift < psPLC.conc_energy_shift {
				energy = silk_RSHIFT(energy, psPLC.conc_energy_shift-energy_shift)
			}

			// Fade in the energy difference.
			if energy > psPLC.conc_energy {
				LZ := silk_CLZ32(psPLC.conc_energy)
				LZ = LZ - 1
				psPLC.conc_energy = silk_LSHIFT(psPLC.conc_energy, opus_int(LZ))
				energy = silk_RSHIFT(energy, opus_int(silk_max_32(24-LZ, 0)))

				frac_Q24 := silk_DIV32(psPLC.conc_energy, silk_max_32(energy, 1))

				gain_Q16 := silk_LSHIFT(silk_SQRT_APPROX(frac_Q24), 4)
				slope_Q16 := silk_DIV32_16(((opus_int32)(1)<<16)-gain_Q16, opus_int32(length))
				slope_Q16 = silk_LSHIFT(slope_Q16, 2)
				for i := opus_int(0); i < length; i++ {
					frame[i] = opus_int16(silk_SMULWB(gain_Q16, opus_int32(frame[i])))
					gain_Q16 += slope_Q16
					if gain_Q16 > (opus_int32)(1)<<16 {
						break
					}
				}
			}
		}
		psPLC.last_frame_lost = 0
	}
}
