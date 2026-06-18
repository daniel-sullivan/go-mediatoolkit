package nativeopus

// Port of libopus/silk/lin2log.c.

// silk_lin2log — Approximation of 128 * log2() (very close inverse of
// silk_log2lin()). Converts a linear-scale input to a log scale.
func silk_lin2log(inLin opus_int32) opus_int32 {
	var lz, frac_Q7 opus_int32
	silk_CLZ_FRAC(inLin, &lz, &frac_Q7)
	// Piece-wise parabolic approximation:
	// (frac_Q7 + ((frac_Q7*(128-frac_Q7) * 179) >> 16)) + ((31-lz) << 7)
	return silk_ADD_LSHIFT32(
		silk_SMLAWB(frac_Q7, silk_MUL(frac_Q7, 128-frac_Q7), 179),
		31-lz, 7)
}
