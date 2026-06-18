package nativeopus

// Port of libopus/silk/sigm_Q15.c — approximate sigmoid function.

// fprintf(1, '%d, ', round(1024 * ([1 ./ (1 + exp(-(1:5))), 1] - 1 ./ (1 + exp(-(0:5))))));
var sigm_LUT_slope_Q10 = [6]opus_int32{237, 153, 73, 30, 12, 7}

// fprintf(1, '%d, ', round(32767 * 1 ./ (1 + exp(-(0:5)))));
var sigm_LUT_pos_Q15 = [6]opus_int32{16384, 23955, 28861, 31213, 32178, 32548}

// fprintf(1, '%d, ', round(32767 * 1 ./ (1 + exp((0:5)))));
var sigm_LUT_neg_Q15 = [6]opus_int32{16384, 8812, 3906, 1554, 589, 219}

// silk_sigm_Q15 — Q15 sigmoid approximation. Input is Q5.
func silk_sigm_Q15(in_Q5 opus_int) opus_int {
	if in_Q5 < 0 {
		// Negative input.
		in_Q5 = -in_Q5
		if in_Q5 >= 6*32 {
			return 0 // Clip.
		}
		ind := in_Q5 >> 5
		return opus_int(sigm_LUT_neg_Q15[ind] -
			silk_SMULBB(sigm_LUT_slope_Q10[ind], opus_int32(in_Q5&0x1F)))
	}
	// Positive input.
	if in_Q5 >= 6*32 {
		return 32767 // Clip.
	}
	ind := in_Q5 >> 5
	return opus_int(sigm_LUT_pos_Q15[ind] +
		silk_SMULBB(sigm_LUT_slope_Q10[ind], opus_int32(in_Q5&0x1F)))
}
