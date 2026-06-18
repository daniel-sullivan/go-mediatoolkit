// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only exports for the threshold-adjustment DRIVER ports preparePe /
// calcWeighting / calcPe (adj_thr_pe.go) and initAvoidHoleFlag /
// reduceThresholdsCBR / calcChaosMeasure (adj_thr_hole.go). These thin wrappers
// build the minimal QcOutChannel / PsyOutChannel / atsElement / PsyOutToolsInfo
// state from flat int32 inputs and run the unexported Go ports, so the cgo
// parity slice under internal/parity_tests/enc-adj-thr/ can compare bit-for-bit
// against the genuine vendored FDK statics (reached via a TU that #includes
// adj_thr.cpp). Not part of the production API. One-group long-block layout
// (sfbPerGroup == sfbCnt) is used; per-sfb indexing is absolute (sfbGrp+sfb).

// ParityPreparePe runs preparePe for a single channel and returns the resulting
// sfbNLines (length maxGroupedSFB) and the stamped peData.offset.
func ParityPreparePe(sfbEnergyLdData, sfbThresholdLdData, sfbFormFactorLdData []int32,
	sfbOffset []int, sfbCnt, sfbPerGroup, maxSfbPerGroup, peOffset int) (sfbNLines []int32, offset int32) {
	qc := new(QcOutChannel)
	psy := new(PsyOutChannel)
	copy(qc.SfbEnergyLdData[:], sfbEnergyLdData)
	copy(qc.SfbThresholdLdData[:], sfbThresholdLdData)
	copy(qc.SfbFormFactorLdData[:], sfbFormFactorLdData)
	psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
	copy(psy.SfbOffsets[:], sfbOffset)

	var pd peData
	preparePe(&pd, []*PsyOutChannel{psy}, []*QcOutChannel{qc}, 1, peOffset)
	out := make([]int32, maxGroupedSFB)
	copy(out, pd.peChannelData[0].sfbNLines[:])
	return out, pd.offset
}

// ParityCalcWeighting runs calcWeighting for nChannels (1 or 2). Inputs are flat
// per-channel arrays of length maxGroupedSFB (channel ch at offset ch*maxGroupedSFB),
// plus the seeded chaosMeasureEnFac / lastEnFacPatch state and msMask. Returns the
// resulting sfbEnFacLd (per channel), the updated chaosMeasureEnFac and
// lastEnFacPatch.
func ParityCalcWeighting(nChannels int, sfbEnergyLdData, sfbEnergy []int32,
	sfbNLines []int32, sfbOffset []int, lastWindowSequence []int, msMask []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	chaosMeasureEnFacIn [2]int32, lastEnFacPatchIn [2]int) (
	sfbEnFacLd []int32, chaosMeasureEnFacOut [2]int32, lastEnFacPatchOut [2]int) {

	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	var pd peData
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbEnergyLdData[:], sfbEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		copy(psy.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		psy.LastWindowSequence = lastWindowSequence[ch]
		copy(psy.SfbOffsets[:], sfbOffset)
		copy(pd.peChannelData[ch].sfbNLines[:], sfbNLines[base:base+maxGroupedSFB])
		qcs[ch], psys[ch] = qc, psy
	}
	var tools PsyOutToolsInfo
	for i := 0; i < maxGroupedSFB; i++ {
		tools.MsMask[i] = int(msMask[i])
	}
	ats := new(atsElement)
	ats.chaosMeasureEnFac = chaosMeasureEnFacIn
	ats.lastEnFacPatch = lastEnFacPatchIn

	calcWeighting(&pd, psys, qcs, &tools, ats, nChannels, 1)

	sfbEnFacLd = make([]int32, nChannels*maxGroupedSFB)
	for ch := 0; ch < nChannels; ch++ {
		copy(sfbEnFacLd[ch*maxGroupedSFB:], qcs[ch].SfbEnFacLd[:])
	}
	return sfbEnFacLd, ats.chaosMeasureEnFac, ats.lastEnFacPatch
}

// ParityCalcPe runs calcPe for nChannels. Inputs are flat per-channel arrays
// (length maxGroupedSFB, channel ch at ch*maxGroupedSFB). Returns the element pe,
// constPart, nActiveLines after seeding peData.offset with peOffset.
func ParityCalcPe(nChannels int, sfbWeightedEnergyLdData, sfbThresholdLdData,
	sfbNLines []int32, isBook, isScale []int, sfbCnt, sfbPerGroup, maxSfbPerGroup, peOffset int) (
	pe, constPart, nActiveLines int32) {

	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	var pd peData
	pd.offset = int32(peOffset)
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbWeightedEnergyLdData[:], sfbWeightedEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbThresholdLdData[:], sfbThresholdLdData[base:base+maxGroupedSFB])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		for i := 0; i < maxGroupedSFB; i++ {
			psy.IsBook[i] = isBook[base+i]
			psy.IsScale[i] = isScale[base+i]
		}
		copy(pd.peChannelData[ch].sfbNLines[:], sfbNLines[base:base+maxGroupedSFB])
		qcs[ch], psys[ch] = qc, psy
	}
	calcPe(psys, qcs, &pd, nChannels)
	return pd.pe, pd.constPart, pd.nActiveLines
}

