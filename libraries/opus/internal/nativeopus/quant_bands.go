package nativeopus

import "math"

// Port of libopus/celt/quant_bands.h + quant_bands.c. Float-path only.
//
// Deferred / skipped:
//   - log2Amp: declared in quant_bands.h but never defined in the C
//     tree (and never referenced). Not ported.
//   - FIXED_POINT branches of every function.
//
// All multiply-accumulate patterns (MAC16_16, x - coef*oldE - prev,
// etc.) route through fma_add/fma_sub to match the C oracle's non-FMA
// sequence under -ffp-contract=off.

// eMeans — mean band energy, Q4 values converted back to float.
// C: quant_bands.c:53-59.
var eMeans = [25]opus_val16{
	6.437500, 6.250000, 5.750000, 5.312500, 5.062500,
	4.812500, 4.500000, 4.375000, 4.875000, 4.687500,
	4.562500, 4.437500, 4.875000, 4.625000, 4.312500,
	4.500000, 4.375000, 4.625000, 4.750000, 4.437500,
	3.750000, 3.750000, 3.750000, 3.750000, 3.750000,
}

// Prediction / leakage coefficients per LM (2.5 / 5 / 10 / 20 ms).
// C: quant_bands.c:67-69.
var (
	pred_coef  = [4]opus_val16{29440 / 32768.0, 26112 / 32768.0, 21248 / 32768.0, 16384 / 32768.0}
	beta_coef  = [4]opus_val16{30147 / 32768.0, 22282 / 32768.0, 12124 / 32768.0, 6554 / 32768.0}
	beta_intra = opus_val16(4915 / 32768.0)
)

// e_prob_model — Laplace-like PDF parameters. C: quant_bands.c:77-138.
// Indexing: [LM][intra][2*band], [2*band+1].
var e_prob_model = [4][2][42]byte{
	// 120-sample frames.
	{
		{
			72, 127, 65, 129, 66, 128, 65, 128, 64, 128, 62, 128, 64, 128,
			64, 128, 92, 78, 92, 79, 92, 78, 90, 79, 116, 41, 115, 40,
			114, 40, 132, 26, 132, 26, 145, 17, 161, 12, 176, 10, 177, 11,
		},
		{
			24, 179, 48, 138, 54, 135, 54, 132, 53, 134, 56, 133, 55, 132,
			55, 132, 61, 114, 70, 96, 74, 88, 75, 88, 87, 74, 89, 66,
			91, 67, 100, 59, 108, 50, 120, 40, 122, 37, 97, 43, 78, 50,
		},
	},
	// 240-sample frames.
	{
		{
			83, 78, 84, 81, 88, 75, 86, 74, 87, 71, 90, 73, 93, 74,
			93, 74, 109, 40, 114, 36, 117, 34, 117, 34, 143, 17, 145, 18,
			146, 19, 162, 12, 165, 10, 178, 7, 189, 6, 190, 8, 177, 9,
		},
		{
			23, 178, 54, 115, 63, 102, 66, 98, 69, 99, 74, 89, 71, 91,
			73, 91, 78, 89, 86, 80, 92, 66, 93, 64, 102, 59, 103, 60,
			104, 60, 117, 52, 123, 44, 138, 35, 133, 31, 97, 38, 77, 45,
		},
	},
	// 480-sample frames.
	{
		{
			61, 90, 93, 60, 105, 42, 107, 41, 110, 45, 116, 38, 113, 38,
			112, 38, 124, 26, 132, 27, 136, 19, 140, 20, 155, 14, 159, 16,
			158, 18, 170, 13, 177, 10, 187, 8, 192, 6, 175, 9, 159, 10,
		},
		{
			21, 178, 59, 110, 71, 86, 75, 85, 84, 83, 91, 66, 88, 73,
			87, 72, 92, 75, 98, 72, 105, 58, 107, 54, 115, 52, 114, 55,
			112, 56, 129, 51, 132, 40, 150, 33, 140, 29, 98, 35, 77, 42,
		},
	},
	// 960-sample frames.
	{
		{
			42, 121, 96, 66, 108, 43, 111, 40, 117, 44, 123, 32, 120, 36,
			119, 33, 127, 33, 134, 34, 139, 21, 147, 23, 152, 20, 158, 25,
			154, 26, 166, 21, 173, 16, 184, 13, 184, 10, 150, 13, 139, 15,
		},
		{
			22, 178, 63, 114, 74, 82, 84, 83, 92, 82, 103, 62, 96, 72,
			96, 67, 101, 73, 107, 72, 113, 55, 118, 52, 125, 52, 118, 52,
			117, 55, 135, 49, 137, 39, 157, 32, 145, 29, 97, 33, 77, 40,
		},
	},
}

