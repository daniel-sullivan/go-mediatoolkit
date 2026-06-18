// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// AAC-LC raw_data_block decode driver: the element-list read state machine
// (CChannelElement_Read, channel.cpp:414) restricted to the AAC-LC SCE/CPE
// sequences, the per-channel decode orchestration (CChannelElement_Decode,
// channel.cpp:162), and the top-level frame loop (CAacDecoder_DecodeFrame +
// the aacDecoder_DecodeFrame output stage, aacdecoder.cpp / aacdecoder_lib.cpp)
// — assembled to turn one access unit into interleaved int16 PCM.
//
// Scope: AAC-LC (AOT 2), single-element streams (one SCE or one CPE per access
// unit, plus a terminating ID_END), no SBR/PS/MPS/DRC/PNS-noise, no error
// resilience (HCR/RVLC), no gain control. flags == 0 throughout. These are the
// only paths the codec/aac decoder needs for .m4a AAC-LC; the unsupported
// syntaxes return ErrUnsupported so the caller can surface a clean error.

// Syntactic element IDs, ported 1:1 from the MP4_ELEMENT_ID enum
// (FDK_audio.h, ID_SCE/ID_CPE/...). Only the AAC-LC subset is modelled.
const (
	idSCE = 0 // single_channel_element
	idCPE = 1 // channel_pair_element
	idCCE = 2 // coupling_channel_element
	idLFE = 3 // lfe_channel_element
	idDSE = 4 // data_stream_element
	idPCE = 5 // program_config_element
	idFIL = 6 // fill_element
	idEND = 7 // END
)

// channelData holds the per-channel parsed state CChannelElement_Read fills and
// CChannelElement_Decode consumes — the AAC-LC subset of CAacDecoderChannelInfo.
type channelData struct {
	ics         cIcsInfo
	globalGain  uint8
	codeBook    [8 * 16]uint8 // pDynData->aCodeBook (section codebooks)
	scaleFactor [8 * 16]int16 // pDynData->aScaleFactor
	sfbScale    [8 * 16]int16 // pDynData->aSfbScale
	specScale   [8]int16      // specScale (per-window block exponent)
	spectrum    []int32       // pSpectralCoefficient (flat window-major)
	tns         CTnsData
	pulse       cPulseData
	pns         cPnsData
	timePCM     []int32 // per-channel planar PCM_DEC (int32) time output
}

// channelState is the persistent per-output-channel state across frames: the
// IMDCT overlap-add handle (CAacDecoderStaticChannelInfo->IMdct).
type channelState struct {
	mdct    mdctT
	overlap [768]int32 // pOverlapBuffer, OverlapBufferSize == 768
}

func newChannelState() *channelState {
	s := new(channelState)
	mdctInit(&s.mdct, s.overlap[:], 768)
	return s
}

// readSingleChannelElement ports the AAC-LC SCE element-list sequence (el_aac_sce,
// FDK_tools_rom.cpp:6547): global_gain, ics_info, section_data, scale_factor_data,
// pulse, tns_data_present, tns_data, gain_control_data_present, spectral_data —
// then the final CBlock_InverseQuantizeSpectralData (channel.cpp:890). CRC regs
// and element_instance_tag are read but ignored. granuleLength is the SPEC stride.
func readSingleChannelElement(bs *bitStream, ch *channelData, sri *samplingRateInfo,
	frameLength int, flags uint32) aacDecoderError {
	// element_instance_tag (4 bits) — ignored for decode.
	bs.readBits(4)
	return readOneChannel(bs, ch, sri, frameLength, flags, 0)
}

