// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Perceptual-entropy DRIVER stage of the AAC encoder threshold-adjustment loop,
// ported 1:1 from the vendored FDK-AAC reference libAACenc/src/adj_thr.cpp. These
// three statics sit directly above the already-ported line_pe.cpp leaves
// (prepareSfbPe / calcSfbPe): FDKaacEnc_preparePe precomputes the constant
// per-sfb line counts; FDKaacEnc_calcWeighting derives the ld64-domain energy
// weighting factor (sfbEnFacLd) from the audible-spectrum flatness; and
// FDKaacEnc_calcPe sums the per-channel perceptual entropy against the granted
// bit budget. FDKaacEnc_peCalculation (the non-static driver that chains the
// three plus the energy/threshold weighting) is the orchestration tier and is
// not ported here.
//
// CBR/AAC-LC path only. usePatchTool is hard-wired to 1 by the only caller
// (FDKaacEnc_peCalculation), so the disabled-tool early-return is carried for
// fidelity but the weighting body always runs.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8 on the aarch64 target (KEEPS bit 31).

// preparePe is the 1:1 port of FDKaacEnc_preparePe (adj_thr.cpp:719-734):
// per channel run FDKaacEnc_prepareSfbPe to fill the constant sfbNLines, then
// stamp peData.offset with the fixed PE offset.
func preparePe(peData *peData, psyOutChannel []*PsyOutChannel,
	qcOutChannel []*QcOutChannel, nChannels, peOffset int) {
	for ch := 0; ch < nChannels; ch++ {
		psyOutChan := psyOutChannel[ch]
		// psyOutChan->sfbEnergyLdData / sfbThresholdLdData alias the QC_OUT_CHANNEL
		// memory (interface.h:140), held on QcOutChannel in the Go model.
		qcOutChan := qcOutChannel[ch]
		sfbOffset := make([]int32, psyOutChan.SfbCnt+1)
		for i := 0; i <= psyOutChan.SfbCnt; i++ {
			sfbOffset[i] = int32(psyOutChan.SfbOffsets[i])
		}
		prepareSfbPe(&peData.peChannelData[ch],
			qcOutChan.SfbEnergyLdData[:], qcOutChan.SfbThresholdLdData[:],
			qcOutChan.SfbFormFactorLdData[:], sfbOffset,
			psyOutChan.SfbCnt, psyOutChan.SfbPerGroup, psyOutChan.MaxSfbPerGroup)
	}
	peData.offset = int32(peOffset)
}

