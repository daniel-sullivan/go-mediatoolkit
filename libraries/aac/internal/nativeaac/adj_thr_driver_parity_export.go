// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Parity-only export for the threshold-adjustment top entry adjustThresholds
// (adj_thr_state.go) and the reduction heart it drives (adaptThresholdsToPe /
// correctThresh / reduceMinSnr / allowMoreHoles / calcThreshExp / adaptMinSnr /
// initAvoidHoleFlag / reduceThresholdsCBR / calcPe / calcPeNoAH / resetAHFlags).
// The wrapper rebuilds the multi-element CHANNEL_MAPPING / QC_OUT_ELEMENT /
// PSY_OUT_ELEMENT / QC_OUT / ADJ_THR_STATE view from flat per-channel int32 inputs
// for a single-element CBR AAC-LC frame (SCE or CPE, INTRA bit-distribution), runs
// the unexported Go port, and returns the mutated sfbThresholdLdData per channel —
// so the cgo parity slice under internal/parity_tests/enc-adj-thr/ can compare it
// bit-for-bit against the genuine vendored FDKaacEnc_AdjustThresholds. Not part of
// the production API. One-group long-block layout (sfbPerGroup == sfbCnt).
//
// AdjThrParams bundles the per-element ADJ_THR_STATE / ATS_ELEMENT parameters the
// driver reads (mirrors what FDKaacEnc_AdjThrInit would have stamped). Seeding them
// directly keeps the oracle and port in lock-step without re-porting AdjThrInit
// into the comparison path.

// ParityFL2FXConstDBLf narrows a single-precision literal through float32 before
// the FL2FXCONST_DBL *2^31 scaling (the `f` suffix), so the parity test can build
// the AdjThrInit minSnr-adaptation constants bit-identically to the C compiler.
func ParityFL2FXConstDBLf(v float32) int32 { return fl2fxconstDBLf(v) }

// ParityFL2FXConstDBL is the double-precision FL2FXCONST_DBL (no `f` suffix).
func ParityFL2FXConstDBL(v float64) int32 { return fl2fxconstDBL(v) }

// AdjThrParams is the flat parameter bundle for ParityAdjustThresholds.
type AdjThrParams struct {
	PeOffset                    int
	ModifyMinSnr                int
	StartSfbL, StartSfbS        int
	MaxRed, StartRatio          int32
	RedRatioFac, RedOffs        int32
	MaxIter2ndGuess             int
	GrantedPeCorr               int
	Pe, ConstPart, NActiveLines int32
}

