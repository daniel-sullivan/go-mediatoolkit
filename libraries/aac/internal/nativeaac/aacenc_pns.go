// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Perceptual Noise Substitution (PNS) encode-side detect/code chain, ported 1:1
// from libAACenc/src/aacenc_pns.cpp. This is the per-non-LFE-channel chain that
// decides which scalefactor bands are coded as noise and populates the
// psyOutChannel.noiseNrg the bitstream writer emits:
//
//   - FDKaacEnc_InitPnsConfiguration fills PNS_CONFIG (via GetPnsParam).
//   - FDKaacEnc_PnsDetect runs noise detection (noisedet.go) and applies the
//     fuzzy-measure / threshold-vs-energy decision plus hole-filling to set
//     pnsFlag[], then computes noiseNrg via FDKaacEnc_CalcNoiseNrgs.
//   - FDKaacEnc_CodePnsChannel finalises noiseNrg (delta-limit, threshold bump).
//   - FDKaacEnc_PreProcessPnsChannelPair / FDKaacEnc_PostProcessPnsChannelPair
//     compute the L/R noise correlation and use it to gate the M/S mask.
//
// fdk-aac encode is FIXED-POINT: every value is an int32 FIXP_DBL / int16
// FIXP_SGL Q-format quantity. The chain is pure integer fixed-point (fMult int64
// products, arithmetic shifts, the table-driven CalcLdData / CalcInvLdData) —
// bit-identical regardless of vectorization — so it carries only the aacfdk
// fence (no aac_strict FP split). PNS_CONFIG / NoiseParams are defined in
// psy_configuration.go (one coherent definition).

// noNoisePns (NO_NOISE_PNS == FDK_INT_MIN, the noiseNrg "not PNS-coded"
// sentinel) is defined in dyn_bits.go (one coherent definition) and reused here.

// PNS algorithm constants.
const (
	logNormPcm     = -15 // LOG_NORM_PCM (psy_const.h:161)
	ldDataScaling  = 64  // LD_DATA_SCALING (fixpoint_math.h:113, == 64.0f)
	codeBookPnsLav = 60  // CODE_BOOK_PNS_LAV (bit_cnt.h:178)
)

// PNSData ports PNS_DATA (aacenc_pns.h:118-122): the per-channel PNS working
// state. noiseFuzzyMeasure is the per-band fuzzy noise measure (FIXP_SGL),
// noiseEnergyCorrelation the L/R noise correlation (FIXP_DBL), pnsFlag the
// per-band PNS on/off decision.
type PNSData struct {
	NoiseFuzzyMeasure      [maxGroupedSfb]int16 // noiseFuzzyMeasure[MAX_GROUPED_SFB] (FIXP_SGL)
	NoiseEnergyCorrelation [maxGroupedSfb]int32 // noiseEnergyCorrelation[MAX_GROUPED_SFB] (FIXP_DBL)
	PnsFlag                [maxGroupedSfb]int   // pnsFlag[MAX_GROUPED_SFB]
}

// pnsMinCorrelationEnergy is the file-static minCorrelationEnergy
// (aacenc_pns.cpp:111-113): FL2FXCONST_DBL(0.0). The file-static
// noiseCorrelationThresh (aacenc_pns.cpp:114-115) is FL2FXCONST_DBL(0.36) ==
// 0.6^2, materialised inline at use as fl2fxconstDBL(0.36).
const pnsMinCorrelationEnergy = 0

// InitPnsConfiguration is the 1:1 port of FDKaacEnc_InitPnsConfiguration
// (aacenc_pns.cpp:137-153): fill pnsConf with the noise-detection params plus
// the correlation thresholds and usePns flag. Returns the AAC encoder error code
// (0 == AAC_ENC_OK).
func InitPnsConfiguration(pnsConf *PNSConfig, bitRate, sampleRate, usePns, sfbCnt int,
	sfbOffset []int32, numChan, isLC int) int {

	// init noise detection
	newUsePns, errStatus := GetPnsParam(&pnsConf.NoiseParams, bitRate, sampleRate,
		sfbCnt, sfbOffset, usePns, numChan, isLC)
	if errStatus != aacEncOK {
		return errStatus
	}
	usePns = newUsePns

	pnsConf.MinCorrelationEnergy = pnsMinCorrelationEnergy
	pnsConf.NoiseCorrelationThresh = fl2fxconstDBL(0.36)

	pnsConf.UsePns = usePns

	return aacEncOK
}

