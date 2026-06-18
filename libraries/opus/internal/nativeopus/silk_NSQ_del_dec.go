package nativeopus

// Port of libopus/silk/NSQ_del_dec.c.
//
// Delayed-decision noise-shaping quantizer with static helpers
// silk_nsq_del_dec_scale_states and silk_noise_shape_quantizer_del_dec.

// NSQ_del_dec_struct — C: NSQ_del_dec.c:37-50.
// All fields are opus_int32 in the C struct; their concatenation is
// treated as a flat int32 array at line 605-606 for a memcpy that
// slides in from index i.
type NSQ_del_dec_struct struct {
	sLPC_Q14  [MAX_SUB_FRAME_LENGTH + NSQ_LPC_BUF_LENGTH]opus_int32
	RandState [DECISION_DELAY]opus_int32
	Q_Q10     [DECISION_DELAY]opus_int32
	Xq_Q14    [DECISION_DELAY]opus_int32
	Pred_Q15  [DECISION_DELAY]opus_int32
	Shape_Q14 [DECISION_DELAY]opus_int32
	sAR2_Q14  [MAX_SHAPE_LPC_ORDER]opus_int32
	LF_AR_Q14 opus_int32
	Diff_Q14  opus_int32
	Seed      opus_int32
	SeedInit  opus_int32
	RD_Q10    opus_int32
}

// nsq_del_dec_int32_count returns the number of opus_int32 slots in a
// NSQ_del_dec_struct when viewed as a flat int32 array.
const nsq_del_dec_int32_count = (MAX_SUB_FRAME_LENGTH + NSQ_LPC_BUF_LENGTH) +
	DECISION_DELAY + DECISION_DELAY + DECISION_DELAY + DECISION_DELAY + DECISION_DELAY +
	MAX_SHAPE_LPC_ORDER + 5

// nsq_del_dec_as_flat copies the struct's fields into a flat int32 array
// in the same field order as the C layout.
func nsq_del_dec_as_flat(dst *[nsq_del_dec_int32_count]opus_int32, src *NSQ_del_dec_struct) {
	off := 0
	copy(dst[off:], src.sLPC_Q14[:])
	off += len(src.sLPC_Q14)
	copy(dst[off:], src.RandState[:])
	off += len(src.RandState)
	copy(dst[off:], src.Q_Q10[:])
	off += len(src.Q_Q10)
	copy(dst[off:], src.Xq_Q14[:])
	off += len(src.Xq_Q14)
	copy(dst[off:], src.Pred_Q15[:])
	off += len(src.Pred_Q15)
	copy(dst[off:], src.Shape_Q14[:])
	off += len(src.Shape_Q14)
	copy(dst[off:], src.sAR2_Q14[:])
	off += len(src.sAR2_Q14)
	dst[off] = src.LF_AR_Q14
	off++
	dst[off] = src.Diff_Q14
	off++
	dst[off] = src.Seed
	off++
	dst[off] = src.SeedInit
	off++
	dst[off] = src.RD_Q10
}

// nsq_del_dec_from_flat is the inverse of nsq_del_dec_as_flat.
func nsq_del_dec_from_flat(dst *NSQ_del_dec_struct, src *[nsq_del_dec_int32_count]opus_int32) {
	off := 0
	copy(dst.sLPC_Q14[:], src[off:])
	off += len(dst.sLPC_Q14)
	copy(dst.RandState[:], src[off:])
	off += len(dst.RandState)
	copy(dst.Q_Q10[:], src[off:])
	off += len(dst.Q_Q10)
	copy(dst.Xq_Q14[:], src[off:])
	off += len(dst.Xq_Q14)
	copy(dst.Pred_Q15[:], src[off:])
	off += len(dst.Pred_Q15)
	copy(dst.Shape_Q14[:], src[off:])
	off += len(dst.Shape_Q14)
	copy(dst.sAR2_Q14[:], src[off:])
	off += len(dst.sAR2_Q14)
	dst.LF_AR_Q14 = src[off]
	off++
	dst.Diff_Q14 = src[off]
	off++
	dst.Seed = src[off]
	off++
	dst.SeedInit = src[off]
	off++
	dst.RD_Q10 = src[off]
}

// nsq_del_dec_copy_tail mirrors the C-level pointer-arithmetic memcpy:
//
//	silk_memcpy( ((opus_int32*)&dst) + i,
//	             ((opus_int32*)&src) + i,
//	             sizeof(NSQ_del_dec_struct) - i * sizeof(opus_int32) );
//
// That is, copies every int32 from flat-index i onward.
func nsq_del_dec_copy_tail(dst, src *NSQ_del_dec_struct, i opus_int) {
	var dstFlat, srcFlat [nsq_del_dec_int32_count]opus_int32
	nsq_del_dec_as_flat(&dstFlat, dst)
	nsq_del_dec_as_flat(&srcFlat, src)
	copy(dstFlat[i:], srcFlat[i:])
	nsq_del_dec_from_flat(dst, &dstFlat)
}

