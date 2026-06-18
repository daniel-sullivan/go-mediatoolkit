package nativeopus

// SIMD-style (4-lane) reference port of libopus'
// silk_biquad_alt_stride2_neon (silk/arm/biquad_alt_neon_intr.c).
//
// This file contains a pure-Go implementation that mirrors the NEON
// kernel's data-level parallelism: it tracks a 4-lane int32 state
// vector with layout {S0_ch0, S0_ch1, S1_ch0, S1_ch1} — i.e. the same
// two-element-per-channel state the scalar kernel uses, but
// reshuffled to match the NEON load/trn pattern — and processes two
// stereo sample-pairs per outer iteration exactly as the NEON code
// does.
//
// Each lane analogue of a NEON intrinsic is expressed via a small
// helper so the structural correspondence with biquad_alt_neon_intr.c
// is 1:1. The helpers are kept private to this file to avoid namespace
// pollution; they cover the exact subset used by the biquad kernel:
// vqdmulhq_s32 (saturating doubling multiply-high), vqdmulhq_lane_s32,
// vshll_n_s16, vqshrn_n_s32, vrsraq_n_s32, vzip_s32/vtrn_s32/
// vcombine_s32. No compiler intrinsics, no asm — the algorithm itself
// is verified here so a future asm port has a proven structural
// template to match.
//
// Guarantees: bit-exact with silk_biquad_alt_stride2_c for all valid
// inputs (see parity tests in parity_tests/benchcmp).

import "math"

// ── Per-lane NEON intrinsic analogues ───────────────────────────────

// vqdmulhqS32Ref — saturating doubling multiply-high, element-wise.
// 1:1 with vqdmulhq_s32.
//
//	result[i] = sat32( (2 * a[i] * b[i]) >> 32 )
//
// Saturation only fires when a[i] == b[i] == INT32_MIN (product would
// be 2^63 which saturates to INT32_MAX). We preserve the exact
// semantics for parity with the intrinsic.
func vqdmulhqS32Ref(a, b [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		p := int64(a[i]) * int64(b[i])
		// Doubled, then top 32 bits.
		r := (p << 1) >> 32
		// Saturation: only the INT32_MIN*INT32_MIN corner overflows.
		if a[i] == math.MinInt32 && b[i] == math.MinInt32 {
			r = math.MaxInt32
		}
		out[i] = opus_int32(r)
	}
	return out
}

// vshllNS1615Ref — widening shift left by 15 (int16 → int32). 1:1 with
// vshll_n_s16(x, 15). Sign-extends each lane to int32 then shifts.
func vshllNS1615Ref(a [4]opus_int16) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		out[i] = opus_int32(a[i]) << 15
	}
	return out
}

// vqshrnNS3214Ref — saturating narrow shift right by 14 (int32 →
// int16). 1:1 with vqshrn_n_s32(x, 14). Arithmetic-shift each lane
// then saturate to int16.
func vqshrnNS3214Ref(a [4]opus_int32) [4]opus_int16 {
	var out [4]opus_int16
	for i := range a {
		v := a[i] >> 14
		switch {
		case v > math.MaxInt16:
			out[i] = math.MaxInt16
		case v < math.MinInt16:
			out[i] = math.MinInt16
		default:
			out[i] = opus_int16(v)
		}
	}
	return out
}

