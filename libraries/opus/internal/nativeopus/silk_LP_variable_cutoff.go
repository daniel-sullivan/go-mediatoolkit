package nativeopus

// Port of libopus/silk/LP_variable_cutoff.c.
//
// Variable cut-off low-pass filter with piece-wise linear
// interpolation between elliptic filter designs. The C file contains
// the silk_LP_state struct decl inline; we port the struct here since
// it is only used by this function and the larger silk_encoder_state
// (not yet ported).

// silk_LP_state — variable cut-off low-pass filter state.
type silk_LP_state struct {
	In_LP_State         [2]opus_int32 // Low-pass filter state.
	transition_frame_no opus_int32    // Counter mapped to a cut-off frequency.
	mode                opus_int      // <0: switch down, >0: switch up, 0: no-op.
	saved_fs_kHz        opus_int32    // Non-zero: last Fs before a bandwidth switch reset.
}

// silk_LP_interpolate_filter_taps — static helper in the C file.
// Interpolates filter taps from the silk_Transition_LP_* tables.
func silk_LP_interpolate_filter_taps(B_Q28, A_Q28 []opus_int32, ind opus_int, fac_Q16 opus_int32) {
	if ind < TRANSITION_INT_NUM-1 {
		if fac_Q16 > 0 {
			if fac_Q16 < 32768 {
				// Piece-wise linear interpolation of B and A.
				for nb := 0; nb < TRANSITION_NB; nb++ {
					B_Q28[nb] = silk_SMLAWB(
						silk_Transition_LP_B_Q28[ind][nb],
						silk_Transition_LP_B_Q28[ind+1][nb]-silk_Transition_LP_B_Q28[ind][nb],
						fac_Q16)
				}
				for na := 0; na < TRANSITION_NA; na++ {
					A_Q28[na] = silk_SMLAWB(
						silk_Transition_LP_A_Q28[ind][na],
						silk_Transition_LP_A_Q28[ind+1][na]-silk_Transition_LP_A_Q28[ind][na],
						fac_Q16)
				}
			} else {
				// (fac_Q16 - (1<<16)) fits in 16-bit int.
				silk_assert(fac_Q16-(1<<16) == silk_SAT16(fac_Q16-(1<<16)))
				for nb := 0; nb < TRANSITION_NB; nb++ {
					B_Q28[nb] = silk_SMLAWB(
						silk_Transition_LP_B_Q28[ind+1][nb],
						silk_Transition_LP_B_Q28[ind+1][nb]-silk_Transition_LP_B_Q28[ind][nb],
						fac_Q16-(opus_int32(1)<<16))
				}
				for na := 0; na < TRANSITION_NA; na++ {
					A_Q28[na] = silk_SMLAWB(
						silk_Transition_LP_A_Q28[ind+1][na],
						silk_Transition_LP_A_Q28[ind+1][na]-silk_Transition_LP_A_Q28[ind][na],
						fac_Q16-(opus_int32(1)<<16))
				}
			}
		} else {
			copy(B_Q28[:TRANSITION_NB], silk_Transition_LP_B_Q28[ind][:])
			copy(A_Q28[:TRANSITION_NA], silk_Transition_LP_A_Q28[ind][:])
		}
	} else {
		copy(B_Q28[:TRANSITION_NB], silk_Transition_LP_B_Q28[TRANSITION_INT_NUM-1][:])
		copy(A_Q28[:TRANSITION_NA], silk_Transition_LP_A_Q28[TRANSITION_INT_NUM-1][:])
	}
}

// silk_LP_variable_cutoff — low-pass filter with variable cutoff
// frequency. Set psLP.mode != 0 to run the filter; set mode=0 to
// deactivate.
func silk_LP_variable_cutoff(psLP *silk_LP_state, frame []opus_int16, frame_length opus_int) {
	var B_Q28 [TRANSITION_NB]opus_int32
	var A_Q28 [TRANSITION_NA]opus_int32
	var fac_Q16 opus_int32
	var ind opus_int

	silk_assert(psLP.transition_frame_no >= 0 && psLP.transition_frame_no <= TRANSITION_FRAMES)

	if psLP.mode != 0 {
		// Calculate index and interpolation factor.
		if TRANSITION_INT_STEPS == 64 {
			fac_Q16 = silk_LSHIFT(TRANSITION_FRAMES-psLP.transition_frame_no, 16-6)
		} else {
			fac_Q16 = silk_DIV32_16(
				silk_LSHIFT(TRANSITION_FRAMES-psLP.transition_frame_no, 16),
				TRANSITION_FRAMES)
		}
		ind = opus_int(silk_RSHIFT(fac_Q16, 16))
		fac_Q16 -= silk_LSHIFT(opus_int32(ind), 16)

		silk_assert(ind >= 0)
		silk_assert(ind < TRANSITION_INT_NUM)

		// Interpolate filter coefficients.
		silk_LP_interpolate_filter_taps(B_Q28[:], A_Q28[:], ind, fac_Q16)

		// Update transition frame number for next frame.
		psLP.transition_frame_no = silk_LIMIT(
			psLP.transition_frame_no+opus_int32(psLP.mode), 0, TRANSITION_FRAMES)

		// ARMA low-pass filtering.
		silk_assert(TRANSITION_NB == 3 && TRANSITION_NA == 2)
		silk_biquad_alt_stride1(frame, B_Q28[:], A_Q28[:], psLP.In_LP_State[:],
			frame, opus_int32(frame_length))
	}
}
