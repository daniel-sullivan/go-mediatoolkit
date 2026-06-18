// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDKaacEnc_InitTnsConfiguration, ported 1:1 from libAACenc/src/aacenc_tns.cpp
// (:377-498). It fills a TNS_CONFIG for the requested bitrate/samplerate/
// blockType from the psychoacoustic configuration's scalefactor-band layout.
//
// Scope: AAC-LC only — granuleLength 960/1024. The 480/512 LD/ELD granule
// lengths (the only branch that calls FDKaacEnc_GetTnsParam +
// FDKaacEnc_CalcGaussWindow) and any other granule length disable TNS exactly as
// the C default branch does (tnsActive = FALSE). That LD path is intentionally
// NOT ported here. Every value is integer/Q-format; the AAC-LC path uses only
// integer arithmetic and the integer ROM acfWindowLong/acfWindowShort, so this
// is aacfdk-fenced with no aac_strict split.

const (
	tnsFilterDirectionUp = 0 // FILTER_DIRECTION (aacenc_tns.cpp:111): 0 = up

	// TNS thresholds / orders for the AAC-LC 960/1024 path
	// (aacenc_tns.cpp:432-449).
	encTnsThreshOnHi = 1437
	encTnsThreshOnLo = 1500
)

// initTnsConfiguration ports FDKaacEnc_InitTnsConfiguration (aacenc_tns.cpp:377)
// for the AAC-LC granuleLength 960/1024 path. tC is filled in place; pC is the
// already-initialised PSY_CONFIGURATION (reads sfbActive, sfbOffset). active
// enables the TNS tool. ldSbrPresent / useTnsPeak are accepted for signature
// parity but only matter on the unported LD path. Returns AacEncOK or
// EncoderError(1) on a getTnsMaxBands failure / channels<=0 (matching the C
// `(AAC_ENCODER_ERROR)1`).
func initTnsConfiguration(bitRate, sampleRate, channels, blockType, granuleLength,
	isLowDelay, ldSbrPresent int, tC *TNSConfig, pC *PsyConfiguration,
	active, useTnsPeak int) EncoderError {

	_ = ldSbrPresent
	_ = useTnsPeak

	if channels <= 0 {
		return EncoderError(1)
	}

	tC.IsLowDelay = isLowDelay

	// initialize TNS filter flag, order, and coefficient resolution
	if active != 0 {
		tC.TnsActive = 1 // TRUE
	} else {
		tC.TnsActive = 0 // FALSE
	}
	if blockType == encShortWindow {
		tC.MaxOrder = 5
	} else {
		tC.MaxOrder = 12
	}
	if bitRate < 16000 {
		tC.MaxOrder -= 2
	}
	if blockType == encShortWindow {
		tC.CoefRes = 3
	} else {
		tC.CoefRes = 4
	}

	// LPC stop line: highest MDCT line to be coded, but do not go beyond
	// TNS_MAX_BANDS!
	isShort := 0
	if blockType == encShortWindow {
		isShort = 1
	}
	tC.LpcStopBand = getTnsMaxBands(sampleRate, granuleLength, isShort)
	if tC.LpcStopBand < 0 {
		return EncoderError(1)
	}

	tC.LpcStopBand = int(fMin(int32(tC.LpcStopBand), int32(pC.SfbActive)))
	tC.LpcStopLine = int(pC.SfbOffset[tC.LpcStopBand])

	switch granuleLength {
	case 960, 1024:
		// TNS start line: skip lower MDCT lines to prevent artifacts due to
		// filter mismatch.
		if blockType == encShortWindow {
			tC.LpcStartBand[lofilt] = 0
		} else {
			if sampleRate < 9391 {
				tC.LpcStartBand[lofilt] = 2
			} else if sampleRate < 18783 {
				tC.LpcStartBand[lofilt] = 4
			} else {
				tC.LpcStartBand[lofilt] = 8
			}
		}
		tC.LpcStartLine[lofilt] = int(pC.SfbOffset[tC.LpcStartBand[lofilt]])

		i := tC.LpcStopBand
		for int(pC.SfbOffset[i]) >
			(tC.LpcStartLine[lofilt] + (tC.LpcStopLine-tC.LpcStartLine[lofilt])/4) {
			i--
		}
		tC.LpcStartBand[hifilt] = i
		tC.LpcStartLine[hifilt] = int(pC.SfbOffset[i])

		tC.ConfTab.ThreshOn[hifilt] = encTnsThreshOnHi
		tC.ConfTab.ThreshOn[lofilt] = encTnsThreshOnLo

		tC.ConfTab.TnsLimitOrder[hifilt] = tC.MaxOrder
		tC.ConfTab.TnsLimitOrder[lofilt] = int(fMax(0, int32(tC.MaxOrder-7)))

		tC.ConfTab.TnsFilterDirection[hifilt] = tnsFilterDirectionUp
		tC.ConfTab.TnsFilterDirection[lofilt] = tnsFilterDirectionUp

		// signal Merged4to2QuartersAutoCorrelation in
		// FDKaacEnc_MergedAutoCorrelation
		tC.ConfTab.AcfSplit[hifilt] = -1
		tC.ConfTab.AcfSplit[lofilt] = -1

		tC.ConfTab.FilterEnabled[hifilt] = 1
		tC.ConfTab.FilterEnabled[lofilt] = 1
		tC.ConfTab.SeperateFiltersAllowed = 1

		// compute autocorrelation window: copy the precomputed integer ROM, with
		// the same FDKmemcpy(min(sizeof(src), sizeof(dst))) clamp as the C.
		if blockType == encShortWindow {
			copyAcfWindow(tC.AcfWindow[hifilt][:], encAcfWindowShort[:])
			copyAcfWindow(tC.AcfWindow[lofilt][:], encAcfWindowShort[:])
		} else {
			copyAcfWindow(tC.AcfWindow[hifilt][:], encAcfWindowLong[:])
			copyAcfWindow(tC.AcfWindow[lofilt][:], encAcfWindowLong[:])
		}

	default:
		// granuleLength 480/512 (LD/ELD, FDKaacEnc_GetTnsParam +
		// FDKaacEnc_CalcGaussWindow) is out of scope for AAC-LC; any other length
		// disables TNS — both behave as the C default branch.
		tC.TnsActive = 0 // FALSE
	}

	return AacEncOK
}

// copyAcfWindow reproduces the FDKmemcpy(dst, src,
// fMin(sizeof(src), sizeof(dst))) clamp the C uses for the acfWindow copy: it
// copies min(len(src), len(dst)) elements. For AAC-LC the dst (encAcfWindowSize
// == TNS_MAX_ORDER+3+1 == 16) is exactly the long window size; the short window
// (8) copies only its 8 entries, leaving the rest as-is (matching the C, where
// TNS_CONFIG is memset to 0 before init).
func copyAcfWindow(dst, src []int32) {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	copy(dst[:n], src[:n])
}