// NSQ_sample_struct — C: NSQ_del_dec.c:52-60.
type NSQ_sample_struct struct {
	Q_Q10        opus_int32
	RD_Q10       opus_int32
	xq_Q14       opus_int32
	LF_AR_Q14    opus_int32
	Diff_Q14     opus_int32
	sLTP_shp_Q14 opus_int32
	LPC_exc_Q14  opus_int32
}

// silk_NSQ_del_dec_c — main entry. C: NSQ_del_dec.c:114-309.
func silk_NSQ_del_dec_c(
	psEncC *silk_encoder_state,
	NSQ *silk_nsq_state,
	psIndices *SideInfoIndices,
	x16 []opus_int16,
	pulses []opus_int8,
	PredCoef_Q12 []opus_int16,
	LTPCoef_Q14 []opus_int16,
	AR_Q13 []opus_int16,
	HarmShapeGain_Q14 []opus_int,
	Tilt_Q14 []opus_int,
	LF_shp_Q14 []opus_int32,
	Gains_Q16 []opus_int32,
	pitchL []opus_int,
	Lambda_Q10 opus_int,
	LTP_scale_Q14 opus_int,
) {
	// Set unvoiced lag to the previous one, overwrite later for voiced.
	lag := NSQ.lagPrev

	// Initialize delayed decision states. Fixed-size stack array — cost
	// of `make` was visible in 22-allocs-per-SILK-encode before. State
	// count caps at MAX_DEL_DEC_STATES=4; the slice views the leading
	// nStatesDelayedDecision entries.
	var psDelDecArr [MAX_DEL_DEC_STATES]NSQ_del_dec_struct
	psDelDec := psDelDecArr[:psEncC.nStatesDelayedDecision]
	for k := opus_int(0); k < psEncC.nStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		psDD.Seed = (opus_int32(k) + opus_int32(psIndices.Seed)) & 3
		psDD.SeedInit = psDD.Seed
		psDD.RD_Q10 = 0
		psDD.LF_AR_Q14 = NSQ.sLF_AR_shp_Q14
		psDD.Diff_Q14 = NSQ.sDiff_shp_Q14
		psDD.Shape_Q14[0] = NSQ.sLTP_shp_Q14[psEncC.ltp_mem_length-1]
		copy(psDD.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], NSQ.sLPC_Q14[:NSQ_LPC_BUF_LENGTH])
		psDD.sAR2_Q14 = NSQ.sAR2_Q14
	}

	offset_Q10 := opus_int32(silk_Quantization_Offsets_Q10[psIndices.signalType>>1][psIndices.quantOffsetType])
	smpl_buf_idx := opus_int(0) // index of oldest samples

	decisionDelay := silk_min_int(DECISION_DELAY, psEncC.subfr_length)

	// For voiced frames limit the decision delay to lower than the pitch lag.
	if opus_int(psIndices.signalType) == TYPE_VOICED {
		for k := opus_int(0); k < psEncC.nb_subfr; k++ {
			decisionDelay = silk_min_int(decisionDelay, pitchL[k]-LTP_ORDER/2-1)
		}
	} else {
		if lag > 0 {
			decisionDelay = silk_min_int(decisionDelay, lag-LTP_ORDER/2-1)
		}
	}

	var LSF_interpolation_flag opus_int
	if psIndices.NLSFInterpCoef_Q2 == 4 {
		LSF_interpolation_flag = 0
	} else {
		LSF_interpolation_flag = 1
	}

	// Stack-allocate the scratch buffers: ltp_mem_length+frame_length is
	// bounded by MAX_FRAME_LENGTH on each side, and subfr_length is
	// bounded by MAX_SUB_FRAME_LENGTH. Escape analysis keeps these on
	// the goroutine stack for typical call sites. Previously this path
	// accounted for ~4 heap allocs per SILK encode.
	var sLTP_Q15_buf [MAX_FRAME_LENGTH + MAX_FRAME_LENGTH]opus_int32
	var sLTP_buf [MAX_FRAME_LENGTH + MAX_FRAME_LENGTH]opus_int16
	var x_sc_Q10_buf [MAX_SUB_FRAME_LENGTH]opus_int32
	var delayedGain_Q10_buf [DECISION_DELAY]opus_int32
	sLTP_Q15 := sLTP_Q15_buf[:psEncC.ltp_mem_length+psEncC.frame_length]
	sLTP := sLTP_buf[:psEncC.ltp_mem_length+psEncC.frame_length]
	x_sc_Q10 := x_sc_Q10_buf[:psEncC.subfr_length]
	delayedGain_Q10 := delayedGain_Q10_buf[:]

	// Set up pointers to start of sub frame.
	pxqOff := psEncC.ltp_mem_length
	NSQ.sLTP_shp_buf_idx = psEncC.ltp_mem_length
	NSQ.sLTP_buf_idx = psEncC.ltp_mem_length
	subfr := opus_int(0)
	x16Off := opus_int(0)
	pulsesOff := opus_int(0)

	var Winner_ind, last_smple_idx opus_int
	var RDmin_Q10 opus_int32

	for k := opus_int(0); k < psEncC.nb_subfr; k++ {
		A_Q12 := PredCoef_Q12[((k>>1)|(1-LSF_interpolation_flag))*MAX_LPC_ORDER:]
		B_Q14 := LTPCoef_Q14[k*LTP_ORDER:]
		AR_shp_Q13 := AR_Q13[k*MAX_SHAPE_LPC_ORDER:]

		// Noise shape parameters.
		HarmShapeFIRPacked_Q14 := silk_RSHIFT(opus_int32(HarmShapeGain_Q14[k]), 2)
		HarmShapeFIRPacked_Q14 |= silk_LSHIFT(opus_int32(silk_RSHIFT(opus_int32(HarmShapeGain_Q14[k]), 1)), 16)

		NSQ.rewhite_flag = 0
		if opus_int(psIndices.signalType) == TYPE_VOICED {
			lag = pitchL[k]

			// Re-whitening.
			if (k & (3 - (LSF_interpolation_flag << 1))) == 0 {
				if k == 2 {
					// RESET DELAYED DECISIONS.
					// Find winner.
					RDmin_Q10 = psDelDec[0].RD_Q10
					Winner_ind = 0
					for i := opus_int(1); i < psEncC.nStatesDelayedDecision; i++ {
						if psDelDec[i].RD_Q10 < RDmin_Q10 {
							RDmin_Q10 = psDelDec[i].RD_Q10
							Winner_ind = i
						}
					}
					for i := opus_int(0); i < psEncC.nStatesDelayedDecision; i++ {
						if i != Winner_ind {
							psDelDec[i].RD_Q10 += silk_int32_MAX >> 4
						}
					}

					// Copy final part of signals from winner state to output and long-term filter states.
					// pulsesOff/pxqOff are already past the prior subframes, so
					// +i-decisionDelay reaches back into them (decisionDelay <= subfr_length).
					psDD := &psDelDec[Winner_ind]
					last_smple_idx = smpl_buf_idx + decisionDelay
					for i := opus_int(0); i < decisionDelay; i++ {
						last_smple_idx = (last_smple_idx - 1) % DECISION_DELAY
						if last_smple_idx < 0 {
							last_smple_idx += DECISION_DELAY
						}
						pulses[pulsesOff+i-decisionDelay] = opus_int8(silk_RSHIFT_ROUND(psDD.Q_Q10[last_smple_idx], 10))
						NSQ.xq[pxqOff+i-decisionDelay] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(
							silk_SMULWW(psDD.Xq_Q14[last_smple_idx], Gains_Q16[1]), 14)))
						NSQ.sLTP_shp_Q14[NSQ.sLTP_shp_buf_idx-decisionDelay+i] = psDD.Shape_Q14[last_smple_idx]
					}

					subfr = 0
				}

				// Rewhiten with new A coefs.
				start_idx := psEncC.ltp_mem_length - lag - psEncC.predictLPCOrder - LTP_ORDER/2

				silk_LPC_analysis_filter(sLTP[start_idx:], NSQ.xq[start_idx+k*psEncC.subfr_length:],
					A_Q12, opus_int32(psEncC.ltp_mem_length-start_idx), opus_int32(psEncC.predictLPCOrder), psEncC.arch)

				NSQ.sLTP_buf_idx = psEncC.ltp_mem_length
				NSQ.rewhite_flag = 1
			}
		}

		silk_nsq_del_dec_scale_states(psEncC, NSQ, psDelDec, x16[x16Off:], x_sc_Q10, sLTP, sLTP_Q15, k,
			psEncC.nStatesDelayedDecision, LTP_scale_Q14, Gains_Q16, pitchL, opus_int(psIndices.signalType), decisionDelay)

		if !testingForceScalarNSQDelDec && nsqSIMDAvailable && psEncC.nStatesDelayedDecision == MAX_DEL_DEC_STATES {
			silk_noise_shape_quantizer_del_dec_soa(NSQ, psDelDec, opus_int(psIndices.signalType), x_sc_Q10,
				pulses, pulsesOff, NSQ.xq[:], pxqOff, sLTP_Q15,
				delayedGain_Q10, A_Q12, B_Q14, AR_shp_Q13, lag, HarmShapeFIRPacked_Q14, Tilt_Q14[k], LF_shp_Q14[k],
				Gains_Q16[k], Lambda_Q10, offset_Q10, psEncC.subfr_length, subfr, psEncC.shapingLPCOrder,
				psEncC.predictLPCOrder, opus_int32(psEncC.warping_Q16), psEncC.nStatesDelayedDecision, &smpl_buf_idx, decisionDelay, psEncC.arch)
		} else {
			silk_noise_shape_quantizer_del_dec(NSQ, psDelDec, opus_int(psIndices.signalType), x_sc_Q10,
				pulses, pulsesOff, NSQ.xq[:], pxqOff, sLTP_Q15,
				delayedGain_Q10, A_Q12, B_Q14, AR_shp_Q13, lag, HarmShapeFIRPacked_Q14, Tilt_Q14[k], LF_shp_Q14[k],
				Gains_Q16[k], Lambda_Q10, offset_Q10, psEncC.subfr_length, subfr, psEncC.shapingLPCOrder,
				psEncC.predictLPCOrder, opus_int32(psEncC.warping_Q16), psEncC.nStatesDelayedDecision, &smpl_buf_idx, decisionDelay, psEncC.arch)
		}
		subfr++

		x16Off += psEncC.subfr_length
		pulsesOff += psEncC.subfr_length
		pxqOff += psEncC.subfr_length
	}

	// Find winner.
	RDmin_Q10 = psDelDec[0].RD_Q10
	Winner_ind = 0
	for k := opus_int(1); k < psEncC.nStatesDelayedDecision; k++ {
		if psDelDec[k].RD_Q10 < RDmin_Q10 {
			RDmin_Q10 = psDelDec[k].RD_Q10
			Winner_ind = k
		}
	}

	// Copy final part of signals from winner state to output and long-term filter states.
	psDD := &psDelDec[Winner_ind]
	psIndices.Seed = opus_int8(psDD.SeedInit)
	last_smple_idx = smpl_buf_idx + decisionDelay
	Gain_Q10 := silk_RSHIFT32(Gains_Q16[psEncC.nb_subfr-1], 6)
	for i := opus_int(0); i < decisionDelay; i++ {
		last_smple_idx = (last_smple_idx - 1) % DECISION_DELAY
		if last_smple_idx < 0 {
			last_smple_idx += DECISION_DELAY
		}

		pulses[pulsesOff+i-decisionDelay] = opus_int8(silk_RSHIFT_ROUND(psDD.Q_Q10[last_smple_idx], 10))
		NSQ.xq[pxqOff+i-decisionDelay] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(
			silk_SMULWW(psDD.Xq_Q14[last_smple_idx], Gain_Q10), 8)))
		NSQ.sLTP_shp_Q14[NSQ.sLTP_shp_buf_idx-decisionDelay+i] = psDD.Shape_Q14[last_smple_idx]
	}
	copy(NSQ.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], psDD.sLPC_Q14[psEncC.subfr_length:psEncC.subfr_length+NSQ_LPC_BUF_LENGTH])
	NSQ.sAR2_Q14 = psDD.sAR2_Q14

	// Update states.
	NSQ.sLF_AR_shp_Q14 = psDD.LF_AR_Q14
	NSQ.sDiff_shp_Q14 = psDD.Diff_Q14
	NSQ.lagPrev = pitchL[psEncC.nb_subfr-1]

	// Save quantized speech signal.
	copy(NSQ.xq[:psEncC.ltp_mem_length], NSQ.xq[psEncC.frame_length:psEncC.frame_length+psEncC.ltp_mem_length])
	copy(NSQ.sLTP_shp_Q14[:psEncC.ltp_mem_length], NSQ.sLTP_shp_Q14[psEncC.frame_length:psEncC.frame_length+psEncC.ltp_mem_length])
}

