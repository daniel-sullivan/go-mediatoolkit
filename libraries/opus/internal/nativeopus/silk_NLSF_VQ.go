package nativeopus

// Port of libopus/silk/NLSF_VQ.c.

// silk_NLSF_VQ — Compute quantization errors for an LPC_order-element
// input vector against a VQ codebook of K vectors.
func silk_NLSF_VQ(err_Q24 []opus_int32, in_Q15 []opus_int16,
	pCB_Q8 []opus_uint8, pWght_Q9 []opus_int16, K, LPC_order opus_int) {
	celt_assert((LPC_order & 1) == 0)

	cbBase := opus_int(0)
	wBase := opus_int(0)
	for i := opus_int(0); i < K; i++ {
		var sum_error_Q24 opus_int32
		var pred_Q24 opus_int32
		for m := LPC_order - 2; m >= 0; m -= 2 {
			// Index m+1.
			diff_Q15 := silk_SUB_LSHIFT32(opus_int32(in_Q15[m+1]), opus_int32(pCB_Q8[cbBase+m+1]), 7)
			diffw_Q24 := silk_SMULBB(diff_Q15, opus_int32(pWght_Q9[wBase+m+1]))
			sum_error_Q24 = silk_ADD32(sum_error_Q24,
				silk_abs(silk_SUB_RSHIFT32(diffw_Q24, pred_Q24, 1)))
			pred_Q24 = diffw_Q24

			// Index m.
			diff_Q15 = silk_SUB_LSHIFT32(opus_int32(in_Q15[m]), opus_int32(pCB_Q8[cbBase+m]), 7)
			diffw_Q24 = silk_SMULBB(diff_Q15, opus_int32(pWght_Q9[wBase+m]))
			sum_error_Q24 = silk_ADD32(sum_error_Q24,
				silk_abs(silk_SUB_RSHIFT32(diffw_Q24, pred_Q24, 1)))
			pred_Q24 = diffw_Q24

			silk_assert(sum_error_Q24 >= 0)
		}
		err_Q24[i] = sum_error_Q24
		cbBase += LPC_order
		wBase += LPC_order
	}
}
