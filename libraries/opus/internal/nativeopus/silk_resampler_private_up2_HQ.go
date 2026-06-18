package nativeopus

// Port of libopus/silk/resampler_private_up2_HQ.c.

// silk_resampler_private_up2_HQ — Upsample by 2, high quality. Uses
// 2nd-order allpass filters for the 2x upsampling followed by a notch
// filter just above Nyquist.
func silk_resampler_private_up2_HQ(S []opus_int32, out []opus_int16, in_ []opus_int16, length opus_int32) {
	silk_assert(silk_resampler_up2_hq_0[0] > 0)
	silk_assert(silk_resampler_up2_hq_0[1] > 0)
	silk_assert(silk_resampler_up2_hq_0[2] < 0)
	silk_assert(silk_resampler_up2_hq_1[0] > 0)
	silk_assert(silk_resampler_up2_hq_1[1] > 0)
	silk_assert(silk_resampler_up2_hq_1[2] < 0)

	for k := opus_int32(0); k < length; k++ {
		in32 := silk_LSHIFT(opus_int32(in_[k]), 10)

		// Even sample: three all-pass sections.
		Y := silk_SUB32(in32, S[0])
		X := silk_SMULWB(Y, opus_int32(silk_resampler_up2_hq_0[0]))
		out32_1 := silk_ADD32(S[0], X)
		S[0] = silk_ADD32(in32, X)

		Y = silk_SUB32(out32_1, S[1])
		X = silk_SMULWB(Y, opus_int32(silk_resampler_up2_hq_0[1]))
		out32_2 := silk_ADD32(S[1], X)
		S[1] = silk_ADD32(out32_1, X)

		Y = silk_SUB32(out32_2, S[2])
		X = silk_SMLAWB(Y, Y, opus_int32(silk_resampler_up2_hq_0[2]))
		out32_1 = silk_ADD32(S[2], X)
		S[2] = silk_ADD32(out32_2, X)

		out[2*k] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(out32_1, 10)))

		// Odd sample.
		Y = silk_SUB32(in32, S[3])
		X = silk_SMULWB(Y, opus_int32(silk_resampler_up2_hq_1[0]))
		out32_1 = silk_ADD32(S[3], X)
		S[3] = silk_ADD32(in32, X)

		Y = silk_SUB32(out32_1, S[4])
		X = silk_SMULWB(Y, opus_int32(silk_resampler_up2_hq_1[1]))
		out32_2 = silk_ADD32(S[4], X)
		S[4] = silk_ADD32(out32_1, X)

		Y = silk_SUB32(out32_2, S[5])
		X = silk_SMLAWB(Y, Y, opus_int32(silk_resampler_up2_hq_1[2]))
		out32_1 = silk_ADD32(S[5], X)
		S[5] = silk_ADD32(out32_2, X)

		out[2*k+1] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(out32_1, 10)))
	}
}

// silk_resampler_private_up2_HQ_wrapper — state wrapper.
func silk_resampler_private_up2_HQ_wrapper(SS *silk_resampler_state_struct,
	out []opus_int16, in_ []opus_int16, length opus_int32) {
	silk_resampler_private_up2_HQ(SS.sIIR[:], out, in_, length)
}
