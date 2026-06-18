package nativeopus

// Port of libopus/silk/NLSF_VQ_weights_laroia.c.
//
// Laroia low-complexity NLSF weights.

// silk_NLSF_VQ_weights_laroia — Compute Laroia weights for NLSFs.
func silk_NLSF_VQ_weights_laroia(pNLSFW_Q_OUT, pNLSF_Q15 []opus_int16, D opus_int) {
	celt_assert(D > 0)
	celt_assert((D & 1) == 0)

	// First value.
	tmp1_int := silk_max_int(opus_int(pNLSF_Q15[0]), 1)
	tmp1_int32 := silk_DIV32_16(opus_int32(1)<<(15+NLSF_W_Q), opus_int32(tmp1_int))
	tmp2_int := silk_max_int(opus_int(pNLSF_Q15[1])-opus_int(pNLSF_Q15[0]), 1)
	tmp2_int32 := silk_DIV32_16(opus_int32(1)<<(15+NLSF_W_Q), opus_int32(tmp2_int))
	pNLSFW_Q_OUT[0] = opus_int16(silk_min_int(opus_int(tmp1_int32+tmp2_int32), opus_int(silk_int16_MAX)))
	silk_assert(pNLSFW_Q_OUT[0] > 0)

	// Main loop.
	for k := opus_int(1); k < D-1; k += 2 {
		tmp1_int = silk_max_int(opus_int(pNLSF_Q15[k+1])-opus_int(pNLSF_Q15[k]), 1)
		tmp1_int32 = silk_DIV32_16(opus_int32(1)<<(15+NLSF_W_Q), opus_int32(tmp1_int))
		pNLSFW_Q_OUT[k] = opus_int16(silk_min_int(opus_int(tmp1_int32+tmp2_int32), opus_int(silk_int16_MAX)))
		silk_assert(pNLSFW_Q_OUT[k] > 0)

		tmp2_int = silk_max_int(opus_int(pNLSF_Q15[k+2])-opus_int(pNLSF_Q15[k+1]), 1)
		tmp2_int32 = silk_DIV32_16(opus_int32(1)<<(15+NLSF_W_Q), opus_int32(tmp2_int))
		pNLSFW_Q_OUT[k+1] = opus_int16(silk_min_int(opus_int(tmp1_int32+tmp2_int32), opus_int(silk_int16_MAX)))
		silk_assert(pNLSFW_Q_OUT[k+1] > 0)
	}

	// Last value.
	tmp1_int = silk_max_int((1<<15)-opus_int(pNLSF_Q15[D-1]), 1)
	tmp1_int32 = silk_DIV32_16(opus_int32(1)<<(15+NLSF_W_Q), opus_int32(tmp1_int))
	pNLSFW_Q_OUT[D-1] = opus_int16(silk_min_int(opus_int(tmp1_int32+tmp2_int32), opus_int(silk_int16_MAX)))
	silk_assert(pNLSFW_Q_OUT[D-1] > 0)
}
