package nativeopus

// Port of libopus/celt/bands.h + bands.c. Float-path only, non-QEXT.
//
// Skipped:
//   - FIXED_POINT branches throughout (compute_band_energies /
//     normalise_bands / anti_collapse / intensity_stereo /
//     stereo_split / stereo_merge / compute_theta /
//     quant_partition / quant_band / quant_band_stereo
//     all have #ifdef FIXED_POINT paths we don't compile).
//   - ENABLE_QEXT ARG_QEXT(...) parameters and the cubic_quant/unquant
//     partition. Our config.h leaves ENABLE_QEXT undefined.
//   - MEASURE_NORM_MSE (debug-only measure_norm_mse).
//   - DISABLE_UPDATE_DRAFT branches: our build has the 2018 update-draft
//     path enabled (the #ifndef is the compiled branch).
//
// MAC16_16 / a + b*c / a - b*c patterns route through fma_add /
// fma_sub so the Go-side instruction stream matches the C oracle
// compiled with -ffp-contract=off.

// hysteresis_decision — C: bands.c:46-59.
func hysteresis_decision(val opus_val16, thresholds, hysteresis []opus_val16, N, prev int) int {
	var i int
	for i = 0; i < N; i++ {
		if val < thresholds[i] {
			break
		}
	}
	if i > prev && val < thresholds[prev]+hysteresis[prev] {
		i = prev
	}
	if i < prev && val > thresholds[prev-1]-hysteresis[prev-1] {
		i = prev
	}
	return i
}

// celt_lcg_rand — linear congruential PRNG. C: bands.c:61-64.
func celt_lcg_rand(seed opus_uint32) opus_uint32 {
	return 1664525*seed + 1013904223
}

// bitexact_cos — bit-exact cos approximation used by the bit allocator.
// C: bands.c:66-78.
func bitexact_cos(x opus_int16) opus_int16 {
	tmp := (4096 + opus_int32(x)*opus_int32(x)) >> 13
	celt_sig_assert(tmp <= 32767)
	x2 := opus_int16(tmp)
	x2 = opus_int16((32767 - opus_int32(x2)) + FRAC_MUL16(opus_int32(x2),
		(-7651+FRAC_MUL16(opus_int32(x2),
			(8277+FRAC_MUL16(-626, opus_int32(x2)))))))
	celt_sig_assert(x2 <= 32766)
	return 1 + x2
}

// bitexact_log2tan — bit-exact log2(tan(x)). C: bands.c:80-91.
func bitexact_log2tan(isin, icos int) int {
	lc := ec_ilog(opus_uint32(icos))
	ls := ec_ilog(opus_uint32(isin))
	icos <<= 15 - lc
	isin <<= 15 - ls
	return (ls-lc)*(1<<11) +
		int(FRAC_MUL16(opus_int32(isin), FRAC_MUL16(opus_int32(isin), -2597)+7932)) -
		int(FRAC_MUL16(opus_int32(icos), FRAC_MUL16(opus_int32(icos), -2597)+7932))
}

// compute_band_energies — sqrt of per-band energy. C: bands.c:151-166
// (float path).
func compute_band_energies(m *OpusCustomMode, X []celt_sig, bandE []celt_ener,
	end, C, LM, arch int) {
	eBands := m.eBands
	N := m.shortMdctSize << LM
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			sum := opus_val32(1e-27) + celt_inner_prod(
				X[c*N+(int(eBands[i])<<LM):],
				X[c*N+(int(eBands[i])<<LM):],
				(int(eBands[i+1])-int(eBands[i]))<<LM, arch)
			bandE[i+c*m.nbEBands] = celt_sqrt(sum)
		}
	}
}

// normalise_bands — scale each band so that its energy is 1.
// C: bands.c:169-183 (float path).
func normalise_bands(m *OpusCustomMode, freq []celt_sig, X []celt_norm,
	bandE []celt_ener, end, C, M int) {
	eBands := m.eBands
	N := M * m.shortMdctSize
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			g := opus_val16(1.0 / (1e-27 + bandE[i+c*m.nbEBands]))
			for j := M * int(eBands[i]); j < M*int(eBands[i+1]); j++ {
				X[j+c*N] = mul_f32(freq[j+c*N], g)
			}
		}
	}
}

// denormalise_bands — restore amplitude from unit-energy normalised
// bands. C: bands.c:188-256 (float path).
func denormalise_bands(m *OpusCustomMode, X []celt_norm, freq []celt_sig,
	bandLogE []celt_glog, start, end, M, downsample, silence int) {
	eBands := m.eBands
	N := M * m.shortMdctSize
	bound := M * int(eBands[end])
	if downsample != 1 {
		bound = IMIN(bound, N/downsample)
	}
	if silence != 0 {
		bound = 0
		start = 0
		end = 0
	}
	fIdx := 0
	xIdx := M * int(eBands[start])
	if start != 0 {
		for i := 0; i < M*int(eBands[start]); i++ {
			freq[fIdx] = 0
			fIdx++
		}
	} else {
		fIdx += M * int(eBands[start])
	}
	for i := start; i < end; i++ {
		band_end := M * int(eBands[i+1])
		lg := ADD32(opus_val32(bandLogE[i]), SHL32(opus_val32(eMeans[i]), 0))
		g := celt_exp2_db(MIN32(32.0, lg))
		j := M * int(eBands[i])
		for j < band_end {
			freq[fIdx] = PSHR32(MULT32_32_Q31(SHL32(X[xIdx], 0), g), 0)
			fIdx++
			xIdx++
			j++
		}
	}
	celt_assert(start <= end)
	OPUS_CLEAR(freq[bound:], N-bound)
}

