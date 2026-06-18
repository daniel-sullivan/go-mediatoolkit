package nativeopus

// 1:1 port of libopus/silk/float/LPC_analysis_filter_FLP.c.
// LPC analysis filter. State is zero-initialised; the first `Order`
// output samples are written to zero. All arithmetic is float32; each
// a+b*c is separately rounded in the C oracle (-ffp-contract=off),
// so use fma_add on the Go side to defeat Go's arm64 FMADDS fusion.

// lpcPred generic kernel for orders in {6,8,10,12,16}. The C code
// inlines fixed-order kernels; we fold them into one loop here. The
// accumulation order matches the C left-to-right sum:
//   LPC_pred = s_ptr[0]*P[0] + s_ptr[-1]*P[1] + ... + s_ptr[-(O-1)]*P[O-1]
// which with separate rounds is
//   t = s_ptr[0]*P[0]; for j=1..O-1: t = t + s_ptr[-j]*P[j].

func silk_LPC_analysis_filter_FLP(
	r_LPC []silk_float,
	PredCoef []silk_float,
	s []silk_float,
	length opus_int,
	Order opus_int,
) {
	// silk_LPC_analysis_filter_FLP asserts Order in {6,8,10,12,16};
	// we honour the same assertion via switch + dispatch, but the
	// kernel body is identical — differences are unroll width only,
	// and the compiler-side rounding is the same whether unrolled or
	// not since float32 a+b*c remains separately rounded when
	// -ffp-contract=off is in force. For bit-exact parity we mirror
	// what C produces: each cross-term add is a separate round.
	switch Order {
	case 6, 8, 10, 12, 16:
		// OK
	default:
		silk_assert(false)
		return
	}

	for ix := Order; ix < length; ix++ {
		// s_ptr = &s[ix-1]
		base := ix - 1
		// t = s[base]*P[0]
		var t silk_float = mul_f32(s[base], PredCoef[0])
		for j := opus_int(1); j < Order; j++ {
			// t = t + s[base-j]*P[j]
			t = fma_add(t, s[base-j], PredCoef[j])
		}
		// r_LPC[ix] = s_ptr[1] - LPC_pred  ==  s[ix] - t
		r_LPC[ix] = s[ix] - t
	}

	// First Order output samples set to zero.
	for i := opus_int(0); i < Order; i++ {
		r_LPC[i] = 0
	}
}
