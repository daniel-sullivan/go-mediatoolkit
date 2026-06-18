package nativeopus

// Port of libopus/silk/resampler_private_IIR_FIR.c.

// silk_resampler_private_IIR_FIR_INTERPOL — inner interpolation loop.
// Returns the new out slice offset (number of samples consumed).
func silk_resampler_private_IIR_FIR_INTERPOL(out []opus_int16, outOff int,
	buf []opus_int16, max_index_Q16, index_increment_Q16 opus_int32) int {
	for index_Q16 := opus_int32(0); index_Q16 < max_index_Q16; index_Q16 += index_increment_Q16 {
		table_index := silk_SMULWB(index_Q16&0xFFFF, 12)
		bufPtr := int(index_Q16 >> 16)

		res_Q15 := silk_SMULBB(opus_int32(buf[bufPtr+0]), opus_int32(silk_resampler_frac_FIR_12[table_index][0]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+1]), opus_int32(silk_resampler_frac_FIR_12[table_index][1]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+2]), opus_int32(silk_resampler_frac_FIR_12[table_index][2]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+3]), opus_int32(silk_resampler_frac_FIR_12[table_index][3]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+4]), opus_int32(silk_resampler_frac_FIR_12[11-table_index][3]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+5]), opus_int32(silk_resampler_frac_FIR_12[11-table_index][2]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+6]), opus_int32(silk_resampler_frac_FIR_12[11-table_index][1]))
		res_Q15 = silk_SMLABB(res_Q15, opus_int32(buf[bufPtr+7]), opus_int32(silk_resampler_frac_FIR_12[11-table_index][0]))
		out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q15, 15)))
		outOff++
	}
	return outOff
}

// silk_resampler_private_IIR_FIR — Upsample via allpass 2x + FIR interpolation.
func silk_resampler_private_IIR_FIR(S *silk_resampler_state_struct,
	out []opus_int16, in_ []opus_int16, inLen opus_int32) {

	buf := S.scratch_buf[:2*S.batchSize+RESAMPLER_ORDER_FIR_12]
	copy(buf[:RESAMPLER_ORDER_FIR_12], S.sFIR_i16[:RESAMPLER_ORDER_FIR_12])

	inOff := 0
	outOff := 0
	index_increment_Q16 := S.invRatio_Q16
	var nSamplesIn opus_int32
	for {
		nSamplesIn = silk_min(inLen, opus_int32(S.batchSize))

		silk_resampler_private_up2_HQ(S.sIIR[:], buf[RESAMPLER_ORDER_FIR_12:], in_[inOff:], nSamplesIn)

		max_index_Q16 := silk_LSHIFT32(nSamplesIn, 16+1)
		outOff = silk_resampler_private_IIR_FIR_INTERPOL(out, outOff, buf, max_index_Q16, index_increment_Q16)
		inOff += int(nSamplesIn)
		inLen -= nSamplesIn

		if inLen > 0 {
			copy(buf[:RESAMPLER_ORDER_FIR_12], buf[nSamplesIn<<1:(nSamplesIn<<1)+RESAMPLER_ORDER_FIR_12])
		} else {
			break
		}
	}

	copy(S.sFIR_i16[:RESAMPLER_ORDER_FIR_12], buf[nSamplesIn<<1:(nSamplesIn<<1)+RESAMPLER_ORDER_FIR_12])
}
