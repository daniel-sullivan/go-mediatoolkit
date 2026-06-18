// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// The post-read decode tools (CChannelElement_Decode, channel.cpp:162) for the
// AAC-LC SCE/CPE paths, the per-channel frequency-to-time bridge, and the int16
// interleave that mirrors the limiter-disabled output stage. Errors are the Go
// sentinels the public package surfaces.

import "errors"

// errUnsupportedConfig etc. are local sentinels; the public libraries/aac layer
// maps decodeError outputs onto its own aac:-prefixed sentinels.
var (
	errUnsupportedConfig  = errors.New("nativeaac: unsupported decoder configuration")
	errChannelMismatch    = errors.New("nativeaac: element channel count does not match decoder")
	errNoElement          = errors.New("nativeaac: no audio element in access unit")
	errUnsupportedElement = errors.New("nativeaac: unsupported syntactic element")
	errDecode             = errors.New("nativeaac: AAC decode error")
)

// decodeError maps a C AAC_DECODER_ERROR enum to a Go error (non-OK => errDecode).
func decodeError(e aacDecoderError) error {
	if e == aacDecOK {
		return nil
	}
	return errDecode
}

// decodeMonoElement ports the SCE branch of CChannelElement_Decode
// (channel.cpp:162) for AAC-LC: no joint stereo, just scaleSpectralData (folding
// TNS head room) then ApplyTools (TNS).
func decodeMonoElement(ch *channelData, sri *samplingRateInfo, frameLength int, flags uint32) {
	granuleLength := granuleFor(&ch.ics, frameLength)
	noSfbs := uint8(getScaleFactorBandsTransmitted(&ch.ics))
	maxTnsBands := getMaximumTnsBands(&ch.ics, int(sri.samplingRateIndex))

	// CBlock_ScaleSpectralData (channel.cpp:274).
	scaleSpectralData(&ch.ics, sri, noSfbs, ch.sfbScale[:], ch.specScale[:],
		ch.spectrum, granuleLength, &ch.tns, maxTnsBands)

	// ApplyTools -> CTns_Apply (channel.cpp:335, block.cpp:962).
	cTnsApply(&ch.tns, &ch.ics, ch.spectrum, sri, granuleLength,
		uint8(getScaleFactorBandsTransmitted(&ch.ics)), 0, flags)
}

// decodeStereoElement ports the CPE branch of CChannelElement_Decode
// (channel.cpp:162) for AAC-LC: M/S (if common_window), intensity stereo,
// per-channel scaleSpectralData, then per-channel ApplyTools (TNS).
func decodeStereoElement(l, r *channelData, jsd *JointStereoData, commonWindow uint8,
	sri *samplingRateInfo, frameLength int, flags uint32) {
	granuleLength := granuleFor(&l.ics, frameLength)

	maxSfBandsL := getScaleFactorBandsTransmitted(&l.ics)
	maxSfBandsR := getScaleFactorBandsTransmitted(&r.ics)

	// apply M/S (channel.cpp:203) — only when common_window.
	if commonWindow != 0 {
		maxSfbSte := int(l.ics.maxSfbSte)
		ApplyMS(jsd, l.spectrum, r.spectrum, l.sfbScale[:], r.sfbScale[:],
			getScaleFactorBandOffsets(&l.ics, sri),
			windowGroupLenBytes(&l.ics), getWindowGroups(&l.ics),
			maxSfbSte, maxSfBandsL, maxSfBandsR, granuleLength)
	}

	// apply intensity stereo (channel.cpp:231) — common_window only.
	if commonWindow == 1 {
		ApplyIS(jsd, l.spectrum, r.spectrum, r.codeBook[:], r.scaleFactor[:],
			l.sfbScale[:], r.sfbScale[:], getScaleFactorBandOffsets(&l.ics, sri),
			windowGroupLenBytes(&l.ics), getWindowGroups(&l.ics),
			getScaleFactorBandsTransmitted(&l.ics), granuleLength)
	}

	// per-channel CBlock_ScaleSpectralData (channel.cpp:274). When common_window,
	// noSfbs == max(maxSfBandsL, maxSfBandsR).
	noSfbs := uint8(maxSfBandsL)
	if commonWindow == 1 {
		m := maxSfBandsL
		if maxSfBandsR > m {
			m = maxSfBandsR
		}
		noSfbs = uint8(m)
	}
	scaleSpectralData(&l.ics, sri, noSfbs, l.sfbScale[:], l.specScale[:],
		l.spectrum, granuleLength, &l.tns, getMaximumTnsBands(&l.ics, int(sri.samplingRateIndex)))
	noSfbsR := uint8(maxSfBandsR)
	if commonWindow == 1 {
		noSfbsR = noSfbs
	}
	scaleSpectralData(&r.ics, sri, noSfbsR, r.sfbScale[:], r.specScale[:],
		r.spectrum, granuleLength, &r.tns, getMaximumTnsBands(&r.ics, int(sri.samplingRateIndex)))

	// per-channel ApplyTools -> CTns_Apply (channel.cpp:335).
	cTnsApply(&l.tns, &l.ics, l.spectrum, sri, granuleLength,
		uint8(getScaleFactorBandsTransmitted(&l.ics)), 0, flags)
	cTnsApply(&r.tns, &r.ics, r.spectrum, sri, granuleLength,
		uint8(getScaleFactorBandsTransmitted(&r.ics)), 0, flags)
}

