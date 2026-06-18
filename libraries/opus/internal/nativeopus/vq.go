package nativeopus

import "math"

// Port of libopus/celt/vq.h + vq.c. Float-path only.
//
// Skipped / deferred:
//   - FIXED_POINT variants (norm_scaleup/down, celt_inner_prod_norm_shift).
//   - ENABLE_QEXT paths: op_pvq_search_N2, op_pvq_search_extra,
//     op_pvq_refine, ec_enc_refine/dec_refine, cubic_quant/unquant.
//     Our vendored config.h has ENABLE_QEXT undefined.
//
// All MAC16_16(c, a, b) patterns (which expand to `c + a*b` in float
// builds) route through fma_add to match the C oracle's non-FMA
// instruction sequence under -ffp-contract=off.

// SPREAD_* — C: bands.h:68-71. Defined here so vq.go is usable before
// bands.c is ported; bands.go will redefine these identically (Go
// allows duplicate const declarations across files in the same package
// only if the names differ, so bands.go's port will `use` these
// constants rather than redeclare).
const (
	SPREAD_NONE       = 0
	SPREAD_LIGHT      = 1
	SPREAD_NORMAL     = 2
	SPREAD_AGGRESSIVE = 3
)

// In float mode these map to celt_inner_prod. C: vq.h:52-55.
func celt_inner_prod_norm(x, y []celt_norm, length, arch int) opus_val32 {
	return celt_inner_prod(x, y, length, arch)
}

// exp_rotation1 — one pass of the PVQ rotation. C: vq.c:75-101.
func exp_rotation1(X []celt_norm, length, stride int, c, s opus_val16) {
	ms := NEG16(s)
	// Forward scan: [0 .. len-stride).
	xptr := 0
	for i := 0; i < length-stride; i++ {
		x1 := X[xptr]
		x2 := X[xptr+stride]
		X[xptr+stride] = EXTRACT16(PSHR32(fma_add(MULT16_16(c, x2), s, x1), 15))
		X[xptr] = EXTRACT16(PSHR32(fma_add(MULT16_16(c, x1), ms, x2), 15))
		xptr++
	}
	// Reverse scan: starts at len-2*stride-1.
	xptr = length - 2*stride - 1
	for i := length - 2*stride - 1; i >= 0; i-- {
		x1 := X[xptr]
		x2 := X[xptr+stride]
		X[xptr+stride] = EXTRACT16(PSHR32(fma_add(MULT16_16(c, x2), s, x1), 15))
		X[xptr] = EXTRACT16(PSHR32(fma_add(MULT16_16(c, x1), ms, x2), 15))
		xptr--
	}
}

// exp_rotation — PVQ pre/post-rotation driver. C: vq.c:104-147.
func exp_rotation(X []celt_norm, length, dir, stride, K, spread int) {
	SPREAD_FACTOR := [3]int{15, 10, 5}
	if 2*K >= length || spread == SPREAD_NONE {
		return
	}
	factor := SPREAD_FACTOR[spread-1]

	gain := celt_div(opus_val32(MULT16_16(Q15_ONE, opus_val16(length))),
		opus_val32(length+factor*K))
	theta := HALF16(opus_val16(MULT16_16_Q15(opus_val16(gain), opus_val16(gain))))

	c := celt_cos_norm(EXTEND32(theta))
	s := celt_cos_norm(EXTEND32(SUB16(Q15ONE, theta)))

	stride2 := 0
	if length >= 8*stride {
		stride2 = 1
		// Equivalent to rounded sqrt(len/stride).
		for (stride2*stride2+stride2)*stride+(stride>>2) < length {
			stride2++
		}
	}
	length = int(celt_udiv(opus_uint32(length), opus_uint32(stride)))
	for i := 0; i < stride; i++ {
		sub := X[i*length:]
		if dir < 0 {
			if stride2 != 0 {
				exp_rotation1(sub, length, stride2, s, c)
			}
			exp_rotation1(sub, length, 1, c, s)
		} else {
			exp_rotation1(sub, length, 1, c, -s)
			if stride2 != 0 {
				exp_rotation1(sub, length, stride2, s, -c)
			}
		}
	}
}

