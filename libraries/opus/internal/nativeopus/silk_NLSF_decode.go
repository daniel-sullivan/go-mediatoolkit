package nativeopus

// Port of libopus/silk/NLSF_decode.c.

// silk_NLSF_residual_dequant — static predictive dequantizer for NLSF residuals.
func silk_NLSF_residual_dequant(x_Q10 []opus_int16, indices []opus_int8,
	pred_coef_Q8 []opus_uint8, quant_step_size_Q16 opus_int, order opus_int16) {
	var out_Q10, pred_Q10 opus_int
	for i := opus_int(order) - 1; i >= 0; i-- {
		pred_Q10 = opus_int(silk_RSHIFT(silk_SMULBB(
			opus_int32(out_Q10), opus_int32(opus_int16(pred_coef_Q8[i]))), 8))
		out_Q10 = opus_int(silk_LSHIFT(opus_int32(indices[i]), 10))
		if out_Q10 > 0 {
			out_Q10 = opus_int(silk_SUB16(opus_int16(out_Q10),
				opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10))))
		} else if out_Q10 < 0 {
			out_Q10 = opus_int(silk_ADD16(opus_int16(out_Q10),
				opus_int16(SILK_FIX_CONST(NLSF_QUANT_LEVEL_ADJ, 10))))
		}
		out_Q10 = opus_int(silk_SMLAWB(opus_int32(pred_Q10),
			opus_int32(out_Q10), opus_int32(quant_step_size_Q16)))
		x_Q10[i] = opus_int16(out_Q10)
	}
}

// silk_NLSF_decode — NLSF vector decoder.
func silk_NLSF_decode(pNLSF_Q15 []opus_int16, NLSFIndices []opus_int8,
	psNLSF_CB *silk_NLSF_CB_struct) {
	var pred_Q8 [MAX_LPC_ORDER]opus_uint8
	var ec_ix [MAX_LPC_ORDER]opus_int16
	var res_Q10 [MAX_LPC_ORDER]opus_int16
	var NLSF_Q15_tmp opus_int32

	// Unpack entropy-table indices and predictor for current CB1 index.
	silk_NLSF_unpack(ec_ix[:], pred_Q8[:], psNLSF_CB, opus_int(NLSFIndices[0]))

	// Predictive residual dequantizer.
	silk_NLSF_residual_dequant(res_Q10[:], NLSFIndices[1:], pred_Q8[:],
		opus_int(psNLSF_CB.quantStepSize_Q16), psNLSF_CB.order)

	// Apply inverse square-rooted weights to first stage and add to output.
	cbBase := opus_int(NLSFIndices[0]) * opus_int(psNLSF_CB.order)
	for i := opus_int(0); i < opus_int(psNLSF_CB.order); i++ {
		NLSF_Q15_tmp = silk_ADD_LSHIFT32(
			silk_DIV32_16(silk_LSHIFT(opus_int32(res_Q10[i]), 14),
				opus_int32(psNLSF_CB.CB1_Wght_Q9[cbBase+i])),
			opus_int32(opus_int16(psNLSF_CB.CB1_NLSF_Q8[cbBase+i])), 7)
		pNLSF_Q15[i] = opus_int16(silk_LIMIT(NLSF_Q15_tmp, 0, 32767))
	}

	// NLSF stabilization.
	silk_NLSF_stabilize(pNLSF_Q15, psNLSF_CB.deltaMin_Q15, opus_int(psNLSF_CB.order))
}
