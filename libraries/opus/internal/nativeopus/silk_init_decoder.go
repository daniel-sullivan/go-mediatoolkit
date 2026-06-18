package nativeopus

// Port of libopus/silk/init_decoder.c + decoder_set_fs.c.
//
// ENABLE_OSCE / ENABLE_DEEP_PLC are disabled in our build, so their
// per-feature blocks are omitted.

// silk_reset_decoder — clear the full decoder state except for fields
// outside the reset region (currently none in our configuration), then
// re-seed the CNG/PLC and record arch.
//
// C: init_decoder.c:43-67.
func silk_reset_decoder(psDec *silk_decoder_state) opus_int {
	psDec.reset_from_prev_gain_Q16()

	psDec.first_frame_after_reset = 1
	psDec.prev_gain_Q16 = 65536
	// C calls opus_select_arch(); our float port uses arch = 0 throughout.
	psDec.arch = 0

	silk_CNG_Reset(psDec)
	silk_PLC_Reset(psDec)

	return 0
}

// silk_init_decoder — zero-initialize the decoder state then reset it.
// C: init_decoder.c:73-83.
func silk_init_decoder(psDec *silk_decoder_state) opus_int {
	*psDec = silk_decoder_state{}
	silk_reset_decoder(psDec)
	return 0
}

// silk_decoder_set_fs — update internal + API sampling rates.
// C: decoder_set_fs.c:35-107.
func silk_decoder_set_fs(psDec *silk_decoder_state, fs_kHz opus_int, fs_API_Hz opus_int32) opus_int {
	var frame_length opus_int
	var ret opus_int

	celt_assert(fs_kHz == 8 || fs_kHz == 12 || fs_kHz == 16)
	celt_assert(psDec.nb_subfr == MAX_NB_SUBFR || psDec.nb_subfr == MAX_NB_SUBFR/2)

	// New (sub)frame length.
	psDec.subfr_length = opus_int(silk_SMULBB(SUB_FRAME_LENGTH_MS, opus_int32(fs_kHz)))
	frame_length = opus_int(silk_SMULBB(opus_int32(psDec.nb_subfr), opus_int32(psDec.subfr_length)))

	// Re-init resampler when switching internal or external sampling rate.
	if psDec.fs_kHz != fs_kHz || psDec.fs_API_hz != fs_API_Hz {
		ret += silk_resampler_init(&psDec.resampler_state,
			silk_SMULBB(opus_int32(fs_kHz), 1000), fs_API_Hz, 0)
		psDec.fs_API_hz = fs_API_Hz
	}

	if psDec.fs_kHz != fs_kHz || frame_length != psDec.frame_length {
		if fs_kHz == 8 {
			if psDec.nb_subfr == MAX_NB_SUBFR {
				psDec.pitch_contour_iCDF = silk_pitch_contour_NB_iCDF[:]
			} else {
				psDec.pitch_contour_iCDF = silk_pitch_contour_10_ms_NB_iCDF[:]
			}
		} else {
			if psDec.nb_subfr == MAX_NB_SUBFR {
				psDec.pitch_contour_iCDF = silk_pitch_contour_iCDF[:]
			} else {
				psDec.pitch_contour_iCDF = silk_pitch_contour_10_ms_iCDF[:]
			}
		}
		if psDec.fs_kHz != fs_kHz {
			psDec.ltp_mem_length = opus_int(silk_SMULBB(LTP_MEM_LENGTH_MS, opus_int32(fs_kHz)))
			if fs_kHz == 8 || fs_kHz == 12 {
				psDec.LPC_order = MIN_LPC_ORDER
				psDec.psNLSF_CB = &silk_NLSF_CB_NB_MB
			} else {
				psDec.LPC_order = MAX_LPC_ORDER
				psDec.psNLSF_CB = &silk_NLSF_CB_WB
			}
			switch fs_kHz {
			case 16:
				psDec.pitch_lag_low_bits_iCDF = silk_uniform8_iCDF[:]
			case 12:
				psDec.pitch_lag_low_bits_iCDF = silk_uniform6_iCDF[:]
			case 8:
				psDec.pitch_lag_low_bits_iCDF = silk_uniform4_iCDF[:]
			default:
				celt_assert(false)
			}
			psDec.first_frame_after_reset = 1
			psDec.lagPrev = 100
			psDec.LastGainIndex = 10
			psDec.prevSignalType = TYPE_NO_VOICE_ACTIVITY
			psDec.outBuf = [MAX_FRAME_LENGTH + 2*MAX_SUB_FRAME_LENGTH]opus_int16{}
			psDec.sLPC_Q14_buf = [MAX_LPC_ORDER]opus_int32{}
		}

		psDec.fs_kHz = fs_kHz
		psDec.frame_length = frame_length
	}

	celt_assert(psDec.frame_length > 0 && psDec.frame_length <= MAX_FRAME_LENGTH)
	return ret
}
