package nativeopus

// Port of libopus/silk/float/encode_frame_FLP.c.
//
// Implements the top-level per-frame float-path SILK encoder entry
// points:
//   silk_encode_do_VAD_FLP — voice-activity detection + DTX logic
//   silk_encode_frame_FLP  — one SILK frame: pitch, shape, LPC/LTP,
//                            gains, NSQ, entropy coding with bitrate
//                            inner loop
//   silk_LBRR_encode_FLP   — Low-Bitrate-Redundancy shadow encode
//
// The bitrate-control inner loop copies and restores the range coder
// state (ec_enc) and NSQ state on each iteration. We mirror the C
// silk_memcpy behaviour by assigning the structs by value; the ec_enc
// `buf` slice header is captured by value, which preserves the
// C invariant that psRangeEnc->buf continues to point at the caller's
// byte buffer across restores. When the C code explicitly saves the
// bytes in ec_buf_copy and later restores them via silk_memcpy into
// psRangeEnc->buf, we do the same with an intermediate []byte slice.

// silk_encode_do_VAD_FLP — C: encode_frame_FLP.c:45-80.
func silk_encode_do_VAD_FLP(
	psEnc *silk_encoder_state_FLP,
	activity opus_int,
) {
	activity_threshold := SILK_FIX_CONST(SPEECH_ACTIVITY_DTX_THRES, 8)

	// Voice Activity Detection.
	silk_VAD_GetSA_Q8_c(&psEnc.sCmn, psEnc.sCmn.inputBuf[1:])

	// If Opus VAD is inactive and Silk VAD is active: lower Silk VAD
	// to just under the threshold.
	if activity == VAD_NO_ACTIVITY && opus_int32(psEnc.sCmn.speech_activity_Q8) >= activity_threshold {
		psEnc.sCmn.speech_activity_Q8 = opus_int(activity_threshold - 1)
	}

	// Convert speech activity into VAD and DTX flags.
	if opus_int32(psEnc.sCmn.speech_activity_Q8) < activity_threshold {
		psEnc.sCmn.indices.signalType = TYPE_NO_VOICE_ACTIVITY
		psEnc.sCmn.noSpeechCounter++
		if psEnc.sCmn.noSpeechCounter <= NB_SPEECH_FRAMES_BEFORE_DTX {
			psEnc.sCmn.inDTX = 0
		} else if psEnc.sCmn.noSpeechCounter > MAX_CONSECUTIVE_DTX+NB_SPEECH_FRAMES_BEFORE_DTX {
			psEnc.sCmn.noSpeechCounter = NB_SPEECH_FRAMES_BEFORE_DTX
			psEnc.sCmn.inDTX = 0
		}
		psEnc.sCmn.VAD_flags[psEnc.sCmn.nFramesEncoded] = 0
	} else {
		psEnc.sCmn.noSpeechCounter = 0
		psEnc.sCmn.inDTX = 0
		psEnc.sCmn.indices.signalType = TYPE_UNVOICED
		psEnc.sCmn.VAD_flags[psEnc.sCmn.nFramesEncoded] = 1
	}
}

