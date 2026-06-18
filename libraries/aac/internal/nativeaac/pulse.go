// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// AAC pulse-data tool: CPulseData_Read and CPulseData_Apply, ported 1:1 from
// libAACdec/src/pulsedata.cpp. Pulse data is only legal in long blocks; it adds
// small integer offsets to the quantized spectrum before inverse quantization.
// Integer kernels — bit-identical regardless of build.

// nMaxLines ports N_MAX_LINES (pulsedata.h:109): the max pulse count + 1.
const nMaxLines = 4

// cPulseData ports the CPulseData struct (pulsedata.h:113).
type cPulseData struct {
	PulseDataPresent uint8
	NumberPulse      uint8
	PulseStartBand   uint8
	PulseOffset      [nMaxLines]uint8
	PulseAmp         [nMaxLines]uint8
}

// cPulseDataRead ports CPulseData_Read (pulsedata.cpp:107): read the pulse side
// info. sfbStartLines is GetScaleFactorBandOffsets for the (long) block;
// maxSfBands is GetScaleFactorBandsTransmitted; frameLength is the granule
// length. Returns 0 / AAC_DEC_DECODE_FRAME_ERROR.
func cPulseDataRead(bs *bitStream, pulseData *cPulseData, sfbStartLines []int16,
	maxSfBands int, isLong bool, frameLength int) aacDecoderError {
	k := 0

	// reset pulse data flag
	pulseData.PulseDataPresent = 0

	pulseData.PulseDataPresent = uint8(bs.readBit())
	if pulseData.PulseDataPresent != 0 {
		if !isLong {
			return aacDecDecodeFrameError
		}

		pulseData.NumberPulse = uint8(bs.readBits(2))
		pulseData.PulseStartBand = uint8(bs.readBits(6))

		if int(pulseData.PulseStartBand) >= maxSfBands {
			return aacDecDecodeFrameError
		}

		k = int(sfbStartLines[pulseData.PulseStartBand])

		for i := 0; i <= int(pulseData.NumberPulse); i++ {
			pulseData.PulseOffset[i] = uint8(bs.readBits(5))
			pulseData.PulseAmp[i] = uint8(bs.readBits(4))
			k += int(pulseData.PulseOffset[i])
		}

		if k >= frameLength {
			return aacDecDecodeFrameError
		}
	}

	return aacDecOK
}

// cPulseDataApply ports CPulseData_Apply (pulsedata.cpp:145): add the pulse
// amplitudes to the quantized spectrum (long block only). pScaleFactorBandOffsets
// is the long-block sfb offset table; coef the quantized MDCT lines.
func cPulseDataApply(pulseData *cPulseData, pScaleFactorBandOffsets []int16, coef []int32) {
	if pulseData.PulseDataPresent != 0 {
		k := int(pScaleFactorBandOffsets[pulseData.PulseStartBand])

		for i := 0; i <= int(pulseData.NumberPulse); i++ {
			k += int(pulseData.PulseOffset[i])
			if coef[k] > 0 {
				coef[k] += int32(pulseData.PulseAmp[i])
			} else {
				coef[k] -= int32(pulseData.PulseAmp[i])
			}
		}
	}
}
