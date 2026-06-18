package nativeopus

// Port of libopus/silk/encode_indices.c.
//
// Entropy-code all per-frame side information to the range encoder:
// signal type, gain indices, NLSF indices, pitch lags + contour,
// LTP gain codebook indices, LTP scaling and the rand seed.

// silk_encode_indices — C: encode_indices.c:35-181.
func silk_encode_indices(
	psEncC *silk_encoder_state,
	psRangeEnc *ec_enc,
	FrameIndex opus_int,
	encode_LBRR opus_int,
	condCoding opus_int,
) {
	var ec_ix [MAX_LPC_ORDER]opus_int16
	var pred_Q8 [MAX_LPC_ORDER]opus_uint8

	var psIndices *SideInfoIndices
	if encode_LBRR != 0 {
		psIndices = &psEncC.indices_LBRR[FrameIndex]
	} else {
		psIndices = &psEncC.indices
	}

	// Signal type and quantizer offset.
	typeOffset := 2*opus_int(psIndices.signalType) + opus_int(psIndices.quantOffsetType)
	celt_assert(typeOffset >= 0 && typeOffset < 6)
	celt_assert(encode_LBRR == 0 || typeOffset >= 2)
	if encode_LBRR != 0 || typeOffset >= 2 {
		ec_enc_icdf(psRangeEnc, int(typeOffset-2), asByteSlice(silk_type_offset_VAD_iCDF[:]), 8)
	} else {
		ec_enc_icdf(psRangeEnc, int(typeOffset), asByteSlice(silk_type_offset_no_VAD_iCDF[:]), 8)
	}

	// Encode gains.
	if condCoding == CODE_CONDITIONALLY {
		silk_assert(psIndices.GainsIndices[0] >= 0 && psIndices.GainsIndices[0] < MAX_DELTA_GAIN_QUANT-MIN_DELTA_GAIN_QUANT+1)
		ec_enc_icdf(psRangeEnc, int(psIndices.GainsIndices[0]), asByteSlice(silk_delta_gain_iCDF[:]), 8)
	} else {
		silk_assert(psIndices.GainsIndices[0] >= 0 && psIndices.GainsIndices[0] < N_LEVELS_QGAIN)
		ec_enc_icdf(psRangeEnc, int(silk_RSHIFT(opus_int32(psIndices.GainsIndices[0]), 3)),
			asByteSlice(silk_gain_iCDF[psIndices.signalType][:]), 8)
		ec_enc_icdf(psRangeEnc, int(psIndices.GainsIndices[0]&7), asByteSlice(silk_uniform8_iCDF[:]), 8)
	}
	for i := opus_int(1); i < psEncC.nb_subfr; i++ {
		silk_assert(psIndices.GainsIndices[i] >= 0 && psIndices.GainsIndices[i] < MAX_DELTA_GAIN_QUANT-MIN_DELTA_GAIN_QUANT+1)
		ec_enc_icdf(psRangeEnc, int(psIndices.GainsIndices[i]), asByteSlice(silk_delta_gain_iCDF[:]), 8)
	}

	// Encode NLSFs.
	ec_enc_icdf(psRangeEnc, int(psIndices.NLSFIndices[0]),
		psEncC.psNLSF_CB.CB1_iCDF[(psIndices.signalType>>1)*opus_int8(psEncC.psNLSF_CB.nVectors):], 8)
	silk_NLSF_unpack(ec_ix[:], pred_Q8[:], psEncC.psNLSF_CB, opus_int(psIndices.NLSFIndices[0]))
	celt_assert(psEncC.psNLSF_CB.order == opus_int16(psEncC.predictLPCOrder))
	for i := opus_int(0); i < opus_int(psEncC.psNLSF_CB.order); i++ {
		v := psIndices.NLSFIndices[i+1]
		switch {
		case v >= NLSF_QUANT_MAX_AMPLITUDE:
			ec_enc_icdf(psRangeEnc, 2*NLSF_QUANT_MAX_AMPLITUDE,
				psEncC.psNLSF_CB.ec_iCDF[ec_ix[i]:], 8)
			ec_enc_icdf(psRangeEnc, int(v-NLSF_QUANT_MAX_AMPLITUDE),
				asByteSlice(silk_NLSF_EXT_iCDF[:]), 8)
		case v <= -NLSF_QUANT_MAX_AMPLITUDE:
			ec_enc_icdf(psRangeEnc, 0, psEncC.psNLSF_CB.ec_iCDF[ec_ix[i]:], 8)
			ec_enc_icdf(psRangeEnc, int(-v-NLSF_QUANT_MAX_AMPLITUDE),
				asByteSlice(silk_NLSF_EXT_iCDF[:]), 8)
		default:
			ec_enc_icdf(psRangeEnc, int(v+NLSF_QUANT_MAX_AMPLITUDE),
				psEncC.psNLSF_CB.ec_iCDF[ec_ix[i]:], 8)
		}
	}

	// Encode NLSF interpolation factor.
	if psEncC.nb_subfr == MAX_NB_SUBFR {
		silk_assert(psIndices.NLSFInterpCoef_Q2 >= 0 && psIndices.NLSFInterpCoef_Q2 < 5)
		ec_enc_icdf(psRangeEnc, int(psIndices.NLSFInterpCoef_Q2),
			asByteSlice(silk_NLSF_interpolation_factor_iCDF[:]), 8)
	}

	if psIndices.signalType == TYPE_VOICED {
		// Pitch lag index.
		encode_absolute_lagIndex := opus_int(1)
		if condCoding == CODE_CONDITIONALLY && psEncC.ec_prevSignalType == TYPE_VOICED {
			delta_lagIndex := opus_int(psIndices.lagIndex) - opus_int(psEncC.ec_prevLagIndex)
			if delta_lagIndex < -8 || delta_lagIndex > 11 {
				delta_lagIndex = 0
			} else {
				delta_lagIndex += 9
				encode_absolute_lagIndex = 0
			}
			silk_assert(delta_lagIndex >= 0 && delta_lagIndex < 21)
			ec_enc_icdf(psRangeEnc, int(delta_lagIndex), asByteSlice(silk_pitch_delta_iCDF[:]), 8)
		}
		if encode_absolute_lagIndex != 0 {
			pitch_high_bits := silk_DIV32_16(opus_int32(psIndices.lagIndex), silk_RSHIFT(opus_int32(psEncC.fs_kHz), 1))
			pitch_low_bits := opus_int32(psIndices.lagIndex) - silk_SMULBB(pitch_high_bits, silk_RSHIFT(opus_int32(psEncC.fs_kHz), 1))
			silk_assert(pitch_low_bits < opus_int32(psEncC.fs_kHz/2))
			silk_assert(pitch_high_bits < 32)
			ec_enc_icdf(psRangeEnc, int(pitch_high_bits), asByteSlice(silk_pitch_lag_iCDF[:]), 8)
			ec_enc_icdf(psRangeEnc, int(pitch_low_bits), psEncC.pitch_lag_low_bits_iCDF, 8)
		}
		psEncC.ec_prevLagIndex = psIndices.lagIndex

		// Contour index.
		silk_assert(psIndices.contourIndex >= 0)
		silk_assert((psIndices.contourIndex < 34 && psEncC.fs_kHz > 8 && psEncC.nb_subfr == 4) ||
			(psIndices.contourIndex < 11 && psEncC.fs_kHz == 8 && psEncC.nb_subfr == 4) ||
			(psIndices.contourIndex < 12 && psEncC.fs_kHz > 8 && psEncC.nb_subfr == 2) ||
			(psIndices.contourIndex < 3 && psEncC.fs_kHz == 8 && psEncC.nb_subfr == 2))
		ec_enc_icdf(psRangeEnc, int(psIndices.contourIndex), psEncC.pitch_contour_iCDF, 8)

		// LTP gain codebook indices.
		silk_assert(psIndices.PERIndex >= 0 && psIndices.PERIndex < 3)
		ec_enc_icdf(psRangeEnc, int(psIndices.PERIndex), asByteSlice(silk_LTP_per_index_iCDF[:]), 8)

		for k := opus_int(0); k < psEncC.nb_subfr; k++ {
			silk_assert(psIndices.LTPIndex[k] >= 0 && opus_int(psIndices.LTPIndex[k]) < (8<<opus_int(psIndices.PERIndex)))
			ec_enc_icdf(psRangeEnc, int(psIndices.LTPIndex[k]),
				asByteSlice(silk_LTP_gain_iCDF_ptrs[psIndices.PERIndex]), 8)
		}

		// LTP scaling.
		if condCoding == CODE_INDEPENDENTLY {
			silk_assert(psIndices.LTP_scaleIndex >= 0 && psIndices.LTP_scaleIndex < 3)
			ec_enc_icdf(psRangeEnc, int(psIndices.LTP_scaleIndex), asByteSlice(silk_LTPscale_iCDF[:]), 8)
		}
		silk_assert(condCoding == 0 || psIndices.LTP_scaleIndex == 0)
	}

	psEncC.ec_prevSignalType = opus_int(psIndices.signalType)

	// Rand seed.
	silk_assert(psIndices.Seed >= 0 && psIndices.Seed < 4)
	ec_enc_icdf(psRangeEnc, int(psIndices.Seed), asByteSlice(silk_uniform4_iCDF[:]), 8)
}
