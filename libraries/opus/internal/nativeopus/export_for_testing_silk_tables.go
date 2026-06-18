package nativeopus

// SILK table test shims — return the Go tables as byte/int16/int32
// slices so benchcmp can byte-for-byte compare against the C rom.

func ExportTestSilkTable_GainICDF() []byte {
	out := make([]byte, 0, 3*8)
	for _, row := range silk_gain_iCDF {
		out = append(out, row[:]...)
	}
	return out
}
func ExportTestSilkTable_DeltaGainICDF() []byte {
	return append([]byte(nil), silk_delta_gain_iCDF[:]...)
}
func ExportTestSilkTable_PitchLagICDF() []byte { return append([]byte(nil), silk_pitch_lag_iCDF[:]...) }
func ExportTestSilkTable_PitchDeltaICDF() []byte {
	return append([]byte(nil), silk_pitch_delta_iCDF[:]...)
}
func ExportTestSilkTable_PitchContourICDF() []byte {
	return append([]byte(nil), silk_pitch_contour_iCDF[:]...)
}
func ExportTestSilkTable_PitchContourNBICDF() []byte {
	return append([]byte(nil), silk_pitch_contour_NB_iCDF[:]...)
}
func ExportTestSilkTable_PitchContour10msICDF() []byte {
	return append([]byte(nil), silk_pitch_contour_10_ms_iCDF[:]...)
}
func ExportTestSilkTable_PitchContour10msNBICDF() []byte {
	return append([]byte(nil), silk_pitch_contour_10_ms_NB_iCDF[:]...)
}

func ExportTestSilkTable_StereoPredQuantQ13() []int16 {
	out := make([]int16, len(silk_stereo_pred_quant_Q13))
	for i, v := range silk_stereo_pred_quant_Q13 {
		out[i] = int16(v)
	}
	return out
}
func ExportTestSilkTable_StereoPredJointICDF() []byte {
	return append([]byte(nil), silk_stereo_pred_joint_iCDF[:]...)
}
func ExportTestSilkTable_LTPscaleICDF() []byte {
	return append([]byte(nil), silk_LTPscale_iCDF[:]...)
}
func ExportTestSilkTable_LTPPerIndexICDF() []byte {
	return append([]byte(nil), silk_LTP_per_index_iCDF[:]...)
}
func ExportTestSilkTable_SignICDF() []byte { return append([]byte(nil), silk_sign_iCDF[:]...) }

func ExportTestSilkTable_NLSFCB1NBMBQ8() []byte {
	return append([]byte(nil), silk_NLSF_CB1_NB_MB_Q8[:]...)
}
func ExportTestSilkTable_NLSFCB1WBQ8() []byte {
	return append([]byte(nil), silk_NLSF_CB1_WB_Q8[:]...)
}
func ExportTestSilkTable_NLSFCB1WghtQ9() []int16 {
	out := make([]int16, len(silk_NLSF_CB1_Wght_Q9))
	for i, v := range silk_NLSF_CB1_Wght_Q9 {
		out[i] = int16(v)
	}
	return out
}
func ExportTestSilkTable_NLSFCB1WBWghtQ9() []int16 {
	out := make([]int16, len(silk_NLSF_CB1_WB_Wght_Q9))
	for i, v := range silk_NLSF_CB1_WB_Wght_Q9 {
		out[i] = int16(v)
	}
	return out
}

func ExportTestSilkTable_CBLagsStage2() []int8 {
	out := make([]int8, 0, PE_MAX_NB_SUBFR*PE_NB_CBKS_STAGE2_EXT)
	for _, row := range silk_CB_lags_stage2 {
		for _, v := range row {
			out = append(out, int8(v))
		}
	}
	return out
}
func ExportTestSilkTable_CBLagsStage3() []int8 {
	out := make([]int8, 0, PE_MAX_NB_SUBFR*PE_NB_CBKS_STAGE3_MAX)
	for _, row := range silk_CB_lags_stage3 {
		for _, v := range row {
			out = append(out, int8(v))
		}
	}
	return out
}

// Resampler tables.
func ExportTestSilkTable_Resampler_3_4_COEFS() []int16 {
	out := make([]int16, len(silk_Resampler_3_4_COEFS))
	for i, v := range silk_Resampler_3_4_COEFS {
		out[i] = int16(v)
	}
	return out
}
func ExportTestSilkTable_Resampler_1_2_COEFS() []int16 {
	out := make([]int16, len(silk_Resampler_1_2_COEFS))
	for i, v := range silk_Resampler_1_2_COEFS {
		out[i] = int16(v)
	}
	return out
}
func ExportTestSilkTable_ResamplerFracFIR12() []int16 {
	out := make([]int16, 0, 12*RESAMPLER_ORDER_FIR_12/2)
	for _, row := range silk_resampler_frac_FIR_12 {
		for _, v := range row {
			out = append(out, int16(v))
		}
	}
	return out
}

// Shell code tables.
func ExportTestSilkTable_ShellCodeTable0() []byte {
	return append([]byte(nil), silk_shell_code_table0[:]...)
}
func ExportTestSilkTable_ShellCodeTable3() []byte {
	return append([]byte(nil), silk_shell_code_table3[:]...)
}

// Transition LP filter coeffs.
func ExportTestSilkTable_TransitionLPBQ28() []int32 {
	out := make([]int32, 0, TRANSITION_INT_NUM*TRANSITION_NB)
	for _, row := range silk_Transition_LP_B_Q28 {
		for _, v := range row {
			out = append(out, int32(v))
		}
	}
	return out
}
