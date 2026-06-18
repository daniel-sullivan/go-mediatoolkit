package nativeopus

// Port of libopus/silk/bwexpander.c.

// silk_bwexpander — Chirp (bandwidth expand) an LP AR filter in place.
// Input without leading 1. chirp_Q16 is typically in [0, 1].
func silk_bwexpander(ar []opus_int16, d opus_int, chirp_Q16 opus_int32) {
	chirp_minus_one_Q16 := chirp_Q16 - 65536

	// NB: Don't use silk_SMULWB in place of silk_RSHIFT_ROUND(silk_MUL(),16)
	// below — bias in silk_SMULWB can lead to unstable filters.
	for i := opus_int(0); i < d-1; i++ {
		ar[i] = opus_int16(silk_RSHIFT_ROUND(silk_MUL(chirp_Q16, opus_int32(ar[i])), 16))
		chirp_Q16 += silk_RSHIFT_ROUND(silk_MUL(chirp_Q16, chirp_minus_one_Q16), 16)
	}
	ar[d-1] = opus_int16(silk_RSHIFT_ROUND(silk_MUL(chirp_Q16, opus_int32(ar[d-1])), 16))
}
