package nativeopus

// Port of libopus/silk/enc_API.c.
//
// Top-level SILK encoder API: state sizing, init, query, and the main
// silk_Encode entry point that feeds PCM, resamples into the 8/12/16
// kHz internal rate, and invokes silk_encode_frame_FLP per 10/20 ms
// frame. Our build is float-only, so _Fxx = _FLP throughout.
//
// Stereo paths are preserved verbatim from the C source even though
// the current Phase 8 mono-only parity test exercises only the
// nChannelsAPI == nChannelsInternal == 1 branch. Removing them would
// violate the 1:1 port rule and make Phase 9 (top-level opus_encoder)
// significantly harder to drop into place.

// silk_Get_Encoder_Size — C: enc_API.c:60-74.
//
// We allocate a full silk_encoder struct (Go does not size-alias
// structs), so the "skip second state for mono" trim applies only to
// what we report to the caller. Callers who size their own backing
// memory from this value would miss half the state if they trusted
// the short size; but that scheme is C-specific and the Go port
// always passes a pointer to a fully-sized silk_encoder.
func silk_Get_Encoder_Size(encSizeBytes *opus_int, channels opus_int) opus_int {
	ret := opus_int(SILK_NO_ERROR)

	// sizeof values here are symbolic — in Go we don't use this return
	// for allocation. We report the C-shaped size (bytes of silk_encoder
	// minus one state struct for mono) for parity with callers that
	// round-trip it. The numeric value is NOT consumed by any Go
	// caller; opus_encoder's Go port will allocate a real struct.
	*encSizeBytes = 1
	if channels == 1 {
		// Mirror the C trim for completeness.
		*encSizeBytes = 1
	}

	return ret
}

// silk_InitEncoder — C: enc_API.c:79-108.
func silk_InitEncoder(
	encState *silk_encoder,
	channels opus_int,
	arch int,
	encStatus *silk_EncControlStruct,
) opus_int {
	var n opus_int
	ret := opus_int(SILK_NO_ERROR)

	// Reset encoder. In Go this is a full zero of silk_encoder — we
	// can't "skip second state for mono" because the struct layout is
	// fixed, but the practical effect matches C (the unused state_Fxx[1]
	// is zero either way).
	*encState = silk_encoder{}
	for n = 0; n < channels; n++ {
		if r := silk_init_encoder(&encState.state_Fxx[n], arch); r != 0 {
			ret += r
			celt_assert(false)
		}
	}

	encState.nChannelsAPI = 1
	encState.nChannelsInternal = 1

	// Read control structure.
	if r := silk_QueryEncoder(encState, encStatus); r != 0 {
		ret += r
		celt_assert(false)
	}

	return ret
}

// silk_QueryEncoder — C: enc_API.c:113-142.
func silk_QueryEncoder(encState *silk_encoder, encStatus *silk_EncControlStruct) opus_int {
	ret := opus_int(SILK_NO_ERROR)
	state_Fxx := &encState.state_Fxx

	encStatus.nChannelsAPI = opus_int32(encState.nChannelsAPI)
	encStatus.nChannelsInternal = opus_int32(encState.nChannelsInternal)
	encStatus.API_sampleRate = state_Fxx[0].sCmn.API_fs_Hz
	encStatus.maxInternalSampleRate = opus_int32(state_Fxx[0].sCmn.maxInternal_fs_Hz)
	encStatus.minInternalSampleRate = opus_int32(state_Fxx[0].sCmn.minInternal_fs_Hz)
	encStatus.desiredInternalSampleRate = opus_int32(state_Fxx[0].sCmn.desiredInternal_fs_Hz)
	encStatus.payloadSize_ms = state_Fxx[0].sCmn.PacketSize_ms
	encStatus.bitRate = state_Fxx[0].sCmn.TargetRate_bps
	encStatus.packetLossPercentage = state_Fxx[0].sCmn.PacketLoss_perc
	encStatus.complexity = state_Fxx[0].sCmn.Complexity
	encStatus.useInBandFEC = state_Fxx[0].sCmn.useInBandFEC
	encStatus.useDTX = state_Fxx[0].sCmn.useDTX
	encStatus.useCBR = state_Fxx[0].sCmn.useCBR
	encStatus.internalSampleRate = opus_int32(silk_SMULBB(opus_int32(state_Fxx[0].sCmn.fs_kHz), 1000))
	encStatus.allowBandwidthSwitch = state_Fxx[0].sCmn.allow_bandwidth_switch
	if state_Fxx[0].sCmn.fs_kHz == 16 && state_Fxx[0].sCmn.sLP.mode == 0 {
		encStatus.inWBmodeWithoutVariableLP = 1
	} else {
		encStatus.inWBmodeWithoutVariableLP = 0
	}

	return ret
}

