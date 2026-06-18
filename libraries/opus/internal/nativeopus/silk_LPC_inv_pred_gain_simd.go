package nativeopus

// SIMD-style (4-lane) reference port of libopus'
// silk_LPC_inverse_pred_gain_neon (silk/arm/LPC_inv_pred_gain_neon_intr.c).
//
// This file contains a pure-Go implementation that mirrors the NEON
// kernel's data-level parallelism: the hot inner loop works on 4-lane
// int32 vectors (represented as [4]opus_int32) with saturating /
// rounding helpers that are 1:1 with the NEON intrinsics used by the
// C reference. No compiler intrinsics, no asm — the algorithm itself
// is verified here so the arm64 asm port has a proven structural
// template to match.
//
// Guarantees:
//   * Bit-exact with silk_LPC_inverse_pred_gain_QA_c for all valid
//     inputs (see parity tests in parity_tests/benchcmp).
//   * The same saturation / overflow-detection behaviour as the NEON
//     kernel, including the narrowing-shift overflow trick (see
//     comment block in the C source, lines 41-51).

import "math"

// ── Per-lane NEON intrinsic analogues ───────────────────────────────

// vqrdmulhqLaneRef — saturating rounding doubling multiply-high by a
// broadcast scalar. 1:1 with vqrdmulhq_lane_s32(a, b, 0) where b is a
// 2-lane int32 vector with the scalar in lane 0.
//
//	result[i] = sat32( (2 * a[i] * coef + 2^31) >> 32 )
//	         = sat32( (a[i] * coef + 2^30) >> 31 )
//
// Saturation triggers only when a[i] == coef == INT32_MIN (result
// would be 2^31 which is out of range); A_LIMIT prevents this in the
// actual LPC stability algorithm, but we preserve it for parity with
// the NEON intrinsic.
func vqrdmulhqLaneRef(a [4]opus_int32, coef opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		p := int64(a[i])*int64(coef) + (1 << 30)
		r := p >> 31
		if r > math.MaxInt32 {
			r = math.MaxInt32
		} else if r < math.MinInt32 {
			r = math.MinInt32
		}
		out[i] = opus_int32(r)
	}
	return out
}

// vqsubqS32Ref — saturating subtract, element-wise. 1:1 with
// vqsubq_s32.
func vqsubqS32Ref(a, b [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		out[i] = silk_SUB_SAT32(a[i], b[i])
	}
	return out
}

// vmullS32Ref — widening multiply: 2 int32 lanes → 2 int64 lanes.
// 1:1 with vmull_s32.
func vmullS32Ref(a, b [2]opus_int32) [2]opus_int64 {
	return [2]opus_int64{
		opus_int64(a[0]) * opus_int64(b[0]),
		opus_int64(a[1]) * opus_int64(b[1]),
	}
}

// vrshlqS64Ref — variable rounding shift on 2 int64 lanes. Matches
// vrshlq_s64 semantics: positive shift == left shift; negative shift
// == right shift with rounding (half-ulp added before the shift).
// The C kernel always passes the same shift in both lanes, but the
// intrinsic supports per-lane shifts; we preserve that here for
// fidelity.
func vrshlqS64Ref(a [2]opus_int64, shift [2]int) [2]opus_int64 {
	var out [2]opus_int64
	for i := 0; i < 2; i++ {
		s := shift[i]
		switch {
		case s >= 0:
			// Left shift. No rounding applies to left shifts.
			out[i] = opus_int64(uint64(a[i]) << uint(s))
		case s == 0:
			out[i] = a[i]
		default:
			rs := -s
			// Rounding: add half-ulp then shift. Matches
			// silk_RSHIFT_ROUND64 exactly.
			if rs == 1 {
				out[i] = (a[i] >> 1) + (a[i] & 1)
			} else {
				out[i] = ((a[i] >> (rs - 1)) + 1) >> 1
			}
		}
	}
	return out
}

