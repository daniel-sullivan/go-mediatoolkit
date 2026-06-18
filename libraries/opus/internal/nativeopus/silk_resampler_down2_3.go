package nativeopus

// Port of libopus/silk/resampler_down2_3.c.

const silk_resampler_down2_3_ORDER_FIR = 4

// silk_resampler_down2_3 — Downsample by 2/3, low quality.
func silk_resampler_down2_3(S []opus_int32, out []opus_int16, in_ []opus_int16, inLen opus_int32) {
	buf := make([]opus_int32, RESAMPLER_MAX_BATCH_SIZE_IN+silk_resampler_down2_3_ORDER_FIR)
	copy(buf[:silk_resampler_down2_3_ORDER_FIR], S[:silk_resampler_down2_3_ORDER_FIR])

	inOff := opus_int32(0)
	outOff := 0
	var nSamplesIn opus_int32
	for {
		nSamplesIn = silk_min(inLen, RESAMPLER_MAX_BATCH_SIZE_IN)

		// Second-order AR filter (output in Q8).
		silk_resampler_private_AR2(
			S[silk_resampler_down2_3_ORDER_FIR:],
			buf[silk_resampler_down2_3_ORDER_FIR:],
			in_[inOff:],
			silk_Resampler_2_3_COEFS_LQ[:], nSamplesIn)

		bufPtr := 0
		counter := nSamplesIn
		for counter > 2 {
			res_Q6 := silk_SMULWB(buf[bufPtr+0], opus_int32(silk_Resampler_2_3_COEFS_LQ[2]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+1], opus_int32(silk_Resampler_2_3_COEFS_LQ[3]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+2], opus_int32(silk_Resampler_2_3_COEFS_LQ[5]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+3], opus_int32(silk_Resampler_2_3_COEFS_LQ[4]))
			out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q6, 6)))
			outOff++

			res_Q6 = silk_SMULWB(buf[bufPtr+1], opus_int32(silk_Resampler_2_3_COEFS_LQ[4]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+2], opus_int32(silk_Resampler_2_3_COEFS_LQ[5]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+3], opus_int32(silk_Resampler_2_3_COEFS_LQ[3]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+4], opus_int32(silk_Resampler_2_3_COEFS_LQ[2]))
			out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q6, 6)))
			outOff++

			bufPtr += 3
			counter -= 3
		}

		inOff += nSamplesIn
		inLen -= nSamplesIn
		if inLen > 0 {
			copy(buf[:silk_resampler_down2_3_ORDER_FIR], buf[nSamplesIn:nSamplesIn+silk_resampler_down2_3_ORDER_FIR])
		} else {
			break
		}
	}

	copy(S[:silk_resampler_down2_3_ORDER_FIR], buf[nSamplesIn:nSamplesIn+silk_resampler_down2_3_ORDER_FIR])
}