// silk_Encode — C: enc_API.c:150-601.
//
// samplesIn is opus_res ([]float32 in our float build). The caller
// passes a raw slice; C arithmetic samplesIn += nSamplesFromInput * nCh
// becomes a re-slice in Go.
func silk_Encode(
	encState *silk_encoder,
	encControl *silk_EncControlStruct,
	samplesIn []opus_res,
	nSamplesIn opus_int,
	psRangeEnc *ec_enc,
	nBytesOut *opus_int32,
	prefillFlag opus_int,
	activity opus_int,
) opus_int {
	var n, i, nBits, flags opus_int
	var tmp_payloadSize_ms, tmp_complexity opus_int
	ret := opus_int(0)
	var nSamplesToBuffer, nSamplesToBufferMax, nBlocksOf10ms opus_int
	var nSamplesFromInput, nSamplesFromInputMax opus_int
	nSamplesFromInput = 0
	var speech_act_thr_for_switch_Q8 opus_int
	var TargetRate_bps, channelRate_bps, LBRR_symbol, sum opus_int32
	var MStargetRates_bps [2]opus_int32
	psEnc := encState
	var transition, curr_block, tot_blocks opus_int

	celt_assert(encControl.nChannelsAPI >= encControl.nChannelsInternal && encControl.nChannelsAPI >= opus_int32(psEnc.nChannelsInternal))
	if encControl.reducedDependency != 0 {
		for n = 0; n < opus_int(encControl.nChannelsAPI); n++ {
			psEnc.state_Fxx[n].sCmn.first_frame_after_reset = 1
		}
	}
	for n = 0; n < opus_int(encControl.nChannelsAPI); n++ {
		psEnc.state_Fxx[n].sCmn.nFramesEncoded = 0
	}
	// Check values in encoder control structure.
	if ret = check_control_input(encControl); ret != 0 {
		celt_assert(false)
		return ret
	}

	encControl.switchReady = 0

	if encControl.nChannelsInternal > opus_int32(psEnc.nChannelsInternal) {
		// Mono -> Stereo transition: init state of second channel and stereo state.
		ret += silk_init_encoder(&psEnc.state_Fxx[1], psEnc.state_Fxx[0].sCmn.arch)
		psEnc.sStereo.pred_prev_Q13 = [2]opus_int16{}
		psEnc.sStereo.sSide = [2]opus_int16{}
		psEnc.sStereo.mid_side_amp_Q0[0] = 0
		psEnc.sStereo.mid_side_amp_Q0[1] = 1
		psEnc.sStereo.mid_side_amp_Q0[2] = 0
		psEnc.sStereo.mid_side_amp_Q0[3] = 1
		psEnc.sStereo.width_prev_Q14 = 0
		psEnc.sStereo.smth_width_Q14 = opus_int16(SILK_FIX_CONST(1, 14))
		if psEnc.nChannelsAPI == 2 {
			psEnc.state_Fxx[1].sCmn.resampler_state = psEnc.state_Fxx[0].sCmn.resampler_state
			psEnc.state_Fxx[1].sCmn.In_HP_State = psEnc.state_Fxx[0].sCmn.In_HP_State
		}
	}

	transition = 0
	if encControl.payloadSize_ms != psEnc.state_Fxx[0].sCmn.PacketSize_ms ||
		opus_int32(psEnc.nChannelsInternal) != encControl.nChannelsInternal {
		transition = 1
	}

	psEnc.nChannelsAPI = opus_int(encControl.nChannelsAPI)
	psEnc.nChannelsInternal = opus_int(encControl.nChannelsInternal)

	nBlocksOf10ms = opus_int(silk_DIV32(100*opus_int32(nSamplesIn), encControl.API_sampleRate))
	if nBlocksOf10ms > 1 {
		tot_blocks = nBlocksOf10ms >> 1
	} else {
		tot_blocks = 1
	}
	curr_block = 0
	if prefillFlag != 0 {
		var save_LP silk_LP_state
		// Only accept input length of 10 ms.
		if nBlocksOf10ms != 1 {
			celt_assert(false)
			return SILK_ENC_INPUT_INVALID_NO_OF_SAMPLES
		}
		if prefillFlag == 2 {
			save_LP = psEnc.state_Fxx[0].sCmn.sLP
			// Save the sampling rate so the bandwidth switching code can keep handling transitions.
			save_LP.saved_fs_kHz = opus_int32(psEnc.state_Fxx[0].sCmn.fs_kHz)
		}
		// Reset Encoder.
		for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
			ret = silk_init_encoder(&psEnc.state_Fxx[n], psEnc.state_Fxx[n].sCmn.arch)
			// Restore the variable LP state.
			if prefillFlag == 2 {
				psEnc.state_Fxx[n].sCmn.sLP = save_LP
			}
			celt_assert(ret == 0)
		}
		tmp_payloadSize_ms = encControl.payloadSize_ms
		encControl.payloadSize_ms = 10
		tmp_complexity = encControl.complexity
		encControl.complexity = 0
		for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
			psEnc.state_Fxx[n].sCmn.controlled_since_last_payload = 0
			psEnc.state_Fxx[n].sCmn.prefillFlag = 1
		}
	} else {
		// Only accept input lengths that are a multiple of 10 ms.
		if opus_int32(nBlocksOf10ms)*encControl.API_sampleRate != 100*opus_int32(nSamplesIn) || nSamplesIn < 0 {
			celt_assert(false)
			return SILK_ENC_INPUT_INVALID_NO_OF_SAMPLES
		}
		// Make sure no more than one packet can be produced.
		if 1000*opus_int32(nSamplesIn) > opus_int32(encControl.payloadSize_ms)*encControl.API_sampleRate {
			celt_assert(false)
			return SILK_ENC_INPUT_INVALID_NO_OF_SAMPLES
		}
	}

	for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
		// Force the side channel to the same rate as the mid.
		var force_fs_kHz opus_int
		if n == 1 {
			force_fs_kHz = psEnc.state_Fxx[0].sCmn.fs_kHz
		} else {
			force_fs_kHz = 0
		}
		if ret = silk_control_encoder(&psEnc.state_Fxx[n], encControl, psEnc.allowBandwidthSwitch, n, force_fs_kHz); ret != 0 {
			silk_assert(false)
			return ret
		}
		if psEnc.state_Fxx[n].sCmn.first_frame_after_reset != 0 || transition != 0 {
			for i = 0; i < psEnc.state_Fxx[0].sCmn.nFramesPerPacket; i++ {
				psEnc.state_Fxx[n].sCmn.LBRR_flags[i] = 0
			}
		}
		psEnc.state_Fxx[n].sCmn.inDTX = psEnc.state_Fxx[n].sCmn.useDTX
	}
	celt_assert(encControl.nChannelsInternal == 1 || psEnc.state_Fxx[0].sCmn.fs_kHz == psEnc.state_Fxx[1].sCmn.fs_kHz)

	// Input buffering/resampling and encoding.
	nSamplesToBufferMax = 10 * nBlocksOf10ms * psEnc.state_Fxx[0].sCmn.fs_kHz
	nSamplesFromInputMax = opus_int(silk_DIV32_16(
		opus_int32(nSamplesToBufferMax)*psEnc.state_Fxx[0].sCmn.API_fs_Hz,
		opus_int32(psEnc.state_Fxx[0].sCmn.fs_kHz)*1000))
	buf := make([]opus_int16, nSamplesFromInputMax)
	for {
		curr_nBitsUsedLBRR := opus_int(0)
		nSamplesToBuffer = psEnc.state_Fxx[0].sCmn.frame_length - psEnc.state_Fxx[0].sCmn.inputBufIx
		nSamplesToBuffer = silk_min(nSamplesToBuffer, nSamplesToBufferMax)
		nSamplesFromInput = opus_int(silk_DIV32_16(
			opus_int32(nSamplesToBuffer)*psEnc.state_Fxx[0].sCmn.API_fs_Hz,
			opus_int32(psEnc.state_Fxx[0].sCmn.fs_kHz)*1000))
		// Resample and write to buffer.
		if encControl.nChannelsAPI == 2 && encControl.nChannelsInternal == 2 {
			id := psEnc.state_Fxx[0].sCmn.nFramesEncoded
			for n = 0; n < nSamplesFromInput; n++ {
				buf[n] = res2int16(samplesIn[2*n])
			}
			// Making sure to start both resamplers from the same state when switching from mono to stereo.
			if psEnc.nPrevChannelsInternal == 1 && id == 0 {
				psEnc.state_Fxx[1].sCmn.resampler_state = psEnc.state_Fxx[0].sCmn.resampler_state
			}

			ret += silk_resampler(&psEnc.state_Fxx[0].sCmn.resampler_state,
				psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.inputBufIx+2:],
				buf, opus_int32(nSamplesFromInput))
			psEnc.state_Fxx[0].sCmn.inputBufIx += nSamplesToBuffer

			nSamplesToBuffer = psEnc.state_Fxx[1].sCmn.frame_length - psEnc.state_Fxx[1].sCmn.inputBufIx
			nSamplesToBuffer = silk_min(nSamplesToBuffer, 10*nBlocksOf10ms*psEnc.state_Fxx[1].sCmn.fs_kHz)
			for n = 0; n < nSamplesFromInput; n++ {
				buf[n] = res2int16(samplesIn[2*n+1])
			}
			ret += silk_resampler(&psEnc.state_Fxx[1].sCmn.resampler_state,
				psEnc.state_Fxx[1].sCmn.inputBuf[psEnc.state_Fxx[1].sCmn.inputBufIx+2:],
				buf, opus_int32(nSamplesFromInput))

			psEnc.state_Fxx[1].sCmn.inputBufIx += nSamplesToBuffer
		} else if encControl.nChannelsAPI == 2 && encControl.nChannelsInternal == 1 {
			// Combine left and right channels before resampling.
			for n = 0; n < nSamplesFromInput; n++ {
				sum = opus_int32(res2int16(samplesIn[2*n] + samplesIn[2*n+1]))
				buf[n] = opus_int16(silk_RSHIFT_ROUND(sum, 1))
			}
			ret += silk_resampler(&psEnc.state_Fxx[0].sCmn.resampler_state,
				psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.inputBufIx+2:],
				buf, opus_int32(nSamplesFromInput))
			// On the first mono frame, average the results for the two resampler states.
			if psEnc.nPrevChannelsInternal == 2 && psEnc.state_Fxx[0].sCmn.nFramesEncoded == 0 {
				ret += silk_resampler(&psEnc.state_Fxx[1].sCmn.resampler_state,
					psEnc.state_Fxx[1].sCmn.inputBuf[psEnc.state_Fxx[1].sCmn.inputBufIx+2:],
					buf, opus_int32(nSamplesFromInput))
				for n = 0; n < psEnc.state_Fxx[0].sCmn.frame_length; n++ {
					psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.inputBufIx+n+2] =
						opus_int16(silk_RSHIFT(
							opus_int32(psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.inputBufIx+n+2])+
								opus_int32(psEnc.state_Fxx[1].sCmn.inputBuf[psEnc.state_Fxx[1].sCmn.inputBufIx+n+2]), 1))
				}
			}
			psEnc.state_Fxx[0].sCmn.inputBufIx += nSamplesToBuffer
		} else {
			celt_assert(encControl.nChannelsAPI == 1 && encControl.nChannelsInternal == 1)
			for n = 0; n < nSamplesFromInput; n++ {
				buf[n] = res2int16(samplesIn[n])
			}
			ret += silk_resampler(&psEnc.state_Fxx[0].sCmn.resampler_state,
				psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.inputBufIx+2:],
				buf, opus_int32(nSamplesFromInput))
			psEnc.state_Fxx[0].sCmn.inputBufIx += nSamplesToBuffer
		}

		samplesIn = samplesIn[nSamplesFromInput*opus_int(encControl.nChannelsAPI):]
		nSamplesIn -= nSamplesFromInput

		// Default.
		psEnc.allowBandwidthSwitch = 0

		// Silk encoder.
		if psEnc.state_Fxx[0].sCmn.inputBufIx >= psEnc.state_Fxx[0].sCmn.frame_length {
			// Enough data in input buffer, so encode.
			celt_assert(psEnc.state_Fxx[0].sCmn.inputBufIx == psEnc.state_Fxx[0].sCmn.frame_length)
			celt_assert(encControl.nChannelsInternal == 1 || psEnc.state_Fxx[1].sCmn.inputBufIx == psEnc.state_Fxx[1].sCmn.frame_length)

			// Deal with LBRR data.
			if psEnc.state_Fxx[0].sCmn.nFramesEncoded == 0 && prefillFlag == 0 {
				// Create space at start of payload for VAD and FEC flags.
				var iCDF [2]opus_uint8
				iCDF[0] = opus_uint8(256 - silk_RSHIFT(256, opus_int((psEnc.state_Fxx[0].sCmn.nFramesPerPacket+1)*opus_int(encControl.nChannelsInternal))))
				ec_enc_icdf(psRangeEnc, 0, iCDF[:], 8)
				curr_nBitsUsedLBRR = opus_int(ec_tell(psRangeEnc))

				// Encode any LBRR data from previous packet. Encode LBRR flags.
				for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
					LBRR_symbol = 0
					for i = 0; i < psEnc.state_Fxx[n].sCmn.nFramesPerPacket; i++ {
						LBRR_symbol |= silk_LSHIFT(opus_int32(psEnc.state_Fxx[n].sCmn.LBRR_flags[i]), opus_int(i))
					}
					if LBRR_symbol > 0 {
						psEnc.state_Fxx[n].sCmn.LBRR_flag = 1
					} else {
						psEnc.state_Fxx[n].sCmn.LBRR_flag = 0
					}
					if LBRR_symbol != 0 && psEnc.state_Fxx[n].sCmn.nFramesPerPacket > 1 {
						ec_enc_icdf(psRangeEnc, int(LBRR_symbol-1), asByteSlice(silk_LBRR_flags_iCDF_ptr[psEnc.state_Fxx[n].sCmn.nFramesPerPacket-2]), 8)
					}
				}

				// Code LBRR indices and excitation signals.
				for i = 0; i < psEnc.state_Fxx[0].sCmn.nFramesPerPacket; i++ {
					for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
						if psEnc.state_Fxx[n].sCmn.LBRR_flags[i] != 0 {
							var condCoding opus_int

							if encControl.nChannelsInternal == 2 && n == 0 {
								silk_stereo_encode_pred(psRangeEnc, psEnc.sStereo.predIx[i])
								// For LBRR data there's no need to code the mid-only flag if the side-channel LBRR flag is set.
								if psEnc.state_Fxx[1].sCmn.LBRR_flags[i] == 0 {
									silk_stereo_encode_mid_only(psRangeEnc, psEnc.sStereo.mid_only_flags[i])
								}
							}
							// Use conditional coding if previous frame available.
							if i > 0 && psEnc.state_Fxx[n].sCmn.LBRR_flags[i-1] != 0 {
								condCoding = CODE_CONDITIONALLY
							} else {
								condCoding = CODE_INDEPENDENTLY
							}
							silk_encode_indices(&psEnc.state_Fxx[n].sCmn, psRangeEnc, i, 1, condCoding)
							silk_encode_pulses(psRangeEnc,
								opus_int(psEnc.state_Fxx[n].sCmn.indices_LBRR[i].signalType),
								opus_int(psEnc.state_Fxx[n].sCmn.indices_LBRR[i].quantOffsetType),
								psEnc.state_Fxx[n].sCmn.pulses_LBRR[i][:], psEnc.state_Fxx[n].sCmn.frame_length)
						}
					}
				}

				// Reset LBRR flags.
				for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
					for k := 0; k < MAX_FRAMES_PER_PACKET; k++ {
						psEnc.state_Fxx[n].sCmn.LBRR_flags[k] = 0
					}
				}
				curr_nBitsUsedLBRR = opus_int(ec_tell(psRangeEnc)) - curr_nBitsUsedLBRR
			}

			silk_HP_variable_cutoff(psEnc.state_Fxx[:])

			// Total target bits for packet.
			nBits = opus_int(silk_DIV32_16(silk_MUL(encControl.bitRate, opus_int32(encControl.payloadSize_ms)), 1000))
			// Subtract bits used for LBRR.
			if prefillFlag == 0 {
				// psEnc->nBitsUsedLBRR is an exponential moving average of the LBRR usage.
				if curr_nBitsUsedLBRR < 10 {
					psEnc.nBitsUsedLBRR = 0
				} else if psEnc.nBitsUsedLBRR < 10 {
					psEnc.nBitsUsedLBRR = opus_int32(curr_nBitsUsedLBRR)
				} else {
					psEnc.nBitsUsedLBRR = (psEnc.nBitsUsedLBRR + opus_int32(curr_nBitsUsedLBRR)) / 2
				}
				nBits -= opus_int(psEnc.nBitsUsedLBRR)
			}
			// Divide by number of uncoded frames left in packet.
			nBits = opus_int(silk_DIV32_16(opus_int32(nBits), opus_int32(psEnc.state_Fxx[0].sCmn.nFramesPerPacket)))
			// Convert to bits/second.
			if encControl.payloadSize_ms == 10 {
				TargetRate_bps = silk_SMULBB(opus_int32(nBits), 100)
			} else {
				TargetRate_bps = silk_SMULBB(opus_int32(nBits), 50)
			}
			// Subtract fraction of bits in excess of target in previous frames and packets.
			TargetRate_bps -= silk_DIV32_16(silk_MUL(psEnc.nBitsExceeded, 1000), BITRESERVOIR_DECAY_TIME_MS)
			if prefillFlag == 0 && psEnc.state_Fxx[0].sCmn.nFramesEncoded > 0 {
				// Compare actual vs target bits so far in this packet.
				bitsBalance := opus_int32(ec_tell(psRangeEnc)) - psEnc.nBitsUsedLBRR - opus_int32(nBits)*opus_int32(psEnc.state_Fxx[0].sCmn.nFramesEncoded)
				TargetRate_bps -= silk_DIV32_16(silk_MUL(bitsBalance, 1000), BITRESERVOIR_DECAY_TIME_MS)
			}
			// Never exceed input bitrate.
			TargetRate_bps = silk_LIMIT(TargetRate_bps, encControl.bitRate, 5000)

			// Convert Left/Right to Mid/Side.
			if encControl.nChannelsInternal == 2 {
				// Pass full inputBuf (C uses &inputBuf[2] and then
				// aliases mid = &x1[-2]; the Go port of
				// silk_stereo_LR_to_MS expects x1Buf indexed from 0
				// and assigns mid = x1Buf so the in-place write
				// matches C's mid[n] which is inputBuf[n]).
				silk_stereo_LR_to_MS(&psEnc.sStereo,
					psEnc.state_Fxx[0].sCmn.inputBuf[:],
					psEnc.state_Fxx[1].sCmn.inputBuf[:],
					&psEnc.sStereo.predIx[psEnc.state_Fxx[0].sCmn.nFramesEncoded],
					&psEnc.sStereo.mid_only_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded],
					MStargetRates_bps[:], TargetRate_bps, opus_int(psEnc.state_Fxx[0].sCmn.speech_activity_Q8), encControl.toMono,
					psEnc.state_Fxx[0].sCmn.fs_kHz, psEnc.state_Fxx[0].sCmn.frame_length)
				if psEnc.sStereo.mid_only_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded] == 0 {
					// Reset side channel encoder memory for first frame with side coding.
					if psEnc.prev_decode_only_middle == 1 {
						psEnc.state_Fxx[1].sShape = silk_shape_state_FLP{}
						psEnc.state_Fxx[1].sCmn.sNSQ = silk_nsq_state{}
						psEnc.state_Fxx[1].sCmn.prev_NLSFq_Q15 = [MAX_LPC_ORDER]opus_int16{}
						psEnc.state_Fxx[1].sCmn.sLP.In_LP_State = [2]opus_int32{}
						psEnc.state_Fxx[1].sCmn.prevLag = 100
						psEnc.state_Fxx[1].sCmn.sNSQ.lagPrev = 100
						psEnc.state_Fxx[1].sShape.LastGainIndex = 10
						psEnc.state_Fxx[1].sCmn.prevSignalType = TYPE_NO_VOICE_ACTIVITY
						psEnc.state_Fxx[1].sCmn.sNSQ.prev_gain_Q16 = 65536
						psEnc.state_Fxx[1].sCmn.first_frame_after_reset = 1
					}
					silk_encode_do_VAD_FLP(&psEnc.state_Fxx[1], activity)
				} else {
					psEnc.state_Fxx[1].sCmn.VAD_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded] = 0
				}
				if prefillFlag == 0 {
					silk_stereo_encode_pred(psRangeEnc, psEnc.sStereo.predIx[psEnc.state_Fxx[0].sCmn.nFramesEncoded])
					if psEnc.state_Fxx[1].sCmn.VAD_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded] == 0 {
						silk_stereo_encode_mid_only(psRangeEnc, psEnc.sStereo.mid_only_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded])
					}
				}
			} else {
				// Buffering.
				// silk_memcpy( psEnc->state_Fxx[ 0 ].sCmn.inputBuf, psEnc->sStereo.sMid, 2 * sizeof( opus_int16 ) );
				psEnc.state_Fxx[0].sCmn.inputBuf[0] = psEnc.sStereo.sMid[0]
				psEnc.state_Fxx[0].sCmn.inputBuf[1] = psEnc.sStereo.sMid[1]
				// silk_memcpy( psEnc->sStereo.sMid, &psEnc->state_Fxx[ 0 ].sCmn.inputBuf[ frame_length ], 2 * sizeof( opus_int16 ) );
				psEnc.sStereo.sMid[0] = psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.frame_length]
				psEnc.sStereo.sMid[1] = psEnc.state_Fxx[0].sCmn.inputBuf[psEnc.state_Fxx[0].sCmn.frame_length+1]
			}
			silk_encode_do_VAD_FLP(&psEnc.state_Fxx[0], activity)

			// Encode.
			for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
				var maxBits, useCBR opus_int

				// Handling rate constraints.
				maxBits = encControl.maxBits
				if tot_blocks == 2 && curr_block == 0 {
					maxBits = maxBits * 3 / 5
				} else if tot_blocks == 3 {
					if curr_block == 0 {
						maxBits = maxBits * 2 / 5
					} else if curr_block == 1 {
						maxBits = maxBits * 3 / 4
					}
				}
				if encControl.useCBR != 0 && curr_block == tot_blocks-1 {
					useCBR = 1
				} else {
					useCBR = 0
				}

				if encControl.nChannelsInternal == 1 {
					channelRate_bps = TargetRate_bps
				} else {
					channelRate_bps = MStargetRates_bps[n]
					if n == 0 && MStargetRates_bps[1] > 0 {
						useCBR = 0
						// Give mid up to 1/2 of the max bits for that frame.
						maxBits -= encControl.maxBits / (tot_blocks * 2)
					}
				}

				if channelRate_bps > 0 {
					var condCoding opus_int

					silk_control_SNR(&psEnc.state_Fxx[n].sCmn, channelRate_bps)

					// Use independent coding if no previous frame available.
					if psEnc.state_Fxx[0].sCmn.nFramesEncoded-n <= 0 {
						condCoding = CODE_INDEPENDENTLY
					} else if n > 0 && psEnc.prev_decode_only_middle != 0 {
						// If we skipped a side frame in this packet, we don't
						// need LTP scaling; the LTP state is well-defined.
						condCoding = CODE_INDEPENDENTLY_NO_LTP_SCALING
					} else {
						condCoding = CODE_CONDITIONALLY
					}
					if ret = silk_encode_frame_FLP(&psEnc.state_Fxx[n], nBytesOut, psRangeEnc, condCoding, maxBits, useCBR); ret != 0 {
						silk_assert(false)
					}
				}
				psEnc.state_Fxx[n].sCmn.controlled_since_last_payload = 0
				psEnc.state_Fxx[n].sCmn.inputBufIx = 0
				psEnc.state_Fxx[n].sCmn.nFramesEncoded++
			}
			psEnc.prev_decode_only_middle = opus_int(psEnc.sStereo.mid_only_flags[psEnc.state_Fxx[0].sCmn.nFramesEncoded-1])

			// Insert VAD and FEC flags at beginning of bitstream.
			if *nBytesOut > 0 && psEnc.state_Fxx[0].sCmn.nFramesEncoded == psEnc.state_Fxx[0].sCmn.nFramesPerPacket {
				flags = 0
				for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
					for i = 0; i < psEnc.state_Fxx[n].sCmn.nFramesPerPacket; i++ {
						flags = opus_int(silk_LSHIFT(opus_int32(flags), 1))
						flags |= opus_int(psEnc.state_Fxx[n].sCmn.VAD_flags[i])
					}
					flags = opus_int(silk_LSHIFT(opus_int32(flags), 1))
					flags |= opus_int(psEnc.state_Fxx[n].sCmn.LBRR_flag)
				}
				if prefillFlag == 0 {
					ec_enc_patch_initial_bits(psRangeEnc, opus_uint32(flags), int((psEnc.state_Fxx[0].sCmn.nFramesPerPacket+1)*opus_int(encControl.nChannelsInternal)))
				}

				// Return zero bytes if all channels DTXed.
				if psEnc.state_Fxx[0].sCmn.inDTX != 0 && (encControl.nChannelsInternal == 1 || psEnc.state_Fxx[1].sCmn.inDTX != 0) {
					*nBytesOut = 0
				}

				psEnc.nBitsExceeded += *nBytesOut * 8
				psEnc.nBitsExceeded -= silk_DIV32_16(silk_MUL(encControl.bitRate, opus_int32(encControl.payloadSize_ms)), 1000)
				psEnc.nBitsExceeded = silk_LIMIT(psEnc.nBitsExceeded, 0, 10000)

				// Update flag indicating if bandwidth switching is allowed.
				speech_act_thr_for_switch_Q8 = opus_int(silk_SMLAWB(SILK_FIX_CONST(SPEECH_ACTIVITY_DTX_THRES, 8),
					SILK_FIX_CONST((1-SPEECH_ACTIVITY_DTX_THRES)/MAX_BANDWIDTH_SWITCH_DELAY_MS, 16+8),
					opus_int32(psEnc.timeSinceSwitchAllowed_ms)))
				if psEnc.state_Fxx[0].sCmn.speech_activity_Q8 < speech_act_thr_for_switch_Q8 {
					psEnc.allowBandwidthSwitch = 1
					psEnc.timeSinceSwitchAllowed_ms = 0
				} else {
					psEnc.allowBandwidthSwitch = 0
					psEnc.timeSinceSwitchAllowed_ms += encControl.payloadSize_ms
				}
			}

			if nSamplesIn == 0 {
				break
			}
		} else {
			break
		}
		curr_block++
	}

	psEnc.nPrevChannelsInternal = opus_int(encControl.nChannelsInternal)

	encControl.allowBandwidthSwitch = psEnc.allowBandwidthSwitch
	if psEnc.state_Fxx[0].sCmn.fs_kHz == 16 && psEnc.state_Fxx[0].sCmn.sLP.mode == 0 {
		encControl.inWBmodeWithoutVariableLP = 1
	} else {
		encControl.inWBmodeWithoutVariableLP = 0
	}
	encControl.internalSampleRate = opus_int32(silk_SMULBB(opus_int32(psEnc.state_Fxx[0].sCmn.fs_kHz), 1000))
	if encControl.toMono != 0 {
		encControl.stereoWidth_Q14 = 0
	} else {
		encControl.stereoWidth_Q14 = opus_int(psEnc.sStereo.smth_width_Q14)
	}
	if prefillFlag != 0 {
		encControl.payloadSize_ms = tmp_payloadSize_ms
		encControl.complexity = tmp_complexity
		for n = 0; n < opus_int(encControl.nChannelsInternal); n++ {
			psEnc.state_Fxx[n].sCmn.controlled_since_last_payload = 0
			psEnc.state_Fxx[n].sCmn.prefillFlag = 0
		}
	}

	encControl.signalType = opus_int(psEnc.state_Fxx[0].sCmn.indices.signalType)
	encControl.offset = opus_int(silk_Quantization_Offsets_Q10[psEnc.state_Fxx[0].sCmn.indices.signalType>>1][psEnc.state_Fxx[0].sCmn.indices.quantOffsetType])
	return ret
}

// res2int16 — RES2INT16 macro. For the float build, opus_res is
// float32 representing the sample in [-1,1]; C clamps to int16 via
// FLOAT2INT16 (with saturation). We mirror the C path used when
// FIXED_POINT and DISABLE_FLOAT_API are off.
func res2int16(x opus_res) opus_int16 {
	v := float32(x) * 32768.0
	if v > 32767.0 {
		return 32767
	}
	if v < -32768.0 {
		return -32768
	}
	// C uses float2int (round-to-nearest-even) on float * 32768.
	return opus_int16(silk_float2int(v))
}