// readChannelPairElement ports the AAC-LC CPE element-list sequence (el_aac_cpe +
// cpe0/cpe1, FDK_tools_rom.cpp:6568): element_instance_tag, common_window, then
// per common_window either two independent channels (cpe0) or a shared ics_info +
// ms + two channels (cpe1).
func readChannelPairElement(bs *bitStream, l, r *channelData, commonWindow *uint8,
	jsd *JointStereoData, sri *samplingRateInfo, frameLength int, flags uint32) aacDecoderError {
	// element_instance_tag (4 bits) — ignored.
	bs.readBits(4)

	// common_window (1 bit).
	cw := uint8(bs.readBits(1))
	*commonWindow = cw

	if cw == 0 {
		// cpe0: two fully-independent channels.
		if err := readOneChannel(bs, l, sri, frameLength, flags, 0); err != aacDecOK {
			return err
		}
		return readOneChannel(bs, r, sri, frameLength, flags, 0)
	}

	// cpe1: shared ics_info + ms mask, then per-channel data.
	// ics_info (channel.cpp:488 → IcsRead) into L, copied to R.
	if err := icsRead(bs, &l.ics, sri, flags); err != aacDecOK {
		return err
	}
	r.ics = l.ics

	// ms (channel.cpp:532 → CJointStereo_Read) — AAC-LC subset.
	if err := jointStereoRead(bs, jsd, getWindowGroups(&l.ics),
		getScaleFactorBandsTransmitted(&l.ics), flags); err != aacDecOK {
		return err
	}

	// max_sfb_ste mirrors GetScaleMaxFactorBandsTransmitted (channel.cpp:537).
	maxSfbSte := getScaleFactorBandsTransmitted(&l.ics)
	if rb := getScaleFactorBandsTransmitted(&r.ics); rb > maxSfbSte {
		maxSfbSte = rb
	}
	l.ics.maxSfbSte = uint8(maxSfbSte)
	r.ics.maxSfbSte = uint8(maxSfbSte)

	// Channel L body: global_gain, section_data, scale_factor_data, pulse,
	// tns_data_present, tns_data, gain_control_data_present, spectral_data.
	if err := readOneChannelBody(bs, l, sri, frameLength, flags, 1); err != aacDecOK {
		return err
	}
	// Channel R body (same sequence after next_channel + adtscrc_start_reg2).
	if err := readOneChannelBody(bs, r, sri, frameLength, flags, 1); err != aacDecOK {
		return err
	}

	// Final inverse-quantize both channels (channel.cpp:890).
	if err := inverseQuantizeChannel(l, sri, frameLength); err != aacDecOK {
		return err
	}
	return inverseQuantizeChannel(r, sri, frameLength)
}

// readOneChannel reads a full independent channel (ics_info + body + final
// inverse-quantize). commonWindow gates the intensity-codebook legality check.
func readOneChannel(bs *bitStream, ch *channelData, sri *samplingRateInfo,
	frameLength int, flags uint32, commonWindow uint8) aacDecoderError {
	// global_gain (channel.cpp:578) — but el_aac_sce orders global_gain BEFORE
	// ics_info. We follow the SCE order: global_gain, then ics_info.
	ch.globalGain = uint8(bs.readBits(8))

	if err := icsRead(bs, &ch.ics, sri, flags); err != aacDecOK {
		return err
	}

	if err := readOneChannelTail(bs, ch, sri, frameLength, flags, commonWindow); err != aacDecOK {
		return err
	}
	return inverseQuantizeChannel(ch, sri, frameLength)
}

// readOneChannelBody reads the CPE-common-window per-channel body: global_gain
// then the tail (section_data onward). ics_info was already read into ch.ics.
func readOneChannelBody(bs *bitStream, ch *channelData, sri *samplingRateInfo,
	frameLength int, flags uint32, commonWindow uint8) aacDecoderError {
	// global_gain (channel.cpp:578).
	ch.globalGain = uint8(bs.readBits(8))
	return readOneChannelTail(bs, ch, sri, frameLength, flags, commonWindow)
}

