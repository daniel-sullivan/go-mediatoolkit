package nativeopus

// Port of libopus/silk/LPC_analysis_filter.c.
//
// Variable-order MA prediction error filter. State is kept internally
// (zero-init); the first d output samples are set to zero. Bit-exact
// with the C !USE_CELT_FIR branch.

// silk_LPC_analysis_filter — out[i] = in[i] - sum_{j=0..d-1} B[j]*in[i-1-j],
// scaled to Q0 with rounding and saturated to int16.
func silk_LPC_analysis_filter(out, in_, B []opus_int16, len_, d opus_int32, arch int) {
	_ = arch
	celt_assert(d >= 6)
	celt_assert((d & 1) == 0)
	celt_assert(d <= len_)

	for ix := d; ix < len_; ix++ {
		in_ptr_base := ix - 1

		out32_Q12 := silk_SMULBB(opus_int32(in_[in_ptr_base+0]), opus_int32(B[0]))
		// Allowing wrap-around so that two wraps can cancel each other.
		out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-1]), opus_int32(B[1]))
		out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-2]), opus_int32(B[2]))
		out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-3]), opus_int32(B[3]))
		out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-4]), opus_int32(B[4]))
		out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-5]), opus_int32(B[5]))
		for j := opus_int32(6); j < d; j += 2 {
			out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-j]), opus_int32(B[j]))
			out32_Q12 = silk_SMLABB_ovflw(out32_Q12, opus_int32(in_[in_ptr_base-j-1]), opus_int32(B[j+1]))
		}

		// Subtract prediction.
		out32_Q12 = silk_SUB32_ovflw(silk_LSHIFT(opus_int32(in_[in_ptr_base+1]), 12), out32_Q12)

		// Scale to Q0 and saturate.
		out[ix] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(out32_Q12, 12)))
	}

	// Set first d output samples to zero.
	for i := opus_int32(0); i < d; i++ {
		out[i] = 0
	}
}
