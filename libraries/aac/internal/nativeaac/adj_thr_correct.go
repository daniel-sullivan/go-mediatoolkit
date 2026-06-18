// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Multi-element threshold-correction stage of the AAC encoder
// threshold-adjustment DRIVER tier, ported 1:1 from the vendored FDK-AAC
// reference libAACenc/src/adj_thr.cpp. These three statics implement the
// "Part IV" closeout of FDKaacEnc_adaptThresholdsToPe — the (A)/(B)/(C) refinement
// chain that drives redPeGlobal toward desiredPe once the two reduction-value
// guesses converge:
//
//	(A) FDKaacEnc_correctThresh  — distribute the residual deltaPe across sfbs.
//	(B) FDKaacEnc_allowMoreHoles — quantise additional low-energy bands to zero.
//	(C) FDKaacEnc_reduceMinSnr   — raise the uppermost-sfb thresholds to minSnr 1dB.
//
// CBR/AAC-LC path only. The C uses scratch aliased onto QC_OUT_CHANNEL.quantSpec
// (sfbPeFactorsLdData) and qcElement[0]->dynMem_SfbNActiveLinesLdData; the Go port
// models that scratch with plain per-element/-channel int32 slices — the alias is
// pure memory reuse and does not affect the computed values.
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8; CountLeadingBits == fNorm; CalcLdInt/CalcInvLdData/
// CalcLdData/scaleValue/fMin/fixnormz_D are the already-verified leaf kernels.

// numNrgLevs / invIntTabSize / invSqrt4TabSize (adj_thr.cpp:107-116).
const numNrgLevs = 8

