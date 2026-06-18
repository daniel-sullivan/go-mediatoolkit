package nativeopus

// Port of libopus/silk/Inlines.h — SILK's small reusable helpers for
// leading-zero counting, Q-domain square-root approximation, and
// variable-Q integer division/inversion.

// silk_CLZ64 — leading-zero count for int64. C dispatches to silk_CLZ32
// on the upper or lower word depending on which is non-zero.
func silk_CLZ64(in opus_int64) opus_int32 {
	in_upper := opus_int32(silk_RSHIFT64(in, 32))
	if in_upper == 0 {
		return 32 + silk_CLZ32(opus_int32(in))
	}
	return silk_CLZ32(in_upper)
}

// silk_CLZ_FRAC decomposes `in` into its leading-zero count and the 7
// bits immediately right of the leading 1 (Q7 fractional part).
//
// C signature: void silk_CLZ_FRAC(opus_int32 in, opus_int32 *lz,
//
//	opus_int32 *frac_Q7).
func silk_CLZ_FRAC(in opus_int32, lz, frac_Q7 *opus_int32) {
	lzeros := silk_CLZ32(in)
	*lz = lzeros
	*frac_Q7 = silk_ROR32(in, 24-opus_int(lzeros)) & 0x7f
}

// silk_SQRT_APPROX — Q-domain square-root approximation used
// throughout SILK. Accuracy: < +/-10% for output > 15, < +/-2.5% for
// output > 120.
func silk_SQRT_APPROX(x opus_int32) opus_int32 {
	if x <= 0 {
		return 0
	}
	var lz, frac_Q7 opus_int32
	silk_CLZ_FRAC(x, &lz, &frac_Q7)

	var y opus_int32
	if lz&1 != 0 {
		y = 32768
	} else {
		y = 46214 // sqrt(2) * 32768
	}
	// Get scaling right.
	y >>= silk_RSHIFT(opus_int32(lz), 1)
	// Increment using fractional part of input.
	y = silk_SMLAWB(y, y, silk_SMULBB(213, frac_Q7))
	return y
}

// silk_DIV32_varQ — approximates (a32 << Qres) / b32 as int32.
// C: Inlines.h:93–136.
func silk_DIV32_varQ(a32, b32 opus_int32, Qres opus_int) opus_int32 {
	silk_assert(b32 != 0)
	silk_assert(Qres >= 0)

	// Compute number of bits head-room and normalize inputs.
	a_headrm := opus_int(silk_CLZ32(silk_abs_int32(a32))) - 1
	a32_nrm := silk_LSHIFT(a32, a_headrm) // Q: a_headrm
	b_headrm := opus_int(silk_CLZ32(silk_abs_int32(b32))) - 1
	b32_nrm := silk_LSHIFT(b32, b_headrm) // Q: b_headrm

	// Inverse of b32 with 14 bits of precision.
	b32_inv := silk_DIV32_16(silk_int32_MAX>>2, silk_RSHIFT(b32_nrm, 16)) // Q: 29 + 16 - b_headrm

	// First approximation.
	result := silk_SMULWB(a32_nrm, b32_inv) // Q: 29 + a_headrm - b_headrm

	// Residual: subtract denom * first_approx from a32_nrm. OK to
	// overflow — final magnitude stays small.
	a32_nrm = silk_SUB32_ovflw(a32_nrm, silk_LSHIFT_ovflw(silk_SMMUL(b32_nrm, result), 3))

	// Refinement.
	result = silk_SMLAWB(result, a32_nrm, b32_inv)

	// Convert to Qres domain.
	lshift := 29 + a_headrm - b_headrm - Qres
	if lshift < 0 {
		return silk_LSHIFT_SAT32(result, -lshift)
	}
	if lshift < 32 {
		return silk_RSHIFT(result, lshift)
	}
	// Avoid undefined result on wider shifts.
	return 0
}

// silk_INVERSE32_varQ — approximates (1 << Qres) / b32 as int32.
// C: Inlines.h:139–178.
func silk_INVERSE32_varQ(b32 opus_int32, Qres opus_int) opus_int32 {
	silk_assert(b32 != 0)
	silk_assert(Qres > 0)

	b_headrm := opus_int(silk_CLZ32(silk_abs_int32(b32))) - 1
	b32_nrm := silk_LSHIFT(b32, b_headrm) // Q: b_headrm

	// Inverse with 14 bits of precision.
	b32_inv := silk_DIV32_16(silk_int32_MAX>>2, silk_RSHIFT(b32_nrm, 16)) // Q: 29 + 16 - b_headrm

	// First approximation: shift up by 16.
	result := silk_LSHIFT(b32_inv, 16) // Q: 61 - b_headrm

	// Residual: (1 << 29) - denom * first_approx, lifted to Q32.
	err_Q32 := silk_LSHIFT(opus_int32(1)<<29-silk_SMULWB(b32_nrm, b32_inv), 3)

	// Refinement.
	result = silk_SMLAWW(result, err_Q32, b32_inv)

	lshift := 61 - b_headrm - Qres
	if lshift <= 0 {
		return silk_LSHIFT_SAT32(result, -lshift)
	}
	if lshift < 32 {
		return silk_RSHIFT(result, lshift)
	}
	return 0
}
