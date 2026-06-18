package nativeopus

import "math"

// 1:1 port of libopus/silk/float/pitch_analysis_core_FLP.c.
// See the 9 rules for bit-exact parity in the FLP-wave docs:
// literal mirroring, fma_add/fma_sub for every a +/- b*c, add_f32/
// sub_f32/mul_f32 wrapping bare float arithmetic, double-precision
// literals vs float (`.5` vs `.5f`), strict left-to-right associativity,
// celt_sqrt narrows at the boundary, and silk_SMULBB(float, int) in
// float sources is just a scalar multiply — no int16 narrowing — since
// clang at -O2 treats the operand-narrowing as UB and skips it.

// pitchScratchSize mirrors the C `#define SCRATCH_SIZE 22`.
const pitchScratchSize = 22

// flattenCB2_stage2 / flattenCB2_stage2_10ms / flattenCB2_stage3 /
// flattenCB2_stage3_10ms — return a flat row-major view of the 2D
// codebook arrays for matrix_ptr indexing. The C source exposes these
// as const opus_int8[rows][cols] and takes `&arr[0][0]` which is a flat
// pointer; Go doesn't let us form that from a [R][C]int8 directly, so
// we build a flat slice on demand.

func flattenCBStage2() []opus_int8 {
	out := make([]opus_int8, PE_MAX_NB_SUBFR*PE_NB_CBKS_STAGE2_EXT)
	for r := 0; r < PE_MAX_NB_SUBFR; r++ {
		for c := 0; c < PE_NB_CBKS_STAGE2_EXT; c++ {
			out[r*PE_NB_CBKS_STAGE2_EXT+c] = silk_CB_lags_stage2[r][c]
		}
	}
	return out
}

func flattenCBStage2_10ms() []opus_int8 {
	out := make([]opus_int8, (PE_MAX_NB_SUBFR>>1)*PE_NB_CBKS_STAGE2_10MS)
	for r := 0; r < PE_MAX_NB_SUBFR>>1; r++ {
		for c := 0; c < PE_NB_CBKS_STAGE2_10MS; c++ {
			out[r*PE_NB_CBKS_STAGE2_10MS+c] = silk_CB_lags_stage2_10_ms[r][c]
		}
	}
	return out
}

func flattenCBStage3() []opus_int8 {
	out := make([]opus_int8, PE_MAX_NB_SUBFR*PE_NB_CBKS_STAGE3_MAX)
	for r := 0; r < PE_MAX_NB_SUBFR; r++ {
		for c := 0; c < PE_NB_CBKS_STAGE3_MAX; c++ {
			out[r*PE_NB_CBKS_STAGE3_MAX+c] = silk_CB_lags_stage3[r][c]
		}
	}
	return out
}

func flattenCBStage3_10ms() []opus_int8 {
	out := make([]opus_int8, (PE_MAX_NB_SUBFR>>1)*PE_NB_CBKS_STAGE3_10MS)
	for r := 0; r < PE_MAX_NB_SUBFR>>1; r++ {
		for c := 0; c < PE_NB_CBKS_STAGE3_10MS; c++ {
			out[r*PE_NB_CBKS_STAGE3_10MS+c] = silk_CB_lags_stage3_10_ms[r][c]
		}
	}
	return out
}

func flattenLagRangeStage3(complexity int) []opus_int8 {
	out := make([]opus_int8, PE_MAX_NB_SUBFR*2)
	for r := 0; r < PE_MAX_NB_SUBFR; r++ {
		for c := 0; c < 2; c++ {
			out[r*2+c] = silk_Lag_range_stage3[complexity][r][c]
		}
	}
	return out
}

func flattenLagRangeStage3_10ms() []opus_int8 {
	out := make([]opus_int8, (PE_MAX_NB_SUBFR>>1)*2)
	for r := 0; r < PE_MAX_NB_SUBFR>>1; r++ {
		for c := 0; c < 2; c++ {
			out[r*2+c] = silk_Lag_range_stage3_10_ms[r][c]
		}
	}
	return out
}

// silk_log2 — mirror of SigProc_FLP.h:
//
//	static OPUS_INLINE silk_float silk_log2( double x ) {
//	    return ( silk_float )( 3.32192809488736 * log10( x ) );
//	}
//
// The multiply and log10 run in double; the cast to silk_float is the
// narrowing round.
func silk_log2(x float64) silk_float {
	return silk_float(mul_f64(3.32192809488736, math.Log10(x)))
}

// silk_max_float — mirror of SigProc_FLP.h macro; the conditional
// returns the bit-exact larger value.
func silk_max_float(a, b silk_float) silk_float {
	if a > b {
		return a
	}
	return b
}

