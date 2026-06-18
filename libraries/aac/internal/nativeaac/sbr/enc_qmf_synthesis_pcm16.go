// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// Encode-side QMF synthesis slot producing INT_PCM == int16 time samples — the
// SAMPLE_BITS == 16 instantiation of qmfSynthesisFilteringSlot the PS-encode
// downmix (ps_main.cpp DownmixPSQmfData) uses. It is byte-identical to the shared
// decode-side SynthesisFilteringSlot EXCEPT SAMPLE_BITS_QMFOUT is 16 (not 32), so
// the per-sample output formatting scale shifts by 16 bits and the result
// saturates into the int16 range the encoder's downsampled core buffer carries.
//
// The decode-side qmf_synthesis.go is the INT_PCM_QMFOUT == LONG (32-bit)
// instantiation and MUST stay 32-bit (the SBR decoder narrows separately); this
// is the distinct 16-bit twin the encoder needs, kept separate so the decode
// kernel is untouched. fdk-aac is FIXED-POINT — byte-identical.
//
// This is the HE-AAC v2 PS half-rate downmix's synthesis: it runs at L == 32
// (noChannels == noQmfBands>>1). The shared inverseModulationHQ it calls selects
// its DCT-IV/DST-IV twiddles by L (dct_getTables, dct.cpp:138-142) — L == 32 needs
// sin_step 64 + SineWindow32, NOT the L == 64 tables the 64-band decode synthesis
// uses; the ps-enc-downmix parity slice pins this stateful downmix byte-exact.

// sampleBitsQmfOut16 is SAMPLE_BITS_QMFOUT for the INT_PCM == SHORT (16-bit)
// synthesis instantiation the encoder builds (qmf.cpp:826-832, SAMPLE_BITS==16).
const sampleBitsQmfOut16 = 16

// synPrototypeFirSlotPCM16 is the SAMPLE_BITS_QMFOUT == 16 twin of
// synPrototypeFirSlot (qmf_pcm.h:128-215): identical math, but the output scale
// uses sampleBitsQmfOut16 and the saturated sample is written as int16.
func synPrototypeFirSlotPCM16(h *FilterBank, realSlot, imagSlot []int32, timeOut []int16, stride int) {
	noChannels := h.noChannels
	pFilter := h.pFilter
	pStride := h.pStride

	scale := (dfractBits - sampleBitsQmfOut16) - 1 - h.outScalefactor - h.outGainE

	pFlt := pStride * qmfNoPoly
	pFltm := (h.filterSize / 2) - pStride*qmfNoPoly

	gain := nativeaac.StcNarrow(h.outGainM)

	var rndVal int32 = 0
	if scale > 0 {
		if scale < (dfractBits - 1) {
			rndVal = int32(1) << uint(scale-1)
		} else {
			scale = dfractBits - 1
		}
	} else {
		if scale < -(dfractBits - 1) {
			scale = -(dfractBits - 1)
		}
	}

	sta := 0
	for j := noChannels - 1; j >= 0; j-- {
		imag := imagSlot[j]
		real := realSlot[j]

		are := nativeaac.FMultAddDiv2SD(h.filterStates[sta+0], pFilter[pFltm+0], real)

		if gain != int16(-32768) {
			are = nativeaac.FMultDS(are, gain)
		}
		var tmp int32
		if scale >= 0 {
			tmp = nativeaac.SaturateRightShift(are+rndVal, uint(scale))
		} else {
			tmp = nativeaac.SaturateLeftShift(are, uint(-scale))
		}
		// INT_PCM == int16: the saturated value already fits int16 (the 16-bit
		// SAMPLE_BITS_QMFOUT scale brought it into [-32768, 32767]).
		timeOut[j*stride] = int16(tmp)

		h.filterStates[sta+0] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+1], pFilter[pFlt+4], imag)
		h.filterStates[sta+1] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+2], pFilter[pFltm+1], real)
		h.filterStates[sta+2] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+3], pFilter[pFlt+3], imag)
		h.filterStates[sta+3] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+4], pFilter[pFltm+2], real)
		h.filterStates[sta+4] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+5], pFilter[pFlt+2], imag)
		h.filterStates[sta+5] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+6], pFilter[pFltm+3], real)
		h.filterStates[sta+6] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+7], pFilter[pFlt+1], imag)
		h.filterStates[sta+7] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+8], pFilter[pFltm+4], real)
		h.filterStates[sta+8] = nativeaac.FMultDiv2SD(pFilter[pFlt+0], imag)

		pFlt += pStride * qmfNoPoly
		pFltm -= pStride * qmfNoPoly
		sta += 9
	}
}

// SynthesisFilteringSlotPCM16 is the SAMPLE_BITS == 16 twin of
// SynthesisFilteringSlot (qmf_pcm.h:305-333): complex inverse modulation followed
// by the int16 synthesis prototype FIR. timeOut receives noChannels int16 samples.
func SynthesisFilteringSlotPCM16(h *FilterBank, realSlot, imagSlot []int32,
	scaleFactorLowBand, scaleFactorHighBand int, timeOut []int16, stride int, pWorkBuffer []int32) {
	inverseModulationHQ(h, realSlot, imagSlot, scaleFactorLowBand, scaleFactorHighBand, pWorkBuffer)
	synPrototypeFirSlotPCM16(h, pWorkBuffer, pWorkBuffer[h.noChannels:], timeOut, stride)
}
