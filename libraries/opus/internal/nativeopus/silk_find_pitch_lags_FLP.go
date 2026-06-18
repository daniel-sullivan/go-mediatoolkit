package nativeopus

// 1:1 port of libopus/silk/float/find_pitch_lags_FLP.c.
// Pitch-analysis mid-driver: windows the LPC-analysis buffer,
// computes the autocorrelation, converts reflection coefficients
// to prediction coefficients via k2a, applies bandwidth expansion,
// LPC-analysis-filters the input to get a residual, then calls
// silk_pitch_analysis_core_FLP on the residual.

// silk_find_pitch_lags_FLP — the caller passes a slice `x` whose
// index 0 corresponds to the C `x[0]` pointer. Internally the function
// needs to reach `x[-ltp_mem_length .. buf_len)`, which is emulated by
// negative-indexing into the underlying buffer via the `xBufOff` /
// `xBufPtrOff` integers below; callers must therefore pass the full
// speech buffer starting at the LTP history, using xBase[:] plus an
// offset. Because Go slices don't permit negative indices, we require
// callers to pass the underlying buffer (via find_pitch_lags_FLP_with_xbuf)
// rather than just a view. This wrapper keeps the C-matching
// signature for internal use where `x` is already a view that happens
// to have capacity-to-the-left.
func silk_find_pitch_lags_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	res []silk_float,
	x []silk_float,
	arch int,
) {
	silk_find_pitch_lags_FLP_withBase(psEnc, psEncCtrl, res, x, 0, arch)
}

