package nativeopus

// Port of libopus/silk/ana_filt_bank_1.c.
//
// Two-band analysis filter based on first-order allpass filters.

// Coefficients for 2-band filter bank based on first-order allpass
// filters. A_fb1_21 = (opus_int16)(20623 << 1) with wrap = -24290.
const (
	silk_A_fb1_20 = opus_int16(5394 << 1)
	silk_A_fb1_21 = opus_int16(-24290)
)

// silk_ana_filt_bank_1 — Split signal in two decimated bands using
// first-order allpass filters.
func silk_ana_filt_bank_1(in_ []opus_int16, S []opus_int32,
	outL, outH []opus_int16, N opus_int32) {
	N2 := silk_RSHIFT(N, 1)

	// Internal variables and state are in Q10 format.
	for k := opus_int32(0); k < N2; k++ {
		// Convert to Q10.
		in32 := silk_LSHIFT(opus_int32(in_[2*k]), 10)

		// All-pass section for even input sample.
		Y := silk_SUB32(in32, S[0])
		X := silk_SMLAWB(Y, Y, opus_int32(silk_A_fb1_21))
		out_1 := silk_ADD32(S[0], X)
		S[0] = silk_ADD32(in32, X)

		// Convert to Q10.
		in32 = silk_LSHIFT(opus_int32(in_[2*k+1]), 10)

		// All-pass section for odd input sample and add to previous.
		Y = silk_SUB32(in32, S[1])
		X = silk_SMULWB(Y, opus_int32(silk_A_fb1_20))
		out_2 := silk_ADD32(S[1], X)
		S[1] = silk_ADD32(in32, X)

		outL[k] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(silk_ADD32(out_2, out_1), 11)))
		outH[k] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(silk_SUB32(out_2, out_1), 11)))
	}
}
