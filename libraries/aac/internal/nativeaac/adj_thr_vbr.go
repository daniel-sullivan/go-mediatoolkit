// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// VBR (variable-bitrate) threshold-adaptation stage of the AAC encoder
// threshold-adjustment loop, ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/adj_thr.cpp. This is the else-branch sibling of the CBR
// adaptThresholdsToPe path: instead of reducing thresholds to hit a granted PE,
// the VBR path reduces them by a fixed quality-driven reduction value derived
// from a per-frame chaos measure (FDKaacEnc_calcChaosMeasure, in adj_thr_hole.go)
// and the bitrate-mode quality factor (vbrQualFactor, seeded by QCInit).
//
// Pure fixed-point: every value is an int32 FIXP_DBL / INT with carried block
// exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8; fMultDiv2 == fMultDiv2DD; CntLeadingZeros (on the
// strictly-positive frameEnergy) == fNormzPos; schur_div / CalcLdData /
// CalcInvLdData / CalcLdInt / fMin / fMax are the already-verified leaf kernels.

// invInt is the 1:1 port of the file-scope static invInt[INV_INT_TAB_SIZE]
// (adj_thr.cpp:108-111): 1/n in Q1.31 for n in [0,7] (index 0 saturated). Used
// to correct the short-block per-group energy by the group's window count.
var invInt = [8]int32{
	int32(0x7fffffff), int32(0x7fffffff), 0x40000000, 0x2aaaaaaa,
	0x20000000, 0x19999999, 0x15555555, 0x12492492,
}

// invSqrt4 is the 1:1 port of the file-scope static invSqrt4[INV_SQRT4_TAB_SIZE]
// (adj_thr.cpp:113-116): n^(-1/4) in Q1.31 for n in [0,7] (index 0 saturated).
// Used to scale the short-block per-sfb threshold exponent by the group length.
var invSqrt4 = [8]int32{
	int32(0x7fffffff), int32(0x7fffffff), 0x6ba27e65, 0x61424bb5,
	0x5a827999, 0x55994845, 0x51c8e33c, 0x4eb160d1,
}

