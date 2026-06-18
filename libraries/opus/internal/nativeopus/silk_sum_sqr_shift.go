package nativeopus

// Port of libopus/silk/sum_sqr_shift.c.

// silk_sum_sqr_shift — Compute number of bits to right-shift the sum
// of squares of a vector of int16s to make it fit in an int32. Outputs
// the shifted energy and the number of bits shifted.
func silk_sum_sqr_shift(energy *opus_int32, shift *opus_int, x []opus_int16, len_ opus_int) {
	// Do a first run with the maximum shift we could have.
	shft := opus_int(31 - silk_CLZ32(opus_int32(len_)))
	// Be conservative with rounding and start with nrg=len.
	nrg := opus_int32(len_)
	var i opus_int
	for i = 0; i < len_-1; i += 2 {
		nrg_tmp := opus_uint32(silk_SMULBB(opus_int32(x[i]), opus_int32(x[i])))
		nrg_tmp = opus_uint32(silk_SMLABB_ovflw(opus_int32(nrg_tmp),
			opus_int32(x[i+1]), opus_int32(x[i+1])))
		nrg = opus_int32(silk_ADD_RSHIFT_uint(opus_uint32(nrg), nrg_tmp, shft))
	}
	if i < len_ {
		// One sample left to process.
		nrg_tmp := opus_uint32(silk_SMULBB(opus_int32(x[i]), opus_int32(x[i])))
		nrg = opus_int32(silk_ADD_RSHIFT_uint(opus_uint32(nrg), nrg_tmp, shft))
	}
	silk_assert(nrg >= 0)
	// Make sure the result will fit in a 32-bit signed integer with two
	// bits of headroom.
	shft = opus_int(silk_max_32(0, opus_int32(shft)+3-silk_CLZ32(nrg)))
	nrg = 0
	for i = 0; i < len_-1; i += 2 {
		nrg_tmp := opus_uint32(silk_SMULBB(opus_int32(x[i]), opus_int32(x[i])))
		nrg_tmp = opus_uint32(silk_SMLABB_ovflw(opus_int32(nrg_tmp),
			opus_int32(x[i+1]), opus_int32(x[i+1])))
		nrg = opus_int32(silk_ADD_RSHIFT_uint(opus_uint32(nrg), nrg_tmp, shft))
	}
	if i < len_ {
		nrg_tmp := opus_uint32(silk_SMULBB(opus_int32(x[i]), opus_int32(x[i])))
		nrg = opus_int32(silk_ADD_RSHIFT_uint(opus_uint32(nrg), nrg_tmp, shft))
	}
	silk_assert(nrg >= 0)

	*shift = shft
	*energy = nrg
}