// ParityInitAvoidHoleFlag runs initAvoidHoleFlag for nChannels. Inputs are flat
// per-channel arrays. Returns the resulting ahFlag (nChannels*maxGroupedSFB) and
// the mutated sfbSpreadEnergy / sfbMinSnrLdData per channel.
func ParityInitAvoidHoleFlag(nChannels int,
	sfbSpreadEnergy, sfbEnergy, sfbEnergyLdData, sfbMinSnrLdData []int32,
	sfbOffset []int, lastWindowSequence []int, msMask []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup, modifyMinSnr int) (
	ahFlag []uint8, sfbSpreadEnergyOut, sfbMinSnrLdDataOut []int32) {

	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	ahRows := make([][]uint8, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbSpreadEnergy[:], sfbSpreadEnergy[base:base+maxGroupedSFB])
		copy(qc.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		copy(psy.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		copy(qc.SfbEnergyLdData[:], sfbEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbMinSnrLdData[:], sfbMinSnrLdData[base:base+maxGroupedSFB])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		psy.LastWindowSequence = lastWindowSequence[ch]
		copy(psy.SfbOffsets[:], sfbOffset)
		qcs[ch], psys[ch] = qc, psy
		ahRows[ch] = make([]uint8, maxGroupedSFB)
	}
	var tools PsyOutToolsInfo
	for i := 0; i < maxGroupedSFB; i++ {
		tools.MsMask[i] = int(msMask[i])
	}
	ahp := &ahParam{modifyMinSnr: modifyMinSnr}

	initAvoidHoleFlag(qcs, psys, ahRows, &tools, nChannels, ahp)

	ahFlag = make([]uint8, nChannels*maxGroupedSFB)
	sfbSpreadEnergyOut = make([]int32, nChannels*maxGroupedSFB)
	sfbMinSnrLdDataOut = make([]int32, nChannels*maxGroupedSFB)
	for ch := 0; ch < nChannels; ch++ {
		copy(ahFlag[ch*maxGroupedSFB:], ahRows[ch])
		copy(sfbSpreadEnergyOut[ch*maxGroupedSFB:], qcs[ch].SfbSpreadEnergy[:])
		copy(sfbMinSnrLdDataOut[ch*maxGroupedSFB:], qcs[ch].SfbMinSnrLdData[:])
	}
	return ahFlag, sfbSpreadEnergyOut, sfbMinSnrLdDataOut
}

// ParityReduceThresholdsCBR runs reduceThresholdsCBR for nChannels. Inputs are
// flat per-channel arrays plus the seeded ahFlag and thrExp matrices. Returns the
// reduced sfbThresholdLdData per channel and the mutated ahFlag.
func ParityReduceThresholdsCBR(nChannels int,
	sfbWeightedEnergyLdData, sfbThresholdLdData, sfbMinSnrLdData []int32,
	ahFlagIn []uint8, thrExp []int32, sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	redValM int32, redValE int32) (sfbThresholdLdDataOut []int32, ahFlagOut []uint8) {

	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	ahRows := make([][]uint8, nChannels)
	thrRows := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbWeightedEnergyLdData[:], sfbWeightedEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbThresholdLdData[:], sfbThresholdLdData[base:base+maxGroupedSFB])
		copy(qc.SfbMinSnrLdData[:], sfbMinSnrLdData[base:base+maxGroupedSFB])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		qcs[ch], psys[ch] = qc, psy
		ahRows[ch] = append([]uint8(nil), ahFlagIn[base:base+maxGroupedSFB]...)
		thrRows[ch] = append([]int32(nil), thrExp[base:base+maxGroupedSFB]...)
	}

	reduceThresholdsCBR(qcs, psys, ahRows, thrRows, nChannels, redValM, redValE)

	sfbThresholdLdDataOut = make([]int32, nChannels*maxGroupedSFB)
	ahFlagOut = make([]uint8, nChannels*maxGroupedSFB)
	for ch := 0; ch < nChannels; ch++ {
		copy(sfbThresholdLdDataOut[ch*maxGroupedSFB:], qcs[ch].SfbThresholdLdData[:])
		copy(ahFlagOut[ch*maxGroupedSFB:], ahRows[ch])
	}
	return sfbThresholdLdDataOut, ahFlagOut
}

