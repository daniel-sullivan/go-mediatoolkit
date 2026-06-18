// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Pure-Go 1:1 port of FDKaacEnc_psyMainInit (libAACenc/src/psy_main.cpp:300):
// the psychoacoustic-model configuration entry point the encoder init path
// (FDKaacEnc_Initialize, aacenc_init.go) calls once the channel mapping and
// bandwidth are resolved. It maps the (audioObjectType, channel mode) pair to
// the filterbank type and tnsChannels, stores granuleLength, then fills the
// per-block-type PSY_CONFIGURATION psyConf[0]=LONG (and, for granuleLength>512,
// psyConf[1]=SHORT) by calling the already-ported leaf inits:
//
//	FDKaacEnc_InitPsyConfiguration  (InitPsyConfiguration, psy_configuration.go)
//	FDKaacEnc_InitTnsConfiguration  (EncInitTnsConfiguration, enc_tns_finish_parity_export.go)
//	FDKaacEnc_InitPreEchoControl    (InitPreEchoControl, psy_main_parity_export.go)
//	FDKaacEnc_InitPnsConfiguration  (InitPnsConfiguration, aacenc_pns.go)
//
// AAC-LC scope: audioObjectType == AOT_AAC_LC -> filterBank == FB_LC and
// granuleLength == 1024 (> 512), so both LONG and SHORT psyConf entries are
// initialised; the LD/ELD (FB_LD/FB_ELD, granuleLength<=512) branches are ported
// 1:1 but inert. SBR/ELD are excluded: ldSbrPresent derives from
// (syntaxFlags & AC_SBR_PRESENT), which is 0 for plain AAC-LC.
//
// Pure integer / pointer wiring around already-verified leaves; aacfdk-fenced.

package nativeaac

// fbTypeForAot mirrors the audioObjectType -> FB_TYPE switch of
// FDKaacEnc_psyMainInit (psy_main.cpp:322-332): FB_LD for AOT_ER_AAC_LD, FB_ELD
// for AOT_ER_AAC_ELD, FB_LC for everything else (AAC-LC).
func fbTypeForAot(aot AudioObjectType) int {
	switch aot {
	case AOTErAACLD:
		return fbLD
	case AOTErAACELD:
		return fbELD
	default:
		return fbLC
	}
}