// vmovnS64Ref — truncating narrow: int64 → int32 (low 32 bits).
// 1:1 with vmovn_s64.
func vmovnS64Ref(a [2]opus_int64) [2]opus_int32 {
	return [2]opus_int32{opus_int32(a[0]), opus_int32(a[1])}
}

// vshrnS64Ref31 — narrowing shift right by 31 (low 32 bits after
// arithmetic right shift). 1:1 with vshrn_n_s64(a, 31).
func vshrnS64Ref31(a [2]opus_int64) [2]opus_int32 {
	return [2]opus_int32{opus_int32(a[0] >> 31), opus_int32(a[1] >> 31)}
}

// vcombineS32Ref — concatenate two 2-lane int32 vectors into a 4-lane
// vector: low || high.
func vcombineS32Ref(lo, hi [2]opus_int32) [4]opus_int32 {
	return [4]opus_int32{lo[0], lo[1], hi[0], hi[1]}
}

// vgetLowS32Ref / vgetHighS32Ref — split a 4-lane int32 vector.
func vgetLowS32Ref(a [4]opus_int32) [2]opus_int32  { return [2]opus_int32{a[0], a[1]} }
func vgetHighS32Ref(a [4]opus_int32) [2]opus_int32 { return [2]opus_int32{a[2], a[3]} }

// vrev64qS32Ref — reverse elements within each 64-bit lane, for a
// 4-lane int32 vector. 1:1 with vrev64q_s32.
//
//	in  = [a, b, c, d]
//	out = [b, a, d, c]
func vrev64qS32Ref(a [4]opus_int32) [4]opus_int32 {
	return [4]opus_int32{a[1], a[0], a[3], a[2]}
}

// vmaxqS32Ref / vminqS32Ref — elementwise max/min.
func vmaxqS32Ref(a, b [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		if a[i] > b[i] {
			out[i] = a[i]
		} else {
			out[i] = b[i]
		}
	}
	return out
}
func vminqS32Ref(a, b [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		if a[i] < b[i] {
			out[i] = a[i]
		} else {
			out[i] = b[i]
		}
	}
	return out
}

// ── Phase A: core SIMD-style QA kernel ──────────────────────────────