// VbrReduceCapture records the exact inputs reduceThresholdsVBR receives on each
// invocation of adaptThresholdsVBR, so a parity test can replay frame N through
// the genuine fdk reduceThresholdsVBR and localise an e2e divergence. Capture is
// off unless VbrCaptureEnabled is set.
type VbrReduceCapture struct {
	NChannels                                                    int
	SfbCnt, SfbPerGroup, MaxSfbPerGroup, LastWindowSequence      int
	GroupLen, SfbOffset                                          []int
	VbrQualFactor, ChaosMeasureOldIn                             int32
	SfbWeightedEnergyLdData, SfbThresholdLdData, SfbMinSnrLdData []int32
	SfbFormFactorLdData, SfbEnergy, SfbEnergyLdData, ThrExp      []int32
	AhFlagIn                                                     []uint8
}

var (
	// VbrCaptureEnabled turns on per-frame capture in adaptThresholdsVBR.
	VbrCaptureEnabled bool
	// VbrCaptures accumulates one entry per adaptThresholdsVBR call when enabled.
	VbrCaptures []VbrReduceCapture
)

// ParityReduceThresholdsVBR runs reduceThresholdsVBR over the flat per-channel
// arrays plus the seeded ahFlag/thrExp matrices, lastWindowSequence, groupLen,
// sfbOffsets and the in/out chaosMeasureOld. Returns the reduced thresholds, the
// mutated ahFlag, and the updated chaosMeasureOld.
func ParityReduceThresholdsVBR(nChannels, stride int,
	sfbWeightedEnergyLdData, sfbThresholdLdData, sfbMinSnrLdData, sfbFormFactorLdData,
	sfbEnergy, sfbEnergyLdData []int32, ahFlagIn []uint8, thrExp []int32, sfbOffset []int,
	sfbCnt, sfbPerGroup, maxSfbPerGroup, lastWindowSequence int, groupLen []int,
	vbrQualFactor, chaosMeasureOld int32) (
	sfbThresholdLdDataOut []int32, ahFlagOut []uint8, chaosMeasureOldOut int32) {

	qcs := make([]*QcOutChannel, nChannels)
	psys := make([]*PsyOutChannel, nChannels)
	ahRows := make([][]uint8, nChannels)
	thrRows := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * stride
		copy(qc.SfbWeightedEnergyLdData[:], sfbWeightedEnergyLdData[base:base+stride])
		copy(qc.SfbThresholdLdData[:], sfbThresholdLdData[base:base+stride])
		copy(qc.SfbMinSnrLdData[:], sfbMinSnrLdData[base:base+stride])
		copy(qc.SfbFormFactorLdData[:], sfbFormFactorLdData[base:base+stride])
		copy(qc.SfbEnergy[:], sfbEnergy[base:base+stride])
		copy(qc.SfbEnergyLdData[:], sfbEnergyLdData[base:base+stride])
		// reduceThresholdsVBR reads psy.SfbEnergy (the aliased copy); calcChaosMeasure
		// reads qc.SfbEnergy / qc.SfbEnergyLdData. Mirror the live aliasing by
		// populating both psy and qc.
		copy(psy.SfbEnergy[:], sfbEnergy[base:base+stride])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		psy.LastWindowSequence = lastWindowSequence
		copy(psy.SfbOffsets[:], sfbOffset)
		for g := 0; g < len(groupLen) && g < len(psy.GroupLen); g++ {
			psy.GroupLen[g] = groupLen[g]
		}
		qcs[ch], psys[ch] = qc, psy
		ahRows[ch] = append([]uint8(nil), ahFlagIn[base:base+stride]...)
		thrRows[ch] = append([]int32(nil), thrExp[base:base+stride]...)
	}

	cmo := chaosMeasureOld
	reduceThresholdsVBR(qcs, psys, ahRows, thrRows, nChannels, vbrQualFactor, &cmo)

	sfbThresholdLdDataOut = make([]int32, nChannels*stride)
	ahFlagOut = make([]uint8, nChannels*stride)
	for ch := 0; ch < nChannels; ch++ {
		copy(sfbThresholdLdDataOut[ch*stride:(ch+1)*stride], qcs[ch].SfbThresholdLdData[:stride])
		copy(ahFlagOut[ch*stride:(ch+1)*stride], ahRows[ch])
	}
	return sfbThresholdLdDataOut, ahFlagOut, cmo
}

// ParityCalcChaosMeasure runs calcChaosMeasure for a single channel over flat
// inputs and returns the chaos measure.
func ParityCalcChaosMeasure(sfbEnergyLdData, sfbThresholdLdData, sfbEnergy,
	sfbFormFactorLdData []int32, sfbOffset []int, sfbCnt, sfbPerGroup, maxSfbPerGroup int) int32 {
	qc := new(QcOutChannel)
	psy := new(PsyOutChannel)
	copy(qc.SfbEnergyLdData[:], sfbEnergyLdData)
	copy(qc.SfbThresholdLdData[:], sfbThresholdLdData)
	copy(qc.SfbEnergy[:], sfbEnergy)
	psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
	copy(psy.SfbOffsets[:], sfbOffset)
	return calcChaosMeasure(psy, qc, sfbFormFactorLdData)
}
