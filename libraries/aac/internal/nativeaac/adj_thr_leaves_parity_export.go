// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only exports for the threshold-adjustment leaf kernels (adj_thr.cpp).
// These thin wrappers let the cgo parity slice under
// internal/parity_tests/enc-adj-thr-leaves/ drive the unexported
// calcThreshExp / adaptMinSnr / resetAHFlags / calcPeNoAH / calcBitSave /
// calcBitSpend / adjustPeMinMax against the genuine vendored statics and compare
// bit-for-bit. The channel-array helpers rebuild the QC_OUT_CHANNEL /
// PSY_OUT_CHANNEL view from flat per-channel inputs, mirroring the C oracle's
// struct seeding. Not part of the production API.

// ParityCalcThreshExp runs calcThreshExp for nChannels (each with its own
// sfbThresholdLdData and sfbCnt/sfbPerGroup/maxSfbPerGroup) and returns the
// per-channel thrExp rows (length MaxGroupedSFB each).
func ParityCalcThreshExp(sfbThresholdLdData [][]int32, sfbCnt, sfbPerGroup, maxSfbPerGroup []int,
	nChannels int) [][]int32 {
	qc := make([]*QcOutChannel, nChannels)
	psy := make([]*PsyOutChannel, nChannels)
	thrExp := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc[ch] = new(QcOutChannel)
		copy(qc[ch].SfbThresholdLdData[:], sfbThresholdLdData[ch])
		psy[ch] = &PsyOutChannel{SfbCnt: sfbCnt[ch], SfbPerGroup: sfbPerGroup[ch], MaxSfbPerGroup: maxSfbPerGroup[ch]}
		thrExp[ch] = make([]int32, MaxGroupedSFB)
	}
	calcThreshExp(thrExp, qc, psy, nChannels)
	return thrExp
}

// ParityAdaptMinSnr runs adaptMinSnr over per-channel sfbEnergy/sfbEnergyLdData/
// sfbMinSnrLdData and returns the updated per-channel sfbMinSnrLdData
// (length MaxGroupedSFB each).
func ParityAdaptMinSnr(sfbEnergy, sfbEnergyLdData, sfbMinSnrLdData [][]int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup []int,
	maxRed, startRatio, redRatioFac, redOffs int32, nChannels int) [][]int32 {
	qc := make([]*QcOutChannel, nChannels)
	psy := make([]*PsyOutChannel, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc[ch] = new(QcOutChannel)
		copy(qc[ch].SfbEnergyLdData[:], sfbEnergyLdData[ch])
		copy(qc[ch].SfbMinSnrLdData[:], sfbMinSnrLdData[ch])
		psy[ch] = &PsyOutChannel{SfbCnt: sfbCnt[ch], SfbPerGroup: sfbPerGroup[ch], MaxSfbPerGroup: maxSfbPerGroup[ch]}
		copy(psy[ch].SfbEnergy[:], sfbEnergy[ch])
	}
	msa := &minSnrAdaptParam{maxRed: maxRed, startRatio: startRatio, redRatioFac: redRatioFac, redOffs: redOffs}
	adaptMinSnr(qc, psy, msa, nChannels)
	out := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		out[ch] = append([]int32(nil), qc[ch].SfbMinSnrLdData[:]...)
	}
	return out
}

// ParityResetAHFlags runs resetAHFlags over per-channel ahFlag rows and returns
// the updated rows (length MaxGroupedSFB each).
func ParityResetAHFlags(ahFlag [][]uint8, sfbCnt, sfbPerGroup, maxSfbPerGroup []int,
	nChannels int) [][]uint8 {
	psy := make([]*PsyOutChannel, nChannels)
	flags := make([][]uint8, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		psy[ch] = &PsyOutChannel{SfbCnt: sfbCnt[ch], SfbPerGroup: sfbPerGroup[ch], MaxSfbPerGroup: maxSfbPerGroup[ch]}
		flags[ch] = append([]uint8(nil), ahFlag[ch]...)
	}
	resetAHFlags(flags, nChannels, psy)
	return flags
}

// ParityCalcPeNoAH runs calcPeNoAH over per-channel peChannelData
// (sfbPe/sfbConstPart/sfbNActiveLines) + ahFlag and returns (pe, constPart,
// nActiveLines).
func ParityCalcPeNoAH(offset int32, sfbPe, sfbConstPart, sfbNActiveLines [][]int32,
	ahFlag [][]uint8, sfbCnt, sfbPerGroup, maxSfbPerGroup []int, nChannels int) (int, int, int) {
	var pd peData
	pd.offset = offset
	psy := make([]*PsyOutChannel, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		copy(pd.peChannelData[ch].sfbPe[:], sfbPe[ch])
		copy(pd.peChannelData[ch].sfbConstPart[:], sfbConstPart[ch])
		copy(pd.peChannelData[ch].sfbNActiveLines[:], sfbNActiveLines[ch])
		psy[ch] = &PsyOutChannel{SfbCnt: sfbCnt[ch], SfbPerGroup: sfbPerGroup[ch], MaxSfbPerGroup: maxSfbPerGroup[ch]}
	}
	return calcPeNoAH(&pd, ahFlag, psy, nChannels)
}

// ParityCalcBitSave wraps calcBitSave.
func ParityCalcBitSave(fillLevel, clipLow, clipHigh, minBitSave, maxBitSave, bitsaveSlope int32) int32 {
	return calcBitSave(fillLevel, clipLow, clipHigh, minBitSave, maxBitSave, bitsaveSlope)
}

// ParityCalcBitSpend wraps calcBitSpend.
func ParityCalcBitSpend(fillLevel, clipLow, clipHigh, minBitSpend, maxBitSpend, bitspendSlope int32) int32 {
	return calcBitSpend(fillLevel, clipLow, clipHigh, minBitSpend, maxBitSpend, bitspendSlope)
}

// ParityAdjustPeMinMax wraps adjustPeMinMax, returning the updated (peMin, peMax).
func ParityAdjustPeMinMax(currPe, peMin, peMax int) (int, int) {
	adjustPeMinMax(currPe, &peMin, &peMax)
	return peMin, peMax
}