// readOneChannelTail reads section_data, scale_factor_data, pulse,
// tns_data_present, tns_data, gain_control_data_present, spectral_data, and
// applies pulse data to the quantized spectrum (block.cpp:757).
func readOneChannelTail(bs *bitStream, ch *channelData, sri *samplingRateInfo,
	frameLength int, flags uint32, commonWindow uint8) aacDecoderError {
	granuleLength := frameLength
	if ch.ics.windowSequence == blockShort {
		granuleLength = frameLength / 8
	}
	if ch.spectrum == nil {
		ch.spectrum = make([]int32, 1024)
	}

	// section_data (channel.cpp:583 → CBlock_ReadSectionData).
	var sd cSectionData
	if err := readSectionData(bs, &ch.ics, sri, commonWindow, flags, &sd); err != aacDecOK {
		return err
	}
	ch.codeBook = sd.aCodeBook

	// scale_factor_data (channel.cpp:599 → CBlock_ReadScaleFactorData).
	if err := readScaleFactorData(bs, &ch.ics, sri, ch.codeBook[:], ch.scaleFactor[:],
		ch.globalGain, &ch.pns, flags); err != aacDecOK {
		return err
	}

	// pulse (channel.cpp:609 → CPulseData_Read).
	if err := cPulseDataRead(bs, &ch.pulse, getScaleFactorBandOffsets(&ch.ics, sri),
		getScaleFactorBandsTransmitted(&ch.ics), isLongBlock(&ch.ics), frameLength); err != aacDecOK {
		return err
	}

	// tns_data_present (channel.cpp:622) + tns_data (channel.cpp:630).
	cTnsReadDataPresentFlag(bs, &ch.tns)
	if err := cTnsRead(bs, &ch.tns, &ch.ics, flags); err != aacDecOK {
		return err
	}

	// gain_control_data_present (channel.cpp:640): a 1 here is unsupported.
	if bs.readBits(1) != 0 {
		return aacDecUnsupportedGainControl
	}

	// spectral_data (channel.cpp:747 → CBlock_ReadSpectralData, plain Huffman).
	in := spectralInput{
		codeBook:         ch.codeBook[:],
		bandOffsets:      getScaleFactorBandOffsets(&ch.ics, sri),
		windowGroups:     getWindowGroups(&ch.ics),
		windowGroupLen:   windowGroupLenInts(&ch.ics),
		granuleLength:    granuleLength,
		transmittedBands: getScaleFactorBandsTransmitted(&ch.ics),
	}
	readSpectralData(bs, &in, ch.spectrum)

	// apply pulse data (block.cpp:757): long block only.
	if isLongBlock(&ch.ics) {
		cPulseDataApply(&ch.pulse, getScaleFactorBandOffsets(&ch.ics, sri), ch.spectrum)
	}

	return aacDecOK
}

// inverseQuantizeChannel ports the per-channel CBlock_InverseQuantizeSpectralData
// (channel.cpp:898) on the quantized spectrum.
func inverseQuantizeChannel(ch *channelData, sri *samplingRateInfo, frameLength int) aacDecoderError {
	granuleLength := frameLength
	if ch.ics.windowSequence == blockShort {
		granuleLength = frameLength / 8
	}
	return inverseQuantizeSpectralData(&ch.ics, sri, ch.codeBook[:], ch.sfbScale[:],
		ch.scaleFactor[:], ch.spectrum, granuleLength, nil, 0)
}

// windowGroupLenInts returns the per-group window-group lengths as []int (what
// spectralInput / ApplyMS-Go want from the [8]uint8 array).
func windowGroupLenInts(p *cIcsInfo) []int {
	out := make([]int, getWindowGroups(p))
	for i := range out {
		out[i] = int(p.windowGroupLength[i])
	}
	return out
}

// windowGroupLenBytes returns the per-group window-group lengths as []uint8 for
// the apply tools (which take the raw GetWindowGroupLengthTable).
func windowGroupLenBytes(p *cIcsInfo) []uint8 {
	out := make([]uint8, getWindowGroups(p))
	for i := range out {
		out[i] = p.windowGroupLength[i]
	}
	return out
}
