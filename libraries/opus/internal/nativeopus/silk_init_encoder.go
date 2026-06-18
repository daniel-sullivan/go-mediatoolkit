package nativeopus

// Port of libopus/silk/init_encoder.c.
//
// Initialize a float-side SILK encoder state. This is the
// silk_encoder_state_FLP variant (silk_encoder_state_Fxx) since our
// build is float-only.

// silk_init_encoder — C: init_encoder.c:46-68.
func silk_init_encoder(psEnc *silk_encoder_state_FLP, arch int) opus_int {
	// Zero the entire state.
	*psEnc = silk_encoder_state_FLP{}

	psEnc.sCmn.arch = arch

	psEnc.sCmn.variable_HP_smth1_Q15 = silk_LSHIFT(
		silk_lin2log(opus_int32(SILK_FIX_CONST(VARIABLE_HP_MIN_CUTOFF_HZ, 16)))-(16<<7),
		8)
	psEnc.sCmn.variable_HP_smth2_Q15 = psEnc.sCmn.variable_HP_smth1_Q15

	// Deactivate LSF interpolation and pitch prediction on first frame.
	psEnc.sCmn.first_frame_after_reset = 1

	ret := silk_VAD_Init(&psEnc.sCmn.sVAD)
	return ret
}
