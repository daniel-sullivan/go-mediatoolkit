package nativeopus

// Port of libopus/silk/dec_API.c. Omits ENABLE_OSCE/DEEP_PLC paths
// (disabled in our build) and the static silk_get_TOC (the C source
// compiles it out under `#if 0`).

// silk_decoder — SILK decoder super-struct (one per stream).
// C: dec_API.c:47-56.
type silk_decoder struct {
	channel_state           [DECODER_NUM_CHANNELS]silk_decoder_state
	sStereo                 stereo_dec_state
	nChannelsAPI            opus_int
	nChannelsInternal       opus_int
	prev_decode_only_middle opus_int

	// Per-call scratch for silk_Decode. samplesOut1 holds both channels'
	// decoded output before MS→LR / resampling; samplesOut2 is a per-
	// channel resample buffer sized to the max API-rate output of one
	// SILK frame. Both are overwritten each call.
	scratch_samplesOut1 [2 * (MAX_FRAME_LENGTH + 2)]opus_int16
	scratch_samplesOut2 [MAX_FRAME_LENGTH_MS * MAX_API_FS_KHZ]opus_int16
}

// silk_Get_Decoder_Size — return size in bytes. Our build uses native
// Go structs so we return the size-of equivalent (via unsafe.Sizeof is
// not required at call-sites that allocate Go `silk_decoder` directly —
// this function is kept for API parity with the C source and returns 1
// as a placeholder; callers that allocate via this size pass it through
// to `malloc` / `OPUS_ALLOC_SIZED`, which the Go port short-circuits).
// C: dec_API.c:80-89.
func silk_Get_Decoder_Size(decSizeBytes *opus_int) opus_int {
	*decSizeBytes = 1
	return SILK_NO_ERROR
}

// silk_ResetDecoder — reset decoder state (per-channel + stereo).
// C: dec_API.c:92-107.
func silk_ResetDecoder(psDec *silk_decoder) opus_int {
	ret := opus_int(SILK_NO_ERROR)
	for n := 0; n < DECODER_NUM_CHANNELS; n++ {
		ret = silk_reset_decoder(&psDec.channel_state[n])
	}
	psDec.sStereo = stereo_dec_state{}
	psDec.prev_decode_only_middle = 0
	return ret
}

// silk_InitDecoder — initialize decoder state (per-channel + stereo).
// C: dec_API.c:110-132.
func silk_InitDecoder(psDec *silk_decoder) opus_int {
	ret := opus_int(SILK_NO_ERROR)
	for n := 0; n < DECODER_NUM_CHANNELS; n++ {
		ret = silk_init_decoder(&psDec.channel_state[n])
	}
	psDec.sStereo = stereo_dec_state{}
	psDec.prev_decode_only_middle = 0
	return ret
}

