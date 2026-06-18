package nativeopus

import "math"

// 1:1 port of libopus/silk/float/burg_modified_FLP.c.
// Computes reflection (parcor) coefficients from an input signal via
// the modified covariance (Burg) method. All intermediates are double;
// the C oracle is compiled with -ffp-contract=off so every a+b*c and
// a-b*c is separately rounded. Use fma_add64 / fma_sub64 on the Go
// side to match, and keep the source-level left-to-right grouping.

// MAX_FRAME_SIZE in burg_modified_FLP.c — `subfr_length * nb_subfr`
// bounded by 0.005*16000 + 16 = 96 samples per subframe * 4 = 384.
const silkBurgMaxFrameSize = 384

func silk_burg_modified_FLP(
	A []silk_float,
	x []silk_float,
	minInvGain silk_float,
	subfr_length, nb_subfr, D opus_int,
	arch int,
) silk_float {
	_ = arch
	var k, n, s opus_int
	var reached_max_gain int
	var C0, invGain, num, nrg_f, nrg_b, rc, Atmp, tmp1, tmp2 float64
	var C_first_row, C_last_row [SILK_MAX_ORDER_LPC]float64
	var CAf, CAb [SILK_MAX_ORDER_LPC + 1]float64
	var Af [SILK_MAX_ORDER_LPC]float64

	// C0 = silk_energy_FLP(x, nb_subfr*subfr_length)
	C0 = silk_energy_FLP(x, nb_subfr*subfr_length)

	// C_first_row zeroed by Go's zero-valued arrays.
	for s = 0; s < nb_subfr; s++ {
		xPtr := s * subfr_length
		for n = 1; n < D+1; n++ {
			C_first_row[n-1] = add_f64(C_first_row[n-1],
				silk_inner_product_FLP(x[xPtr:], x[xPtr+n:], subfr_length-n, arch))
		}
	}
	// silk_memcpy(C_last_row, C_first_row, ...)
	C_last_row = C_first_row

	// CAb[0] = CAf[0] = C0 + FIND_LPC_COND_FAC * C0 + 1e-9f
	//   C left-to-right: (C0 + FIND_LPC_COND_FAC*C0) + 1e-9f.
	//   Note: FIND_LPC_COND_FAC is 1e-5 in both sides. 1e-9f promotes
	//   to double.
	// C's `FIND_LPC_COND_FAC` is `1e-5f` (float32) and `1e-9f` is
	// float32. In `C0 + FIND_LPC_COND_FAC * C0 + 1e-9f`, the float
	// constants promote to double for the mul/add on C0. To reproduce
	// the exact rounded constant, narrow through float32 then widen.
	findLPCCondFac := float64(float32(FIND_LPC_COND_FAC))
	init := C0 + mul_f64(findLPCCondFac, C0)
	init = add_f64(init, float64(float32(1e-9)))
	CAf[0] = init
	CAb[0] = init
	invGain = 1.0
	reached_max_gain = 0

	for n = 0; n < D; n++ {
		// First pair of nested loops over subframes.
		for s = 0; s < nb_subfr; s++ {
			xPtr := s * subfr_length
			tmp1 = float64(x[xPtr+n])
			tmp2 = float64(x[xPtr+subfr_length-n-1])
			for k = 0; k < n; k++ {
				// C: C_first_row[k] -= x_ptr[n] * x_ptr[n-k-1];
				//    C_last_row[k]  -= x_ptr[subfr_length-n-1]*x_ptr[subfr_length-n+k];
				//    Atmp = Af[k];
				//    tmp1 += x_ptr[n-k-1] * Atmp;
				//    tmp2 += x_ptr[subfr_length-n+k] * Atmp;
				// x_ptr is silk_float* so x[..]*x[..] is a float32 mul.
				// Atmp is double, so x_ptr[..] * Atmp promotes float to
				// double and multiplies in double.
				xn_f := x[xPtr+n]
				xnk_f := x[xPtr+n-k-1]
				xs_f := x[xPtr+subfr_length-n-1]
				xsk_f := x[xPtr+subfr_length-n+k]
				// float32 muls, promoted to double for the += on the
				// double accumulator.
				prod1 := mul_f32(xn_f, xnk_f)
				prod2 := mul_f32(xs_f, xsk_f)
				C_first_row[k] = sub_f64(C_first_row[k], float64(prod1))
				C_last_row[k] = sub_f64(C_last_row[k], float64(prod2))
				Atmp = Af[k]
				// x_ptr[..] * Atmp: float promoted to double, double mul.
				// Then += on tmp1/tmp2 (double + double).
				tmp1 = tmp1 + mul_f64(float64(xnk_f), Atmp)
				tmp2 = tmp2 + mul_f64(float64(xsk_f), Atmp)
			}
			for k = 0; k <= n; k++ {
				// C: CAf[k] -= tmp1 * x_ptr[n-k];   tmp1 is double.
				//    CAb[k] -= tmp2 * x_ptr[subfr_length-n+k-1];
				// tmp1 * x_ptr[..]: double * float = double. Then -=.
				CAf[k] = CAf[k] - mul_f64(tmp1, float64(x[xPtr+n-k]))
				CAb[k] = CAb[k] - mul_f64(tmp2, float64(x[xPtr+subfr_length-n+k-1]))
			}
		}
		tmp1 = C_first_row[n]
		tmp2 = C_last_row[n]
		for k = 0; k < n; k++ {
			Atmp = Af[k]
			// tmp1 += C_last_row[n-k-1] * Atmp
			// tmp2 += C_first_row[n-k-1] * Atmp
			tmp1 = tmp1 + mul_f64(C_last_row[n-k-1], Atmp)
			tmp2 = tmp2 + mul_f64(C_first_row[n-k-1], Atmp)
		}
		CAf[n+1] = tmp1
		CAb[n+1] = tmp2

		// Nominator and denominator for next reflection coefficient.
		num = CAb[n+1]
		nrg_b = CAb[0]
		nrg_f = CAf[0]
		for k = 0; k < n; k++ {
			Atmp = Af[k]
			// num   += CAb[n-k] * Atmp
			// nrg_b += CAb[k+1] * Atmp
			// nrg_f += CAf[k+1] * Atmp
			num = num + mul_f64(CAb[n-k], Atmp)
			nrg_b = nrg_b + mul_f64(CAb[k+1], Atmp)
			nrg_f = nrg_f + mul_f64(CAf[k+1], Atmp)
		}

		// rc = -2.0 * num / (nrg_f + nrg_b)
		//   C: (-2.0 * num) / (nrg_f + nrg_b). Left-to-right, all double.
		numerator := mul_f64(-2.0, num)
		denom := add_f64(nrg_f, nrg_b)
		rc = numerator / denom

		// tmp1 = invGain * (1.0 - rc*rc)
		//   C: invGain * (1.0 - rc*rc). Inner non-fused mul.
		oneMinusRc2 := 1.0 - mul_f64(rc, rc)
		tmp1 = mul_f64(invGain, oneMinusRc2)
		if tmp1 <= float64(minInvGain) {
			// C: rc = sqrt(1.0 - minInvGain / invGain);
			//   math.Sqrt matches C sqrt in double.
			rc = math.Sqrt(1.0 - float64(minInvGain)/invGain)
			if num > 0 {
				rc = -rc
			}
			invGain = float64(minInvGain)
			reached_max_gain = 1
		} else {
			invGain = tmp1
		}

		// Update AR coefficients.
		for k = 0; k < (n+1)>>1; k++ {
			tmp1 = Af[k]
			tmp2 = Af[n-k-1]
			// Af[k]       = tmp1 + rc * tmp2
			// Af[n-k-1]   = tmp2 + rc * tmp1
			Af[k] = tmp1 + mul_f64(rc, tmp2)
			Af[n-k-1] = tmp2 + mul_f64(rc, tmp1)
		}
		Af[n] = rc

		if reached_max_gain != 0 {
			for k = n + 1; k < D; k++ {
				Af[k] = 0.0
			}
			break
		}

		// Update C*Af and C*Ab.
		for k = 0; k <= n+1; k++ {
			tmp1 = CAf[k]
			// CAf[k]          += rc * CAb[n - k + 1]
			// CAb[n - k + 1]  += rc * tmp1
			CAf[k] = CAf[k] + mul_f64(rc, CAb[n-k+1])
			CAb[n-k+1] = CAb[n-k+1] + mul_f64(rc, tmp1)
		}
	}

	if reached_max_gain != 0 {
		for k = 0; k < D; k++ {
			A[k] = silk_float(-Af[k])
		}
		// Subtract energy of preceding samples from C0.
		for s = 0; s < nb_subfr; s++ {
			C0 = sub_f64(C0, silk_energy_FLP(x[s*subfr_length:], D))
		}
		// Approximate residual energy.
		nrg_f = mul_f64(C0, invGain)
	} else {
		nrg_f = CAf[0]
		tmp1 = 1.0
		for k = 0; k < D; k++ {
			Atmp = Af[k]
			// nrg_f += CAf[k+1] * Atmp
			// tmp1  += Atmp * Atmp
			nrg_f = nrg_f + mul_f64(CAf[k+1], Atmp)
			tmp1 = tmp1 + mul_f64(Atmp, Atmp)
			A[k] = silk_float(-Atmp)
		}
		// nrg_f -= FIND_LPC_COND_FAC * C0 * tmp1
		//   C left-to-right: (FIND_LPC_COND_FAC * C0) * tmp1.
		prod := mul_f64(mul_f64(findLPCCondFac, C0), tmp1)
		nrg_f = sub_f64(nrg_f, prod)
	}
	return silk_float(nrg_f)
}
