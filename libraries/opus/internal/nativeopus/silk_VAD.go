package nativeopus

// Port of libopus/silk/VAD.c.
//
// SILK voice activity detection: analysis filter-bank (0-1 kHz, 1-2,
// 2-4, 4-8 kHz), per-band energy accumulation, smoothed noise-level
// estimation, and sigmoid-scored speech activity output.

// tiltWeights — tilt measure coefficients per band (C: VAD.c:77).
var vad_tiltWeights = [VAD_N_BANDS]opus_int32{30000, 6000, -12000, -12000}

// silk_VAD_Init — C: VAD.c:46-74.
func silk_VAD_Init(psSilk_VAD *silk_VAD_state) opus_int {
	*psSilk_VAD = silk_VAD_state{}

	for b := 0; b < VAD_N_BANDS; b++ {
		psSilk_VAD.NoiseLevelBias[b] = silk_max_32(silk_DIV32_16(VAD_NOISE_LEVELS_BIAS, opus_int32(b+1)), 1)
	}
	for b := 0; b < VAD_N_BANDS; b++ {
		psSilk_VAD.NL[b] = silk_MUL(100, psSilk_VAD.NoiseLevelBias[b])
		psSilk_VAD.inv_NL[b] = silk_DIV32(opus_int32(silk_int32_MAX), psSilk_VAD.NL[b])
	}
	psSilk_VAD.counter = 15
	for b := 0; b < VAD_N_BANDS; b++ {
		psSilk_VAD.NrgRatioSmth_Q8[b] = 100 * 256
	}
	return 0
}

// silk_VAD_GetNoiseLevels — C: VAD.c:303-360.
func silk_VAD_GetNoiseLevels(pX []opus_int32, psSilk_VAD *silk_VAD_state) {
	var min_coef opus_int32
	if psSilk_VAD.counter < 1000 {
		min_coef = silk_DIV32_16(opus_int32(silk_int16_MAX), silk_RSHIFT(psSilk_VAD.counter, 4)+1)
		psSilk_VAD.counter++
	} else {
		min_coef = 0
	}

	for k := 0; k < VAD_N_BANDS; k++ {
		nl := psSilk_VAD.NL[k]
		silk_assert(nl >= 0)

		nrg := silk_ADD_POS_SAT32(pX[k], psSilk_VAD.NoiseLevelBias[k])
		silk_assert(nrg > 0)

		inv_nrg := silk_DIV32(opus_int32(silk_int32_MAX), nrg)
		silk_assert(inv_nrg >= 0)

		var coef opus_int32
		if nrg > silk_LSHIFT(nl, 3) {
			coef = VAD_NOISE_LEVEL_SMOOTH_COEF_Q16 >> 3
		} else if nrg < nl {
			coef = VAD_NOISE_LEVEL_SMOOTH_COEF_Q16
		} else {
			coef = silk_SMULWB(silk_SMULWW(inv_nrg, nl), VAD_NOISE_LEVEL_SMOOTH_COEF_Q16<<1)
		}
		coef = silk_max_32(coef, min_coef)

		psSilk_VAD.inv_NL[k] = silk_SMLAWB(psSilk_VAD.inv_NL[k], inv_nrg-psSilk_VAD.inv_NL[k], coef)
		silk_assert(psSilk_VAD.inv_NL[k] >= 0)

		nl = silk_DIV32(opus_int32(silk_int32_MAX), psSilk_VAD.inv_NL[k])
		silk_assert(nl >= 0)

		nl = silk_min(nl, 0x00FFFFFF)
		psSilk_VAD.NL[k] = nl
	}
}

