// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Threshold-adjustment leaf kernels of the AAC encoder rate-control loop. 1:1
// ports of the small independent helpers in libAACenc/src/adj_thr.cpp that
// FDKaacEnc_adaptThresholdsToPe / FDKaacEnc_AdjustThresholds drive: the
// per-sfb threshold-exponent precompute, the minSnr adaptation, the avoid-hole
// flag reset, the avoid-hole-excluded PE recompute, and the bit-reservoir
// save/spend curves plus the pe min/max smoothing.
//
// Pure fixed-point (FIXP_DBL == int32, INT == int). The LD-domain helpers
// (CalcInvLdData / CalcLdData / CalcLdInt) and fMultI / fDivNorm are the
// already-verified leaf kernels; these functions only sequence them, so every
// value is bit-identical regardless of -ffp-contract / vectorization. No float,
// no transcendental. The literal C `fMult` (== fixmul_DD) is the ARMv8 `smull;
// asr #31` form on this target, so it is ported as fixmulDDarm8, NOT the package
// fMult/fMultDD generic helper (which is the ((a*b)>>32)<<1 form). aacfdk-fenced.

package nativeaac

// calcThreshExp is the 1:1 port of FDKaacEnc_calcThreshExp (adj_thr.cpp:443-459):
// per active sfb, thrExp[ch][sfb] = CalcInvLdData(sfbThresholdLdData[sfb] >> 2),
// i.e. the fourth root of the threshold in the linear domain (4*log shifted by
// 2). thrExp and psyOutChannel/qcOutChannel are 2-channel arrays-of-pointers
// (mono uses index 0 only). The threshold lives on QC_OUT_CHANNEL; the loop
// bounds (sfbCnt/sfbPerGroup/maxSfbPerGroup) come from PSY_OUT_CHANNEL.
func calcThreshExp(thrExp [][]int32,
	qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel, nChannels int) {
	for ch := 0; ch < nChannels; ch++ {
		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
				thrExpLdData := qcOutChannel[ch].SfbThresholdLdData[sfbGrp+sfb] >> 2
				thrExp[ch][sfbGrp+sfb] = calcInvLdData(thrExpLdData)
			}
		}
	}
}

