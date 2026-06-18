package nativeopus

// Port of libopus/silk/control_audio_bandwidth.c.
//
// State machine for switching the encoder's internal sampling rate
// (kHz) in response to external rate constraints and bandwidth-switch
// flags.

// silk_control_audio_bandwidth — C: control_audio_bandwidth.c:36-132.
func silk_control_audio_bandwidth(psEncC *silk_encoder_state, encControl *silk_EncControlStruct) opus_int {
	orig_kHz := psEncC.fs_kHz
	// Handle a bandwidth-switching reset where we need to be aware what
	// the last sampling rate was.
	if orig_kHz == 0 {
		orig_kHz = opus_int(psEncC.sLP.saved_fs_kHz)
	}
	fs_kHz := orig_kHz
	fs_Hz := silk_SMULBB(opus_int32(fs_kHz), 1000)
	if fs_Hz == 0 {
		// Encoder has just been initialized.
		fs_Hz = silk_min(opus_int32(psEncC.desiredInternal_fs_Hz), psEncC.API_fs_Hz)
		fs_kHz = opus_int(silk_DIV32_16(fs_Hz, 1000))
	} else if fs_Hz > psEncC.API_fs_Hz ||
		fs_Hz > opus_int32(psEncC.maxInternal_fs_Hz) ||
		fs_Hz < opus_int32(psEncC.minInternal_fs_Hz) {
		// Clamp to [min, min(API, max)].
		fs_Hz = psEncC.API_fs_Hz
		fs_Hz = silk_min(fs_Hz, opus_int32(psEncC.maxInternal_fs_Hz))
		fs_Hz = silk_max(fs_Hz, opus_int32(psEncC.minInternal_fs_Hz))
		fs_kHz = opus_int(silk_DIV32_16(fs_Hz, 1000))
	} else {
		// Sampling rate transition state machine.
		if psEncC.sLP.transition_frame_no >= TRANSITION_FRAMES {
			psEncC.sLP.mode = 0
		}
		if psEncC.allow_bandwidth_switch != 0 || encControl.opusCanSwitch != 0 {
			if silk_SMULBB(opus_int32(orig_kHz), 1000) > opus_int32(psEncC.desiredInternal_fs_Hz) {
				// Switch down.
				if psEncC.sLP.mode == 0 {
					psEncC.sLP.transition_frame_no = TRANSITION_FRAMES
					psEncC.sLP.In_LP_State = [2]opus_int32{}
				}
				if encControl.opusCanSwitch != 0 {
					psEncC.sLP.mode = 0
					if orig_kHz == 16 {
						fs_kHz = 12
					} else {
						fs_kHz = 8
					}
				} else {
					if psEncC.sLP.transition_frame_no <= 0 {
						encControl.switchReady = 1
						encControl.maxBits -= encControl.maxBits * 5 / (encControl.payloadSize_ms + 5)
					} else {
						psEncC.sLP.mode = -2
					}
				}
			} else if silk_SMULBB(opus_int32(orig_kHz), 1000) < opus_int32(psEncC.desiredInternal_fs_Hz) {
				// Switch up.
				if encControl.opusCanSwitch != 0 {
					if orig_kHz == 8 {
						fs_kHz = 12
					} else {
						fs_kHz = 16
					}
					psEncC.sLP.transition_frame_no = 0
					psEncC.sLP.In_LP_State = [2]opus_int32{}
					psEncC.sLP.mode = 1
				} else {
					if psEncC.sLP.mode == 0 {
						encControl.switchReady = 1
						encControl.maxBits -= encControl.maxBits * 5 / (encControl.payloadSize_ms + 5)
					} else {
						psEncC.sLP.mode = 1
					}
				}
			} else {
				if psEncC.sLP.mode < 0 {
					psEncC.sLP.mode = 1
				}
			}
		}
	}

	return fs_kHz
}