// anti_collapse — inject noise into collapsed blocks to prevent energy
// holes. C: bands.c:259-353 (float path).
func anti_collapse(m *OpusCustomMode, X_ []celt_norm, collapse_masks []byte,
	LM, C, size, start, end int, logE, prev1logE, prev2logE []celt_glog,
	pulses []int, seed opus_uint32, encode, arch int) {
	for i := start; i < end; i++ {
		N0 := int(m.eBands[i+1] - m.eBands[i])
		celt_sig_assert(pulses[i] >= 0)
		depth := int(celt_udiv(opus_uint32(1+pulses[i]), opus_uint32(m.eBands[i+1]-m.eBands[i]))) >> LM

		thresh := opus_val16(0.5 * celt_exp2(-0.125*float32(depth)))
		sqrt_1 := celt_rsqrt(opus_val32(N0 << LM))

		for c := 0; c < C; c++ {
			prev1 := prev1logE[c*m.nbEBands+i]
			prev2 := prev2logE[c*m.nbEBands+i]
			if encode == 0 && C == 1 {
				prev1 = MAXG(prev1, prev1logE[m.nbEBands+i])
				prev2 = MAXG(prev2, prev2logE[m.nbEBands+i])
			}
			Ediff := opus_val32(logE[c*m.nbEBands+i]) - opus_val32(MING(prev1, prev2))
			Ediff = MAX32(0, Ediff)

			// r needs to be multiplied by 2 or 2*sqrt(2) depending on
			// LM because short blocks don't have the same energy as
			// long.
			r := 2.0 * celt_exp2_db(-Ediff)
			if LM == 3 {
				r *= 1.41421356
			}
			r = MIN16(thresh, r)
			r = mul_f32(r, sqrt_1)
			Xoff := c*size + (int(m.eBands[i]) << LM)
			renormalize := 0
			for k := 0; k < 1<<LM; k++ {
				// Detect collapse.
				if collapse_masks[i*C+c]&(1<<uint(k)) == 0 {
					// Fill with noise.
					for j := 0; j < N0; j++ {
						seed = celt_lcg_rand(seed)
						if seed&0x8000 != 0 {
							X_[Xoff+(j<<LM)+k] = r
						} else {
							X_[Xoff+(j<<LM)+k] = -r
						}
					}
					renormalize = 1
				}
			}
			// We just added some energy, so we need to renormalise.
			if renormalize != 0 {
				renormalise_vector(X_[Xoff:], N0<<LM, opus_val32(Q31ONE), arch)
			}
		}
	}
}

// compute_channel_weights — C: bands.c:362-377 (float path).
func compute_channel_weights(Ex, Ey celt_ener, w []opus_val16) {
	minE := MIN32(Ex, Ey)
	Ex = ADD32(Ex, minE/3)
	Ey = ADD32(Ey, minE/3)
	w[0] = Ex
	w[1] = Ey
}

// intensity_stereo — mix stereo into a single intensity band.
// C: bands.c:379-403 (float path).
func intensity_stereo(m *OpusCustomMode, X, Y []celt_norm, bandE []celt_ener,
	bandID, N int) {
	i := bandID
	left := opus_val16(bandE[i])
	right := opus_val16(bandE[i+m.nbEBands])
	// norm = EPSILON + sqrt(EPSILON + left*left + right*right)
	// Inner sum has two multiplies; the `EPSILON + l*l` fuses under
	// -ffp-contract=fast, so pin to non-fused.
	inner := add_f32(add_f32(EPSILON, mul_f32(left, left)), mul_f32(right, right))
	norm := EPSILON + celt_sqrt(inner)
	a1 := DIV32_16(SHL32(EXTEND32(left), 15), opus_val16(norm))
	a2 := DIV32_16(SHL32(EXTEND32(right), 15), opus_val16(norm))
	for j := 0; j < N; j++ {
		// X[j] = a1*X[j] + a2*Y[j] — no FMA.
		X[j] = add_f32(mul_f32(a1, X[j]), mul_f32(a2, Y[j]))
	}
}

// stereo_split — in-place 1/sqrt(2) mid/side split. C: bands.c:405-416.
func stereo_split(X, Y []celt_norm, N int) {
	for j := 0; j < N; j++ {
		l := mul_f32(0.70710678, X[j])
		r := mul_f32(0.70710678, Y[j])
		X[j] = add_f32(l, r)
		Y[j] = sub_f32(r, l)
	}
}

// stereo_merge — recover L/R from mid/side. C: bands.c:418-467 (float).
func stereo_merge(X, Y []celt_norm, mid opus_val32, N, arch int) {
	xp := celt_inner_prod(Y, X, N, arch)
	side := celt_inner_prod(Y, Y, N, arch)
	// Compensating for the mid normalization.
	xp = mul_f32(mid, xp)
	midSq := mul_f32(mid, mid)
	// El = (midSq + side) - 2*xp; Er = (midSq + side) + 2*xp.
	// C evaluates left-to-right: `midSq + side - 2*xp` / `+ 2*xp`.
	midSqPlusSide := add_f32(midSq, side)
	twoXp := add_f32(xp, xp)
	El := sub_f32(midSqPlusSide, twoXp)
	Er := add_f32(midSqPlusSide, twoXp)
	if Er < opus_val32(6e-4) || El < opus_val32(6e-4) {
		copy(Y[:N], X[:N])
		return
	}

	lgain := celt_rsqrt_norm(El)
	rgain := celt_rsqrt_norm(Er)

	for j := 0; j < N; j++ {
		l := mul_f32(mid, X[j])
		r := Y[j]
		X[j] = mul_f32(lgain, sub_f32(l, r))
		Y[j] = mul_f32(rgain, add_f32(l, r))
	}
}

// spreading_decision — decide how much spreading to apply.
// C: bands.c:470-561.
func spreading_decision(m *OpusCustomMode, X []celt_norm, average *int,
	last_decision int, hf_average, tapset_decision *int, update_hf, end, C, M int,
	spread_weight []int) int {
	sum := 0
	nbBands := 0
	eBands := m.eBands
	hf_sum := 0

	celt_assert(end > 0)
	N0 := M * m.shortMdctSize

	if M*(int(eBands[end])-int(eBands[end-1])) <= 8 {
		return SPREAD_NONE
	}
	for c := 0; c < C; c++ {
		for i := 0; i < end; i++ {
			N := M * (int(eBands[i+1]) - int(eBands[i]))
			if N <= 8 {
				continue
			}
			xIdx := M*int(eBands[i]) + c*N0
			tcount := [3]int{0, 0, 0}
			for j := 0; j < N; j++ {
				x := X[xIdx+j]
				x2N := mul_f32(mul_f32(x, x), opus_val32(N))
				if x2N < 0.25 {
					tcount[0]++
				}
				if x2N < 0.0625 {
					tcount[1]++
				}
				if x2N < 0.015625 {
					tcount[2]++
				}
			}
			// Only include four last bands (8 kHz and up).
			if i > m.nbEBands-4 {
				hf_sum += int(celt_udiv(opus_uint32(32*(tcount[1]+tcount[0])), opus_uint32(N)))
			}
			tmp := 0
			if 2*tcount[2] >= N {
				tmp++
			}
			if 2*tcount[1] >= N {
				tmp++
			}
			if 2*tcount[0] >= N {
				tmp++
			}
			sum += tmp * spread_weight[i]
			nbBands += spread_weight[i]
		}
	}

	if update_hf != 0 {
		if hf_sum != 0 {
			hf_sum = int(celt_udiv(opus_uint32(hf_sum), opus_uint32(C*(4-m.nbEBands+end))))
		}
		*hf_average = (*hf_average + hf_sum) >> 1
		hf_sum = *hf_average
		if *tapset_decision == 2 {
			hf_sum += 4
		} else if *tapset_decision == 0 {
			hf_sum -= 4
		}
		if hf_sum > 22 {
			*tapset_decision = 2
		} else if hf_sum > 18 {
			*tapset_decision = 1
		} else {
			*tapset_decision = 0
		}
	}
	celt_assert(nbBands > 0)
	celt_assert(sum >= 0)
	sum = int(celt_udiv(opus_uint32(sum)<<8, opus_uint32(nbBands)))
	// Recursive averaging.
	sum = (sum + *average) >> 1
	*average = sum
	// Hysteresis.
	sum = (3*sum + (((3 - last_decision) << 7) + 64) + 2) >> 2
	var decision int
	if sum < 80 {
		decision = SPREAD_AGGRESSIVE
	} else if sum < 256 {
		decision = SPREAD_NORMAL
	} else if sum < 384 {
		decision = SPREAD_LIGHT
	} else {
		decision = SPREAD_NONE
	}
	return decision
}

