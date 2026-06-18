package nativeopus

// Port of libopus/silk/resampler_down2.c.

// silk_resampler_down2 — Downsample by 2, mediocre quality.
func silk_resampler_down2(S []opus_int32, out []opus_int16, in_ []opus_int16, inLen opus_int32) {
	len2 := silk_RSHIFT32(inLen, 1)
	celt_assert(silk_resampler_down2_0 > 0)
	celt_assert(silk_resampler_down2_1 < 0)
	for k := opus_int32(0); k < len2; k++ {
		// Convert to Q10.
		in32 := silk_LSHIFT(opus_int32(in_[2*k]), 10)
		Y := silk_SUB32(in32, S[0])
		X := silk_SMLAWB(Y, Y, opus_int32(silk_resampler_down2_1))
		out32 := silk_ADD32(S[0], X)
		S[0] = silk_ADD32(in32, X)

		in32 = silk_LSHIFT(opus_int32(in_[2*k+1]), 10)
		Y = silk_SUB32(in32, S[1])
		X = silk_SMULWB(Y, opus_int32(silk_resampler_down2_0))
		out32 = silk_ADD32(out32, S[1])
		out32 = silk_ADD32(out32, X)
		S[1] = silk_ADD32(in32, X)

		out[k] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(out32, 11)))
	}
}