// silk_Decode — decode one SILK frame from an opus bitstream.
// C: dec_API.c:135-475.
func silk_Decode(psDec *silk_decoder, decControl *silk_DecControlStruct,
	lostFlag opus_int, newPacketFlag opus_int,
	psRangeDec *ec_dec, samplesOut []opus_res, nSamplesOut *opus_int32,
	arch int) opus_int {

	var (
		decode_only_middle = opus_int(0)
		ret                = opus_int(SILK_NO_ERROR)
		nSamplesOutDec     opus_int32
		LBRR_symbol        opus_int32
	)
	MS_pred_Q13 := [2]opus_int32{0, 0}
	channel_state := psDec.channel_state[:]
	var has_side, stereo_to_mono opus_int

	celt_assert(decControl.nChannelsInternal == 1 || decControl.nChannelsInternal == 2)

	// Test if first frame in payload.
	if newPacketFlag != 0 {
		for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
			channel_state[n].nFramesDecoded = 0
		}
	}

	// Mono -> Stereo transition: init second channel.
	if decControl.nChannelsInternal > opus_int32(psDec.nChannelsInternal) {
		ret += silk_init_decoder(&channel_state[1])
	}

	if decControl.nChannelsInternal == 1 && psDec.nChannelsInternal == 2 &&
		decControl.internalSampleRate == 1000*opus_int32(channel_state[0].fs_kHz) {
		stereo_to_mono = 1
	} else {
		stereo_to_mono = 0
	}

	if channel_state[0].nFramesDecoded == 0 {
		for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
			var fs_kHz_dec opus_int
			switch decControl.payloadSize_ms {
			case 0:
				channel_state[n].nFramesPerPacket = 1
				channel_state[n].nb_subfr = 2
			case 10:
				channel_state[n].nFramesPerPacket = 1
				channel_state[n].nb_subfr = 2
			case 20:
				channel_state[n].nFramesPerPacket = 1
				channel_state[n].nb_subfr = 4
			case 40:
				channel_state[n].nFramesPerPacket = 2
				channel_state[n].nb_subfr = 4
			case 60:
				channel_state[n].nFramesPerPacket = 3
				channel_state[n].nb_subfr = 4
			default:
				celt_assert(false)
				return SILK_DEC_INVALID_FRAME_SIZE
			}
			fs_kHz_dec = opus_int((decControl.internalSampleRate >> 10) + 1)
			if fs_kHz_dec != 8 && fs_kHz_dec != 12 && fs_kHz_dec != 16 {
				celt_assert(false)
				return SILK_DEC_INVALID_SAMPLING_FREQUENCY
			}
			ret += silk_decoder_set_fs(&channel_state[n], fs_kHz_dec, decControl.API_sampleRate)
		}
	}

	if decControl.nChannelsAPI == 2 && decControl.nChannelsInternal == 2 &&
		(psDec.nChannelsAPI == 1 || psDec.nChannelsInternal == 1) {
		psDec.sStereo.pred_prev_Q13 = [2]opus_int16{}
		psDec.sStereo.sSide = [2]opus_int16{}
		channel_state[1].resampler_state = channel_state[0].resampler_state
	}
	psDec.nChannelsAPI = opus_int(decControl.nChannelsAPI)
	psDec.nChannelsInternal = opus_int(decControl.nChannelsInternal)

	if decControl.API_sampleRate > opus_int32(MAX_API_FS_KHZ)*1000 || decControl.API_sampleRate < 8000 {
		return SILK_DEC_INVALID_SAMPLING_FREQUENCY
	}

	if lostFlag != FLAG_PACKET_LOST && channel_state[0].nFramesDecoded == 0 {
		// First decoder call for this payload: decode VAD/LBRR flags.
		for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
			for i := opus_int(0); i < channel_state[n].nFramesPerPacket; i++ {
				channel_state[n].VAD_flags[i] = ec_dec_bit_logp(psRangeDec, 1)
			}
			channel_state[n].LBRR_flag = ec_dec_bit_logp(psRangeDec, 1)
		}
		// Decode LBRR flags.
		for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
			channel_state[n].LBRR_flags = [MAX_FRAMES_PER_PACKET]opus_int{}
			if channel_state[n].LBRR_flag != 0 {
				if channel_state[n].nFramesPerPacket == 1 {
					channel_state[n].LBRR_flags[0] = 1
				} else {
					LBRR_symbol = opus_int32(ec_dec_icdf(psRangeDec,
						asByteSlice(silk_LBRR_flags_iCDF_ptr[channel_state[n].nFramesPerPacket-2]), 8)) + 1
					for i := opus_int(0); i < channel_state[n].nFramesPerPacket; i++ {
						channel_state[n].LBRR_flags[i] = opus_int(silk_RSHIFT(LBRR_symbol, opus_int(i)) & 1)
					}
				}
			}
		}

		if lostFlag == FLAG_DECODE_NORMAL {
			// Regular decoding: skip all LBRR data.
			for i := opus_int(0); i < channel_state[0].nFramesPerPacket; i++ {
				for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
					if channel_state[n].LBRR_flags[i] != 0 {
						var pulses [MAX_FRAME_LENGTH]opus_int16
						var condCoding opus_int

						if decControl.nChannelsInternal == 2 && n == 0 {
							silk_stereo_decode_pred(psRangeDec, MS_pred_Q13[:])
							if channel_state[1].LBRR_flags[i] == 0 {
								silk_stereo_decode_mid_only(psRangeDec, &decode_only_middle)
							}
						}
						if i > 0 && channel_state[n].LBRR_flags[i-1] != 0 {
							condCoding = CODE_CONDITIONALLY
						} else {
							condCoding = CODE_INDEPENDENTLY
						}
						silk_decode_indices(&channel_state[n], psRangeDec, i, 1, condCoding)
						silk_decode_pulses(psRangeDec, pulses[:],
							opus_int(channel_state[n].indices.signalType),
							opus_int(channel_state[n].indices.quantOffsetType),
							channel_state[n].frame_length)
					}
				}
			}
		}
	}

	// Get MS predictor index.
	if decControl.nChannelsInternal == 2 {
		if lostFlag == FLAG_DECODE_NORMAL ||
			(lostFlag == FLAG_DECODE_LBRR && channel_state[0].LBRR_flags[channel_state[0].nFramesDecoded] == 1) {
			silk_stereo_decode_pred(psRangeDec, MS_pred_Q13[:])
			if (lostFlag == FLAG_DECODE_NORMAL && channel_state[1].VAD_flags[channel_state[0].nFramesDecoded] == 0) ||
				(lostFlag == FLAG_DECODE_LBRR && channel_state[1].LBRR_flags[channel_state[0].nFramesDecoded] == 0) {
				silk_stereo_decode_mid_only(psRangeDec, &decode_only_middle)
			} else {
				decode_only_middle = 0
			}
		} else {
			for n := 0; n < 2; n++ {
				MS_pred_Q13[n] = opus_int32(psDec.sStereo.pred_prev_Q13[n])
			}
		}
	}

	// Reset side-channel memory on transition.
	if decControl.nChannelsInternal == 2 && decode_only_middle == 0 && psDec.prev_decode_only_middle == 1 {
		psDec.channel_state[1].outBuf = [MAX_FRAME_LENGTH + 2*MAX_SUB_FRAME_LENGTH]opus_int16{}
		psDec.channel_state[1].sLPC_Q14_buf = [MAX_LPC_ORDER]opus_int32{}
		psDec.channel_state[1].lagPrev = 100
		psDec.channel_state[1].LastGainIndex = 10
		psDec.channel_state[1].prevSignalType = TYPE_NO_VOICE_ACTIVITY
		psDec.channel_state[1].first_frame_after_reset = 1
	}

	// Temp buffer for per-channel output (frame_length + 2 for biquad delay).
	samplesOut1_tmp_storage1 := psDec.scratch_samplesOut1[:int(decControl.nChannelsInternal)*int(channel_state[0].frame_length+2)]
	var samplesOut1_tmp [2][]opus_int16
	samplesOut1_tmp[0] = samplesOut1_tmp_storage1[:channel_state[0].frame_length+2]
	samplesOut1_tmp[1] = samplesOut1_tmp_storage1[channel_state[0].frame_length+2:]

	if lostFlag == FLAG_DECODE_NORMAL {
		if decode_only_middle != 0 {
			has_side = 0
		} else {
			has_side = 1
		}
	} else {
		hasSideBool := psDec.prev_decode_only_middle == 0 ||
			(decControl.nChannelsInternal == 2 && lostFlag == FLAG_DECODE_LBRR &&
				channel_state[1].LBRR_flags[channel_state[1].nFramesDecoded] == 1)
		if hasSideBool {
			has_side = 1
		} else {
			has_side = 0
		}
	}
	channel_state[0].sPLC.enable_deep_plc = decControl.enable_deep_plc
	// Call decoder for one frame.
	for n := opus_int32(0); n < decControl.nChannelsInternal; n++ {
		if n == 0 || has_side != 0 {
			FrameIndex := channel_state[0].nFramesDecoded - opus_int(n)
			var condCoding opus_int
			if FrameIndex <= 0 {
				condCoding = CODE_INDEPENDENTLY
			} else if lostFlag == FLAG_DECODE_LBRR {
				if channel_state[n].LBRR_flags[FrameIndex-1] != 0 {
					condCoding = CODE_CONDITIONALLY
				} else {
					condCoding = CODE_INDEPENDENTLY
				}
			} else if n > 0 && psDec.prev_decode_only_middle != 0 {
				condCoding = CODE_INDEPENDENTLY_NO_LTP_SCALING
			} else {
				condCoding = CODE_CONDITIONALLY
			}
			ret += silk_decode_frame(&channel_state[n], psRangeDec, samplesOut1_tmp[n][2:], &nSamplesOutDec,
				lostFlag, condCoding, arch)
		} else {
			for i := opus_int32(0); i < nSamplesOutDec; i++ {
				samplesOut1_tmp[n][2+i] = 0
			}
		}
		channel_state[n].nFramesDecoded++
	}

	if decControl.nChannelsAPI == 2 && decControl.nChannelsInternal == 2 {
		// Convert Mid/Side to Left/Right.
		silk_stereo_MS_to_LR(&psDec.sStereo, samplesOut1_tmp[0], samplesOut1_tmp[1],
			MS_pred_Q13[:], channel_state[0].fs_kHz, opus_int(nSamplesOutDec))
	} else {
		// Buffering (mono).
		samplesOut1_tmp[0][0] = psDec.sStereo.sMid[0]
		samplesOut1_tmp[0][1] = psDec.sStereo.sMid[1]
		psDec.sStereo.sMid[0] = samplesOut1_tmp[0][nSamplesOutDec]
		psDec.sStereo.sMid[1] = samplesOut1_tmp[0][nSamplesOutDec+1]
	}

	// Number of output samples.
	*nSamplesOut = silk_DIV32(nSamplesOutDec*decControl.API_sampleRate,
		silk_SMULBB(opus_int32(channel_state[0].fs_kHz), 1000))

	samplesOut2_tmp := psDec.scratch_samplesOut2[:*nSamplesOut]
	resample_out_ptr := samplesOut2_tmp

	lim := decControl.nChannelsAPI
	if decControl.nChannelsInternal < lim {
		lim = decControl.nChannelsInternal
	}
	for n := opus_int32(0); n < lim; n++ {
		ret += opus_int(silk_resampler(&channel_state[n].resampler_state,
			resample_out_ptr, samplesOut1_tmp[n][1:], nSamplesOutDec))
		// Interleave if stereo output and stereo stream.
		if decControl.nChannelsAPI == 2 {
			for i := opus_int32(0); i < *nSamplesOut; i++ {
				samplesOut[n+2*i] = INT16TORES(resample_out_ptr[i])
			}
		} else {
			for i := opus_int32(0); i < *nSamplesOut; i++ {
				samplesOut[i] = INT16TORES(resample_out_ptr[i])
			}
		}
	}

	// Two-channel output from mono stream.
	if decControl.nChannelsAPI == 2 && decControl.nChannelsInternal == 1 {
		if stereo_to_mono != 0 {
			ret += opus_int(silk_resampler(&channel_state[1].resampler_state,
				resample_out_ptr, samplesOut1_tmp[0][1:], nSamplesOutDec))
			for i := opus_int32(0); i < *nSamplesOut; i++ {
				samplesOut[1+2*i] = INT16TORES(resample_out_ptr[i])
			}
		} else {
			for i := opus_int32(0); i < *nSamplesOut; i++ {
				samplesOut[1+2*i] = samplesOut[0+2*i]
			}
		}
	}

	// Export pitch lag, measured at 48 kHz.
	if channel_state[0].prevSignalType == TYPE_VOICED {
		mult_tab := [3]int{6, 4, 3}
		decControl.prevPitchLag = channel_state[0].lagPrev * opus_int(mult_tab[(channel_state[0].fs_kHz-8)>>2])
	} else {
		decControl.prevPitchLag = 0
	}

	if lostFlag == FLAG_PACKET_LOST {
		for i := 0; i < psDec.nChannelsInternal; i++ {
			psDec.channel_state[i].LastGainIndex = 10
		}
	} else {
		psDec.prev_decode_only_middle = decode_only_middle
	}
	return ret
}