// ordery_table — Hadamard ordering for N=2, 4, 8, 16. C: bands.c:567-572.
var ordery_table = []int{
	1, 0,
	3, 0, 2, 1,
	7, 0, 4, 3, 6, 1, 5, 2,
	15, 0, 8, 7, 12, 3, 11, 4, 14, 1, 9, 6, 13, 2, 10, 5,
}

// deinterleave_hadamard — C: bands.c:574-598.
// scratch must have length >= N0*stride.
func deinterleave_hadamard(X []celt_norm, N0, stride, hadamard int, scratch []celt_norm) {
	N := N0 * stride
	tmp := scratch[:N]
	celt_assert(stride > 0)
	if hadamard != 0 {
		ordery := ordery_table[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[ordery[i]*N0+j] = X[j*stride+i]
			}
		}
	} else {
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[i*N0+j] = X[j*stride+i]
			}
		}
	}
	copy(X[:N], tmp)
}

// interleave_hadamard — C: bands.c:600-621.
// scratch must have length >= N0*stride.
func interleave_hadamard(X []celt_norm, N0, stride, hadamard int, scratch []celt_norm) {
	N := N0 * stride
	tmp := scratch[:N]
	if hadamard != 0 {
		ordery := ordery_table[stride-2:]
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[j*stride+i] = X[ordery[i]*N0+j]
			}
		}
	} else {
		for i := 0; i < stride; i++ {
			for j := 0; j < N0; j++ {
				tmp[j*stride+i] = X[i*N0+j]
			}
		}
	}
	copy(X[:N], tmp)
}

// haar1 — Haar transform step. C: bands.c:623-636.
func haar1(X []celt_norm, N0, stride int) {
	N0 >>= 1
	for i := 0; i < stride; i++ {
		for j := 0; j < N0; j++ {
			tmp1 := mul_f32(0.70710678, X[stride*2*j+i])
			tmp2 := mul_f32(0.70710678, X[stride*(2*j+1)+i])
			X[stride*2*j+i] = add_f32(tmp1, tmp2)
			X[stride*(2*j+1)+i] = sub_f32(tmp1, tmp2)
		}
	}
}

// compute_qn — resolve qn (number of theta levels). C: bands.c:638-662.
var exp2_table8 = [8]opus_int16{16384, 17866, 19483, 21247, 23170, 25267, 27554, 30048}

func compute_qn(N, b, offset, pulse_cap, stereo int) int {
	N2 := 2*N - 1
	if stereo != 0 && N == 2 {
		N2--
	}
	qb := int(celt_sudiv(opus_int32(b+N2*offset), opus_int32(N2)))
	qb = IMIN(b-pulse_cap-(4<<BITRES), qb)
	qb = IMIN(8<<BITRES, qb)
	var qn int
	if qb < (1 << BITRES >> 1) {
		qn = 1
	} else {
		qn = int(exp2_table8[qb&0x7]) >> (14 - (qb >> BITRES))
		qn = (qn + 1) >> 1 << 1
	}
	celt_assert(qn <= 256)
	return qn
}

// band_ctx — shared state across quant_band / quant_band_stereo.
// C: bands.c:664-686.
type band_ctx struct {
	encode            int
	resynth           int
	m                 *OpusCustomMode
	i                 int
	intensity         int
	spread            int
	tf_change         int
	ec                *ec_ctx
	remaining_bits    opus_int32
	bandE             []celt_ener
	seed              opus_uint32
	arch              int
	theta_round       int
	disable_inv       int
	avoid_split_noise int
	// Scratch slabs populated by quant_all_bands from the caller's
	// state. hadamardTmp is a per-band shuffle buffer (max N per band);
	// iy is the pulse-vector buffer for alg_quant / alg_unquant (N+3
	// elements worst case). Both are re-used across quant_band calls
	// since quant_band is sequential in a single quant_all_bands frame.
	hadamardTmp []celt_norm
	iy          []int
}

// split_ctx — output of compute_theta. C: bands.c:688-698.
type split_ctx struct {
	inv    int
	imid   int
	iside  int
	delta  int
	itheta int
	qalloc int
}