// small_energy_icdf — C: quant_bands.c:140.
var small_energy_icdf = [3]byte{2, 1, 0}

// loss_distortion — sum-of-squared-differences between current and
// previous band energies, clipped to 200. C: quant_bands.c:142-154.
func loss_distortion(eBands, oldEBands []celt_glog, start, end, length, C int) opus_val32 {
	var dist opus_val32 = 0
	for c := 0; c < C; c++ {
		for i := start; i < end; i++ {
			d := PSHR32(SUB32(eBands[i+c*length], oldEBands[i+c*length]), 0)
			dist = fma_add(dist, d, d)
		}
	}
	return MIN32(200, SHR32(dist, 14))
}

// quant_coarse_energy_impl — C: quant_bands.c:156-258 (float path).
func quant_coarse_energy_impl(m *OpusCustomMode, start, end int,
	eBands []celt_glog, oldEBands []celt_glog,
	budget, tell opus_int32,
	prob_model []byte, error []celt_glog, enc *ec_enc,
	C, LM, intra int, max_decay celt_glog, lfe int) int {

	badness := 0
	var prev [2]opus_val32 = [2]opus_val32{0, 0}
	var coef, beta opus_val16

	if tell+3 <= budget {
		ec_enc_bit_logp(enc, intra, 3)
	}
	if intra != 0 {
		coef = 0
		beta = beta_intra
	} else {
		beta = beta_coef[LM]
		coef = pred_coef[LM]
	}

	// Encode at a fixed coarse resolution.
	for i := start; i < end; i++ {
		for c := 0; c < C; c++ {
			var bits_left int
			var qi, qi0 int
			var q, f, tmp opus_val32
			var x, oldE, decay_bound celt_glog
			x = eBands[i+c*m.nbEBands]
			oldE = MAXG(-GCONST(9.0), oldEBands[i+c*m.nbEBands])
			// f = x - coef*oldE - prev[c]
			f = sub_f32(fma_sub(x, coef, oldE), prev[c])
			// Rounding to nearest integer here is really important!
			qi = int(math.Floor(float64(0.5 + f)))
			decay_bound = MAXG(-GCONST(28.0), oldEBands[i+c*m.nbEBands]) - max_decay
			// Prevent the energy from going down too quickly (e.g. for
			// bands that have just one bin).
			if qi < 0 && x < decay_bound {
				qi += int(SHR32(SUB32(decay_bound, x), 0))
				if qi > 0 {
					qi = 0
				}
			}
			qi0 = qi
			// If we don't have enough bits to encode all the energy,
			// just assume something safe.
			tell = opus_int32(ec_tell(enc))
			bits_left = int(budget) - int(tell) - 3*C*(end-i)
			if i != start && bits_left < 30 {
				if bits_left < 24 {
					qi = IMIN(1, qi)
				}
				if bits_left < 16 {
					qi = IMAX(-1, qi)
				}
			}
			if lfe != 0 && i >= 2 {
				qi = IMIN(qi, 0)
			}
			if int(budget)-int(tell) >= 15 {
				pi := 2 * IMIN(i, 20)
				ec_laplace_encode(enc, &qi,
					opus_uint32(prob_model[pi])<<7, int(prob_model[pi+1])<<6)
			} else if int(budget)-int(tell) >= 2 {
				qi = IMAX(-1, IMIN(qi, 1))
				var sym int
				if qi < 0 {
					sym = 2*qi ^ -1
				} else {
					sym = 2 * qi
				}
				ec_enc_icdf(enc, sym, small_energy_icdf[:], 2)
			} else if int(budget)-int(tell) >= 1 {
				qi = IMIN(0, qi)
				ec_enc_bit_logp(enc, -qi, 1)
			} else {
				qi = -1
			}
			error[i+c*m.nbEBands] = f - SHL32(opus_val32(qi), 0)
			badness += abs_int(qi0 - qi)
			q = SHL32(EXTEND32(opus_val16(qi)), 0)

			// tmp = coef*oldE + prev[c] + q
			tmp = add_f32(fma_add(prev[c], coef, oldE), q)
			oldEBands[i+c*m.nbEBands] = tmp
			// prev[c] = prev[c] + q - beta*q
			prev[c] = fma_sub(add_f32(prev[c], q), beta, q)
		}
	}
	if lfe != 0 {
		return 0
	}
	return badness
}

