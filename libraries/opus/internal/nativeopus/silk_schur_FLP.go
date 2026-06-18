package nativeopus

// 1:1 port of libopus/silk/float/schur_FLP.c.
// Computes reflection coefficients from an autocorrelation sequence
// using a Schur recursion. All intermediates are double.

func silk_schur_FLP(refl_coef, auto_corr []silk_float, order opus_int) silk_float {
	var k, n opus_int
	// C[SILK_MAX_ORDER_LPC+1][2]
	var C [SILK_MAX_ORDER_LPC + 1][2]float64
	var Ctmp1, Ctmp2, rc_tmp float64

	// Copy correlations.
	k = 0
	for {
		C[k][0] = float64(auto_corr[k])
		C[k][1] = float64(auto_corr[k])
		k++
		if k > order {
			break
		}
	}

	for k = 0; k < order; k++ {
		// C[0][1] may underflow to very small; clamp at 1e-9. C uses
		// silk_max_float which is `((a) > (b)) ? (a) : (b)`.
		denom := C[0][1]
		if denom < 1e-9 {
			denom = 1e-9
		}
		rc_tmp = -C[k+1][0] / denom

		refl_coef[k] = silk_float(rc_tmp)

		// Update correlations.
		for n = 0; n < order-k; n++ {
			Ctmp1 = C[n+k+1][0]
			Ctmp2 = C[n][1]
			// C: C[n+k+1][0] = Ctmp1 + Ctmp2 * rc_tmp;
			// C: C[n][1]     = Ctmp2 + Ctmp1 * rc_tmp;
			C[n+k+1][0] = fma_add64(Ctmp1, Ctmp2, rc_tmp)
			C[n][1] = fma_add64(Ctmp2, Ctmp1, rc_tmp)
		}
	}

	return silk_float(C[0][1])
}