// correctThresh is the 1:1 port of FDKaacEnc_correctThresh (adj_thr.cpp:1320-1507):
// if the residual pe difference deltaPe is small enough, distribute it across the
// scalefactor bands proportional to each sfb's active-line count over its threshold
// exponent, deriving new thresholds and marking avoid-hole bands AH_ACTIVE.
//
// ahFlag/thrExp are [8][2][MAX_GROUPED_SFB] matrices (per element/channel/sfb).
func correctThresh(cm *ChannelMapping, qcElement []*QcOutElement,
	psyOutElement []*PsyOutElement, ahFlag [][][]uint8, thrExp [][][]int32,
	redValM int32, redValE int, deltaPe, processElements, elementOffset int) {

	nElements := elementOffset + processElements

	// scratch: sfbPeFactorsLdData[el][ch][sfb], sfbNActiveLinesLdData[el][ch][sfb]
	sfbPeFactorsLdData := make([][][]int32, len(qcElement))
	sfbNActiveLinesLdData := make([][][]int32, len(qcElement))
	for el := range qcElement {
		sfbPeFactorsLdData[el] = make([][]int32, 2)
		sfbNActiveLinesLdData[el] = make([][]int32, 2)
		for ch := 0; ch < 2; ch++ {
			sfbPeFactorsLdData[el][ch] = make([]int32, maxGroupedSFB)
			sfbNActiveLinesLdData[el][ch] = make([]int32, maxGroupedSFB)
		}
	}

	// for each sfb calc relative factors for pe changes
	normFactorInt := 0

	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
			psyOutChan := psyOutElement[elementId].PsyOutChannel[ch]
			peChanData := &qcElement[elementId].PeData.peChannelData[ch]

			for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
				for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
					if peChanData.sfbNActiveLines[sfbGrp+sfb] == 0 {
						sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] = fl2fxconstDBL(-1.0)
					} else {
						sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] =
							calcLdInt(peChanData.sfbNActiveLines[sfbGrp+sfb])
					}
					if ((ahFlag[elementId][ch][sfbGrp+sfb] < ahActive) || (deltaPe > 0)) &&
						peChanData.sfbNActiveLines[sfbGrp+sfb] != 0 {
						if thrExp[elementId][ch][sfbGrp+sfb] > -redValM {
							minScale := fixMin(int(fNorm(thrExp[elementId][ch][sfbGrp+sfb])),
								int(fNorm(redValM))-redValE) - 1

							// sumld = ld64( sfbThrExp + redVal )
							sumLd := calcLdData(
								scaleValue(thrExp[elementId][ch][sfbGrp+sfb], int32(minScale))+
									scaleValue(redValM, int32(redValE+minScale))) -
								int32(minScale<<(dfractBits-1-ldDataShift))

							if sumLd < fl2fxconstDBL(0.0) {
								sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] =
									sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] - sumLd
							} else {
								if sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] >
									(fl2fxconstDBL(-1.0) + sumLd) {
									sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] =
										sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] - sumLd
								} else {
									sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] =
										sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb]
								}
							}

							normFactorInt += int(calcInvLdData(
								sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb]))
						} else {
							sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] = fl2fxconstDBL(1.0)
						}
					} else {
						sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] = fl2fxconstDBL(-1.0)
					}
				}
			}
		}
	}

	// normFactorLdData = ld64(deltaPe/normFactorInt)
	deltaPeAbs := deltaPe
	if deltaPe < 0 {
		deltaPeAbs = -deltaPe
	}
	normFactorLdData := calcLdData(int32(deltaPeAbs)) - calcLdData(int32(normFactorInt))

	// distribute the pe difference to the scalefactors and calculate thresholds
	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
			qcOutChan := qcElement[elementId].QcOutChannel[ch]
			psyOutChan := psyOutElement[elementId].PsyOutChannel[ch]
			peChanData := &qcElement[elementId].PeData.peChannelData[ch]

			for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
				for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
					if peChanData.sfbNActiveLines[sfbGrp+sfb] > 0 {
						var thrFactorLdData int32
						// pe difference for this sfb
						if sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] == fl2fxconstDBL(-1.0) ||
							deltaPe == 0 {
							thrFactorLdData = fl2fxconstDBL(0.0)
						} else {
							// new threshold
							tmp := calcInvLdData(
								sfbPeFactorsLdData[elementId][ch][sfbGrp+sfb] +
									normFactorLdData -
									sfbNActiveLinesLdData[elementId][ch][sfbGrp+sfb] -
									fl2fxconstDBL(float64(ldDataShift)/ldDataScaling))

							// limit thrFactor to 60dB
							if deltaPe >= 0 {
								tmp = -tmp
							}
							thrFactorLdData = fMin(tmp, fl2fxconstDBL(20.0/ldDataScaling))
						}

						// new threshold
						sfbThrLdData := qcOutChan.SfbThresholdLdData[sfbGrp+sfb]
						sfbEnLdData := qcOutChan.SfbWeightedEnergyLdData[sfbGrp+sfb]

						var sfbThrReducedLdData int32
						if thrFactorLdData < fl2fxconstDBL(0.0) {
							if sfbThrLdData > (fl2fxconstDBL(-1.0) - thrFactorLdData) {
								sfbThrReducedLdData = sfbThrLdData + thrFactorLdData
							} else {
								sfbThrReducedLdData = fl2fxconstDBL(-1.0)
							}
						} else {
							sfbThrReducedLdData = sfbThrLdData + thrFactorLdData
						}

						// avoid hole
						if (sfbThrReducedLdData-sfbEnLdData > qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]) &&
							(ahFlag[elementId][ch][sfbGrp+sfb] == ahInactive) {
							if sfbEnLdData > (sfbThrLdData - qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]) {
								sfbThrReducedLdData = qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] + sfbEnLdData
							} else {
								sfbThrReducedLdData = sfbThrLdData
							}
							ahFlag[elementId][ch][sfbGrp+sfb] = ahActive
						}

						qcOutChan.SfbThresholdLdData[sfbGrp+sfb] = sfbThrReducedLdData
					}
				}
			}
		}
	}
}

