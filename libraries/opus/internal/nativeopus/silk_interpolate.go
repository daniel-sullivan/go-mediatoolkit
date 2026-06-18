package nativeopus

// Port of libopus/silk/interpolate.c.

// silk_interpolate — xi[i] = x0[i] + ((x1[i] - x0[i]) * ifact_Q2) >> 2.
// ifact_Q2 is in [0, 4]. Operates on the first d samples.
func silk_interpolate(xi, x0, x1 []opus_int16, ifact_Q2, d opus_int) {
	celt_assert(ifact_Q2 >= 0)
	celt_assert(ifact_Q2 <= 4)
	for i := opus_int(0); i < d; i++ {
		xi[i] = opus_int16(silk_ADD_RSHIFT(
			opus_int32(x0[i]),
			silk_SMULBB(opus_int32(x1[i])-opus_int32(x0[i]), opus_int32(ifact_Q2)),
			2))
	}
}
