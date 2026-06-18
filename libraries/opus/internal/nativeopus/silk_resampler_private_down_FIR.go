package nativeopus

// Port of libopus/silk/resampler_private_down_FIR.c.

// silk_resampler_private_down_FIR_INTERPOL — inner interpolation loop.
// Dispatches on FIR_Order. Returns the new output index.
func silk_resampler_private_down_FIR_INTERPOL(out []opus_int16, outOff int,
	buf []opus_int32, FIR_Coefs []opus_int16, FIR_Order, FIR_Fracs opus_int,
	max_index_Q16, index_increment_Q16 opus_int32) int {

	switch FIR_Order {
	case RESAMPLER_DOWN_ORDER_FIR0:
		for index_Q16 := opus_int32(0); index_Q16 < max_index_Q16; index_Q16 += index_increment_Q16 {
			bufPtr := int(silk_RSHIFT(index_Q16, 16))
			interpol_ind := silk_SMULWB(index_Q16&0xFFFF, opus_int32(FIR_Fracs))
			ip := RESAMPLER_DOWN_ORDER_FIR0 / 2 * int(interpol_ind)
			res_Q6 := silk_SMULWB(buf[bufPtr+0], opus_int32(FIR_Coefs[ip+0]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+1], opus_int32(FIR_Coefs[ip+1]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+2], opus_int32(FIR_Coefs[ip+2]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+3], opus_int32(FIR_Coefs[ip+3]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+4], opus_int32(FIR_Coefs[ip+4]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+5], opus_int32(FIR_Coefs[ip+5]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+6], opus_int32(FIR_Coefs[ip+6]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+7], opus_int32(FIR_Coefs[ip+7]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+8], opus_int32(FIR_Coefs[ip+8]))
			ip2 := RESAMPLER_DOWN_ORDER_FIR0 / 2 * int(FIR_Fracs-1-opus_int(interpol_ind))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+17], opus_int32(FIR_Coefs[ip2+0]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+16], opus_int32(FIR_Coefs[ip2+1]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+15], opus_int32(FIR_Coefs[ip2+2]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+14], opus_int32(FIR_Coefs[ip2+3]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+13], opus_int32(FIR_Coefs[ip2+4]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+12], opus_int32(FIR_Coefs[ip2+5]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+11], opus_int32(FIR_Coefs[ip2+6]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+10], opus_int32(FIR_Coefs[ip2+7]))
			res_Q6 = silk_SMLAWB(res_Q6, buf[bufPtr+9], opus_int32(FIR_Coefs[ip2+8]))
			out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q6, 6)))
			outOff++
		}
	case RESAMPLER_DOWN_ORDER_FIR1:
		for index_Q16 := opus_int32(0); index_Q16 < max_index_Q16; index_Q16 += index_increment_Q16 {
			bufPtr := int(silk_RSHIFT(index_Q16, 16))
			res_Q6 := silk_SMULWB(silk_ADD32(buf[bufPtr+0], buf[bufPtr+23]), opus_int32(FIR_Coefs[0]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+1], buf[bufPtr+22]), opus_int32(FIR_Coefs[1]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+2], buf[bufPtr+21]), opus_int32(FIR_Coefs[2]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+3], buf[bufPtr+20]), opus_int32(FIR_Coefs[3]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+4], buf[bufPtr+19]), opus_int32(FIR_Coefs[4]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+5], buf[bufPtr+18]), opus_int32(FIR_Coefs[5]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+6], buf[bufPtr+17]), opus_int32(FIR_Coefs[6]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+7], buf[bufPtr+16]), opus_int32(FIR_Coefs[7]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+8], buf[bufPtr+15]), opus_int32(FIR_Coefs[8]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+9], buf[bufPtr+14]), opus_int32(FIR_Coefs[9]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+10], buf[bufPtr+13]), opus_int32(FIR_Coefs[10]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+11], buf[bufPtr+12]), opus_int32(FIR_Coefs[11]))
			out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q6, 6)))
			outOff++
		}
	case RESAMPLER_DOWN_ORDER_FIR2:
		for index_Q16 := opus_int32(0); index_Q16 < max_index_Q16; index_Q16 += index_increment_Q16 {
			bufPtr := int(silk_RSHIFT(index_Q16, 16))
			res_Q6 := silk_SMULWB(silk_ADD32(buf[bufPtr+0], buf[bufPtr+35]), opus_int32(FIR_Coefs[0]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+1], buf[bufPtr+34]), opus_int32(FIR_Coefs[1]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+2], buf[bufPtr+33]), opus_int32(FIR_Coefs[2]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+3], buf[bufPtr+32]), opus_int32(FIR_Coefs[3]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+4], buf[bufPtr+31]), opus_int32(FIR_Coefs[4]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+5], buf[bufPtr+30]), opus_int32(FIR_Coefs[5]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+6], buf[bufPtr+29]), opus_int32(FIR_Coefs[6]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+7], buf[bufPtr+28]), opus_int32(FIR_Coefs[7]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+8], buf[bufPtr+27]), opus_int32(FIR_Coefs[8]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+9], buf[bufPtr+26]), opus_int32(FIR_Coefs[9]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+10], buf[bufPtr+25]), opus_int32(FIR_Coefs[10]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+11], buf[bufPtr+24]), opus_int32(FIR_Coefs[11]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+12], buf[bufPtr+23]), opus_int32(FIR_Coefs[12]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+13], buf[bufPtr+22]), opus_int32(FIR_Coefs[13]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+14], buf[bufPtr+21]), opus_int32(FIR_Coefs[14]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+15], buf[bufPtr+20]), opus_int32(FIR_Coefs[15]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+16], buf[bufPtr+19]), opus_int32(FIR_Coefs[16]))
			res_Q6 = silk_SMLAWB(res_Q6, silk_ADD32(buf[bufPtr+17], buf[bufPtr+18]), opus_int32(FIR_Coefs[17]))
			out[outOff] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(res_Q6, 6)))
			outOff++
		}
	default:
		celt_assert(false)
	}
	return outOff
}

// silk_resampler_private_down_FIR — AR2 + FIR downsampler.
func silk_resampler_private_down_FIR(S *silk_resampler_state_struct,
	out []opus_int16, in_ []opus_int16, inLen opus_int32) {

	buf := make([]opus_int32, S.batchSize+S.FIR_Order)
	copy(buf[:S.FIR_Order], S.sFIR_i32[:S.FIR_Order])

	FIR_Coefs := S.Coefs[2:]

	inOff := 0
	outOff := 0
	index_increment_Q16 := S.invRatio_Q16
	var nSamplesIn opus_int32
	for {
		nSamplesIn = silk_min(inLen, opus_int32(S.batchSize))
		// Second-order AR filter.
		silk_resampler_private_AR2(S.sIIR[:], buf[S.FIR_Order:], in_[inOff:], S.Coefs, nSamplesIn)

		max_index_Q16 := silk_LSHIFT32(nSamplesIn, 16)
		outOff = silk_resampler_private_down_FIR_INTERPOL(out, outOff, buf, FIR_Coefs,
			S.FIR_Order, S.FIR_Fracs, max_index_Q16, index_increment_Q16)

		inOff += int(nSamplesIn)
		inLen -= nSamplesIn
		if inLen > 1 {
			copy(buf[:S.FIR_Order], buf[nSamplesIn:opus_int32(nSamplesIn)+opus_int32(S.FIR_Order)])
		} else {
			break
		}
	}
	copy(S.sFIR_i32[:S.FIR_Order], buf[nSamplesIn:opus_int32(nSamplesIn)+opus_int32(S.FIR_Order)])
}
