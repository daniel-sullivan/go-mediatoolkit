// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// QMF analysis: time samples -> 64-band complex subband matrix, the HIGH-QUALITY
// (complex) STD path. 1:1 port of qmfForwardModulationHQ (qmf.cpp:221-300),
// qmfAnaPrototypeFirSlot (qmf_pcm.h:439-485), qmfAnalysisFilteringSlot
// (qmf_pcm.h:525-577) and qmfAnalysisFiltering (qmf_pcm.h:591-620). Everything is
// int32 FIXP_DBL with FIXP_SGL ROM; EXACT-integer parity.

// anaPrototypeFirSlot is the 1:1 port of qmfAnaPrototypeFirSlot (qmf_pcm.h:439-
// 485), the symmetric analysis prototype FIR for one slot. It folds the
// 2*no_channels filter states through the 5-tap (QMF_NO_POLY) polyphase
// prototype into analysisBuffer (length 2*no_channels). pStride == 1 for the
// 64-band case.
//
//	pData_0 = analysisBuffer + 2*no_channels - 1   (writes high half downward)
//	pData_1 = analysisBuffer                        (writes low half upward)
//	sta_0   = pFilterStates                         (forward)
//	sta_1   = pFilterStates + 2*QMF_NO_POLY*no_channels - 1 (backward)
//	staStep1 = no_channels<<1 ; staStep2 = (no_channels<<3) - 1
func anaPrototypeFirSlot(analysisBuffer []int32, noChannels int, pFilter []int16, pStride int, pFilterStates []int32) {
	pFlt := 0 // index into pFilter
	pData0 := 2*noChannels - 1
	pData1 := 0

	sta0 := 0
	sta1 := 2*qmfNoPoly*noChannels - 1
	pfltStep := qmfNoPoly * pStride
	staStep1 := noChannels << 1
	staStep2 := (noChannels << 3) - 1 // rewind one less

	for k := 0; k < noChannels; k++ {
		accu := nativeaac.FMultDiv2DS(pFilterStates[sta1], pFilter[pFlt+0])
		sta1 -= staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta1], pFilter[pFlt+1])
		sta1 -= staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta1], pFilter[pFlt+2])
		sta1 -= staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta1], pFilter[pFlt+3])
		sta1 -= staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta1], pFilter[pFlt+4])
		analysisBuffer[pData1] = accu << 1
		pData1++
		sta1 += staStep2

		pFlt += pfltStep
		accu = nativeaac.FMultDiv2DS(pFilterStates[sta0], pFilter[pFlt+0])
		sta0 += staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta0], pFilter[pFlt+1])
		sta0 += staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta0], pFilter[pFlt+2])
		sta0 += staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta0], pFilter[pFlt+3])
		sta0 += staStep1
		accu += nativeaac.FMultDiv2DS(pFilterStates[sta0], pFilter[pFlt+4])
		analysisBuffer[pData0] = accu << 1
		pData0--
		sta0 -= staStep2
	}
}

// forwardModulationHQ is the 1:1 port of qmfForwardModulationHQ (qmf.cpp:221-300)
// for the STD 64-band case (no CLDFB/MPSLDFB). It performs the complex-valued
// forward modulation: it pre-rotates timeIn (the "time advance by one sample"
// trick valid only for L==64 STD), runs DCT-IV on the real part and DST-IV on the
// imaginary part, and — because L==64 in STD mode — SKIPS the trailing complex
// rotation (the advance already accounts for it, qmf.cpp:271-272).
//
// timeIn has length 2*L (the analysis prototype output); rSubband / iSubband
// receive L == 64 complex subband values each.
func forwardModulationHQ(h *FilterBank, timeIn, rSubband, iSubband []int32) {
	L := h.noChannels
	L2 := L << 1

	if L == 64 {
		// Time advance by one sample (== the trailing complex rotation), STD L==64
		// (qmf.cpp:234-251).
		x := timeIn[1] >> 1
		y := timeIn[0]
		rSubband[0] = x + (y >> 1)
		iSubband[0] = x - (y >> 1)
		for i := 1; i < L; i++ {
			x = timeIn[i+1] >> 1 // u[n+1]
			y = timeIn[L2-i]     // u[2M-n]
			rSubband[i] = x - (y >> 1)
			iSubband[i] = x + (y >> 1)
		}
		nativeaac.QmfDctIV(rSubband, L, qmfL64SinStep, sineWindow64Flat[:], sineTable1024Flat)
		nativeaac.QmfDstIV(iSubband, L, qmfL64SinStep, sineWindow64Flat[:], sineTable1024Flat)
		// L == 64 STD: the trailing complex rotation is skipped (qmf.cpp:271-272).
		return
	}

	// L != 64 (the 32-band analysis): the else-branch pre-modulation (qmf.cpp:
	// 252-265), the L=32 DCT-IV/DST-IV, then the trailing complex rotation.
	for i := 0; i < L; i += 2 {
		x0 := timeIn[i+0] >> 1
		x1 := timeIn[i+1] >> 1
		y0 := timeIn[L2-1-i]
		y1 := timeIn[L2-2-i]
		rSubband[i+0] = x0 - (y0 >> 1)
		rSubband[i+1] = x1 - (y1 >> 1)
		iSubband[i+0] = x0 + (y0 >> 1)
		iSubband[i+1] = x1 + (y1 >> 1)
	}

	nativeaac.QmfDctIV(rSubband, L, qmfL32SinStep, sineWindow32Flat[:], sineTable1024Flat)
	nativeaac.QmfDstIV(iSubband, L, qmfL32SinStep, sineWindow32Flat[:], sineTable1024Flat)

	// Trailing complex rotation (qmf.cpp:284-297). QMF_FLAG_MPSLDFB_OPTIMIZE_
	// MODULATION excluded (MPS-only). cplxMult(&iSubband, &rSubband, iSubband,
	// rSubband, t_cos, t_sin) over len == L.
	tCos := h.tCos
	tSin := h.tSin
	for i := 0; i < L; i++ {
		ir := iSubband[i]
		rr := rSubband[i]
		iSubband[i] = nativeaac.FMultDS(ir, tCos[i]) - nativeaac.FMultDS(rr, tSin[i])
		rSubband[i] = nativeaac.FMultDS(ir, tSin[i]) + nativeaac.FMultDS(rr, tCos[i])
	}
}