// silk_float2short_array — mirror of SigProc_FLP.h inline; writes from
// length-1 down to 0, using float2int (round-to-nearest-even) and
// silk_SAT16.
func silk_float2short_array(out []opus_int16, in []silk_float, length opus_int32) {
	for k := length - 1; k >= 0; k-- {
		out[k] = opus_int16(silk_SAT16(float2int(float32(in[k]))))
	}
}

// silk_short2float_array — mirror of SigProc_FLP.h inline; writes from
// length-1 down to 0.
func silk_short2float_array(out []silk_float, in []opus_int16, length opus_int32) {
	for k := length - 1; k >= 0; k-- {
		out[k] = silk_float(in[k])
	}
}

// silk_pitch_analysis_core_FLP — O: voicing estimate (0 voiced, 1 unvoiced).
func silk_pitch_analysis_core_FLP(
	frame []silk_float,
	pitch_out []opus_int,
	lagIndex *opus_int16,
	contourIndex *opus_int8,
	LTPCorr *silk_float,
	prevLag opus_int,
	search_thres1 silk_float,
	search_thres2 silk_float,
	Fs_kHz opus_int,
	complexity opus_int,
	nb_subfr opus_int,
	arch int,
) opus_int {
	var i, k, d, j opus_int
	var frame_8kHz [PE_MAX_FRAME_LENGTH_MS * 8]silk_float
	var frame_4kHz [PE_MAX_FRAME_LENGTH_MS * 4]silk_float
	var frame_8_FIX [PE_MAX_FRAME_LENGTH_MS * 8]opus_int16
	var frame_4_FIX [PE_MAX_FRAME_LENGTH_MS * 4]opus_int16
	var filt_state [6]opus_int32
	var threshold, contour_bias silk_float
	var C [PE_MAX_NB_SUBFR][(PE_MAX_LAG >> 1) + 5]silk_float
	var xcorr [PE_MAX_LAG_MS*4 - PE_MIN_LAG_MS*4 + 1]opus_val32
	var CC [PE_NB_CBKS_STAGE2_EXT]silk_float
	var cross_corr, normalizer, energy, energy_tmp float64
	var d_srch [PE_D_SRCH_LENGTH]opus_int
	var d_comp [(PE_MAX_LAG >> 1) + 5]opus_int16
	var length_d_srch, length_d_comp opus_int
	var Cmax, CCmax, CCmax_b, CCmax_new_b, CCmax_new silk_float
	var CBimax, CBimax_new, lag, start_lag, end_lag, lag_new opus_int
	var cbk_size opus_int
	var lag_log2, prevLag_log2, delta_lag_log2_sqr silk_float
	var energies_st3 [PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX][PE_NB_STAGE3_LAGS]silk_float
	var cross_corr_st3 [PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX][PE_NB_STAGE3_LAGS]silk_float
	var lag_counter opus_int
	var frame_length, frame_length_8kHz, frame_length_4kHz opus_int
	var sf_length, sf_length_8kHz, sf_length_4kHz opus_int
	var min_lag, min_lag_8kHz, min_lag_4kHz opus_int
	var max_lag, max_lag_8kHz, max_lag_4kHz opus_int
	var nb_cbk_search opus_int
	var Lag_CB_ptr []opus_int8

	// Check for valid sampling frequency
	celt_assert(Fs_kHz == 8 || Fs_kHz == 12 || Fs_kHz == 16)

	// Check for valid complexity setting
	celt_assert(complexity >= SILK_PE_MIN_COMPLEX)
	celt_assert(complexity <= SILK_PE_MAX_COMPLEX)

	silk_assert(search_thres1 >= 0.0 && search_thres1 <= 1.0)
	silk_assert(search_thres2 >= 0.0 && search_thres2 <= 1.0)

	// Set up frame lengths max / min lag for the sampling frequency
	frame_length = (PE_LTP_MEM_LENGTH_MS + nb_subfr*PE_SUBFR_LENGTH_MS) * Fs_kHz
	frame_length_4kHz = (PE_LTP_MEM_LENGTH_MS + nb_subfr*PE_SUBFR_LENGTH_MS) * 4
	frame_length_8kHz = (PE_LTP_MEM_LENGTH_MS + nb_subfr*PE_SUBFR_LENGTH_MS) * 8
	sf_length = PE_SUBFR_LENGTH_MS * Fs_kHz
	sf_length_4kHz = PE_SUBFR_LENGTH_MS * 4
	sf_length_8kHz = PE_SUBFR_LENGTH_MS * 8
	min_lag = PE_MIN_LAG_MS * Fs_kHz
	min_lag_4kHz = PE_MIN_LAG_MS * 4
	min_lag_8kHz = PE_MIN_LAG_MS * 8
	max_lag = PE_MAX_LAG_MS*Fs_kHz - 1
	max_lag_4kHz = PE_MAX_LAG_MS * 4
	max_lag_8kHz = PE_MAX_LAG_MS*8 - 1

	// Resample from input sampled at Fs_kHz to 8 kHz
	if Fs_kHz == 16 {
		// Resample to 16 -> 8 khz
		var frame_16_FIX [16 * PE_MAX_FRAME_LENGTH_MS]opus_int16
		silk_float2short_array(frame_16_FIX[:], frame, opus_int32(frame_length))
		for z := 0; z < 2; z++ {
			filt_state[z] = 0
		}
		silk_resampler_down2(filt_state[:], frame_8_FIX[:], frame_16_FIX[:], opus_int32(frame_length))
		silk_short2float_array(frame_8kHz[:], frame_8_FIX[:], opus_int32(frame_length_8kHz))
	} else if Fs_kHz == 12 {
		// Resample to 12 -> 8 khz
		var frame_12_FIX [12 * PE_MAX_FRAME_LENGTH_MS]opus_int16
		silk_float2short_array(frame_12_FIX[:], frame, opus_int32(frame_length))
		for z := 0; z < 6; z++ {
			filt_state[z] = 0
		}
		silk_resampler_down2_3(filt_state[:], frame_8_FIX[:], frame_12_FIX[:], opus_int32(frame_length))
		silk_short2float_array(frame_8kHz[:], frame_8_FIX[:], opus_int32(frame_length_8kHz))
	} else {
		celt_assert(Fs_kHz == 8)
		silk_float2short_array(frame_8_FIX[:], frame, opus_int32(frame_length_8kHz))
	}

	// Decimate again to 4 kHz
	for z := 0; z < 2; z++ {
		filt_state[z] = 0
	}
	silk_resampler_down2(filt_state[:], frame_4_FIX[:], frame_8_FIX[:], opus_int32(frame_length_8kHz))
	silk_short2float_array(frame_4kHz[:], frame_4_FIX[:], opus_int32(frame_length_4kHz))

	// Low-pass filter.
	// C: frame_4kHz[i] = silk_ADD_SAT16( frame_4kHz[i], frame_4kHz[i-1] );
	//   silk_ADD_SAT16 is defined on opus_int16, but here it's called on
	//   silk_float. The macro expands to
	//     (opus_int16) silk_SAT16( silk_ADD32((opus_int32)a, (opus_int32)b) )
	//   so each float operand is truncated toward zero to int32 before
	//   the saturating 16-bit add, and the result is an int16 which is
	//   then stored back implicitly into the silk_float array (an
	//   int16->float conversion). Since frame_4kHz was populated from
	//   silk_short2float_array values are already integral, but the
	//   explicit truncation must match C at -O2 with -ffp-contract=off.
	for i = frame_length_4kHz - 1; i > 0; i-- {
		frame_4kHz[i] = silk_float(
			opus_int16(silk_SAT16(silk_ADD32(opus_int32(frame_4kHz[i]), opus_int32(frame_4kHz[i-1])))))
	}

	//******************************************************************************
	// FIRST STAGE, operating in 4 khz
	//******************************************************************************
	for ii := opus_int(0); ii < nb_subfr; ii++ {
		for jj := 0; jj < (PE_MAX_LAG>>1)+5; jj++ {
			C[ii][jj] = 0
		}
	}
	targetOff := opus_int(silk_LSHIFT(opus_int32(sf_length_4kHz), 2))
	for k = 0; k < nb_subfr>>1; k++ {
		basisOff := targetOff - min_lag_4kHz

		celt_pitch_xcorr(frame_4kHz[targetOff:], frame_4kHz[targetOff-max_lag_4kHz:], xcorr[:], int(sf_length_8kHz), int(max_lag_4kHz-min_lag_4kHz+1), arch)

		// Calculate first vector products before loop
		cross_corr = float64(xcorr[max_lag_4kHz-min_lag_4kHz])
		// normalizer = energy(target) + energy(basis) + sf_length_8kHz * 4000.0f
		// C: sf_length_8kHz * 4000.0f is silk_float because 4000.0f is
		//   float. The outer sum is double because the two silk_energy_FLP
		//   calls return double; the silk_float addend is promoted.
		e_t := silk_energy_FLP(frame_4kHz[targetOff:], sf_length_8kHz)
		e_b := silk_energy_FLP(frame_4kHz[basisOff:], sf_length_8kHz)
		sfScale := mul_f32(float32(sf_length_8kHz), 4000.0) // float32
		// Left-to-right: (e_t + e_b) + sfScale.
		normalizer = add_f64(add_f64(e_t, e_b), float64(sfScale))

		// C[0][min_lag_4kHz] += (silk_float)( 2 * cross_corr / normalizer );
		// 2 is promoted to double by cross_corr. Division in double.
		addVal := silk_float(mul_f64(2.0, cross_corr) / normalizer)
		C[0][min_lag_4kHz] = add_f32(C[0][min_lag_4kHz], addVal)

		// From now on normalizer is computed recursively
		for d = min_lag_4kHz + 1; d <= max_lag_4kHz; d++ {
			basisOff--

			cross_corr = float64(xcorr[max_lag_4kHz-d])

			// Add contribution of new sample and remove contribution from oldest sample
			//   normalizer += basis[0]*(double)basis[0] - basis[sf_length_8kHz]*(double)basis[sf_length_8kHz]
			// RHS evaluated first: p1 - p2, then added to normalizer.
			b0 := float64(frame_4kHz[basisOff])
			bS := float64(frame_4kHz[basisOff+sf_length_8kHz])
			p1 := mul_f64(b0, b0)
			p2 := mul_f64(bS, bS)
			diff := sub_f64(p1, p2)
			normalizer = add_f64(normalizer, diff)
			addVal := silk_float(mul_f64(2.0, cross_corr) / normalizer)
			C[0][d] = add_f32(C[0][d], addVal)
		}
		// Update target pointer
		targetOff += sf_length_8kHz
	}

	// Apply short-lag bias.
	// C: C[0][i] -= C[0][i] * i / 4096.0f;
	//   The RHS is a float32 expression: (C[0][i] * i) is float32 * int -> float32, then / 4096.0f.
	//   Left-to-right evaluation with -ffp-contract=off.
	for i = max_lag_4kHz; i >= min_lag_4kHz; i-- {
		num := mul_f32(C[0][i], float32(i))
		term := num / 4096.0
		C[0][i] = sub_f32(C[0][i], term)
	}

	// Sort
	length_d_srch = 4 + 2*complexity
	celt_assert(3*length_d_srch <= PE_D_SRCH_LENGTH)
	silk_insertion_sort_decreasing_FLP(C[0][min_lag_4kHz:], d_srch[:], max_lag_4kHz-min_lag_4kHz+1, length_d_srch)

	// Escape if correlation is very low already here
	Cmax = C[0][min_lag_4kHz]
	if Cmax < 0.2 {
		for z := opus_int(0); z < nb_subfr; z++ {
			pitch_out[z] = 0
		}
		*LTPCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}

	threshold = mul_f32(search_thres1, Cmax)
	for i = 0; i < length_d_srch; i++ {
		// Convert to 8 kHz indices for the sorted correlation that exceeds the threshold
		if C[0][min_lag_4kHz+i] > threshold {
			d_srch[i] = opus_int(silk_LSHIFT(opus_int32(d_srch[i]+min_lag_4kHz), 1))
		} else {
			length_d_srch = i
			break
		}
	}
	celt_assert(length_d_srch > 0)

	for i = min_lag_8kHz - 5; i < max_lag_8kHz+5; i++ {
		d_comp[i] = 0
	}
	for i = 0; i < length_d_srch; i++ {
		d_comp[d_srch[i]] = 1
	}

	// Convolution
	for i = max_lag_8kHz + 3; i >= min_lag_8kHz; i-- {
		d_comp[i] += d_comp[i-1] + d_comp[i-2]
	}

	length_d_srch = 0
	for i = min_lag_8kHz; i < max_lag_8kHz+1; i++ {
		if d_comp[i+1] > 0 {
			d_srch[length_d_srch] = i
			length_d_srch++
		}
	}

	// Convolution
	for i = max_lag_8kHz + 3; i >= min_lag_8kHz; i-- {
		d_comp[i] += d_comp[i-1] + d_comp[i-2] + d_comp[i-3]
	}

	length_d_comp = 0
	for i = min_lag_8kHz; i < max_lag_8kHz+4; i++ {
		if d_comp[i] > 0 {
			d_comp[length_d_comp] = opus_int16(i - 2)
			length_d_comp++
		}
	}

	//**********************************************************************************
	// SECOND STAGE, operating at 8 kHz, on lag sections with high correlation
	//*************************************************************************************
	//*********************************************************************************
	// Find energy of each subframe projected onto its history, for a range of delays
	//*********************************************************************************
	for ii := opus_int(0); ii < PE_MAX_NB_SUBFR; ii++ {
		for jj := 0; jj < (PE_MAX_LAG>>1)+5; jj++ {
			C[ii][jj] = 0
		}
	}

	var target_base []silk_float
	var target_base_off opus_int
	if Fs_kHz == 8 {
		target_base = frame
		target_base_off = PE_LTP_MEM_LENGTH_MS * 8
	} else {
		target_base = frame_8kHz[:]
		target_base_off = PE_LTP_MEM_LENGTH_MS * 8
	}
	for k = 0; k < nb_subfr; k++ {
		energy_tmp = add_f64(silk_energy_FLP(target_base[target_base_off:], sf_length_8kHz), 1.0)
		for j = 0; j < length_d_comp; j++ {
			d = opus_int(d_comp[j])
			// basis_ptr = target_ptr - d
			cross_corr = silk_inner_product_FLP(target_base[target_base_off-d:], target_base[target_base_off:], sf_length_8kHz, arch)
			if cross_corr > 0.0 {
				energy = silk_energy_FLP(target_base[target_base_off-d:], sf_length_8kHz)
				// C[k][d] = (silk_float)( 2 * cross_corr / ( energy + energy_tmp ) );
				denom := add_f64(energy, energy_tmp)
				C[k][d] = silk_float(mul_f64(2.0, cross_corr) / denom)
			} else {
				C[k][d] = 0.0
			}
		}
		target_base_off += sf_length_8kHz
	}

	// search over lag range and lags codebook
	// scale factor for lag codebook, as a function of center lag
	CCmax = 0.0 // This value doesn't matter
	CCmax_b = -1000.0

	CBimax = 0 // To avoid returning undefined lag values
	lag = -1   // To check if lag with strong enough correlation has been found

	if prevLag > 0 {
		if Fs_kHz == 12 {
			prevLag = opus_int(silk_LSHIFT(opus_int32(prevLag), 1)) / 3
		} else if Fs_kHz == 16 {
			prevLag = opus_int(silk_RSHIFT(opus_int32(prevLag), 1))
		}
		prevLag_log2 = silk_log2(float64(prevLag))
	} else {
		prevLag_log2 = 0
	}

	// Set up stage 2 codebook based on number of subframes
	if nb_subfr == PE_MAX_NB_SUBFR {
		cbk_size = PE_NB_CBKS_STAGE2_EXT
		Lag_CB_ptr = flattenCBStage2()
		if Fs_kHz == 8 && complexity > SILK_PE_MIN_COMPLEX {
			// If input is 8 khz use a larger codebook here because it is last stage
			nb_cbk_search = PE_NB_CBKS_STAGE2_EXT
		} else {
			nb_cbk_search = PE_NB_CBKS_STAGE2
		}
	} else {
		cbk_size = PE_NB_CBKS_STAGE2_10MS
		Lag_CB_ptr = flattenCBStage2_10ms()
		nb_cbk_search = PE_NB_CBKS_STAGE2_10MS
	}

	for k = 0; k < length_d_srch; k++ {
		d = d_srch[k]
		for j = 0; j < nb_cbk_search; j++ {
			CC[j] = 0.0
			for i = 0; i < nb_subfr; i++ {
				// Try all codebooks
				// CC[j] += C[i][ d + matrix_ptr(Lag_CB_ptr,i,j,cbk_size) ]
				idx := d + opus_int(matrix_ptr(Lag_CB_ptr, i, j, cbk_size))
				CC[j] = add_f32(CC[j], C[i][idx])
			}
		}
		// Find best codebook
		CCmax_new = -1000.0
		CBimax_new = 0
		for i = 0; i < nb_cbk_search; i++ {
			if CC[i] > CCmax_new {
				CCmax_new = CC[i]
				CBimax_new = i
			}
		}

		// Bias towards shorter lags
		lag_log2 = silk_log2(float64(d))
		// CCmax_new_b = CCmax_new - PE_SHORTLAG_BIAS * nb_subfr * lag_log2;
		// C: PE_SHORTLAG_BIAS is 0.2 (a double — no trailing f). The
		//   expression is (PE_SHORTLAG_BIAS * nb_subfr * lag_log2).
		//   With 0.2 a double, (double * int) is double, then
		//   (double * silk_float) is double. The subtract from
		//   CCmax_new promotes CCmax_new to double, and the result is
		//   assigned to CCmax_new_b (silk_float) — narrowed.
		// Evaluate left-to-right in double.
		biasD := mul_f64(mul_f64(PE_SHORTLAG_BIAS, float64(nb_subfr)), float64(lag_log2))
		CCmax_new_b = silk_float(sub_f64(float64(CCmax_new), biasD))

		// Bias towards previous lag
		if prevLag > 0 {
			delta_lag_log2_sqr = sub_f32(lag_log2, prevLag_log2)
			delta_lag_log2_sqr = mul_f32(delta_lag_log2_sqr, delta_lag_log2_sqr)
			// CCmax_new_b -= PE_PREVLAG_BIAS * nb_subfr * (*LTPCorr) * delta_lag_log2_sqr / ( delta_lag_log2_sqr + 0.5f );
			// 0.5f (float); PE_PREVLAG_BIAS is 0.2 (double). So the
			// expression is in double (LTPCorr, delta_lag_log2_sqr
			// promoted).
			num := mul_f64(mul_f64(mul_f64(PE_PREVLAG_BIAS, float64(nb_subfr)), float64(*LTPCorr)), float64(delta_lag_log2_sqr))
			// denom = delta_lag_log2_sqr + 0.5f — this is float32 first,
			// then promoted to double when used.
			denomF := add_f32(delta_lag_log2_sqr, 0.5)
			CCmax_new_b = silk_float(sub_f64(float64(CCmax_new_b), num/float64(denomF)))
		}

		if CCmax_new_b > CCmax_b && // Find maximum biased correlation
			CCmax_new > mul_f32(float32(nb_subfr), search_thres2) { // Correlation needs to be high enough to be voiced
			CCmax_b = CCmax_new_b
			CCmax = CCmax_new
			lag = d
			CBimax = CBimax_new
		}
	}

	if lag == -1 {
		// No suitable candidate found
		for z := opus_int(0); z < PE_MAX_NB_SUBFR; z++ {
			pitch_out[z] = 0
		}
		*LTPCorr = 0.0
		*lagIndex = 0
		*contourIndex = 0
		return 1
	}

	// Output normalized correlation
	// C: *LTPCorr = (silk_float)( CCmax / nb_subfr );
	//   CCmax is silk_float; nb_subfr is int. Division promotes to
	//   whichever is wider; the cast asks for silk_float. In C both
	//   operands are promoted to double (because nb_subfr becomes
	//   double via the int-to-double conversion? No — actually / with
	//   a float and int promotes int to float). With -O2 the standard
	//   promotion is: float / int → float. But the `(silk_float)` cast
	//   doesn't force double arithmetic. So this is a single float32
	//   divide.
	*LTPCorr = CCmax / silk_float(nb_subfr)
	silk_assert(*LTPCorr >= 0.0)

	if Fs_kHz > 8 {
		// Search in original signal

		// Compensate for decimation
		silk_assert(lag == opus_int(silk_SAT16(opus_int32(lag))))
		if Fs_kHz == 12 {
			// C: silk_RSHIFT_ROUND( silk_SMULBB( lag, 3 ), 1 )
			lag = opus_int(silk_RSHIFT_ROUND(silk_SMULBB(opus_int32(lag), 3), 1))
		} else { // Fs_kHz == 16
			lag = opus_int(silk_LSHIFT(opus_int32(lag), 1))
		}

		lag = silk_LIMIT_int(lag, min_lag, max_lag)
		start_lag = silk_max_int(lag-2, min_lag)
		end_lag = silk_min_int(lag+2, max_lag)
		lag_new = lag // to avoid undefined lag
		CBimax = 0    // to avoid undefined lag

		CCmax = -1000.0

		// Calculate the correlations and energies needed in stage 3
		silk_P_Ana_calc_corr_st3(&cross_corr_st3, frame, start_lag, sf_length, nb_subfr, complexity, arch)
		silk_P_Ana_calc_energy_st3(&energies_st3, frame, start_lag, sf_length, nb_subfr, complexity)

		lag_counter = 0
		silk_assert(lag == opus_int(silk_SAT16(opus_int32(lag))))
		// C: contour_bias = PE_FLATCONTOUR_BIAS / lag;
		//   PE_FLATCONTOUR_BIAS is 0.05 (double). lag is int. Division
		//   in double; assigned to silk_float with narrowing.
		contour_bias = silk_float(PE_FLATCONTOUR_BIAS / float64(lag))

		// Set up cbk parameters according to complexity setting and frame length
		if nb_subfr == PE_MAX_NB_SUBFR {
			nb_cbk_search = opus_int(silk_nb_cbk_searchs_stage3[complexity])
			cbk_size = PE_NB_CBKS_STAGE3_MAX
			Lag_CB_ptr = flattenCBStage3()
		} else {
			nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
			cbk_size = PE_NB_CBKS_STAGE3_10MS
			Lag_CB_ptr = flattenCBStage3_10ms()
		}

		target_base_off = PE_LTP_MEM_LENGTH_MS * Fs_kHz
		energy_tmp = add_f64(silk_energy_FLP(frame[target_base_off:], nb_subfr*sf_length), 1.0)
		for d = start_lag; d <= end_lag; d++ {
			for j = 0; j < nb_cbk_search; j++ {
				cross_corr = 0.0
				energy = energy_tmp
				for k = 0; k < nb_subfr; k++ {
					cross_corr = add_f64(cross_corr, float64(cross_corr_st3[k][j][lag_counter]))
					energy = add_f64(energy, float64(energies_st3[k][j][lag_counter]))
				}
				if cross_corr > 0.0 {
					CCmax_new = silk_float(mul_f64(2.0, cross_corr) / energy)
					// Reduce depending on flatness of contour.
					// C: CCmax_new *= 1.0f - contour_bias * j;
					//   (1.0f - contour_bias*j) is float32. Multiply is float32.
					factor := sub_f32(1.0, mul_f32(contour_bias, float32(j)))
					CCmax_new = mul_f32(CCmax_new, factor)
				} else {
					CCmax_new = 0.0
				}

				if CCmax_new > CCmax && (d+opus_int(silk_CB_lags_stage3[0][j])) <= max_lag {
					CCmax = CCmax_new
					lag_new = d
					CBimax = j
				}
			}
			lag_counter++
		}

		for k = 0; k < nb_subfr; k++ {
			pitch_out[k] = lag_new + opus_int(matrix_ptr(Lag_CB_ptr, k, CBimax, cbk_size))
			pitch_out[k] = silk_LIMIT(pitch_out[k], min_lag, PE_MAX_LAG_MS*Fs_kHz)
		}
		*lagIndex = opus_int16(lag_new - min_lag)
		*contourIndex = opus_int8(CBimax)
	} else { // Fs_kHz == 8
		// Save Lags
		for k = 0; k < nb_subfr; k++ {
			pitch_out[k] = lag + opus_int(matrix_ptr(Lag_CB_ptr, k, CBimax, cbk_size))
			pitch_out[k] = silk_LIMIT(pitch_out[k], min_lag_8kHz, PE_MAX_LAG_MS*8)
		}
		*lagIndex = opus_int16(lag - min_lag_8kHz)
		*contourIndex = opus_int8(CBimax)
	}
	celt_assert(*lagIndex >= 0)
	// return as voiced
	return 0
}

