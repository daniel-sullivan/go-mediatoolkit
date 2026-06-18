package nativeopus

// Port of libopus/silk/HP_variable_cutoff.c.
//
// Adaptive HP filter cutoff tracking based on pitch lag statistics.
// Called once per packet on the multi-channel silk_encoder_state_FLP
// array. Only the first channel's sCmn state participates — this
// matches the C behavior where the function takes a single top-level
// state_Fxx[] pointer.

// silk_HP_variable_cutoff — C: HP_variable_cutoff.c:39-77.
func silk_HP_variable_cutoff(state_Fxx []silk_encoder_state_FLP) {
	psEncC1 := &state_Fxx[0].sCmn

	if psEncC1.prevSignalType == TYPE_VOICED {
		// Pitch frequency in Hz (Q16).
		pitch_freq_Hz_Q16 := silk_DIV32_16(
			silk_LSHIFT(silk_MUL(opus_int32(psEncC1.fs_kHz), 1000), 16),
			opus_int32(psEncC1.prevLag))
		pitch_freq_log_Q7 := silk_lin2log(pitch_freq_Hz_Q16) - (16 << 7)

		// Quality adjustment.
		quality_Q15 := opus_int32(psEncC1.input_quality_bands_Q15[0])
		pitch_freq_log_Q7 = silk_SMLAWB(pitch_freq_log_Q7,
			silk_SMULWB(silk_LSHIFT(-quality_Q15, 2), quality_Q15),
			pitch_freq_log_Q7-(silk_lin2log(SILK_FIX_CONST(VARIABLE_HP_MIN_CUTOFF_HZ, 16))-(16<<7)))

		delta_freq_Q7 := pitch_freq_log_Q7 - silk_RSHIFT(psEncC1.variable_HP_smth1_Q15, 8)
		if delta_freq_Q7 < 0 {
			// Less smoothing for decreasing pitch frequency.
			delta_freq_Q7 = silk_MUL(delta_freq_Q7, 3)
		}

		delta_freq_Q7 = silk_LIMIT_32(delta_freq_Q7,
			-SILK_FIX_CONST(VARIABLE_HP_MAX_DELTA_FREQ, 7),
			SILK_FIX_CONST(VARIABLE_HP_MAX_DELTA_FREQ, 7))

		psEncC1.variable_HP_smth1_Q15 = silk_SMLAWB(psEncC1.variable_HP_smth1_Q15,
			silk_SMULBB(opus_int32(psEncC1.speech_activity_Q8), delta_freq_Q7),
			SILK_FIX_CONST(VARIABLE_HP_SMTH_COEF1, 16))

		psEncC1.variable_HP_smth1_Q15 = silk_LIMIT_32(psEncC1.variable_HP_smth1_Q15,
			silk_LSHIFT(silk_lin2log(VARIABLE_HP_MIN_CUTOFF_HZ), 8),
			silk_LSHIFT(silk_lin2log(VARIABLE_HP_MAX_CUTOFF_HZ), 8))
	}
}