// compute_theta — decide on the theta split for a band. C: bands.c:700-933.
func compute_theta(ctx *band_ctx, sctx *split_ctx,
	X, Y []celt_norm, N int, b *int, B, B0, LM, stereo int, fill *int) {
	itheta := 0
	var imid, iside int
	var delta int
	encode := ctx.encode
	m := ctx.m
	i := ctx.i
	intensity := ctx.intensity
	ec := ctx.ec
	bandE := ctx.bandE

	// Decide on the resolution to give to the split parameter theta.
	pulse_cap := int(m.logN[i]) + LM*(1<<BITRES)
	offset := pulse_cap >> 1
	if stereo != 0 && N == 2 {
		offset -= QTHETA_OFFSET_TWOPHASE
	} else {
		offset -= QTHETA_OFFSET
	}
	qn := compute_qn(N, *b, offset, pulse_cap, stereo)
	if stereo != 0 && i >= intensity {
		qn = 1
	}
	var itheta_q30 opus_int32
	if encode != 0 {
		itheta_q30 = stereo_itheta(X, Y, stereo, N, ctx.arch)
		itheta = int(itheta_q30 >> 16)
	}
	tell := opus_int32(ec_tell_frac(ec))
	inv := 0
	if qn != 1 {
		if encode != 0 {
			if stereo == 0 || ctx.theta_round == 0 {
				itheta = (itheta*qn + 8192) >> 14
				if stereo == 0 && ctx.avoid_split_noise != 0 && itheta > 0 && itheta < qn {
					// Check whether theta will cause noise injection on
					// one side; if so, snap to the zero-energy side.
					unquantized := int(celt_udiv(opus_uint32(itheta)*16384, opus_uint32(qn)))
					imid = int(bitexact_cos(opus_int16(unquantized)))
					iside = int(bitexact_cos(opus_int16(16384 - unquantized)))
					delta = int(FRAC_MUL16(opus_int32((N-1)<<7), opus_int32(bitexact_log2tan(iside, imid))))
					if delta > *b {
						itheta = qn
					} else if delta < -*b {
						itheta = 0
					}
				}
			} else {
				var bias int
				if itheta > 8192 {
					bias = 32767 / qn
				} else {
					bias = -32767 / qn
				}
				down := IMIN(qn-1, IMAX(0, (itheta*qn+bias)>>14))
				if ctx.theta_round < 0 {
					itheta = down
				} else {
					itheta = down + 1
				}
			}
		}
		// Entropy coding of the angle.
		if stereo != 0 && N > 2 {
			p0 := 3
			x := itheta
			x0 := qn / 2
			ft := p0*(x0+1) + x0
			if encode != 0 {
				var fl, fh opus_uint32
				if x <= x0 {
					fl = opus_uint32(p0 * x)
					fh = opus_uint32(p0 * (x + 1))
				} else {
					fl = opus_uint32((x - 1 - x0) + (x0+1)*p0)
					fh = opus_uint32((x - x0) + (x0+1)*p0)
				}
				ec_encode(ec, fl, fh, opus_uint32(ft))
			} else {
				fs := int(ec_decode(ec, opus_uint32(ft)))
				if fs < (x0+1)*p0 {
					x = fs / p0
				} else {
					x = x0 + 1 + (fs - (x0+1)*p0)
				}
				var fl, fh opus_uint32
				if x <= x0 {
					fl = opus_uint32(p0 * x)
					fh = opus_uint32(p0 * (x + 1))
				} else {
					fl = opus_uint32((x - 1 - x0) + (x0+1)*p0)
					fh = opus_uint32((x - x0) + (x0+1)*p0)
				}
				ec_dec_update(ec, fl, fh, opus_uint32(ft))
				itheta = x
			}
		} else if B0 > 1 || stereo != 0 {
			// Uniform pdf.
			if encode != 0 {
				ec_enc_uint(ec, opus_uint32(itheta), opus_uint32(qn+1))
			} else {
				itheta = int(ec_dec_uint(ec, opus_uint32(qn+1)))
			}
		} else {
			ft := ((qn >> 1) + 1) * ((qn >> 1) + 1)
			if encode != 0 {
				var fl, fs int
				if itheta <= (qn >> 1) {
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					fs = qn + 1 - itheta
					fl = ft - ((qn+1-itheta)*(qn+2-itheta))>>1
				}
				ec_encode(ec, opus_uint32(fl), opus_uint32(fl+fs), opus_uint32(ft))
			} else {
				fl := 0
				fm := int(ec_decode(ec, opus_uint32(ft)))
				var fs int
				if fm < ((qn>>1)*((qn>>1)+1))>>1 {
					itheta = (int(isqrt32(opus_uint32(8*fm+1))) - 1) >> 1
					fs = itheta + 1
					fl = itheta * (itheta + 1) >> 1
				} else {
					itheta = (2*(qn+1) - int(isqrt32(opus_uint32(8*(ft-fm-1)+1)))) >> 1
					fs = qn + 1 - itheta
					fl = ft - ((qn+1-itheta)*(qn+2-itheta))>>1
				}
				ec_dec_update(ec, opus_uint32(fl), opus_uint32(fl+fs), opus_uint32(ft))
			}
		}
		celt_assert(itheta >= 0)
		itheta = int(celt_udiv(opus_uint32(itheta)*16384, opus_uint32(qn)))
		if encode != 0 && stereo != 0 {
			if itheta == 0 {
				intensity_stereo(m, X, Y, bandE, i, N)
			} else {
				stereo_split(X, Y, N)
			}
		}
	} else if stereo != 0 {
		if encode != 0 {
			if itheta > 8192 && ctx.disable_inv == 0 {
				inv = 1
			}
			if inv != 0 {
				for j := 0; j < N; j++ {
					Y[j] = -Y[j]
				}
			}
			intensity_stereo(m, X, Y, bandE, i, N)
		}
		if *b > 2<<BITRES && ctx.remaining_bits > 2<<BITRES {
			if encode != 0 {
				ec_enc_bit_logp(ec, inv, 2)
			} else {
				inv = ec_dec_bit_logp(ec, 2)
			}
		} else {
			inv = 0
		}
		if ctx.disable_inv != 0 {
			inv = 0
		}
		itheta = 0
	}
	qalloc := int(opus_int32(ec_tell_frac(ec)) - tell)
	*b -= qalloc

	if itheta == 0 {
		imid = 32767
		iside = 0
		*fill &= (1 << uint(B)) - 1
		delta = -16384
	} else if itheta == 16384 {
		imid = 0
		iside = 32767
		*fill &= ((1 << uint(B)) - 1) << uint(B)
		delta = 16384
	} else {
		imid = int(bitexact_cos(opus_int16(itheta)))
		iside = int(bitexact_cos(opus_int16(16384 - itheta)))
		delta = int(FRAC_MUL16(opus_int32((N-1)<<7), opus_int32(bitexact_log2tan(iside, imid))))
	}

	sctx.inv = inv
	sctx.imid = imid
	sctx.iside = iside
	sctx.delta = delta
	sctx.itheta = itheta
	sctx.qalloc = qalloc
}

// quant_band_n1 — single-sample band (sign bit + lowband_out).
// C: bands.c:934-967.
func quant_band_n1(ctx *band_ctx, X, Y, lowband_out []celt_norm) uint {
	encode := ctx.encode
	ec := ctx.ec
	stereo := 0
	if Y != nil {
		stereo = 1
	}
	x := X
	for c := 0; c < 1+stereo; c++ {
		sign := 0
		if ctx.remaining_bits >= 1<<BITRES {
			if encode != 0 {
				if x[0] < 0 {
					sign = 1
				}
				ec_enc_bits(ec, opus_uint32(sign), 1)
			} else {
				sign = int(ec_dec_bits(ec, 1))
			}
			ctx.remaining_bits -= 1 << BITRES
		}
		if ctx.resynth != 0 {
			if sign != 0 {
				x[0] = -NORM_SCALING
			} else {
				x[0] = NORM_SCALING
			}
		}
		x = Y
	}
	if lowband_out != nil {
		lowband_out[0] = SHR32(X[0], 4)
	}
	return 1
}

