// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// QMF synthesis: 64-band complex subband matrix -> time samples, the
// HIGH-QUALITY (complex) STD path. 1:1 port of qmfInverseModulationHQ
// (qmf.cpp:398-475, the !CLDFB branch), qmfSynPrototypeFirSlot (qmf_pcm.h:128-
// 215), qmfSynthesisFilteringSlot (qmf_pcm.h:305-333) and qmfSynthesisFiltering
// (qmf_pcm.h:365-404). Output here is 32-bit time samples (the INT_PCM_QMFOUT ==
// LONG instantiation, SAMPLE_BITS_QMFOUT == 32, qmf.cpp:826-832). Everything is
// int32 FIXP_DBL with FIXP_SGL ROM; EXACT-integer parity.

// sampleBitsQmfOut is SAMPLE_BITS_QMFOUT for the 32-bit synthesis output
// instantiation (qmf.cpp:828); DFRACT_BITS == 32.
const (
	sampleBitsQmfOut = 32
	dfractBits       = 32
)

// inverseModulationHQ is the 1:1 port of qmfInverseModulationHQ (qmf.cpp:398-475)
// for the STD path (the `(synQmf->flags & QMF_FLAG_CLDFB) == 0` branch). It
// scales-and-saturates the low/high band subband inputs into the work buffer's
// real/imag halves, clears above usb, runs DCT-IV on real and DST-IV on imag, and
// applies the final STD post-rotation (note the negated array accesses that
// compensate the missing minus sign in the low/high band gain, qmf.cpp:459-473).
//
// pWorkBuffer must hold 2*no_channels int32: tReal == [0:L], tImag == [L:2L].
func inverseModulationHQ(h *FilterBank, qmfReal, qmfImag []int32, scaleFactorLowBand, scaleFactorHighBand int, pWorkBuffer []int32) {
	L := h.noChannels
	M := L >> 1
	shift := 0
	tReal := pWorkBuffer[0:L]
	tImag := pWorkBuffer[L : 2*L]

	// STD (non-CLDFB) scale (qmf.cpp:426-435).
	scaleBand(tReal, qmfReal, 0, h.lsb, scaleFactorLowBand)
	scaleBand(tReal, qmfReal, h.lsb, h.usb-h.lsb, scaleFactorHighBand)
	scaleBand(tImag, qmfImag, 0, h.lsb, scaleFactorLowBand)
	scaleBand(tImag, qmfImag, h.lsb, h.usb-h.lsb, scaleFactorHighBand)

	for i := h.usb; i < L; i++ {
		tReal[i] = 0
		tImag[i] = 0
	}

	// dct_IV(.., L, ..) selects its twiddles via dct_getTables(L) (dct.cpp:138-142):
	// for the radix-2 case (L==64 and L==32) sin_twiddle is SineTable1024 either
	// way, but sin_step and the window depend on L — L==64 => step 32 / SineWindow64,
	// L==32 => step 64 / SineWindow32. The PS half-rate synthesis bank runs at L==32,
	// so the table choice must follow L (the 64-band decode path was the only prior
	// consumer, hence the earlier hard-coded L==64 tables).
	sinStep := qmfL64SinStep
	window := sineWindow64Flat[:]
	if L != 64 {
		sinStep = qmfL32SinStep
		window = sineWindow32Flat[:]
	}
	nativeaac.QmfDctIV(tReal, L, sinStep, window, sineTable1024Flat)
	nativeaac.QmfDstIV(tImag, L, sinStep, window, sineTable1024Flat)
	_ = shift

	// STD post-rotation (qmf.cpp:458-473): negated accesses.
	for i := 0; i < M; i++ {
		r1 := -tReal[i]
		i2 := -tImag[L-1-i]
		r2 := -tReal[L-i-1]
		i1 := -tImag[i]

		tReal[i] = (r1 - i1) >> 1
		tImag[L-1-i] = -(r1 + i1) >> 1
		tReal[L-i-1] = (r2 - i2) >> 1
		tImag[i] = -(r2 + i2) >> 1
	}
}

// scaleBand writes dst[off:off+n] = src[off:off+n] * 2^scalefactor with
// saturation, the scaleValuesSaturate(FIXP_DBL*, const FIXP_DBL*, INT, INT) calls
// in inverseModulationHQ. A length <= 0 (e.g. usb == lsb) is a no-op.
func scaleBand(dst, src []int32, off, n, scalefactor int) {
	if n <= 0 {
		return
	}
	nativeaac.ScaleValuesSaturateDst(dst[off:], src[off:], n, int32(scalefactor))
}

