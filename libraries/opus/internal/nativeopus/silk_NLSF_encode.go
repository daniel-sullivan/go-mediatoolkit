package nativeopus

// Port of libopus/silk/NLSF_encode.c.

// silk_NLSF_encode — NLSF vector encoder; returns RD value in Q25.
func silk_NLSF_encode(NLSFIndices []opus_int8, pNLSF_Q15 []opus_int16,
	psNLSF_CB *silk_NLSF_CB_struct, pW_Q2 []opus_int16,
	NLSF_mu_Q20 opus_int, nSurvivors, signalType opus_int) opus_int32 {

	celt_assert(signalType >= 0 && signalType <= 2)
	silk_assert(NLSF_mu_Q20 <= 32767 && NLSF_mu_Q20 >= 0)

	// NLSF stabilization.
	silk_NLSF_stabilize(pNLSF_Q15, psNLSF_CB.deltaMin_Q15, opus_int(psNLSF_CB.order))

	// First stage: VQ.
	err_Q24 := make([]opus_int32, psNLSF_CB.nVectors)
	silk_NLSF_VQ(err_Q24, pNLSF_Q15, psNLSF_CB.CB1_NLSF_Q8, psNLSF_CB.CB1_Wght_Q9,
		opus_int(psNLSF_CB.nVectors), opus_int(psNLSF_CB.order))

	// Sort quantization errors.
	tempIndices1 := make([]opus_int, nSurvivors)
	silk_insertion_sort_increasing(err_Q24, tempIndices1, opus_int(psNLSF_CB.nVectors), nSurvivors)

	RD_Q25 := make([]opus_int32, nSurvivors)
	tempIndices2 := make([]opus_int8, nSurvivors*MAX_LPC_ORDER)

	var res_Q10 [MAX_LPC_ORDER]opus_int16
	var NLSF_tmp_Q15 [MAX_LPC_ORDER]opus_int16
	var W_adj_Q5 [MAX_LPC_ORDER]opus_int16
	var pred_Q8 [MAX_LPC_ORDER]opus_uint8
	var ec_ix [MAX_LPC_ORDER]opus_int16

	// Loop over survivors.
	for s := opus_int(0); s < nSurvivors; s++ {
		ind1 := tempIndices1[s]

		cbBase := ind1 * opus_int(psNLSF_CB.order)
		for i := opus_int(0); i < opus_int(psNLSF_CB.order); i++ {
			NLSF_tmp_Q15[i] = silk_LSHIFT16(opus_int16(psNLSF_CB.CB1_NLSF_Q8[cbBase+i]), 7)
			W_tmp_Q9 := opus_int32(psNLSF_CB.CB1_Wght_Q9[cbBase+i])
			res_Q10[i] = opus_int16(silk_RSHIFT(silk_SMULBB(
				opus_int32(pNLSF_Q15[i])-opus_int32(NLSF_tmp_Q15[i]), W_tmp_Q9), 14))
			W_adj_Q5[i] = opus_int16(silk_DIV32_varQ(opus_int32(pW_Q2[i]),
				silk_SMULBB(W_tmp_Q9, W_tmp_Q9), 21))
		}

		// Unpack entropy table indices and predictor.
		silk_NLSF_unpack(ec_ix[:], pred_Q8[:], psNLSF_CB, ind1)

		// Trellis quantizer.
		RD_Q25[s] = silk_NLSF_del_dec_quant(
			tempIndices2[s*MAX_LPC_ORDER:],
			res_Q10[:], W_adj_Q5[:], pred_Q8[:], ec_ix[:],
			psNLSF_CB.ec_Rates_Q5, opus_int(psNLSF_CB.quantStepSize_Q16),
			psNLSF_CB.invQuantStepSize_Q6, opus_int32(NLSF_mu_Q20),
			psNLSF_CB.order)

		// Add rate for first stage.
		iCDFBase := (signalType >> 1) * opus_int(psNLSF_CB.nVectors)
		var prob_Q8 opus_int
		if ind1 == 0 {
			prob_Q8 = 256 - opus_int(psNLSF_CB.CB1_iCDF[iCDFBase+ind1])
		} else {
			prob_Q8 = opus_int(psNLSF_CB.CB1_iCDF[iCDFBase+ind1-1]) -
				opus_int(psNLSF_CB.CB1_iCDF[iCDFBase+ind1])
		}
		bits_q7 := opus_int32((8 << 7) - silk_lin2log(opus_int32(prob_Q8)))
		RD_Q25[s] = silk_SMLABB(RD_Q25[s], bits_q7, silk_RSHIFT(opus_int32(NLSF_mu_Q20), 2))
	}

	// Find lowest rate-distortion error.
	bestIndex := make([]opus_int, 1)
	silk_insertion_sort_increasing(RD_Q25, bestIndex, nSurvivors, 1)

	NLSFIndices[0] = opus_int8(tempIndices1[bestIndex[0]])
	copy(NLSFIndices[1:1+opus_int(psNLSF_CB.order)],
		tempIndices2[bestIndex[0]*MAX_LPC_ORDER:bestIndex[0]*MAX_LPC_ORDER+opus_int(psNLSF_CB.order)])

	// Decode.
	silk_NLSF_decode(pNLSF_Q15, NLSFIndices, psNLSF_CB)

	return RD_Q25[0]
}
