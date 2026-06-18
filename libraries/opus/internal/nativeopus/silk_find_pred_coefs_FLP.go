package nativeopus

import "math"

// 1:1 port of libopus/silk/float/find_pred_coefs_FLP.c.
//
// Finds LPC and LTP coefficients for the current frame. Calls into
// previously-ported SILK float-path primitives: silk_find_LTP_FLP,
// silk_quant_LTP_gains_FLP, silk_LTP_scale_ctrl_FLP,
// silk_LTP_analysis_filter_FLP, silk_scale_copy_vector_FLP,
// silk_find_LPC_FLP, silk_process_NLSFs_FLP, silk_residual_energy_FLP.
//
// Pointer-arithmetic notes for the port:
//   * C takes `res_pitch[]` as a pointer into a larger buffer; the Go
//     port takes the full backing slice plus `resPitchOff`, which
//     silk_find_LTP_FLP consumes at offset lag[k]+LTP_ORDER/2 below.
//   * C takes `x[]` as a pointer into a larger buffer with valid
//     history for `x - predictLPCOrder`; Go port mirrors with `xBuf`
//     and `xOff`. `xOff - predictLPCOrder` must be ≥ 0.
//
// Float-arithmetic notes:
//   * The `minInvGain` expression evaluates
//     `(silk_float)pow(2, LTPredCodGain/3) / MAX_PREDICTION_POWER_GAIN`
//     in C. `pow` returns `double`, the division happens in `double`,
//     then casts to `silk_float`. Mirror via math.Pow + float64 div
//     and one narrowing cast at the end.
//   * `minInvGain /= 0.25f + 0.75f * coding_quality` — the unsuffixed
//     `.25`/`.75` in the source use `0.25f`/`0.75f` (float32 literals).
//     Inner mul-add needs fma_add to stop Go from fusing `a+b*c` into
//     FMADDS on arm64.