// calcWeighting is the 1:1 port of FDKaacEnc_calcWeighting (adj_thr.cpp:755-878):
// derive the per-sfb energy weighting factor sfbEnFacLd (in the ld64 log domain)
// from the audible-spectrum spectral flatness so the threshold adjustment can
// favour tonal over noisy regions. Only the no-short-window branch computes a
// patch; the short-window branch resets the patch state for the next frame.
//
// usePatchTool: the only caller passes 1 (enabled). The else-branch (disabled
// tool early-return after clearing sfbEnFacLd) is carried for 1:1 fidelity.
//
// fDivNorm(num, denom) is the 2-arg overload (fixpoint_math.cpp:481-499):
// scaleValue(fDivNorm(num,denom,&e), e) with a saturate-to-MAXVAL_DBL guard when
// the normalised mantissa is exactly 0.5 and e == 1.
func calcWeighting(peData *peData, psyOutChannel []*PsyOutChannel,
	qcOutChannel []*QcOutChannel, toolsInfo *PsyOutToolsInfo,
	adjThrStateElement *atsElement, nChannels, usePatchTool int) {
	noShortWindowInFrame := true
	exePatchM := 0

	for ch := 0; ch < nChannels; ch++ {
		if psyOutChannel[ch].LastWindowSequence == encShortWindow {
			noShortWindowInFrame = false
		}
		// FDKmemclear(qcOutChannel[ch]->sfbEnFacLd, MAX_GROUPED_SFB)
		for i := range qcOutChannel[ch].SfbEnFacLd {
			qcOutChannel[ch].SfbEnFacLd[i] = 0
		}
	}

	if usePatchTool == 0 {
		return // tool is disabled
	}

	for ch := 0; ch < nChannels; ch++ {
		psyOutChan := psyOutChannel[ch]
		qcOutChan := qcOutChannel[ch]

		if noShortWindowInFrame { // retain energy ratio between blocks of different length
			var nrgSum14, nrgSum12, nrgSum34, nrgTotal int32
			nLinesSum := 0

			// calculate flatness of audible spectrum (above masking threshold)
			for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
				for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
					nrgFac12 := calcInvLdData(qcOutChan.SfbEnergyLdData[sfbGrp+sfb] >> 1) // nrg^(1/2)
					nrgFac14 := calcInvLdData(qcOutChan.SfbEnergyLdData[sfbGrp+sfb] >> 2) // nrg^(1/4)

					// maximal number of bands is 64 -> results scaling factor 6
					nLinesSum += int(peData.peChannelData[ch].sfbNLines[sfbGrp+sfb]) // relevant lines
					nrgTotal += psyOutChan.SfbEnergy[sfbGrp+sfb] >> 6                // sum up nrg
					nrgSum12 += nrgFac12 >> 6                                        // sum up nrg^(2/4)
					nrgSum14 += nrgFac14 >> 6                                        // sum up nrg^(1/4)
					nrgSum34 += fixmulDDarm8(nrgFac14, nrgFac12) >> 6                // sum up nrg^(3/4)
				}
			}

			nrgTotal = calcLdData(nrgTotal) // ld64 of total nrg

			nrgFacLd14 := calcLdData(nrgSum14) - nrgTotal // ld64(nrgSum14/nrgTotal)
			nrgFacLd12 := calcLdData(nrgSum12) - nrgTotal // ld64(nrgSum12/nrgTotal)
			nrgFacLd34 := calcLdData(nrgSum34) - nrgTotal // ld64(nrgSum34/nrgTotal)

			// nLinesSum cannot exceed total lines (prepareSfbPe takes care of it)
			adjThrStateElement.chaosMeasureEnFac[ch] = fMax(fl2fxconstDBL(0.1875),
				fDivNorm2(int32(nLinesSum), int32(psyOutChan.SfbOffsets[psyOutChan.SfbCnt])))

			usePatch := 0
			if adjThrStateElement.chaosMeasureEnFac[ch] > fl2fxconstDBL(0.78125) {
				usePatch = 1
			}
			exePatch := 0
			if usePatch != 0 && adjThrStateElement.lastEnFacPatch[ch] != 0 {
				exePatch = 1
			}

			for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
				for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
					var sfbExePatch int
					// for MS coupled SFBs, also execute patch in side channel if done in mid
					if ch == 1 && toolsInfo.MsMask[sfbGrp+sfb] != 0 {
						sfbExePatch = exePatchM
					} else {
						sfbExePatch = exePatch
					}

					if sfbExePatch != 0 && psyOutChan.SfbEnergy[sfbGrp+sfb] > 0 {
						// execute patch based on spectral flatness calculated above
						switch {
						case adjThrStateElement.chaosMeasureEnFac[ch] > fl2fxconstDBL(0.8125):
							qcOutChan.SfbEnFacLd[sfbGrp+sfb] =
								(nrgFacLd14 +
									(qcOutChan.SfbEnergyLdData[sfbGrp+sfb] +
										(qcOutChan.SfbEnergyLdData[sfbGrp+sfb] >> 1))) >> 1 // sfbEnergy^(3/4)
						case adjThrStateElement.chaosMeasureEnFac[ch] > fl2fxconstDBL(0.796875):
							qcOutChan.SfbEnFacLd[sfbGrp+sfb] =
								(nrgFacLd12 + qcOutChan.SfbEnergyLdData[sfbGrp+sfb]) >> 1 // sfbEnergy^(2/4)
						default:
							qcOutChan.SfbEnFacLd[sfbGrp+sfb] =
								(nrgFacLd34 + (qcOutChan.SfbEnergyLdData[sfbGrp+sfb] >> 1)) >> 1 // sfbEnergy^(1/4)
						}
						qcOutChan.SfbEnFacLd[sfbGrp+sfb] = fMin(qcOutChan.SfbEnFacLd[sfbGrp+sfb], 0)
					}
				}
			} // sfb loop

			adjThrStateElement.lastEnFacPatch[ch] = usePatch
			exePatchM = exePatch
		} else {
			// !noShortWindowInFrame
			adjThrStateElement.chaosMeasureEnFac[ch] = fl2fxconstDBL(0.75)
			// allow use of sfbEnFac patch in upcoming frame
			adjThrStateElement.lastEnFacPatch[ch] = 1 // TRUE
		}
	} // ch loop
}

// calcPe is the 1:1 port of FDKaacEnc_calcPe (adj_thr.cpp:885-905): for both
// channels run FDKaacEnc_calcSfbPe over the weighted energies/thresholds and
// accumulate the element-level pe/constPart/nActiveLines (pe seeded with the
// fixed peData.offset).
func calcPe(psyOutChannel []*PsyOutChannel, qcOutChannel []*QcOutChannel,
	peData *peData, nChannels int) {
	peData.pe = peData.offset
	peData.constPart = 0
	peData.nActiveLines = 0
	for ch := 0; ch < nChannels; ch++ {
		peChanData := &peData.peChannelData[ch]
		psyOutChan := psyOutChannel[ch]

		isBook := make([]int32, MaxGroupedSFB)
		isScale := make([]int32, MaxGroupedSFB)
		for i := 0; i < MaxGroupedSFB; i++ {
			isBook[i] = int32(psyOutChan.IsBook[i])
			isScale[i] = int32(psyOutChan.IsScale[i])
		}

		calcSfbPe(peChanData,
			qcOutChannel[ch].SfbWeightedEnergyLdData[:],
			qcOutChannel[ch].SfbThresholdLdData[:],
			psyOutChan.SfbCnt, psyOutChan.SfbPerGroup, psyOutChan.MaxSfbPerGroup,
			isBook, isScale)

		peData.pe += peChanData.pe
		peData.constPart += peChanData.constPart
		peData.nActiveLines += peChanData.nActiveLines
	}
}

// fDivNorm2 is the 1:1 port of the 2-arg overload fDivNorm(FIXP_DBL num,
// FIXP_DBL denom) (fixpoint_math.cpp:481-499): exponent-0 normalised division
// num/denom (precondition denom >= num) built on the 3-arg fDivNorm, with the
// saturate-to-MAXVAL_DBL guard for the exact-0.5/e==1 overflow case.
//
//	res = fDivNorm(num, denom, &e);
//	if (res == (1 << (DFRACT_BITS-2)) && e == 1) res = MAXVAL_DBL;
//	else res = scaleValue(res, e);
func fDivNorm2(num, denom int32) int32 {
	res, e := fDivNorm(num, denom)
	if res == int32(1)<<(dfractBits-2) && e == 1 {
		return maxvalDBL // MAXVAL_DBL
	}
	return scaleValue(res, e)
}