// vrsraqNS3214Ref — rounding shift right by 14 and accumulate. 1:1
// with vrsraq_n_s32(acc, val, 14). The rounding is "add 2^13 then
// arithmetic-shift-right by 14", which matches silk_RSHIFT_ROUND with
// shift=14.
func vrsraqNS3214Ref(acc, val [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range acc {
		// rounding right shift by 14: (x + (1<<13)) >> 14 when shift>1.
		rr := (val[i]>>(14-1) + 1) >> 1
		out[i] = acc[i] + rr
	}
	return out
}

// vshlNS3202Ref — logical/arithmetic left shift by 2 (4 lanes). 1:1
// with vshl_n_s32(x, 2). Semantically a wrap-around shift (matches C
// `x << 2` on an int32, which the scalar silk_LSHIFT emulates).
func vshlNS3202Ref(a [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		// Use uint32 to get wrapping shift semantics.
		out[i] = opus_int32(uint32(a[i]) << 2)
	}
	return out
}

// vaddqS32BiquadRef / vsubqS32BiquadRef — wrapping int32 add / sub.
// 1:1 with vaddq_s32 / vsubq_s32 (NEON integer add is not saturating).
func vaddqS32BiquadRef(a, b [4]opus_int32) [4]opus_int32 {
	var out [4]opus_int32
	for i := range a {
		out[i] = opus_int32(uint32(a[i]) + uint32(b[i]))
	}
	return out
}

// ── Kernel ──────────────────────────────────────────────────────────

// silk_biquad_alt_stride2_simd_ref — pure-Go 4-lane SIMD-style
// reference. Structure and data flow match silk_biquad_alt_stride2_neon
// in libopus exactly.
//
// Lane convention for the running 4-lane state vector S_v4:
//
//	lane 0 : channel-0 lower state (scalar S[0])
//	lane 1 : channel-1 lower state (scalar S[2])
//	lane 2 : channel-0 upper state (scalar S[1])
//	lane 3 : channel-1 upper state (scalar S[3])
//
// This matches the NEON reshuffle at C file lines 101-103 (vld1q +
// vtrn → recombined lane order {S[0], S[2], S[1], S[3]}). On entry
// we perform the same shuffle; on exit we unshuffle back.
//
// Bit-exact with silk_biquad_alt_stride2_c.
func silk_biquad_alt_stride2_simd_ref(
	in_ []opus_int16, B_Q28, A_Q28 []opus_int32,
	S []opus_int32, out []opus_int16, len_ opus_int32,
) {
	// ----- Precompute per-channel coefficient vectors -----
	//
	// Matches C lines 85-100. A_Q28 has 2 entries; negated and split
	// into lower (14-bit) and upper (18-bit) halves, then each half
	// is pre-shifted by 15 so that a subsequent vqdmulhq_s32 yields
	// exactly silk_SMULWB(.., A_{L|U}_Q28) in Q-format.
	//
	// The pre-shift trick: silk_SMULWB(x, y) = (x * int16(y)) >> 16
	// and vqdmulhq_s32(a, b) = sat((2*a*b) >> 32). If y' = y << 15,
	// vqdmulhq_s32(x, y') ≈ (2*x*(y<<15)) >> 32 = (x*y) >> 16 = SMULWB
	// (saturation only at the INT32_MIN×INT32_MIN corner, which is
	// bounded away by A_Q28 magnitudes).
	//
	// For the lower half, the shift sequence is vshl_n_s32 << 18
	// followed by a logical-right-shift-by-3 (emulating << 15 but
	// explicitly clearing the top 3 bits). The logical shift is
	// critical for the sign-conforming behaviour noted in the C
	// comment at line 87-88.
	//
	// For the upper half, we take the arithmetic-right-shift by 14
	// first (discarding the low 14 bits), then shift left 16 and
	// right 1 (signed) — net effect << 15 with clipped top bits.

	negA0 := uint32(-A_Q28[0])
	negA1 := uint32(-A_Q28[1])

	// Lower 14 bits × 2 channels, pre-shifted << 15. Equivalent to
	// ((-A & 0x3FFF) << 15) but derived via (((-A)<<18) >> 3) so the
	// top bits match the NEON vshr_n_u32 (logical shift) path exactly.
	// For -A_Q28 the result is simply (-A & 0x3FFF) << 15 = the low
	// 14 bits shifted up 15 positions; keep it as a uint32 then cast.
	A0_L := opus_int32((negA0 & 0x3FFF) << 15)
	A1_L := opus_int32((negA1 & 0x3FFF) << 15)

	// Upper 18 bits × 2 channels, pre-shifted << 15. Derived via
	// the NEON sequence (vshr_n_s32 14 → vshl_n_s32 16 → vshr_n_s32 1)
	// which yields silk_RSHIFT(-A, 14) << 15 with the top two bits
	// clipped to conform to the C reference's int32 domain (per
	// comment at line 90 of biquad_alt_neon_intr.c). Reproduced
	// bit-for-bit here: the intermediate << 16 is a wrapping shift on
	// int32 and the final >> 1 is the signed arithmetic shift.
	A0_U := opus_int32(uint32(opus_int32(negA0)>>14)<<16) >> 1
	A1_U := opus_int32(uint32(opus_int32(negA1)>>14)<<16) >> 1

	// Broadcast coefficient vectors, pattern {A0, A0, A1, A1} for
	// A_{L,U} (matches NEON's A{0,0,1,1} layout, see vzip+vcombine
	// at C lines 95-99). Lanes 0,1 pair with S_v4 lanes 0,1 (the
	// two channels' lower state); lanes 2,3 pair with S_v4 lanes
	// 2,3 (the two channels' upper state).
	A_L_v4 := [4]opus_int32{A0_L, A0_L, A1_L, A1_L}
	A_U_v4 := [4]opus_int32{A0_U, A0_U, A1_U, A1_U}

	// B_Q28 lanes {B1, B1, B2, B2}, NOT pre-shifted. The SMULWB
	// substitute for the B side relies on the *input* being pre-
	// shifted <<15 (in_v4 = vshll_n_s16(in, 15)) rather than the
	// coefficient. Layout matches NEON's B_Q28[{1,1,2,2}] after the
	// vzip at C line 97.
	B_v4 := [4]opus_int32{B_Q28[1], B_Q28[1], B_Q28[2], B_Q28[2]}

	// B_Q28[0] broadcast (NOT pre-shifted, same reasoning).
	B0 := B_Q28[0]

	// Offset vector: (1 << 14) - 1 per lane.
	offset := opus_int32((1 << 14) - 1)
	offset_v4 := [4]opus_int32{offset, offset, offset, offset}

	// ----- Initial state shuffle -----
	//
	// NEON: S_s32x4 = vld1q(S); then vtrn on halves to get
	// {S[0], S[2], S[1], S[3]}.
	S_v4 := [4]opus_int32{S[0], S[2], S[1], S[3]}

	// ----- Main loop: 2 stereo samples per iteration (k += 2) -----
	var k opus_int32
	for ; k < len_-1; k += 2 {
		// Load 4 samples of interleaved input: {in[2k], in[2k+1],
		// in[2k+2], in[2k+3]} = {ch0-t, ch1-t, ch0-t+1, ch1-t+1}.
		in_s16 := [4]opus_int16{
			in_[2*k+0], in_[2*k+1],
			in_[2*k+2], in_[2*k+3],
		}
		in_v4 := vshllNS1615Ref(in_s16) // all 4 lanes << 15

		// t_s32x4 = vqdmulhq_lane_s32(in_v4, B0_pre) — saturating
		// doubling multiply-high of in_v4 by the broadcast scalar
		// B0_pre. Equivalent to: per-lane SMULWB(in_i << 15, B[0]).
		t_v4 := vqdmulhqS32Ref(in_v4, [4]opus_int32{B0, B0, B0, B0})

		// Split in_v4 into two {ch0, ch1, ch0, ch1} 4-lane vectors,
		// one per timestep.
		in_t0 := [4]opus_int32{in_v4[0], in_v4[1], in_v4[0], in_v4[1]}
		in_t1 := [4]opus_int32{in_v4[2], in_v4[3], in_v4[2], in_v4[3]}

		// Run two kernel iterations back-to-back, sharing S_v4.
		var out_t0, out_t1 [2]opus_int32
		S_v4, out_t0 = biquadAltStride2KernelRef(
			A_L_v4, A_U_v4, B_v4,
			[2]opus_int32{t_v4[0], t_v4[1]}, // low half of t_v4
			in_t0, S_v4,
		)
		S_v4, out_t1 = biquadAltStride2KernelRef(
			A_L_v4, A_U_v4, B_v4,
			[2]opus_int32{t_v4[2], t_v4[3]}, // high half of t_v4
			in_t1, S_v4,
		)

		// Pack 4 × out32_Q14 (2 from each kernel call), add offset,
		// saturating-narrow >> 14, and store as 4 × int16.
		out_v4 := [4]opus_int32{out_t0[0], out_t0[1], out_t1[0], out_t1[1]}
		out_v4 = vaddqS32BiquadRef(out_v4, offset_v4)
		out_s16 := vqshrnNS3214Ref(out_v4)
		out[2*k+0] = out_s16[0]
		out[2*k+1] = out_s16[1]
		out[2*k+2] = out_s16[2]
		out[2*k+3] = out_s16[3]
	}

	// ----- Leftover: 1 stereo sample (if len_ odd) -----
	if k < len_ {
		in_s16 := [4]opus_int16{in_[2*k+0], in_[2*k+1], 0, 0}
		in_v4 := vshllNS1615Ref(in_s16)
		// Same lane broadcast: take low half of the << 15 result.
		t_lo := vqdmulhqS32Ref(
			[4]opus_int32{in_v4[0], in_v4[1], in_v4[0], in_v4[1]},
			[4]opus_int32{B0, B0, B0, B0},
		)
		in_t0 := [4]opus_int32{in_v4[0], in_v4[1], in_v4[0], in_v4[1]}

		var out_t0 [2]opus_int32
		S_v4, out_t0 = biquadAltStride2KernelRef(
			A_L_v4, A_U_v4, B_v4,
			[2]opus_int32{t_lo[0], t_lo[1]},
			in_t0, S_v4,
		)

		out_v4 := [4]opus_int32{out_t0[0], out_t0[1], out_t0[0], out_t0[1]}
		out_v4 = vaddqS32BiquadRef(out_v4, offset_v4)
		out_s16 := vqshrnNS3214Ref(out_v4)
		out[2*k+0] = out_s16[0]
		out[2*k+1] = out_s16[1]
	}

	// ----- Unshuffle state back to scalar layout -----
	//
	// NEON stores S[0]=lane0, S[1]=lane2, S[2]=lane1, S[3]=lane3 via
	// individual vst1q_lane_s32 at C lines 146-149.
	S[0] = S_v4[0]
	S[1] = S_v4[2]
	S[2] = S_v4[1]
	S[3] = S_v4[3]
}

// biquadAltStride2KernelRef — inline per-iteration helper. Mirrors
// silk_biquad_alt_stride2_kernel (C lines 39-53) exactly.
//
// Inputs:
//   - A_L_v4, A_U_v4 : coefficient vectors {A0_L, A1_L, A0_L, A1_L} etc.
//   - B_v4           : coefficient vector {B1, B1, B2, B2}, pre-shifted.
//   - t_lo           : 2 lanes of the B[0]·in pre-computed partial.
//   - in_v4          : {ch0-t, ch1-t, ch0-t, ch1-t} (input pre-shifted).
//   - S_v4           : current state vector, layout described above.
//
// Returns updated S_v4 and the 2-lane out32_Q14 (channel pair for
// this timestep).
func biquadAltStride2KernelRef(
	A_L_v4, A_U_v4, B_v4 [4]opus_int32,
	t_lo [2]opus_int32,
	in_v4 [4]opus_int32,
	S_v4 [4]opus_int32,
) (newS [4]opus_int32, out32_Q14 [2]opus_int32) {
	// out32_Q14[0,1] = S[0..1] + t_lo[0..1].
	out32_Q14[0] = opus_int32(uint32(S_v4[0]) + uint32(t_lo[0]))
	out32_Q14[1] = opus_int32(uint32(S_v4[1]) + uint32(t_lo[1]))

	// S_v4 = vcombine(vget_high(S_v4), 0) — shift "upper" state down
	// to "lower" slot; upper slot zero'd.
	S_v4 = [4]opus_int32{S_v4[2], S_v4[3], 0, 0}

	// out32_Q14 <<= 2 (wrapping) on the 2-lane half.
	out32_Q14[0] = opus_int32(uint32(out32_Q14[0]) << 2)
	out32_Q14[1] = opus_int32(uint32(out32_Q14[1]) << 2)

	// out32_Q14_v4 = {out32_Q14[0], out32_Q14[1], out32_Q14[0], out32_Q14[1]}.
	out_v4 := [4]opus_int32{out32_Q14[0], out32_Q14[1], out32_Q14[0], out32_Q14[1]}

	// t_v4 = vqdmulhq_s32(out_v4, A_L_v4) — the SMULWB-equivalent on
	// the lower 14 bits of the coefficient.
	t_v4 := vqdmulhqS32Ref(out_v4, A_L_v4)

	// S_v4 = vrsraq_n_s32(S_v4, t_v4, 14) — rounding-right-shift-14
	// and accumulate into S.
	S_v4 = vrsraqNS3214Ref(S_v4, t_v4)

	// t_v4 = vqdmulhq_s32(out_v4, A_U_v4) — SMULWB-equivalent on upper
	// 18 bits.
	t_v4 = vqdmulhqS32Ref(out_v4, A_U_v4)
	S_v4 = vaddqS32BiquadRef(S_v4, t_v4)

	// t_v4 = vqdmulhq_s32(in_v4, B_v4) — SMULWB-equivalent of
	// B_Q28[{1,1,2,2}] × in{0,1,0,1}.
	t_v4 = vqdmulhqS32Ref(in_v4, B_v4)
	S_v4 = vaddqS32BiquadRef(S_v4, t_v4)

	newS = S_v4
	return
}