// reduceThresholdsVBR is the 1:1 port of FDKaacEnc_reduceThresholdsVBR
// (adj_thr.cpp:1108-1312): the VBR threshold-reduction formula. It accumulates
// per-group / per-channel energy, derives a frame chaos measure (smoothed across
// frames via chaosMeasureOld), maps it through the characteristic curve, computes
// a reduction value per group (long block: one redVal[0]), then reduces each
// active sfb's threshold by that value while guarding against creating holes.
//
// ahFlag/thrExp are [2][MAX_GROUPED_SFB] matrices (per channel/sfb).
func reduceThresholdsVBR(
	qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	ahFlag [][]uint8, thrExp [][]int32, nChannels int,
	vbrQualFactor int32, chaosMeasureOld *int32) {

	const (
		scaleGroupEnergy = 8 // SCALE_GROUP_ENERGY
		winTypeScale     = 3 // WIN_TYPE_SCALE
	)
	// CONST_CHAOS_MEAS_AVG_FAC_0 == FL2FXCONST_DBL(0.25f),
	// CONST_CHAOS_MEAS_AVG_FAC_1 == FL2FXCONST_DBL(1.f - 0.25f). The C literals
	// carry the `f` suffix, so the expression is evaluated in float32 then widened
	// to double by the FL2FXCONST_DBL cast — fl2fxconstDBLf reproduces that.
	constChaosMeasAvgFac0 := fl2fxconstDBLf(0.25)
	constChaosMeasAvgFac1 := fl2fxconstDBLf(float32(1.0) - float32(0.25))
	// MIN_LDTHRESH == FL2FXCONST_DBL(-0.515625f).
	minLdThresh := fl2fxconstDBLf(-0.515625)

	var chGroupEnergy [TransFac][2]int32 // energy for each group and channel
	var chChaosMeasure [2]int32
	frameEnergy := fl2fxconstDBLf(1e-10) // FL2FXCONST_DBL(1e-10f)
	chaosMeasure := fl2fxconstDBLf(0.0)
	var redVal [TransFac]int32

	for ch := 0; ch < nChannels; ch++ {
		psyOutChan := psyOutChannel[ch]

		// adding up energy for each channel and each group separately
		chEnergy := fl2fxconstDBLf(0.0)
		groupCnt := 0

		for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
			chGroupEnergy[groupCnt][ch] = fl2fxconstDBLf(0.0)
			for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
				chGroupEnergy[groupCnt][ch] += psyOutChan.SfbEnergy[sfbGrp+sfb] >> scaleGroupEnergy
			}
			chEnergy += chGroupEnergy[groupCnt][ch]
			groupCnt++
		}
		frameEnergy += chEnergy

		// chaosMeasure
		if psyOutChannel[0].LastWindowSequence == encShortWindow {
			// assume a constant chaos measure of 0.5f for short blocks
			chChaosMeasure[ch] = fl2fxconstDBLf(0.5)
		} else {
			chChaosMeasure[ch] = calcChaosMeasure(psyOutChannel[ch], qcOutChannel[ch],
				qcOutChannel[ch].SfbFormFactorLdData[:])
		}
		chaosMeasure += fMult(chChaosMeasure[ch], chEnergy)
	}

	if frameEnergy > chaosMeasure {
		scale := int32(fNormzPos(frameEnergy)) - 1
		num := chaosMeasure << uint(scale)
		denum := frameEnergy << uint(scale)
		chaosMeasure = schurDiv(num, denum, 16)
	} else {
		chaosMeasure = fl2fxconstDBLf(1.0)
	}

	// averaging chaos measure
	chaosMeasureAvg := fMult(constChaosMeasAvgFac0, chaosMeasure) +
		fMult(constChaosMeasAvgFac1, *chaosMeasureOld)
	// use min-value, safe for next frame
	chaosMeasure = fMin(chaosMeasure, chaosMeasureAvg)
	*chaosMeasureOld = chaosMeasure

	// characteristic curve
	//   chaosMeasure = 0.2f + 0.7f/0.3f * (chaosMeasure - 0.2f);
	//   chaosMeasure = fixMin(1.0f, fixMax(0.1f, chaosMeasure));
	//   constants scaled by 4.f
	// 0.7f / (4.f * 0.3f): float32 arithmetic (0.3f is not exactly representable),
	// so compute in float32 then widen, matching FL2FXCONST_DBL(0.7f/(4.f*0.3f)).
	chaosMeasure = (fl2fxconstDBLf(0.2) >> 2) +
		fMult(fl2fxconstDBLf(float32(0.7)/(float32(4.0)*float32(0.3))), chaosMeasure-fl2fxconstDBLf(0.2))
	chaosMeasure = fMin(fl2fxconstDBLf(1.0)>>2,
		fMax(fl2fxconstDBLf(0.1)>>2, chaosMeasure)) << 2

	// calculation of reduction value
	if psyOutChannel[0].LastWindowSequence == encShortWindow { // short-blocks
		groupCnt := 0
		for sfbGrp := 0; sfbGrp < psyOutChannel[0].SfbCnt; sfbGrp += psyOutChannel[0].SfbPerGroup {
			groupEnergy := fl2fxconstDBLf(0.0)

			for ch := 0; ch < nChannels; ch++ {
				groupEnergy += chGroupEnergy[groupCnt][ch] // adding up the channels groupEnergy
			}

			// correction of group energy
			groupEnergy = fMult(groupEnergy, invInt[psyOutChannel[0].GroupLen[groupCnt]])
			// do not allow a higher redVal than calculated framewise
			groupEnergy = fMin(groupEnergy, frameEnergy>>winTypeScale)

			// 2*WIN_TYPE_SCALE = 6 => 6+2 = 8 ==> 8/4 = int number
			groupEnergy >>= 2

			redVal[groupCnt] = fMult(fMult(vbrQualFactor, chaosMeasure),
				calcInvLdData(calcLdData(groupEnergy)>>2)) <<
				uint((2+(2*winTypeScale)+scaleGroupEnergy)>>2)
			groupCnt++
		}
	} else { // long-block
		redVal[0] = fMult(fMult(vbrQualFactor, chaosMeasure),
			calcInvLdData(calcLdData(frameEnergy)>>2)) << uint(scaleGroupEnergy>>2)
	}

	for ch := 0; ch < nChannels; ch++ {
		qcOutChan := qcOutChannel[ch]
		psyOutChan := psyOutChannel[ch]

		for sfbGrp := 0; sfbGrp < psyOutChan.SfbCnt; sfbGrp += psyOutChan.SfbPerGroup {
			for sfb := 0; sfb < psyOutChan.MaxSfbPerGroup; sfb++ {
				sfbEnLdData := qcOutChan.SfbWeightedEnergyLdData[sfbGrp+sfb]
				sfbThrLdData := qcOutChan.SfbThresholdLdData[sfbGrp+sfb]
				sfbThrExp := thrExp[ch][sfbGrp+sfb]

				if sfbThrLdData >= minLdThresh && sfbEnLdData > sfbThrLdData &&
					ahFlag[ch][sfbGrp+sfb] != ahActive {

					var sfbThrReducedLdData int32

					// Short-Window
					if psyOutChannel[ch].LastWindowSequence == encShortWindow {
						groupNumber := sfb / psyOutChan.SfbPerGroup

						// FL2FXCONST_DBL(2.82f / 4.f): float32 arithmetic then widen.
						sfbThrExp = fMult(sfbThrExp,
							fMult(fl2fxconstDBLf(float32(2.82)/float32(4.0)),
								invSqrt4[psyOutChan.GroupLen[groupNumber]])) << 2

						if sfbThrExp <= (limitThrReducedLdData - redVal[groupNumber]) {
							sfbThrReducedLdData = fl2fxconstDBLf(-1.0)
						} else {
							if redVal[groupNumber] >= fl2fxconstDBLf(1.0)-sfbThrExp {
								sfbThrReducedLdData = fl2fxconstDBLf(0.0)
							} else {
								// threshold reduction formula
								sfbThrReducedLdData = calcLdData(sfbThrExp + redVal[groupNumber])
								sfbThrReducedLdData <<= 2
							}
						}
						sfbThrReducedLdData += calcLdInt(int32(psyOutChan.GroupLen[groupNumber])) -
							(int32(6) << (dfractBits - 1 - ldDataShift))
					} else {
						// Long-Window
						if redVal[0] >= fl2fxconstDBLf(1.0)-sfbThrExp {
							sfbThrReducedLdData = fl2fxconstDBLf(0.0)
						} else {
							// threshold reduction formula
							sfbThrReducedLdData = calcLdData(sfbThrExp + redVal[0])
							sfbThrReducedLdData <<= 2
						}
					}

					// avoid holes
					if (sfbThrReducedLdData-sfbEnLdData) > qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] &&
						ahFlag[ch][sfbGrp+sfb] != noAH {
						if qcOutChan.SfbMinSnrLdData[sfbGrp+sfb] > (fl2fxconstDBLf(-1.0) - sfbEnLdData) {
							sfbThrReducedLdData = fMax(
								qcOutChan.SfbMinSnrLdData[sfbGrp+sfb]+sfbEnLdData, sfbThrLdData)
						} else {
							sfbThrReducedLdData = sfbThrLdData
						}
						ahFlag[ch][sfbGrp+sfb] = ahActive
					}

					if sfbThrReducedLdData < fl2fxconstDBLf(-0.5) {
						sfbThrReducedLdData = fl2fxconstDBLf(-1.0)
					}

					sfbThrReducedLdData = fMax(minLdThresh, sfbThrReducedLdData)

					qcOutChan.SfbThresholdLdData[sfbGrp+sfb] = sfbThrReducedLdData
				}
			}
		}
	}
}

