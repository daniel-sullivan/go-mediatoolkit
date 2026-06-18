// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the self-contained integer rate-control
// driver helpers of the FDK-AAC quantizer/coder, translated from
// libAACenc/src/qc_main.cpp. These are the bit-distribution / bit-reservoir /
// fill-bit primitives the top-level FDKaacEnc_QCMain and FDKaacEnc_EncodeFrame
// orchestration calls. Every function is pure integer arithmetic (truncating
// divisions, fMax/fMin clamps, fMultI fixed-point fractional multiply), so it
// is bit-identical regardless of vectorization and needs no aac_strict FP
// split — the parity gate asserts exact int32 equality against the genuine
// vendored fdk symbols.
//
// The translation is faithful: control flow, the &~7 / %8 alignment masks, the
// truncating integer divisions and the saturating clamps match the C exactly —
// do not "improve" the algorithm.
//
// NOTE (scope): the full FDKaacEnc_QCMain quantization loop
// (qc_main.cpp:788) and FDKaacEnc_EncodeFrame (aacenc.cpp:769) also reach into
// FDKaacEnc_AdjustThresholds / FDKaacEnc_EstimateScaleFactors /
// FDKaacEnc_QuantizeSpectrum / FDKaacEnc_dynBitCount (ported elsewhere) plus
// crashRecovery and the encoder config/init state machine; the top-level loop
// assembly is not in this file. This file ports the leaf rate-control helpers
// that loop calls, each independently parity-checkable against the vendored C.

package nativeaac

const frameLenBytesModulo = 1 // FRAME_LEN_BYTES_MODULO (qc_main.cpp:144)
const frameLenBytesInt = 2    // FRAME_LEN_BYTES_INT    (qc_main.cpp:145)

// calcFrameLen mirrors FDKaacEnc_calcFrameLen() (qc_main.cpp:179): compute the
// per-frame byte payload (or its modulo remainder) for a bitrate. The C uses
// truncating integer division/modulo on the (granuleLength>>3)*bitRate product.
func calcFrameLen(bitRate, sampleRate, granuleLength, mode int) int {
	result := (granuleLength >> 3) * bitRate

	switch mode {
	case frameLenBytesModulo:
		result %= sampleRate
	case frameLenBytesInt:
		result /= sampleRate
	}
	return result
}

// framePadding mirrors FDKaacEnc_framePadding() (qc_main.cpp:206): decide
// whether the current frame needs an extra padding byte, maintaining the
// paddingRest accumulator across frames (initialised to sampleRate). Returns 1
// when padding is on, 0 otherwise, and updates *paddingRest in place.
func framePadding(bitRate, sampleRate, granuleLength int, paddingRest *int) int {
	paddingOn := 0

	difference := calcFrameLen(bitRate, sampleRate, granuleLength, frameLenBytesModulo)
	*paddingRest -= difference

	if *paddingRest <= 0 {
		paddingOn = 1
		*paddingRest += sampleRate
	}

	return paddingOn
}

// AdjustBitrate mirrors FDKaacEnc_AdjustBitrate() (qc_main.cpp:469): adjust the
// frame length via padding on a frame-to-frame basis so a non-byte-aligned
// target bitrate is realised on average. Returns the average total bits for the
// frame; the (CBR) padding state is carried in hQC.Padding.PaddingRest.
func AdjustBitrate(hQC *QcState, bitRate, sampleRate, granuleLength int) int {
	paddingOn := framePadding(bitRate, sampleRate, granuleLength, &hQC.Padding.PaddingRest)

	frameLen := paddingOn + calcFrameLen(bitRate, sampleRate, granuleLength, frameLenBytesInt)

	return frameLen << 3
}

// isAudioElement mirrors isAudioElement(elType) (qc_main.cpp:491): true for
// SCE/CPE/LFE.
func isAudioElement(elType int) bool {
	return elType == idSCE || elType == idCPE || elType == idLFE
}

