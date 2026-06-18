package nativeopus

// Port of libopus/silk/A2NLSF.c.

const (
	silk_A2NLSF_BIN_DIV_STEPS_A2NLSF_FIX  = 3
	silk_A2NLSF_MAX_ITERATIONS_A2NLSF_FIX = 16
)

// silk_A2NLSF_trans_poly — Transform polynomials from cos(n*f) to cos(f)^n.
func silk_A2NLSF_trans_poly(p []opus_int32, dd opus_int) {
	for k := opus_int(2); k <= dd; k++ {
		for n := dd; n > k; n-- {
			p[n-2] -= p[n]
		}
		p[k-2] -= silk_LSHIFT(p[k], 1)
	}
}

// silk_A2NLSF_eval_poly — Polynomial evaluation, returns Q16 value.
func silk_A2NLSF_eval_poly(p []opus_int32, x opus_int32, dd opus_int) opus_int32 {
	y32 := p[dd]
	x_Q16 := silk_LSHIFT(x, 4)
	if opus_likely(dd == 8) {
		y32 = silk_SMLAWW(p[7], y32, x_Q16)
		y32 = silk_SMLAWW(p[6], y32, x_Q16)
		y32 = silk_SMLAWW(p[5], y32, x_Q16)
		y32 = silk_SMLAWW(p[4], y32, x_Q16)
		y32 = silk_SMLAWW(p[3], y32, x_Q16)
		y32 = silk_SMLAWW(p[2], y32, x_Q16)
		y32 = silk_SMLAWW(p[1], y32, x_Q16)
		y32 = silk_SMLAWW(p[0], y32, x_Q16)
	} else {
		for n := dd - 1; n >= 0; n-- {
			y32 = silk_SMLAWW(p[n], y32, x_Q16)
		}
	}
	return y32
}

// silk_A2NLSF_init — set up P, Q polynomials from filter coefficients.
func silk_A2NLSF_init(a_Q16, P, Q []opus_int32, dd opus_int) {
	P[dd] = silk_LSHIFT(1, 16)
	Q[dd] = silk_LSHIFT(1, 16)
	for k := opus_int(0); k < dd; k++ {
		P[k] = -a_Q16[dd-k-1] - a_Q16[dd+k]
		Q[k] = -a_Q16[dd-k-1] + a_Q16[dd+k]
	}
	// Divide out zeros as z=1 is always a root in Q and z=-1 in P.
	for k := dd; k > 0; k-- {
		P[k-1] -= P[k]
		Q[k-1] += Q[k]
	}
	silk_A2NLSF_trans_poly(P, dd)
	silk_A2NLSF_trans_poly(Q, dd)
}

// silk_A2NLSF — Compute Normalized Line Spectral Frequencies (NLSFs)
// from whitening filter coefficients.
func silk_A2NLSF(NLSF []opus_int16, a_Q16 []opus_int32, d opus_int) {
	var P [SILK_MAX_ORDER_LPC/2 + 1]opus_int32
	var Q [SILK_MAX_ORDER_LPC/2 + 1]opus_int32
	var PQ [2][]opus_int32
	PQ[0] = P[:]
	PQ[1] = Q[:]

	dd := silk_RSHIFT(opus_int32(d), 1)

	silk_A2NLSF_init(a_Q16, P[:], Q[:], opus_int(dd))

	p := P[:]

	xlo := opus_int32(silk_LSFCosTab_FIX_Q12[0])
	ylo := silk_A2NLSF_eval_poly(p, xlo, opus_int(dd))

	var root_ix opus_int
	if ylo < 0 {
		NLSF[0] = 0
		p = Q[:]
		ylo = silk_A2NLSF_eval_poly(p, xlo, opus_int(dd))
		root_ix = 1
	} else {
		root_ix = 0
	}
	k := opus_int(1)
	i := opus_int(0)
	var thr opus_int32

	for {
		xhi := opus_int32(silk_LSFCosTab_FIX_Q12[k])
		yhi := silk_A2NLSF_eval_poly(p, xhi, opus_int(dd))

		// Detect zero crossing.
		if (ylo <= 0 && yhi >= thr) || (ylo >= 0 && yhi <= -thr) {
			if yhi == 0 {
				thr = 1
			} else {
				thr = 0
			}
			// Binary division.
			ffrac := opus_int32(-256)
			for m := opus_int(0); m < silk_A2NLSF_BIN_DIV_STEPS_A2NLSF_FIX; m++ {
				xmid := silk_RSHIFT_ROUND(xlo+xhi, 1)
				ymid := silk_A2NLSF_eval_poly(p, xmid, opus_int(dd))

				if (ylo <= 0 && ymid >= 0) || (ylo >= 0 && ymid <= 0) {
					xhi = xmid
					yhi = ymid
				} else {
					xlo = xmid
					ylo = ymid
					ffrac = silk_ADD_RSHIFT(ffrac, 128, m)
				}
			}

			// Interpolate.
			if silk_abs(ylo) < 65536 {
				den := ylo - yhi
				nom := silk_LSHIFT(ylo, 8-silk_A2NLSF_BIN_DIV_STEPS_A2NLSF_FIX) + silk_RSHIFT(den, 1)
				if den != 0 {
					ffrac += silk_DIV32(nom, den)
				}
			} else {
				ffrac += silk_DIV32(ylo, silk_RSHIFT(ylo-yhi, 8-silk_A2NLSF_BIN_DIV_STEPS_A2NLSF_FIX))
			}
			NLSF[root_ix] = opus_int16(silk_min_32(silk_LSHIFT(opus_int32(k), 8)+ffrac, opus_int32(silk_int16_MAX)))
			silk_assert(NLSF[root_ix] >= 0)

			root_ix++
			if root_ix >= d {
				return
			}
			p = PQ[root_ix&1]

			xlo = opus_int32(silk_LSFCosTab_FIX_Q12[k-1])
			ylo = silk_LSHIFT(opus_int32(1-(root_ix&2)), 12)
		} else {
			k++
			xlo = xhi
			ylo = yhi
			thr = 0

			if k > LSF_COS_TAB_SZ_FIX {
				i++
				if i > silk_A2NLSF_MAX_ITERATIONS_A2NLSF_FIX {
					// Set NLSFs to white spectrum and exit.
					NLSF[0] = opus_int16(silk_DIV32_16(1<<15, opus_int32(d+1)))
					for k = 1; k < d; k++ {
						NLSF[k] = opus_int16(silk_ADD16(NLSF[k-1], NLSF[0]))
					}
					return
				}

				silk_bwexpander_32(a_Q16, d, 65536-silk_LSHIFT(1, i))

				silk_A2NLSF_init(a_Q16, P[:], Q[:], opus_int(dd))
				p = P[:]
				xlo = opus_int32(silk_LSFCosTab_FIX_Q12[0])
				ylo = silk_A2NLSF_eval_poly(p, xlo, opus_int(dd))
				if ylo < 0 {
					NLSF[0] = 0
					p = Q[:]
					ylo = silk_A2NLSF_eval_poly(p, xlo, opus_int(dd))
					root_ix = 1
				} else {
					root_ix = 0
				}
				k = 1
			}
		}
	}
}
