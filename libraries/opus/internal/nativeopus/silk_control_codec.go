package nativeopus

// Port of libopus/silk/control_codec.c.
//
// Encoder-side control routines: resampler preparation, internal
// sampling-frequency setup, complexity tuning, and LBRR gain control.
// This port follows the non-FIXED_POINT (float) build path since the
// encoder state here is silk_encoder_state_FLP (x_buf is silk_float).

// silk_control_encoder — main entry. C: control_codec.c:64-132.
func silk_control_encoder(
	psEnc *silk_encoder_state_FLP,
	encControl *silk_EncControlStruct,
	allow_bw_switch opus_int,
	channelNb opus_int,
	force_fs_kHz opus_int,
) opus_int {
	ret := opus_int(0)

	psEnc.sCmn.useDTX = encControl.useDTX
	psEnc.sCmn.useCBR = encControl.useCBR
	psEnc.sCmn.API_fs_Hz = encControl.API_sampleRate
	psEnc.sCmn.maxInternal_fs_Hz = opus_int(encControl.maxInternalSampleRate)
	psEnc.sCmn.minInternal_fs_Hz = opus_int(encControl.minInternalSampleRate)
	psEnc.sCmn.desiredInternal_fs_Hz = opus_int(encControl.desiredInternalSampleRate)
	psEnc.sCmn.useInBandFEC = encControl.useInBandFEC
	psEnc.sCmn.nChannelsAPI = opus_int(encControl.nChannelsAPI)
	psEnc.sCmn.nChannelsInternal = opus_int(encControl.nChannelsInternal)
	psEnc.sCmn.allow_bandwidth_switch = allow_bw_switch
	psEnc.sCmn.channelNb = channelNb

	if psEnc.sCmn.controlled_since_last_payload != 0 && psEnc.sCmn.prefillFlag == 0 {
		if psEnc.sCmn.API_fs_Hz != psEnc.sCmn.prev_API_fs_Hz && psEnc.sCmn.fs_kHz > 0 {
			// Change in API sampling rate in the middle of encoding a packet.
			ret += silk_setup_resamplers(psEnc, psEnc.sCmn.fs_kHz)
		}
		return ret
	}

	// Beyond this point we know that there are no previously coded frames in the payload buffer.

	var fs_kHz opus_int
	fs_kHz = silk_control_audio_bandwidth(&psEnc.sCmn, encControl)
	if force_fs_kHz != 0 {
		fs_kHz = force_fs_kHz
	}

	ret += silk_setup_resamplers(psEnc, fs_kHz)
	ret += silk_setup_fs(psEnc, fs_kHz, encControl.payloadSize_ms)
	ret += silk_setup_complexity(&psEnc.sCmn, encControl.complexity)

	psEnc.sCmn.PacketLoss_perc = encControl.packetLossPercentage

	ret += silk_setup_LBRR(&psEnc.sCmn, encControl)

	psEnc.sCmn.controlled_since_last_payload = 1

	return ret
}

