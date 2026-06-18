package nativeopus

// Port of libopus/silk/decode_pulses.c.

// silk_decode_pulses — Decode quantization indices of excitation.
func silk_decode_pulses(psRangeDec *ec_dec, pulses []opus_int16,
	signalType, quantOffsetType, frame_length opus_int) {
	var sum_pulses [MAX_NB_SHELL_BLOCKS]opus_int
	var nLshifts [MAX_NB_SHELL_BLOCKS]opus_int

	RateLevelIndex := ec_dec_icdf(psRangeDec, asByteSlice(silk_rate_levels_iCDF[signalType>>1][:]), 8)

	silk_assert(1<<LOG2_SHELL_CODEC_FRAME_LENGTH == SHELL_CODEC_FRAME_LENGTH)
	iter := opus_int(silk_RSHIFT(opus_int32(frame_length), LOG2_SHELL_CODEC_FRAME_LENGTH))
	if iter*SHELL_CODEC_FRAME_LENGTH < frame_length {
		celt_assert(frame_length == 12*10)
		iter++
	}

	// Sum-Weighted-Pulses decoding.
	for i := opus_int(0); i < iter; i++ {
		nLshifts[i] = 0
		sum_pulses[i] = ec_dec_icdf(psRangeDec, asByteSlice(silk_pulses_per_block_iCDF[RateLevelIndex][:]), 8)

		for sum_pulses[i] == SILK_MAX_PULSES+1 {
			nLshifts[i]++
			off := 0
			if nLshifts[i] == 10 {
				off = 1
			}
			sum_pulses[i] = ec_dec_icdf(psRangeDec,
				asByteSlice(silk_pulses_per_block_iCDF[N_RATE_LEVELS-1][off:]), 8)
		}
	}

	// Shell decoding.
	for i := opus_int(0); i < iter; i++ {
		base := int(silk_SMULBB(opus_int32(i), SHELL_CODEC_FRAME_LENGTH))
		if sum_pulses[i] > 0 {
			silk_shell_decoder(pulses[base:], psRangeDec, sum_pulses[i])
		} else {
			for k := 0; k < SHELL_CODEC_FRAME_LENGTH; k++ {
				pulses[base+k] = 0
			}
		}
	}

	// LSB decoding.
	for i := opus_int(0); i < iter; i++ {
		if nLshifts[i] > 0 {
			nLS := nLshifts[i]
			base := int(silk_SMULBB(opus_int32(i), SHELL_CODEC_FRAME_LENGTH))
			for k := 0; k < SHELL_CODEC_FRAME_LENGTH; k++ {
				abs_q := opus_int(pulses[base+k])
				for j := opus_int(0); j < nLS; j++ {
					abs_q = opus_int(silk_LSHIFT(opus_int32(abs_q), 1))
					abs_q += ec_dec_icdf(psRangeDec, asByteSlice(silk_lsb_iCDF[:]), 8)
				}
				pulses[base+k] = opus_int16(abs_q)
			}
			sum_pulses[i] |= nLS << 5
		}
	}

	silk_decode_signs(psRangeDec, pulses, frame_length, signalType, quantOffsetType, sum_pulses[:])
}