// reduceMinSnr is the 1:1 port of FDKaacEnc_reduceMinSnr (adj_thr.cpp:1514-1600):
// if the desired pe cannot be reached, raise the uppermost-sfb thresholds to a
// minSnr of 1 dB (SnrLdFac), accumulating the resulting pe delta into redPeGlobal,
// starting at the highest sfb and walking down until the budget is met.
//
// redPeGlobal is updated in place (returned).
func reduceMinSnr(cm *ChannelMapping, qcElement []*QcOutElement,
	psyOutElement []*PsyOutElement, ahFlag [][][]uint8, desiredPe int,
	redPeGlobal *int, processElements, elementOffset int) {

	globalMaxSfb := 0
	nElements := elementOffset + processElements
	newGlobalPe := *redPeGlobal

	if newGlobalPe <= desiredPe {
		*redPeGlobal = newGlobalPe
		return
	}

	// global maximum of maxSfbPerGroup
	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
			globalMaxSfb = fixMax(globalMaxSfb,
				psyOutElement[elementId].PsyOutChannel[ch].MaxSfbPerGroup)
		}
	}

	// as long as globalPE is above desirePE reduce SNR to 1.0 dB, starting at
	// highest SFB
	for {
		globalMaxSfb--
		if !(newGlobalPe > desiredPe && globalMaxSfb >= 0) {
			break
		}
		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			peData := &qcElement[elementId].PeData

			for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
				qcOutChan := qcElement[elementId].QcOutChannel[ch]
				psyOutChan := psyOutElement[elementId].PsyOutChannel[ch]

				// try to reduce SNR of channel's uppermost SFB(s)
				if globalMaxSfb < psyOutChan.MaxSfbPerGroup {
					deltaPe := 0

					for sfb := globalMaxSfb; sfb < psyOutChan.SfbCnt; sfb += psyOutChan.SfbPerGroup {
						if ahFlag[elementId][ch][sfb] != noAH &&
							qcOutChan.SfbMinSnrLdData[sfb] < snrLdFac &&
							(qcOutChan.SfbWeightedEnergyLdData[sfb] >
								qcOutChan.SfbThresholdLdData[sfb]-snrLdFac) {
							// increase threshold to new minSnr of 1dB
							qcOutChan.SfbMinSnrLdData[sfb] = snrLdFac
							qcOutChan.SfbThresholdLdData[sfb] =
								qcOutChan.SfbWeightedEnergyLdData[sfb] + snrLdFac

							// calc new pe; C2 + C3*ld(1/0.8) = 1.5
							deltaPe -= int(peData.peChannelData[ch].sfbPe[sfb])

							// sfbPe = 1.5 * sfbNLines
							peData.peChannelData[ch].sfbPe[sfb] =
								(3 * peData.peChannelData[ch].sfbNLines[sfb]) << (peConstPartShift - 1)
							deltaPe += int(peData.peChannelData[ch].sfbPe[sfb])
						}
					}

					deltaPe >>= peConstPartShift
					peData.pe += int32(deltaPe)
					peData.peChannelData[ch].pe += int32(deltaPe)
					newGlobalPe += deltaPe
				}

				// stop if enough has been saved
				if newGlobalPe <= desiredPe {
					*redPeGlobal = newGlobalPe
					return
				}
			}
		}
	}

	*redPeGlobal = newGlobalPe
}