func silk_find_pred_coefs_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	resPitchBuf []silk_float,
	resPitchOff opus_int,
	xBuf []silk_float,
	xOff opus_int,
	condCoding opus_int,
) {
	var i opus_int
	var XXLTP [MAX_NB_SUBFR * LTP_ORDER * LTP_ORDER]silk_float
	var xXLTP [MAX_NB_SUBFR * LTP_ORDER]silk_float
	var invGains [MAX_NB_SUBFR]silk_float
	// Set to NLSF_Q15 to zero so we don't copy junk to the state.
	var NLSF_Q15 [MAX_LPC_ORDER]opus_int16
	var LPC_in_pre [MAX_NB_SUBFR*MAX_LPC_ORDER + MAX_FRAME_LENGTH]silk_float
	var minInvGain silk_float

	// Weighting for weighted least squares.
	for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
		silk_assert(psEncCtrl.Gains[i] > 0.0)
		invGains[i] = 1.0 / psEncCtrl.Gains[i]
	}

	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// **********
		// VOICED
		// **********
		celt_assert(psEnc.sCmn.ltp_mem_length-psEnc.sCmn.predictLPCOrder >= psEncCtrl.pitchL[0]+LTP_ORDER/2)

		// LTP analysis.
		silk_find_LTP_FLP(XXLTP[:], xXLTP[:], resPitchBuf, resPitchOff,
			psEncCtrl.pitchL[:], psEnc.sCmn.subfr_length, psEnc.sCmn.nb_subfr, psEnc.sCmn.arch)

		// Quantize LTP gain parameters.
		silk_quant_LTP_gains_FLP(psEncCtrl.LTPCoef[:], psEnc.sCmn.indices.LTPIndex[:], &psEnc.sCmn.indices.PERIndex,
			&psEnc.sCmn.sum_log_gain_Q7, &psEncCtrl.LTPredCodGain, XXLTP[:], xXLTP[:],
			psEnc.sCmn.subfr_length, psEnc.sCmn.nb_subfr, psEnc.sCmn.arch)

		// Control LTP scaling.
		silk_LTP_scale_ctrl_FLP(psEnc, psEncCtrl, condCoding)

		// Create LTP residual.
		silk_LTP_analysis_filter_FLP(LPC_in_pre[:], xBuf, xOff-psEnc.sCmn.predictLPCOrder,
			psEncCtrl.LTPCoef[:], psEncCtrl.pitchL[:], invGains[:],
			psEnc.sCmn.subfr_length, psEnc.sCmn.nb_subfr, psEnc.sCmn.predictLPCOrder)
	} else {
		// ************
		// UNVOICED
		// ************
		// Create signal with prepended subframes, scaled by inverse gains.
		xPtrOff := xOff - psEnc.sCmn.predictLPCOrder // x_ptr = x - predictLPCOrder
		xPreOff := opus_int(0)                       // x_pre_ptr = LPC_in_pre
		for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
			silk_scale_copy_vector_FLP(LPC_in_pre[xPreOff:], xBuf[xPtrOff:], invGains[i],
				psEnc.sCmn.subfr_length+psEnc.sCmn.predictLPCOrder)
			xPreOff += psEnc.sCmn.subfr_length + psEnc.sCmn.predictLPCOrder
			xPtrOff += psEnc.sCmn.subfr_length
		}
		// silk_memset( psEncCtrl->LTPCoef, 0, psEnc->sCmn.nb_subfr * LTP_ORDER * sizeof( silk_float ) );
		for j := opus_int(0); j < psEnc.sCmn.nb_subfr*LTP_ORDER; j++ {
			psEncCtrl.LTPCoef[j] = 0
		}
		psEncCtrl.LTPredCodGain = 0.0
		psEnc.sCmn.sum_log_gain_Q7 = 0
	}

	// Limit on total predictive coding gain.
	if psEnc.sCmn.first_frame_after_reset != 0 {
		minInvGain = 1.0 / MAX_PREDICTION_POWER_GAIN_AFTER_RESET
	} else {
		// C: minInvGain = (silk_float)pow( 2, LTPredCodGain/3 ) / MAX_PREDICTION_POWER_GAIN;
		// Steps, left-to-right under C conversion rules:
		//   1. LTPredCodGain/3 : silk_float / int → silk_float.
		//   2. pow(double, double) : silk_float promoted to double → pow → double.
		//   3. (silk_float)pow(...) : narrowing cast to float32.
		//   4. ... / MAX_PREDICTION_POWER_GAIN : 1e4 is an unsuffixed double literal.
		//      float32 / double promotes LHS to double; divide in double.
		//   5. Assignment to silk_float : narrow to float32.
		gainOver3 := psEncCtrl.LTPredCodGain / silk_float(3)
		powD := math.Pow(2, float64(gainOver3))
		powF := silk_float(powD) // narrow to float32.
		minInvGain = silk_float(float64(powF) / MAX_PREDICTION_POWER_GAIN)
		// minInvGain /= 0.25f + 0.75f * coding_quality;
		// Unsuffixed C literals 0.25/0.75 with 'f' suffix are float32.
		denom := fma_add(silk_float(0.25), silk_float(0.75), psEncCtrl.coding_quality)
		minInvGain = minInvGain / denom
	}

	// LPC_in_pre contains the LTP-filtered input for voiced, and the unfiltered input for unvoiced.
	silk_find_LPC_FLP(&psEnc.sCmn, NLSF_Q15[:], LPC_in_pre[:], minInvGain, psEnc.sCmn.arch)

	// Quantize LSFs.
	silk_process_NLSFs_FLP(&psEnc.sCmn, &psEncCtrl.PredCoef, NLSF_Q15[:], psEnc.sCmn.prev_NLSFq_Q15[:])

	// Calculate residual energy using quantized LPC coefficients.
	silk_residual_energy_FLP(psEncCtrl.ResNrg[:], LPC_in_pre[:], psEncCtrl.PredCoef, psEncCtrl.Gains[:],
		psEnc.sCmn.subfr_length, psEnc.sCmn.nb_subfr, psEnc.sCmn.predictLPCOrder)

	// Copy to prediction struct for use in next frame for interpolation.
	copy(psEnc.sCmn.prev_NLSFq_Q15[:], NLSF_Q15[:])
}
