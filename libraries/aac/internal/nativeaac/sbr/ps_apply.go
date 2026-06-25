// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Parametric-stereo upmix, ported 1:1 from the vendored Fraunhofer FDK-AAC
// psdec.cpp: CreatePsDec / ResetPsDec (instance + hybrid/decorrelator init),
// PreparePsProcessing (hybrid delay-line warm-up on frame->slot switch),
// initSlotBasedRotation (IID/ICC -> h11/h12/h21/h22 mixing matrix per envelope,
// with the inline_fixp_cos_sin rotation), applySlotBasedRotation (the H-matrix
// stereo reconstruction with per-slot interpolation), and ApplyPsSlot (the full
// per-timeslot mono->stereo synthesis: hybrid analysis -> decorrelation ->
// rotation -> hybrid synthesis). IPD/OPD synthesis is disabled (baseline PS).

// psGroupTable is groupTable[NO_IID_GROUPS+1] (psdec.cpp:429): the sub-subband
// start indices of each of the 22 IID groups.
var psGroupTable = [psNoIidGroups + 1]uint8{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11,
	12, 13, 14, 15, 16, 18, 21, 25, 30, 42, 71,
}

const psNoHybridDataBands = 71 // NO_HYBRID_DATA_BANDS (psdec.cpp:587)

// createPsDec ports CreatePsDec (psdec.cpp:139-213): open the hybrid analysis
// filterbank over the instance's LF state memory, set noSubSamples from the AAC
// frame length, open the decorrelator over the instance buffer, clear bitstream
// data, and run ResetPsDec. Returns 0 on success, -1 on error.
func createPsDec(h *psDec, aacSamplesPerFrame int) int {
	fdkHybridAnalysisOpen(&h.Mpeg.HybridAnalysis, h.pHybridAnaStatesLFdmx(), nil)

	switch aacSamplesPerFrame {
	case 960:
		h.NoSubSamples = 30
	case 1024:
		h.NoSubSamples = 32
	default:
		h.NoSubSamples = -1
	}
	if int(h.NoSubSamples) > psMaxNumCol || h.NoSubSamples <= 0 {
		return -1
	}
	h.NoChannels = psNoQmfChannels

	h.PsDecodedPrv = 0
	h.ProcFrameBased = -1
	for i := 0; i < 2; i++ {
		h.BPsDataAvail[i] = pptNone
	}

	if fdkDecorrelateOpen(&h.Mpeg.ApDecor, h.Mpeg.DecorrBufferCplx[:]) != 0 {
		return -1
	}

	for i := 0; i < 2; i++ {
		h.BsData[i] = mpegPsBsData{}
	}

	if resetPsDec(h) != sbrdecOK {
		return -1
	}
	return 0
}

// pHybridAnaStatesLFdmx returns the instance's hybrid analysis LF state memory,
// the C pHybridAnaStatesLFdmx[2*13*NO_QMF_BANDS_HYBRID20].
func (h *psDec) pHybridAnaStatesLFdmx() []int32 {
	return h.Mpeg.PHybridAnaStatesLFdmx[:]
}

// resetPsDec ports ResetPsDec (psdec.cpp:245-287): init the hybrid analysis +
// the two synthesis filterbanks, init the decorrelator (DECORR_PS, isLegacyPS),
// and seed the prev h-coefficients (h11/h12 = 0.5, h21/h22 = 0).
func resetPsDec(h *psDec) sbrError {
	h.Mpeg.LastUsb = 0

	fdkHybridAnalysisInit(&h.Mpeg.HybridAnalysis, threeToTen, psNoQmfBandsHybrid20, psNoQmfBandsHybrid20, true)

	for i := 0; i < 2; i++ {
		fdkHybridSynthesisInit(&h.Mpeg.HybridSynthesis[i], threeToTen, psNoQmfChannels, psNoQmfChannels)
	}

	if fdkDecorrelateInit(&h.Mpeg.ApDecor, 71, decorrPs, duckerAutomatic, 0, 0, 0, 0, 1 /* isLegacyPS */, 1) != 0 {
		return sbrdecNotInitialized
	}

	half := nativeaac.Fl2fxconstDBL(0.5)
	for i := 0; i < psNoIidGroups; i++ {
		h.Mpeg.H11rPrev[i] = half
		h.Mpeg.H12rPrev[i] = half
	}
	for i := range h.Mpeg.H21rPrev {
		h.Mpeg.H21rPrev[i] = 0
	}
	for i := range h.Mpeg.H22rPrev {
		h.Mpeg.H22rPrev[i] = 0
	}
	return sbrdecOK
}