// silk_VAD_GetSA_Q8_c — C: VAD.c:82-295.
func silk_VAD_GetSA_Q8_c(psEncC *silk_encoder_state, pIn []opus_int16) opus_int {
	psSilk_VAD := &psEncC.sVAD

	frame_length32 := opus_int32(psEncC.frame_length)

	silk_assert(VAD_N_BANDS == 4)
	celt_assert(MAX_FRAME_LENGTH >= psEncC.frame_length)
	celt_assert(psEncC.frame_length <= 512)
	celt_assert(frame_length32 == 8*silk_RSHIFT(frame_length32, 3))

	decimated_framelength1 := silk_RSHIFT(frame_length32, 1)
	decimated_framelength2 := silk_RSHIFT(frame_length32, 2)
	decimated_framelength := silk_RSHIFT(frame_length32, 3)

	var X_offset [VAD_N_BANDS]opus_int32
	X_offset[0] = 0
	X_offset[1] = decimated_framelength + decimated_framelength2
	X_offset[2] = X_offset[1] + decimated_framelength
	X_offset[3] = X_offset[2] + decimated_framelength2
	X := make([]opus_int16, X_offset[3]+decimated_framelength1)

	silk_ana_filt_bank_1(pIn, psSilk_VAD.AnaState[:], X, X[X_offset[3]:], frame_length32)
	silk_ana_filt_bank_1(X, psSilk_VAD.AnaState1[:], X, X[X_offset[2]:], decimated_framelength1)
	silk_ana_filt_bank_1(X, psSilk_VAD.AnaState2[:], X, X[X_offset[1]:], decimated_framelength2)

	// HP filter on lowest band (differentiator).
	X[decimated_framelength-1] = opus_int16(silk_RSHIFT(opus_int32(X[decimated_framelength-1]), 1))
	HPstateTmp := X[decimated_framelength-1]
	for i := decimated_framelength - 1; i > 0; i-- {
		X[i-1] = opus_int16(silk_RSHIFT(opus_int32(X[i-1]), 1))
		X[i] -= X[i-1]
	}
	X[0] -= psSilk_VAD.HPstate
	psSilk_VAD.HPstate = HPstateTmp

	var Xnrg [VAD_N_BANDS]opus_int32
	var sumSquared opus_int32
	for b := 0; b < VAD_N_BANDS; b++ {
		decimated_framelength = silk_RSHIFT(frame_length32, silk_min_int(VAD_N_BANDS-opus_int(b), VAD_N_BANDS-1))
		dec_subframe_length := silk_RSHIFT(decimated_framelength, VAD_INTERNAL_SUBFRAMES_LOG2)
		dec_subframe_offset := opus_int32(0)

		Xnrg[b] = psSilk_VAD.XnrgSubfr[b]
		for s := 0; s < VAD_INTERNAL_SUBFRAMES; s++ {
			sumSquared = 0
			for i := opus_int32(0); i < dec_subframe_length; i++ {
				x_tmp := silk_RSHIFT(opus_int32(X[X_offset[b]+i+dec_subframe_offset]), 3)
				sumSquared = silk_SMLABB(sumSquared, x_tmp, x_tmp)
				silk_assert(sumSquared >= 0)
			}
			if s < VAD_INTERNAL_SUBFRAMES-1 {
				Xnrg[b] = silk_ADD_POS_SAT32(Xnrg[b], sumSquared)
			} else {
				Xnrg[b] = silk_ADD_POS_SAT32(Xnrg[b], silk_RSHIFT(sumSquared, 1))
			}
			dec_subframe_offset += dec_subframe_length
		}
		psSilk_VAD.XnrgSubfr[b] = sumSquared
	}

	silk_VAD_GetNoiseLevels(Xnrg[:], psSilk_VAD)

	sumSquared = 0
	var input_tilt opus_int32
	var NrgToNoiseRatio_Q8 [VAD_N_BANDS]opus_int32
	for b := 0; b < VAD_N_BANDS; b++ {
		speech_nrg := Xnrg[b] - psSilk_VAD.NL[b]
		if speech_nrg > 0 {
			if (Xnrg[b] & opus_int32(-0x800000)) == 0 {
				NrgToNoiseRatio_Q8[b] = silk_DIV32(silk_LSHIFT(Xnrg[b], 8), psSilk_VAD.NL[b]+1)
			} else {
				NrgToNoiseRatio_Q8[b] = silk_DIV32(Xnrg[b], silk_RSHIFT(psSilk_VAD.NL[b], 8)+1)
			}
			SNR_Q7 := silk_lin2log(NrgToNoiseRatio_Q8[b]) - 8*128
			sumSquared = silk_SMLABB(sumSquared, SNR_Q7, SNR_Q7)

			if speech_nrg < (opus_int32(1) << 20) {
				SNR_Q7 = silk_SMULWB(silk_LSHIFT(silk_SQRT_APPROX(speech_nrg), 6), SNR_Q7)
			}
			input_tilt = silk_SMLAWB(input_tilt, vad_tiltWeights[b], SNR_Q7)
		} else {
			NrgToNoiseRatio_Q8[b] = 256
		}
	}

	sumSquared = silk_DIV32_16(sumSquared, VAD_N_BANDS)

	pSNR_dB_Q7 := opus_int32(opus_int16(3 * silk_SQRT_APPROX(sumSquared)))

	SA_Q15 := silk_sigm_Q15(opus_int(silk_SMULWB(VAD_SNR_FACTOR_Q16, pSNR_dB_Q7)) - VAD_NEGATIVE_OFFSET_Q5)

	psEncC.input_tilt_Q15 = opus_int(silk_LSHIFT(opus_int32(silk_sigm_Q15(opus_int(input_tilt))-16384), 1))

	speech_nrg := opus_int32(0)
	for b := 0; b < VAD_N_BANDS; b++ {
		speech_nrg += opus_int32(b+1) * silk_RSHIFT(Xnrg[b]-psSilk_VAD.NL[b], 4)
	}
	if psEncC.frame_length == 20*psEncC.fs_kHz {
		speech_nrg = silk_RSHIFT32(speech_nrg, 1)
	}
	if speech_nrg <= 0 {
		SA_Q15 = opus_int(silk_RSHIFT(opus_int32(SA_Q15), 1))
	} else if speech_nrg < 16384 {
		speech_nrg = silk_LSHIFT32(speech_nrg, 16)
		speech_nrg = silk_SQRT_APPROX(speech_nrg)
		SA_Q15 = opus_int(silk_SMULWB(32768+speech_nrg, opus_int32(SA_Q15)))
	}
	psEncC.speech_activity_Q8 = silk_min_int(opus_int(silk_RSHIFT(opus_int32(SA_Q15), 7)), opus_int(silk_uint8_MAX))

	smooth_coef_Q16 := silk_SMULWB(VAD_SNR_SMOOTH_COEF_Q18, silk_SMULWB(opus_int32(SA_Q15), opus_int32(SA_Q15)))
	if psEncC.frame_length == 10*psEncC.fs_kHz {
		smooth_coef_Q16 >>= 1
	}
	for b := 0; b < VAD_N_BANDS; b++ {
		psSilk_VAD.NrgRatioSmth_Q8[b] = silk_SMLAWB(psSilk_VAD.NrgRatioSmth_Q8[b],
			NrgToNoiseRatio_Q8[b]-psSilk_VAD.NrgRatioSmth_Q8[b], smooth_coef_Q16)
		SNR_Q7 := 3 * (silk_lin2log(psSilk_VAD.NrgRatioSmth_Q8[b]) - 8*128)
		psEncC.input_quality_bands_Q15[b] = silk_sigm_Q15(opus_int(silk_RSHIFT(SNR_Q7-16*128, 4)))
	}

	return 0
}