// adaptThresholdsVBR is the 1:1 port of FDKaacEnc_AdaptThresholdsVBR
// (adj_thr.cpp:2160-2194): the VBR sibling of adaptThresholdsToPe. It computes
// the threshold exponents (calcThreshExp), lowers the minSnr requirements for
// low-energy bands (adaptMinSnr), seeds the avoid-hole flags (initAvoidHoleFlag),
// then applies the VBR threshold reduction (reduceThresholdsVBR).
func adaptThresholdsVBR(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	adjThrStateElement *atsElement, toolsInfo *PsyOutToolsInfo, nChannels int) {

	// scratch: pAhFlag[2][MAX_GROUPED_SFB], pThrExp[2][MAX_GROUPED_SFB]
	pAhFlag := make([][]uint8, 2)
	pThrExp := make([][]int32, 2)
	for i := range pAhFlag {
		pAhFlag[i] = make([]uint8, maxGroupedSfb)
		pThrExp[i] = make([]int32, maxGroupedSfb)
	}

	// thresholds to the power of redExp
	calcThreshExp(pThrExp, qcOutChannel, psyOutChannel, nChannels)

	// lower the minSnr requirements for low energies compared to the average
	// energy in this frame
	adaptMinSnr(qcOutChannel, psyOutChannel, &adjThrStateElement.minSnrAdaptParam, nChannels)

	// init ahFlag (0: no ah necessary, 1: ah possible, 2: ah active)
	initAvoidHoleFlag(qcOutChannel, psyOutChannel, pAhFlag, toolsInfo, nChannels,
		&adjThrStateElement.ahParam)

	if VbrCaptureEnabled {
		captureVbrReduceInputs(qcOutChannel, psyOutChannel, pAhFlag, pThrExp, nChannels,
			adjThrStateElement.vbrQualFactor, adjThrStateElement.chaosMeasureOld)
	}

	// reduce thresholds
	reduceThresholdsVBR(qcOutChannel, psyOutChannel, pAhFlag, pThrExp, nChannels,
		adjThrStateElement.vbrQualFactor, &adjThrStateElement.chaosMeasureOld)
}