// preparePsProcessing ports PreparePsProcessing (psdec.cpp:294-321): when
// switching from frame-based to slot-based processing, fill the hybrid analysis
// delay buffer by running HYBRID_FILTER_DELAY warm-up slots from the left/mono
// QMF input (descaled by scaleFactorLowBand).
func preparePsProcessing(h *psDec, rIntBufferLeft, iIntBufferLeft [][]int32, scaleFactorLowBand int) {
	if h.ProcFrameBased == 1 {
		for i := 0; i < psHybridFilterDelay; i++ {
			var qmfInput [2][psNoQmfBandsHybrid20]int32
			var hybridOutput [2][psNoSubQmfChannels]int32
			for j := 0; j < psNoQmfBandsHybrid20; j++ {
				qmfInput[0][j] = nativeaac.ScaleValue(rIntBufferLeft[i][j], int32(scaleFactorLowBand))
				qmfInput[1][j] = nativeaac.ScaleValue(iIntBufferLeft[i][j], int32(scaleFactorLowBand))
			}
			fdkHybridAnalysisApply(&h.Mpeg.HybridAnalysis, qmfInput[0][:], qmfInput[1][:], hybridOutput[0][:], hybridOutput[1][:])
		}
		h.ProcFrameBased = 0
	}
}

// initSlotBasedRotation ports initSlotBasedRotation (psdec.cpp:323-427): from the
// mapped IID/ICC indices of the current envelope, dequantize the scale factors,
// compute the rotation angles (Alpha/Beta) and the h11/h12/h21/h22 coefficients
// via inline_fixp_cos_sin, and set up the per-slot interpolation deltas
// (DeltaHxx) between the previous envelope's coefficients and this one's.
func initSlotBasedRotation(h *psDec, env, usb int) {
	var pScaleFactors []int32
	var noIidSteps int
	if h.BsData[h.ProcessSlot].BFineIidQ != 0 {
		pScaleFactors = psScaleFactorsFine[:]
		noIidSteps = psNoIidStepsFine
	} else {
		pScaleFactors = psScaleFactors[:]
		noIidSteps = psNoIidSteps
	}

	pCoef := h.Mpeg.PCoef
	for group := 0; group < psNoIidGroups; group++ {
		bin := int(psBins2GroupMap20[group])

		scaleR := pScaleFactors[noIidSteps+int(pCoef.AaIidIndexMapped[env][bin])]
		scaleL := pScaleFactors[noIidSteps-int(pCoef.AaIidIndexMapped[env][bin])]

		alphaIcc := psAlphas[pCoef.AaIccIndexMapped[env][bin]]
		beta := nativeaac.FMultDD(nativeaac.FMultDD(alphaIcc, scaleR-scaleL), fixpSqrt05)
		alpha := alphaIcc >> 1

		var trigData [4]int32
		nativeaac.InlineFixpCosSin(beta+alpha, beta-alpha, 2, trigData[:])
		h11r := nativeaac.FMultDD(scaleL, trigData[0])
		h12r := nativeaac.FMultDD(scaleR, trigData[2])
		h21r := nativeaac.FMultDD(scaleL, trigData[1])
		h22r := nativeaac.FMultDD(scaleR, trigData[3])

		// invL = 1/(length of envelope), FX_DBL2FX_SGL == (val >> 16).
		envLen := int(h.BsData[h.ProcessSlot].AEnvStartStop[env+1]) - int(h.BsData[h.ProcessSlot].AEnvStartStop[env])
		invL := int16(nativeaac.GetInvInt(envLen) >> 16)

		pCoef.H11r[group] = h.Mpeg.H11rPrev[group]
		pCoef.H12r[group] = h.Mpeg.H12rPrev[group]
		pCoef.H21r[group] = h.Mpeg.H21rPrev[group]
		pCoef.H22r[group] = h.Mpeg.H22rPrev[group]

		pCoef.DeltaH11r[group] = nativeaac.FMultDS(h11r-pCoef.H11r[group], invL)
		pCoef.DeltaH12r[group] = nativeaac.FMultDS(h12r-pCoef.H12r[group], invL)
		pCoef.DeltaH21r[group] = nativeaac.FMultDS(h21r-pCoef.H21r[group], invL)
		pCoef.DeltaH22r[group] = nativeaac.FMultDS(h22r-pCoef.H22r[group], invL)

		h.Mpeg.H11rPrev[group] = h11r
		h.Mpeg.H12rPrev[group] = h12r
		h.Mpeg.H21rPrev[group] = h21r
		h.Mpeg.H22rPrev[group] = h22r
	}
	_ = usb
}

