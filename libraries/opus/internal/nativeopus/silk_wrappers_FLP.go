package nativeopus

// 1:1 port of libopus/silk/float/wrappers_FLP.c.
//
// Float↔int wrappers that marshal per-frame silk_encoder_state_FLP
// and silk_encoder_control_FLP fields into fixed-point scratch
// buffers, then call the shared SILK fixed-point kernels
// (silk_A2NLSF, silk_NLSF2A, silk_process_NLSFs, silk_NSQ /
// silk_NSQ_del_dec, silk_quant_LTP_gains) before unpacking the
// results back into silk_float.
//
// Arithmetic notes
// ----------------
//   * silk_float2int rounds float32→int32 via round-to-nearest-even.
//     Several call sites in this file then cast the result further
//     to (opus_int16). The cast is technically UB when the float is
//     out of int16 range, but the C inputs here (AR * 8192, LTPCoef
//     * 16384, PredCoef * 4096) all stay within [-32768, 32767] for
//     realistic encoder state, so mirroring the narrow cast is safe
//     and produces bit-exact results with C.
//   * `silk_LSHIFT32(x, 16) | (opus_uint16)y` in LF_shp_Q14
//     packs two 16-bit halves into one int32. On both sides the
//     result of `silk_float2int(...)` is already a rounded int32; we
//     narrow through opus_uint16 for the low half to match C.
//   * `(silk_float)int16 * (1.0f / 4096.0f)` etc: the int16 is
//     promoted to int in C's integer promotions, then converted to
//     float at the `(silk_float)` cast, then multiplied by the
//     float32 constant. We evaluate the cast in float32 directly.

// silk_A2NLSF_FLP / silk_NLSF2A_FLP are ported alongside their only
// caller in silk_find_LPC_FLP.go; this file does not re-declare them.

// silk_process_NLSFs_FLP — Floating-point NLSF processing wrapper.
// C: wrappers_FLP.c:74-91.
func silk_process_NLSFs_FLP(
	psEncC *silk_encoder_state,
	PredCoef *[2][MAX_LPC_ORDER]silk_float,
	NLSF_Q15 []opus_int16, // [MAX_LPC_ORDER] (I/O)
	prev_NLSF_Q15 []opus_int16, // [MAX_LPC_ORDER]
) {
	var PredCoef_Q12 [2][MAX_LPC_ORDER]opus_int16

	silk_process_NLSFs(psEncC, &PredCoef_Q12, NLSF_Q15, prev_NLSF_Q15)

	for j := opus_int(0); j < 2; j++ {
		for i := opus_int(0); i < psEncC.predictLPCOrder; i++ {
			PredCoef[j][i] = mul_f32(silk_float(PredCoef_Q12[j][i]), 1.0/4096.0)
		}
	}
}

