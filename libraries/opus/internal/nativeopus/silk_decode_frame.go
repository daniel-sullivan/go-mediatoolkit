package nativeopus

// Port of libopus/silk/decode_frame.c.

// silk_decode_frame — decode one SILK frame.
// C: decode_frame.c:43-169.
func silk_decode_frame(psDec *silk_decoder_state, psRangeDec *ec_dec,
	pOut []opus_int16, pN *opus_int32,
	lostFlag opus_int, condCoding opus_int, arch int) opus_int {

	var psDecCtrl silk_decoder_control
	var mv_len opus_int
	ret := opus_int(0)

	L := psDec.frame_length
	psDecCtrl.LTP_scale_Q14 = 0

	celt_assert(L > 0 && L <= MAX_FRAME_LENGTH)

	if lostFlag == FLAG_DECODE_NORMAL ||
		(lostFlag == FLAG_DECODE_LBRR && psDec.LBRR_flags[psDec.nFramesDecoded] == 1) {

		pulsesLen := (L + SHELL_CODEC_FRAME_LENGTH - 1) & ^(SHELL_CODEC_FRAME_LENGTH - 1)
		pulses := psDec.scratch_pulses[:pulsesLen]

		// Decode side info indices.
		silk_decode_indices(psDec, psRangeDec, psDec.nFramesDecoded, lostFlag, condCoding)

		// Decode excitation pulses.
		silk_decode_pulses(psRangeDec, pulses,
			opus_int(psDec.indices.signalType),
			opus_int(psDec.indices.quantOffsetType),
			psDec.frame_length)

		// Decode params.
		silk_decode_parameters(psDec, &psDecCtrl, condCoding)

		// Inverse NSQ.
		silk_decode_core(psDec, &psDecCtrl, pOut, pulses, arch)

		// Update output buffer.
		celt_assert(psDec.ltp_mem_length >= psDec.frame_length)
		mv_len = psDec.ltp_mem_length - psDec.frame_length
		copy(psDec.outBuf[:mv_len], psDec.outBuf[psDec.frame_length:psDec.frame_length+mv_len])
		copy(psDec.outBuf[mv_len:mv_len+psDec.frame_length], pOut[:psDec.frame_length])

		// Update PLC state.
		silk_PLC(psDec, &psDecCtrl, pOut, 0, arch)

		psDec.lossCnt = 0
		psDec.prevSignalType = opus_int(psDec.indices.signalType)
		celt_assert(psDec.prevSignalType >= 0 && psDec.prevSignalType <= 2)

		psDec.first_frame_after_reset = 0
	} else {
		// Packet loss: extrapolate.
		silk_PLC(psDec, &psDecCtrl, pOut, 1, arch)

		// Update output buffer.
		celt_assert(psDec.ltp_mem_length >= psDec.frame_length)
		mv_len = psDec.ltp_mem_length - psDec.frame_length
		copy(psDec.outBuf[:mv_len], psDec.outBuf[psDec.frame_length:psDec.frame_length+mv_len])
		copy(psDec.outBuf[mv_len:mv_len+psDec.frame_length], pOut[:psDec.frame_length])
	}

	// Comfort noise generation / estimation.
	silk_CNG(psDec, &psDecCtrl, pOut, L)

	// Smooth connection of extrapolated and good frames.
	silk_PLC_glue_frames(psDec, pOut, L)

	psDec.lagPrev = psDecCtrl.pitchL[psDec.nb_subfr-1]

	*pN = opus_int32(L)
	return ret
}