// silk_setup_resamplers — C: control_codec.c:134-197.
// Non-FIXED_POINT branch: x_buf is silk_float, so we materialize to int16
// temporarily before resampling.
func silk_setup_resamplers(psEnc *silk_encoder_state_FLP, fs_kHz opus_int) opus_int {
	ret := opus_int(SILK_NO_ERROR)

	if psEnc.sCmn.fs_kHz != fs_kHz || psEnc.sCmn.prev_API_fs_Hz != psEnc.sCmn.API_fs_Hz {
		if psEnc.sCmn.fs_kHz == 0 {
			// Initialize the resampler for enc_API.c preparing resampling from API_fs_Hz to fs_kHz.
			ret += opus_int(silk_resampler_init(&psEnc.sCmn.resampler_state, psEnc.sCmn.API_fs_Hz, opus_int32(fs_kHz)*1000, 1))
		} else {
			buf_length_ms := silk_LSHIFT(opus_int32(psEnc.sCmn.nb_subfr*5), 1) + LA_SHAPE_MS
			old_buf_samples := buf_length_ms * opus_int32(psEnc.sCmn.fs_kHz)

			new_buf_samples := buf_length_ms * opus_int32(fs_kHz)
			x_bufFIX := make([]opus_int16, silk_max(old_buf_samples, new_buf_samples))
			// silk_float2short_array writes length-1..0.
			for k := old_buf_samples - 1; k >= 0; k-- {
				x_bufFIX[k] = opus_int16(silk_SAT16(float2int(float32(psEnc.x_buf[k]))))
			}

			// Initialize resampler for temporary resampling of x_buf data to API_fs_Hz.
			var temp_resampler_state silk_resampler_state_struct
			ret += opus_int(silk_resampler_init(&temp_resampler_state, silk_SMULBB(opus_int32(psEnc.sCmn.fs_kHz), 1000), psEnc.sCmn.API_fs_Hz, 0))

			// Calculate number of samples to temporarily upsample.
			api_buf_samples := buf_length_ms * silk_DIV32_16(psEnc.sCmn.API_fs_Hz, 1000)

			// Temporary resampling of x_buf data to API_fs_Hz.
			x_buf_API_fs_Hz := make([]opus_int16, api_buf_samples)
			ret += opus_int(silk_resampler(&temp_resampler_state, x_buf_API_fs_Hz, x_bufFIX, old_buf_samples))

			// Initialize the resampler for enc_API.c preparing resampling from API_fs_Hz to fs_kHz.
			ret += opus_int(silk_resampler_init(&psEnc.sCmn.resampler_state, psEnc.sCmn.API_fs_Hz, silk_SMULBB(opus_int32(fs_kHz), 1000), 1))

			// Correct resampler state by resampling buffered data from API_fs_Hz to fs_kHz.
			ret += opus_int(silk_resampler(&psEnc.sCmn.resampler_state, x_bufFIX, x_buf_API_fs_Hz, api_buf_samples))

			// silk_short2float_array writes length-1..0.
			for k := new_buf_samples - 1; k >= 0; k-- {
				psEnc.x_buf[k] = silk_float(x_bufFIX[k])
			}
		}
	}

	psEnc.sCmn.prev_API_fs_Hz = psEnc.sCmn.API_fs_Hz

	return ret
}

