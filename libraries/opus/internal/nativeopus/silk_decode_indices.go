package nativeopus

// Port of libopus/silk/decode_indices.c.

// silk_decode_indices — decode side-information parameters from payload.
// C: decode_indices.c:35-151.
func silk_decode_indices(psDec *silk_decoder_state, psRangeDec *ec_dec,
	FrameIndex opus_int, decode_LBRR opus_int, condCoding opus_int) {

	var ec_ix [MAX_LPC_ORDER]opus_int16
	var pred_Q8 [MAX_LPC_ORDER]opus_uint8
	var Ix opus_int

	// Decode signal type and quantizer offset.
	if decode_LBRR != 0 || psDec.VAD_flags[FrameIndex] != 0 {
		Ix = ec_dec_icdf(psRangeDec, asByteSlice(silk_type_offset_VAD_iCDF[:]), 8) + 2
	} else {
		Ix = ec_dec_icdf(psRangeDec, asByteSlice(silk_type_offset_no_VAD_iCDF[:]), 8)
	}
	psDec.indices.signalType = opus_int8(silk_RSHIFT(opus_int32(Ix), 1))
	psDec.indices.quantOffsetType = opus_int8(Ix & 1)

	// Decode gains.
	// First subframe.
	if condCoding == CODE_CONDITIONALLY {
		psDec.indices.GainsIndices[0] = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_delta_gain_iCDF[:]), 8))
	} else {
		psDec.indices.GainsIndices[0] = opus_int8(silk_LSHIFT(opus_int32(
			ec_dec_icdf(psRangeDec, asByteSlice(silk_gain_iCDF[psDec.indices.signalType][:]), 8)), 3))
		psDec.indices.GainsIndices[0] += opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_uniform8_iCDF[:]), 8))
	}

	// Remaining subframes.
	for i := opus_int(1); i < psDec.nb_subfr; i++ {
		psDec.indices.GainsIndices[i] = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_delta_gain_iCDF[:]), 8))
	}

	// Decode LSF indices.
	cb1Offset := opus_int(psDec.indices.signalType>>1) * opus_int(psDec.psNLSF_CB.nVectors)
	psDec.indices.NLSFIndices[0] = opus_int8(ec_dec_icdf(psRangeDec,
		asByteSlice(psDec.psNLSF_CB.CB1_iCDF[cb1Offset:]), 8))
	silk_NLSF_unpack(ec_ix[:], pred_Q8[:], psDec.psNLSF_CB, opus_int(psDec.indices.NLSFIndices[0]))
	celt_assert(psDec.psNLSF_CB.order == opus_int16(psDec.LPC_order))
	for i := opus_int(0); i < opus_int(psDec.psNLSF_CB.order); i++ {
		Ix = ec_dec_icdf(psRangeDec, asByteSlice(psDec.psNLSF_CB.ec_iCDF[ec_ix[i]:]), 8)
		if Ix == 0 {
			Ix -= ec_dec_icdf(psRangeDec, asByteSlice(silk_NLSF_EXT_iCDF[:]), 8)
		} else if Ix == 2*NLSF_QUANT_MAX_AMPLITUDE {
			Ix += ec_dec_icdf(psRangeDec, asByteSlice(silk_NLSF_EXT_iCDF[:]), 8)
		}
		psDec.indices.NLSFIndices[i+1] = opus_int8(Ix - NLSF_QUANT_MAX_AMPLITUDE)
	}

	// Decode LSF interpolation factor.
	if psDec.nb_subfr == MAX_NB_SUBFR {
		psDec.indices.NLSFInterpCoef_Q2 = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_NLSF_interpolation_factor_iCDF[:]), 8))
	} else {
		psDec.indices.NLSFInterpCoef_Q2 = 4
	}

	if psDec.indices.signalType == TYPE_VOICED {
		// Decode pitch lags.
		decode_absolute_lagIndex := opus_int(1)
		if condCoding == CODE_CONDITIONALLY && psDec.ec_prevSignalType == TYPE_VOICED {
			delta_lagIndex := opus_int16(ec_dec_icdf(psRangeDec, asByteSlice(silk_pitch_delta_iCDF[:]), 8))
			if delta_lagIndex > 0 {
				delta_lagIndex = delta_lagIndex - 9
				psDec.indices.lagIndex = psDec.ec_prevLagIndex + delta_lagIndex
				decode_absolute_lagIndex = 0
			}
		}
		if decode_absolute_lagIndex != 0 {
			psDec.indices.lagIndex = opus_int16(ec_dec_icdf(psRangeDec, asByteSlice(silk_pitch_lag_iCDF[:]), 8)) *
				opus_int16(silk_RSHIFT(opus_int32(psDec.fs_kHz), 1))
			psDec.indices.lagIndex += opus_int16(ec_dec_icdf(psRangeDec, asByteSlice(psDec.pitch_lag_low_bits_iCDF), 8))
		}
		psDec.ec_prevLagIndex = psDec.indices.lagIndex

		// Get contour index.
		psDec.indices.contourIndex = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(psDec.pitch_contour_iCDF), 8))

		// Decode LTP gains.
		psDec.indices.PERIndex = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_LTP_per_index_iCDF[:]), 8))

		for k := opus_int(0); k < psDec.nb_subfr; k++ {
			psDec.indices.LTPIndex[k] = opus_int8(ec_dec_icdf(psRangeDec,
				asByteSlice(silk_LTP_gain_iCDF_ptrs[psDec.indices.PERIndex]), 8))
		}

		// Decode LTP scaling.
		if condCoding == CODE_INDEPENDENTLY {
			psDec.indices.LTP_scaleIndex = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_LTPscale_iCDF[:]), 8))
		} else {
			psDec.indices.LTP_scaleIndex = 0
		}
	}
	psDec.ec_prevSignalType = opus_int(psDec.indices.signalType)

	// Decode seed.
	psDec.indices.Seed = opus_int8(ec_dec_icdf(psRangeDec, asByteSlice(silk_uniform4_iCDF[:]), 8))
}
