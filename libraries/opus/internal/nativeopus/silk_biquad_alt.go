package nativeopus

// Port of libopus/silk/biquad_alt.c — second-order ARMA filter,
// alternative implementation that can handle slowly varying filter
// coefficients. DIRECT FORM II TRANSPOSED with a 2-element state.

// silk_biquad_alt_stride1 — mono biquad.
func silk_biquad_alt_stride1(in_ []opus_int16, B_Q28, A_Q28 []opus_int32,
	S []opus_int32, out []opus_int16, len_ opus_int32) {
	// Negate A_Q28 values and split into upper/lower parts.
	A0_L_Q28 := (-A_Q28[0]) & 0x00003FFF
	A0_U_Q28 := silk_RSHIFT(-A_Q28[0], 14)
	A1_L_Q28 := (-A_Q28[1]) & 0x00003FFF
	A1_U_Q28 := silk_RSHIFT(-A_Q28[1], 14)

	for k := opus_int32(0); k < len_; k++ {
		inval := opus_int32(in_[k])
		out32_Q14 := silk_LSHIFT(silk_SMLAWB(S[0], B_Q28[0], inval), 2)

		S[0] = S[1] + silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14, A0_L_Q28), 14)
		S[0] = silk_SMLAWB(S[0], out32_Q14, A0_U_Q28)
		S[0] = silk_SMLAWB(S[0], B_Q28[1], inval)

		S[1] = silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14, A1_L_Q28), 14)
		S[1] = silk_SMLAWB(S[1], out32_Q14, A1_U_Q28)
		S[1] = silk_SMLAWB(S[1], B_Q28[2], inval)

		out[k] = opus_int16(silk_SAT16(silk_RSHIFT(out32_Q14+(1<<14)-1, 14)))
	}
}

// silk_biquad_alt_stride2_c — stereo-interleaved biquad.
func silk_biquad_alt_stride2_c(in_ []opus_int16, B_Q28, A_Q28 []opus_int32,
	S []opus_int32, out []opus_int16, len_ opus_int32) {
	A0_L_Q28 := (-A_Q28[0]) & 0x00003FFF
	A0_U_Q28 := silk_RSHIFT(-A_Q28[0], 14)
	A1_L_Q28 := (-A_Q28[1]) & 0x00003FFF
	A1_U_Q28 := silk_RSHIFT(-A_Q28[1], 14)

	var out32_Q14 [2]opus_int32
	for k := opus_int32(0); k < len_; k++ {
		out32_Q14[0] = silk_LSHIFT(silk_SMLAWB(S[0], B_Q28[0], opus_int32(in_[2*k+0])), 2)
		out32_Q14[1] = silk_LSHIFT(silk_SMLAWB(S[2], B_Q28[0], opus_int32(in_[2*k+1])), 2)

		S[0] = S[1] + silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14[0], A0_L_Q28), 14)
		S[2] = S[3] + silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14[1], A0_L_Q28), 14)
		S[0] = silk_SMLAWB(S[0], out32_Q14[0], A0_U_Q28)
		S[2] = silk_SMLAWB(S[2], out32_Q14[1], A0_U_Q28)
		S[0] = silk_SMLAWB(S[0], B_Q28[1], opus_int32(in_[2*k+0]))
		S[2] = silk_SMLAWB(S[2], B_Q28[1], opus_int32(in_[2*k+1]))

		S[1] = silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14[0], A1_L_Q28), 14)
		S[3] = silk_RSHIFT_ROUND(silk_SMULWB(out32_Q14[1], A1_L_Q28), 14)
		S[1] = silk_SMLAWB(S[1], out32_Q14[0], A1_U_Q28)
		S[3] = silk_SMLAWB(S[3], out32_Q14[1], A1_U_Q28)
		S[1] = silk_SMLAWB(S[1], B_Q28[2], opus_int32(in_[2*k+0]))
		S[3] = silk_SMLAWB(S[3], B_Q28[2], opus_int32(in_[2*k+1]))

		out[2*k+0] = opus_int16(silk_SAT16(silk_RSHIFT(out32_Q14[0]+(1<<14)-1, 14)))
		out[2*k+1] = opus_int16(silk_SAT16(silk_RSHIFT(out32_Q14[1]+(1<<14)-1, 14)))
	}
}
