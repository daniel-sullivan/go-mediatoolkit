package nativeopus

// 1:1 port of libopus/silk/float/warped_autocorrelation_FLP.c.
// Autocorrelations for a warped frequency axis. All intermediates are
// double. Each a+b*c / a-b*c is separately rounded in C with
// -ffp-contract=off — use fma_add64 / fma_sub64 on the Go side.

func silk_warped_autocorrelation_FLP_c(
	corr []silk_float,
	input []silk_float,
	warping silk_float,
	length, order opus_int,
) {
	var state [MAX_SHAPE_LPC_ORDER + 1]float64
	var C [MAX_SHAPE_LPC_ORDER + 1]float64
	w := float64(warping)

	for n := opus_int(0); n < length; n++ {
		tmp1 := float64(input[n])
		// Allpass sections — two at a time.
		for i := opus_int(0); i < order; i += 2 {
			// C: tmp2 = state[i] + warping*state[i+1] - warping*tmp1;
			//    left-to-right.
			tmp2 := fma_add64(state[i], w, state[i+1])
			tmp2 = fma_sub64(tmp2, w, tmp1)
			state[i] = tmp1
			// C: C[i] += state[0] * tmp1;
			C[i] = fma_add64(C[i], state[0], tmp1)
			// C: tmp1 = state[i+1] + warping*state[i+2] - warping*tmp2;
			tmp1New := fma_add64(state[i+1], w, state[i+2])
			tmp1New = fma_sub64(tmp1New, w, tmp2)
			state[i+1] = tmp2
			// C: C[i+1] += state[0] * tmp2;
			C[i+1] = fma_add64(C[i+1], state[0], tmp2)
			tmp1 = tmp1New
		}
		state[order] = tmp1
		// C: C[order] += state[0] * tmp1;
		C[order] = fma_add64(C[order], state[0], tmp1)
	}

	for i := opus_int(0); i < order+1; i++ {
		corr[i] = silk_float(C[i])
	}
}
