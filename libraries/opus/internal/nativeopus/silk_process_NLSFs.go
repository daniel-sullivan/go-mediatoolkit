package nativeopus

// Port of libopus/silk/process_NLSFs.c.
//
// Limit, stabilize, convert (to LPC) and quantize the encoder's NLSF
// vector. Optionally interpolates with the previous frame's quantized
// NLSFs per the indices.NLSFInterpCoef_Q2 flag.

// silk_process_NLSFs — C: process_NLSFs.c:35-107.
func silk_process_NLSFs(
	psEncC *silk_encoder_state,
	PredCoef_Q12 *[2][MAX_LPC_ORDER]opus_int16,
	pNLSF_Q15 []opus_int16,
	prev_NLSFq_Q15 []opus_int16,
) {
	var pNLSF0_temp_Q15 [MAX_LPC_ORDER]opus_int16
	var pNLSFW_QW [MAX_LPC_ORDER]opus_int16
	var pNLSFW0_temp_QW [MAX_LPC_ORDER]opus_int16

	silk_assert(psEncC.speech_activity_Q8 >= 0)
	silk_assert(psEncC.speech_activity_Q8 <= opus_int(SILK_FIX_CONST(1.0, 8)))
	celt_assert(psEncC.useInterpolatedNLSFs == 1 || psEncC.indices.NLSFInterpCoef_Q2 == (1<<2))

	// NLSF_mu = 0.003 - 0.0015 * speech_activity.
	NLSF_mu_Q20 := silk_SMLAWB(SILK_FIX_CONST(0.003, 20),
		SILK_FIX_CONST(-0.001, 28), opus_int32(psEncC.speech_activity_Q8))
	if psEncC.nb_subfr == 2 {
		// Multiply by 1.5 for 10 ms packets.
		NLSF_mu_Q20 = silk_ADD_RSHIFT(NLSF_mu_Q20, NLSF_mu_Q20, 1)
	}
	celt_assert(NLSF_mu_Q20 > 0)
	silk_assert(NLSF_mu_Q20 <= SILK_FIX_CONST(0.005, 20))

	silk_NLSF_VQ_weights_laroia(pNLSFW_QW[:], pNLSF_Q15, psEncC.predictLPCOrder)

	doInterpolate := psEncC.useInterpolatedNLSFs == 1 && psEncC.indices.NLSFInterpCoef_Q2 < 4
	if doInterpolate {
		silk_interpolate(pNLSF0_temp_Q15[:], prev_NLSFq_Q15, pNLSF_Q15,
			opus_int(psEncC.indices.NLSFInterpCoef_Q2), psEncC.predictLPCOrder)

		silk_NLSF_VQ_weights_laroia(pNLSFW0_temp_QW[:], pNLSF0_temp_Q15[:], psEncC.predictLPCOrder)

		i_sqr_Q15 := silk_LSHIFT(
			silk_SMULBB(opus_int32(psEncC.indices.NLSFInterpCoef_Q2), opus_int32(psEncC.indices.NLSFInterpCoef_Q2)),
			11)
		for i := opus_int(0); i < psEncC.predictLPCOrder; i++ {
			pNLSFW_QW[i] = silk_ADD16(
				opus_int16(silk_RSHIFT(opus_int32(pNLSFW_QW[i]), 1)),
				opus_int16(silk_RSHIFT(silk_SMULBB(opus_int32(pNLSFW0_temp_QW[i]), i_sqr_Q15), 16)))
			silk_assert(pNLSFW_QW[i] >= 1)
		}
	}

	silk_NLSF_encode(psEncC.indices.NLSFIndices[:], pNLSF_Q15, psEncC.psNLSF_CB, pNLSFW_QW[:],
		opus_int(NLSF_mu_Q20), psEncC.NLSF_MSVQ_Survivors, opus_int(psEncC.indices.signalType))

	// Convert quantized NLSFs back to LPC coefficients (second half).
	silk_NLSF2A(PredCoef_Q12[1][:], pNLSF_Q15, psEncC.predictLPCOrder, psEncC.arch)

	if doInterpolate {
		silk_interpolate(pNLSF0_temp_Q15[:], prev_NLSFq_Q15, pNLSF_Q15,
			opus_int(psEncC.indices.NLSFInterpCoef_Q2), psEncC.predictLPCOrder)
		silk_NLSF2A(PredCoef_Q12[0][:], pNLSF0_temp_Q15[:], psEncC.predictLPCOrder, psEncC.arch)
	} else {
		celt_assert(psEncC.predictLPCOrder <= MAX_LPC_ORDER)
		copy(PredCoef_Q12[0][:psEncC.predictLPCOrder], PredCoef_Q12[1][:psEncC.predictLPCOrder])
	}
}