// qmfL32SinStep is the dct_getTables sin_step for L==32: ld2_length == 4 (32 =
// 2^5, but dct.cpp's ld2_length = DFRACT_BITS-1-fNormz(32)-1 == 4), radix-2 case
// 0x4, sin_step == 1<<(10-4) == 64; sin_twiddle == SineTable1024 (dct.cpp:138-142).
const qmfL32SinStep = 64

// qmfL64SinStep is the dct_getTables sin_step for L==64: ld2_length == 5, radix-2
// case 0x4, sin_step == 1<<(10-5) == 32 (dct.cpp:138-142). The DCT kernels apply
// the internal `inc >>= 1`; QmfDctIV/DstIV pass this value through verbatim
// (dct_IV does NOT halve sin_step — only dct_II/III do — matching dctIV's use of
// sinStep directly).
const qmfL64SinStep = 32

// AnalysisFilteringSlot is the 1:1 port of qmfAnalysisFilteringSlot
// (qmf_pcm.h:525-577) for the HQ STD path: it feeds one slot of time input
// (no_channels samples at the given stride) into the oldest filter states, runs
// the symmetric analysis prototype FIR, the complex forward modulation, then
// shifts the filter-state delay line by no_channels.
//
// qmfReal / qmfImag receive no_channels complex subband values. timeIn is the
// int32 PCM input (FIXP_QAS); workBuffer must hold >= 2*no_channels int32. The
// FilterStates buffer is updated in place.
func AnalysisFilteringSlot(h *FilterBank, qmfReal, qmfImag, timeIn []int32, stride int, workBuffer []int32) {
	offset := h.noChannels * (qmfNoPoly*2 - 1)

	// Feed time signal into the oldest no_channels states (qmf_pcm.h:537-548).
	{
		dst := offset // into FilterStates
		ti := 0
		for i := h.noChannels >> 1; i != 0; i-- {
			h.filterStates[dst] = timeIn[ti]
			dst++
			ti += stride
			h.filterStates[dst] = timeIn[ti]
			dst++
			ti += stride
		}
	}

	// Symmetric prototype FIR (STD path; NonSymmetric excluded).
	anaPrototypeFirSlot(workBuffer, h.noChannels, h.pFilter, h.pStride, h.filterStates)

	// HQ complex forward modulation (LP path excluded from HE-AAC v1 scope).
	forwardModulationHQ(h, workBuffer, qmfReal, qmfImag)

	// Shift filter states down by no_channels (qmf_pcm.h:574-576).
	copy(h.filterStates[0:offset], h.filterStates[h.noChannels:h.noChannels+offset])
}

// AnalysisFiltering is the 1:1 port of qmfAnalysisFiltering (qmf_pcm.h:591-620)
// for the HQ STD path: it sets the low-band block exponent from timeInE and runs
// AnalysisFilteringSlot over the no_col time slots, advancing timeIn by
// no_channels*stride per slot.
//
// qmfReal / qmfImag are no_col slices of no_channels complex subband values.
func AnalysisFiltering(h *FilterBank, qmfReal, qmfImag [][]int32, scaleFactor *ScaleFactor, timeIn []int32, timeInE, stride int, workBuffer []int32) {
	noChannels := h.noChannels

	scaleFactor.LbScale = -algScalingAnalysis - timeInE
	scaleFactor.LbScale -= h.filterScale

	ti := 0
	for i := 0; i < h.noCol; i++ {
		AnalysisFilteringSlot(h, qmfReal[i], qmfImag[i], timeIn[ti:], stride, workBuffer)
		ti += noChannels * stride
	}
}
