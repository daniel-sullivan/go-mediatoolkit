package nativeopus

// Port of libopus/silk/resampler_private_AR2.c.

// silk_resampler_private_AR2 — Second-order AR filter with single
// delay elements.
func silk_resampler_private_AR2(S, out_Q8 []opus_int32, in_ []opus_int16,
	A_Q14 []opus_int16, len_ opus_int32) {
	var out32 opus_int32
	for k := opus_int32(0); k < len_; k++ {
		out32 = silk_ADD_LSHIFT32(S[0], opus_int32(in_[k]), 8)
		out_Q8[k] = out32
		out32 = silk_LSHIFT(out32, 2)
		S[0] = silk_SMLAWB(S[1], out32, opus_int32(A_Q14[0]))
		S[1] = silk_SMULWB(out32, opus_int32(A_Q14[1]))
	}
}