// applySlotBasedRotation ports applySlotBasedRotation (psdec.cpp:433-537): for
// each IID group interpolate the H-matrix one slot forward (Hxx += DeltaHxx),
// then reconstruct the left/right hybrid sub-subband signals across the group's
// bands: l = H11*s + H21*d, r = H12*s + H22*d (s = left, d = decorrelated right).
func applySlotBasedRotation(h *psDec, mHybridRealLeft, mHybridImagLeft, mHybridRealRight, mHybridImagRight []int32) {
	pCoef := h.Mpeg.PCoef
	for group := 0; group < psNoIidGroups; group++ {
		pCoef.H11r[group] += pCoef.DeltaH11r[group]
		pCoef.H12r[group] += pCoef.DeltaH12r[group]
		pCoef.H21r[group] += pCoef.DeltaH21r[group]
		pCoef.H22r[group] += pCoef.DeltaH22r[group]

		start := int(psGroupTable[group])
		stop := int(psGroupTable[group+1])
		h11 := pCoef.H11r[group]
		h12 := pCoef.H12r[group]
		h21 := pCoef.H21r[group]
		h22 := pCoef.H22r[group]
		for subband := start; subband < stop; subband++ {
			// C: fMultAdd(fMultDiv2(Hxx, left), Hyy, right). On the __ARM_ARCH_8__
			// target fMultAdd(x,a,b) == fixmadd_DD == fixmadddiv2_DD(x,a,b)<<1 ==
			// (x + (a*b)>>32)<<1, i.e. the <<1 wraps the WHOLE sum (NOT x + fMult).
			// So tmp == (fMultDiv2DD(Hxx,left) + fMultDiv2DD(Hyy,right)) << 1.
			tmpLeft := (nativeaac.FMultDiv2DD(h11, mHybridRealLeft[subband]) + nativeaac.FMultDiv2DD(h21, mHybridRealRight[subband])) << 1
			tmpRight := (nativeaac.FMultDiv2DD(h12, mHybridRealLeft[subband]) + nativeaac.FMultDiv2DD(h22, mHybridRealRight[subband])) << 1
			mHybridRealLeft[subband] = tmpLeft
			mHybridRealRight[subband] = tmpRight

			tmpLeft = (nativeaac.FMultDiv2DD(h11, mHybridImagLeft[subband]) + nativeaac.FMultDiv2DD(h21, mHybridImagRight[subband])) << 1
			tmpRight = (nativeaac.FMultDiv2DD(h12, mHybridImagLeft[subband]) + nativeaac.FMultDiv2DD(h22, mHybridImagRight[subband])) << 1
			mHybridImagLeft[subband] = tmpLeft
			mHybridImagRight[subband] = tmpRight
		}
	}
}

