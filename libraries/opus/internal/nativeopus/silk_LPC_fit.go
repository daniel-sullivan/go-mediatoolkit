package nativeopus

// Port of libopus/silk/LPC_fit.c.

// silk_LPC_fit — Convert int32 coefficients to int16 coefs and make
// sure there's no wrap-around. This logic is reused in _celt_lpc();
// keep bug fixes in sync.
func silk_LPC_fit(a_QOUT []opus_int16, a_QIN []opus_int32, QOUT, QIN, d opus_int) {
	var i, k, idx opus_int
	var maxabs, absval, chirp_Q16 opus_int32

	// Limit the maximum absolute value of the prediction coefficients
	// so that they fit in int16.
	for i = 0; i < 10; i++ {
		// Find maximum absolute value and its index.
		maxabs = 0
		for k = 0; k < d; k++ {
			absval = silk_abs(a_QIN[k])
			if absval > maxabs {
				maxabs = absval
				idx = k
			}
		}
		maxabs = silk_RSHIFT_ROUND(maxabs, QIN-QOUT)

		if maxabs > opus_int32(silk_int16_MAX) {
			// Reduce magnitude of prediction coefficients.
			// (silk_int32_MAX >> 14) + silk_int16_MAX = 163838.
			maxabs = silk_min(maxabs, 163838)
			chirp_Q16 = SILK_FIX_CONST(0.999, 16) - silk_DIV32(
				silk_LSHIFT(maxabs-opus_int32(silk_int16_MAX), 14),
				silk_RSHIFT32(silk_MUL(maxabs, opus_int32(idx)+1), 2))
			silk_bwexpander_32(a_QIN, d, chirp_Q16)
		} else {
			break
		}
	}

	if i == 10 {
		// Reached the last iteration, clip the coefficients.
		for k = 0; k < d; k++ {
			a_QOUT[k] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(a_QIN[k], QIN-QOUT)))
			a_QIN[k] = silk_LSHIFT(opus_int32(a_QOUT[k]), QIN-QOUT)
		}
	} else {
		for k = 0; k < d; k++ {
			a_QOUT[k] = opus_int16(silk_RSHIFT_ROUND(a_QIN[k], QIN-QOUT))
		}
	}
}