// adaptMinSnr is the 1:1 port of FDKaacEnc_adaptMinSnr (adj_thr.cpp:466-540):
// reduce the per-sfb minSnr requirement (qcOutChannel->sfbMinSnrLdData) by
// minSnr^minSnrRed, where minSnrRed depends on the band's energy relative to the
// channel-average energy (avgEnLD64 - sfbEnergyLdData). Computed entirely in the
// ld64 log domain; the >>6 / +0.09375 dance compensates the LD_DATA_SHIFT(==6)
// headroom the energy accumulation keeps.
func adaptMinSnr(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	msaParam *minSnrAdaptParam, nChannels int) {
	// FL2FXCONST_DBL(-0.00503012648262f): narrow through float32 (the `f` suffix).
	minSnrLimitLD64 := fl2fxconstDBL(float64(float32(-0.00503012648262))) // ld64(0.8f)

	msaParamMaxRed := msaParam.maxRed
	msaParamStartRatio := msaParam.startRatio
	// FL2FXCONST_DBL(0.3010299956f): the `f` suffix rounds the literal to float32
	// BEFORE the macro's *2^31, so the Go constant must narrow through float32 too.
	msaParamRedRatioFac := fixmulDDarm8(msaParam.redRatioFac, fl2fxconstDBL(float64(float32(0.3010299956))))
	msaParamRedOffs := msaParam.redOffs

	for ch := 0; ch < nChannels; ch++ {
		// calc average energy per scalefactor band
		nSfb := 0
		var accu int32 = 0

		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			maxSfbPerGroup := psyOutChannel[ch].MaxSfbPerGroup
			nSfb += maxSfbPerGroup
			for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
				accu += psyOutChannel[ch].SfbEnergy[sfbGrp+sfb] >> 6
			}
		}

		var avgEnLD64 int32
		if accu == 0 || nSfb == 0 {
			avgEnLD64 = fl2fxconstDBL(-1.0)
		} else {
			nSfbLD64 := calcLdInt(int32(nSfb))
			avgEnLD64 = calcLdData(accu)
			// FL2FXCONST_DBL(0.09375f): compensate the >>6 shift; narrow via float32.
			avgEnLD64 = avgEnLD64 + fl2fxconstDBL(float64(float32(0.09375))) - nSfbLD64
		}

		maxSfbPerGroup := psyOutChannel[ch].MaxSfbPerGroup
		sfbCnt := psyOutChannel[ch].SfbCnt
		sfbPerGroup := psyOutChannel[ch].SfbPerGroup

		for sfbGrp := 0; sfbGrp < sfbCnt; sfbGrp += sfbPerGroup {
			for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
				sfbEnergyLdData := qcOutChannel[ch].SfbEnergyLdData[sfbGrp+sfb]
				sfbMinSnrLdData := qcOutChannel[ch].SfbMinSnrLdData[sfbGrp+sfb]
				dbRatio := avgEnLD64 - sfbEnergyLdData
				update := msaParamStartRatio < dbRatio
				// scaled by 1.0f/64.0f
				minSnrRed := msaParamRedOffs + fixmulDDarm8(msaParamRedRatioFac, dbRatio)
				minSnrRed = fixMaxDBL(minSnrRed, msaParamMaxRed) // scaled by 1.0f/64.0f
				minSnrRed = fixmulDDarm8(sfbMinSnrLdData, minSnrRed) << 6
				minSnrRed = fixMinDBL(minSnrLimitLD64, minSnrRed)
				if update {
					qcOutChannel[ch].SfbMinSnrLdData[sfbGrp+sfb] = minSnrRed
				} else {
					qcOutChannel[ch].SfbMinSnrLdData[sfbGrp+sfb] = sfbMinSnrLdData
				}
			}
		}
	}
}

// resetAHFlags is the 1:1 port of FDKaacEnc_resetAHFlags (adj_thr.cpp:1854-1869):
// every avoid-hole flag that the reduction loop set to AH_ACTIVE is reset to
// AH_INACTIVE, leaving NO_AH bands untouched.
func resetAHFlags(ahFlag [][]uint8, nChannels int,
	psyOutChannel []*PsyOutChannel) {
	for ch := 0; ch < nChannels; ch++ {
		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
				if ahFlag[ch][sfbGrp+sfb] == ahActive {
					ahFlag[ch][sfbGrp+sfb] = ahInactive
				}
			}
		}
	}
}

// calcPeNoAH is the 1:1 port of FDKaacEnc_FDKaacEnc_calcPeNoAH
// (adj_thr.cpp:951-977): recompute pe/constPart/nActiveLines over only the bands
// NOT held by avoid-hole (ahFlag < AH_ACTIVE), starting from the PE offset. The
// constPart accumulates with CONSTPART_HEADROOM extra fractional bits, then both
// sums are shifted back into the PE_CONSTPART_SHIFT domain.
func calcPeNoAH(peData *peData, ahFlag [][]uint8,
	psyOutChannel []*PsyOutChannel, nChannels int) (pe, constPart, nActiveLines int) {
	peTmp := int(peData.offset)
	constPartTmp := 0
	nActiveLinesTmp := 0
	for ch := 0; ch < nChannels; ch++ {
		peChanData := &peData.peChannelData[ch]
		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
				if ahFlag[ch][sfbGrp+sfb] < ahActive {
					peTmp += int(peChanData.sfbPe[sfbGrp+sfb])
					constPartTmp += int(peChanData.sfbConstPart[sfbGrp+sfb]) >> constPartHeadroom
					nActiveLinesTmp += int(peChanData.sfbNActiveLines[sfbGrp+sfb])
				}
			}
		}
	}
	// correct scaled pe and constPart values
	pe = peTmp >> peConstPartShift
	constPart = constPartTmp >> (peConstPartShift - constPartHeadroom)
	nActiveLines = nActiveLinesTmp
	return pe, constPart, nActiveLines
}