// silk_P_Ana_calc_corr_st3 — stage-3 correlations.
func silk_P_Ana_calc_corr_st3(
	cross_corr_st3 *[PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX][PE_NB_STAGE3_LAGS]silk_float,
	frame []silk_float,
	start_lag opus_int,
	sf_length opus_int,
	nb_subfr opus_int,
	complexity opus_int,
	arch int,
) {
	var i, j, k, lag_counter, lag_low, lag_high opus_int
	var nb_cbk_search, delta, idx, cbk_size opus_int
	var scratch_mem [pitchScratchSize]silk_float
	var xcorr [pitchScratchSize]opus_val32
	var Lag_range_ptr, Lag_CB_ptr []opus_int8

	celt_assert(complexity >= SILK_PE_MIN_COMPLEX)
	celt_assert(complexity <= SILK_PE_MAX_COMPLEX)

	if nb_subfr == PE_MAX_NB_SUBFR {
		Lag_range_ptr = flattenLagRangeStage3(int(complexity))
		Lag_CB_ptr = flattenCBStage3()
		nb_cbk_search = opus_int(silk_nb_cbk_searchs_stage3[complexity])
		cbk_size = PE_NB_CBKS_STAGE3_MAX
	} else {
		celt_assert(nb_subfr == PE_MAX_NB_SUBFR>>1)
		Lag_range_ptr = flattenLagRangeStage3_10ms()
		Lag_CB_ptr = flattenCBStage3_10ms()
		nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
		cbk_size = PE_NB_CBKS_STAGE3_10MS
	}

	targetOff := opus_int(silk_LSHIFT(opus_int32(sf_length), 2)) // Pointer to middle of frame
	for k = 0; k < nb_subfr; k++ {
		lag_counter = 0

		// Calculate the correlations for each subframe
		lag_low = opus_int(matrix_ptr(Lag_range_ptr, k, 0, 2))
		lag_high = opus_int(matrix_ptr(Lag_range_ptr, k, 1, 2))
		silk_assert(lag_high-lag_low+1 <= pitchScratchSize)
		celt_pitch_xcorr(frame[targetOff:], frame[targetOff-start_lag-lag_high:], xcorr[:], int(sf_length), int(lag_high-lag_low+1), arch)
		for j = lag_low; j <= lag_high; j++ {
			silk_assert(lag_counter < pitchScratchSize)
			scratch_mem[lag_counter] = silk_float(xcorr[lag_high-j])
			lag_counter++
		}

		delta = opus_int(matrix_ptr(Lag_range_ptr, k, 0, 2))
		for i = 0; i < nb_cbk_search; i++ {
			// Fill out the 3 dim array that stores the correlations for
			// each code_book vector for each start lag
			idx = opus_int(matrix_ptr(Lag_CB_ptr, k, i, cbk_size)) - delta
			for j = 0; j < PE_NB_STAGE3_LAGS; j++ {
				silk_assert(idx+j < pitchScratchSize)
				silk_assert(idx+j < lag_counter)
				cross_corr_st3[k][i][j] = scratch_mem[idx+j]
			}
		}
		targetOff += sf_length
	}
}

