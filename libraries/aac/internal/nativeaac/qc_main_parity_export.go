// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file exports the unexported qc_main.cpp rate-control driver helpers to
// the sibling parity_tests/enc-frame package so they can be asserted bit-for-bit
// against the genuine vendored fdk reference. It carries the aacfdk fence like
// the rest of the port; the exported wrappers are thin and add no logic.

package nativeaac

// CalcFrameLenForParity exposes calcFrameLen (FDKaacEnc_calcFrameLen).
func CalcFrameLenForParity(bitRate, sampleRate, granuleLength, mode int) int {
	return calcFrameLen(bitRate, sampleRate, granuleLength, mode)
}

// FramePaddingForParity exposes framePadding (FDKaacEnc_framePadding); returns
// (paddingOn, newPaddingRest).
func FramePaddingForParity(bitRate, sampleRate, granuleLength, paddingRest int) (int, int) {
	on := framePadding(bitRate, sampleRate, granuleLength, &paddingRest)
	return on, paddingRest
}

// FrameLenBytesInt / FrameLenBytesModulo expose the FRAME_LEN_RESULT_MODE enum.
const (
	FrameLenBytesInt    = frameLenBytesInt
	FrameLenBytesModulo = frameLenBytesModulo
)

// AdjustBitrateForParity exposes AdjustBitrate (FDKaacEnc_AdjustBitrate);
// returns (avgTotalBits, newPaddingRest).
func AdjustBitrateForParity(paddingRest, bitRate, sampleRate, granuleLength int) (int, int) {
	hQC := &QcState{Padding: Padding{PaddingRest: paddingRest}}
	avg := AdjustBitrate(hQC, bitRate, sampleRate, granuleLength)
	return avg, hQC.Padding.PaddingRest
}

// CalcMaxValueInSfbForParity exposes calcMaxValueInSfb
// (FDKaacEnc_calcMaxValueInSfb).
func CalcMaxValueInSfbForParity(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int, quantSpectrum []int16, maxValue []uint) int {
	return calcMaxValueInSfb(sfbCnt, maxSfbPerGroup, sfbPerGroup, sfbOffset, quantSpectrum, maxValue)
}

// BitResRedistributionForParity drives BitResRedistribution over nElements SCE
// elements with flat inputs, returning (err, bitResLevelEl[], maxBitResBitsEl[])
// to match the cgo oracle shim.
func BitResRedistributionForParity(nElements int, relativeBits []int32, bitResTot, bitResTotMax, maxBitsPerFrame, avgTotalBits int) (int, []int, []int) {
	hQC := &QcState{BitResTot: bitResTot, BitResTotMax: bitResTotMax, MaxBitsPerFrame: maxBitsPerFrame}
	cm := &ChannelMapping{NElements: nElements}
	for i := 0; i < nElements; i++ {
		cm.ElInfo[i].ElType = idSCE
		hQC.ElementBits[i] = &ElementBits{RelativeBitsEl: relativeBits[i]}
	}
	err := BitResRedistribution(hQC, cm, avgTotalBits)
	lvl := make([]int, nElements)
	maxb := make([]int, nElements)
	for i := 0; i < nElements; i++ {
		lvl[i] = hQC.ElementBits[i].BitResLevelEl
		maxb[i] = hQC.ElementBits[i].MaxBitResBitsEl
	}
	return int(err), lvl, maxb
}

// DistributeElementDynBitsForParity drives distributeElementDynBits +
// updateUsedDynBits over nElements SCE elements, returning
// (err, grantedDynBits[], sumDynBits) to match the cgo oracle shim.
func DistributeElementDynBitsForParity(nElements int, relativeBits []int32, codeBits int, dynBitsUsed []int) (int, []int, int) {
	hQC := &QcState{}
	cm := &ChannelMapping{NElements: nElements}
	var elp [8]*QcOutElement
	for i := 0; i < nElements; i++ {
		cm.ElInfo[i].ElType = idSCE
		hQC.ElementBits[i] = &ElementBits{RelativeBitsEl: relativeBits[i]}
		elp[i] = &QcOutElement{DynBitsUsed: dynBitsUsed[i]}
	}
	err := distributeElementDynBits(hQC, elp, cm, codeBits)
	var sum int
	updateUsedDynBits(&sum, elp, cm)
	granted := make([]int, nElements)
	for i := 0; i < nElements; i++ {
		granted[i] = elp[i].GrantedDynBits
	}
	return int(err), granted, sum
}

// TotalConsumedBitsForParity drives getTotalConsumedBits over a single sub
// frame of nElements SCE elements.
func TotalConsumedBitsForParity(nElements int, dynBitsUsed, staticBitsUsed, extBitsUsed []int, globalExtBits, globHdrBits int) int {
	cm := &ChannelMapping{NElements: nElements}
	qo := &QcOut{GlobalExtBits: globalExtBits}
	var grid [][8]*QcOutElement = make([][8]*QcOutElement, 1)
	for i := 0; i < nElements; i++ {
		cm.ElInfo[i].ElType = idSCE
		grid[0][i] = &QcOutElement{
			DynBitsUsed:    dynBitsUsed[i],
			StaticBitsUsed: staticBitsUsed[i],
			ExtBitsUsed:    extBitsUsed[i],
		}
	}
	return getTotalConsumedBits([]*QcOut{qo}, grid, cm, globHdrBits, 1)
}

// UpdateFillBitsForParity drives updateFillBits over qcOut[0], returning
// (totFillBits, totalBits).
func UpdateFillBitsForParity(bitrateMode, minBitsPerFrame, bitResTot, bitResTotMax, grantedDynBits, usedDynBits, staticBits, elementExtBits, globalExtBits int) (int, int) {
	hQC := &QcState{
		BitrateMode:     QcdataBrMode(bitrateMode),
		MinBitsPerFrame: minBitsPerFrame,
		BitResTot:       bitResTot,
		BitResTotMax:    bitResTotMax,
	}
	qo := &QcOut{
		GrantedDynBits: grantedDynBits,
		UsedDynBits:    usedDynBits,
		StaticBits:     staticBits,
		ElementExtBits: elementExtBits,
		GlobalExtBits:  globalExtBits,
	}
	updateFillBits(nil, hQC, hQC.ElementBits, []*QcOut{qo})
	return qo.TotFillBits, qo.TotalBits
}

// UpdateBitresForParity drives updateBitres over qcOut[0], returning the new
// bitResTot.
func UpdateBitresForParity(bitrateMode, bitResTot, maxBitsPerFrame, bitResTotMax, grantedDynBits, usedDynBits, totFillBits, alignBits int) int {
	hQC := &QcState{
		BitrateMode:     QcdataBrMode(bitrateMode),
		BitResTot:       bitResTot,
		MaxBitsPerFrame: maxBitsPerFrame,
		BitResTotMax:    bitResTotMax,
	}
	qo := &QcOut{
		GrantedDynBits: grantedDynBits,
		UsedDynBits:    usedDynBits,
		TotFillBits:    totFillBits,
		AlignBits:      alignBits,
	}
	updateBitres(nil, hQC, []*QcOut{qo})
	return hQC.BitResTot
}