// silk_setup_fs — C: control_codec.c:199-305.
func silk_setup_fs(psEnc *silk_encoder_state_FLP, fs_kHz opus_int, PacketSize_ms opus_int) opus_int {
	ret := opus_int(SILK_NO_ERROR)

	// Set packet size.
	if PacketSize_ms != psEnc.sCmn.PacketSize_ms {
		if PacketSize_ms != 10 && PacketSize_ms != 20 && PacketSize_ms != 40 && PacketSize_ms != 60 {
			ret = SILK_ENC_PACKET_SIZE_NOT_SUPPORTED
		}
		if PacketSize_ms <= 10 {
			psEnc.sCmn.nFramesPerPacket = 1
			if PacketSize_ms == 10 {
				psEnc.sCmn.nb_subfr = 2
			} else {
				psEnc.sCmn.nb_subfr = 1
			}
			psEnc.sCmn.frame_length = opus_int(silk_SMULBB(opus_int32(PacketSize_ms), opus_int32(fs_kHz)))
			psEnc.sCmn.pitch_LPC_win_length = opus_int(silk_SMULBB(opus_int32(FIND_PITCH_LPC_WIN_MS_2_SF), opus_int32(fs_kHz)))
			if psEnc.sCmn.fs_kHz == 8 {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:]
			} else {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:]
			}
		} else {
			psEnc.sCmn.nFramesPerPacket = opus_int(silk_DIV32_16(opus_int32(PacketSize_ms), opus_int32(MAX_FRAME_LENGTH_MS)))
			psEnc.sCmn.nb_subfr = MAX_NB_SUBFR
			psEnc.sCmn.frame_length = opus_int(silk_SMULBB(20, opus_int32(fs_kHz)))
			psEnc.sCmn.pitch_LPC_win_length = opus_int(silk_SMULBB(opus_int32(FIND_PITCH_LPC_WIN_MS), opus_int32(fs_kHz)))
			if psEnc.sCmn.fs_kHz == 8 {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:]
			} else {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_iCDF[:]
			}
		}
		psEnc.sCmn.PacketSize_ms = PacketSize_ms
		psEnc.sCmn.TargetRate_bps = 0 // trigger new SNR computation
	}

	// Set internal sampling frequency.
	if psEnc.sCmn.fs_kHz != fs_kHz {
		// Reset part of the state.
		psEnc.sShape = silk_shape_state_FLP{}
		psEnc.sCmn.sNSQ = silk_nsq_state{}
		for i := range psEnc.sCmn.prev_NLSFq_Q15 {
			psEnc.sCmn.prev_NLSFq_Q15[i] = 0
		}
		psEnc.sCmn.sLP.In_LP_State[0] = 0
		psEnc.sCmn.sLP.In_LP_State[1] = 0
		psEnc.sCmn.inputBufIx = 0
		psEnc.sCmn.nFramesEncoded = 0
		psEnc.sCmn.TargetRate_bps = 0 // trigger new SNR computation

		// Initialize non-zero parameters.
		psEnc.sCmn.prevLag = 100
		psEnc.sCmn.first_frame_after_reset = 1
		psEnc.sShape.LastGainIndex = 10
		psEnc.sCmn.sNSQ.lagPrev = 100
		psEnc.sCmn.sNSQ.prev_gain_Q16 = 65536
		psEnc.sCmn.prevSignalType = TYPE_NO_VOICE_ACTIVITY

		psEnc.sCmn.fs_kHz = fs_kHz
		if psEnc.sCmn.fs_kHz == 8 {
			if psEnc.sCmn.nb_subfr == MAX_NB_SUBFR {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:]
			} else {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:]
			}
		} else {
			if psEnc.sCmn.nb_subfr == MAX_NB_SUBFR {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_iCDF[:]
			} else {
				psEnc.sCmn.pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:]
			}
		}
		if psEnc.sCmn.fs_kHz == 8 || psEnc.sCmn.fs_kHz == 12 {
			psEnc.sCmn.predictLPCOrder = MIN_LPC_ORDER
			psEnc.sCmn.psNLSF_CB = &silk_NLSF_CB_NB_MB
		} else {
			psEnc.sCmn.predictLPCOrder = MAX_LPC_ORDER
			psEnc.sCmn.psNLSF_CB = &silk_NLSF_CB_WB
		}
		psEnc.sCmn.subfr_length = SUB_FRAME_LENGTH_MS * fs_kHz
		psEnc.sCmn.frame_length = opus_int(silk_SMULBB(opus_int32(psEnc.sCmn.subfr_length), opus_int32(psEnc.sCmn.nb_subfr)))
		psEnc.sCmn.ltp_mem_length = opus_int(silk_SMULBB(opus_int32(LTP_MEM_LENGTH_MS), opus_int32(fs_kHz)))
		psEnc.sCmn.la_pitch = opus_int(silk_SMULBB(opus_int32(LA_PITCH_MS), opus_int32(fs_kHz)))
		psEnc.sCmn.max_pitch_lag = opus_int(silk_SMULBB(18, opus_int32(fs_kHz)))
		if psEnc.sCmn.nb_subfr == MAX_NB_SUBFR {
			psEnc.sCmn.pitch_LPC_win_length = opus_int(silk_SMULBB(opus_int32(FIND_PITCH_LPC_WIN_MS), opus_int32(fs_kHz)))
		} else {
			psEnc.sCmn.pitch_LPC_win_length = opus_int(silk_SMULBB(opus_int32(FIND_PITCH_LPC_WIN_MS_2_SF), opus_int32(fs_kHz)))
		}
		if psEnc.sCmn.fs_kHz == 16 {
			psEnc.sCmn.pitch_lag_low_bits_iCDF = silk_uniform8_iCDF[:]
		} else if psEnc.sCmn.fs_kHz == 12 {
			psEnc.sCmn.pitch_lag_low_bits_iCDF = silk_uniform6_iCDF[:]
		} else {
			psEnc.sCmn.pitch_lag_low_bits_iCDF = silk_uniform4_iCDF[:]
		}
	}

	return ret
}