// applyPsSlot ports ApplyPsSlot (psdec.cpp:546-714): the full per-timeslot
// mono->stereo PS synthesis. rIntBufferLeft/iIntBufferLeft are the [slot][band]
// QMF rows of the left/mono channel (rIntBufferLeft[0] is the current slot,
// indices 0..HYBRID_FILTER_DELAY index into the delay region); rIntBufferRight/
// iIntBufferRight receive the synthesised right channel for this slot.
func applyPsSlot(h *psDec, rIntBufferLeft, iIntBufferLeft [][]int32, rIntBufferRight, iIntBufferRight []int32,
	scaleFactorLowBandNoOv, scaleFactorLowBand, scaleFactorHighBand, lsb, usb int) {

	var qmfInputData [2][psNoQmfBandsHybrid20]int32
	// hybridData[ch][reim] are the per-slot hybrid sub-subband working buffers.
	var hybridData [2][2][]int32
	pHybridData := make([]int32, 4*psNoHybridDataBands)
	hybridData[0][0] = pHybridData[0*psNoHybridDataBands : 1*psNoHybridDataBands]
	hybridData[0][1] = pHybridData[1*psNoHybridDataBands : 2*psNoHybridDataBands]
	hybridData[1][0] = pHybridData[2*psNoHybridDataBands : 3*psNoHybridDataBands]
	hybridData[1][1] = pHybridData[3*psNoHybridDataBands : 4*psNoHybridDataBands]

	// Hybrid analysis. Get qmf input data and apply descaling (the HYBRID_FILTER_DELAY-th
	// slot of the left buffer is the one aligned to the current hybrid output).
	for i := 0; i < psNoQmfBandsHybrid20; i++ {
		qmfInputData[0][i] = nativeaac.ScaleValue(rIntBufferLeft[psHybridFilterDelay][i], int32(scaleFactorLowBandNoOv))
		qmfInputData[1][i] = nativeaac.ScaleValue(iIntBufferLeft[psHybridFilterDelay][i], int32(scaleFactorLowBandNoOv))
	}

	// LF part.
	fdkHybridAnalysisApply(&h.Mpeg.HybridAnalysis, qmfInputData[0][:], qmfInputData[1][:], hybridData[0][0], hybridData[0][1])

	// HF part: bands up to lsb (NO_SUB_QMF_CHANNELS-2 is the hybrid HF base offset).
	scaleValuesCopy(hybridData[0][0][psNoSubQmfChannels-2:], rIntBufferLeft[0][psNoQmfBandsHybrid20:], lsb-psNoQmfBandsHybrid20, scaleFactorLowBand)
	scaleValuesCopy(hybridData[0][1][psNoSubQmfChannels-2:], iIntBufferLeft[0][psNoQmfBandsHybrid20:], lsb-psNoQmfBandsHybrid20, scaleFactorLowBand)

	// bands from lsb to usb.
	hfOff := psNoSubQmfChannels - 2 - psNoQmfBandsHybrid20
	scaleValuesCopy(hybridData[0][0][lsb+hfOff:], rIntBufferLeft[0][lsb:], usb-lsb, scaleFactorHighBand)
	scaleValuesCopy(hybridData[0][1][lsb+hfOff:], iIntBufferLeft[0][lsb:], usb-lsb, scaleFactorHighBand)

	// bands from usb to NO_SUB_QMF_CHANNELS (should be zero for non-overlap slots).
	copy(hybridData[0][0][usb+hfOff:usb+hfOff+(psNoQmfChannels-usb)], rIntBufferLeft[0][usb:usb+(psNoQmfChannels-usb)])
	copy(hybridData[0][1][usb+hfOff:usb+hfOff+(psNoQmfChannels-usb)], iIntBufferLeft[0][usb:usb+(psNoQmfChannels-usb)])

	// Decorrelation: s_k(n) -> d_k(n) (left -> right hybrid data).
	fdkDecorrelateApply(&h.Mpeg.ApDecor, hybridData[0][0], hybridData[0][1], hybridData[1][0], hybridData[1][1], 0)

	// Stereo processing (H-matrix rotation).
	applySlotBasedRotation(h, hybridData[0][0], hybridData[0][1], hybridData[1][0], hybridData[1][1])

	// Hybrid synthesis (left -> rIntBufferLeft[0], right -> rIntBufferRight).
	for i := 0; i < 2; i++ {
		var outRe, outImg []int32
		if i == 0 {
			outRe = rIntBufferLeft[0]
			outImg = iIntBufferLeft[0]
		} else {
			outRe = rIntBufferRight
			outImg = iIntBufferRight
		}
		fdkHybridSynthesisApply(&h.Mpeg.HybridSynthesis[i], hybridData[i][0], hybridData[i][1], outRe, outImg)
	}
}

// scaleValuesCopy ports scaleValues(dst, src, len, scale) (scale.h): copy len
// int32 values from src to dst applying the arithmetic scale (left/right shift).
func scaleValuesCopy(dst, src []int32, n, scale int) {
	for i := 0; i < n; i++ {
		dst[i] = nativeaac.ScaleValue(src[i], int32(scale))
	}
}
