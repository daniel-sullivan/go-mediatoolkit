package nativeopus

// 1:1 port of libopus/silk/float/LPC_inv_pred_gain_FLP.c.
// Tests LPC stability and returns the inverse prediction gain.
// Internal math is double; source coefficients are float32.

func silk_LPC_inverse_pred_gain_FLP(A []silk_float, order opus_int32) silk_float {
	var invGain, rc, rc_mult1, rc_mult2, tmp1, tmp2 float64
	var Atmp [SILK_MAX_ORDER_LPC]silk_float

	// silk_memcpy(Atmp, A, order*sizeof(silk_float))
	copy(Atmp[:order], A[:order])

	invGain = 1.0
	for k := order - 1; k > 0; k-- {
		rc = -float64(Atmp[k])
		// C: rc_mult1 = 1.0f - rc * rc  (double context).
		rc_mult1 = fma_sub64(1.0, rc, rc)
		invGain *= rc_mult1
		if invGain*MAX_PREDICTION_POWER_GAIN < 1.0 {
			return 0.0
		}
		rc_mult2 = 1.0 / rc_mult1
		for n := opus_int32(0); n < (k+1)>>1; n++ {
			tmp1 = float64(Atmp[n])
			tmp2 = float64(Atmp[k-n-1])
			// Atmp[n]       = (tmp1 - tmp2*rc) * rc_mult2
			// Atmp[k-n-1]   = (tmp2 - tmp1*rc) * rc_mult2
			Atmp[n] = silk_float(fma_sub64(tmp1, tmp2, rc) * rc_mult2)
			Atmp[k-n-1] = silk_float(fma_sub64(tmp2, tmp1, rc) * rc_mult2)
		}
	}
	rc = -float64(Atmp[0])
	rc_mult1 = fma_sub64(1.0, rc, rc)
	invGain *= rc_mult1
	if invGain*MAX_PREDICTION_POWER_GAIN < 1.0 {
		return 0.0
	}
	return silk_float(invGain)
}