// silk_LPC_inverse_pred_gain_QA_simd_ref — pure-Go SIMD-style
// reference. Structure and data flow match
// LPC_inverse_pred_gain_QA_neon in libopus exactly: the outer stability
// / Q-domain updates are scalar, and the AR-coefficient update is a
// 4-lane SIMD block followed by a scalar tail. Returns the same
// invGain_Q30 as silk_LPC_inverse_pred_gain_QA_c, or 0 on instability.
func silk_LPC_inverse_pred_gain_QA_simd_ref(A_QA []opus_int32, order opus_int) opus_int32 {
	var k, n, mult2Q opus_int
	var invGain_Q30, rc_Q31, rc_mult1_Q30, rc_mult2, tmp1, tmp2 opus_int32
	var maxV, minV [4]opus_int32

	for i := range maxV {
		maxV[i] = silk_int32_MIN
		minV[i] = silk_int32_MAX
	}
	invGain_Q30 = SILK_FIX_CONST(1, 30)

	for k = order - 1; k > 0; k-- {
		// Stability check.
		if A_QA[k] > silk_inv_pred_A_LIMIT || A_QA[k] < -silk_inv_pred_A_LIMIT {
			return 0
		}

		// RC = negated AR coef, scaled to Q31.
		rc_Q31 = -silk_LSHIFT(A_QA[k], 31-silk_inv_pred_QA)

		// rc_mult1_Q30 range: [1, 2^30].
		rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31))
		silk_assert(rc_mult1_Q30 > (1 << 15))
		silk_assert(rc_mult1_Q30 <= (1 << 30))

		// Update inverse gain.
		invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2)
		silk_assert(invGain_Q30 >= 0)
		silk_assert(invGain_Q30 <= (1 << 30))
		if invGain_Q30 < SILK_FIX_CONST(1.0/MAX_PREDICTION_POWER_GAIN, 30) {
			return 0
		}

		// rc_mult2 range: [2^30, silk_int32_MAX].
		mult2Q = 32 - opus_int(silk_CLZ32(silk_abs(rc_mult1_Q30)))
		rc_mult2 = silk_INVERSE32_varQ(rc_mult1_Q30, mult2Q+30)

		// 4-lane SIMD inner loop.
		negMult2Q := [2]int{int(-mult2Q), int(-mult2Q)}
		for n = 0; n < ((k+1)>>1)-3; n += 4 {
			// Load tmp1 = A_QA[n..n+3].
			var tmp1V [4]opus_int32
			copy(tmp1V[:], A_QA[n:n+4])

			// Load tmp2 = A_QA[k-n-4..k-n-1], then reverse-pair to
			// mirror the NEON vrev64q + vcombine swap, which puts
			// A_QA[k-n-1] in lane 0 (matching the scalar index
			// pairing (n, k-n-1)).
			var tmp2V [4]opus_int32
			copy(tmp2V[:], A_QA[k-n-4:k-n])
			tmp2V = vrev64qS32Ref(tmp2V)
			tmp2V = vcombineS32Ref(vgetHighS32Ref(tmp2V), vgetLowS32Ref(tmp2V))

			// t0 = MUL32_FRAC_Q(tmp2, rc_Q31, 31) lane-wise.
			// t1 = MUL32_FRAC_Q(tmp1, rc_Q31, 31) lane-wise.
			t0 := vqrdmulhqLaneRef(tmp2V, rc_Q31)
			t1 := vqrdmulhqLaneRef(tmp1V, rc_Q31)

			// t_QA0 = SUB_SAT(tmp1, t0). t_QA1 = SUB_SAT(tmp2, t1).
			tQA0 := vqsubqS32Ref(tmp1V, t0)
			tQA1 := vqsubqS32Ref(tmp2V, t1)

			// Widen: 4 lanes → 2×(2-lane int64 vectors).
			t0_lo := vmullS32Ref(vgetLowS32Ref(tQA0), [2]opus_int32{rc_mult2, rc_mult2})
			t1_hi := vmullS32Ref(vgetHighS32Ref(tQA0), [2]opus_int32{rc_mult2, rc_mult2})
			t2_lo := vmullS32Ref(vgetLowS32Ref(tQA1), [2]opus_int32{rc_mult2, rc_mult2})
			t3_hi := vmullS32Ref(vgetHighS32Ref(tQA1), [2]opus_int32{rc_mult2, rc_mult2})

			// Rounding right shift by mult2Q (expressed as a
			// negative left shift to vrshlq_s64).
			t0_lo = vrshlqS64Ref(t0_lo, negMult2Q)
			t1_hi = vrshlqS64Ref(t1_hi, negMult2Q)
			t2_lo = vrshlqS64Ref(t2_lo, negMult2Q)
			t3_hi = vrshlqS64Ref(t3_hi, negMult2Q)

			// Narrow-truncate to int32 for storage.
			t0_s32 := vcombineS32Ref(vmovnS64Ref(t0_lo), vmovnS64Ref(t1_hi))
			t1_s32 := vcombineS32Ref(vmovnS64Ref(t2_lo), vmovnS64Ref(t3_hi))

			// Narrow-shift-right-by-31: s0 / s1 capture bits 31..62
			// of each 64-bit lane. For a value that fits in int32,
			// these bits are all 0 or all 1 (sign extension); any
			// other pattern flags a 32-bit overflow. Tracked via
			// max/min across the whole SIMD pass — checked at the
			// end.
			s0_s32 := vcombineS32Ref(vshrnS64Ref31(t0_lo), vshrnS64Ref31(t1_hi))
			s1_s32 := vcombineS32Ref(vshrnS64Ref31(t2_lo), vshrnS64Ref31(t3_hi))
			maxV = vmaxqS32Ref(maxV, s0_s32)
			minV = vminqS32Ref(minV, s0_s32)
			maxV = vmaxqS32Ref(maxV, s1_s32)
			minV = vminqS32Ref(minV, s1_s32)

			// Reverse t1 back for the A_QA[k-n-4..k-n-1] store.
			t1_s32 = vrev64qS32Ref(t1_s32)
			t1_s32 = vcombineS32Ref(vgetHighS32Ref(t1_s32), vgetLowS32Ref(t1_s32))

			copy(A_QA[n:n+4], t0_s32[:])
			copy(A_QA[k-n-4:k-n], t1_s32[:])
		}

		// Scalar tail. Note: the SIMD block above may have
		// speculatively updated A_QA entries past (k+1)>>1 when
		// k is not a multiple of 8, but that is harmless: those
		// entries are paired with n < (k+1)>>1 and will still be
		// overwritten with correct values below, OR they lie in
		// the "overflow-check via max/min" window which gates the
		// whole return at the end of the outer loop. This mirrors
		// the C comment "We always calculate extra elements of
		// A_QA buffer when (k % 4) != 0".
		for ; n < (k+1)>>1; n++ {
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

	// Stability check on A_QA[0].
	if A_QA[k] > silk_inv_pred_A_LIMIT || A_QA[k] < -silk_inv_pred_A_LIMIT {
		return 0
	}

	// Horizontal reduction of the SIMD overflow tracker: if any lane
	// of any SIMD iteration produced a tmp64 outside int32 range, the
	// shifted top bits are not uniformly 0 or -1, so max > 0 or
	// min < -1. This is the NEON kernel's clever 32-bit-overflow
	// detector explained in the C source block comment.
	var gmax opus_int32 = silk_int32_MIN
	var gmin opus_int32 = silk_int32_MAX
	for i := 0; i < 4; i++ {
		if maxV[i] > gmax {
			gmax = maxV[i]
		}
		if minV[i] < gmin {
			gmin = minV[i]
		}
	}
	if gmax > 0 || gmin < -1 {
		return 0
	}

	// Finalise: mirror C's tail-end inverse-gain update on A_QA[0].
	rc_Q31 = -silk_LSHIFT(A_QA[0], 31-silk_inv_pred_QA)
	rc_mult1_Q30 = silk_SUB32(SILK_FIX_CONST(1, 30), silk_SMMUL(rc_Q31, rc_Q31))
	invGain_Q30 = silk_LSHIFT(silk_SMMUL(invGain_Q30, rc_mult1_Q30), 2)
	silk_assert(invGain_Q30 >= 0)
	silk_assert(invGain_Q30 <= (1 << 30))
	if invGain_Q30 < SILK_FIX_CONST(1.0/MAX_PREDICTION_POWER_GAIN, 30) {
		return 0
	}
	return invGain_Q30
}

// silk_LPC_inverse_pred_gain_simd_ref — Q12 entry point that delegates
// to silk_LPC_inverse_pred_gain_QA_simd_ref. Mirrors the C wrapper
// exactly (same DC-response precheck, same Q12→QA shift). Provided so
// the parity test can compare whole-pipeline bit-exactness, not just
// the inner QA kernel.
func silk_LPC_inverse_pred_gain_simd_ref(A_Q12 []opus_int16, order opus_int) opus_int32 {
	var Atmp_QA [SILK_MAX_ORDER_LPC]opus_int32
	var DC_resp opus_int32
	for k := opus_int(0); k < order; k++ {
		DC_resp += opus_int32(A_Q12[k])
		Atmp_QA[k] = silk_LSHIFT32(opus_int32(A_Q12[k]), silk_inv_pred_QA-12)
	}
	if DC_resp >= 4096 {
		return 0
	}
	return silk_LPC_inverse_pred_gain_QA_simd_ref(Atmp_QA[:], order)
}