// psyMainInit is the 1:1 port of FDKaacEnc_psyMainInit (psy_main.cpp:300). It
// fills hPsy.PsyConf[] in place. bitRate is the psy bitrate (config->bitRate -
// ancDataBitRate), bandwidth the resolved 90 dB bandwidth, syntaxFlags the
// transport syntax flags (AC_SBR_PRESENT is the only one consulted here),
// initFlags non-zero on a cold start (force psyInitStates reset). Returns the
// AAC_ENC error of the first failing leaf init, else AAC_ENC_OK.
func psyMainInit(hPsy *PsyInternal, audioObjectType AudioObjectType, cm *ChannelMapping,
	sampleRate, granuleLength, bitRate, tnsMask, bandwidth, usePns, useIS, useMS int,
	syntaxFlags uint, initFlags uint32) EncoderError {

	channelsEff := cm.NChannelsEff
	tnsChannels := 0

	// switch (FDKaacEnc_GetMonoStereoMode(cm->encMode)) -> tnsChannels
	switch getMonoStereoMode(cm.EncMode) {
	case ElementModeMono:
		tnsChannels = 1
	case ElementModeStereo:
		tnsChannels = 2
	default:
		tnsChannels = 0
	}

	filterBank := fbTypeForAot(audioObjectType)

	hPsy.GranuleLength = granuleLength

	// AC_SBR_PRESENT (FDK_audio.h:311) == 0x008000.
	ldSbrPresent := 0
	if syntaxFlags&acSbrPresent != 0 {
		ldSbrPresent = 1
	}
	isLD := 0
	if isLowDelay(audioObjectType) {
		isLD = 1
	}

	// LONG window psy + TNS config.
	errorStatus := EncoderError(InitPsyConfiguration(
		bitRate/channelsEff, sampleRate, bandwidth, encLongWindow,
		hPsy.GranuleLength, useIS, useMS, &hPsy.PsyConf[0], filterBank))
	if errorStatus != AacEncOK {
		return errorStatus
	}

	errorStatus = EncoderError(EncInitTnsConfiguration(
		(bitRate*tnsChannels)/channelsEff, sampleRate, tnsChannels,
		encLongWindow, hPsy.GranuleLength, isLD, ldSbrPresent,
		&hPsy.PsyConf[0].TnsConf, &hPsy.PsyConf[0], tnsMask&2, tnsMask&8))
	if errorStatus != AacEncOK {
		return errorStatus
	}

	if granuleLength > 512 {
		errorStatus = EncoderError(InitPsyConfiguration(
			bitRate/channelsEff, sampleRate, bandwidth, encShortWindow,
			hPsy.GranuleLength, useIS, useMS, &hPsy.PsyConf[1], filterBank))
		if errorStatus != AacEncOK {
			return errorStatus
		}

		errorStatus = EncoderError(EncInitTnsConfiguration(
			(bitRate*tnsChannels)/channelsEff, sampleRate, tnsChannels,
			encShortWindow, hPsy.GranuleLength, isLD, ldSbrPresent,
			&hPsy.PsyConf[1].TnsConf, &hPsy.PsyConf[1], tnsMask&1, tnsMask&4))
		if errorStatus != AacEncOK {
			return errorStatus
		}
	}

	for i := 0; i < cm.NElements; i++ {
		for ch := 0; ch < cm.ElInfo[i].NChannelsInEl; ch++ {
			if initFlags != 0 {
				// reset states
				psyInitStates(hPsy.PsyElement[i].PsyStatic[ch], audioObjectType)
			}

			// FDKaacEnc_InitPreEchoControl writes sfbThresholdnm1[] and
			// calcPreEcho / mdctScalenm1 of the per-channel PSY_STATIC. The Go
			// InitPreEchoControl (psy_main_parity_export.go) returns
			// (calcPreEcho, mdctScalenm1) and fills the threshold slice in place.
			st := hPsy.PsyElement[i].PsyStatic[ch]
			mdctScaleNm1, calcPreEcho := InitPreEchoControl(
				st.SfbThresholdNm1[:], hPsy.PsyConf[0].SfbPcmQuantThreshold[:],
				hPsy.PsyConf[0].SfbCnt)
			st.CalcPreEcho = calcPreEcho
			st.MdctScaleNm1 = mdctScaleNm1
		}
	}

	usePnsFlag := usePns
	errorStatus = EncoderError(InitPnsConfiguration(
		&hPsy.PsyConf[0].PnsConf, bitRate/channelsEff, sampleRate, usePnsFlag,
		hPsy.PsyConf[0].SfbCnt, hPsy.PsyConf[0].SfbOffset[:],
		cm.ElInfo[0].NChannelsInEl, boolToInt(hPsy.PsyConf[0].Filterbank == fbLC)))
	if errorStatus != AacEncOK {
		return errorStatus
	}

	if granuleLength > 512 {
		errorStatus = EncoderError(InitPnsConfiguration(
			&hPsy.PsyConf[1].PnsConf, bitRate/channelsEff, sampleRate, usePnsFlag,
			hPsy.PsyConf[1].SfbCnt, hPsy.PsyConf[1].SfbOffset[:],
			cm.ElInfo[1].NChannelsInEl, boolToInt(hPsy.PsyConf[1].Filterbank == fbLC)))
		if errorStatus != AacEncOK {
			return errorStatus
		}
	}

	return errorStatus
}

// boolToInt maps a Go bool to the C int idiom (1/0) used where the C passes a
// comparison result as an INT argument.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