// normalise_residual — C: vq.c:150-181 (float path).
func normalise_residual(iy []int, X []celt_norm, N int, Ryy, gain opus_val32, shift int) {
	_ = shift
	t := Ryy
	g := MULT32_32_Q31(celt_rsqrt_norm32(t), gain)
	for i := 0; i < N; i++ {
		X[i] = VSHR32(MULT16_32_Q15(opus_val16(iy[i]), g), 0)
	}
}

// extract_collapse_mask — C: vq.c:183-203.
func extract_collapse_mask(iy []int, N, B int) uint {
	if B <= 1 {
		return 1
	}
	N0 := int(celt_udiv(opus_uint32(N), opus_uint32(B)))
	var collapse_mask uint
	for i := 0; i < B; i++ {
		var tmp int
		for j := 0; j < N0; j++ {
			tmp |= iy[i*N0+j]
		}
		if tmp != 0 {
			collapse_mask |= 1 << uint(i)
		}
	}
	return collapse_mask
}

// op_pvq_search_c — PVQ pyramid search. C: vq.c:205-374.
func op_pvq_search_c(X []celt_norm, iy []int, K, N, arch int) opus_val16 {
	_ = arch
	y := make([]celt_norm, N)
	signx := make([]int, N)

	// Get rid of the sign.
	var sum opus_val32 = 0
	for j := 0; j < N; j++ {
		if X[j] < 0 {
			signx[j] = 1
		} else {
			signx[j] = 0
		}
		X[j] = ABS16(X[j])
		iy[j] = 0
		y[j] = 0
	}

	var xy opus_val32 = 0
	var yy opus_val16 = 0

	pulsesLeft := K

	// Pre-search by projecting on the pyramid.
	if K > (N >> 1) {
		var rcp opus_val16
		for j := 0; j < N; j++ {
			sum += X[j]
		}
		// Prevents infinities and NaNs from causing too many pulses.
		// 64 is an approximation of infinity here.
		if !(sum > EPSILON && sum < 64) {
			X[0] = QCONST16(1.0, 14)
			for j := 1; j < N; j++ {
				X[j] = 0
			}
			sum = QCONST16(1.0, 14)
		}
		// Using K+e with e < 1 guarantees we cannot get more than K
		// pulses.
		rcp = EXTRACT16(MULT16_32_Q16(opus_val16(K)+0.8, celt_rcp(sum)))
		for j := 0; j < N; j++ {
			iy[j] = int(math.Floor(float64(rcp * X[j])))
			y[j] = celt_norm(iy[j])
			yy = opus_val16(fma_add(opus_val32(yy), y[j], y[j]))
			xy = fma_add(xy, X[j], y[j])
			y[j] *= 2
			pulsesLeft -= iy[j]
		}
	}
	celt_sig_assert(pulsesLeft >= 0)

	// Emergency: fill bin 0 if we somehow have too many pulses left.
	if pulsesLeft > N+3 {
		tmp := opus_val16(pulsesLeft)
		yy = opus_val16(fma_add(opus_val32(yy), tmp, tmp))
		yy = opus_val16(fma_add(opus_val32(yy), tmp, y[0]))
		iy[0] += pulsesLeft
		pulsesLeft = 0
	}

	for i := 0; i < pulsesLeft; i++ {
		var Rxy, Ryy opus_val16
		var best_id int
		var best_num opus_val32
		var best_den opus_val16

		best_id = 0
		// Squared magnitude term added outside the loop.
		yy = ADD16(yy, 1)

		// Position 0 out of the loop.
		Rxy = EXTRACT16(SHR32(ADD32(xy, EXTEND32(X[0])), 0))
		Ryy = ADD16(yy, y[0])

		Rxy = opus_val16(MULT16_16_Q15(Rxy, Rxy))
		best_den = Ryy
		best_num = opus_val32(Rxy)
		for j := 1; j < N; j++ {
			Rxy = EXTRACT16(SHR32(ADD32(xy, EXTEND32(X[j])), 0))
			Ryy = ADD16(yy, y[j])

			Rxy = opus_val16(MULT16_16_Q15(Rxy, Rxy))
			if opus_unlikely(MULT16_16(best_den, Rxy) > MULT16_16(Ryy, opus_val16(best_num))) {
				best_den = Ryy
				best_num = opus_val32(Rxy)
				best_id = j
			}
		}

		// Update running sums.
		xy = ADD32(xy, EXTEND32(X[best_id]))
		yy = ADD16(yy, y[best_id])
		y[best_id] += 2
		iy[best_id]++
	}

	// Restore signs.
	for j := 0; j < N; j++ {
		iy[j] = (iy[j] ^ -signx[j]) + signx[j]
	}
	return opus_val16(yy)
}