// silk_encode_frame_FLP — C: encode_frame_FLP.c:85-385.
func silk_encode_frame_FLP(
	psEnc *silk_encoder_state_FLP,
	pnBytesOut *opus_int32,
	psRangeEnc *ec_enc,
	condCoding opus_int,
	maxBits opus_int,
	useCBR opus_int,
) opus_int {
	var sEncCtrl silk_encoder_control_FLP
	var i, iter, maxIter, found_upper, found_lower opus_int
	ret := opus_int(0)
	var res_pitch [2*MAX_FRAME_LENGTH + LA_PITCH_MAX]silk_float
	var sRangeEnc_copy, sRangeEnc_copy2 ec_enc
	// VARDECL(silk_nsq_state, sNSQ_copy) -> size 2.
	var sNSQ_copy [2]silk_nsq_state
	var seed_copy, nBits, nBits_lower, nBits_upper, gainMult_lower, gainMult_upper opus_int32
	var gainsID, gainsID_lower, gainsID_upper opus_int32
	var gainMult_Q8 opus_int16
	var ec_prevLagIndex_copy opus_int16
	var ec_prevSignalType_copy opus_int
	var LastGainIndex_copy2 opus_int8
	var pGains_Q16 [MAX_NB_SUBFR]opus_int32
	var gain_lock [MAX_NB_SUBFR]opus_int
	var best_gain_mult [MAX_NB_SUBFR]opus_int16
	var best_sum [MAX_NB_SUBFR]opus_int
	var bits_margin opus_int

	// For CBR, 5 bits below budget is close enough. For VBR, allow
	// up to 25% below the cap if we initially busted the budget.
	if useCBR != 0 {
		bits_margin = 5
	} else {
		bits_margin = maxBits / 4
	}
	// This is totally unnecessary but many compilers ...
	LastGainIndex_copy2 = 0
	nBits_lower = 0
	nBits_upper = 0
	gainMult_lower = 0
	gainMult_upper = 0

	psEnc.sCmn.indices.Seed = opus_int8(psEnc.sCmn.frameCounter & 3)
	psEnc.sCmn.frameCounter++

	// Set up Input Pointers, and insert frame in input buffer.
	// pointers aligned with start of frame to encode.
	x_frame_off := psEnc.sCmn.ltp_mem_length         // index into psEnc.x_buf
	res_pitch_frame_off := psEnc.sCmn.ltp_mem_length // index into res_pitch

	// Ensure smooth bandwidth transitions.
	silk_LP_variable_cutoff(&psEnc.sCmn.sLP, psEnc.sCmn.inputBuf[1:], psEnc.sCmn.frame_length)

	// Copy new frame to front of input buffer.
	silk_short2float_array(
		psEnc.x_buf[x_frame_off+LA_SHAPE_MS*psEnc.sCmn.fs_kHz:],
		psEnc.sCmn.inputBuf[1:],
		opus_int32(psEnc.sCmn.frame_length))

	// Add tiny signal to avoid high CPU load from denormalized
	// floating point numbers.
	for i = 0; i < 8; i++ {
		psEnc.x_buf[x_frame_off+LA_SHAPE_MS*psEnc.sCmn.fs_kHz+i*(psEnc.sCmn.frame_length>>3)] =
			fma_add(
				psEnc.x_buf[x_frame_off+LA_SHAPE_MS*psEnc.sCmn.fs_kHz+i*(psEnc.sCmn.frame_length>>3)],
				silk_float(1-(i&2)), 1e-6)
	}

	if psEnc.sCmn.prefillFlag == 0 {
		var ec_buf_copy [1275]opus_uint8

		// Find pitch lags, initial LPC analysis. C passes x = x_frame
		// (psEnc->x_buf + ltp_mem_length). The callee walks backward
		// by ltp_mem_length into the LTP history, so we must pass the
		// full x_buf slice with the explicit offset via _withBase.
		silk_find_pitch_lags_FLP_withBase(psEnc, &sEncCtrl, res_pitch[:], psEnc.x_buf[:], x_frame_off, psEnc.sCmn.arch)

		// Noise shape analysis. Callee accesses x - la_shape, so pass
		// the full backing slice with offset.
		silk_noise_shape_analysis_FLP_withBase(psEnc, &sEncCtrl, res_pitch[res_pitch_frame_off:], psEnc.x_buf[:], x_frame_off)

		// Find linear prediction coefficients (LPC + LTP). Both
		// resPitch and x need explicit offsets for the -predictLPCOrder
		// and -ltp_mem_length indexing that happens inside.
		silk_find_pred_coefs_FLP(psEnc, &sEncCtrl, res_pitch[:], res_pitch_frame_off, psEnc.x_buf[:], x_frame_off, condCoding)

		// Process gains.
		silk_process_gains_FLP(psEnc, &sEncCtrl, condCoding)

		// Low Bitrate Redundant Encoding.
		silk_LBRR_encode_FLP(psEnc, &sEncCtrl, psEnc.x_buf[x_frame_off:], condCoding)

		// Loop over quantizer and entropy coding to control bitrate.
		maxIter = 6
		gainMult_Q8 = opus_int16(SILK_FIX_CONST(1, 8))
		found_lower = 0
		found_upper = 0
		gainsID = silk_gains_ID(psEnc.sCmn.indices.GainsIndices[:], psEnc.sCmn.nb_subfr)
		gainsID_lower = -1
		gainsID_upper = -1
		// Copy part of the input state.
		sRangeEnc_copy = *psRangeEnc
		sNSQ_copy[0] = psEnc.sCmn.sNSQ
		seed_copy = opus_int32(psEnc.sCmn.indices.Seed)
		ec_prevLagIndex_copy = psEnc.sCmn.ec_prevLagIndex
		ec_prevSignalType_copy = psEnc.sCmn.ec_prevSignalType
		// ec_buf_copy is a fixed 1275-byte buffer allocated above.
		for iter = 0; ; iter++ {
			if gainsID == gainsID_lower {
				nBits = nBits_lower
			} else if gainsID == gainsID_upper {
				nBits = nBits_upper
			} else {
				// Restore part of the input state.
				if iter > 0 {
					*psRangeEnc = sRangeEnc_copy
					psEnc.sCmn.sNSQ = sNSQ_copy[0]
					psEnc.sCmn.indices.Seed = opus_int8(seed_copy)
					psEnc.sCmn.ec_prevLagIndex = ec_prevLagIndex_copy
					psEnc.sCmn.ec_prevSignalType = ec_prevSignalType_copy
				}

				// Noise shaping quantization.
				silk_NSQ_wrapper_FLP(psEnc, &sEncCtrl, &psEnc.sCmn.indices, &psEnc.sCmn.sNSQ,
					psEnc.sCmn.pulses[:], psEnc.x_buf[x_frame_off:])

				if iter == maxIter && found_lower == 0 {
					sRangeEnc_copy2 = *psRangeEnc
				}

				// Encode Parameters.
				silk_encode_indices(&psEnc.sCmn, psRangeEnc, psEnc.sCmn.nFramesEncoded, 0, condCoding)

				// Encode Excitation Signal.
				silk_encode_pulses(psRangeEnc, opus_int(psEnc.sCmn.indices.signalType), opus_int(psEnc.sCmn.indices.quantOffsetType),
					psEnc.sCmn.pulses[:], psEnc.sCmn.frame_length)

				nBits = opus_int32(ec_tell(psRangeEnc))

				// If we still bust after the last iteration, do some damage control.
				if iter == maxIter && found_lower == 0 && nBits > opus_int32(maxBits) {
					*psRangeEnc = sRangeEnc_copy2

					// Keep gains the same as the last frame.
					psEnc.sShape.LastGainIndex = sEncCtrl.lastGainIndexPrev
					for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
						psEnc.sCmn.indices.GainsIndices[i] = 4
					}
					if condCoding != CODE_CONDITIONALLY {
						psEnc.sCmn.indices.GainsIndices[0] = sEncCtrl.lastGainIndexPrev
					}
					psEnc.sCmn.ec_prevLagIndex = ec_prevLagIndex_copy
					psEnc.sCmn.ec_prevSignalType = ec_prevSignalType_copy
					// Clear all pulses.
					for i = 0; i < psEnc.sCmn.frame_length; i++ {
						psEnc.sCmn.pulses[i] = 0
					}

					silk_encode_indices(&psEnc.sCmn, psRangeEnc, psEnc.sCmn.nFramesEncoded, 0, condCoding)

					silk_encode_pulses(psRangeEnc, opus_int(psEnc.sCmn.indices.signalType), opus_int(psEnc.sCmn.indices.quantOffsetType),
						psEnc.sCmn.pulses[:], psEnc.sCmn.frame_length)

					nBits = opus_int32(ec_tell(psRangeEnc))
				}

				if useCBR == 0 && iter == 0 && nBits <= opus_int32(maxBits) {
					break
				}
			}

			if iter == maxIter {
				if found_lower != 0 && (gainsID == gainsID_lower || nBits > opus_int32(maxBits)) {
					// Restore output state from earlier iteration that did meet the bitrate budget.
					*psRangeEnc = sRangeEnc_copy2
					celt_assert(sRangeEnc_copy2.offs <= 1275)
					// silk_memcpy(psRangeEnc->buf, ec_buf_copy, sRangeEnc_copy2.offs)
					copy(psRangeEnc.buf[:sRangeEnc_copy2.offs], ec_buf_copy[:sRangeEnc_copy2.offs])
					psEnc.sCmn.sNSQ = sNSQ_copy[1]
					psEnc.sShape.LastGainIndex = LastGainIndex_copy2
				}
				break
			}

			if nBits > opus_int32(maxBits) {
				if found_lower == 0 && iter >= 2 {
					// Adjust the quantizer's rate/distortion tradeoff and discard previous "upper" results.
					sEncCtrl.Lambda = silk_max_float(mul_f32(sEncCtrl.Lambda, 1.5), 1.5)
					// Reducing dithering can help us hit the target.
					psEnc.sCmn.indices.quantOffsetType = 0
					found_upper = 0
					gainsID_upper = -1
				} else {
					found_upper = 1
					nBits_upper = nBits
					gainMult_upper = opus_int32(gainMult_Q8)
					gainsID_upper = gainsID
				}
			} else if nBits < opus_int32(maxBits-bits_margin) {
				found_lower = 1
				nBits_lower = nBits
				gainMult_lower = opus_int32(gainMult_Q8)
				if gainsID != gainsID_lower {
					gainsID_lower = gainsID
					// Copy part of the output state.
					sRangeEnc_copy2 = *psRangeEnc
					celt_assert(psRangeEnc.offs <= 1275)
					copy(ec_buf_copy[:psRangeEnc.offs], psRangeEnc.buf[:psRangeEnc.offs])
					sNSQ_copy[1] = psEnc.sCmn.sNSQ
					LastGainIndex_copy2 = psEnc.sShape.LastGainIndex
				}
			} else {
				// Close enough.
				break
			}

			if found_lower == 0 && nBits > opus_int32(maxBits) {
				var j opus_int
				for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
					sum := opus_int(0)
					for j = i * psEnc.sCmn.subfr_length; j < (i+1)*psEnc.sCmn.subfr_length; j++ {
						v := psEnc.sCmn.pulses[j]
						if v < 0 {
							sum += opus_int(-v)
						} else {
							sum += opus_int(v)
						}
					}
					if iter == 0 || (sum < best_sum[i] && gain_lock[i] == 0) {
						best_sum[i] = sum
						best_gain_mult[i] = gainMult_Q8
					} else {
						gain_lock[i] = 1
					}
				}
			}
			if (found_lower & found_upper) == 0 {
				// Adjust gain according to high-rate rate/distortion curve.
				if nBits > opus_int32(maxBits) {
					gainMult_Q8 = opus_int16(silk_min_32(1024, opus_int32(gainMult_Q8)*3/2))
				} else {
					gainMult_Q8 = opus_int16(silk_max_32(64, opus_int32(gainMult_Q8)*4/5))
				}
			} else {
				// Adjust gain by interpolating.
				gainMult_Q8 = opus_int16(gainMult_lower + ((gainMult_upper-gainMult_lower)*(opus_int32(maxBits)-nBits_lower))/(nBits_upper-nBits_lower))
				// New gain multiplier must be between 25% and 75% of old range
				// (note that gainMult_upper < gainMult_lower).
				if opus_int32(gainMult_Q8) > silk_ADD_RSHIFT32(gainMult_lower, gainMult_upper-gainMult_lower, 2) {
					gainMult_Q8 = opus_int16(silk_ADD_RSHIFT32(gainMult_lower, gainMult_upper-gainMult_lower, 2))
				} else if opus_int32(gainMult_Q8) < silk_SUB_RSHIFT32(gainMult_upper, gainMult_upper-gainMult_lower, 2) {
					gainMult_Q8 = opus_int16(silk_SUB_RSHIFT32(gainMult_upper, gainMult_upper-gainMult_lower, 2))
				}
			}

			for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
				var tmp opus_int16
				if gain_lock[i] != 0 {
					tmp = best_gain_mult[i]
				} else {
					tmp = gainMult_Q8
				}
				pGains_Q16[i] = silk_LSHIFT_SAT32(silk_SMULWB(sEncCtrl.GainsUnq_Q16[i], opus_int32(tmp)), 8)
			}

			// Quantize gains.
			psEnc.sShape.LastGainIndex = sEncCtrl.lastGainIndexPrev
			var condBool opus_int
			if condCoding == CODE_CONDITIONALLY {
				condBool = 1
			} else {
				condBool = 0
			}
			silk_gains_quant(psEnc.sCmn.indices.GainsIndices[:], pGains_Q16[:],
				&psEnc.sShape.LastGainIndex, condBool, psEnc.sCmn.nb_subfr)

			// Unique identifier of gains vector.
			gainsID = silk_gains_ID(psEnc.sCmn.indices.GainsIndices[:], psEnc.sCmn.nb_subfr)

			// Overwrite unquantized gains with quantized gains and convert back to Q0 from Q16.
			for i = 0; i < psEnc.sCmn.nb_subfr; i++ {
				sEncCtrl.Gains[i] = mul_f32(silk_float(pGains_Q16[i]), 1.0/65536.0)
			}
		}
	}

	// Update input buffer.
	// silk_memmove(psEnc->x_buf, &psEnc->x_buf[frame_length],
	//     (ltp_mem_length + LA_SHAPE_MS * fs_kHz) * sizeof(silk_float));
	moveLen := psEnc.sCmn.ltp_mem_length + LA_SHAPE_MS*psEnc.sCmn.fs_kHz
	copy(psEnc.x_buf[:moveLen], psEnc.x_buf[psEnc.sCmn.frame_length:psEnc.sCmn.frame_length+moveLen])

	// Exit without entropy coding.
	if psEnc.sCmn.prefillFlag != 0 {
		// No payload.
		*pnBytesOut = 0
		return ret
	}

	// Parameters needed for next frame.
	psEnc.sCmn.prevLag = sEncCtrl.pitchL[psEnc.sCmn.nb_subfr-1]
	psEnc.sCmn.prevSignalType = psEnc.sCmn.indices.signalType

	// Finalize payload.
	psEnc.sCmn.first_frame_after_reset = 0
	// Payload size.
	*pnBytesOut = silk_RSHIFT(opus_int32(ec_tell(psRangeEnc))+7, 3)

	return ret
}