// BitResRedistribution mirrors FDKaacEnc_BitResRedistribution()
// (qc_main.cpp:738): split the total bitreservoir across the audio elements in
// proportion to their relativeBitsEl, with the leftover (from fMultI rounding)
// folded back element by element so the per-element levels sum exactly to the
// reservoir. Returns AacEncBitresTooLow / ...TooHigh on a violated fill level.
func BitResRedistribution(hQC *QcState, cm *ChannelMapping, avgTotalBits int) EncoderError {
	if hQC.BitResTot < 0 {
		return AacEncBitresTooLow
	}
	if hQC.BitResTot > hQC.BitResTotMax {
		return AacEncBitresTooHigh
	}

	totalBits, totalBitsMax := 0, 0

	totalBitreservoir := fixMin(hQC.BitResTot, hQC.MaxBitsPerFrame-avgTotalBits)
	totalBitreservoirMax := fixMin(hQC.BitResTotMax, hQC.MaxBitsPerFrame-avgTotalBits)

	for i := cm.NElements - 1; i >= 0; i-- {
		if isAudioElement(cm.ElInfo[i].ElType) {
			hQC.ElementBits[i].BitResLevelEl =
				int(fMultI(hQC.ElementBits[i].RelativeBitsEl, int32(totalBitreservoir)))
			totalBits += hQC.ElementBits[i].BitResLevelEl

			hQC.ElementBits[i].MaxBitResBitsEl =
				int(fMultI(hQC.ElementBits[i].RelativeBitsEl, int32(totalBitreservoirMax)))
			totalBitsMax += hQC.ElementBits[i].MaxBitResBitsEl
		}
	}
	for i := 0; i < cm.NElements; i++ {
		if isAudioElement(cm.ElInfo[i].ElType) {
			deltaBits := fixMax(totalBitreservoir-totalBits, -hQC.ElementBits[i].BitResLevelEl)
			hQC.ElementBits[i].BitResLevelEl += deltaBits
			totalBits += deltaBits

			deltaBits = fixMax(totalBitreservoirMax-totalBitsMax, -hQC.ElementBits[i].MaxBitResBitsEl)
			hQC.ElementBits[i].MaxBitResBitsEl += deltaBits
			totalBitsMax += deltaBits
		}
	}

	return AacEncOK
}

// distributeElementDynBits mirrors FDKaacEnc_distributeElementDynBits()
// (qc_main.cpp:504): distribute codeBits over the audio elements in proportion
// to relativeBitsEl, then correct the fMultI rounding difference by adding a
// positive remainder to the element with the fewest bits or subtracting a
// negative remainder from the element with the most.
func distributeElementDynBits(hQC *QcState, qcElement [8]*QcOutElement, cm *ChannelMapping, codeBits int) EncoderError {
	totalBits := 0

	for i := cm.NElements - 1; i >= 0; i-- {
		if isAudioElement(cm.ElInfo[i].ElType) {
			qcElement[i].GrantedDynBits =
				fixMax(0, int(fMultI(hQC.ElementBits[i].RelativeBitsEl, int32(codeBits))))
			totalBits += qcElement[i].GrantedDynBits
		}
	}

	if codeBits != totalBits {
		elMaxBits := cm.NElements - 1
		elMinBits := cm.NElements - 1

		for i := cm.NElements - 1; i >= 0; i-- {
			if isAudioElement(cm.ElInfo[i].ElType) {
				if qcElement[i].GrantedDynBits > qcElement[elMaxBits].GrantedDynBits {
					elMaxBits = i
				}
				if qcElement[i].GrantedDynBits < qcElement[elMinBits].GrantedDynBits {
					elMinBits = i
				}
			}
		}
		if codeBits-totalBits > 0 {
			qcElement[elMinBits].GrantedDynBits += codeBits - totalBits
		} else {
			qcElement[elMaxBits].GrantedDynBits += codeBits - totalBits
		}
	}

	return AacEncOK
}

// updateUsedDynBits mirrors FDKaacEnc_updateUsedDynBits() (qc_main.cpp:677):
// sum dynBitsUsed over all audio elements into *sumDynBitsConsumed.
func updateUsedDynBits(sumDynBitsConsumed *int, qcElement [8]*QcOutElement, cm *ChannelMapping) EncoderError {
	*sumDynBitsConsumed = 0

	for i := 0; i < cm.NElements; i++ {
		if isAudioElement(cm.ElInfo[i].ElType) {
			*sumDynBitsConsumed += qcElement[i].DynBitsUsed
		}
	}

	return AacEncOK
}

// getTotalConsumedDynBits mirrors FDKaacEnc_getTotalConsumedDynBits()
// (qc_main.cpp:698): sum usedDynBits over all sub frames, returning -1 if any
// sub frame's bit consumption is not yet valid.
func getTotalConsumedDynBits(qcOut []*QcOut, nSubFrames int) int {
	totalBits := 0

	for c := 0; c < nSubFrames; c++ {
		if qcOut[c].UsedDynBits == -1 {
			return -1
		}
		totalBits += qcOut[c].UsedDynBits
	}

	return totalBits
}

// getTotalConsumedBits mirrors FDKaacEnc_getTotalConsumedBits()
// (qc_main.cpp:712): sum the per-element dyn/static/ext bits plus the global
// extension bits, byte-align (the (8-dataBits%8)%8 padding), and add globHdrBits
// per sub frame.
func getTotalConsumedBits(qcOut []*QcOut, qcElement [][8]*QcOutElement, cm *ChannelMapping, globHdrBits, nSubFrames int) int {
	totalUsedBits := 0

	for c := 0; c < nSubFrames; c++ {
		dataBits := 0
		for i := 0; i < cm.NElements; i++ {
			if isAudioElement(cm.ElInfo[i].ElType) {
				dataBits += qcElement[c][i].DynBitsUsed +
					qcElement[c][i].StaticBitsUsed +
					qcElement[c][i].ExtBitsUsed
			}
		}
		dataBits += qcOut[c].GlobalExtBits

		totalUsedBits += (8 - (dataBits % 8)) % 8
		totalUsedBits += dataBits + globHdrBits
	}
	return totalUsedBits
}

