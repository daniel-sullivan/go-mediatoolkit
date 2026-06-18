package nativeopus

// Port of libopus/silk/encode_pulses.c.
//
// Encode quantization indices of the excitation: rate-level selection,
// shell-coding of the absolute magnitudes, LSB handling for scaled-down
// frames and sign encoding.

// combine_and_check — C static: encode_pulses.c:39-57. Combine pairs
// and flag if any sum exceeds max_pulses.
func combine_and_check_encode(pulses_comb, pulses_in []opus_int, max_pulses opus_int, length opus_int) opus_int {
	for k := opus_int(0); k < length; k++ {
		sum := pulses_in[2*k] + pulses_in[2*k+1]
		if sum > max_pulses {
			return 1
		}
		pulses_comb[k] = sum
	}
	return 0
}

// silk_encode_pulses — C: encode_pulses.c:60-206.
func silk_encode_pulses(
	psRangeEnc *ec_enc,
	signalType opus_int,
	quantOffsetType opus_int,
	pulses []opus_int8,
	frame_length opus_int,
) {
	var pulses_comb [8]opus_int

	// Calculate number of shell blocks.
	silk_assert(1<<LOG2_SHELL_CODEC_FRAME_LENGTH == SHELL_CODEC_FRAME_LENGTH)
	iter := silk_RSHIFT(opus_int32(frame_length), LOG2_SHELL_CODEC_FRAME_LENGTH)
	if iter*SHELL_CODEC_FRAME_LENGTH < opus_int32(frame_length) {
		celt_assert(frame_length == 12*10)
		iter++
		// Zero-pad the tail.
		for i := frame_length; i < frame_length+SHELL_CODEC_FRAME_LENGTH; i++ {
			pulses[i] = 0
		}
	}

	// Take absolute values.
	abs_pulses := make([]opus_int, iter*SHELL_CODEC_FRAME_LENGTH)
	silk_assert((SHELL_CODEC_FRAME_LENGTH & 3) == 0)
	for i := opus_int32(0); i < iter*SHELL_CODEC_FRAME_LENGTH; i += 4 {
		abs_pulses[i+0] = opus_int(silk_abs(pulses[i+0]))
		abs_pulses[i+1] = opus_int(silk_abs(pulses[i+1]))
		abs_pulses[i+2] = opus_int(silk_abs(pulses[i+2]))
		abs_pulses[i+3] = opus_int(silk_abs(pulses[i+3]))
	}

	// Sum pulses per shell frame, optionally right-shifting until within range.
	sum_pulses := make([]opus_int, iter)
	nRshifts := make([]opus_int, iter)
	abs_off := opus_int32(0)
	for i := opus_int32(0); i < iter; i++ {
		nRshifts[i] = 0
		for {
			// 1+1 -> 2, 2+2 -> 4, 4+4 -> 8, 8+8 -> 16
			scale_down := combine_and_check_encode(pulses_comb[:], abs_pulses[abs_off:], opus_int(silk_max_pulses_table[0]), 8)
			scale_down += combine_and_check_encode(pulses_comb[:], pulses_comb[:], opus_int(silk_max_pulses_table[1]), 4)
			scale_down += combine_and_check_encode(pulses_comb[:], pulses_comb[:], opus_int(silk_max_pulses_table[2]), 2)
			scale_down += combine_and_check_encode(sum_pulses[i:], pulses_comb[:], opus_int(silk_max_pulses_table[3]), 1)
			if scale_down != 0 {
				nRshifts[i]++
				for k := opus_int32(0); k < SHELL_CODEC_FRAME_LENGTH; k++ {
					abs_pulses[abs_off+k] = opus_int(silk_RSHIFT(opus_int32(abs_pulses[abs_off+k]), 1))
				}
			} else {
				break
			}
		}
		abs_off += SHELL_CODEC_FRAME_LENGTH
	}

	// Rate level selection.
	minSumBits_Q5 := opus_int32(silk_int32_MAX)
	RateLevelIndex := opus_int(0)
	for k := opus_int(0); k < N_RATE_LEVELS-1; k++ {
		nBits_ptr := silk_pulses_per_block_BITS_Q5[k]
		sumBits_Q5 := opus_int32(silk_rate_levels_BITS_Q5[signalType>>1][k])
		for i := opus_int32(0); i < iter; i++ {
			if nRshifts[i] > 0 {
				sumBits_Q5 += opus_int32(nBits_ptr[SILK_MAX_PULSES+1])
			} else {
				sumBits_Q5 += opus_int32(nBits_ptr[sum_pulses[i]])
			}
		}
		if sumBits_Q5 < minSumBits_Q5 {
			minSumBits_Q5 = sumBits_Q5
			RateLevelIndex = k
		}
	}
	ec_enc_icdf(psRangeEnc, int(RateLevelIndex), asByteSlice(silk_rate_levels_iCDF[signalType>>1][:]), 8)

	// Sum-weighted-pulses encoding.
	cdf_ptr := silk_pulses_per_block_iCDF[RateLevelIndex][:]
	last_cdf := silk_pulses_per_block_iCDF[N_RATE_LEVELS-1][:]
	for i := opus_int32(0); i < iter; i++ {
		if nRshifts[i] == 0 {
			ec_enc_icdf(psRangeEnc, int(sum_pulses[i]), asByteSlice(cdf_ptr), 8)
		} else {
			ec_enc_icdf(psRangeEnc, SILK_MAX_PULSES+1, asByteSlice(cdf_ptr), 8)
			for k := opus_int(0); k < nRshifts[i]-1; k++ {
				ec_enc_icdf(psRangeEnc, SILK_MAX_PULSES+1, asByteSlice(last_cdf), 8)
			}
			ec_enc_icdf(psRangeEnc, int(sum_pulses[i]), asByteSlice(last_cdf), 8)
		}
	}

	// Shell encoding.
	for i := opus_int32(0); i < iter; i++ {
		if sum_pulses[i] > 0 {
			silk_shell_encoder(psRangeEnc, abs_pulses[i*SHELL_CODEC_FRAME_LENGTH:])
		}
	}

	// LSB encoding.
	for i := opus_int32(0); i < iter; i++ {
		if nRshifts[i] > 0 {
			pulses_ptr := pulses[i*SHELL_CODEC_FRAME_LENGTH:]
			nLS := nRshifts[i] - 1
			for k := opus_int32(0); k < SHELL_CODEC_FRAME_LENGTH; k++ {
				abs_q := opus_int32(opus_int8(silk_abs(pulses_ptr[k])))
				for j := nLS; j > 0; j-- {
					bit := int(silk_RSHIFT(abs_q, j) & 1)
					ec_enc_icdf(psRangeEnc, bit, asByteSlice(silk_lsb_iCDF[:]), 8)
				}
				bit := int(abs_q & 1)
				ec_enc_icdf(psRangeEnc, bit, asByteSlice(silk_lsb_iCDF[:]), 8)
			}
		}
	}

	// Sign encoding.
	silk_encode_signs(psRangeEnc, pulses, frame_length, signalType, quantOffsetType, sum_pulses)
}