// quant_partition — recursive PVQ partition. C: bands.c:973-1177
// (non-QEXT float path).
func quant_partition(ctx *band_ctx, X []celt_norm, N, b, B int,
	lowband []celt_norm, LM int, gain opus_val32, fill int) uint {
	encode := ctx.encode
	m := ctx.m
	i := ctx.i
	spread := ctx.spread
	ec := ctx.ec
	var cm uint
	B0 := B

	// If we need 1.5 more bit than we can produce, split the band in two.
	cache := m.cache.bits[m.cache.index[(LM+1)*m.nbEBands+i]:]
	if LM != -1 && b > int(cache[cache[0]])+12 && N > 2 {
		var mbits, sbits, delta int
		var itheta int
		var qalloc int
		var sctx split_ctx
		var next_lowband2 []celt_norm
		var rebalance opus_int32
		var mid, side opus_val32

		N >>= 1
		Y := X[N:]
		LM -= 1
		if B == 1 {
			fill = (fill & 1) | (fill << 1)
		}
		B = (B + 1) >> 1

		compute_theta(ctx, &sctx, X, Y, N, &b, B, B0, LM, 0, &fill)
		imid := sctx.imid
		iside := sctx.iside
		delta = sctx.delta
		itheta = sctx.itheta
		qalloc = sctx.qalloc
		mid = (1.0 / 32768) * opus_val32(imid)
		side = (1.0 / 32768) * opus_val32(iside)

		// Give more bits to low-energy MDCTs than they would otherwise
		// deserve.
		if B0 > 1 && itheta&0x3fff != 0 {
			if itheta > 8192 {
				// Rough approximation for pre-echo masking.
				delta -= delta >> uint(4-LM)
			} else {
				// Forward-masking slope of 1.5 dB per 10 ms.
				delta = IMIN(0, delta+(N<<BITRES>>uint(5-LM)))
			}
		}
		mbits = IMAX(0, IMIN(b, (b-delta)/2))
		sbits = b - mbits
		ctx.remaining_bits -= opus_int32(qalloc)

		if lowband != nil {
			next_lowband2 = lowband[N:]
		}

		rebalance = ctx.remaining_bits
		if mbits >= sbits {
			cm = quant_partition(ctx, X, N, mbits, B, lowband, LM,
				mul_f32(gain, mid), fill)
			rebalance = opus_int32(mbits) - (rebalance - ctx.remaining_bits)
			if rebalance > 3<<BITRES && itheta != 0 {
				sbits += int(rebalance) - (3 << BITRES)
			}
			cm |= quant_partition(ctx, Y, N, sbits, B, next_lowband2, LM,
				mul_f32(gain, side), fill>>uint(B)) << uint(B0>>1)
		} else {
			cm = quant_partition(ctx, Y, N, sbits, B, next_lowband2, LM,
				mul_f32(gain, side), fill>>uint(B)) << uint(B0>>1)
			rebalance = opus_int32(sbits) - (rebalance - ctx.remaining_bits)
			if rebalance > 3<<BITRES && itheta != 16384 {
				mbits += int(rebalance) - (3 << BITRES)
			}
			cm |= quant_partition(ctx, X, N, mbits, B, lowband, LM,
				mul_f32(gain, mid), fill)
		}
	} else {
		// Basic no-split case.
		q := bits2pulses(m, i, LM, b)
		curr_bits := pulses2bits(m, i, LM, q)
		ctx.remaining_bits -= opus_int32(curr_bits)

		// Ensures we can never bust the budget.
		for ctx.remaining_bits < 0 && q > 0 {
			ctx.remaining_bits += opus_int32(curr_bits)
			q--
			curr_bits = pulses2bits(m, i, LM, q)
			ctx.remaining_bits -= opus_int32(curr_bits)
		}

		if q != 0 {
			K := get_pulses(q)
			// Actual quantization.
			if encode != 0 {
				cm = alg_quant(X, N, K, spread, B, ec, gain, ctx.resynth, ctx.arch, ctx.iy)
			} else {
				cm = alg_unquant(X, N, K, spread, B, ec, gain, ctx.iy)
			}
		} else {
			// If there's no pulse, fill the band anyway.
			if ctx.resynth != 0 {
				cm_mask := uint(1<<uint(B)) - 1
				fill &= int(cm_mask)
				if fill == 0 {
					for j := 0; j < N; j++ {
						X[j] = 0
					}
				} else {
					if lowband == nil {
						// Noise.
						for j := 0; j < N; j++ {
							ctx.seed = celt_lcg_rand(ctx.seed)
							X[j] = celt_norm(opus_int32(ctx.seed) >> 20)
						}
						cm = cm_mask
					} else {
						// Folded spectrum.
						for j := 0; j < N; j++ {
							ctx.seed = celt_lcg_rand(ctx.seed)
							// About 48 dB below the "normal" folding level.
							tmp := opus_val16(1.0 / 256)
							if ctx.seed&0x8000 == 0 {
								tmp = -tmp
							}
							X[j] = lowband[j] + tmp
						}
						cm = uint(fill)
					}
					renormalise_vector(X, N, gain, ctx.arch)
				}
			}
		}
	}

	return cm
}

