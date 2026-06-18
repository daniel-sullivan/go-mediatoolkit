package nativeopus

// Port of libopus/silk/code_signs.c.

// silk_enc_map(a) = (a >> 15) + 1, branch-free sign bit flattener.
func silk_enc_map(a opus_int8) opus_int {
	return opus_int(silk_RSHIFT(opus_int32(a), 15)) + 1
}

// silk_dec_map(a) = (a << 1) - 1.
func silk_dec_map(a opus_int) opus_int {
	return opus_int(silk_LSHIFT(opus_int32(a), 1)) - 1
}

// silk_encode_signs — Encode signs of excitation.
func silk_encode_signs(psRangeEnc *ec_enc, pulses []opus_int8,
	length, signalType, quantOffsetType opus_int, sum_pulses []opus_int) {
	var icdf [2]opus_uint8
	icdf[1] = 0
	i := silk_SMULBB(7, silk_ADD_LSHIFT(opus_int32(quantOffsetType), opus_int32(signalType), 1))
	icdf_base := opus_int(i)
	length = opus_int(silk_RSHIFT(opus_int32(length+SHELL_CODEC_FRAME_LENGTH/2), LOG2_SHELL_CODEC_FRAME_LENGTH))
	qOff := 0
	for i := opus_int(0); i < length; i++ {
		p := sum_pulses[i]
		if p > 0 {
			m := p & 0x1F
			if m > 6 {
				m = 6
			}
			icdf[0] = silk_sign_iCDF[icdf_base+m]
			for j := 0; j < SHELL_CODEC_FRAME_LENGTH; j++ {
				if pulses[qOff+j] != 0 {
					ec_enc_icdf(psRangeEnc, silk_enc_map(pulses[qOff+j]), asByteSlice(icdf[:]), 8)
				}
			}
		}
		qOff += SHELL_CODEC_FRAME_LENGTH
	}
}

// silk_decode_signs — Decode and apply signs to excitation.
func silk_decode_signs(psRangeDec *ec_dec, pulses []opus_int16,
	length, signalType, quantOffsetType opus_int, sum_pulses []opus_int) {
	var icdf [2]opus_uint8
	icdf[1] = 0
	i := silk_SMULBB(7, silk_ADD_LSHIFT(opus_int32(quantOffsetType), opus_int32(signalType), 1))
	icdf_base := opus_int(i)
	length = opus_int(silk_RSHIFT(opus_int32(length+SHELL_CODEC_FRAME_LENGTH/2), LOG2_SHELL_CODEC_FRAME_LENGTH))
	qOff := 0
	for i := opus_int(0); i < length; i++ {
		p := sum_pulses[i]
		if p > 0 {
			m := p & 0x1F
			if m > 6 {
				m = 6
			}
			icdf[0] = silk_sign_iCDF[icdf_base+m]
			for j := 0; j < SHELL_CODEC_FRAME_LENGTH; j++ {
				if pulses[qOff+j] > 0 {
					pulses[qOff+j] *= opus_int16(silk_dec_map(ec_dec_icdf(psRangeDec, asByteSlice(icdf[:]), 8)))
				}
			}
		}
		qOff += SHELL_CODEC_FRAME_LENGTH
	}
}