// silk_LBRR_encode_FLP — C: encode_frame_FLP.c:388-447.
func silk_LBRR_encode_FLP(
	psEnc *silk_encoder_state_FLP,
	psEncCtrl *silk_encoder_control_FLP,
	xfw []silk_float,
	condCoding opus_int,
) {
	var k opus_int
	var Gains_Q16 [MAX_NB_SUBFR]opus_int32
	var TempGains [MAX_NB_SUBFR]silk_float
	psIndices_LBRR := &psEnc.sCmn.indices_LBRR[psEnc.sCmn.nFramesEncoded]
	var sNSQ_LBRR [1]silk_nsq_state

	// Control use of inband LBRR.
	if psEnc.sCmn.LBRR_enabled != 0 && opus_int32(psEnc.sCmn.speech_activity_Q8) > SILK_FIX_CONST(LBRR_SPEECH_ACTIVITY_THRES, 8) {
		psEnc.sCmn.LBRR_flags[psEnc.sCmn.nFramesEncoded] = 1

		// Copy noise shaping quantizer state and quantization indices from regular encoding.
		sNSQ_LBRR[0] = psEnc.sCmn.sNSQ
		*psIndices_LBRR = psEnc.sCmn.indices

		// Save original gains.
		for i := opus_int(0); i < psEnc.sCmn.nb_subfr; i++ {
			TempGains[i] = psEncCtrl.Gains[i]
		}

		if psEnc.sCmn.nFramesEncoded == 0 || psEnc.sCmn.LBRR_flags[psEnc.sCmn.nFramesEncoded-1] == 0 {
			// First frame in packet or previous frame not LBRR coded.
			psEnc.sCmn.LBRRprevLastGainIndex = psEnc.sShape.LastGainIndex

			// Increase Gains to get target LBRR rate.
			psIndices_LBRR.GainsIndices[0] += opus_int8(psEnc.sCmn.LBRR_GainIncreases)
			psIndices_LBRR.GainsIndices[0] = opus_int8(silk_min_int(opus_int(psIndices_LBRR.GainsIndices[0]), N_LEVELS_QGAIN-1))
		}

		// Decode to get gains in sync with decoder.
		var condBool opus_int
		if condCoding == CODE_CONDITIONALLY {
			condBool = 1
		} else {
			condBool = 0
		}
		silk_gains_dequant(Gains_Q16[:], psIndices_LBRR.GainsIndices[:],
			&psEnc.sCmn.LBRRprevLastGainIndex, condBool, psEnc.sCmn.nb_subfr)

		// Overwrite unquantized gains with quantized gains and convert back to Q0 from Q16.
		for k = 0; k < psEnc.sCmn.nb_subfr; k++ {
			psEncCtrl.Gains[k] = mul_f32(silk_float(Gains_Q16[k]), 1.0/65536.0)
		}

		// Noise shaping quantization.
		silk_NSQ_wrapper_FLP(psEnc, psEncCtrl, psIndices_LBRR, &sNSQ_LBRR[0],
			psEnc.sCmn.pulses_LBRR[psEnc.sCmn.nFramesEncoded][:], xfw)

		// Restore original gains.
		for i := opus_int(0); i < psEnc.sCmn.nb_subfr; i++ {
			psEncCtrl.Gains[i] = TempGains[i]
		}
	}
}