// quant_band — mono band quant driver. C: bands.c:1248-1378
// (non-QEXT float path).
func quant_band(ctx *band_ctx, X []celt_norm, N, b, B int,
	lowband []celt_norm, LM int, lowband_out []celt_norm,
	gain opus_val32, lowband_scratch []celt_norm, fill int) uint {
	N0 := N
	N_B := N
	B0 := B
	time_divide := 0
	recombine := 0
	var cm uint
	encode := ctx.encode
	tf_change := ctx.tf_change

	longBlocks := 0
	if B0 == 1 {
		longBlocks = 1
	}

	N_B = int(celt_udiv(opus_uint32(N_B), opus_uint32(B)))

	// Special case for one sample.
	if N == 1 {
		return quant_band_n1(ctx, X, nil, lowband_out)
	}

	if tf_change > 0 {
		recombine = tf_change
	}

	if lowband_scratch != nil && lowband != nil && (recombine != 0 || (N_B&1 == 0 && tf_change < 0) || B0 > 1) {
		copy(lowband_scratch[:N], lowband[:N])
		lowband = lowband_scratch
	}

	bit_interleave_table := [16]byte{0, 1, 1, 1, 2, 3, 3, 3, 2, 3, 3, 3, 2, 3, 3, 3}
	for k := 0; k < recombine; k++ {
		if encode != 0 {
			haar1(X, N>>uint(k), 1<<uint(k))
		}
		if lowband != nil {
			haar1(lowband, N>>uint(k), 1<<uint(k))
		}
		fill = int(bit_interleave_table[fill&0xF]) | int(bit_interleave_table[fill>>4])<<2
	}
	B >>= uint(recombine)
	N_B <<= uint(recombine)

	// Increasing the time resolution.
	for N_B&1 == 0 && tf_change < 0 {
		if encode != 0 {
			haar1(X, N_B, B)
		}
		if lowband != nil {
			haar1(lowband, N_B, B)
		}
		fill |= fill << uint(B)
		B <<= 1
		N_B >>= 1
		time_divide++
		tf_change++
	}
	B0 = B
	N_B0 := N_B

	// Reorganize samples time-order vs frequency-order.
	if B0 > 1 {
		if encode != 0 {
			deinterleave_hadamard(X, N_B>>uint(recombine), B0<<uint(recombine), longBlocks, ctx.hadamardTmp)
		}
		if lowband != nil {
			deinterleave_hadamard(lowband, N_B>>uint(recombine), B0<<uint(recombine), longBlocks, ctx.hadamardTmp)
		}
	}

	cm = quant_partition(ctx, X, N, b, B, lowband, LM, gain, fill)

	// Resynthesis path (decoder or theta-RDO encoder).
	if ctx.resynth != 0 {
		// Undo the time→frequency reorganisation.
		if B0 > 1 {
			interleave_hadamard(X, N_B>>uint(recombine), B0<<uint(recombine), longBlocks, ctx.hadamardTmp)
		}

		// Undo time-freq changes.
		N_B = N_B0
		B = B0
		for k := 0; k < time_divide; k++ {
			B >>= 1
			N_B <<= 1
			cm |= cm >> uint(B)
			haar1(X, N_B, B)
		}

		bit_deinterleave_table := [16]byte{
			0x00, 0x03, 0x0C, 0x0F, 0x30, 0x33, 0x3C, 0x3F,
			0xC0, 0xC3, 0xCC, 0xCF, 0xF0, 0xF3, 0xFC, 0xFF,
		}
		for k := 0; k < recombine; k++ {
			cm = uint(bit_deinterleave_table[cm])
			haar1(X, N0>>uint(k), 1<<uint(k))
		}
		B <<= uint(recombine)

		// Scale output for later folding.
		if lowband_out != nil {
			n := celt_sqrt(opus_val32(N0))
			for j := 0; j < N0; j++ {
				lowband_out[j] = mul_f32(n, X[j])
			}
		}
		cm &= (1 << uint(B)) - 1
	}
	return cm
}

// MIN_STEREO_ENERGY — float build value. C: bands.c:1383.
const MIN_STEREO_ENERGY = opus_val32(1e-10)

// quant_band_stereo — stereo band quant driver. C: bands.c:1387-1572.
func quant_band_stereo(ctx *band_ctx, X, Y []celt_norm, N, b, B int,
	lowband []celt_norm, LM int, lowband_out, lowband_scratch []celt_norm,
	fill int) uint {
	encode := ctx.encode
	ec := ctx.ec
	var cm uint
	var sctx split_ctx

	// Special case for one sample.
	if N == 1 {
		return quant_band_n1(ctx, X, Y, lowband_out)
	}

	orig_fill := fill

	if encode != 0 {
		if ctx.bandE[ctx.i] < MIN_STEREO_ENERGY || ctx.bandE[ctx.m.nbEBands+ctx.i] < MIN_STEREO_ENERGY {
			if ctx.bandE[ctx.i] > ctx.bandE[ctx.m.nbEBands+ctx.i] {
				copy(Y[:N], X[:N])
			} else {
				copy(X[:N], Y[:N])
			}
		}
	}
	compute_theta(ctx, &sctx, X, Y, N, &b, B, B, LM, 1, &fill)
	inv := sctx.inv
	imid := sctx.imid
	iside := sctx.iside
	delta := sctx.delta
	itheta := sctx.itheta
	qalloc := sctx.qalloc
	mid := opus_val32((1.0 / 32768) * float32(imid))
	side := opus_val32((1.0 / 32768) * float32(iside))

	if N == 2 {
		// N=2 shortcut: only one bit for side.
		mbits := b
		sbits := 0
		if itheta != 0 && itheta != 16384 {
			sbits = 1 << BITRES
		}
		mbits -= sbits
		c := 0
		if itheta > 8192 {
			c = 1
		}
		ctx.remaining_bits -= opus_int32(qalloc + sbits)

		var x2, y2 []celt_norm
		if c != 0 {
			x2 = Y
			y2 = X
		} else {
			x2 = X
			y2 = Y
		}
		sign := 0
		if sbits != 0 {
			if encode != 0 {
				// Sign of (x2[0]*y2[1] - x2[1]*y2[0]).
				if mul_f32(x2[0], y2[1])-mul_f32(x2[1], y2[0]) < 0 {
					sign = 1
				}
				ec_enc_bits(ec, opus_uint32(sign), 1)
			} else {
				sign = int(ec_dec_bits(ec, 1))
			}
		}
		signFactor := 1 - 2*sign
		// Use orig_fill: even if itheta==16384 cleared the low bits, we
		// want to fold the side.
		cm = quant_band(ctx, x2, N, mbits, B, lowband, LM, lowband_out,
			opus_val32(Q31ONE), lowband_scratch, orig_fill)
		y2[0] = -celt_norm(signFactor) * x2[1]
		y2[1] = celt_norm(signFactor) * x2[0]
		if ctx.resynth != 0 {
			X[0] = mul_f32(mid, X[0])
			X[1] = mul_f32(mid, X[1])
			Y[0] = mul_f32(side, Y[0])
			Y[1] = mul_f32(side, Y[1])
			tmp := X[0]
			X[0] = sub_f32(tmp, Y[0])
			Y[0] = add_f32(tmp, Y[0])
			tmp = X[1]
			X[1] = sub_f32(tmp, Y[1])
			Y[1] = add_f32(tmp, Y[1])
		}
	} else {
		// Normal split code.
		mbits := IMAX(0, IMIN(b, (b-delta)/2))
		sbits := b - mbits
		ctx.remaining_bits -= opus_int32(qalloc)

		rebalance := ctx.remaining_bits
		if mbits >= sbits {
			cm = quant_band(ctx, X, N, mbits, B, lowband, LM, lowband_out,
				opus_val32(Q31ONE), lowband_scratch, fill)
			rebalance = opus_int32(mbits) - (rebalance - ctx.remaining_bits)
			if rebalance > 3<<BITRES && itheta != 0 {
				sbits += int(rebalance) - (3 << BITRES)
			}
			// High bits of fill are zero for a stereo split — side
			// won't be folded.
			cm |= quant_band(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>uint(B))
		} else {
			cm = quant_band(ctx, Y, N, sbits, B, nil, LM, nil, side, nil, fill>>uint(B))
			rebalance = opus_int32(sbits) - (rebalance - ctx.remaining_bits)
			if rebalance > 3<<BITRES && itheta != 16384 {
				mbits += int(rebalance) - (3 << BITRES)
			}
			cm |= quant_band(ctx, X, N, mbits, B, lowband, LM, lowband_out,
				opus_val32(Q31ONE), lowband_scratch, fill)
		}
	}

	if ctx.resynth != 0 {
		if N != 2 {
			stereo_merge(X, Y, mid, N, ctx.arch)
		}
		if inv != 0 {
			for j := 0; j < N; j++ {
				Y[j] = -Y[j]
			}
		}
	}
	return cm
}

