package nativeopus

// Port of libopus/silk/NLSF_del_dec_quant.c — delayed-decision
// quantizer for NLSF residuals.

// silk_NLSF_del_dec_quant — Return RD value in Q25.
func silk_NLSF_del_dec_quant(indices []opus_int8, x_Q10, w_Q5 []opus_int16,
	pred_coef_Q8 []opus_uint8, ec_ix []opus_int16, ec_rates_Q5 []opus_uint8,
	quant_step_size_Q16 opus_int, inv_quant_step_size_Q6 opus_int16,
	mu_Q20 opus_int32, order opus_int16) opus_int32 {

	var i, j, nStates, ind_tmp, ind_min_max, ind_max_min, in_Q10, res_Q10 opus_int
	var pred_Q10, diff_Q10, rate0_Q5, rate1_Q5 opus_int
	var out0_Q10, out1_Q10 opus_int16
	var RD_tmp_Q25, min_Q25, min_max_Q25, max_min_Q25 opus_int32

	var ind_sort [NLSF_QUANT_DEL_DEC_STATES]opus_int
	var ind [NLSF_QUANT_DEL_DEC_STATES][MAX_LPC_ORDER]opus_int8
	var prev_out_Q10 [2 * NLSF_QUANT_DEL_DEC_STATES]opus_int16
	var RD_Q25 [2 * NLSF_QUANT_DEL_DEC_STATES]opus_int32
	var RD_min_Q25 [NLSF_QUANT_DEL_DEC_STATES]opus_int32
	var RD_max_Q25 [NLSF_QUANT_DEL_DEC_STATES]opus_int32

	var out0_Q10_table [2 * NLSF_QUANT_MAX_AMPLITUDE_EXT]opus_int
	var out1_Q10_table [2 * NLSF_QUANT_MAX_AMPLITUDE_EXT]opus_int

	for i = -NLSF_QUANT_MAX_AMPLITUDE_EXT; i <= NLSF_QUANT_MAX_AMPLITUDE_EXT-1; i++ {
		o0 := silk_LSHIFT(opus_int32(i), 10)
		o1 := silk_ADD16(opus_int16(o0), 1024)
		o0_16 := opus_int16(o0)
		switch {
		case i > 0:
			o0_16 = silk_SUB16(o0_16, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
			o1 = silk_SUB16(o1, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
		case i == 0:
			o1 = silk_SUB16(o1, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
		case i == -1:
			o0_16 = silk_ADD16(o0_16, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
		default:
			o0_16 = silk_ADD16(o0_16, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
			o1 = silk_ADD16(o1, opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10)))
		}
		out0_Q10_table[i+NLSF_QUANT_MAX_AMPLITUDE_EXT] = opus_int(silk_RSHIFT(
			silk_SMULBB(opus_int32(o0_16), opus_int32(quant_step_size_Q16)), 16))
		out1_Q10_table[i+NLSF_QUANT_MAX_AMPLITUDE_EXT] = opus_int(silk_RSHIFT(
			silk_SMULBB(opus_int32(o1), opus_int32(quant_step_size_Q16)), 16))
	}

	silk_assert((NLSF_QUANT_DEL_DEC_STATES & (NLSF_QUANT_DEL_DEC_STATES - 1)) == 0)

	nStates = 1
	RD_Q25[0] = 0
	prev_out_Q10[0] = 0
	for i = opus_int(order) - 1; i >= 0; i-- {
		ratesBase := opus_int(ec_ix[i])
		in_Q10 = opus_int(x_Q10[i])
		for j = 0; j < nStates; j++ {
			pred_Q10 = opus_int(silk_RSHIFT(silk_SMULBB(
				opus_int32(opus_int16(pred_coef_Q8[i])),
				opus_int32(prev_out_Q10[j])), 8))
			res_Q10 = opus_int(silk_SUB16(opus_int16(in_Q10), opus_int16(pred_Q10)))
			ind_tmp = opus_int(silk_RSHIFT(silk_SMULBB(
				opus_int32(inv_quant_step_size_Q6), opus_int32(res_Q10)), 16))
			ind_tmp = silk_LIMIT(ind_tmp, -NLSF_QUANT_MAX_AMPLITUDE_EXT, NLSF_QUANT_MAX_AMPLITUDE_EXT-1)
			ind[j][i] = opus_int8(ind_tmp)

			// compute outputs for ind_tmp and ind_tmp+1.
			out0_Q10 = opus_int16(out0_Q10_table[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE_EXT])
			out1_Q10 = opus_int16(out1_Q10_table[ind_tmp+NLSF_QUANT_MAX_AMPLITUDE_EXT])

			out0_Q10 = silk_ADD16(out0_Q10, opus_int16(pred_Q10))
			out1_Q10 = silk_ADD16(out1_Q10, opus_int16(pred_Q10))
			prev_out_Q10[j] = out0_Q10
			prev_out_Q10[j+nStates] = out1_Q10

			// compute RD for ind_tmp and ind_tmp+1.
			switch {
			case ind_tmp+1 >= NLSF_QUANT_MAX_AMPLITUDE:
				if ind_tmp+1 == NLSF_QUANT_MAX_AMPLITUDE {
					rate0_Q5 = opus_int(ec_rates_Q5[ratesBase+ind_tmp+NLSF_QUANT_MAX_AMPLITUDE])
					rate1_Q5 = 280
				} else {
					rate0_Q5 = opus_int(silk_SMLABB(280-43*NLSF_QUANT_MAX_AMPLITUDE, 43, opus_int32(ind_tmp)))
					rate1_Q5 = opus_int(silk_ADD16(opus_int16(rate0_Q5), 43))
				}
			case ind_tmp <= -NLSF_QUANT_MAX_AMPLITUDE:
				if ind_tmp == -NLSF_QUANT_MAX_AMPLITUDE {
					rate0_Q5 = 280
					rate1_Q5 = opus_int(ec_rates_Q5[ratesBase+ind_tmp+1+NLSF_QUANT_MAX_AMPLITUDE])
				} else {
					rate0_Q5 = opus_int(silk_SMLABB(280-43*NLSF_QUANT_MAX_AMPLITUDE, -43, opus_int32(ind_tmp)))
					rate1_Q5 = opus_int(silk_SUB16(opus_int16(rate0_Q5), 43))
				}
			default:
				rate0_Q5 = opus_int(ec_rates_Q5[ratesBase+ind_tmp+NLSF_QUANT_MAX_AMPLITUDE])
				rate1_Q5 = opus_int(ec_rates_Q5[ratesBase+ind_tmp+1+NLSF_QUANT_MAX_AMPLITUDE])
			}
			RD_tmp_Q25 = RD_Q25[j]
			diff_Q10 = opus_int(silk_SUB16(opus_int16(in_Q10), out0_Q10))
			RD_Q25[j] = silk_SMLABB(
				silk_MLA(RD_tmp_Q25, silk_SMULBB(opus_int32(diff_Q10), opus_int32(diff_Q10)), opus_int32(w_Q5[i])),
				mu_Q20, opus_int32(rate0_Q5))
			diff_Q10 = opus_int(silk_SUB16(opus_int16(in_Q10), out1_Q10))
			RD_Q25[j+nStates] = silk_SMLABB(
				silk_MLA(RD_tmp_Q25, silk_SMULBB(opus_int32(diff_Q10), opus_int32(diff_Q10)), opus_int32(w_Q5[i])),
				mu_Q20, opus_int32(rate1_Q5))
		}

		if nStates <= NLSF_QUANT_DEL_DEC_STATES/2 {
			// double number of states and copy.
			for j = 0; j < nStates; j++ {
				ind[j+nStates][i] = ind[j][i] + 1
			}
			nStates = int(silk_LSHIFT(opus_int32(nStates), 1))
			for j = nStates; j < NLSF_QUANT_DEL_DEC_STATES; j++ {
				ind[j][i] = ind[j-nStates][i]
			}
		} else {
			// sort lower and upper half of RD_Q25, pairwise.
			for j = 0; j < NLSF_QUANT_DEL_DEC_STATES; j++ {
				if RD_Q25[j] > RD_Q25[j+NLSF_QUANT_DEL_DEC_STATES] {
					RD_max_Q25[j] = RD_Q25[j]
					RD_min_Q25[j] = RD_Q25[j+NLSF_QUANT_DEL_DEC_STATES]
					RD_Q25[j] = RD_min_Q25[j]
					RD_Q25[j+NLSF_QUANT_DEL_DEC_STATES] = RD_max_Q25[j]
					// swap prev_out values.
					out0_Q10 = prev_out_Q10[j]
					prev_out_Q10[j] = prev_out_Q10[j+NLSF_QUANT_DEL_DEC_STATES]
					prev_out_Q10[j+NLSF_QUANT_DEL_DEC_STATES] = out0_Q10
					ind_sort[j] = j + NLSF_QUANT_DEL_DEC_STATES
				} else {
					RD_min_Q25[j] = RD_Q25[j]
					RD_max_Q25[j] = RD_Q25[j+NLSF_QUANT_DEL_DEC_STATES]
					ind_sort[j] = j
				}
			}
			for {
				min_max_Q25 = silk_int32_MAX
				max_min_Q25 = 0
				ind_min_max = 0
				ind_max_min = 0
				for j = 0; j < NLSF_QUANT_DEL_DEC_STATES; j++ {
					if min_max_Q25 > RD_max_Q25[j] {
						min_max_Q25 = RD_max_Q25[j]
						ind_min_max = j
					}
					if max_min_Q25 < RD_min_Q25[j] {
						max_min_Q25 = RD_min_Q25[j]
						ind_max_min = j
					}
				}
				if min_max_Q25 >= max_min_Q25 {
					break
				}
				ind_sort[ind_max_min] = ind_sort[ind_min_max] ^ NLSF_QUANT_DEL_DEC_STATES
				RD_Q25[ind_max_min] = RD_Q25[ind_min_max+NLSF_QUANT_DEL_DEC_STATES]
				prev_out_Q10[ind_max_min] = prev_out_Q10[ind_min_max+NLSF_QUANT_DEL_DEC_STATES]
				RD_min_Q25[ind_max_min] = 0
				RD_max_Q25[ind_min_max] = silk_int32_MAX
				copy(ind[ind_max_min][:MAX_LPC_ORDER], ind[ind_min_max][:MAX_LPC_ORDER])
			}
			// increment index if it comes from the upper half.
			for j = 0; j < NLSF_QUANT_DEL_DEC_STATES; j++ {
				ind[j][i] += opus_int8(silk_RSHIFT(opus_int32(ind_sort[j]), NLSF_QUANT_DEL_DEC_STATES_LOG2))
			}
		}
	}

	// last sample: find winner, copy indices and return RD value.
	ind_tmp = 0
	min_Q25 = silk_int32_MAX
	for j = 0; j < 2*NLSF_QUANT_DEL_DEC_STATES; j++ {
		if min_Q25 > RD_Q25[j] {
			min_Q25 = RD_Q25[j]
			ind_tmp = j
		}
	}
	for j = 0; j < opus_int(order); j++ {
		indices[j] = ind[ind_tmp&(NLSF_QUANT_DEL_DEC_STATES-1)][j]
		silk_assert(indices[j] >= -NLSF_QUANT_MAX_AMPLITUDE_EXT)
		silk_assert(indices[j] <= NLSF_QUANT_MAX_AMPLITUDE_EXT)
	}
	indices[0] += opus_int8(silk_RSHIFT(opus_int32(ind_tmp), NLSF_QUANT_DEL_DEC_STATES_LOG2))
	silk_assert(indices[0] <= NLSF_QUANT_MAX_AMPLITUDE_EXT)
	silk_assert(min_Q25 >= 0)
	return min_Q25
}