// silk_NSQ_wrapper_FLP — Floating-point Silk NSQ wrapper.
// C: wrappers_FLP.c:96-170.
func silk_NSQ_wrapper_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	psIndices *SideInfoIndices,
	psNSQ *silk_nsq_state,
	pulses []opus_int8, // output
	x []silk_float, // input [frame_length]
) {
	var x16 [MAX_FRAME_LENGTH]opus_int16
	var Gains_Q16 [MAX_NB_SUBFR]opus_int32
	var PredCoef_Q12 [2][MAX_LPC_ORDER]opus_int16
	var LTPCoef_Q14 [LTP_ORDER * MAX_NB_SUBFR]opus_int16
	var LTP_scale_Q14 opus_int

	// Noise shaping parameters.
	var AR_Q13 [MAX_NB_SUBFR * MAX_SHAPE_LPC_ORDER]opus_int16
	var LF_shp_Q14 [MAX_NB_SUBFR]opus_int32
	var Lambda_Q10 opus_int
	var Tilt_Q14 [MAX_NB_SUBFR]opus_int
	var HarmShapeGain_Q14 [MAX_NB_SUBFR]opus_int

	// AR_Q13 from AR float coefs, scale by 8192.
	for i := opus_int(0); i < psEnc.sCmn.nb_subfr; i++ {
		for j := opus_int(0); j < psEnc.sCmn.shapingLPCOrder; j++ {
			AR_Q13[i*MAX_SHAPE_LPC_ORDER+j] = opus_int16(silk_float2int(
				mul_f32(psEncCtrl.AR[i*MAX_SHAPE_LPC_ORDER+j], 8192.0)))
		}
	}

	for i := opus_int(0); i < psEnc.sCmn.nb_subfr; i++ {
		// LF_shp_Q14 packs LF_AR_shp (high 16) | LF_MA_shp (low 16).
		hi := silk_LSHIFT32(silk_float2int(mul_f32(psEncCtrl.LF_AR_shp[i], 16384.0)), 16)
		lo := opus_int32(opus_uint16(silk_float2int(mul_f32(psEncCtrl.LF_MA_shp[i], 16384.0))))
		LF_shp_Q14[i] = hi | lo
		Tilt_Q14[i] = opus_int(silk_float2int(mul_f32(psEncCtrl.Tilt[i], 16384.0)))
		HarmShapeGain_Q14[i] = opus_int(silk_float2int(mul_f32(psEncCtrl.HarmShapeGain[i], 16384.0)))
	}
	Lambda_Q10 = opus_int(silk_float2int(mul_f32(psEncCtrl.Lambda, 1024.0)))

	// Prediction and coding parameters.
	for i := opus_int(0); i < psEnc.sCmn.nb_subfr*LTP_ORDER; i++ {
		LTPCoef_Q14[i] = opus_int16(silk_float2int(mul_f32(psEncCtrl.LTPCoef[i], 16384.0)))
	}

	for j := opus_int(0); j < 2; j++ {
		for i := opus_int(0); i < psEnc.sCmn.predictLPCOrder; i++ {
			PredCoef_Q12[j][i] = opus_int16(silk_float2int(mul_f32(psEncCtrl.PredCoef[j][i], 4096.0)))
		}
	}

	for i := opus_int(0); i < psEnc.sCmn.nb_subfr; i++ {
		Gains_Q16[i] = silk_float2int(mul_f32(psEncCtrl.Gains[i], 65536.0))
		silk_assert(Gains_Q16[i] > 0)
	}

	if psIndices.signalType == TYPE_VOICED {
		LTP_scale_Q14 = opus_int(silk_LTPScales_table_Q14[psIndices.LTP_scaleIndex])
	} else {
		LTP_scale_Q14 = 0
	}

	// Convert input to fix.
	for i := opus_int(0); i < psEnc.sCmn.frame_length; i++ {
		x16[i] = opus_int16(silk_float2int(x[i]))
	}

	// pitchL is opus_int in C (on our ports, []opus_int). Construct a
	// slice view to pass through.
	pitchL := psEncCtrl.pitchL[:]

	// Call NSQ.
	// C passes `PredCoef_Q12[0]` which, as a decayed pointer to the
	// first element of a 2D int16 array, exposes 2*MAX_LPC_ORDER
	// contiguous entries. Flatten here so the callee can index at
	// offsets 0 or MAX_LPC_ORDER.
	var PredCoefFlat [2 * MAX_LPC_ORDER]opus_int16
	for j := 0; j < 2; j++ {
		for i := 0; i < MAX_LPC_ORDER; i++ {
			PredCoefFlat[j*MAX_LPC_ORDER+i] = PredCoef_Q12[j][i]
		}
	}
	if psEnc.sCmn.nStatesDelayedDecision > 1 || psEnc.sCmn.warping_Q16 > 0 {
		silk_NSQ_del_dec_c(&psEnc.sCmn, psNSQ, psIndices, x16[:], pulses,
			PredCoefFlat[:], LTPCoef_Q14[:], AR_Q13[:],
			HarmShapeGain_Q14[:], Tilt_Q14[:], LF_shp_Q14[:], Gains_Q16[:],
			pitchL, Lambda_Q10, LTP_scale_Q14)
	} else {
		silk_NSQ_c(&psEnc.sCmn, psNSQ, psIndices, x16[:], pulses,
			PredCoefFlat[:], LTPCoef_Q14[:], AR_Q13[:],
			HarmShapeGain_Q14[:], Tilt_Q14[:], LF_shp_Q14[:], Gains_Q16[:],
			pitchL, Lambda_Q10, LTP_scale_Q14)
	}
}

// silk_quant_LTP_gains_FLP — Floating-point Silk LTP quantization wrapper.
// C: wrappers_FLP.c:175-209.
func silk_quant_LTP_gains_FLP(
	B []silk_float, // [MAX_NB_SUBFR * LTP_ORDER] output
	cbk_index []opus_int8, // [MAX_NB_SUBFR] output
	periodicity_index *opus_int8,
	sum_log_gain_Q7 *opus_int32,
	pred_gain_dB *silk_float,
	XX []silk_float, // [MAX_NB_SUBFR * LTP_ORDER * LTP_ORDER]
	xX []silk_float, // [MAX_NB_SUBFR * LTP_ORDER]
	subfr_len opus_int,
	nb_subfr opus_int,
	arch int,
) {
	var pred_gain_dB_Q7 opus_int
	var B_Q14 [MAX_NB_SUBFR * LTP_ORDER]opus_int16
	var XX_Q17 [MAX_NB_SUBFR * LTP_ORDER * LTP_ORDER]opus_int32
	var xX_Q17 [MAX_NB_SUBFR * LTP_ORDER]opus_int32

	// C uses a do..while loop to marshal XX and xX; index is 0..
	// nb_subfr * LTP_ORDER*LTP_ORDER - 1 and nb_subfr*LTP_ORDER - 1.
	for i := opus_int(0); i < nb_subfr*LTP_ORDER*LTP_ORDER; i++ {
		XX_Q17[i] = silk_float2int(mul_f32(XX[i], 131072.0))
	}
	for i := opus_int(0); i < nb_subfr*LTP_ORDER; i++ {
		xX_Q17[i] = silk_float2int(mul_f32(xX[i], 131072.0))
	}

	silk_quant_LTP_gains(B_Q14[:], cbk_index, periodicity_index, sum_log_gain_Q7,
		&pred_gain_dB_Q7, XX_Q17[:], xX_Q17[:], subfr_len, nb_subfr, arch)

	for i := opus_int(0); i < nb_subfr*LTP_ORDER; i++ {
		B[i] = mul_f32(silk_float(B_Q14[i]), 1.0/16384.0)
	}

	*pred_gain_dB = mul_f32(silk_float(pred_gain_dB_Q7), 1.0/128.0)
}
