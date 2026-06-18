package nativeopus

// Port of libopus/silk/LPC_inv_pred_gain.c.

const (
	silk_inv_pred_QA = 24
)

var silk_inv_pred_A_LIMIT = SILK_FIX_CONST(0.99975, silk_inv_pred_QA)

// mul32_frac_Q — (opus_int32)silk_RSHIFT_ROUND64(silk_SMULL(a,b), Q).
func silk_inv_pred_mul32_frac_Q(a32, b32 opus_int32, Q opus_int) opus_int32 {
	return opus_int32(silk_RSHIFT_ROUND64(silk_SMULL(a32, b32), Q))
}

// LPC_inverse_pred_gain_QA_c — test LPC stability and compute inverse
// prediction gain. Returns Q30 inverse gain, or 0 if unstable.
func silk_LPC_inverse_pred_gain_QA_c(A_QA []opus_int32, order opus_int) opus_int32 {
	var k, n, mult2Q opus_int
	var invGain_Q30, rc_Q31, rc_mult1_Q30, rc_mult2, tmp1, tmp2 opus_int32

	invGain_Q30 = SILK_FIX_CONST(1, 30)
	for k = order - 1; k > 0; k-- {
		// Check for stability.
		if A_QA[k] > silk_inv_pred_A_LIMIT || A_QA[k] < -silk_inv_pred_A_LIMIT {
			return 0
		}

		// Set RC equal to negated AR coef.
		rc_Q31 = -silk_LSHIFT(A_QA[k], 31-silk_inv_pred_QA)

		// rc_mult1_Q30 range: [1, 2^30].
		rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31))
		silk_assert(rc_mult1_Q30 > (1 << 15))
		silk_assert(rc_mult1_Q30 <= (1 << 30))

		// Update inverse gain. invGain_Q30 range: [0, 2^30].
		invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2)
		silk_assert(invGain_Q30 >= 0)
		silk_assert(invGain_Q30 <= (1 << 30))
		if invGain_Q30 < SILK_FIX_CONST(1.0/MAX_PREDICTION_POWER_GAIN, 30) {
			return 0
		}

		// rc_mult2 range: [2^30, silk_int32_MAX].
		mult2Q = 32 - opus_int(silk_CLZ32(silk_abs(rc_mult1_Q30)))
		rc_mult2 = silk_INVERSE32_varQ(rc_mult1_Q30, mult2Q+30)

		// Update AR coefficient.
		for n = 0; n < (k+1)>>1; n++ {
			var tmp64 opus_int64
			tmp1 = A_QA[n]
			tmp2 = A_QA[k-n-1]
			tmp64 = silk_RSHIFT_ROUND64(silk_SMULL(
				silk_SUB_SAT32(tmp1, silk_inv_pred_mul32_frac_Q(tmp2, rc_Q31, 31)),
				rc_mult2), mult2Q)
			if tmp64 > opus_int64(silk_int32_MAX) || tmp64 < opus_int64(silk_int32_MIN) {
				return 0
			}
			A_QA[n] = opus_int32(tmp64)
			tmp64 = silk_RSHIFT_ROUND64(silk_SMULL(
				silk_SUB_SAT32(tmp2, silk_inv_pred_mul32_frac_Q(tmp1, rc_Q31, 31)),
				rc_mult2), mult2Q)
			if tmp64 > opus_int64(silk_int32_MAX) || tmp64 < opus_int64(silk_int32_MIN) {
				return 0
			}
			A_QA[k-n-1] = opus_int32(tmp64)
		}
	}

	// Check for stability.
	if A_QA[k] > silk_inv_pred_A_LIMIT || A_QA[k] < -silk_inv_pred_A_LIMIT {
		return 0
	}

	// Set RC equal to negated AR coef.
	rc_Q31 = -silk_LSHIFT(A_QA[0], 31-silk_inv_pred_QA)

	// Range: [1, 2^30].
	rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31))

	// Update inverse gain. Range: [0, 2^30].
	invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2)
	silk_assert(invGain_Q30 >= 0)
	silk_assert(invGain_Q30 <= (1 << 30))
	if invGain_Q30 < SILK_FIX_CONST(1.0/MAX_PREDICTION_POWER_GAIN, 30) {
		return 0
	}

	return invGain_Q30
}

// silk_LPC_inverse_pred_gain_c — For input in Q12 domain.
func silk_LPC_inverse_pred_gain_c(A_Q12 []opus_int16, order opus_int) opus_int32 {
	var Atmp_QA [SILK_MAX_ORDER_LPC]opus_int32
	var DC_resp opus_int32

	// Increase Q domain of the AR coefficients.
	for k := opus_int(0); k < order; k++ {
		DC_resp += opus_int32(A_Q12[k])
		Atmp_QA[k] = silk_LSHIFT32(opus_int32(A_Q12[k]), silk_inv_pred_QA-12)
	}
	// If the DC is unstable, skip the full calculation.
	if DC_resp >= 4096 {
		return 0
	}
	return silk_LPC_inverse_pred_gain_QA_c(Atmp_QA[:], order)
}