// silk_setup_complexity — C: control_codec.c:307-401.
func silk_setup_complexity(psEncC *silk_encoder_state, Complexity opus_int) opus_int {
	ret := opus_int(0)

	if Complexity < 1 {
		psEncC.pitchEstimationComplexity = SILK_PE_MIN_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.8, 16)
		psEncC.pitchEstimationLPCOrder = 6
		psEncC.shapingLPCOrder = 12
		psEncC.la_shape = 3 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 1
		psEncC.useInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 2
		psEncC.warping_Q16 = 0
	} else if Complexity < 2 {
		psEncC.pitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.76, 16)
		psEncC.pitchEstimationLPCOrder = 8
		psEncC.shapingLPCOrder = 14
		psEncC.la_shape = 5 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 1
		psEncC.useInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 3
		psEncC.warping_Q16 = 0
	} else if Complexity < 3 {
		psEncC.pitchEstimationComplexity = SILK_PE_MIN_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.8, 16)
		psEncC.pitchEstimationLPCOrder = 6
		psEncC.shapingLPCOrder = 12
		psEncC.la_shape = 3 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 2
		psEncC.warping_Q16 = 0
	} else if Complexity < 4 {
		psEncC.pitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.76, 16)
		psEncC.pitchEstimationLPCOrder = 8
		psEncC.shapingLPCOrder = 14
		psEncC.la_shape = 5 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 0
		psEncC.NLSF_MSVQ_Survivors = 4
		psEncC.warping_Q16 = 0
	} else if Complexity < 6 {
		psEncC.pitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.74, 16)
		psEncC.pitchEstimationLPCOrder = 10
		psEncC.shapingLPCOrder = 16
		psEncC.la_shape = 5 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 2
		psEncC.useInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 6
		psEncC.warping_Q16 = psEncC.fs_kHz * opus_int(SILK_FIX_CONST(WARPING_MULTIPLIER, 16))
	} else if Complexity < 8 {
		psEncC.pitchEstimationComplexity = SILK_PE_MID_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.72, 16)
		psEncC.pitchEstimationLPCOrder = 12
		psEncC.shapingLPCOrder = 20
		psEncC.la_shape = 5 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = 3
		psEncC.useInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 8
		psEncC.warping_Q16 = psEncC.fs_kHz * opus_int(SILK_FIX_CONST(WARPING_MULTIPLIER, 16))
	} else {
		psEncC.pitchEstimationComplexity = SILK_PE_MAX_COMPLEX
		psEncC.pitchEstimationThreshold_Q16 = SILK_FIX_CONST(0.7, 16)
		psEncC.pitchEstimationLPCOrder = 16
		psEncC.shapingLPCOrder = 24
		psEncC.la_shape = 5 * psEncC.fs_kHz
		psEncC.nStatesDelayedDecision = MAX_DEL_DEC_STATES
		psEncC.useInterpolatedNLSFs = 1
		psEncC.NLSF_MSVQ_Survivors = 16
		psEncC.warping_Q16 = psEncC.fs_kHz * opus_int(SILK_FIX_CONST(WARPING_MULTIPLIER, 16))
	}

	// Do not allow higher pitch estimation LPC order than predict LPC order.
	psEncC.pitchEstimationLPCOrder = silk_min_int(psEncC.pitchEstimationLPCOrder, psEncC.predictLPCOrder)
	psEncC.shapeWinLength = SUB_FRAME_LENGTH_MS*psEncC.fs_kHz + 2*psEncC.la_shape
	psEncC.Complexity = Complexity

	return ret
}

// silk_setup_LBRR — C: control_codec.c:403-423.
func silk_setup_LBRR(psEncC *silk_encoder_state, encControl *silk_EncControlStruct) opus_int {
	ret := opus_int(SILK_NO_ERROR)

	LBRR_in_previous_packet := psEncC.LBRR_enabled
	psEncC.LBRR_enabled = encControl.LBRR_coded
	if psEncC.LBRR_enabled != 0 {
		// Set gain increase for coding LBRR excitation.
		if LBRR_in_previous_packet == 0 {
			psEncC.LBRR_GainIncreases = 7
		} else {
			psEncC.LBRR_GainIncreases = silk_max_int(7-opus_int(silk_SMULWB(opus_int32(psEncC.PacketLoss_perc), SILK_FIX_CONST(0.2, 16))), 3)
		}
	}

	return ret
}