// PnsDetect is the 1:1 port of FDKaacEnc_PnsDetect (aacenc_pns.cpp:173-292):
// decide which sfb's are PNS-coded and compute their noiseNrg. Resets pnsFlag/
// noiseNrg, short-circuits when PNS is disabled or the block type/algorithm
// flags forbid detection, runs noise detection, applies the fuzzy + threshold
// decision and the hole-filling/single-band cleanup, then fills noiseNrg.
func PnsDetect(pnsConf *PNSConfig, pnsData *PNSData, lastWindowSequence, sfbActive,
	maxSfbPerGroup int, sfbThresholdLdData []int32, sfbOffset []int32,
	mdctSpectrum []int32, sfbMaxScaleSpec []int32, sfbtonality []int16,
	tnsOrder, tnsPredictionGain, tnsActive int, sfbEnergyLdData []int32, noiseNrg []int) {

	// Reset pns info.
	for i := range pnsData.PnsFlag {
		pnsData.PnsFlag[i] = 0
	}
	for sfb := 0; sfb < maxGroupedSfb; sfb++ {
		noiseNrg[sfb] = noNoisePns
	}

	// Disable PNS and skip detection in certain cases.
	if pnsConf.UsePns == 0 {
		return
	}
	// AAC - LC core encoder
	if (pnsConf.NoiseParams.DetectionAlgorithmFlags&isLowComplexity) != 0 &&
		lastWindowSequence == encShortWindow {
		return
	}
	// AAC - (E)LD core encoder
	if (pnsConf.NoiseParams.DetectionAlgorithmFlags&isLowComplexity) == 0 &&
		(pnsConf.NoiseParams.DetectionAlgorithmFlags&justLongWindow) != 0 &&
		lastWindowSequence != encLongWindow {
		return
	}

	// call noise detection
	pnsNoiseDetection(pnsConf, pnsData, sfbActive, sfbOffset, tnsOrder,
		tnsPredictionGain, tnsActive, mdctSpectrum, sfbMaxScaleSpec, sfbtonality)

	// set startNoiseSfb (long)
	startNoiseSfb := int(pnsConf.NoiseParams.StartSfb)

	// Set noise substitution status.
	for sfb := 0; sfb < sfbActive; sfb++ {
		// No PNS below startNoiseSfb.
		if sfb < startNoiseSfb {
			pnsData.PnsFlag[sfb] = 0
			continue
		}

		// do noise substitution if fuzzy measure is high enough, sfb freq >
		// minimum sfb freq, and signal in coder band is not masked.
		if pnsData.NoiseFuzzyMeasure[sfb] > fl2fxconstSGL(0.5) &&
			(sfbThresholdLdData[sfb]+fl2fxconstDBL(0.5849625/64.0)) < sfbEnergyLdData[sfb] {
			// thr * 1.5 = thrLd + ld(1.5)/64
			pnsData.PnsFlag[sfb] = 1 // PNS_ON
		} else {
			pnsData.PnsFlag[sfb] = 0 // PNS_OFF
		}
		// no PNS if LTP is active
	}

	// avoid PNS holes
	if pnsData.NoiseFuzzyMeasure[0] > fl2fxconstSGL(0.5) && pnsData.PnsFlag[1] != 0 {
		pnsData.PnsFlag[0] = 1
	}

	for sfb := 1; sfb < maxSfbPerGroup-1; sfb++ {
		if pnsData.NoiseFuzzyMeasure[sfb] > pnsConf.NoiseParams.GapFillThr &&
			pnsData.PnsFlag[sfb-1] != 0 && pnsData.PnsFlag[sfb+1] != 0 {
			pnsData.PnsFlag[sfb] = 1
		}
	}

	if maxSfbPerGroup > 0 {
		// avoid PNS hole
		if pnsData.NoiseFuzzyMeasure[maxSfbPerGroup-1] > pnsConf.NoiseParams.GapFillThr &&
			pnsData.PnsFlag[maxSfbPerGroup-2] != 0 {
			pnsData.PnsFlag[maxSfbPerGroup-1] = 1
		}
		// avoid single PNS band
		if pnsData.PnsFlag[maxSfbPerGroup-2] == 0 {
			pnsData.PnsFlag[maxSfbPerGroup-1] = 0
		}
	}

	// avoid single PNS bands
	if pnsData.PnsFlag[1] == 0 {
		pnsData.PnsFlag[0] = 0
	}

	for sfb := 1; sfb < maxSfbPerGroup-1; sfb++ {
		if pnsData.PnsFlag[sfb-1] == 0 && pnsData.PnsFlag[sfb+1] == 0 {
			pnsData.PnsFlag[sfb] = 0
		}
	}

	// calculate noiseNrg's
	calcNoiseNrgs(sfbActive, pnsData.PnsFlag[:], sfbEnergyLdData, noiseNrg)
}