// silk_find_pitch_lags_FLP_withBase — the `xBase` slice is the
// underlying buffer, `xOff` is the index at which C would have its
// `x` pointer. Internally we operate on `xBase` with explicit offsets.
func silk_find_pitch_lags_FLP_withBase(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	res []silk_float,
	xBase []silk_float,
	xOff opus_int,
	arch int,
) {
	// All C-side pointer arithmetic starts from `x[0]`; we apply
	// `xOff` at every index so callers can pass a backing buffer that
	// extends to the left of `x[0]`.
	var buf_len opus_int
	var thrhld, res_nrg silk_float
	var auto_corr [MAX_FIND_PITCH_LPC_ORDER + 1]silk_float
	var A [MAX_FIND_PITCH_LPC_ORDER]silk_float
	var refl_coef [MAX_FIND_PITCH_LPC_ORDER]silk_float
	var Wsig [FIND_PITCH_LPC_WIN_MAX]silk_float

	//******************************************
	// Set up buffer lengths etc based on Fs
	//******************************************
	buf_len = psEnc.sCmn.la_pitch + psEnc.sCmn.frame_length + psEnc.sCmn.ltp_mem_length

	// Safety check
	celt_assert(buf_len >= psEnc.sCmn.pitch_LPC_win_length)

	// x_buf = x - psEnc.sCmn.ltp_mem_length
	// In C this is a pointer arithmetic trick: the caller passes `x`
	// pointing into psEnc.x_buf past the LTP memory region, so going
	// backwards by ltp_mem_length yields the start of x_buf. We
	// express this as an offset into the backing xBase slice.
	// xBufOff is the offset into xBase where C's x_buf[0] lives.
	xBufOff := xOff - psEnc.sCmn.ltp_mem_length
	// x_buf[i] == xBase[i + xBufOff] for i in [0, buf_len).

	//******************************************
	// Estimate LPC AR coefficients
	//******************************************

	// Calculate windowed signal

	// First LA_LTP samples
	// x_buf_ptr = x_buf + buf_len - pitch_LPC_win_length
	xBufPtrOff := xBufOff + buf_len - psEnc.sCmn.pitch_LPC_win_length
	wsigOff := opus_int(0)
	silk_apply_sine_window_FLP(Wsig[wsigOff:], xBase[xBufPtrOff:], 1, psEnc.sCmn.la_pitch)

	// Middle non-windowed samples
	wsigOff += psEnc.sCmn.la_pitch
	xBufPtrOff += psEnc.sCmn.la_pitch
	// silk_memcpy( Wsig_ptr, x_buf_ptr, (pitch_LPC_win_length - (la_pitch<<1)) * sizeof(silk_float) );
	midLen := psEnc.sCmn.pitch_LPC_win_length - (psEnc.sCmn.la_pitch << 1)
	for i := opus_int(0); i < midLen; i++ {
		Wsig[wsigOff+i] = xBase[xBufPtrOff+i]
	}

	// Last LA_LTP samples
	wsigOff += midLen
	xBufPtrOff += midLen
	silk_apply_sine_window_FLP(Wsig[wsigOff:], xBase[xBufPtrOff:], 2, psEnc.sCmn.la_pitch)

	// Calculate autocorrelation sequence
	silk_autocorrelation_FLP(auto_corr[:], Wsig[:], psEnc.sCmn.pitch_LPC_win_length, psEnc.sCmn.pitchEstimationLPCOrder+1, arch)

	// Add white noise, as a fraction of the energy.
	// C: auto_corr[0] += auto_corr[0] * FIND_PITCH_WHITE_NOISE_FRACTION + 1;
	//   FIND_PITCH_WHITE_NOISE_FRACTION is 1e-3f (float). The + 1 adds
	//   1 to the float product before being added to auto_corr[0].
	//   Left-to-right: (a0*fwnf) + 1 -> float; then auto_corr[0] +=
	//   that.
	noise := mul_f32(auto_corr[0], FIND_PITCH_WHITE_NOISE_FRACTION)
	auto_corr[0] = add_f32(auto_corr[0], add_f32(noise, 1))

	// Calculate the reflection coefficients using Schur
	res_nrg = silk_schur_FLP(refl_coef[:], auto_corr[:], psEnc.sCmn.pitchEstimationLPCOrder)

	// Prediction gain
	// C: psEncCtrl->predGain = auto_corr[0] / silk_max_float( res_nrg, 1.0f );
	psEncCtrl.predGain = auto_corr[0] / silk_max_float(res_nrg, 1.0)

	// Convert reflection coefficients to prediction coefficients
	silk_k2a_FLP(A[:], refl_coef[:], opus_int32(psEnc.sCmn.pitchEstimationLPCOrder))

	// Bandwidth expansion
	silk_bwexpander_FLP(A[:], psEnc.sCmn.pitchEstimationLPCOrder, FIND_PITCH_BANDWIDTH_EXPANSION)

	//*****************************************
	// LPC analysis filtering
	//*****************************************
	silk_LPC_analysis_filter_FLP(res, A[:], xBase[xBufOff:], buf_len, psEnc.sCmn.pitchEstimationLPCOrder)

	if psEnc.sCmn.indices.signalType != TYPE_NO_VOICE_ACTIVITY && psEnc.sCmn.first_frame_after_reset == 0 {
		// Threshold for pitch estimator
		// C: thrhld  = 0.6f;
		//    thrhld -= 0.004f * pitchEstimationLPCOrder;
		//    thrhld -= 0.1f   * speech_activity_Q8 * ( 1.0f / 256.0f );
		//    thrhld -= 0.15f  * (prevSignalType >> 1);
		//    thrhld -= 0.1f   * input_tilt_Q15 * ( 1.0f / 32768.0f );
		// Each assignment: thrhld = thrhld - <float>. Left-to-right
		// mul then -.
		thrhld = 0.6
		thrhld = fma_sub(thrhld, 0.004, float32(psEnc.sCmn.pitchEstimationLPCOrder))
		// 0.1f * speech_activity_Q8 is float * int -> float; then * (1/256.0f).
		// Left-to-right: (0.1 * s) * (1/256). Use mul_f32 chain and fma_sub.
		t1 := mul_f32(mul_f32(0.1, float32(psEnc.sCmn.speech_activity_Q8)), 1.0/256.0)
		thrhld = sub_f32(thrhld, t1)
		// 0.15f * (prevSignalType >> 1) — the shift is integer; then
		// multiplied by float. Single mul, then sub.
		shifted := int(psEnc.sCmn.prevSignalType >> 1)
		t2 := mul_f32(0.15, float32(shifted))
		thrhld = sub_f32(thrhld, t2)
		// 0.1f * input_tilt_Q15 * (1.0f / 32768.0f)
		t3 := mul_f32(mul_f32(0.1, float32(psEnc.sCmn.input_tilt_Q15)), 1.0/32768.0)
		thrhld = sub_f32(thrhld, t3)

		//*****************************************
		// Call Pitch estimator
		//*****************************************
		if silk_pitch_analysis_core_FLP(res, psEncCtrl.pitchL[:], &psEnc.sCmn.indices.lagIndex,
			&psEnc.sCmn.indices.contourIndex, &psEnc.LTPCorr, psEnc.sCmn.prevLag,
			// pitchEstimationThreshold_Q16 / 65536.0f — float division,
			// numerator is int32 promoted to float.
			float32(psEnc.sCmn.pitchEstimationThreshold_Q16)/65536.0,
			thrhld, psEnc.sCmn.fs_kHz, psEnc.sCmn.pitchEstimationComplexity, psEnc.sCmn.nb_subfr, arch) == 0 {
			psEnc.sCmn.indices.signalType = TYPE_VOICED
		} else {
			psEnc.sCmn.indices.signalType = TYPE_UNVOICED
		}
	} else {
		for k := 0; k < len(psEncCtrl.pitchL); k++ {
			psEncCtrl.pitchL[k] = 0
		}
		psEnc.sCmn.indices.lagIndex = 0
		psEnc.sCmn.indices.contourIndex = 0
		psEnc.LTPCorr = 0
	}
}
