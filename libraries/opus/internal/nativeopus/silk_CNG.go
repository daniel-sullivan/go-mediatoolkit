package nativeopus

// Port of libopus/silk/CNG.c.

// silk_CNG_exc — generate excitation for CNG LPC synthesis.
// C: CNG.c:36-60 (static).
func silk_CNG_exc(exc_Q14 []opus_int32, exc_buf_Q14 []opus_int32,
	length opus_int, rand_seed *opus_int32) {
	exc_mask := opus_int(CNG_BUF_MASK_MAX)
	for exc_mask > length {
		exc_mask = opus_int(silk_RSHIFT(opus_int32(exc_mask), 1))
	}

	seed := *rand_seed
	for i := opus_int(0); i < length; i++ {
		seed = silk_RAND(seed)
		idx := opus_int(silk_RSHIFT(seed, 24)) & exc_mask
		silk_assert(idx >= 0)
		silk_assert(idx <= CNG_BUF_MASK_MAX)
		exc_Q14[i] = exc_buf_Q14[idx]
	}
	*rand_seed = seed
}

// silk_CNG_Reset — reset the CNG state to defaults.
// C: CNG.c:62-76.
func silk_CNG_Reset(psDec *silk_decoder_state) {
	NLSF_step_Q15 := silk_DIV32_16(opus_int32(silk_int16_MAX), opus_int32(psDec.LPC_order+1))
	NLSF_acc_Q15 := opus_int32(0)
	for i := opus_int(0); i < opus_int(psDec.LPC_order); i++ {
		NLSF_acc_Q15 += NLSF_step_Q15
		psDec.sCNG.CNG_smth_NLSF_Q15[i] = opus_int16(NLSF_acc_Q15)
	}
	psDec.sCNG.CNG_smth_Gain_Q16 = 0
	psDec.sCNG.rand_seed = 3176576
}

