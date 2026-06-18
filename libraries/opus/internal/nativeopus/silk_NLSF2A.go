package nativeopus

// Port of libopus/silk/NLSF2A.c.

const silk_NLSF2A_QA = 16

// silk_NLSF2A_find_poly — helper for NLSF2A.
func silk_NLSF2A_find_poly(out, cLSF []opus_int32, dd opus_int) {
	out[0] = silk_LSHIFT(1, silk_NLSF2A_QA)
	out[1] = -cLSF[0]
	for k := opus_int(1); k < dd; k++ {
		ftmp := cLSF[2*k]
		out[k+1] = silk_LSHIFT(out[k-1], 1) -
			opus_int32(silk_RSHIFT_ROUND64(silk_SMULL(ftmp, out[k]), silk_NLSF2A_QA))
		for n := k; n > 1; n-- {
			out[n] += out[n-2] -
				opus_int32(silk_RSHIFT_ROUND64(silk_SMULL(ftmp, out[n-1]), silk_NLSF2A_QA))
		}
		out[1] -= ftmp
	}
}

// ordering tables that maximize numerical accuracy.
var silk_NLSF2A_ordering16 = [16]uint8{0, 15, 8, 7, 4, 11, 12, 3, 2, 13, 10, 5, 6, 9, 14, 1}
var silk_NLSF2A_ordering10 = [10]uint8{0, 9, 6, 3, 4, 5, 8, 1, 2, 7}

// silk_NLSF2A — compute whitening filter coefficients from normalized
// line spectral frequencies.
func silk_NLSF2A(a_Q12 []opus_int16, NLSF []opus_int16, d opus_int, arch int) {
	silk_assert(LSF_COS_TAB_SZ_FIX == 128)
	celt_assert(d == 10 || d == 16)

	var ordering []uint8
	if d == 16 {
		ordering = silk_NLSF2A_ordering16[:]
	} else {
		ordering = silk_NLSF2A_ordering10[:]
	}

	var cos_LSF_QA [SILK_MAX_ORDER_LPC]opus_int32
	for k := opus_int(0); k < d; k++ {
		silk_assert(NLSF[k] >= 0)
		f_int := silk_RSHIFT(opus_int32(NLSF[k]), 15-7)
		f_frac := opus_int32(NLSF[k]) - silk_LSHIFT(f_int, 15-7)
		silk_assert(f_int >= 0)
		silk_assert(f_int < LSF_COS_TAB_SZ_FIX)

		cos_val := opus_int32(silk_LSFCosTab_FIX_Q12[f_int])
		delta := opus_int32(silk_LSFCosTab_FIX_Q12[f_int+1]) - cos_val

		cos_LSF_QA[ordering[k]] = silk_RSHIFT_ROUND(
			silk_LSHIFT(cos_val, 8)+silk_MUL(delta, f_frac), 20-silk_NLSF2A_QA)
	}

	dd := silk_RSHIFT(opus_int32(d), 1)

	var P [SILK_MAX_ORDER_LPC/2 + 1]opus_int32
	var Q [SILK_MAX_ORDER_LPC/2 + 1]opus_int32
	silk_NLSF2A_find_poly(P[:], cos_LSF_QA[0:], opus_int(dd))
	silk_NLSF2A_find_poly(Q[:], cos_LSF_QA[1:], opus_int(dd))

	var a32_QA1 [SILK_MAX_ORDER_LPC]opus_int32
	for k := opus_int(0); k < opus_int(dd); k++ {
		Ptmp := P[k+1] + P[k]
		Qtmp := Q[k+1] - Q[k]
		a32_QA1[k] = -Qtmp - Ptmp
		a32_QA1[d-k-1] = Qtmp - Ptmp
	}

	silk_LPC_fit(a_Q12, a32_QA1[:], 12, silk_NLSF2A_QA+1, d)

	for i := 0; silk_LPC_inverse_pred_gain_c(a_Q12, d) == 0 && i < MAX_LPC_STABILIZE_ITERATIONS; i++ {
		silk_bwexpander_32(a32_QA1[:], d, 65536-silk_LSHIFT(2, i))
		for k := opus_int(0); k < d; k++ {
			a_Q12[k] = opus_int16(silk_RSHIFT_ROUND(a32_QA1[k], silk_NLSF2A_QA+1-12))
		}
	}
	_ = arch
}