// op_pvq_search — C macro wrapper (no RTCD). C: vq.h:62-65.
func op_pvq_search(X []celt_norm, iy []int, K, N, arch int) opus_val16 {
	return op_pvq_search_c(X, iy, K, N, arch)
}

// alg_quant — algebraic PVQ encode. Non-QEXT path of C: vq.c:550-615.
func alg_quant(X []celt_norm, N, K, spread, B int, enc *ec_enc,
	gain opus_val32, resynth, arch int, scratchIy []int) uint {
	celt_assert2(K > 0, "alg_quant() needs at least one pulse")
	celt_assert2(N > 1, "alg_quant() needs at least two dimensions")

	iy := scratchIy[:N+3]
	exp_rotation(X, N, 1, B, K, spread)

	yy := op_pvq_search(X, iy, K, N, arch)
	collapse_mask := extract_collapse_mask(iy, N, B)
	encode_pulses(iy, opus_int(N), opus_int(K), enc)
	if resynth != 0 {
		normalise_residual(iy, X, N, opus_val32(yy), gain, 0)
	}

	if resynth != 0 {
		exp_rotation(X, N, -1, B, K, spread)
	}
	return collapse_mask
}

// alg_unquant — algebraic PVQ decode. Non-QEXT path of C: vq.c:619-690.
func alg_unquant(X []celt_norm, N, K, spread, B int, dec *ec_dec, gain opus_val32, scratchIy []int) uint {
	celt_assert2(K > 0, "alg_unquant() needs at least one pulse")
	celt_assert2(N > 1, "alg_unquant() needs at least two dimensions")
	iy := scratchIy[:N]
	Ryy := decode_pulses(iy, opus_int(N), opus_int(K), dec)
	normalise_residual(iy, X, N, Ryy, gain, 0)
	exp_rotation(X, N, -1, B, K, spread)
	return extract_collapse_mask(iy, N, B)
}

// renormalise_vector — C: vq.c:692-720.
func renormalise_vector(X []celt_norm, N int, gain opus_val32, arch int) {
	E := opus_val32(EPSILON) + celt_inner_prod_norm(X, X, N, arch)
	t := E
	g := MULT32_32_Q31(celt_rsqrt_norm(t), gain)
	for i := 0; i < N; i++ {
		X[i] = EXTRACT16(PSHR32(MULT16_16(opus_val16(g), X[i]), 0))
	}
}

// stereo_itheta — C: vq.c:722-753.
func stereo_itheta(X, Y []celt_norm, stereo, N, arch int) opus_int32 {
	var Emid, Eside opus_val32 = 0, 0
	if stereo != 0 {
		for i := 0; i < N; i++ {
			m := PSHR32(ADD32(X[i], Y[i]), 0)
			s := PSHR32(SUB32(X[i], Y[i]), 0)
			Emid = fma_add(Emid, m, m)
			Eside = fma_add(Eside, s, s)
		}
	} else {
		Emid += celt_inner_prod_norm(X, X, N, arch)
		Eside += celt_inner_prod_norm(Y, Y, N, arch)
	}
	mid := celt_sqrt32(Emid)
	side := celt_sqrt32(Eside)
	itheta := int(math.Floor(float64(0.5 + 65536.0*16384*celt_atan2p_norm(side, mid))))
	return opus_int32(itheta)
}
