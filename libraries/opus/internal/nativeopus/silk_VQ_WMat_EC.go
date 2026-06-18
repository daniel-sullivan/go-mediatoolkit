package nativeopus

// Port of libopus/silk/VQ_WMat_EC.c.
//
// Entropy-constrained matrix-weighted vector quantizer hard-coded to
// LTP_ORDER (5)-element vectors. Scans a codebook and selects the
// codeword minimizing a rate-distortion metric.

// silk_VQ_WMat_EC_c — C: VQ_WMat_EC.c:35-131.
func silk_VQ_WMat_EC_c(
	ind *opus_int8,
	res_nrg_Q15 *opus_int32,
	rate_dist_Q8 *opus_int32,
	gain_Q7 *opus_int,
	XX_Q17 []opus_int32,
	xX_Q17 []opus_int32,
	cb_Q7 []opus_int8,
	cb_gain_Q7 []opus_uint8,
	cl_Q5 []opus_uint8,
	subfr_len opus_int,
	max_gain_Q7 opus_int32,
	L opus_int,
) {
	var neg_xX_Q24 [5]opus_int32
	neg_xX_Q24[0] = -silk_LSHIFT32(xX_Q17[0], 7)
	neg_xX_Q24[1] = -silk_LSHIFT32(xX_Q17[1], 7)
	neg_xX_Q24[2] = -silk_LSHIFT32(xX_Q17[2], 7)
	neg_xX_Q24[3] = -silk_LSHIFT32(xX_Q17[3], 7)
	neg_xX_Q24[4] = -silk_LSHIFT32(xX_Q17[4], 7)

	*rate_dist_Q8 = opus_int32(silk_int32_MAX)
	*res_nrg_Q15 = opus_int32(silk_int32_MAX)
	*ind = 0

	cb_off := 0
	for k := opus_int(0); k < L; k++ {
		gain_tmp_Q7 := opus_int32(cb_gain_Q7[k])
		cb_row_Q7 := cb_Q7[cb_off : cb_off+LTP_ORDER]

		sum1_Q15 := SILK_FIX_CONST(1.001, 15)
		penalty := silk_LSHIFT32(silk_max(silk_SUB32(gain_tmp_Q7, max_gain_Q7), 0), 11)

		// first row
		sum2_Q24 := silk_MLA(neg_xX_Q24[0], XX_Q17[1], opus_int32(cb_row_Q7[1]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[2], opus_int32(cb_row_Q7[2]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[3], opus_int32(cb_row_Q7[3]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[4], opus_int32(cb_row_Q7[4]))
		sum2_Q24 = silk_LSHIFT32(sum2_Q24, 1)
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[0], opus_int32(cb_row_Q7[0]))
		sum1_Q15 = silk_SMLAWB(sum1_Q15, sum2_Q24, opus_int32(cb_row_Q7[0]))

		// second row
		sum2_Q24 = silk_MLA(neg_xX_Q24[1], XX_Q17[7], opus_int32(cb_row_Q7[2]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[8], opus_int32(cb_row_Q7[3]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[9], opus_int32(cb_row_Q7[4]))
		sum2_Q24 = silk_LSHIFT32(sum2_Q24, 1)
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[6], opus_int32(cb_row_Q7[1]))
		sum1_Q15 = silk_SMLAWB(sum1_Q15, sum2_Q24, opus_int32(cb_row_Q7[1]))

		// third row
		sum2_Q24 = silk_MLA(neg_xX_Q24[2], XX_Q17[13], opus_int32(cb_row_Q7[3]))
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[14], opus_int32(cb_row_Q7[4]))
		sum2_Q24 = silk_LSHIFT32(sum2_Q24, 1)
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[12], opus_int32(cb_row_Q7[2]))
		sum1_Q15 = silk_SMLAWB(sum1_Q15, sum2_Q24, opus_int32(cb_row_Q7[2]))

		// fourth row
		sum2_Q24 = silk_MLA(neg_xX_Q24[3], XX_Q17[19], opus_int32(cb_row_Q7[4]))
		sum2_Q24 = silk_LSHIFT32(sum2_Q24, 1)
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[18], opus_int32(cb_row_Q7[3]))
		sum1_Q15 = silk_SMLAWB(sum1_Q15, sum2_Q24, opus_int32(cb_row_Q7[3]))

		// last row
		sum2_Q24 = silk_LSHIFT32(neg_xX_Q24[4], 1)
		sum2_Q24 = silk_MLA(sum2_Q24, XX_Q17[24], opus_int32(cb_row_Q7[4]))
		sum1_Q15 = silk_SMLAWB(sum1_Q15, sum2_Q24, opus_int32(cb_row_Q7[4]))

		if sum1_Q15 >= 0 {
			// High-rate assumption: 6 dB -> 1 bit/sample.
			bits_res_Q8 := silk_SMULBB(opus_int32(subfr_len), silk_lin2log(sum1_Q15+penalty)-(15<<7))
			// Reduce codelength component by half.
			bits_tot_Q8 := silk_ADD_LSHIFT32(bits_res_Q8, opus_int32(cl_Q5[k]), 3-1)
			if bits_tot_Q8 <= *rate_dist_Q8 {
				*rate_dist_Q8 = bits_tot_Q8
				*res_nrg_Q15 = sum1_Q15 + penalty
				*ind = opus_int8(k)
				*gain_Q7 = opus_int(gain_tmp_Q7)
			}
		}

		cb_off += LTP_ORDER
	}
}
