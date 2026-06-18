package nativeopus

import "math"

// 1:1 port of libopus/silk/float/noise_shape_analysis_FLP.c.
// Noise shaping analysis driver for the SILK float encoder path.
// Computes gain, bandwidth-expanded AR coefficients, warped/unwarped
// auto-correlations, LF/AR/MA shaping parameters, tilt, and harmonic
// shaping smoothing. Heavy FP arithmetic; every `a + b*c` / `a - b*c`
// near a multiplication is routed through fma_add / fma_sub to match
// the C oracle compiled with -ffp-contract=off.

// silk_abs_float — C: ((silk_float)fabs(a)). Double-precision fabs
// then narrowed to float32 at the cast boundary.
func silk_abs_float(a silk_float) silk_float {
	return silk_float(math.Abs(float64(a)))
}

// warped_gain — C static OPUS_INLINE helper; computes gain to make
// warped filter coefficients have a zero mean log frequency response
// on a non-warped frequency scale.
func warped_gain(coefs []silk_float, lambda silk_float, order opus_int) silk_float {
	var i opus_int
	var gain silk_float

	lambda = -lambda
	gain = coefs[order-1]
	for i = order - 2; i >= 0; i-- {
		// C: gain = lambda * gain + coefs[i];
		// Left-to-right fused mul+add; prevent fusion.
		gain = fma_add(coefs[i], lambda, gain)
	}
	// C: (silk_float)( 1.0f / ( 1.0f - lambda * gain ) )
	denom := fma_sub(1.0, lambda, gain)
	return silk_float(1.0 / denom)
}

// warped_true2monic_coefs — C static OPUS_INLINE helper.
// Convert warped filter coefficients to monic pseudo-warped coefficients
// and limit maximum amplitude by bandwidth expansion on true coefs.
func warped_true2monic_coefs(coefs []silk_float, lambda silk_float, limit silk_float, order opus_int) {
	var i, iter opus_int
	var ind opus_int = 0
	var tmp, maxabs, chirp, gain silk_float

	// Convert to monic coefficients
	for i = order - 1; i > 0; i-- {
		// C: coefs[ i - 1 ] -= lambda * coefs[ i ];
		coefs[i-1] = fma_sub(coefs[i-1], lambda, coefs[i])
	}
	// C: gain = ( 1.0f - lambda * lambda ) / ( 1.0f + lambda * coefs[ 0 ] );
	num := fma_sub(1.0, lambda, lambda)
	den := fma_add(1.0, lambda, coefs[0])
	gain = num / den
	for i = 0; i < order; i++ {
		coefs[i] = mul_f32(coefs[i], gain)
	}

	// Limit
	for iter = 0; iter < 10; iter++ {
		// Find maximum absolute value
		maxabs = -1.0
		for i = 0; i < order; i++ {
			tmp = silk_abs_float(coefs[i])
			if tmp > maxabs {
				maxabs = tmp
				ind = i
			}
		}
		if maxabs <= limit {
			// Coefficients are within range - done
			return
		}

		// Convert back to true warped coefficients
		for i = 1; i < order; i++ {
			// C: coefs[ i - 1 ] += lambda * coefs[ i ];
			coefs[i-1] = fma_add(coefs[i-1], lambda, coefs[i])
		}
		gain = 1.0 / gain
		for i = 0; i < order; i++ {
			coefs[i] = mul_f32(coefs[i], gain)
		}

		// Apply bandwidth expansion
		// C: chirp = 0.99f - ( 0.8f + 0.1f * iter ) * ( maxabs - limit ) / ( maxabs * ( ind + 1 ) );
		// Order: inner muls first, left-to-right. Wrap each +/- near a mul.
		innerA := fma_add(0.8, 0.1, float32(iter)) // 0.8 + 0.1*iter
		diff := sub_f32(maxabs, limit)             // maxabs - limit
		numer := mul_f32(innerA, diff)             // (...)*(maxabs-limit)
		denom := mul_f32(maxabs, float32(ind+1))   // maxabs*(ind+1)
		quot := numer / denom
		chirp = sub_f32(0.99, quot)
		silk_bwexpander_FLP(coefs, order, chirp)

		// Convert to monic warped coefficients
		for i = order - 1; i > 0; i-- {
			coefs[i-1] = fma_sub(coefs[i-1], lambda, coefs[i])
		}
		num = fma_sub(1.0, lambda, lambda)
		den = fma_add(1.0, lambda, coefs[0])
		gain = num / den
		for i = 0; i < order; i++ {
			coefs[i] = mul_f32(coefs[i], gain)
		}
	}
	silk_assert(false)
}

