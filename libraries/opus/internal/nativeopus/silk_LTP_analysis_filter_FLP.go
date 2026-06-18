package nativeopus

// 1:1 port of libopus/silk/float/LTP_analysis_filter_FLP.c.
// LTP residual by subtracting B[]-convolution of x at lag pitchL[k],
// then scaling by inverse gain. Per-subframe loops.
//
// C receives `x` as a pointer into a larger buffer (so `x - pitchL[k]`
// is well-defined). For the Go port the caller must pass the full
// backing slice plus `xOff`, the absolute starting index. xOff - max
// pitch must be non-negative.

func silk_LTP_analysis_filter_FLP(
	LTP_res []silk_float,
	xBuf []silk_float,
	xOff opus_int,
	B []silk_float, // [LTP_ORDER * MAX_NB_SUBFR]
	pitchL []opus_int, // [MAX_NB_SUBFR]
	invGains []silk_float, // [MAX_NB_SUBFR]
	subfr_length, nb_subfr, pre_length opus_int,
) {
	var Btmp [LTP_ORDER]silk_float

	resOff := opus_int(0) // LTP_res_ptr = LTP_res + resOff
	for k := opus_int(0); k < nb_subfr; k++ {
		xLagOff := xOff - pitchL[k] // x_lag_ptr = x_ptr - pitchL[k]
		invGain := invGains[k]
		for i := opus_int(0); i < LTP_ORDER; i++ {
			Btmp[i] = B[k*LTP_ORDER+i]
		}

		// LTP analysis FIR filter.
		for i := opus_int(0); i < subfr_length+pre_length; i++ {
			v := xBuf[xOff+i]
			// Subtract long-term prediction, left-to-right per C:
			//   v -= Btmp[j] * x_lag_ptr[LTP_ORDER/2 - j]  for j=0..LTP_ORDER-1
			for j := opus_int(0); j < LTP_ORDER; j++ {
				// C advances x_lag_ptr by 1 each i. So at iteration i,
				// x_lag_ptr[LTP_ORDER/2 - j] = x[xLagOff + i + LTP_ORDER/2 - j].
				v = fma_sub(v, Btmp[j], xBuf[xLagOff+i+LTP_ORDER/2-j])
			}
			LTP_res[resOff+i] = v * invGain
		}

		resOff += subfr_length + pre_length
		xOff += subfr_length
	}
}