// special_hybrid_folding — duplicate part of the first band's norm data
// so the second band can fold. C: bands.c:1575-1586.
func special_hybrid_folding(m *OpusCustomMode, norm, norm2 []celt_norm,
	start, M, dual_stereo int) {
	eBands := m.eBands
	n1 := M * int(eBands[start+1]-eBands[start])
	n2 := M * int(eBands[start+2]-eBands[start+1])
	copy(norm[n1:n1+(n2-n1)], norm[2*n1-n2:2*n1-n2+(n2-n1)])
	if dual_stereo != 0 {
		copy(norm2[n1:n1+(n2-n1)], norm2[2*n1-n2:2*n1-n2+(n2-n1)])
	}
}

// quant_all_bands — main driver. C: bands.c:1589-1922 (non-QEXT float).
//
// scratchNorm/scratchHadamard/scratchIy must each have capacity
// >= C * (M*lastEBand - norm_offset) (for scratchNorm) or >= max per-
// band N (for the others). Callers pass them from their state (see
// OpusCustomEncoder / OpusCustomDecoder scratch fields).
func quant_all_bands(encode int, m *OpusCustomMode, start, end int,
	X_, Y_ []celt_norm, collapse_masks []byte, bandE []celt_ener,
	pulses []int, shortBlocks, spread, dual_stereo, intensity int,
	tf_res []int, total_bits, balance opus_int32, ec *ec_ctx,
	LM, codedBands int, seed *opus_uint32, complexity, arch, disable_inv int,
	scratchNorm, scratchHadamard []celt_norm, scratchIy []int) {

	eBands := m.eBands
	M := 1 << LM
	B := 1
	if shortBlocks != 0 {
		B = M
	}
	norm_offset := M * int(eBands[start])
	// No need to allocate norm for the last band.
	C := 1
	if Y_ != nil {
		C = 2
	}
	_norm := scratchNorm[:C*(M*int(eBands[m.nbEBands-1])-norm_offset)]
	norm := _norm
	norm2 := norm[M*int(eBands[m.nbEBands-1])-norm_offset:]

	theta_rdo := 0
	if encode != 0 && Y_ != nil && dual_stereo == 0 && complexity >= 8 {
		theta_rdo = 1
	}
	resynth := 0
	if encode == 0 || theta_rdo != 0 {
		resynth = 1
	}

	var resynth_alloc int
	if encode != 0 && resynth != 0 {
		resynth_alloc = M * int(eBands[m.nbEBands]-eBands[m.nbEBands-1])
	} else {
		resynth_alloc = 1
	}
	_lowband_scratch := make([]celt_norm, resynth_alloc)
	var lowband_scratch []celt_norm
	if encode != 0 && resynth != 0 {
		lowband_scratch = _lowband_scratch
	} else {
		lowband_scratch = X_[M*int(eBands[m.effEBands-1]):]
	}
	X_save := make([]celt_norm, resynth_alloc)
	Y_save := make([]celt_norm, resynth_alloc)
	X_save2 := make([]celt_norm, resynth_alloc)
	Y_save2 := make([]celt_norm, resynth_alloc)
	norm_save2 := make([]celt_norm, resynth_alloc)

	var ctx band_ctx
	ctx.bandE = bandE
	ctx.ec = ec
	ctx.encode = encode
	ctx.intensity = intensity
	ctx.m = m
	ctx.seed = *seed
	ctx.spread = spread
	ctx.arch = arch
	ctx.disable_inv = disable_inv
	ctx.resynth = resynth
	ctx.theta_round = 0
	ctx.hadamardTmp = scratchHadamard
	ctx.iy = scratchIy

	var bytes_save []byte
	if theta_rdo != 0 {
		bytes_save = make([]byte, 1275)
	}

	// Avoid injecting noise in the first band on transients.
	if B > 1 {
		ctx.avoid_split_noise = 1
	}
	update_lowband := 1
	lowband_offset := 0
	for i := start; i < end; i++ {
		ctx.i = i
		last := 0
		if i == end-1 {
			last = 1
		}

		X := X_[M*int(eBands[i]):]
		var Y []celt_norm
		if Y_ != nil {
			Y = Y_[M*int(eBands[i]):]
		}
		N := M*int(eBands[i+1]) - M*int(eBands[i])
		celt_assert(N > 0)
		tell := opus_int32(ec_tell_frac(ec))

		// Compute how many bits we want to allocate to this band.
		if i != start {
			balance -= tell
		}
		remaining_bits := total_bits - tell - 1
		ctx.remaining_bits = remaining_bits

		var b int
		if i <= codedBands-1 {
			curr_balance := celt_sudiv(balance, opus_int32(IMIN(3, codedBands-i)))
			b = IMAX(0, IMIN(16383, IMIN(int(remaining_bits)+1, pulses[i]+int(curr_balance))))
		} else {
			b = 0
		}

		if resynth != 0 && (M*int(eBands[i])-N >= M*int(eBands[start]) || i == start+1) &&
			(update_lowband != 0 || lowband_offset == 0) {
			lowband_offset = i
		}
		if i == start+1 {
			special_hybrid_folding(m, norm, norm2, start, M, dual_stereo)
		}

		tf_change := tf_res[i]
		ctx.tf_change = tf_change
		if i >= m.effEBands {
			X = norm
			if Y_ != nil {
				Y = norm
			}
			lowband_scratch = nil
		}
		if last != 0 && theta_rdo == 0 {
			lowband_scratch = nil
		}

		// Conservative estimate of the collapse_mask for the bands
		// we're going to be folding from.
		effective_lowband := -1
		var x_cm, y_cm uint
		if lowband_offset != 0 && (spread != SPREAD_AGGRESSIVE || B > 1 || tf_change < 0) {
			effective_lowband = IMAX(0, M*int(eBands[lowband_offset])-norm_offset-N)
			fold_start := lowband_offset
			for {
				fold_start--
				if M*int(eBands[fold_start]) <= effective_lowband+norm_offset {
					break
				}
			}
			fold_end := lowband_offset - 1
			for fold_end++; fold_end < i && M*int(eBands[fold_end]) < effective_lowband+norm_offset+N; fold_end++ {
			}
			fold_i := fold_start
			for {
				x_cm |= uint(collapse_masks[fold_i*C+0])
				y_cm |= uint(collapse_masks[fold_i*C+C-1])
				fold_i++
				if fold_i >= fold_end {
					break
				}
			}
		} else {
			// Otherwise we'll be using the LCG to fold, so all blocks
			// will (almost always) be non-zero.
			x_cm = (1 << uint(B)) - 1
			y_cm = x_cm
		}

		if dual_stereo != 0 && i == intensity {
			// Switch off dual stereo to do intensity.
			dual_stereo = 0
			if resynth != 0 {
				for j := 0; j < M*int(eBands[i])-norm_offset; j++ {
					norm[j] = HALF32(add_f32(norm[j], norm2[j]))
				}
			}
		}
		var lb_slice []celt_norm
		if effective_lowband != -1 {
			lb_slice = norm[effective_lowband:]
		}
		var lb2_slice []celt_norm
		if effective_lowband != -1 && Y_ != nil {
			lb2_slice = norm2[effective_lowband:]
		}
		var lo_slice []celt_norm
		if last == 0 {
			lo_slice = norm[M*int(eBands[i])-norm_offset:]
		}
		var lo2_slice []celt_norm
		if last == 0 && Y_ != nil {
			lo2_slice = norm2[M*int(eBands[i])-norm_offset:]
		}

		if dual_stereo != 0 {
			x_cm = quant_band(&ctx, X, N, b/2, B, lb_slice, LM,
				lo_slice, opus_val32(Q31ONE), lowband_scratch, int(x_cm))
			y_cm = quant_band(&ctx, Y, N, b/2, B, lb2_slice, LM,
				lo2_slice, opus_val32(Q31ONE), lowband_scratch, int(y_cm))
		} else {
			if Y != nil {
				if theta_rdo != 0 && i < intensity {
					var ec_save, ec_save2 ec_ctx
					var ctx_save, ctx_save2 band_ctx
					var dist0, dist1 opus_val32
					var cm, cm2 uint
					var nstart_bytes, nend_bytes, save_bytes int
					var w [2]opus_val16
					compute_channel_weights(bandE[i], bandE[i+m.nbEBands], w[:])
					// Make a copy.
					cm = x_cm | y_cm
					ec_save = *ec
					ctx_save = ctx
					copy(X_save[:N], X[:N])
					copy(Y_save[:N], Y[:N])
					// Encode and round down.
					ctx.theta_round = -1
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B,
						lb_slice, LM, lo_slice, lowband_scratch, int(cm))
					dist0 = add_f32(
						mul_f32(w[0], celt_inner_prod(X_save, X, N, arch)),
						mul_f32(w[1], celt_inner_prod(Y_save, Y, N, arch)))

					// Save first result.
					cm2 = x_cm
					ec_save2 = *ec
					ctx_save2 = ctx
					copy(X_save2[:N], X[:N])
					copy(Y_save2[:N], Y[:N])
					if last == 0 {
						copy(norm_save2[:N], norm[M*int(eBands[i])-norm_offset:M*int(eBands[i])-norm_offset+N])
					}
					nstart_bytes = int(ec_save.offs)
					nend_bytes = int(ec_save.storage)
					bytes_buf := ec_save.buf[nstart_bytes:]
					save_bytes = nend_bytes - nstart_bytes
					copy(bytes_save[:save_bytes], bytes_buf[:save_bytes])
					// Restore.
					*ec = ec_save
					ctx = ctx_save
					copy(X[:N], X_save[:N])
					copy(Y[:N], Y_save[:N])
					if i == start+1 {
						special_hybrid_folding(m, norm, norm2, start, M, dual_stereo)
					}
					// Encode and round up.
					ctx.theta_round = 1
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B,
						lb_slice, LM, lo_slice, lowband_scratch, int(cm))
					dist1 = add_f32(
						mul_f32(w[0], celt_inner_prod(X_save, X, N, arch)),
						mul_f32(w[1], celt_inner_prod(Y_save, Y, N, arch)))
					if dist0 >= dist1 {
						x_cm = cm2
						*ec = ec_save2
						ctx = ctx_save2
						copy(X[:N], X_save2[:N])
						copy(Y[:N], Y_save2[:N])
						if last == 0 {
							copy(norm[M*int(eBands[i])-norm_offset:M*int(eBands[i])-norm_offset+N], norm_save2[:N])
						}
						copy(bytes_buf[:save_bytes], bytes_save[:save_bytes])
					}
				} else {
					ctx.theta_round = 0
					x_cm = quant_band_stereo(&ctx, X, Y, N, b, B,
						lb_slice, LM, lo_slice, lowband_scratch, int(x_cm|y_cm))
				}
			} else {
				x_cm = quant_band(&ctx, X, N, b, B, lb_slice, LM, lo_slice,
					opus_val32(Q31ONE), lowband_scratch, int(x_cm|y_cm))
			}
			y_cm = x_cm
		}
		collapse_masks[i*C+0] = byte(x_cm)
		collapse_masks[i*C+C-1] = byte(y_cm)
		balance += opus_int32(pulses[i]) + tell

		// Update the folding position only as long as we have 1
		// bit/sample depth.
		update_lowband = 0
		if b > N<<BITRES {
			update_lowband = 1
		}
		// We only need to avoid noise on a split for the first band.
		// After that, we have folding.
		ctx.avoid_split_noise = 0
	}
	*seed = ctx.seed
}