// checkMinFrameBitsDemand mirrors checkMinFrameBitsDemand() (qc_main.cpp:571):
// in the single-AU (non-superframe) build the C body short-circuits to 1.
func checkMinFrameBitsDemand(qcOut []*QcOut, minBitsPerFrame, nSubFrames int) int {
	return 1
}

// calcMaxValueInSfb mirrors FDKaacEnc_calcMaxValueInSfb() (qc_main.cpp:1219):
// for every sfb in every group, store the max |quantSpec| line magnitude in
// maxValue[sfb] and return the overall maximum (used to detect MAX_QUANT
// overflow in the quantization loop).
func calcMaxValueInSfb(sfbCnt, maxSfbPerGroup, sfbPerGroup int, sfbOffset []int, quantSpectrum []int16, maxValue []uint) int {
	maxValueAll := 0

	for sfbOffs := 0; sfbOffs < sfbCnt; sfbOffs += sfbPerGroup {
		for sfb := 0; sfb < maxSfbPerGroup; sfb++ {
			maxThisSfb := 0
			for line := sfbOffset[sfbOffs+sfb]; line < sfbOffset[sfbOffs+sfb+1]; line++ {
				tmp := int(fixpAbsShort(quantSpectrum[line]))
				maxThisSfb = fixMax(tmp, maxThisSfb)
			}

			maxValue[sfbOffs+sfb] = uint(maxThisSfb)
			maxValueAll = fixMax(maxThisSfb, maxValueAll)
		}
	}
	return maxValueAll
}

// updateBitres mirrors FDKaacEnc_updateBitres() (qc_main.cpp:1249): update the
// total bit reservoir after a frame. VBR clamps to fMin(maxBitsPerFrame,
// bitResTotMax); CBR/SFR/INVALID add the unused granted dynamic bits back.
func updateBitres(cm *ChannelMapping, qcKernel *QcState, qcOut []*QcOut) {
	switch qcKernel.BitrateMode {
	case QcdataBrModeVBR1, QcdataBrModeVBR2, QcdataBrModeVBR3, QcdataBrModeVBR4, QcdataBrModeVBR5:
		qcKernel.BitResTot = fixMin(qcKernel.MaxBitsPerFrame, qcKernel.BitResTotMax)
	default: // CBR, SFR, INVALID
		c := 0
		qcKernel.BitResTot += qcOut[c].GrantedDynBits -
			(qcOut[c].UsedDynBits + qcOut[c].TotFillBits + qcOut[c].AlignBits)
	}
}

// updateFillBits mirrors FDKaacEnc_updateFillBits() (qc_main.cpp:1168):
// precompute the fill-bit budget for the AU. SFR/FF write none; VBR aligns the
// granted-vs-used delta and pads up to minBitsPerFrame; CBR/INVALID additionally
// caps fill bits at what the bit reservoir cannot absorb.
func updateFillBits(cm *ChannelMapping, qcKernel *QcState, elBits [8]*ElementBits, qcOut []*QcOut) EncoderError {
	switch qcKernel.BitrateMode {
	case QcdataBrModeSFR:
		// no fill bits
	case QcdataBrModeFF:
		// no fill bits
	case QcdataBrModeVBR1, QcdataBrModeVBR2, QcdataBrModeVBR3, QcdataBrModeVBR4, QcdataBrModeVBR5:
		qcOut[0].TotFillBits = (qcOut[0].GrantedDynBits - qcOut[0].UsedDynBits) & 7
		qcOut[0].TotalBits = qcOut[0].StaticBits + qcOut[0].UsedDynBits +
			qcOut[0].TotFillBits + qcOut[0].ElementExtBits + qcOut[0].GlobalExtBits
		qcOut[0].TotFillBits += (fixMax(0, qcKernel.MinBitsPerFrame-qcOut[0].TotalBits) + 7) &^ 7
	default: // CBR, INVALID
		bitResSpace := qcKernel.BitResTotMax - qcKernel.BitResTot
		deltaBitRes := qcOut[0].GrantedDynBits - qcOut[0].UsedDynBits
		qcOut[0].TotFillBits = fixMax(deltaBitRes&7, deltaBitRes-(fixMax(0, bitResSpace-7)&^7))
		qcOut[0].TotalBits = qcOut[0].StaticBits + qcOut[0].UsedDynBits +
			qcOut[0].TotFillBits + qcOut[0].ElementExtBits + qcOut[0].GlobalExtBits
		qcOut[0].TotFillBits += (fixMax(0, qcKernel.MinBitsPerFrame-qcOut[0].TotalBits) + 7) &^ 7
	}

	return AacEncOK
}
