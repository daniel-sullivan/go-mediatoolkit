package nativeopus

// Port of libopus/silk/resampler.c.

// Tables with delay-compensation values to equalize total delay for
// different modes.
var silk_resampler_delay_matrix_enc = [6][3]opus_int8{
	// in \ out   8   12   16
	/*  8 */ {6, 0, 3},
	/* 12 */ {0, 7, 3},
	/* 16 */ {0, 1, 10},
	/* 24 */ {0, 2, 6},
	/* 48 */ {18, 10, 12},
	/* 96 */ {0, 0, 44},
}

var silk_resampler_delay_matrix_dec = [3][6]opus_int8{
	// in \ out   8   12   16   24   48   96
	/*  8 */ {4, 0, 2, 0, 0, 0},
	/* 12 */ {0, 9, 4, 7, 4, 4},
	/* 16 */ {0, 3, 12, 7, 7, 7},
}

// rateID — map [8000, 12000, 16000, 24000, 48000] to [0, 1, 2, 3, 4].
func silk_resampler_rateID(R opus_int32) opus_int {
	gt16 := opus_int32(0)
	if R > 16000 {
		gt16 = 1
	}
	gt24 := opus_int32(0)
	if R > 24000 {
		gt24 = 1
	}
	v := (((R >> 12) - gt16) >> gt24) - 1
	if v > 5 {
		v = 5
	}
	return opus_int(v)
}

// silk_resampler_init — Initialize/reset the resampler state.
func silk_resampler_init(S *silk_resampler_state_struct, Fs_Hz_in, Fs_Hz_out opus_int32, forEnc opus_int) opus_int {
	// Clear state.
	*S = silk_resampler_state_struct{}

	if forEnc != 0 {
		if (Fs_Hz_in != 8000 && Fs_Hz_in != 12000 && Fs_Hz_in != 16000 &&
			Fs_Hz_in != 24000 && Fs_Hz_in != 48000) ||
			(Fs_Hz_out != 8000 && Fs_Hz_out != 12000 && Fs_Hz_out != 16000) {
			celt_assert(false)
			return -1
		}
		S.inputDelay = opus_int(silk_resampler_delay_matrix_enc[silk_resampler_rateID(Fs_Hz_in)][silk_resampler_rateID(Fs_Hz_out)])
	} else {
		if (Fs_Hz_in != 8000 && Fs_Hz_in != 12000 && Fs_Hz_in != 16000) ||
			(Fs_Hz_out != 8000 && Fs_Hz_out != 12000 && Fs_Hz_out != 16000 &&
				Fs_Hz_out != 24000 && Fs_Hz_out != 48000) {
			celt_assert(false)
			return -1
		}
		S.inputDelay = opus_int(silk_resampler_delay_matrix_dec[silk_resampler_rateID(Fs_Hz_in)][silk_resampler_rateID(Fs_Hz_out)])
	}

	S.Fs_in_kHz = opus_int(silk_DIV32_16(Fs_Hz_in, 1000))
	S.Fs_out_kHz = opus_int(silk_DIV32_16(Fs_Hz_out, 1000))
	S.batchSize = S.Fs_in_kHz * RESAMPLER_MAX_BATCH_SIZE_MS

	up2x := opus_int(0)
	switch {
	case Fs_Hz_out > Fs_Hz_in:
		if Fs_Hz_out == silk_MUL(Fs_Hz_in, 2) {
			S.resampler_function = USE_silk_resampler_private_up2_HQ_wrapper
		} else {
			S.resampler_function = USE_silk_resampler_private_IIR_FIR
			up2x = 1
		}
	case Fs_Hz_out < Fs_Hz_in:
		S.resampler_function = USE_silk_resampler_private_down_FIR
		switch {
		case silk_MUL(Fs_Hz_out, 4) == silk_MUL(Fs_Hz_in, 3):
			S.FIR_Fracs = 3
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR0
			S.Coefs = silk_Resampler_3_4_COEFS[:]
		case silk_MUL(Fs_Hz_out, 3) == silk_MUL(Fs_Hz_in, 2):
			S.FIR_Fracs = 2
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR0
			S.Coefs = silk_Resampler_2_3_COEFS[:]
		case silk_MUL(Fs_Hz_out, 2) == Fs_Hz_in:
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR1
			S.Coefs = silk_Resampler_1_2_COEFS[:]
		case silk_MUL(Fs_Hz_out, 3) == Fs_Hz_in:
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_3_COEFS[:]
		case silk_MUL(Fs_Hz_out, 4) == Fs_Hz_in:
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_4_COEFS[:]
		case silk_MUL(Fs_Hz_out, 6) == Fs_Hz_in:
			S.FIR_Fracs = 1
			S.FIR_Order = RESAMPLER_DOWN_ORDER_FIR2
			S.Coefs = silk_Resampler_1_6_COEFS[:]
		default:
			celt_assert(false)
			return -1
		}
	default:
		S.resampler_function = USE_silk_resampler_copy
	}

	S.invRatio_Q16 = silk_LSHIFT32(silk_DIV32(silk_LSHIFT32(Fs_Hz_in, 14+up2x), Fs_Hz_out), 2)
	for silk_SMULWW(S.invRatio_Q16, Fs_Hz_out) < silk_LSHIFT32(Fs_Hz_in, up2x) {
		S.invRatio_Q16++
	}
	return 0
}

// silk_resampler — convert from one sampling rate to another.
func silk_resampler(S *silk_resampler_state_struct, out []opus_int16,
	in_ []opus_int16, inLen opus_int32) opus_int {

	celt_assert(inLen >= opus_int32(S.Fs_in_kHz))
	celt_assert(S.inputDelay <= S.Fs_in_kHz)

	nSamples := S.Fs_in_kHz - S.inputDelay

	copy(S.delayBuf[S.inputDelay:S.inputDelay+nSamples], in_[:nSamples])

	switch S.resampler_function {
	case USE_silk_resampler_private_up2_HQ_wrapper:
		silk_resampler_private_up2_HQ_wrapper(S, out, S.delayBuf[:], opus_int32(S.Fs_in_kHz))
		silk_resampler_private_up2_HQ_wrapper(S, out[S.Fs_out_kHz:], in_[nSamples:], inLen-opus_int32(S.Fs_in_kHz))
	case USE_silk_resampler_private_IIR_FIR:
		silk_resampler_private_IIR_FIR(S, out, S.delayBuf[:], opus_int32(S.Fs_in_kHz))
		silk_resampler_private_IIR_FIR(S, out[S.Fs_out_kHz:], in_[nSamples:], inLen-opus_int32(S.Fs_in_kHz))
	case USE_silk_resampler_private_down_FIR:
		silk_resampler_private_down_FIR(S, out, S.delayBuf[:], opus_int32(S.Fs_in_kHz))
		silk_resampler_private_down_FIR(S, out[S.Fs_out_kHz:], in_[nSamples:], inLen-opus_int32(S.Fs_in_kHz))
	default:
		copy(out[:S.Fs_in_kHz], S.delayBuf[:S.Fs_in_kHz])
		copy(out[S.Fs_out_kHz:S.Fs_out_kHz+opus_int(inLen)-S.Fs_in_kHz],
			in_[nSamples:nSamples+opus_int(inLen)-S.Fs_in_kHz])
	}

	copy(S.delayBuf[:S.inputDelay], in_[opus_int(inLen)-S.inputDelay:opus_int(inLen)])
	return 0
}