// limit_coefs — C static OPUS_INLINE helper.
func limit_coefs(coefs []silk_float, limit silk_float, order opus_int) {
	var i, iter opus_int
	var ind opus_int = 0
	var tmp, maxabs, chirp silk_float

	for iter = 0; iter < 10; iter++ {
		// Find maximum absolute value
		maxabs = -1.0
		for i = 0; i < order; i++ {
			tmp = silk_abs_float(coefs[i])
			if tmp > maxabs {
				maxabs = tmp
				ind = i
			}
		}
		if maxabs <= limit {
			// Coefficients are within range - done
			return
		}

		// Apply bandwidth expansion
		// C: chirp = 0.99f - ( 0.8f + 0.1f * iter ) * ( maxabs - limit ) / ( maxabs * ( ind + 1 ) );
		innerA := fma_add(0.8, 0.1, float32(iter))
		diff := sub_f32(maxabs, limit)
		numer := mul_f32(innerA, diff)
		denom := mul_f32(maxabs, float32(ind+1))
		quot := numer / denom
		chirp = sub_f32(0.99, quot)
		silk_bwexpander_FLP(coefs, order, chirp)
	}
	silk_assert(false)
}

// silk_noise_shape_analysis_FLP — Compute noise shaping coefficients
// and initial gain values.
func silk_noise_shape_analysis_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	pitch_res []silk_float,
	x []silk_float,
) {
	silk_noise_shape_analysis_FLP_withBase(psEnc, psEncCtrl, pitch_res, x, 0)
}