// silk_noise_shape_quantizer_del_dec — C: NSQ_del_dec.c:315-646.
// pulses and xq are passed with an explicit base offset because the
// inner loop uses [i - decisionDelay] lookups that wrap back into the
// prior subframe's memory; Go slices cannot index negatively.
func silk_noise_shape_quantizer_del_dec(
	NSQ *silk_nsq_state,
	psDelDec []NSQ_del_dec_struct,
	signalType opus_int,
	x_Q10 []opus_int32,
	pulses []opus_int8,
	pulsesOff opus_int,
	xq []opus_int16,
	xqOff opus_int,
	sLTP_Q15 []opus_int32,
	delayedGain_Q10 []opus_int32,
	a_Q12 []opus_int16,
	b_Q14 []opus_int16,
	AR_shp_Q13 []opus_int16,
	lag opus_int,
	HarmShapeFIRPacked_Q14 opus_int32,
	Tilt_Q14 opus_int,
	LF_shp_Q14 opus_int32,
	Gain_Q16 opus_int32,
	Lambda_Q10 opus_int,
	offset_Q10 opus_int32,
	length opus_int,
	subfr opus_int,
	shapingLPCOrder opus_int,
	predictLPCOrder opus_int,
	warping_Q16 opus_int32,
	nStatesDelayedDecision opus_int,
	smpl_buf_idx *opus_int,
	decisionDelay opus_int,
	arch int,
) {
	// psSampleState[k][0..1] — stack-allocated at MAX_DEL_DEC_STATES
	// upper bound. Previously this was an allocation per subframe
	// (4× per frame at complexity 10).
	var psSampleStateBuf [MAX_DEL_DEC_STATES][2]NSQ_sample_struct
	psSampleState := psSampleStateBuf[:nStatesDelayedDecision]

	shp_base := NSQ.sLTP_shp_buf_idx - lag + HARM_SHAPE_FIR_TAPS/2
	pred_base := NSQ.sLTP_buf_idx - lag + LTP_ORDER/2
	Gain_Q10 := silk_RSHIFT(Gain_Q16, 6)

	for i := opus_int(0); i < length; i++ {
		// Long-term prediction.
		var LTP_pred_Q14 opus_int32
		if signalType == TYPE_VOICED {
			LTP_pred_Q14 = 2
			LTP_pred_Q14 = silk_SMLAWB(LTP_pred_Q14, sLTP_Q15[pred_base+0], opus_int32(b_Q14[0]))
			LTP_pred_Q14 = silk_SMLAWB(LTP_pred_Q14, sLTP_Q15[pred_base-1], opus_int32(b_Q14[1]))
			LTP_pred_Q14 = silk_SMLAWB(LTP_pred_Q14, sLTP_Q15[pred_base-2], opus_int32(b_Q14[2]))
			LTP_pred_Q14 = silk_SMLAWB(LTP_pred_Q14, sLTP_Q15[pred_base-3], opus_int32(b_Q14[3]))
			LTP_pred_Q14 = silk_SMLAWB(LTP_pred_Q14, sLTP_Q15[pred_base-4], opus_int32(b_Q14[4]))
			LTP_pred_Q14 = silk_LSHIFT(LTP_pred_Q14, 1) // Q13 -> Q14
			pred_base++
		} else {
			LTP_pred_Q14 = 0
		}

		// Long-term shaping.
		var n_LTP_Q14 opus_int32
		if lag > 0 {
			n_LTP_Q14 = silk_SMULWB(silk_ADD_SAT32(NSQ.sLTP_shp_Q14[shp_base+0], NSQ.sLTP_shp_Q14[shp_base-2]), HarmShapeFIRPacked_Q14)
			n_LTP_Q14 = silk_SMLAWT(n_LTP_Q14, NSQ.sLTP_shp_Q14[shp_base-1], HarmShapeFIRPacked_Q14)
			n_LTP_Q14 = silk_SUB_LSHIFT32(LTP_pred_Q14, n_LTP_Q14, 2) // Q12 -> Q14
			shp_base++
		} else {
			n_LTP_Q14 = 0
		}

		for k := opus_int(0); k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]
			psSS := &psSampleState[k]

			// Generate dither.
			psDD.Seed = silk_RAND(psDD.Seed)

			// Short-term prediction: in C, psLPC_Q14 = &psDD->sLPC_Q14[ NSQ_LPC_BUF_LENGTH - 1 + i ].
			psLPC_base := opus_int(NSQ_LPC_BUF_LENGTH - 1 + i)
			LPC_pred_Q14 := silk_noise_shape_quantizer_short_prediction_c(psDD.sLPC_Q14[:], psLPC_base, a_Q12, predictLPCOrder)
			LPC_pred_Q14 = silk_LSHIFT(LPC_pred_Q14, 4) // Q10 -> Q14

			// Noise shape feedback.
			// Output of lowpass section.
			tmp2 := silk_SMLAWB(psDD.Diff_Q14, psDD.sAR2_Q14[0], warping_Q16)
			// Output of allpass section.
			tmp1 := silk_SMLAWB(psDD.sAR2_Q14[0], silk_SUB32_ovflw(psDD.sAR2_Q14[1], tmp2), warping_Q16)
			psDD.sAR2_Q14[0] = tmp2
			n_AR_Q14 := silk_RSHIFT(opus_int32(shapingLPCOrder), 1)
			n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, opus_int32(AR_shp_Q13[0]))
			// Loop over allpass sections.
			for j := opus_int(2); j < shapingLPCOrder; j += 2 {
				tmp2 = silk_SMLAWB(psDD.sAR2_Q14[j-1], silk_SUB32_ovflw(psDD.sAR2_Q14[j+0], tmp1), warping_Q16)
				psDD.sAR2_Q14[j-1] = tmp1
				n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, opus_int32(AR_shp_Q13[j-1]))
				tmp1 = silk_SMLAWB(psDD.sAR2_Q14[j+0], silk_SUB32_ovflw(psDD.sAR2_Q14[j+1], tmp2), warping_Q16)
				psDD.sAR2_Q14[j+0] = tmp2
				n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp2, opus_int32(AR_shp_Q13[j]))
			}
			psDD.sAR2_Q14[shapingLPCOrder-1] = tmp1
			n_AR_Q14 = silk_SMLAWB(n_AR_Q14, tmp1, opus_int32(AR_shp_Q13[shapingLPCOrder-1]))

			n_AR_Q14 = silk_LSHIFT(n_AR_Q14, 1)                                    // Q11 -> Q12
			n_AR_Q14 = silk_SMLAWB(n_AR_Q14, psDD.LF_AR_Q14, opus_int32(Tilt_Q14)) // Q12
			n_AR_Q14 = silk_LSHIFT(n_AR_Q14, 2)                                    // Q12 -> Q14

			n_LF_Q14 := silk_SMULWB(psDD.Shape_Q14[*smpl_buf_idx], LF_shp_Q14) // Q12
			n_LF_Q14 = silk_SMLAWT(n_LF_Q14, psDD.LF_AR_Q14, LF_shp_Q14)       // Q12
			n_LF_Q14 = silk_LSHIFT(n_LF_Q14, 2)                                // Q12 -> Q14

			// r = x[i] - LTP_pred - LPC_pred + n_AR + n_Tilt + n_LF + n_LTP.
			tmp1 = silk_ADD_SAT32(n_AR_Q14, n_LF_Q14)        // Q14
			tmp2 = silk_ADD32_ovflw(n_LTP_Q14, LPC_pred_Q14) // Q13
			tmp1 = silk_SUB_SAT32(tmp2, tmp1)                // Q13
			tmp1 = silk_RSHIFT_ROUND(tmp1, 4)                // Q10

			r_Q10 := silk_SUB32(x_Q10[i], tmp1) // residual error Q10

			// Flip sign depending on dither.
			if psDD.Seed < 0 {
				r_Q10 = -r_Q10
			}
			r_Q10 = silk_LIMIT_32(r_Q10, -(31 << 10), 30<<10)

			// Find two quantization level candidates and measure their rate-distortion.
			q1_Q10 := silk_SUB32(r_Q10, offset_Q10)
			q1_Q0 := silk_RSHIFT(q1_Q10, 10)
			if Lambda_Q10 > 2048 {
				rdo_offset := opus_int32(Lambda_Q10)/2 - 512
				if q1_Q10 > rdo_offset {
					q1_Q0 = silk_RSHIFT(q1_Q10-rdo_offset, 10)
				} else if q1_Q10 < -rdo_offset {
					q1_Q0 = silk_RSHIFT(q1_Q10+rdo_offset, 10)
				} else if q1_Q10 < 0 {
					q1_Q0 = -1
				} else {
					q1_Q0 = 0
				}
			}
			var q2_Q10, rd1_Q10, rd2_Q10 opus_int32
			if q1_Q0 > 0 {
				q1_Q10 = silk_SUB32(silk_LSHIFT(q1_Q0, 10), QUANT_LEVEL_ADJUST_Q10)
				q1_Q10 = silk_ADD32(q1_Q10, offset_Q10)
				q2_Q10 = silk_ADD32(q1_Q10, 1024)
				rd1_Q10 = silk_SMULBB(q1_Q10, opus_int32(Lambda_Q10))
				rd2_Q10 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
			} else if q1_Q0 == 0 {
				q1_Q10 = offset_Q10
				q2_Q10 = silk_ADD32(q1_Q10, 1024-QUANT_LEVEL_ADJUST_Q10)
				rd1_Q10 = silk_SMULBB(q1_Q10, opus_int32(Lambda_Q10))
				rd2_Q10 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
			} else if q1_Q0 == -1 {
				q2_Q10 = offset_Q10
				q1_Q10 = silk_SUB32(q2_Q10, 1024-QUANT_LEVEL_ADJUST_Q10)
				rd1_Q10 = silk_SMULBB(-q1_Q10, opus_int32(Lambda_Q10))
				rd2_Q10 = silk_SMULBB(q2_Q10, opus_int32(Lambda_Q10))
			} else { // q1_Q0 < -1
				q1_Q10 = silk_ADD32(silk_LSHIFT(q1_Q0, 10), QUANT_LEVEL_ADJUST_Q10)
				q1_Q10 = silk_ADD32(q1_Q10, offset_Q10)
				q2_Q10 = silk_ADD32(q1_Q10, 1024)
				rd1_Q10 = silk_SMULBB(-q1_Q10, opus_int32(Lambda_Q10))
				rd2_Q10 = silk_SMULBB(-q2_Q10, opus_int32(Lambda_Q10))
			}
			rr_Q10 := silk_SUB32(r_Q10, q1_Q10)
			rd1_Q10 = silk_RSHIFT(silk_SMLABB(rd1_Q10, rr_Q10, rr_Q10), 10)
			rr_Q10 = silk_SUB32(r_Q10, q2_Q10)
			rd2_Q10 = silk_RSHIFT(silk_SMLABB(rd2_Q10, rr_Q10, rr_Q10), 10)

			if rd1_Q10 < rd2_Q10 {
				psSS[0].RD_Q10 = silk_ADD32(psDD.RD_Q10, rd1_Q10)
				psSS[1].RD_Q10 = silk_ADD32(psDD.RD_Q10, rd2_Q10)
				psSS[0].Q_Q10 = q1_Q10
				psSS[1].Q_Q10 = q2_Q10
			} else {
				psSS[0].RD_Q10 = silk_ADD32(psDD.RD_Q10, rd2_Q10)
				psSS[1].RD_Q10 = silk_ADD32(psDD.RD_Q10, rd1_Q10)
				psSS[0].Q_Q10 = q2_Q10
				psSS[1].Q_Q10 = q1_Q10
			}

			// Update states for best quantization.
			exc_Q14 := silk_LSHIFT32(psSS[0].Q_Q10, 4)
			if psDD.Seed < 0 {
				exc_Q14 = -exc_Q14
			}
			LPC_exc_Q14 := silk_ADD32(exc_Q14, LTP_pred_Q14)
			xq_Q14 := silk_ADD32_ovflw(LPC_exc_Q14, LPC_pred_Q14)

			psSS[0].Diff_Q14 = silk_SUB32_ovflw(xq_Q14, silk_LSHIFT32(x_Q10[i], 4))
			sLF_AR_shp_Q14 := silk_SUB32_ovflw(psSS[0].Diff_Q14, n_AR_Q14)
			psSS[0].sLTP_shp_Q14 = silk_SUB_SAT32(sLF_AR_shp_Q14, n_LF_Q14)
			psSS[0].LF_AR_Q14 = sLF_AR_shp_Q14
			psSS[0].LPC_exc_Q14 = LPC_exc_Q14
			psSS[0].xq_Q14 = xq_Q14

			// Update states for second best quantization.
			exc_Q14 = silk_LSHIFT32(psSS[1].Q_Q10, 4)
			if psDD.Seed < 0 {
				exc_Q14 = -exc_Q14
			}
			LPC_exc_Q14 = silk_ADD32(exc_Q14, LTP_pred_Q14)
			xq_Q14 = silk_ADD32_ovflw(LPC_exc_Q14, LPC_pred_Q14)

			psSS[1].Diff_Q14 = silk_SUB32_ovflw(xq_Q14, silk_LSHIFT32(x_Q10[i], 4))
			sLF_AR_shp_Q14 = silk_SUB32_ovflw(psSS[1].Diff_Q14, n_AR_Q14)
			psSS[1].sLTP_shp_Q14 = silk_SUB_SAT32(sLF_AR_shp_Q14, n_LF_Q14)
			psSS[1].LF_AR_Q14 = sLF_AR_shp_Q14
			psSS[1].LPC_exc_Q14 = LPC_exc_Q14
			psSS[1].xq_Q14 = xq_Q14
		}

		*smpl_buf_idx = (*smpl_buf_idx - 1) % DECISION_DELAY
		if *smpl_buf_idx < 0 {
			*smpl_buf_idx += DECISION_DELAY
		}
		last_smple_idx := (*smpl_buf_idx + decisionDelay) % DECISION_DELAY

		// Find winner.
		RDmin_Q10 := psSampleState[0][0].RD_Q10
		Winner_ind := opus_int(0)
		for k := opus_int(1); k < nStatesDelayedDecision; k++ {
			if psSampleState[k][0].RD_Q10 < RDmin_Q10 {
				RDmin_Q10 = psSampleState[k][0].RD_Q10
				Winner_ind = k
			}
		}

		// Increase RD values of expired states.
		Winner_rand_state := psDelDec[Winner_ind].RandState[last_smple_idx]
		for k := opus_int(0); k < nStatesDelayedDecision; k++ {
			if psDelDec[k].RandState[last_smple_idx] != Winner_rand_state {
				psSampleState[k][0].RD_Q10 = silk_ADD32(psSampleState[k][0].RD_Q10, silk_int32_MAX>>4)
				psSampleState[k][1].RD_Q10 = silk_ADD32(psSampleState[k][1].RD_Q10, silk_int32_MAX>>4)
			}
		}

		// Find worst in first set and best in second set.
		RDmax_Q10 := psSampleState[0][0].RD_Q10
		RDmin_Q10 = psSampleState[0][1].RD_Q10
		RDmax_ind := opus_int(0)
		RDmin_ind := opus_int(0)
		for k := opus_int(1); k < nStatesDelayedDecision; k++ {
			if psSampleState[k][0].RD_Q10 > RDmax_Q10 {
				RDmax_Q10 = psSampleState[k][0].RD_Q10
				RDmax_ind = k
			}
			if psSampleState[k][1].RD_Q10 < RDmin_Q10 {
				RDmin_Q10 = psSampleState[k][1].RD_Q10
				RDmin_ind = k
			}
		}

		// Replace a state if best from second set outperforms worst in first set.
		if RDmin_Q10 < RDmax_Q10 {
			nsq_del_dec_copy_tail(&psDelDec[RDmax_ind], &psDelDec[RDmin_ind], i)
			psSampleState[RDmax_ind][0] = psSampleState[RDmin_ind][1]
		}

		// Write samples from winner to output and long-term filter states.
		psDD := &psDelDec[Winner_ind]
		if subfr > 0 || i >= decisionDelay {
			pulses[pulsesOff+i-decisionDelay] = opus_int8(silk_RSHIFT_ROUND(psDD.Q_Q10[last_smple_idx], 10))
			xq[xqOff+i-decisionDelay] = opus_int16(silk_SAT16(silk_RSHIFT_ROUND(
				silk_SMULWW(psDD.Xq_Q14[last_smple_idx], delayedGain_Q10[last_smple_idx]), 8)))
			NSQ.sLTP_shp_Q14[NSQ.sLTP_shp_buf_idx-decisionDelay] = psDD.Shape_Q14[last_smple_idx]
			sLTP_Q15[NSQ.sLTP_buf_idx-decisionDelay] = psDD.Pred_Q15[last_smple_idx]
		}
		NSQ.sLTP_shp_buf_idx++
		NSQ.sLTP_buf_idx++

		// Update states.
		for k := opus_int(0); k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]
			psSS := &psSampleState[k][0]
			psDD.LF_AR_Q14 = psSS.LF_AR_Q14
			psDD.Diff_Q14 = psSS.Diff_Q14
			psDD.sLPC_Q14[NSQ_LPC_BUF_LENGTH+i] = psSS.xq_Q14
			psDD.Xq_Q14[*smpl_buf_idx] = psSS.xq_Q14
			psDD.Q_Q10[*smpl_buf_idx] = psSS.Q_Q10
			psDD.Pred_Q15[*smpl_buf_idx] = silk_LSHIFT32(psSS.LPC_exc_Q14, 1)
			psDD.Shape_Q14[*smpl_buf_idx] = psSS.sLTP_shp_Q14
			psDD.Seed = silk_ADD32_ovflw(psDD.Seed, silk_RSHIFT_ROUND(psSS.Q_Q10, 10))
			psDD.RandState[*smpl_buf_idx] = psDD.Seed
			psDD.RD_Q10 = psSS.RD_Q10
		}
		delayedGain_Q10[*smpl_buf_idx] = Gain_Q10
	}

	// Update LPC states.
	for k := opus_int(0); k < nStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		copy(psDD.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], psDD.sLPC_Q14[length:length+NSQ_LPC_BUF_LENGTH])
	}
}

