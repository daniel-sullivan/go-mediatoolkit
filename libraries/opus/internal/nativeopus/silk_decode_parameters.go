package nativeopus

// Port of libopus/silk/decode_parameters.c.

// silk_decode_parameters — decode gains, NLSFs, LPC coefs, pitch lags,
// LTP filter coefs, and LTP scaling.
// C: decode_parameters.c:35-115.
func silk_decode_parameters(psDec *silk_decoder_state, psDecCtrl *silk_decoder_control, condCoding opus_int) {
	var pNLSF_Q15 [MAX_LPC_ORDER]opus_int16
	var pNLSF0_Q15 [MAX_LPC_ORDER]opus_int16

	// Dequant Gains.
	conditional := opus_int(0)
	if condCoding == CODE_CONDITIONALLY {
		conditional = 1
	}
	silk_gains_dequant(psDecCtrl.Gains_Q16[:], psDec.indices.GainsIndices[:],
		&psDec.LastGainIndex, conditional, psDec.nb_subfr)

	// Decode NLSFs.
	silk_NLSF_decode(pNLSF_Q15[:], psDec.indices.NLSFIndices[:], psDec.psNLSF_CB)

	// Convert NLSF parameters to AR prediction filter coefficients.
	silk_NLSF2A(psDecCtrl.PredCoef_Q12[1][:], pNLSF_Q15[:], psDec.LPC_order, psDec.arch)

	// If just reset, do not allow interpolation.
	if psDec.first_frame_after_reset == 1 {
		psDec.indices.NLSFInterpCoef_Q2 = 4
	}

	if psDec.indices.NLSFInterpCoef_Q2 < 4 {
		for i := opus_int(0); i < psDec.LPC_order; i++ {
			pNLSF0_Q15[i] = psDec.prevNLSF_Q15[i] + opus_int16(silk_RSHIFT(
				silk_MUL(opus_int32(psDec.indices.NLSFInterpCoef_Q2),
					opus_int32(pNLSF_Q15[i])-opus_int32(psDec.prevNLSF_Q15[i])), 2))
		}
		silk_NLSF2A(psDecCtrl.PredCoef_Q12[0][:], pNLSF0_Q15[:], psDec.LPC_order, psDec.arch)
	} else {
		copy(psDecCtrl.PredCoef_Q12[0][:psDec.LPC_order], psDecCtrl.PredCoef_Q12[1][:psDec.LPC_order])
	}

	copy(psDec.prevNLSF_Q15[:psDec.LPC_order], pNLSF_Q15[:psDec.LPC_order])

	// After packet loss, do BWE of LPC coefs.
	if psDec.lossCnt != 0 {
		silk_bwexpander(psDecCtrl.PredCoef_Q12[0][:], psDec.LPC_order, BWE_AFTER_LOSS_Q16)
		silk_bwexpander(psDecCtrl.PredCoef_Q12[1][:], psDec.LPC_order, BWE_AFTER_LOSS_Q16)
	}

	if psDec.indices.signalType == TYPE_VOICED {
		// Decode pitch values.
		silk_decode_pitch(psDec.indices.lagIndex, psDec.indices.contourIndex,
			psDecCtrl.pitchL[:], psDec.fs_kHz, psDec.nb_subfr)

		// Decode codebook index.
		cbk_ptr_Q7 := silk_LTP_vq_ptrs_Q7[psDec.indices.PERIndex]

		for k := opus_int(0); k < psDec.nb_subfr; k++ {
			Ix := opus_int(psDec.indices.LTPIndex[k])
			for i := opus_int(0); i < LTP_ORDER; i++ {
				psDecCtrl.LTPCoef_Q14[k*LTP_ORDER+i] = opus_int16(silk_LSHIFT(
					opus_int32(cbk_ptr_Q7[Ix*LTP_ORDER+i]), 7))
			}
		}

		// Decode LTP scaling.
		Ix := psDec.indices.LTP_scaleIndex
		psDecCtrl.LTP_scale_Q14 = opus_int(silk_LTPScales_table_Q14[Ix])
	} else {
		for i := opus_int(0); i < psDec.nb_subfr; i++ {
			psDecCtrl.pitchL[i] = 0
		}
		for i := opus_int(0); i < LTP_ORDER*psDec.nb_subfr; i++ {
			psDecCtrl.LTPCoef_Q14[i] = 0
		}
		psDec.indices.PERIndex = 0
		psDecCtrl.LTP_scale_Q14 = 0
	}
}