// ParityAdjustThresholds runs adjustThresholds (CBR / INTRA / one element) over a
// single SCE/CPE element built from flat per-channel inputs, and returns the
// resulting sfbThresholdLdData per channel (nChannels*maxGroupedSFB). All
// ld-domain per-sfb arrays plus the seeded peData (pe/constPart/nActiveLines and
// the per-sfb sfbPe/sfbConstPart/sfbNActiveLines/sfbNLines) and the bresParams are
// supplied so the port and the genuine oracle operate on identical state.
func ParityAdjustThresholds(nChannels int, elType int,
	sfbEnergy, sfbEnergyLdData, sfbThresholdLdData, sfbWeightedEnergyLdData,
	sfbSpreadEnergy, sfbMinSnrLdData, sfbFormFactorLdData, sfbEnFacLd []int32,
	sfbPe, sfbConstPart, sfbNActiveLines, sfbNLines []int32,
	sfbOffset []int, lastWindowSequence []int, msMask []int32,
	sfbCnt, sfbPerGroup, maxSfbPerGroup int,
	p AdjThrParams) (sfbThresholdLdDataOut []int32) {

	// Build channel mapping: one element (index 0) with nChannels.
	cm := new(ChannelMapping)
	cm.NElements = 1
	cm.NChannels = nChannels
	cm.ElInfo[0].ElType = elType
	cm.ElInfo[0].NChannelsInEl = nChannels
	cm.ElInfo[0].ChannelIndex[0] = 0
	if nChannels == 2 {
		cm.ElInfo[0].ChannelIndex[1] = 1
	}

	qcEl := new(QcOutElement)
	psyEl := new(PsyOutElement)
	qcEl.GrantedPeCorr = p.GrantedPeCorr
	qcEl.PeData.pe = p.Pe
	qcEl.PeData.constPart = p.ConstPart
	qcEl.PeData.nActiveLines = p.NActiveLines

	for ch := 0; ch < nChannels; ch++ {
		qc := new(QcOutChannel)
		psy := new(PsyOutChannel)
		base := ch * maxGroupedSFB
		copy(qc.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		copy(psy.SfbEnergy[:], sfbEnergy[base:base+maxGroupedSFB])
		copy(qc.SfbEnergyLdData[:], sfbEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbThresholdLdData[:], sfbThresholdLdData[base:base+maxGroupedSFB])
		copy(qc.SfbWeightedEnergyLdData[:], sfbWeightedEnergyLdData[base:base+maxGroupedSFB])
		copy(qc.SfbSpreadEnergy[:], sfbSpreadEnergy[base:base+maxGroupedSFB])
		copy(qc.SfbMinSnrLdData[:], sfbMinSnrLdData[base:base+maxGroupedSFB])
		copy(qc.SfbFormFactorLdData[:], sfbFormFactorLdData[base:base+maxGroupedSFB])
		copy(qc.SfbEnFacLd[:], sfbEnFacLd[base:base+maxGroupedSFB])
		psy.SfbCnt, psy.SfbPerGroup, psy.MaxSfbPerGroup = sfbCnt, sfbPerGroup, maxSfbPerGroup
		psy.LastWindowSequence = lastWindowSequence[ch]
		copy(psy.SfbOffsets[:], sfbOffset)

		copy(qcEl.PeData.peChannelData[ch].sfbPe[:], sfbPe[base:base+maxGroupedSFB])
		copy(qcEl.PeData.peChannelData[ch].sfbConstPart[:], sfbConstPart[base:base+maxGroupedSFB])
		copy(qcEl.PeData.peChannelData[ch].sfbNActiveLines[:], sfbNActiveLines[base:base+maxGroupedSFB])
		copy(qcEl.PeData.peChannelData[ch].sfbNLines[:], sfbNLines[base:base+maxGroupedSFB])

		qcEl.QcOutChannel[ch] = qc
		psyEl.PsyOutChannel[ch] = psy
	}
	for i := 0; i < maxGroupedSFB; i++ {
		psyEl.ToolsInfo.MsMask[i] = int(msMask[i])
	}

	qcElement := make([]*QcOutElement, 8)
	psyOutElement := make([]*PsyOutElement, 8)
	qcElement[0] = qcEl
	psyOutElement[0] = psyEl

	qcOut := new(QcOut)

	// Seed the ADJ_THR_STATE element params (what AdjThrInit would have stamped).
	st := new(adjThrState)
	st.bitDistributionMode = aacencBdModeIntraElement
	st.maxIter2ndGuess = p.MaxIter2ndGuess
	st.adjThrStateElem[0] = new(atsElement)
	ats := st.adjThrStateElem[0]
	ats.peOffset = p.PeOffset
	ats.ahParam.modifyMinSnr = p.ModifyMinSnr
	ats.ahParam.startSfbL = p.StartSfbL
	ats.ahParam.startSfbS = p.StartSfbS
	ats.minSnrAdaptParam.maxRed = p.MaxRed
	ats.minSnrAdaptParam.startRatio = p.StartRatio
	ats.minSnrAdaptParam.redRatioFac = p.RedRatioFac
	ats.minSnrAdaptParam.redOffs = p.RedOffs

	adjustThresholds(st, qcElement, qcOut, psyOutElement, 1 /* CBR */, cm)

	sfbThresholdLdDataOut = make([]int32, nChannels*maxGroupedSFB)
	for ch := 0; ch < nChannels; ch++ {
		copy(sfbThresholdLdDataOut[ch*maxGroupedSFB:], qcEl.QcOutChannel[ch].SfbThresholdLdData[:])
	}
	return sfbThresholdLdDataOut
}