// synPrototypeFirSlot is the 1:1 port of qmfSynPrototypeFirSlot (qmf_pcm.h:128-
// 215), the symmetric synthesis prototype FIR for one slot, producing
// no_channels time-domain output samples from the no_channels real+imag inputs.
// It performs the PCM formatting (gain multiply if not -1.0, rounding, saturating
// left/right shift) into the 32-bit timeOut at the given stride. The filter
// states (9*no_channels int32 FIXP_QSS) are updated in place.
//
//	scale = (DFRACT_BITS - SAMPLE_BITS_QMFOUT) - 1 - outScalefactor - outGain_e
//	p_flt  = p_Filter + p_stride*QMF_NO_POLY
//	p_fltm = p_Filter + FilterSize/2 - p_stride*QMF_NO_POLY
func synPrototypeFirSlot(h *FilterBank, realSlot, imagSlot, timeOut []int32, stride int) {
	noChannels := h.noChannels
	pFilter := h.pFilter
	pStride := h.pStride

	scale := (dfractBits - sampleBitsQmfOut) - 1 - h.outScalefactor - h.outGainE

	pFlt := pStride * qmfNoPoly                     // p_flt  (5th of 330)
	pFltm := (h.filterSize / 2) - pStride*qmfNoPoly // p_fltm (315th of 330)

	gain := nativeaac.StcNarrow(h.outGainM) // FX_DBL2FX_SGL(outGain_m)

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

	sta := 0 // index into FilterStates
	for j := noChannels - 1; j >= 0; j-- {
		imag := imagSlot[j]
		real := realSlot[j]

		// Are = fMultAddDiv2(sta[0], p_fltm[0], real): p_fltm is FIXP_SGL, real
		// FIXP_DBL -> fMultAddDiv2(FIXP_DBL, FIXP_SGL, FIXP_DBL) (common_fix.h:317).
		are := nativeaac.FMultAddDiv2SD(h.filterStates[sta+0], pFilter[pFltm+0], real)

		if gain != int16(-32768) { // not -1.0f
			are = nativeaac.FMultDS(are, gain) // fMult(FIXP_DBL, FIXP_SGL) == fixmul_DS
		}
		var tmp int32
		if scale >= 0 {
			tmp = nativeaac.SaturateRightShift(are+rndVal, uint(scale))
		} else {
			tmp = nativeaac.SaturateLeftShift(are, uint(-scale))
		}
		timeOut[j*stride] = tmp

		h.filterStates[sta+0] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+1], pFilter[pFlt+4], imag)
		h.filterStates[sta+1] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+2], pFilter[pFltm+1], real)
		h.filterStates[sta+2] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+3], pFilter[pFlt+3], imag)
		h.filterStates[sta+3] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+4], pFilter[pFltm+2], real)
		h.filterStates[sta+4] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+5], pFilter[pFlt+2], imag)
		h.filterStates[sta+5] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+6], pFilter[pFltm+3], real)
		h.filterStates[sta+6] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+7], pFilter[pFlt+1], imag)
		h.filterStates[sta+7] = nativeaac.FMultAddDiv2SD(h.filterStates[sta+8], pFilter[pFltm+4], real)
		h.filterStates[sta+8] = nativeaac.FMultDiv2SD(pFilter[pFlt+0], imag) // fMultDiv2(FIXP_SGL,FIXP_DBL)

		pFlt += pStride * qmfNoPoly
		pFltm -= pStride * qmfNoPoly
		sta += 9 // 2*QMF_NO_POLY-1
	}
}

// SynthesisFilteringSlot is the 1:1 port of qmfSynthesisFilteringSlot
// (qmf_pcm.h:305-333) for the HQ STD path: complex inverse modulation followed by
// the symmetric synthesis prototype FIR.
func SynthesisFilteringSlot(h *FilterBank, realSlot, imagSlot []int32, scaleFactorLowBand, scaleFactorHighBand int, timeOut []int32, stride int, pWorkBuffer []int32) {
	inverseModulationHQ(h, realSlot, imagSlot, scaleFactorLowBand, scaleFactorHighBand, pWorkBuffer)
	synPrototypeFirSlot(h, pWorkBuffer, pWorkBuffer[h.noChannels:], timeOut, stride)
}

// SynthesisFiltering is the 1:1 port of qmfSynthesisFiltering (qmf_pcm.h:365-404)
// for the HQ STD path: it derives the per-area synthesis scale headroom from the
// QMF_SCALE_FACTOR and runs SynthesisFilteringSlot over the no_col time slots,
// writing no_channels samples per slot into timeOut at the given stride.
//
// qmfReal / qmfImag are no_col slices of no_channels complex subband values.
// ovLen splits overlap slots (which use ov_lb_scale) from the current slots.
func SynthesisFiltering(h *FilterBank, qmfReal, qmfImag [][]int32, scaleFactor *ScaleFactor, ovLen int, timeOut []int32, stride int, pWorkBuffer []int32) {
	L := h.noChannels

	scaleFactorHighBand := -algScalingAnalysis - scaleFactor.HbScale - h.filterScale
	scaleFactorLowBandOv := -algScalingAnalysis - scaleFactor.OvLbScale - h.filterScale
	scaleFactorLowBandNoOv := -algScalingAnalysis - scaleFactor.LbScale - h.filterScale

	for i := 0; i < h.noCol; i++ {
		scaleFactorLowBand := scaleFactorLowBandNoOv
		if i < ovLen {
			scaleFactorLowBand = scaleFactorLowBandOv
		}
		SynthesisFilteringSlot(h, qmfReal[i], qmfImag[i], scaleFactorLowBand, scaleFactorHighBand,
			timeOut[i*L*stride:], stride, pWorkBuffer)
	}
}