// calcBitSave is the 1:1 port of FDKaacEnc_calcBitSave (adj_thr.cpp:2218-2231):
// the bit-reservoir SAVE percentage curve — clamp the fill level to
// [clipLow, clipHigh], then bitsave = maxBitSave - (fillLevel-clipLow)*slope.
func calcBitSave(fillLevel, clipLow, clipHigh, minBitSave, maxBitSave, bitsaveSlope int32) int32 {
	fillLevel = fixMaxDBL(fillLevel, clipLow)
	fillLevel = fixMinDBL(fillLevel, clipHigh)
	return maxBitSave - fixmulDDarm8(fillLevel-clipLow, bitsaveSlope)
}

// calcBitSpend is the 1:1 port of FDKaacEnc_calcBitSpend (adj_thr.cpp:2256-2270):
// the bit-reservoir SPEND percentage curve — clamp the fill level to
// [clipLow, clipHigh], then bitspend = minBitSpend + (fillLevel-clipLow)*slope.
func calcBitSpend(fillLevel, clipLow, clipHigh, minBitSpend, maxBitSpend, bitspendSlope int32) int32 {
	fillLevel = fixMaxDBL(fillLevel, clipLow)
	fillLevel = fixMinDBL(fillLevel, clipHigh)
	return minBitSpend + fixmulDDarm8(fillLevel-clipLow, bitspendSlope)
}

// adjustPeMinMax is the 1:1 port of FDKaacEnc_adjustPeMinMax (adj_thr.cpp:2281-2323):
// adapt the peMin/peMax bit-reservoir control band over time toward the current
// pe, then guarantee a minimum spread (minDiff_fix == currPe/6) by splitting it
// around currPe in proportion to the lo/hi parts. peMin/peMax are updated in
// place (returned). MAXVAL_DBL is the saturated maxFacHi.
func adjustPeMinMax(currPe int, peMin, peMax *int) {
	const maxvalDBL = int32(0x7FFFFFFF) // MAXVAL_DBL
	// FL2FXCONST_DBL(0.3f / 0.14f / 0.07f / 0.1666666667f): the `f` suffix rounds
	// each literal to float32 before the macro's *2^31, so narrow through float32.
	minFacHi := fl2fxconstDBL(float64(float32(0.3)))
	maxFacHi := maxvalDBL
	minFacLo := fl2fxconstDBL(float64(float32(0.14)))
	maxFacLo := fl2fxconstDBL(float64(float32(0.07)))
	var diff int

	minDiffFix := fMultI(fl2fxconstDBL(float64(float32(0.1666666667))), int32(currPe))

	if currPe > *peMax {
		diff = currPe - *peMax
		*peMin += int(fMultI(minFacHi, int32(diff)))
		*peMax += int(fMultI(maxFacHi, int32(diff)))
	} else if currPe < *peMin {
		diff = *peMin - currPe
		*peMin -= int(fMultI(minFacLo, int32(diff)))
		*peMax -= int(fMultI(maxFacLo, int32(diff)))
	} else {
		*peMin += int(fMultI(minFacHi, int32(currPe-*peMin)))
		*peMax -= int(fMultI(maxFacLo, int32(*peMax-currPe)))
	}

	if (*peMax - *peMin) < int(minDiffFix) {
		peMaxFix := *peMax
		peMinFix := *peMin

		partLoFix := int32(fixMax(0, currPe-peMinFix))
		partHiFix := int32(fixMax(0, peMaxFix-currPe))

		// fDivNorm(num, denom) here is the 2-arg exponent-0 overload (fDivNorm2),
		// NOT the 3-arg mantissa/exponent form.
		peMaxFix = currPe + int(fMultI(fDivNorm2(partHiFix, partLoFix+partHiFix), minDiffFix))
		peMinFix = currPe - int(fMultI(fDivNorm2(partLoFix, partLoFix+partHiFix), minDiffFix))
		peMinFix = fixMax(0, peMinFix)

		*peMax = peMaxFix
		*peMin = peMinFix
	}
}