func abs_int(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// quant_coarse_energy — C: quant_bands.c:260-358.
func quant_coarse_energy(m *OpusCustomMode, start, end, effEnd int,
	eBands, oldEBands []celt_glog, budget opus_uint32,
	error []celt_glog, enc *ec_enc, C, LM, nbAvailableBytes,
	force_intra int, delayedIntra *opus_val32, two_pass, loss_rate, lfe int) {

	var intra int
	if force_intra != 0 || (two_pass == 0 && *delayedIntra > 2*opus_val32(C*(end-start)) && nbAvailableBytes > (end-start)*C) {
		intra = 1
	}
	intra_bias := opus_int32((opus_int32(budget) * opus_int32(*delayedIntra) * opus_int32(loss_rate)) / opus_int32(C*512))
	new_distortion := loss_distortion(eBands, oldEBands, start, effEnd, m.nbEBands, C)

	tell := opus_int32(ec_tell(enc))
	if tell+3 > opus_int32(budget) {
		two_pass = 0
		intra = 0
	}

	var max_decay celt_glog = GCONST(16.0)
	if end-start > 10 {
		max_decay = MING(max_decay, celt_glog(0.125*float32(nbAvailableBytes)))
	}
	if lfe != 0 {
		max_decay = GCONST(3.0)
	}
	enc_start_state := *enc

	oldEBands_intra := make([]celt_glog, C*m.nbEBands)
	error_intra := make([]celt_glog, C*m.nbEBands)
	copy(oldEBands_intra, oldEBands[:C*m.nbEBands])

	var badness1 int
	if two_pass != 0 || intra != 0 {
		badness1 = quant_coarse_energy_impl(m, start, end, eBands, oldEBands_intra,
			opus_int32(budget), tell, e_prob_model[LM][1][:], error_intra, enc, C, LM, 1,
			max_decay, lfe)
	}

	if intra == 0 {
		tell_intra := opus_int32(ec_tell_frac(enc))

		enc_intra_state := *enc

		nstart_bytes := ec_range_bytes(&enc_start_state)
		nintra_bytes := ec_range_bytes(&enc_intra_state)
		intra_buf := ec_get_buffer(&enc_intra_state)[nstart_bytes:]
		save_bytes := nintra_bytes - nstart_bytes
		intra_bits := make([]byte, save_bytes)
		// Copy bits from intra bit-stream.
		copy(intra_bits, intra_buf[:nintra_bytes-nstart_bytes])

		*enc = enc_start_state

		badness2 := quant_coarse_energy_impl(m, start, end, eBands, oldEBands,
			opus_int32(budget), tell, e_prob_model[LM][intra][:], error, enc, C, LM, 0,
			max_decay, lfe)

		if two_pass != 0 && (badness1 < badness2 || (badness1 == badness2 && opus_int32(ec_tell_frac(enc))+intra_bias > tell_intra)) {
			*enc = enc_intra_state
			// Copy intra bits to bit-stream.
			copy(intra_buf[:nintra_bytes-nstart_bytes], intra_bits)
			copy(oldEBands[:C*m.nbEBands], oldEBands_intra)
			copy(error[:C*m.nbEBands], error_intra)
			intra = 1
		}
	} else {
		copy(oldEBands[:C*m.nbEBands], oldEBands_intra)
		copy(error[:C*m.nbEBands], error_intra)
	}

	if intra != 0 {
		*delayedIntra = new_distortion
	} else {
		// delayedIntra = pred_coef^2 * delayedIntra + new_distortion
		p := MULT16_16_Q15(pred_coef[LM], pred_coef[LM])
		*delayedIntra = fma_add(new_distortion, p, *delayedIntra)
	}
}

// quant_fine_energy — C: quant_bands.c:360-399.
func quant_fine_energy(m *OpusCustomMode, start, end int, oldEBands, error []celt_glog,
	prev_quant, extra_quant []int, enc *ec_enc, C int) {
	for i := start; i < end; i++ {
		var extra, prev opus_int16
		extra = 1 << extra_quant[i]
		if extra_quant[i] <= 0 {
			continue
		}
		if opus_int32(ec_tell(enc))+opus_int32(C*extra_quant[i]) > opus_int32(enc.storage)*8 {
			continue
		}
		if prev_quant != nil {
			prev = opus_int16(prev_quant[i])
		} else {
			prev = 0
		}
		for c := 0; c < C; c++ {
			var q2 int
			var offset celt_glog
			// C: `q2 = (int)floor((error[i+c*m->nbEBands]*(1<<prev)+.5f)*extra);`
			// The inner `a*b + 0.5` is `a*b + c` at float32, which Go's
			// arm64 backend would fuse into FMADDS. Under -ffp-contract=off
			// the C oracle emits separate FMUL+FADD, so route through
			// fma_add to keep the intermediate rounding.
			inner := fma_add(0.5, error[i+c*m.nbEBands], float32(int(1)<<prev))
			q2 = int(math.Floor(float64(mul_f32(inner, float32(extra)))))
			if q2 > int(extra)-1 {
				q2 = int(extra) - 1
			}
			if q2 < 0 {
				q2 = 0
			}
			ec_enc_bits(enc, opus_uint32(q2), extra_quant[i])
			offset = (celt_glog(q2)+0.5)*celt_glog(int(1)<<(14-extra_quant[i]))*(1.0/16384) - 0.5
			offset *= celt_glog(int(1)<<(14-prev)) * (1.0 / 16384)
			oldEBands[i+c*m.nbEBands] += offset
			error[i+c*m.nbEBands] -= offset
		}
	}
}

// quant_energy_finalise — C: quant_bands.c:401-429.
func quant_energy_finalise(m *OpusCustomMode, start, end int, oldEBands, error []celt_glog,
	fine_quant, fine_priority []int, bits_left int, enc *ec_enc, C int) {
	// Use up the remaining bits.
	for prio := 0; prio < 2; prio++ {
		for i := start; i < end && bits_left >= C; i++ {
			if fine_quant[i] >= MAX_FINE_BITS || fine_priority[i] != prio {
				continue
			}
			for c := 0; c < C; c++ {
				var q2 int
				var offset celt_glog
				if error[i+c*m.nbEBands] < 0 {
					q2 = 0
				} else {
					q2 = 1
				}
				ec_enc_bits(enc, opus_uint32(q2), 1)
				offset = (celt_glog(q2) - 0.5) * celt_glog(int(1)<<(14-fine_quant[i]-1)) * (1.0 / 16384)
				if oldEBands != nil {
					oldEBands[i+c*m.nbEBands] += offset
				}
				error[i+c*m.nbEBands] -= offset
				bits_left--
			}
		}
	}
}

// unquant_coarse_energy — C: quant_bands.c:431-494.
func unquant_coarse_energy(m *OpusCustomMode, start, end int, oldEBands []celt_glog,
	intra int, dec *ec_dec, C, LM int) {
	prob_model := e_prob_model[LM][intra][:]
	var prev [2]opus_val64 = [2]opus_val64{0, 0}
	var coef, beta opus_val16
	var budget, tell opus_int32

	if intra != 0 {
		coef = 0
		beta = beta_intra
	} else {
		beta = beta_coef[LM]
		coef = pred_coef[LM]
	}

	budget = opus_int32(dec.storage) * 8

	// Decode at a fixed coarse resolution.
	for i := start; i < end; i++ {
		for c := 0; c < C; c++ {
			var qi int
			var q, tmp opus_val32
			celt_sig_assert(c < 2)
			tell = opus_int32(ec_tell(dec))
			if budget-tell >= 15 {
				pi := 2 * IMIN(i, 20)
				qi = ec_laplace_decode(dec,
					opus_uint32(prob_model[pi])<<7, int(prob_model[pi+1])<<6)
			} else if budget-tell >= 2 {
				qi = ec_dec_icdf(dec, small_energy_icdf[:], 2)
				qi = (qi >> 1) ^ -(qi & 1)
			} else if budget-tell >= 1 {
				qi = -ec_dec_bit_logp(dec, 1)
			} else {
				qi = -1
			}
			q = SHL32(EXTEND32(opus_val16(qi)), 0)

			oldEBands[i+c*m.nbEBands] = MAXG(-GCONST(9.0), oldEBands[i+c*m.nbEBands])
			// tmp = coef*oldE + prev[c] + q
			tmp = add_f32(fma_add(opus_val32(prev[c]), coef, oldEBands[i+c*m.nbEBands]), q)
			oldEBands[i+c*m.nbEBands] = tmp
			// prev[c] = prev[c] + q - beta*q
			prev[c] = opus_val64(fma_sub(add_f32(opus_val32(prev[c]), q), beta, q))
		}
	}
}

// unquant_fine_energy — C: quant_bands.c:496-523.
func unquant_fine_energy(m *OpusCustomMode, start, end int, oldEBands []celt_glog,
	prev_quant, extra_quant []int, dec *ec_dec, C int) {
	for i := start; i < end; i++ {
		var extra, prev opus_int16
		extra = opus_int16(extra_quant[i])
		if extra_quant[i] <= 0 {
			continue
		}
		if opus_int32(ec_tell(dec))+opus_int32(C*extra_quant[i]) > opus_int32(dec.storage)*8 {
			continue
		}
		if prev_quant != nil {
			prev = opus_int16(prev_quant[i])
		} else {
			prev = 0
		}
		for c := 0; c < C; c++ {
			var q2 int
			var offset celt_glog
			q2 = int(ec_dec_bits(dec, int(extra)))
			offset = (celt_glog(q2)+0.5)*celt_glog(int(1)<<(14-extra))*(1.0/16384) - 0.5
			offset *= celt_glog(int(1)<<(14-prev)) * (1.0 / 16384)
			oldEBands[i+c*m.nbEBands] += offset
		}
	}
}

// unquant_energy_finalise — C: quant_bands.c:525-551.
func unquant_energy_finalise(m *OpusCustomMode, start, end int, oldEBands []celt_glog,
	fine_quant, fine_priority []int, bits_left int, dec *ec_dec, C int) {
	// Use up the remaining bits.
	for prio := 0; prio < 2; prio++ {
		for i := start; i < end && bits_left >= C; i++ {
			if fine_quant[i] >= MAX_FINE_BITS || fine_priority[i] != prio {
				continue
			}
			for c := 0; c < C; c++ {
				var q2 int
				var offset celt_glog
				q2 = int(ec_dec_bits(dec, 1))
				offset = (celt_glog(q2) - 0.5) * celt_glog(int(1)<<(14-fine_quant[i]-1)) * (1.0 / 16384)
				if oldEBands != nil {
					oldEBands[i+c*m.nbEBands] += offset
				}
				bits_left--
			}
		}
	}
}

// amp2Log2 — convert band energies to log2 domain, subtracting eMeans.
// C: quant_bands.c:553-572.
func amp2Log2(m *OpusCustomMode, effEnd, end int, bandE []celt_ener, bandLogE []celt_glog, C int) {
	for c := 0; c < C; c++ {
		for i := 0; i < effEnd; i++ {
			bandLogE[i+c*m.nbEBands] =
				celt_log2_db(bandE[i+c*m.nbEBands]) -
					SHL32(celt_glog(eMeans[i]), 0)
		}
		for i := effEnd; i < end; i++ {
			bandLogE[c*m.nbEBands+i] = -GCONST(14.0)
		}
	}
}