// silk_P_Ana_calc_energy_st3 — stage-3 energies.
func silk_P_Ana_calc_energy_st3(
	energies_st3 *[PE_MAX_NB_SUBFR][PE_NB_CBKS_STAGE3_MAX][PE_NB_STAGE3_LAGS]silk_float,
	frame []silk_float,
	start_lag opus_int,
	sf_length opus_int,
	nb_subfr opus_int,
	complexity opus_int,
) {
	var energy float64
	var k, i, j, lag_counter opus_int
	var nb_cbk_search, delta, idx, cbk_size, lag_diff opus_int
	var scratch_mem [pitchScratchSize]silk_float
	var Lag_range_ptr, Lag_CB_ptr []opus_int8

	celt_assert(complexity >= SILK_PE_MIN_COMPLEX)
	celt_assert(complexity <= SILK_PE_MAX_COMPLEX)

	if nb_subfr == PE_MAX_NB_SUBFR {
		Lag_range_ptr = flattenLagRangeStage3(int(complexity))
		Lag_CB_ptr = flattenCBStage3()
		nb_cbk_search = opus_int(silk_nb_cbk_searchs_stage3[complexity])
		cbk_size = PE_NB_CBKS_STAGE3_MAX
	} else {
		celt_assert(nb_subfr == PE_MAX_NB_SUBFR>>1)
		Lag_range_ptr = flattenLagRangeStage3_10ms()
		Lag_CB_ptr = flattenCBStage3_10ms()
		nb_cbk_search = PE_NB_CBKS_STAGE3_10MS
		cbk_size = PE_NB_CBKS_STAGE3_10MS
	}

	targetOff := opus_int(silk_LSHIFT(opus_int32(sf_length), 2))
	for k = 0; k < nb_subfr; k++ {
		lag_counter = 0

		// Calculate the energy for first lag
		// basis_ptr = target_ptr - ( start_lag + Lag_range_ptr[k][0] )
		basisOff := targetOff - (start_lag + opus_int(matrix_ptr(Lag_range_ptr, k, 0, 2)))
		energy = add_f64(silk_energy_FLP(frame[basisOff:], sf_length), 1e-3)
		silk_assert(energy >= 0.0)
		scratch_mem[lag_counter] = silk_float(energy)
		lag_counter++

		lag_diff = opus_int(matrix_ptr(Lag_range_ptr, k, 1, 2)) - opus_int(matrix_ptr(Lag_range_ptr, k, 0, 2)) + 1
		for i = 1; i < lag_diff; i++ {
			// remove part outside new window
			bS := float64(frame[basisOff+sf_length-i])
			pS := mul_f64(bS, bS)
			energy = sub_f64(energy, pS)
			silk_assert(energy >= 0.0)

			// add part that comes into window
			bI := float64(frame[basisOff-i])
			pI := mul_f64(bI, bI)
			energy = add_f64(energy, pI)
			silk_assert(energy >= 0.0)
			silk_assert(lag_counter < pitchScratchSize)
			scratch_mem[lag_counter] = silk_float(energy)
			lag_counter++
		}

		delta = opus_int(matrix_ptr(Lag_range_ptr, k, 0, 2))
		for i = 0; i < nb_cbk_search; i++ {
			// Fill out the 3 dim array that stores the correlations for
			// each code_book vector for each start lag
			idx = opus_int(matrix_ptr(Lag_CB_ptr, k, i, cbk_size)) - delta
			for j = 0; j < PE_NB_STAGE3_LAGS; j++ {
				silk_assert(idx+j < pitchScratchSize)
				silk_assert(idx+j < lag_counter)
				energies_st3[k][i][j] = scratch_mem[idx+j]
				silk_assert(energies_st3[k][i][j] >= 0.0)
			}
		}
		targetOff += sf_length
	}
}