// silk_CNG — update CNG estimate, and apply the CNG when the packet was lost.
// C: CNG.c:79-188.
func silk_CNG(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control,
	frame []opus_int16, length opus_int) {

	var LPC_pred_Q10, max_Gain_Q16, gain_Q16, gain_Q10 opus_int32
	var A_Q12 [MAX_LPC_ORDER]opus_int16
	psCNG := &psDec.sCNG

	if psDec.fs_kHz != psCNG.fs_kHz {
		silk_CNG_Reset(psDec)
		psCNG.fs_kHz = psDec.fs_kHz
	}
	if psDec.lossCnt == 0 && psDec.prevSignalType == TYPE_NO_VOICE_ACTIVITY {
		// Update CNG parameters.

		// Smoothing of LSFs.
		for i := opus_int(0); i < opus_int(psDec.LPC_order); i++ {
			psCNG.CNG_smth_NLSF_Q15[i] += opus_int16(silk_SMULWB(
				opus_int32(psDec.prevNLSF_Q15[i])-opus_int32(psCNG.CNG_smth_NLSF_Q15[i]),
				CNG_NLSF_SMTH_Q16))
		}
		// Find the subframe with the highest gain.
		max_Gain_Q16 = 0
		subfr := opus_int(0)
		for i := opus_int(0); i < psDec.nb_subfr; i++ {
			if psDecCtrl.Gains_Q16[i] > max_Gain_Q16 {
				max_Gain_Q16 = psDecCtrl.Gains_Q16[i]
				subfr = i
			}
		}
		// Update CNG excitation buffer.
		// silk_memmove(&buf[subfr_length], buf, (nb_subfr-1)*subfr_length * sizeof(int32))
		copy(psCNG.CNG_exc_buf_Q14[psDec.subfr_length:psDec.subfr_length+
			(psDec.nb_subfr-1)*psDec.subfr_length],
			psCNG.CNG_exc_buf_Q14[:(psDec.nb_subfr-1)*psDec.subfr_length])
		copy(psCNG.CNG_exc_buf_Q14[:psDec.subfr_length],
			psDec.exc_Q14[subfr*psDec.subfr_length:subfr*psDec.subfr_length+psDec.subfr_length])

		// Smooth gains.
		for i := opus_int(0); i < psDec.nb_subfr; i++ {
			psCNG.CNG_smth_Gain_Q16 += silk_SMULWB(
				psDecCtrl.Gains_Q16[i]-psCNG.CNG_smth_Gain_Q16, CNG_GAIN_SMTH_Q16)
			// If the smoothed gain is 3 dB greater than this subframe's gain, adapt faster.
			if silk_SMULWW(psCNG.CNG_smth_Gain_Q16, CNG_GAIN_SMTH_THRESHOLD_Q16) > psDecCtrl.Gains_Q16[i] {
				psCNG.CNG_smth_Gain_Q16 = psDecCtrl.Gains_Q16[i]
			}
		}
	}

	// Add CNG when packet is lost or during DTX.
	if psDec.lossCnt != 0 {
		CNG_sig_Q14 := make([]opus_int32, length+MAX_LPC_ORDER)

		gain_Q16 = silk_SMULWW(opus_int32(psDec.sPLC.randScale_Q14), psDec.sPLC.prevGain_Q16[1])
		if gain_Q16 >= (1<<21) || psCNG.CNG_smth_Gain_Q16 > (1<<23) {
			gain_Q16 = silk_SMULTT(gain_Q16, gain_Q16)
			gain_Q16 = silk_SUB_LSHIFT32(silk_SMULTT(psCNG.CNG_smth_Gain_Q16, psCNG.CNG_smth_Gain_Q16), gain_Q16, 5)
			gain_Q16 = silk_LSHIFT32(silk_SQRT_APPROX(gain_Q16), 16)
		} else {
			gain_Q16 = silk_SMULWW(gain_Q16, gain_Q16)
			gain_Q16 = silk_SUB_LSHIFT32(silk_SMULWW(psCNG.CNG_smth_Gain_Q16, psCNG.CNG_smth_Gain_Q16), gain_Q16, 5)
			gain_Q16 = silk_LSHIFT32(silk_SQRT_APPROX(gain_Q16), 8)
		}
		gain_Q10 = silk_RSHIFT(gain_Q16, 6)

		silk_CNG_exc(CNG_sig_Q14[MAX_LPC_ORDER:], psCNG.CNG_exc_buf_Q14[:], length, &psCNG.rand_seed)

		// Convert CNG NLSF to filter representation.
		silk_NLSF2A(A_Q12[:], psCNG.CNG_smth_NLSF_Q15[:], psDec.LPC_order, psDec.arch)

		// Generate CNG signal by synthesis filtering.
		copy(CNG_sig_Q14[:MAX_LPC_ORDER], psCNG.CNG_synth_state[:MAX_LPC_ORDER])
		celt_assert(psDec.LPC_order == 10 || psDec.LPC_order == 16)
		for i := opus_int(0); i < length; i++ {
			LPC_pred_Q10 = silk_RSHIFT(opus_int32(psDec.LPC_order), 1)
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-1], opus_int32(A_Q12[0]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-2], opus_int32(A_Q12[1]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-3], opus_int32(A_Q12[2]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-4], opus_int32(A_Q12[3]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-5], opus_int32(A_Q12[4]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-6], opus_int32(A_Q12[5]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-7], opus_int32(A_Q12[6]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-8], opus_int32(A_Q12[7]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-9], opus_int32(A_Q12[8]))
			LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-10], opus_int32(A_Q12[9]))
			if psDec.LPC_order == 16 {
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-11], opus_int32(A_Q12[10]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-12], opus_int32(A_Q12[11]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-13], opus_int32(A_Q12[12]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-14], opus_int32(A_Q12[13]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-15], opus_int32(A_Q12[14]))
				LPC_pred_Q10 = silk_SMLAWB(LPC_pred_Q10, CNG_sig_Q14[MAX_LPC_ORDER+i-16], opus_int32(A_Q12[15]))
			}

			// Update states.
			CNG_sig_Q14[MAX_LPC_ORDER+i] = silk_ADD_SAT32(CNG_sig_Q14[MAX_LPC_ORDER+i],
				silk_LSHIFT_SAT32(LPC_pred_Q10, 4))

			// Scale with gain and add to input signal.
			frame[i] = opus_int16(silk_ADD_SAT16(frame[i],
				opus_int16(silk_SAT16(silk_RSHIFT_ROUND(silk_SMULWW(CNG_sig_Q14[MAX_LPC_ORDER+i], gain_Q10), 8)))))
		}
		copy(psCNG.CNG_synth_state[:MAX_LPC_ORDER], CNG_sig_Q14[length:length+MAX_LPC_ORDER])
	} else {
		for i := opus_int(0); i < opus_int(psDec.LPC_order); i++ {
			psCNG.CNG_synth_state[i] = 0
		}
	}
}