// silk_nsq_del_dec_scale_states — C: NSQ_del_dec.c:648-730.
func silk_nsq_del_dec_scale_states(
	psEncC *silk_encoder_state,
	NSQ *silk_nsq_state,
	psDelDec []NSQ_del_dec_struct,
	x16 []opus_int16,
	x_sc_Q10 []opus_int32,
	sLTP []opus_int16,
	sLTP_Q15 []opus_int32,
	subfr opus_int,
	nStatesDelayedDecision opus_int,
	LTP_scale_Q14 opus_int,
	Gains_Q16 []opus_int32,
	pitchL []opus_int,
	signal_type opus_int,
	decisionDelay opus_int,
) {
	lag := pitchL[subfr]
	inv_gain_Q31 := silk_INVERSE32_varQ(silk_max(Gains_Q16[subfr], 1), 47)

	inv_gain_Q26 := silk_RSHIFT_ROUND(inv_gain_Q31, 5)
	for i := opus_int(0); i < psEncC.subfr_length; i++ {
		x_sc_Q10[i] = silk_SMULWW(opus_int32(x16[i]), inv_gain_Q26)
	}

	if NSQ.rewhite_flag != 0 {
		if subfr == 0 {
			inv_gain_Q31 = silk_LSHIFT(silk_SMULWB(inv_gain_Q31, opus_int32(LTP_scale_Q14)), 2)
		}
		for i := NSQ.sLTP_buf_idx - lag - LTP_ORDER/2; i < NSQ.sLTP_buf_idx; i++ {
			sLTP_Q15[i] = silk_SMULWB(inv_gain_Q31, opus_int32(sLTP[i]))
		}
	}

	if Gains_Q16[subfr] != NSQ.prev_gain_Q16 {
		gain_adj_Q16 := silk_DIV32_varQ(NSQ.prev_gain_Q16, Gains_Q16[subfr], 16)

		for i := NSQ.sLTP_shp_buf_idx - psEncC.ltp_mem_length; i < NSQ.sLTP_shp_buf_idx; i++ {
			NSQ.sLTP_shp_Q14[i] = silk_SMULWW(gain_adj_Q16, NSQ.sLTP_shp_Q14[i])
		}

		if signal_type == TYPE_VOICED && NSQ.rewhite_flag == 0 {
			for i := NSQ.sLTP_buf_idx - lag - LTP_ORDER/2; i < NSQ.sLTP_buf_idx-decisionDelay; i++ {
				sLTP_Q15[i] = silk_SMULWW(gain_adj_Q16, sLTP_Q15[i])
			}
		}

		for k := opus_int(0); k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]

			psDD.LF_AR_Q14 = silk_SMULWW(gain_adj_Q16, psDD.LF_AR_Q14)
			psDD.Diff_Q14 = silk_SMULWW(gain_adj_Q16, psDD.Diff_Q14)

			for i := 0; i < NSQ_LPC_BUF_LENGTH; i++ {
				psDD.sLPC_Q14[i] = silk_SMULWW(gain_adj_Q16, psDD.sLPC_Q14[i])
			}
			for i := 0; i < MAX_SHAPE_LPC_ORDER; i++ {
				psDD.sAR2_Q14[i] = silk_SMULWW(gain_adj_Q16, psDD.sAR2_Q14[i])
			}
			for i := 0; i < DECISION_DELAY; i++ {
				psDD.Pred_Q15[i] = silk_SMULWW(gain_adj_Q16, psDD.Pred_Q15[i])
				psDD.Shape_Q14[i] = silk_SMULWW(gain_adj_Q16, psDD.Shape_Q14[i])
			}
		}

		NSQ.prev_gain_Q16 = Gains_Q16[subfr]
	}
}
