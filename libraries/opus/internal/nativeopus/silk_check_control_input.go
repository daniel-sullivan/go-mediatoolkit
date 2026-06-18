package nativeopus

// Port of libopus/silk/check_control_input.c.
//
// Validates a silk_EncControlStruct pointer for SILK-encoder-friendly
// values. Returns SILK_NO_ERROR on success or a negative SILK_ENC_*
// code identifying the first violated invariant. Checks are performed
// in the same order as the C source so the first-fail diagnostic is
// identical.

// check_control_input mirrors C: check_control_input.c.
func check_control_input(encControl *silk_EncControlStruct) opus_int {
	celt_assert(encControl != nil)

	if (encControl.API_sampleRate != 8000 &&
		encControl.API_sampleRate != 12000 &&
		encControl.API_sampleRate != 16000 &&
		encControl.API_sampleRate != 24000 &&
		encControl.API_sampleRate != 32000 &&
		encControl.API_sampleRate != 44100 &&
		encControl.API_sampleRate != 48000) ||
		(encControl.desiredInternalSampleRate != 8000 &&
			encControl.desiredInternalSampleRate != 12000 &&
			encControl.desiredInternalSampleRate != 16000) ||
		(encControl.maxInternalSampleRate != 8000 &&
			encControl.maxInternalSampleRate != 12000 &&
			encControl.maxInternalSampleRate != 16000) ||
		(encControl.minInternalSampleRate != 8000 &&
			encControl.minInternalSampleRate != 12000 &&
			encControl.minInternalSampleRate != 16000) ||
		encControl.minInternalSampleRate > encControl.desiredInternalSampleRate ||
		encControl.maxInternalSampleRate < encControl.desiredInternalSampleRate ||
		encControl.minInternalSampleRate > encControl.maxInternalSampleRate {
		return SILK_ENC_FS_NOT_SUPPORTED
	}
	if encControl.payloadSize_ms != 10 &&
		encControl.payloadSize_ms != 20 &&
		encControl.payloadSize_ms != 40 &&
		encControl.payloadSize_ms != 60 {
		return SILK_ENC_PACKET_SIZE_NOT_SUPPORTED
	}
	if encControl.packetLossPercentage < 0 || encControl.packetLossPercentage > 100 {
		return SILK_ENC_INVALID_LOSS_RATE
	}
	if encControl.useDTX < 0 || encControl.useDTX > 1 {
		return SILK_ENC_INVALID_DTX_SETTING
	}
	if encControl.useCBR < 0 || encControl.useCBR > 1 {
		return SILK_ENC_INVALID_CBR_SETTING
	}
	if encControl.useInBandFEC < 0 || encControl.useInBandFEC > 1 {
		return SILK_ENC_INVALID_INBAND_FEC_SETTING
	}
	if encControl.nChannelsAPI < 1 || encControl.nChannelsAPI > ENCODER_NUM_CHANNELS {
		return SILK_ENC_INVALID_NUMBER_OF_CHANNELS_ERROR
	}
	if encControl.nChannelsInternal < 1 || encControl.nChannelsInternal > ENCODER_NUM_CHANNELS {
		return SILK_ENC_INVALID_NUMBER_OF_CHANNELS_ERROR
	}
	if encControl.nChannelsInternal > encControl.nChannelsAPI {
		return SILK_ENC_INVALID_NUMBER_OF_CHANNELS_ERROR
	}
	if encControl.complexity < 0 || encControl.complexity > 10 {
		return SILK_ENC_INVALID_COMPLEXITY_SETTING
	}
	return SILK_NO_ERROR
}