// captureVbrReduceInputs snapshots the reduceThresholdsVBR inputs into the
// parity-capture buffer (diagnostic only; gated by VbrCaptureEnabled).
func captureVbrReduceInputs(qcOutChannel []*QcOutChannel, psyOutChannel []*PsyOutChannel,
	ahFlag [][]uint8, thrExp [][]int32, nChannels int, vbrQualFactor, chaosMeasureOld int32) {
	cap := VbrReduceCapture{
		NChannels:          nChannels,
		SfbCnt:             psyOutChannel[0].SfbCnt,
		SfbPerGroup:        psyOutChannel[0].SfbPerGroup,
		MaxSfbPerGroup:     psyOutChannel[0].MaxSfbPerGroup,
		LastWindowSequence: psyOutChannel[0].LastWindowSequence,
		VbrQualFactor:      vbrQualFactor,
		ChaosMeasureOldIn:  chaosMeasureOld,
	}
	const stride = 64 // MAX_GROUPED_SFB
	cap.GroupLen = append([]int(nil), psyOutChannel[0].GroupLen[:]...)
	cap.SfbOffset = append([]int(nil), psyOutChannel[0].SfbOffsets[:psyOutChannel[0].SfbCnt+1]...)
	for ch := 0; ch < nChannels; ch++ {
		cap.SfbWeightedEnergyLdData = append(cap.SfbWeightedEnergyLdData, qcOutChannel[ch].SfbWeightedEnergyLdData[:stride]...)
		cap.SfbThresholdLdData = append(cap.SfbThresholdLdData, qcOutChannel[ch].SfbThresholdLdData[:stride]...)
		cap.SfbMinSnrLdData = append(cap.SfbMinSnrLdData, qcOutChannel[ch].SfbMinSnrLdData[:stride]...)
		cap.SfbFormFactorLdData = append(cap.SfbFormFactorLdData, qcOutChannel[ch].SfbFormFactorLdData[:stride]...)
		cap.SfbEnergy = append(cap.SfbEnergy, qcOutChannel[ch].SfbEnergy[:stride]...)
		cap.SfbEnergyLdData = append(cap.SfbEnergyLdData, qcOutChannel[ch].SfbEnergyLdData[:stride]...)
		thrRow := make([]int32, stride)
		copy(thrRow, thrExp[ch])
		cap.ThrExp = append(cap.ThrExp, thrRow...)
		ahRow := make([]uint8, stride)
		copy(ahRow, ahFlag[ch])
		cap.AhFlagIn = append(cap.AhFlagIn, ahRow...)
	}
	VbrCaptures = append(VbrCaptures, cap)
}
