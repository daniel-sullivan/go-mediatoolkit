package nativeopus

// testingForceScalarNSQDelDec is a test-only flag that, when set, forces
// silk_NSQ_del_dec_c to dispatch to the scalar variant
// silk_noise_shape_quantizer_del_dec even when the SoA path would
// otherwise be selected. Flipped via ExportTestSilkForceScalarNSQDelDec
// so integration parity tests can compare scalar vs SoA outputs on
// byte-identical inputs.
var testingForceScalarNSQDelDec bool

// silk_noise_shape_quantizer_del_dec_soa — 4-lane SoA-fused variant of
// silk_noise_shape_quantizer_del_dec used when nStatesDelayedDecision ==
// MAX_DEL_DEC_STATES and the NSQ SIMD kernels are linked in.
//
// Two SoA-layout kernels hoist the inner-k work out of the scalar loop:
//   - shortPredictionSoASIMD computes the 4-lane LPC short-prediction
//     once per sample, populating lpcPredsAll[0..3].
//   - shortNSQAllpassSIMD runs the 4-lane noise-shape feedback allpass
//     once per sample, mutating soaState.sAR2_Q14 in place and returning
//     nARAll[0..3] — the per-lane n_AR_Q14 accumulator.
//
// Everything else in the per-k loop (Seed, Diff, LF_AR, quantization
// level search, RD tournament, sample write) stays scalar because the
// data dependencies cross lanes (tournament replacement copies a lane
// from second-best into worst-first). The combined hoisting saves ~93
// ns/sample on arm64 which covers the SoA mirror maintenance cost.
//
// The SoA mirror is fed from the AoS at entry, dual-written for
// sLPC_Q14 on sample emit, resynced per-lane after any copy_tail state
// replacement, and written back to AoS for sAR2_Q14 on exit so the
// winner-state update block downstream still reads the right state.
func silk_noise_shape_quantizer_del_dec_soa(
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
	// upper bound.
	var psSampleStateBuf [MAX_DEL_DEC_STATES][2]NSQ_sample_struct
	psSampleState := psSampleStateBuf[:nStatesDelayedDecision]

	// SoA mirror of sLPC_Q14 and sAR2_Q14 for 4-lane NEON kernels.
	// Only populated/used when nStates==MAX_DEL_DEC_STATES (the dispatch
	// gate guarantees this but we still only write lanes < nStates).
	var soaState NSQDelDecSoA
	for k := opus_int(0); k < nStatesDelayedDecision; k++ {
		for t := 0; t < NSQ_LPC_BUF_LENGTH; t++ {
			soaState.sLPC_Q14[t][k] = psDelDec[k].sLPC_Q14[t]
		}
		for t := 0; t < MAX_SHAPE_LPC_ORDER; t++ {
			soaState.sAR2_Q14[t][k] = psDelDec[k].sAR2_Q14[t]
		}
		soaState.Diff_Q14[k] = psDelDec[k].Diff_Q14
		soaState.LF_AR_Q14[k] = psDelDec[k].LF_AR_Q14
	}

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

		// Precompute both SoA kernels for the current sample index.
		psLPC_base := opus_int(NSQ_LPC_BUF_LENGTH - 1 + i)
		var lpcPredsAll [MAX_DEL_DEC_STATES]opus_int32
		var nARAll [MAX_DEL_DEC_STATES]opus_int32
		if nsqSIMDAvailable {
			shortPredictionSoASIMD(&soaState.sLPC_Q14[psLPC_base][0], &a_Q12[0], predictLPCOrder, &lpcPredsAll)
			shortNSQAllpassSIMD(&soaState.sAR2_Q14[0][0], &soaState.Diff_Q14[0], int32(warping_Q16), &AR_shp_Q13[0], int(shapingLPCOrder), &nARAll)
		} else {
			lpcPredsAll = silk_noise_shape_quantizer_short_prediction_soa(&soaState, psLPC_base, a_Q12, predictLPCOrder)
			nARAll = silk_noise_shape_allpass_soa(&soaState, warping_Q16, AR_shp_Q13, shapingLPCOrder)
		}

		for k := opus_int(0); k < nStatesDelayedDecision; k++ {
			psDD := &psDelDec[k]
			psSS := &psSampleState[k]

			// Generate dither.
			psDD.Seed = silk_RAND(psDD.Seed)

			// Short-term prediction precomputed above (SoA kernel).
			LPC_pred_Q14 := silk_LSHIFT(lpcPredsAll[k], 4) // Q10 -> Q14

			// Noise shape feedback n_AR_Q14 precomputed above (SoA
			// kernel already mutated soaState.sAR2_Q14 in place, so we
			// skip the scalar allpass block entirely).
			n_AR_Q14 := nARAll[k]
			n_AR_Q14 = silk_LSHIFT(n_AR_Q14, 1)                                    // Q11 -> Q12
			n_AR_Q14 = silk_SMLAWB(n_AR_Q14, psDD.LF_AR_Q14, opus_int32(Tilt_Q14)) // Q12
			n_AR_Q14 = silk_LSHIFT(n_AR_Q14, 2)                                    // Q12 -> Q14

			n_LF_Q14 := silk_SMULWB(psDD.Shape_Q14[*smpl_buf_idx], LF_shp_Q14) // Q12
			n_LF_Q14 = silk_SMLAWT(n_LF_Q14, psDD.LF_AR_Q14, LF_shp_Q14)       // Q12
			n_LF_Q14 = silk_LSHIFT(n_LF_Q14, 2)                                // Q12 -> Q14

			// r = x[i] - LTP_pred - LPC_pred + n_AR + n_Tilt + n_LF + n_LTP.
			tmp1 := silk_ADD_SAT32(n_AR_Q14, n_LF_Q14)        // Q14
			tmp2 := silk_ADD32_ovflw(n_LTP_Q14, LPC_pred_Q14) // Q13
			tmp1 = silk_SUB_SAT32(tmp2, tmp1)                 // Q13
			tmp1 = silk_RSHIFT_ROUND(tmp1, 4)                 // Q10

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
			// Before copy_tail: sync AoS sAR2_Q14 for the source lane
			// (RDmin_ind) from the SoA mirror, because the SIMD allpass
			// kernel mutated sAR2_Q14 only in the SoA this subframe —
			// copy_tail otherwise would clone stale pre-subframe values.
			// All other copied fields (sLPC_Q14 via dual-write, Diff_Q14
			// and LF_AR_Q14 via the scalar state-update block at end of
			// per-k loop, plus RandState/Q_Q10/Xq_Q14/Pred_Q15/Shape_Q14
			// which are unused by SoA) are already current in AoS.
			for t := 0; t < MAX_SHAPE_LPC_ORDER; t++ {
				psDelDec[RDmin_ind].sAR2_Q14[t] = soaState.sAR2_Q14[t][RDmin_ind]
			}
			nsq_del_dec_copy_tail(&psDelDec[RDmax_ind], &psDelDec[RDmin_ind], i)
			psSampleState[RDmax_ind][0] = psSampleState[RDmin_ind][1]
			// Resync SoA for the overwritten lane — copy_tail has
			// rewritten AoS sLPC_Q14[i..], sAR2_Q14[*], Diff_Q14 and
			// LF_AR_Q14 in psDelDec[RDmax_ind] from the RDmin_ind lane.
			// The SoA mirror of those fields must follow.
			src := &psDelDec[RDmax_ind]
			for idx := int(i); idx < MAX_SUB_FRAME_LENGTH+NSQ_LPC_BUF_LENGTH; idx++ {
				soaState.sLPC_Q14[idx][RDmax_ind] = src.sLPC_Q14[idx]
			}
			for t := 0; t < MAX_SHAPE_LPC_ORDER; t++ {
				soaState.sAR2_Q14[t][RDmax_ind] = src.sAR2_Q14[t]
			}
			soaState.Diff_Q14[RDmax_ind] = src.Diff_Q14
			soaState.LF_AR_Q14[RDmax_ind] = src.LF_AR_Q14
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
			// Dual-write the SoA mirror so the next sample's SoA kernels
			// see the freshly emitted xq_Q14 in sLPC_Q14.
			soaState.sLPC_Q14[NSQ_LPC_BUF_LENGTH+i][k] = psSS.xq_Q14
			// Diff_Q14 and LF_AR_Q14 are also mirrored in the SoA and
			// are consumed by shortNSQAllpassSIMD (Diff_Q14) + the
			// per-k scalar code (LF_AR_Q14). Keep the mirror in sync.
			soaState.Diff_Q14[k] = psSS.Diff_Q14
			soaState.LF_AR_Q14[k] = psSS.LF_AR_Q14
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

	// Sync SoA back to AoS for sAR2_Q14 — the SIMD allpass kernel
	// mutated it only in the SoA mirror. sLPC_Q14 was dual-written per
	// sample so the AoS already has the fresh values. Diff_Q14 and
	// LF_AR_Q14 are already updated in AoS via the scalar per-k state
	// update block, which writes psDD.Diff_Q14 / psDD.LF_AR_Q14 from
	// psSS — the SoA mirror of those is only kept for the SIMD kernel
	// inputs on the next iteration.
	for k := opus_int(0); k < nStatesDelayedDecision; k++ {
		for t := 0; t < MAX_SHAPE_LPC_ORDER; t++ {
			psDelDec[k].sAR2_Q14[t] = soaState.sAR2_Q14[t][k]
		}
	}

	// Update LPC states.
	for k := opus_int(0); k < nStatesDelayedDecision; k++ {
		psDD := &psDelDec[k]
		copy(psDD.sLPC_Q14[:NSQ_LPC_BUF_LENGTH], psDD.sLPC_Q14[length:length+NSQ_LPC_BUF_LENGTH])
	}

	_ = arch
}
