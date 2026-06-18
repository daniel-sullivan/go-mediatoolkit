package nativeopus

// Port of libopus/silk/bwexpander_32.c.

// silk_bwexpander_32 — Chirp (bandwidth expand) an LP AR filter in
// place. Input without leading 1. chirp_Q16 is in Q16. This logic is
// reused in _celt_lpc(); keep bug fixes in sync.
func silk_bwexpander_32(ar []opus_int32, d opus_int, chirp_Q16 opus_int32) {
	chirp_minus_one_Q16 := chirp_Q16 - 65536

	for i := opus_int(0); i < d-1; i++ {
		ar[i] = silk_SMULWW(chirp_Q16, ar[i])
		chirp_Q16 += silk_RSHIFT_ROUND(silk_MUL(chirp_Q16, chirp_minus_one_Q16), 16)
	}
	ar[d-1] = silk_SMULWW(chirp_Q16, ar[d-1])
}
