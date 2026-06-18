package nativeopus

// Port of libopus/silk/quant_LTP_gains.c.
//
// Scan the three LTP gain codebooks, choose the best per-subframe
// entry in each, and pick the codebook whose total rate-distortion
// metric is smallest.

// silk_quant_LTP_gains — C: quant_LTP_gains.c:35-132.
func silk_quant_LTP_gains(
	B_Q14 []opus_int16, // [MAX_NB_SUBFR * LTP_ORDER]
	cbk_index []opus_int8, // [MAX_NB_SUBFR]
	periodicity_index *opus_int8,
	sum_log_gain_Q7 *opus_int32,
	pred_gain_dB_Q7 *opus_int,
	XX_Q17 []opus_int32, // [MAX_NB_SUBFR*LTP_ORDER*LTP_ORDER]
	xX_Q17 []opus_int32, // [MAX_NB_SUBFR*LTP_ORDER]
	subfr_len opus_int,
	nb_subfr opus_int,
	arch int,
) {
	_ = arch
	var temp_idx [MAX_NB_SUBFR]opus_int8
	var res_nrg_Q15, rate_dist_Q7 opus_int32
	// Mirror C: gain_Q7 declared once at function scope so stale values
	// persist across subframe iterations when silk_VQ_WMat_EC_c fails
	// to pick any valid candidate (uninitialized-local stand-in).
	var gain_Q7 opus_int

	min_rate_dist_Q7 := opus_int32(silk_int32_MAX)
	best_sum_log_gain_Q7 := opus_int32(0)

	for k := opus_int(0); k < 3; k++ {
		gain_safety := SILK_FIX_CONST(0.4, 7)

		cl_ptr_Q5 := silk_LTP_gain_BITS_Q5_ptrs[k]
		cbk_ptr_Q7 := silk_LTP_vq_ptrs_Q7[k]
		cbk_gain_ptr_Q7 := silk_LTP_vq_gain_ptrs_Q7[k]
		cbk_size := opus_int(silk_LTP_vq_sizes[k])

		XX_off := 0
		xX_off := 0
		res_nrg_Q15 = 0
		rate_dist_Q7 = 0
		sum_log_gain_tmp_Q7 := *sum_log_gain_Q7

		for j := opus_int(0); j < nb_subfr; j++ {
			max_gain_Q7 := silk_log2lin(
				(SILK_FIX_CONST(MAX_SUM_LOG_GAIN_DB/6.0, 7)-sum_log_gain_tmp_Q7)+
					SILK_FIX_CONST(7, 7)) - gain_safety

			var res_nrg_Q15_subfr, rate_dist_Q7_subfr opus_int32

			silk_VQ_WMat_EC_c(
				&temp_idx[j],
				&res_nrg_Q15_subfr,
				&rate_dist_Q7_subfr,
				&gain_Q7,
				XX_Q17[XX_off:],
				xX_Q17[xX_off:],
				cbk_ptr_Q7,
				cbk_gain_ptr_Q7,
				cl_ptr_Q5,
				subfr_len,
				max_gain_Q7,
				cbk_size,
			)

			res_nrg_Q15 = silk_ADD_POS_SAT32(res_nrg_Q15, res_nrg_Q15_subfr)
			rate_dist_Q7 = silk_ADD_POS_SAT32(rate_dist_Q7, rate_dist_Q7_subfr)
			sum_log_gain_tmp_Q7 = silk_max(0, sum_log_gain_tmp_Q7+
				silk_lin2log(gain_safety+opus_int32(gain_Q7))-SILK_FIX_CONST(7, 7))

			XX_off += LTP_ORDER * LTP_ORDER
			xX_off += LTP_ORDER
		}

		if rate_dist_Q7 <= min_rate_dist_Q7 {
			min_rate_dist_Q7 = rate_dist_Q7
			*periodicity_index = opus_int8(k)
			copy(cbk_index[:nb_subfr], temp_idx[:nb_subfr])
			best_sum_log_gain_Q7 = sum_log_gain_tmp_Q7
		}
	}

	cbk_ptr_Q7 := silk_LTP_vq_ptrs_Q7[*periodicity_index]
	for j := opus_int(0); j < nb_subfr; j++ {
		for k := opus_int(0); k < LTP_ORDER; k++ {
			B_Q14[j*LTP_ORDER+k] = opus_int16(silk_LSHIFT(
				opus_int32(cbk_ptr_Q7[opus_int(cbk_index[j])*LTP_ORDER+k]), 7))
		}
	}

	if nb_subfr == 2 {
		res_nrg_Q15 = silk_RSHIFT32(res_nrg_Q15, 1)
	} else {
		res_nrg_Q15 = silk_RSHIFT32(res_nrg_Q15, 2)
	}

	*sum_log_gain_Q7 = best_sum_log_gain_Q7
	*pred_gain_dB_Q7 = opus_int(silk_SMULBB(-3, silk_lin2log(res_nrg_Q15)-(15<<7)))
}