// pnsNoiseDetection is the 1:1 port of the static wrapper
// FDKaacEnc_FDKaacEnc_noiseDetection (aacenc_pns.cpp:309-337): either clear
// noiseFuzzyMeasure when heavy TNS activity argues against PNS, or call
// noiseDetect.
func pnsNoiseDetection(pnsConf *PNSConfig, pnsData *PNSData, sfbActive int,
	sfbOffset []int32, tnsOrder, tnsPredictionGain, tnsActive int,
	mdctSpectrum []int32, sfbMaxScaleSpec []int32, sfbtonality []int16) {

	condition := true
	if (pnsConf.NoiseParams.DetectionAlgorithmFlags & isLowComplexity) == 0 {
		condition = tnsOrder > 3
	}

	// no PNS if heavy TNS activity: clear pnsData->noiseFuzzyMeasure
	if (pnsConf.NoiseParams.DetectionAlgorithmFlags&useTnsGainThr) != 0 &&
		tnsPredictionGain >= pnsConf.NoiseParams.TnsGainThreshold && condition &&
		!((pnsConf.NoiseParams.DetectionAlgorithmFlags&useTnsPns) != 0 &&
			tnsPredictionGain >= pnsConf.NoiseParams.TnsPNSGainThreshold && tnsActive != 0) {
		// clear all noiseFuzzyMeasure
		for i := 0; i < sfbActive; i++ {
			pnsData.NoiseFuzzyMeasure[i] = 0
		}
	} else {
		// call noise detection, output in pnsData->noiseFuzzyMeasure, use real
		// mdct spectral data
		noiseDetect(mdctSpectrum, sfbMaxScaleSpec, sfbActive, sfbOffset,
			pnsData.NoiseFuzzyMeasure[:], &pnsConf.NoiseParams, sfbtonality)
	}
}

// calcNoiseNrgs is the 1:1 port of FDKaacEnc_CalcNoiseNrgs
// (aacenc_pns.cpp:352-365): compute the integer noise energy for each PNS-flagged
// band from its ld-domain energy.
func calcNoiseNrgs(sfbActive int, pnsFlag []int, sfbEnergyLdData []int32, noiseNrg []int) {
	tmp := (-logNormPcm) << 2

	for sfb := 0; sfb < sfbActive; sfb++ {
		if pnsFlag[sfb] != 0 {
			nrg := (-sfbEnergyLdData[sfb] + fl2fxconstDBL(0.5/64.0)) >> (dfractBits - 1 - 7)
			noiseNrg[sfb] = tmp - int(nrg)
		}
	}
}

// CodePnsChannel is the 1:1 port of FDKaacEnc_CodePnsChannel
// (aacenc_pns.cpp:381-424): finalise the per-channel PNS coding decision —
// bump the threshold of PNS bands so their pe becomes 0, and delta-limit the
// noiseNrg sequence to the codebook LAV. Non-PNS bands get NO_NOISE_PNS.
func CodePnsChannel(sfbActive int, pnsConf *PNSConfig, pnsFlag []int,
	sfbEnergyLdData []int32, noiseNrg []int, sfbThresholdLdData []int32) {

	lastiNoiseEnergy := 0
	firstPNSband := 1 // TRUE for first PNS-coded band

	// no PNS
	if pnsConf.UsePns == 0 {
		for sfb := 0; sfb < sfbActive; sfb++ {
			noiseNrg[sfb] = noNoisePns
		}
		return
	}

	// code PNS
	for sfb := 0; sfb < sfbActive; sfb++ {
		if pnsFlag[sfb] != 0 {
			// high sfbThreshold causes pe = 0
			if noiseNrg[sfb] != noNoisePns {
				sfbThresholdLdData[sfb] = sfbEnergyLdData[sfb] + fl2fxconstDBL(1.0/ldDataScaling)
			}

			// set noiseNrg in valid region
			if firstPNSband == 0 {
				deltaiNoiseEnergy := noiseNrg[sfb] - lastiNoiseEnergy

				if deltaiNoiseEnergy > codeBookPnsLav {
					noiseNrg[sfb] -= deltaiNoiseEnergy - codeBookPnsLav
				} else if deltaiNoiseEnergy < -codeBookPnsLav {
					noiseNrg[sfb] -= deltaiNoiseEnergy + codeBookPnsLav
				}
			} else {
				firstPNSband = 0
			}
			lastiNoiseEnergy = noiseNrg[sfb]
		} else {
			// no PNS coding
			noiseNrg[sfb] = noNoisePns
		}
	}
}