// allowMoreHoles is the 1:1 port of FDKaacEnc_allowMoreHoles (adj_thr.cpp:1607-1851):
// if the desired pe still cannot be reached, quantise additional scalefactor bands
// to zero — first the less-energetic channel of an M/S pair, then bands below a
// sliding energy border ascending from minEn toward avgEn.
func allowMoreHoles(cm *ChannelMapping, qcElement []*QcOutElement,
	psyOutElement []*PsyOutElement, adjThrStateElement []*atsElement,
	ahFlag [][][]uint8, desiredPe, currentPe, processElements, elementOffset int) {

	nElements := elementOffset + processElements
	actPe := currentPe

	if actPe <= desiredPe {
		return // nothing to do
	}

	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		peData := &qcElement[elementId].PeData
		nChannels := cm.ElInfo[elementId].NChannelsInEl

		var qcOutChannel [2]*QcOutChannel
		var psyOutChannel [2]*PsyOutChannel

		for ch := 0; ch < nChannels; ch++ {
			qcOutChannel[ch] = qcElement[elementId].QcOutChannel[ch]
			psyOutChannel[ch] = psyOutElement[elementId].PsyOutChannel[ch]

			for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
				for sfb := psyOutChannel[ch].MaxSfbPerGroup; sfb < psyOutChannel[ch].SfbPerGroup; sfb++ {
					peData.peChannelData[ch].sfbPe[sfbGrp+sfb] = 0
				}
			}
		}

		// for MS allow hole in the channel with less energy
		if nChannels == 2 && psyOutChannel[0].LastWindowSequence == psyOutChannel[1].LastWindowSequence {
			for sfb := psyOutChannel[0].MaxSfbPerGroup - 1; sfb >= 0; sfb-- {
				for sfbGrp := 0; sfbGrp < psyOutChannel[0].SfbCnt; sfbGrp += psyOutChannel[0].SfbPerGroup {
					if psyOutElement[elementId].ToolsInfo.MsMask[sfbGrp+sfb] != 0 {
						energyLdL := qcOutChannel[0].SfbWeightedEnergyLdData[sfbGrp+sfb]
						energyLdR := qcOutChannel[1].SfbWeightedEnergyLdData[sfbGrp+sfb]

						// allow hole in side channel ?
						if ahFlag[elementId][1][sfbGrp+sfb] != noAH &&
							((fl2fxconstDBL(-0.02065512648)>>1)+(qcOutChannel[0].SfbMinSnrLdData[sfbGrp+sfb]>>1)) >
								((energyLdR>>1)-(energyLdL>>1)) {
							ahFlag[elementId][1][sfbGrp+sfb] = noAH
							qcOutChannel[1].SfbThresholdLdData[sfbGrp+sfb] =
								fl2fxconstDBL(0.015625) + energyLdR
							actPe -= int(peData.peChannelData[1].sfbPe[sfbGrp+sfb]) >> peConstPartShift
						} else if ahFlag[elementId][0][sfbGrp+sfb] != noAH &&
							// allow hole in mid channel ?
							((fl2fxconstDBL(-0.02065512648)>>1)+(qcOutChannel[1].SfbMinSnrLdData[sfbGrp+sfb]>>1)) >
								((energyLdL>>1)-(energyLdR>>1)) {
							ahFlag[elementId][0][sfbGrp+sfb] = noAH
							qcOutChannel[0].SfbThresholdLdData[sfbGrp+sfb] =
								fl2fxconstDBL(0.015625) + energyLdL
							actPe -= int(peData.peChannelData[0].sfbPe[sfbGrp+sfb]) >> peConstPartShift
						}
					}
				}
				if actPe <= desiredPe {
					return // stop if enough has been saved
				}
			}
		}
	}

	if actPe > desiredPe {
		// more holes necessary? subsequently erase bands starting with low energies
		var startSfb [8]int
		var sfbCnt [8]int
		var sfbPerGroup [8]int
		var maxSfbPerGroup [8]int
		var enLD64 [numNrgLevs]int32

		// get the scaling factor over all audio elements and channels
		maxSfb := 0
		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
				for sfbGrp := 0; sfbGrp < psyOutElement[elementId].PsyOutChannel[ch].SfbCnt; sfbGrp += psyOutElement[elementId].PsyOutChannel[ch].SfbPerGroup {
					maxSfb += psyOutElement[elementId].PsyOutChannel[ch].MaxSfbPerGroup
				}
			}
		}
		avgEnE := int(dfractBits) - int(fixnormzD(int32(fixMax(0, maxSfb-1)))) // ilog2()

		ahCnt := 0
		maxSfb = 0
		minSfb := encMaxSfb
		var avgEn int32 = fl2fxconstDBL(0.0)
		var minEnLD64 int32 = fl2fxconstDBL(0.0)

		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
				chIdx := cm.ElInfo[elementId].ChannelIndex[ch]
				qcOutChannel := qcElement[elementId].QcOutChannel[ch]
				psyOutChannel := psyOutElement[elementId].PsyOutChannel[ch]

				maxSfbPerGroup[chIdx] = psyOutChannel.MaxSfbPerGroup
				sfbCnt[chIdx] = psyOutChannel.SfbCnt
				sfbPerGroup[chIdx] = psyOutChannel.SfbPerGroup

				maxSfb = fixMax(maxSfb, psyOutChannel.MaxSfbPerGroup)

				if psyOutChannel.LastWindowSequence != encShortWindow {
					startSfb[chIdx] = adjThrStateElement[elementId].ahParam.startSfbL
				} else {
					startSfb[chIdx] = adjThrStateElement[elementId].ahParam.startSfbS
				}

				minSfb = fixMin(minSfb, startSfb[chIdx])

				sfbGrp := 0
				sfb := startSfb[chIdx]

				for {
					for ; sfb < psyOutChannel.MaxSfbPerGroup; sfb++ {
						if ahFlag[elementId][ch][sfbGrp+sfb] != noAH &&
							qcOutChannel.SfbWeightedEnergyLdData[sfbGrp+sfb] >
								qcOutChannel.SfbThresholdLdData[sfbGrp+sfb] {
							minEnLD64 = fMin(minEnLD64, qcOutChannel.SfbEnergyLdData[sfbGrp+sfb])
							avgEn += qcOutChannel.SfbEnergy[sfbGrp+sfb] >> uint(avgEnE)
							ahCnt++
						}
					}
					sfbGrp += psyOutChannel.SfbPerGroup
					sfb = startSfb[chIdx]
					if sfbGrp >= psyOutChannel.SfbCnt {
						break
					}
				}
			}
		}

		var avgEnLD64 int32
		if avgEn == fl2fxconstDBL(0.0) || ahCnt == 0 {
			avgEnLD64 = fl2fxconstDBL(0.0)
		} else {
			avgEnLD64 = calcLdData(avgEn) +
				int32(avgEnE<<(dfractBits-1-ldDataShift)) -
				calcLdInt(int32(ahCnt))
		}

		// calc some energy borders between minEn and avgEn
		enLD64[0] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.06666667))
		enLD64[1] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.20000000))
		enLD64[2] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.33333334))
		enLD64[3] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.46666667))
		enLD64[4] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.60000002))
		enLD64[5] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.73333335))
		enLD64[6] = minEnLD64 + fMult(avgEnLD64-minEnLD64, fl2fxconstDBL(0.86666667))
		enLD64[7] = minEnLD64 + (avgEnLD64 - minEnLD64)

		done := 0
		enIdx := 0
		sfb := maxSfb - 1

		for done == 0 {
			for elementId := elementOffset; elementId < nElements; elementId++ {
				if cm.ElInfo[elementId].ElType == IDDSE {
					continue
				}
				peData := &qcElement[elementId].PeData
				for ch := 0; ch < cm.ElInfo[elementId].NChannelsInEl; ch++ {
					chIdx := cm.ElInfo[elementId].ChannelIndex[ch]
					qcOutChannel := qcElement[elementId].QcOutChannel[ch]
					if sfb >= startSfb[chIdx] && sfb < maxSfbPerGroup[chIdx] {
						for sfbGrp := 0; sfbGrp < sfbCnt[chIdx]; sfbGrp += sfbPerGroup[chIdx] {
							// sfb energy below border ?
							if ahFlag[elementId][ch][sfbGrp+sfb] != noAH &&
								qcOutChannel.SfbEnergyLdData[sfbGrp+sfb] < enLD64[enIdx] {
								// allow hole
								ahFlag[elementId][ch][sfbGrp+sfb] = noAH
								qcOutChannel.SfbThresholdLdData[sfbGrp+sfb] =
									fl2fxconstDBL(0.015625) +
										qcOutChannel.SfbWeightedEnergyLdData[sfbGrp+sfb]
								actPe -= int(peData.peChannelData[ch].sfbPe[sfbGrp+sfb]) >> peConstPartShift
							}
							if actPe <= desiredPe {
								return // stop if enough has been saved
							}
						}
					}
				}
			}

			sfb--
			if sfb < minSfb {
				// restart with next energy border
				sfb = maxSfb
				enIdx++
				if enIdx >= numNrgLevs {
					done = 1
				}
			}
		}
	}
}
