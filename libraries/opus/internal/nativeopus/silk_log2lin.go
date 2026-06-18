package nativeopus

// Port of libopus/silk/log2lin.c.

// silk_log2lin — Approximation of 2^() (very close inverse of
// silk_lin2log()). Converts a Q7 log-scale input to linear scale.
func silk_log2lin(inLog_Q7 opus_int32) opus_int32 {
	if inLog_Q7 < 0 {
		return 0
	} else if inLog_Q7 >= 3967 {
		return silk_int32_MAX
	}

	out := silk_LSHIFT(1, opus_int(silk_RSHIFT(inLog_Q7, 7)))
	frac_Q7 := inLog_Q7 & 0x7F
	if inLog_Q7 < 2048 {
		// Piece-wise parabolic approximation.
		out = silk_ADD_RSHIFT32(out,
			silk_MUL(out, silk_SMLAWB(frac_Q7, silk_SMULBB(frac_Q7, 128-frac_Q7), -174)), 7)
	} else {
		// Piece-wise parabolic approximation.
		out = silk_MLA(out, silk_RSHIFT(out, 7),
			silk_SMLAWB(frac_Q7, silk_SMULBB(frac_Q7, 128-frac_Q7), -174))
	}
	return out
}