// granuleFor returns the SPEC stride (granuleLength) for a block.
func granuleFor(p *cIcsInfo, frameLength int) int {
	if p.windowSequence == blockShort {
		return frameLength / 8
	}
	return frameLength
}

// frequencyToTimeChannel runs the inverse filterbank for one channel into its
// planar int32 PCM_DEC buffer (ch.timePCM).
func frequencyToTimeChannel(st *channelState, ch *channelData, sri *samplingRateInfo,
	frameLength int, scratch []int32) {
	if len(ch.timePCM) < frameLength {
		ch.timePCM = make([]int32, frameLength)
	}
	frequencyToTime(&st.mdct, &ch.ics, ch.timePCM, ch.spectrum, ch.specScale[:],
		frameLength, aacOutDataHeadroom, scratch)
}

// writeInterleavedMono narrows the int32 PCM_DEC time samples to int16 INT_PCM
// (the limiter-disabled aacdecoder_lib.cpp:2002 path: scaleValuesSaturate(dst,
// src, n, PCM_OUT_HEADROOM)). Mono needs no interleave.
func writeInterleavedMono(out []int16, time []int32, frameLength int) {
	for i := 0; i < frameLength; i++ {
		out[i] = pcmDecToInt16(time[i])
	}
}

// writeInterleavedStereo narrows both planar channels to int16 and interleaves
// (the limiter-disabled stereo path: scaleValuesSaturate to a scratch then
// FDK_interleave, aacdecoder_lib.cpp:2008-2015 — equivalent to interleaving the
// per-sample narrowed values).
func writeInterleavedStereo(out []int16, left, right []int32, frameLength int) {
	for i := 0; i < frameLength; i++ {
		out[2*i] = pcmDecToInt16(left[i])
		out[2*i+1] = pcmDecToInt16(right[i])
	}
}

// pcmDecToInt16 ports the limiter-disabled INT_PCM output tail of
// aacDecoder_DecodeFrame for one sample. Two integer steps in order:
//
//  1. The headroom conversion (aacdecoder_lib.cpp:1673-1681): when
//     PCM_OUT_HEADROOM (8) != timeDataHeadroom (aacOutDataHeadroom == 3) the
//     PCM_DEC sample is arithmetic-right-shifted by (PCM_OUT_HEADROOM -
//     timeDataHeadroom) == 5, lifting the carried 3-bit headroom to the 8-bit
//     PCM_OUT_HEADROOM the limiter/output stage assumes.
//
//  2. scaleValuesSaturate(FIXP_SGL, FIXP_DBL, len, pcmLimiterScale) with
//     pcmLimiterScale == PCM_OUT_HEADROOM (8) (aacdecoder_lib.cpp:1911 + 2002):
//     dst = FX_DBL2FX_SGL(fAddSaturate(scaleValueSaturate(src, 8), 0x8000)),
//     where FX_DBL2FX_SGL(x) == (SHORT)(x >> 16) and the +0x8000 rounds.
func pcmDecToInt16(src int32) int16 {
	src >>= (pcmOutHeadroom - aacOutDataHeadroom) // >> 5
	scaled := scaleValueSaturate(src, pcmOutHeadroom)
	added := fAddSaturate(scaled, 0x8000)
	return int16(added >> 16)
}