// PreProcessPnsChannelPair is the 1:1 port of FDKaacEnc_PreProcessPnsChannelPair
// (aacenc_pns.cpp:441-480): calculate the noise correlation of a channel pair and
// store it in both channels' noiseEnergyCorrelation.
func PreProcessPnsChannelPair(sfbActive int, sfbEnergyLeft, sfbEnergyRight,
	sfbEnergyLeftLD, sfbEnergyRightLD, sfbEnergyMid []int32, pnsConf *PNSConfig,
	pnsDataLeft, pnsDataRight *PNSData) {

	if pnsConf.UsePns == 0 {
		return
	}

	for sfb := 0; sfb < sfbActive; sfb++ {
		var ccf int32
		quot := (sfbEnergyLeftLD[sfb] >> 1) + (sfbEnergyRightLD[sfb] >> 1)

		if quot < fl2fxconstDBL(-32.0/ldDataScaling) {
			ccf = 0
		} else {
			accu := sfbEnergyMid[sfb] -
				(((sfbEnergyLeft[sfb] >> 1) + (sfbEnergyRight[sfb] >> 1)) >> 1)
			sign := 0
			if accu < 0 {
				sign = 1
			}
			accu = fAbsDBL(accu)

			ccf = calcLdData(accu) + fl2fxconstDBL(1.0/ldDataScaling) - quot
			// ld(accu*2) = ld(accu) + 1
			if ccf >= 0 {
				ccf = maxvalDBL
			} else if sign != 0 {
				ccf = -calcInvLdData(ccf)
			} else {
				ccf = calcInvLdData(ccf)
			}
		}

		pnsDataLeft.NoiseEnergyCorrelation[sfb] = ccf
		pnsDataRight.NoiseEnergyCorrelation[sfb] = ccf
	}
}

// PostProcessPnsChannelPair is the 1:1 port of FDKaacEnc_PostProcessPnsChannelPair
// (aacenc_pns.cpp:498-541): if PNS is used in both channels, use the M/S mask to
// flag noise correlation (and disable PNS where it is not jointly coded).
func PostProcessPnsChannelPair(sfbActive int, pnsConf *PNSConfig,
	pnsDataLeft, pnsDataRight *PNSData, msMask []int, msDigest *int) {

	if pnsConf.UsePns == 0 {
		return
	}

	for sfb := 0; sfb < sfbActive; sfb++ {
		// MS post processing
		if msMask[sfb] != 0 {
			if pnsDataLeft.PnsFlag[sfb] != 0 && pnsDataRight.PnsFlag[sfb] != 0 {
				// AAC only: Standard. do this to avoid ms flags in layers that
				// should not have it.
				if pnsDataLeft.NoiseEnergyCorrelation[sfb] <= pnsConf.NoiseCorrelationThresh {
					msMask[sfb] = 0
					*msDigest = MsSome
				}
			} else {
				// No PNS coding
				pnsDataLeft.PnsFlag[sfb] = 0
				pnsDataRight.PnsFlag[sfb] = 0
			}
		}

		// Use MS flag to signal noise correlation if pns is active in both
		// channels.
		if pnsDataLeft.PnsFlag[sfb] != 0 && pnsDataRight.PnsFlag[sfb] != 0 {
			if pnsDataLeft.NoiseEnergyCorrelation[sfb] > pnsConf.NoiseCorrelationThresh {
				msMask[sfb] = 1
				*msDigest = MsSome
			}
		}
	}
}