// silk_noise_shape_analysis_FLP_withBase — like silk_noise_shape_analysis_FLP
// but takes an explicit offset into `x` so C-side `x - la_shape` can be
// expressed as negative offset arithmetic on a Go-side backing slice.
func silk_noise_shape_analysis_FLP_withBase(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	pitch_res []silk_float,
	xBase []silk_float,
	xOff opus_int,
) {
	psShapeSt := &psEnc.sShape
	var k, nSamples, nSegs opus_int
	var SNR_adj_dB, HarmShapeGain, Tilt silk_float
	var nrg, log_energy, log_energy_prev, energy_variation silk_float
	var BWExp, gain_mult, gain_add, strength, b, warping silk_float
	var x_windowed [SHAPE_LPC_WIN_MAX]silk_float
	var auto_corr [MAX_SHAPE_LPC_ORDER + 1]silk_float
	var rc [MAX_SHAPE_LPC_ORDER + 1]silk_float

	// Point to start of first LPC analysis block
	// C: x_ptr = x - psEnc->sCmn.la_shape;
	// Expressed as an offset into xBase.
	x_ptr_off := xOff - psEnc.sCmn.la_shape

	/****************/
	/* GAIN CONTROL */
	/****************/
	// C: SNR_adj_dB = psEnc->sCmn.SNR_dB_Q7 * ( 1 / 128.0f );
	// Integer times float — becomes single mul.
	SNR_adj_dB = mul_f32(float32(psEnc.sCmn.SNR_dB_Q7), 1.0/128.0)

	// Input quality is the average of the quality in the lowest two VAD bands
	// C: psEncCtrl->input_quality = 0.5f * ( input_quality_bands_Q15[0] + input_quality_bands_Q15[1] ) * ( 1.0f / 32768.0f );
	// Integer add first, then float mul chain left-to-right.
	iqSum := psEnc.sCmn.input_quality_bands_Q15[0] + psEnc.sCmn.input_quality_bands_Q15[1]
	tmpIQ := mul_f32(0.5, float32(iqSum))
	psEncCtrl.input_quality = mul_f32(tmpIQ, 1.0/32768.0)

	// C: psEncCtrl->coding_quality = silk_sigmoid( 0.25f * ( SNR_adj_dB - 20.0f ) );
	sigArg := mul_f32(0.25, sub_f32(SNR_adj_dB, 20.0))
	psEncCtrl.coding_quality = silk_sigmoid_flp(sigArg)

	if psEnc.sCmn.useCBR == 0 {
		// Reduce coding SNR during low speech activity
		// C: b = 1.0f - psEnc->sCmn.speech_activity_Q8 * ( 1.0f / 256.0f );
		// Left-to-right: int->float mul then sub.
		b = fma_sub(1.0, float32(psEnc.sCmn.speech_activity_Q8), 1.0/256.0)
		// C: SNR_adj_dB -= BG_SNR_DECR_dB * coding_quality * ( 0.5f + 0.5f * input_quality ) * b * b;
		// Left-to-right: (((BG * cq) * (0.5 + 0.5*iq)) * b) * b
		inner := fma_add(0.5, 0.5, psEncCtrl.input_quality) // 0.5 + 0.5*iq
		t := mul_f32(BG_SNR_DECR_dB, psEncCtrl.coding_quality)
		t = mul_f32(t, inner)
		t = mul_f32(t, b)
		t = mul_f32(t, b)
		SNR_adj_dB = sub_f32(SNR_adj_dB, t)
	}

	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// Reduce gains for periodic signals
		// C: SNR_adj_dB += HARM_SNR_INCR_dB * psEnc->LTPCorr;
		SNR_adj_dB = fma_add(SNR_adj_dB, HARM_SNR_INCR_dB, psEnc.LTPCorr)
	} else {
		// For unvoiced signals and low-quality input, adjust the quality slower than SNR_dB setting
		// C: SNR_adj_dB += ( -0.4f * psEnc->sCmn.SNR_dB_Q7 * ( 1 / 128.0f ) + 6.0f ) * ( 1.0f - psEncCtrl->input_quality );
		// Left-to-right inside: ((-0.4 * SNR_Q7) * (1/128)) + 6
		a1 := mul_f32(-0.4, float32(psEnc.sCmn.SNR_dB_Q7))
		a2 := mul_f32(a1, 1.0/128.0)
		a3 := add_f32(a2, 6.0)
		// (1.0 - input_quality)
		a4 := sub_f32(1.0, psEncCtrl.input_quality)
		SNR_adj_dB = fma_add(SNR_adj_dB, a3, a4)
	}

	/*************************/
	/* SPARSENESS PROCESSING */
	/*************************/
	// Set quantizer offset
	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// Initially set to 0; may be overruled in process_gains(..)
		psEnc.sCmn.indices.quantOffsetType = 0
	} else {
		// Sparseness measure, based on relative fluctuations of energy per 2 milliseconds
		nSamples = 2 * psEnc.sCmn.fs_kHz
		energy_variation = 0.0
		log_energy_prev = 0.0
		pitch_res_off := opus_int(0)
		// C: nSegs = silk_SMULBB( SUB_FRAME_LENGTH_MS, psEnc->sCmn.nb_subfr ) / 2;
		nSegs = opus_int(silk_SMULBB(SUB_FRAME_LENGTH_MS, opus_int32(psEnc.sCmn.nb_subfr)) / 2)
		for k = 0; k < nSegs; k++ {
			// C: nrg = (silk_float)nSamples + (silk_float)silk_energy_FLP( pitch_res_ptr, nSamples );
			energyF := silk_float(silk_energy_FLP(pitch_res[pitch_res_off:], nSamples))
			nrg = add_f32(float32(nSamples), energyF)
			// C: log_energy = silk_log2( nrg );
			log_energy = silk_log2(float64(nrg))
			if k > 0 {
				// C: energy_variation += silk_abs_float( log_energy - log_energy_prev );
				diff := sub_f32(log_energy, log_energy_prev)
				energy_variation = add_f32(energy_variation, silk_abs_float(diff))
			}
			log_energy_prev = log_energy
			pitch_res_off += nSamples
		}

		// Set quantization offset depending on sparseness measure
		// C: if( energy_variation > ENERGY_VARIATION_THRESHOLD_QNT_OFFSET * (nSegs-1) )
		thr := mul_f32(ENERGY_VARIATION_THRESHOLD_QNT_OFFSET, float32(nSegs-1))
		if energy_variation > thr {
			psEnc.sCmn.indices.quantOffsetType = 0
		} else {
			psEnc.sCmn.indices.quantOffsetType = 1
		}
	}

	/*******************************/
	/* Control bandwidth expansion */
	/*******************************/
	// More BWE for signals with high prediction gain
	// C: strength = FIND_PITCH_WHITE_NOISE_FRACTION * psEncCtrl->predGain;
	strength = mul_f32(FIND_PITCH_WHITE_NOISE_FRACTION, psEncCtrl.predGain)
	// C: BWExp = BANDWIDTH_EXPANSION / ( 1.0f + strength * strength );
	bwDen := fma_add(1.0, strength, strength)
	BWExp = BANDWIDTH_EXPANSION / bwDen

	// Slightly more warping in analysis will move quantization noise up in frequency
	// C: warping = (silk_float)psEnc->sCmn.warping_Q16 / 65536.0f + 0.01f * psEncCtrl->coding_quality;
	warpFromQ16 := float32(psEnc.sCmn.warping_Q16) / 65536.0
	warping = fma_add(warpFromQ16, 0.01, psEncCtrl.coding_quality)

	/********************************************/
	/* Compute noise shaping AR coefs and gains */
	/********************************************/
	for k = 0; k < psEnc.sCmn.nb_subfr; k++ {
		// Apply window: sine slope followed by flat part followed by cosine slope
		var shift, slope_part, flat_part opus_int
		flat_part = psEnc.sCmn.fs_kHz * 3
		slope_part = (psEnc.sCmn.shapeWinLength - flat_part) / 2

		silk_apply_sine_window_FLP(x_windowed[:], xBase[x_ptr_off:], 1, slope_part)
		shift = slope_part
		// silk_memcpy( x_windowed + shift, x_ptr + shift, flat_part * sizeof(silk_float) );
		for i := opus_int(0); i < flat_part; i++ {
			x_windowed[shift+i] = xBase[x_ptr_off+shift+i]
		}
		shift += flat_part
		silk_apply_sine_window_FLP(x_windowed[shift:], xBase[x_ptr_off+shift:], 2, slope_part)

		// Update pointer: next LPC analysis block
		x_ptr_off += psEnc.sCmn.subfr_length

		if psEnc.sCmn.warping_Q16 > 0 {
			// Calculate warped auto correlation
			silk_warped_autocorrelation_FLP_c(auto_corr[:], x_windowed[:], warping,
				psEnc.sCmn.shapeWinLength, psEnc.sCmn.shapingLPCOrder)
		} else {
			// Calculate regular auto correlation
			silk_autocorrelation_FLP(auto_corr[:], x_windowed[:], psEnc.sCmn.shapeWinLength, psEnc.sCmn.shapingLPCOrder+1, psEnc.sCmn.arch)
		}

		// Add white noise, as a fraction of energy
		// C: auto_corr[ 0 ] += auto_corr[ 0 ] * SHAPE_WHITE_NOISE_FRACTION + 1.0f;
		noise := mul_f32(auto_corr[0], SHAPE_WHITE_NOISE_FRACTION)
		auto_corr[0] = add_f32(auto_corr[0], add_f32(noise, 1.0))

		// Convert correlations to prediction coefficients, and compute residual energy
		nrg = silk_schur_FLP(rc[:], auto_corr[:], psEnc.sCmn.shapingLPCOrder)
		silk_k2a_FLP(psEncCtrl.AR[k*MAX_SHAPE_LPC_ORDER:], rc[:], opus_int32(psEnc.sCmn.shapingLPCOrder))
		// C: psEncCtrl->Gains[ k ] = ( silk_float )sqrt( nrg );
		// sqrt((double)nrg) then narrowed to float32.
		psEncCtrl.Gains[k] = silk_float(math.Sqrt(float64(nrg)))

		if psEnc.sCmn.warping_Q16 > 0 {
			// Adjust gain for warping
			g := warped_gain(psEncCtrl.AR[k*MAX_SHAPE_LPC_ORDER:], warping, psEnc.sCmn.shapingLPCOrder)
			psEncCtrl.Gains[k] = mul_f32(psEncCtrl.Gains[k], g)
		}

		// Bandwidth expansion for synthesis filter shaping
		silk_bwexpander_FLP(psEncCtrl.AR[k*MAX_SHAPE_LPC_ORDER:], psEnc.sCmn.shapingLPCOrder, BWExp)

		if psEnc.sCmn.warping_Q16 > 0 {
			// Convert to monic warped prediction coefficients and limit absolute values
			warped_true2monic_coefs(psEncCtrl.AR[k*MAX_SHAPE_LPC_ORDER:], warping, 3.999, psEnc.sCmn.shapingLPCOrder)
		} else {
			// Limit absolute values
			limit_coefs(psEncCtrl.AR[k*MAX_SHAPE_LPC_ORDER:], 3.999, psEnc.sCmn.shapingLPCOrder)
		}
	}

	/*****************/
	/* Gain tweaking */
	/*****************/
	// Increase gains during low speech activity
	// C: gain_mult = (silk_float)pow( 2.0f, -0.16f * SNR_adj_dB );
	// C: gain_add  = (silk_float)pow( 2.0f,  0.16f * MIN_QGAIN_DB );
	// pow is double; both operands of the inner mul are float (SNR_adj_dB
	// is float; MIN_QGAIN_DB is int literal -> promoted to double at the
	// '*' with 0.16f? In C, 0.16f * 2 (int) — the int promotes to float.
	// So compute float first, then pass to pow as double.
	gainMultArg := mul_f32(-0.16, SNR_adj_dB)
	gain_mult = silk_float(math.Pow(2.0, float64(gainMultArg)))
	gainAddArg := mul_f32(0.16, silk_float(MIN_QGAIN_DB))
	gain_add = silk_float(math.Pow(2.0, float64(gainAddArg)))
	for k = 0; k < psEnc.sCmn.nb_subfr; k++ {
		psEncCtrl.Gains[k] = mul_f32(psEncCtrl.Gains[k], gain_mult)
		psEncCtrl.Gains[k] = add_f32(psEncCtrl.Gains[k], gain_add)
	}

	/************************************************/
	/* Control low-frequency shaping and noise tilt */
	/************************************************/
	// Less low frequency shaping for noisy inputs
	// C: strength = LOW_FREQ_SHAPING * ( 1.0f + LOW_QUALITY_LOW_FREQ_SHAPING_DECR *
	//                 ( input_quality_bands_Q15[0] * ( 1.0f / 32768.0f ) - 1.0f ) );
	iqb0f := mul_f32(float32(psEnc.sCmn.input_quality_bands_Q15[0]), 1.0/32768.0)
	iqb0m1 := sub_f32(iqb0f, 1.0)
	innerStr := fma_add(1.0, LOW_QUALITY_LOW_FREQ_SHAPING_DECR, iqb0m1)
	strength = mul_f32(LOW_FREQ_SHAPING, innerStr)
	// C: strength *= psEnc->sCmn.speech_activity_Q8 * ( 1.0f / 256.0f );
	saF := mul_f32(float32(psEnc.sCmn.speech_activity_Q8), 1.0/256.0)
	strength = mul_f32(strength, saF)
	if psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// Reduce low frequencies quantization noise for periodic signals, depending on pitch lag
		for k = 0; k < psEnc.sCmn.nb_subfr; k++ {
			// C: b = 0.2f / psEnc->sCmn.fs_kHz + 3.0f / psEncCtrl->pitchL[ k ];
			// Each div is float / (int promoted to float). Two divs then add.
			bA := silk_float(0.2) / float32(psEnc.sCmn.fs_kHz)
			bB := silk_float(3.0) / float32(psEncCtrl.pitchL[k])
			b = add_f32(bA, bB)
			// C: psEncCtrl->LF_MA_shp[ k ] = -1.0f + b;
			psEncCtrl.LF_MA_shp[k] = add_f32(-1.0, b)
			// C: psEncCtrl->LF_AR_shp[ k ] =  1.0f - b - b * strength;
			// Left-to-right: (1.0 - b) - b*strength
			t := sub_f32(1.0, b)
			psEncCtrl.LF_AR_shp[k] = fma_sub(t, b, strength)
		}
		// C: Tilt = - HP_NOISE_COEF -
		//         (1 - HP_NOISE_COEF) * HARM_HP_NOISE_COEF * psEnc->sCmn.speech_activity_Q8 * ( 1.0f / 256.0f );
		// Left-to-right: ((((1-hpn) * hhpn) * sa) * (1/256))
		oneMinusHPN := sub_f32(1.0, HP_NOISE_COEF)
		tiltTerm := mul_f32(oneMinusHPN, HARM_HP_NOISE_COEF)
		tiltTerm = mul_f32(tiltTerm, float32(psEnc.sCmn.speech_activity_Q8))
		tiltTerm = mul_f32(tiltTerm, 1.0/256.0)
		Tilt = sub_f32(-HP_NOISE_COEF, tiltTerm)
	} else {
		// C: b = 1.3f / psEnc->sCmn.fs_kHz;
		b = silk_float(1.3) / float32(psEnc.sCmn.fs_kHz)
		// C: psEncCtrl->LF_MA_shp[ 0 ] = -1.0f + b;
		psEncCtrl.LF_MA_shp[0] = add_f32(-1.0, b)
		// C: psEncCtrl->LF_AR_shp[ 0 ] =  1.0f - b - b * strength * 0.6f;
		// Left-to-right: ((b * strength) * 0.6) -> inner; then (1 - b) - inner.
		inner := mul_f32(mul_f32(b, strength), 0.6)
		t := sub_f32(1.0, b)
		psEncCtrl.LF_AR_shp[0] = sub_f32(t, inner)
		for k = 1; k < psEnc.sCmn.nb_subfr; k++ {
			psEncCtrl.LF_MA_shp[k] = psEncCtrl.LF_MA_shp[0]
			psEncCtrl.LF_AR_shp[k] = psEncCtrl.LF_AR_shp[0]
		}
		Tilt = -HP_NOISE_COEF
	}

	/****************************/
	/* HARMONIC SHAPING CONTROL */
	/****************************/
	if USE_HARM_SHAPING != 0 && psEnc.sCmn.indices.signalType == TYPE_VOICED {
		// Harmonic noise shaping
		HarmShapeGain = HARMONIC_SHAPING

		// More harmonic noise shaping for high bitrates or noisy input
		// C: HarmShapeGain += HIGH_RATE_OR_LOW_QUALITY_HARMONIC_SHAPING *
		//                    ( 1.0f - ( 1.0f - coding_quality ) * input_quality );
		// Inner: (1 - coding_quality) -> tmp; then 1 - tmp*input_quality
		oneMinusCQ := sub_f32(1.0, psEncCtrl.coding_quality)
		// innerHS = 1.0 - oneMinusCQ*input_quality. Use fma_sub(a, b, c) = a - b*c.
		innerHS := fma_sub(1.0, oneMinusCQ, psEncCtrl.input_quality)
		HarmShapeGain = fma_add(HarmShapeGain, HIGH_RATE_OR_LOW_QUALITY_HARMONIC_SHAPING, innerHS)

		// Less harmonic noise shaping for less periodic signals
		// C: HarmShapeGain *= ( silk_float )sqrt( psEnc->LTPCorr );
		sqrtLTP := silk_float(math.Sqrt(float64(psEnc.LTPCorr)))
		HarmShapeGain = mul_f32(HarmShapeGain, sqrtLTP)
	} else {
		HarmShapeGain = 0.0
	}

	/*************************/
	/* Smooth over subframes */
	/*************************/
	for k = 0; k < psEnc.sCmn.nb_subfr; k++ {
		// C: psShapeSt->HarmShapeGain_smth += SUBFR_SMTH_COEF * ( HarmShapeGain - HarmShapeGain_smth );
		diff := sub_f32(HarmShapeGain, psShapeSt.HarmShapeGain_smth)
		psShapeSt.HarmShapeGain_smth = fma_add(psShapeSt.HarmShapeGain_smth, SUBFR_SMTH_COEF, diff)
		psEncCtrl.HarmShapeGain[k] = psShapeSt.HarmShapeGain_smth
		// C: psShapeSt->Tilt_smth += SUBFR_SMTH_COEF * ( Tilt - Tilt_smth );
		diffT := sub_f32(Tilt, psShapeSt.Tilt_smth)
		psShapeSt.Tilt_smth = fma_add(psShapeSt.Tilt_smth, SUBFR_SMTH_COEF, diffT)
		psEncCtrl.Tilt[k] = psShapeSt.Tilt_smth
	}
}
