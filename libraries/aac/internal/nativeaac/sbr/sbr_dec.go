// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// This file ports the per-channel SBR decode orchestration 1:1 from
// libSBRdec/src/sbr_dec.cpp — sbr_dec() (the QMF-analysis -> HF-gen -> envelope-
// gain -> QMF-synthesis pass), createSbrDec(), resetSbrDec() — and the header /
// prev-frame init helpers from env_extr.cpp (initHeaderData, initSbrPrevFrameData).
//
// HE-AAC v1 (legacy SBR) ONLY. EXCLUDED everywhere below (with inline notes):
//   - SBRDEC_USAC_HARMONICSBR / HBE / QmfTransposer* / SBRDEC_QUAD_RATE.
//   - SBRDEC_PS_DECODED parametric-stereo slot-based synthesis branch (HE-AAC v2).
//   - PVC (pvcInitFrame/pvcDecodeFrame) and sbrDecoder_drc* (DRC).
//   - SBRDEC_SKIP_QMF_ANA / SBRDEC_SKIP_QMF_SYN (input/output always time-domain).
//   - SBRDEC_LOW_POWER (HQ complex QMF only — useLP is always false here).
//   - copyHarmonicSpectrum (HBE-only).

// cntLeadingZeros ports CntLeadingZeros (sbr_dec uses it via fNormzPos). For a
// FIXP_DBL maxVal it returns the redundant sign bits; nativeaac.CntLeadingZeros
// (== fNormzPos) is the shared primitive qmf.go already uses.
func cntLeadingZeros(x int32) int { return nativeaac.CntLeadingZeros(x) }

// scaleValuesShift ports scaleValues(ptr, len, shift) (scale.h) over an int32
// slice in place — the in-place left/right arithmetic shift the legacy-SBR
// upsampling-rescale branch uses. nativeaac.ScaleValues is the shared twin.
func scaleValuesShift(v []int32, n, shift int) {
	nativeaac.ScaleValues(v[:n], n, int32(shift))
}

// sbrDecRun ports sbr_dec (sbr_dec.cpp:260-880). timeIn is the int32 AAC-LC core
// time signal for this channel; timeOut receives the 2048-sample (noCols*64)
// interleaved SBR output at the given strideOut. applyProcessing is
// (syncState==SBR_ACTIVE). When SBRDEC_PS_DECODED is set, hSbrDecRight / timeOutRight
// / hPsDec drive the PS slot-based mono->stereo synthesis: the left channel writes
// timeOut, the PS-synthesised right channel writes timeOutRight. For the plain
// (mono/stereo v1) path hSbrDecRight / timeOutRight / hPsDec are nil.
func sbrDecRun(
	hSbrDec *SbrDec,
	timeIn, timeOut []int32,
	hSbrDecRight *SbrDec,
	timeOutRight []int32,
	strideOut int,
	hHeaderData *SbrHeaderData,
	hFrameData *SbrFrameData,
	hPrevFrameData *SbrPrevFrameData,
	applyProcessing bool,
	hPsDec *psDec,
	flags uint,
	codecFrameSize int,
	sbrInDataHeadroom int,
) {
	// useLP is always false (HQ complex QMF). SBRDEC_LOW_POWER excluded.
	ovLen := int(hSbrDec.LppTrans.pSettings.overlap)
	noCols := int(hHeaderData.NumberTimeSlots) * int(hHeaderData.TimeStep)

	inCh := hSbrDec.qmfDomainInCh
	outCh := hSbrDec.qmfDomainOutCh

	// Map QMF buffer to pointer array (Overlap + Frame). pReal/pImag start at the
	// ov_len offset where QMF analysis writes for legacy SBR.
	pLowBandReal := inCh.hQmfSlotsReal
	pLowBandImag := inCh.hQmfSlotsImag
	pReal := pLowBandReal[ovLen:]
	pImag := pLowBandImag[ovLen:]

	// SBRDEC_USAC_HARMONICSBR branch (sbr_dec.cpp:304-333) excluded: HBE/USAC.

	// Low band codec signal subband filtering. SBRDEC_SKIP_QMF_ANA excluded; the
	// analysis always runs for HE-AAC v1.
	qmfTemp := make([]int32, 2*64)
	AnalysisFiltering(&inCh.fb, pReal, pImag, &inCh.scaling, timeIn, 0+sbrInDataHeadroom, 1, qmfTemp)

	// Clear upper half of spectrum (sbr_dec.cpp:354-371). HQ: clear both re+im.
	nAnalysisBands := int(hHeaderData.NumberOfAnalysisBands)
	for slot := ovLen; slot < noCols+ovLen; slot++ {
		row := pLowBandReal[slot]
		for b := nAnalysisBands; b < 64; b++ {
			row[b] = 0
		}
		row = pLowBandImag[slot]
		for b := nAnalysisBands; b < 64; b++ {
			row[b] = 0
		}
	}

	// Shift spectral data left to gain accuracy in transposer and adjustor.
	maxVal := maxSubbandSample(pReal, pImag, 0, inCh.fb.noChannels, 0, noCols)

	reserve := fMaxI(0, cntLeadingZeros(maxVal)-1)
	reserve = fMinI(reserve, dfractBits-1-inCh.scaling.LbScale)

	rescaleSubbandSamples(pReal, pImag, 0, inCh.fb.noChannels, 0, noCols, reserve)
	inCh.scaling.LbScale += reserve

	// HBE hbe_scale swap (sbr_dec.cpp:394-399) excluded.

	// Save low band scale, wavecoding or parametric stereo may modify it.
	saveLbScale := inCh.scaling.LbScale

	if applyProcessing {
		borders := hFrameData.FrameInfo.Borders[:]
		lastSlotOffs := int(borders[hFrameData.FrameInfo.NEnvelopes]) - int(hHeaderData.NumberTimeSlots)

		// PVC (pvcInitFrame/pvcDecodeFrame/pvcEndFrame) excluded: HE-AAC v2/USAC.
		var degreeAlias [64]int32 // only used if useLP; cleared region unused here.

		// HBE keepStatesSyncedMode / lppTransposerHBE branch excluded. Legacy SBR:
		hSbrDec.prevFrameLSbr = 1
		hSbrDec.prevFrameHbeSbr = 0

		lppTransposer(
			&hSbrDec.LppTrans, &inCh.scaling, pLowBandReal,
			degreeAlias[:], pLowBandImag, false, /* useLP */
			hHeaderData.BsInfo.SbrPreprocessing != 0,
			int(hHeaderData.FreqBandData.VKMaster[0]), int(hHeaderData.TimeStep),
			int(borders[0]), lastSlotOffs, int(hHeaderData.FreqBandData.NInvfBands),
			hFrameData.SbrInvfMode[:], hPrevFrameData.SbrInvfMode[:])

		// Adjust envelope of current frame. ResetLimiterBands on patching change.
		if int(hFrameData.SbrPatchingMode) != hSbrDec.SbrCalculateEnvelope.SbrPatchingMode {
			ResetLimiterBands(
				hHeaderData.FreqBandData.LimiterBandTab[:],
				&hHeaderData.FreqBandData.NoLimiterBands,
				hHeaderData.FreqBandData.FreqBandTable(0),
				int(hHeaderData.FreqBandData.NSfb[0]),
				hSbrDec.LppTrans.pSettings.patchParam[:],
				int(hSbrDec.LppTrans.pSettings.noOfPatches),
				int(hHeaderData.BsData.LimiterBands),
				hFrameData.SbrPatchingMode,
				nil, /* xOverQmf: HBE-only, nil for legacy SBR */
				0,   /* b41Sbr */
			)
			hSbrDec.SbrCalculateEnvelope.SbrPatchingMode = int(hFrameData.SbrPatchingMode)
		}

		CalculateSbrEnvelope(
			&inCh.scaling, &hSbrDec.SbrCalculateEnvelope,
			hHeaderData, hFrameData, pLowBandReal, pLowBandImag,
			false, /* useLP */
			degreeAlias[:], flags,
			hHeaderData.FrameError != 0 || hPrevFrameData.FrameError != 0)

		// SBRDEC_MAX_HB_FADE_FRAMES high-band fade (sbr_dec.cpp:570-586): the
		// vendored config has SBRDEC_MAX_HB_FADE_FRAMES==0, so this block is
		// compiled out in C; omitted here.

		// Update hPrevFrameData (used in the next frame).
		for i := 0; i < int(hHeaderData.FreqBandData.NInvfBands); i++ {
			hPrevFrameData.SbrInvfMode[i] = hFrameData.SbrInvfMode[i]
		}
		hPrevFrameData.Coupling = hFrameData.Coupling
		hPrevFrameData.StopPos = borders[hFrameData.FrameInfo.NEnvelopes]
		hPrevFrameData.AmpRes = uint8(hFrameData.AmpResolutionCurrFrame)
		hPrevFrameData.PrevSbrPitch = hFrameData.SbrPitchInBins
		hPrevFrameData.PrevFrameInfo = hFrameData.FrameInfo
	} else {
		// Upsampling-rescale branch (sbr_dec.cpp:601-628): rescale lsb..nAnalysisBands
		// to compensate hb_scale used by synthesis. Part of legacy SBR.
		inCh.scaling.HbScale = saveLbScale

		rescale := inCh.scaling.HbScale - inCh.scaling.OvLbScale
		lsb := outCh.fb.lsb
		length := inCh.fb.noChannels - lsb

		if rescale < 0 && length > 0 {
			for i := 0; i < ovLen; i++ {
				scaleValuesShift(pLowBandReal[i][lsb:], length, rescale)
				scaleValuesShift(pLowBandImag[i][lsb:], length, rescale)
			}
		}
	}

	// Legacy SBR: save LPC filter states (sbr_dec.cpp:631-654).
	{
		length := inCh.fb.lsb
		for i := 0; i < lpcOrder+ovLen; i++ {
			copy(hSbrDec.LppTrans.lpcFilterStatesRealLegSBR[i][:length], pLowBandReal[noCols-lpcOrder+i][:length])
			copy(hSbrDec.LppTrans.lpcFilterStatesImagLegSBR[i][:length], pLowBandImag[noCols-lpcOrder+i][:length])
		}
	}

	// Synthesis subband filtering (sbr_dec.cpp:656-857).
	if flags&sbrdecPsDecoded == 0 {
		outScalefactor := -8

		// When a PS decoder is attached but PS is NOT decoded this frame, mark
		// frame-based processing so the next PS frame warms the hybrid delay line
		// (sbr_dec.cpp:664-666).
		if hPsDec != nil {
			hPsDec.ProcFrameBased = 1
		}

		// sbrDecoder_drcApply (DRC) excluded; outScalefactor stays -(8).
		ChangeOutScalefactor(&outCh.fb, outScalefactor)

		hFreq := &hHeaderData.FreqBandData
		saveUsb := outCh.fb.usb
		if outCh.fb.usb < int(hFreq.OvHighSubband) {
			outCh.fb.usb = fMinI(int(hFreq.OvHighSubband), outCh.fb.noChannels)
		}
		qmfTempSyn := make([]int32, 2*64)
		SynthesisFiltering(&outCh.fb, pLowBandReal, pLowBandImag, &inCh.scaling,
			int(hSbrDec.LppTrans.pSettings.overlap), timeOut, strideOut, qmfTempSyn)
		outCh.fb.usb = saveUsb
		hFreq.OvHighSubband = uint8(saveUsb)
	} else {
		// SBRDEC_PS_DECODED: slot-based mono->stereo PS synthesis (sbr_dec.cpp:714-857).
		sbrDecRunPs(hSbrDec, hSbrDecRight, timeOut, timeOutRight, strideOut,
			hHeaderData, reserve, hPsDec, sbrInDataHeadroom,
			pLowBandReal, pLowBandImag, noCols)
	}

	// Update overlap buffer (sbr_dec.cpp:867-872).
	qmfDomainSaveOverlap(inCh, 0)

	hSbrDec.savedStates = 0
	hPrevFrameData.FrameError = hHeaderData.FrameError
	if applyProcessing {
		hSbrDec.applySbrProcOld = 1
	} else {
		hSbrDec.applySbrProcOld = 0
	}
}

// sbrDecRunPs ports the SBRDEC_PS_DECODED branch of sbr_dec (sbr_dec.cpp:714-857):
// the slot-based mono->stereo parametric-stereo synthesis. The left/mono QMF rows
// (pLowBandReal/pLowBandImag, full slot arrays) are fed per timeslot through the
// PS upmix (ApplyPsSlot, with initSlotBasedRotation at each envelope border); the
// left channel writes timeOut and the PS-synthesised right channel writes
// timeOutRight, both via the per-slot QMF synthesis. reserve is the spectral
// pre-shift computed by the caller; inCh.scaling.LbScale already includes it.
func sbrDecRunPs(
	hSbrDec, hSbrDecRight *SbrDec,
	timeOut, timeOutRight []int32,
	strideOut int,
	hHeaderData *SbrHeaderData,
	reserve int,
	hPsDec *psDec,
	sbrInDataHeadroom int,
	pLowBandReal, pLowBandImag [][]int32,
	noCols int,
) {
	inCh := hSbrDec.qmfDomainInCh
	synQmf := &hSbrDec.qmfDomainOutCh.fb
	synQmfRight := &hSbrDecRight.qmfDomainOutCh.fb
	ovLen := int(hSbrDec.LppTrans.pSettings.overlap)

	// Adapt scaling (sbr_dec.cpp:722-737). sdiff recovers the lb_scale before the
	// caller's `lb_scale += reserve`.
	sdiff := inCh.scaling.LbScale - reserve
	scaleFactorHighBand := sdiff - inCh.scaling.HbScale
	scaleFactorLowBandOv := sdiff - inCh.scaling.OvLbScale
	scaleFactorLowBandNoOv := sdiff - inCh.scaling.LbScale

	clampScale := func(v int) int {
		return fMinI(dfractBits-1, fMaxI(-(dfractBits-1), v))
	}
	scaleFactorLowBandOv = clampScale(scaleFactorLowBandOv)
	scaleFactorLowBandNoOv = clampScale(scaleFactorLowBandNoOv)
	scaleFactorHighBand = clampScale(scaleFactorHighBand)

	// If we switched from frame- to slot-based processing copy synth filter states
	// left->right (sbr_dec.cpp:739-752). procFrameBased is cleared by
	// PreparePsProcessing below.
	if hPsDec.ProcFrameBased == 1 {
		nBandsSyn := int(inCh.pGlobalConf.nBandsSynthesis)
		synQmfRight.outScalefactor = synQmf.outScalefactor
		copy(synQmfRight.filterStates[:9*nBandsSyn], synQmf.filterStates[:9*nBandsSyn])
	}

	// Feed delay lines when parametric stereo is switched on (sbr_dec.cpp:754-756).
	preparePsProcessing(hPsDec, pLowBandReal, pLowBandImag, scaleFactorLowBandOv)

	// Use the same synthesis QMF geometry for left and right (sbr_dec.cpp:758-761).
	synQmfRight.noCol = synQmf.noCol
	synQmfRight.lsb = synQmf.lsb
	synQmfRight.usb = synQmf.usb

	pWorkBuffer := make([]int32, 2*64)
	noChannels := synQmf.noChannels

	// outScalefactor == maxShift - 8, maxShift == 0 (no DRC). outScalefactorL/R ==
	// sbrInDataHeadroom + 1 (+1 == psDiffScale, MPEG-PS) (sbr_dec.cpp:792-794).
	outScalefactor := -8
	outScalefactorLR := sbrInDataHeadroom + 1

	env := 0
	for i := 0; i < synQmf.noCol; i++ {
		rQmfReal := pWorkBuffer[:noChannels]
		rQmfImag := pWorkBuffer[noChannels : 2*noChannels]

		if i == int(hPsDec.BsData[hPsDec.ProcessSlot].AEnvStartStop[env]) {
			initSlotBasedRotation(hPsDec, env, int(hHeaderData.FreqBandData.HighSubband))
			env++
		}

		scaleLowBand := scaleFactorLowBandNoOv
		if i < ovLen {
			scaleLowBand = scaleFactorLowBandOv
		}
		applyPsSlot(hPsDec, pLowBandReal[i:], pLowBandImag[i:], rQmfReal, rQmfImag,
			scaleFactorLowBandNoOv, scaleLowBand, scaleFactorHighBand, synQmf.lsb, synQmf.usb)

		// DRC applySlot (both channels) excluded.

		ChangeOutScalefactor(synQmf, outScalefactor)
		ChangeOutScalefactor(synQmfRight, outScalefactor)

		SynthesisFilteringSlot(synQmfRight, rQmfReal, rQmfImag, outScalefactorLR, outScalefactorLR,
			timeOutRight[i*noChannels*strideOut:], strideOut, pWorkBuffer)
		SynthesisFilteringSlot(synQmf, pLowBandReal[i], pLowBandImag[i], outScalefactorLR, outScalefactorLR,
			timeOut[i*noChannels*strideOut:], strideOut, pWorkBuffer)
	}
}

// createSbrDec ports createSbrDec (sbr_dec.cpp:886-990) for HE-AAC v1: init the
// envelope calculator, prev-frame data, and the LPP transposer. HBE buffers and
// the QmfTransposer are excluded.
func createSbrDec(hSbrChannel *SbrChannel, hHeaderData *SbrHeaderData,
	pSettings *transposerSettings, flags uint, overlap, chan_ int, codecFrameSize int) sbrError {
	timeSlots := int(hHeaderData.NumberTimeSlots)
	noCols := timeSlots * int(hHeaderData.TimeStep)
	hs := &hSbrChannel.SbrDec

	hs.scaleHbe = 15
	hs.scaleLb = 15
	hs.scaleOv = 15
	hs.prevFrameLSbr = 0
	hs.prevFrameHbeSbr = 0
	hs.codecFrameSize = codecFrameSize

	if err := createSbrEnvelopeCalc(&hs.SbrCalculateEnvelope, hHeaderData, chan_, flags); err != sbrdecOK {
		return err
	}

	initSbrPrevFrameData(&hSbrChannel.prevFrameData, timeSlots)

	if err := createLppTransposer(
		&hs.LppTrans, pSettings, int(hHeaderData.FreqBandData.LowSubband),
		hHeaderData.FreqBandData.VKMaster[:], int(hHeaderData.FreqBandData.NumMaster),
		int(hHeaderData.FreqBandData.HighSubband), timeSlots, noCols,
		hHeaderData.FreqBandData.FreqBandTableNoise[:], int(hHeaderData.FreqBandData.NNfb),
		hHeaderData.SbrProcSmplRate, chan_, overlap); err != sbrdecOK {
		return err
	}

	// SBRDEC_USAC_HARMONICSBR HBE buffer / QmfTransposerCreate (sbr_dec.cpp:942-987)
	// excluded: HE-AAC v2 / USAC.
	return sbrdecOK
}

// createSbrEnvelopeCalc ports createSbrEnvelopeCalc (env_calc.cpp:1739-1775):
// clear the missing-harmonics flags and previous noise state, set prevTranEnv to
// -1, run resetSbrEnvelopeCalc, and (for chan 0 only) build the freq band tables.
func createSbrEnvelopeCalc(hs *SbrCalculateEnvelope, hHeaderData *SbrHeaderData, chan_ int, flags uint) sbrError {
	err := sbrdecOK
	for i := 0; i < addHarmonicsFlagsSz; i++ {
		hs.HarmFlagsPrev[i] = 0
		hs.HarmFlagsPrevActive[i] = 0
	}
	hs.HarmIndex = 0
	for i := range hs.PrevSbrNoiseFloorLevel {
		hs.PrevSbrNoiseFloorLevel[i] = 0
	}
	hs.PrevNNfb = 0
	for i := range hs.PrevFreqBandTableNoise {
		hs.PrevFreqBandTableNoise[i] = 0
	}
	hs.SinusoidalPositionPrev = 0
	hs.PrevTranEnv = -1

	resetSbrEnvelopeCalc(hs)

	if chan_ == 0 {
		err = resetFreqBandTables(hHeaderData, flags)
	}
	return err
}

// resetSbrDec ports resetSbrDec (sbr_dec.cpp:1025-1482) for the HE-AAC v1
// legacy-SBR path. It updates the analysis/synthesis lsb/usb from the new header,
// clears/keeps overlap data across an x-over change, rescales already-processed
// overlap spectral data to reconcile the separate low/high band scales, resets
// the LPP transposer and limiter bands, and adapts the ov_lb_scale. EXCLUDED: the
// SBRDEC_SYNTAX_USAC overlap-keep, the SBRDEC_USAC_HARMONICSBR QmfTransposer
// reinit + state copy, and useLP (always false here).
func resetSbrDec(hSbrDec *SbrDec, hHeaderData *SbrHeaderData, hPrevFrameData *SbrPrevFrameData,
	flags uint, hFrameData *SbrFrameData) sbrError {
	inCh := hSbrDec.qmfDomainInCh
	outCh := hSbrDec.qmfDomainOutCh

	oldLsb := inCh.fb.lsb
	oldUsb := inCh.fb.usb
	newLsb := int(hHeaderData.FreqBandData.LowSubband)

	overlapBufferReal := inCh.hQmfSlotsReal
	overlapBufferImag := inCh.hQmfSlotsImag

	applySbrProc := int(hHeaderData.SyncState) == sbrActive ||
		(hHeaderData.FrameError == 0 && int(hHeaderData.SyncState) == sbrHeaderState)
	applySbrProcOld := hSbrDec.applySbrProcOld

	if !applySbrProc {
		newLsb = inCh.fb.noChannels
	}
	if applySbrProcOld == 0 {
		oldLsb = inCh.fb.noChannels
		oldUsb = oldLsb
	}

	resetSbrEnvelopeCalc(&hSbrDec.SbrCalculateEnvelope)

	// Change lsb and usb (synthesis then analysis).
	outCh.fb.lsb = fMinI(outCh.fb.noChannels, int(hHeaderData.FreqBandData.LowSubband))
	outCh.fb.usb = fMinI(outCh.fb.noChannels, int(hHeaderData.FreqBandData.HighSubband))
	inCh.fb.lsb = outCh.fb.lsb
	inCh.fb.usb = outCh.fb.usb

	overlap := int(hSbrDec.LppTrans.pSettings.overlap)
	startBand := oldLsb
	stopBand := newLsb
	startSlot := fMaxI(0, int(hHeaderData.TimeStep)*(int(hPrevFrameData.StopPos)-int(hHeaderData.NumberTimeSlots)))
	size := fMaxI(0, stopBand-startBand)

	// !SBRDEC_SYNTAX_USAC (MPEG-4 SBR): zero out the x-over-area overlap.
	for l := startSlot; l < overlap; l++ {
		clearInt32(overlapBufferReal[l][startBand:], size)
		clearInt32(overlapBufferImag[l][startBand:], size)
	}

	// Reset LPC filter states across the changed band range.
	startBand = fMinI(oldLsb, newLsb)
	stopBand = fMaxI(oldLsb, newLsb)
	size = fMaxI(0, stopBand-startBand)
	clearInt32(hSbrDec.LppTrans.lpcFilterStatesRealLegSBR[0][startBand:], size)
	clearInt32(hSbrDec.LppTrans.lpcFilterStatesRealLegSBR[1][startBand:], size)
	clearInt32(hSbrDec.LppTrans.lpcFilterStatesImagLegSBR[0][startBand:], size)
	clearInt32(hSbrDec.LppTrans.lpcFilterStatesImagLegSBR[1][startBand:], size)

	if startSlot != 0 {
		var sourceExp, targetExp, targetLsb, targetUsb int
		if newLsb > oldLsb {
			// case 1 and 3
			sourceExp = int(scale2Exp(inCh.scaling.OvHbScale))
			targetExp = int(scale2Exp(inCh.scaling.OvLbScale))
			startBand = oldLsb
			if newLsb >= oldUsb {
				stopBand = oldUsb // case 1
			} else {
				stopBand = newLsb // case 3
			}
			targetLsb = 0
			targetUsb = oldLsb
		} else {
			// case 2 and 4
			sourceExp = int(scale2Exp(inCh.scaling.OvLbScale))
			targetExp = int(scale2Exp(inCh.scaling.OvHbScale))
			startBand = newLsb
			stopBand = oldLsb
			targetLsb = oldLsb
			targetUsb = oldUsb
		}

		maxVal := maxSubbandSample(overlapBufferReal, overlapBufferImag, startBand, stopBand, 0, startSlot)
		reserve := 0
		if maxVal != 0 {
			reserve = cntLeadingZeros(maxVal) - 1
		}
		reserve = fMinI(reserve, dfractBits-1-exp2Scale(sourceExp))

		if targetExp-(sourceExp-reserve) >= 0 {
			rescaleSubbandSamples(overlapBufferReal, overlapBufferImag, startBand, stopBand, 0, startSlot, reserve)
			sourceExp -= reserve
		}

		deltaExp := targetExp - sourceExp
		if deltaExp < 0 { // x-over-area is dominant
			startBand = targetLsb
			stopBand = targetUsb
			deltaExp = -deltaExp
			if newLsb > oldLsb {
				inCh.scaling.OvLbScale = exp2Scale(sourceExp)
			} else {
				inCh.scaling.OvHbScale = exp2Scale(sourceExp)
			}
		}

		for l := 0; l < startSlot; l++ {
			scaleValuesShift(overlapBufferReal[l][startBand:], stopBand-startBand, -deltaExp)
			scaleValuesShift(overlapBufferImag[l][startBand:], stopBand-startBand, -deltaExp)
		}
	}

	if err := resetLppTransposer(
		&hSbrDec.LppTrans, hHeaderData.FreqBandData.LowSubband,
		hHeaderData.FreqBandData.VKMaster[:], hHeaderData.FreqBandData.NumMaster,
		hHeaderData.FreqBandData.FreqBandTableNoise[:], hHeaderData.FreqBandData.NNfb,
		hHeaderData.FreqBandData.HighSubband, hHeaderData.SbrProcSmplRate); err != sbrdecOK {
		return err
	}

	hSbrDec.savedStates = 0

	// SBRDEC_USAC_HARMONICSBR QmfTransposerReInit + state copy (sbr_dec.cpp:1253-1399)
	// excluded: HE-AAC v2 / USAC.

	// Adapt ov_lb_scale (sbr_dec.cpp:1401-1466).
	{
		adaptLb := false
		diff := 0
		newScale := inCh.scaling.OvLbScale

		if inCh.scaling.OvLbScale != inCh.scaling.LbScale && startSlot != 0 {
			diff = int(scale2Exp(inCh.scaling.OvLbScale)) - int(scale2Exp(inCh.scaling.LbScale))
			if diff > 0 {
				adaptLb = true
				diff = -diff
				newScale = inCh.scaling.OvLbScale
			}
			stopBand = newLsb
		}

		// hFrameData.SbrPatchingMode == 1 path (legacy SBR scales filter states).
		if hFrameData.SbrPatchingMode == 1 {
			for i := 0; i < overlap+lpcOrder; i++ {
				scaleValuesShift(hSbrDec.LppTrans.lpcFilterStatesRealLegSBR[i][:], newLsb, diff)
				scaleValuesShift(hSbrDec.LppTrans.lpcFilterStatesImagLegSBR[i][:], newLsb, diff)
			}
			// SBRDEC_SYNTAX_USAC missing-states copy excluded (MPEG-4 leaves zeros).
			if newLsb > oldLsb {
				stopBand = oldLsb
			}
		}
		if adaptLb && stopBand > startBand {
			for l := startSlot; l < overlap; l++ {
				scaleValuesShift(overlapBufferReal[l][startBand:], stopBand-startBand, diff)
				scaleValuesShift(overlapBufferImag[l][startBand:], stopBand-startBand, diff)
			}
		}
		inCh.scaling.OvLbScale = newScale
	}

	err := ResetLimiterBands(
		hHeaderData.FreqBandData.LimiterBandTab[:],
		&hHeaderData.FreqBandData.NoLimiterBands,
		hHeaderData.FreqBandData.FreqBandTable(0),
		int(hHeaderData.FreqBandData.NSfb[0]),
		hSbrDec.LppTrans.pSettings.patchParam[:],
		int(hSbrDec.LppTrans.pSettings.noOfPatches),
		int(hHeaderData.BsData.LimiterBands),
		hFrameData.SbrPatchingMode,
		nil, 0)

	hSbrDec.SbrCalculateEnvelope.SbrPatchingMode = int(hFrameData.SbrPatchingMode)
	if err != resetLimiterOK {
		return sbrdecUnsupportedConfig
	}
	return sbrdecOK
}

// initHeaderData ports initHeaderData (env_extr.cpp:243-345): derive the SBR
// processing sample rate, analysis-band count, time-step and number-of-timeslots
// for the element, optionally filling the default header. HE-AAC v1: only the
// 1:1 (downsample) and 1:2 (dual-rate) cases are exercised; the 1:4 / 3:8 USAC
// branches are ported faithfully but unreachable for AAC-LC+SBR.
func initHeaderData(hHeaderData *SbrHeaderData, sampleRateIn, sampleRateOut, downscaleFactor,
	samplesPerFrame int, flags uint, setDefaultHdr bool) sbrError {
	sbrErr := sbrdecOK
	var numAnalysisBands int
	var sampleRateProc int

	if flags&(sbrdecSyntaxUsac|sbrdecSyntaxRsvd50) == 0 {
		sampleRateProc = int(sbrdecMapToStdSampleRate(uint32(sampleRateOut*downscaleFactor), 0))
	} else {
		sampleRateProc = sampleRateOut * downscaleFactor
	}

	if sampleRateIn == sampleRateOut {
		hHeaderData.SbrProcSmplRate = uint(sampleRateProc << 1)
		numAnalysisBands = 32
	} else {
		hHeaderData.SbrProcSmplRate = uint(sampleRateProc)
		switch {
		case (sampleRateOut >> 1) == sampleRateIn:
			numAnalysisBands = 32
		case (sampleRateOut >> 2) == sampleRateIn:
			numAnalysisBands = 16
		case (sampleRateOut*3)>>3 == (sampleRateIn*8)>>3:
			numAnalysisBands = 24
		default:
			return sbrdecUnsupportedConfig
		}
	}
	numAnalysisBands /= downscaleFactor

	if setDefaultHdr {
		hHeaderData.SyncState = sbrNotInitialized
		hHeaderData.Status = 0
		hHeaderData.FrameError = 0

		hHeaderData.BsInfo.AmpResolution = 1
		hHeaderData.BsInfo.XoverBand = 0
		hHeaderData.BsInfo.SbrPreprocessing = 0
		hHeaderData.BsInfo.PvcMode = 0

		hHeaderData.BsData.StartFreq = 5
		hHeaderData.BsData.StopFreq = 0
		hHeaderData.BsData.FreqScale = 0
		hHeaderData.BsData.AlterScale = 1
		hHeaderData.BsData.NoiseBands = 2
		hHeaderData.BsData.LimiterBands = 2
		hHeaderData.BsData.LimiterGains = 2
		hHeaderData.BsData.InterpolFreq = 1
		hHeaderData.BsData.SmoothingLength = 1

		if sampleRateOut*downscaleFactor >= 96000 {
			hHeaderData.BsData.StartFreq = 4
			hHeaderData.BsData.StopFreq = 3
		} else if sampleRateOut*downscaleFactor > 24000 {
			hHeaderData.BsData.StartFreq = 7
			hHeaderData.BsData.StopFreq = 3
		}
	}

	if (sampleRateOut >> 2) == sampleRateIn {
		hHeaderData.TimeStep = 4
	} else if flags&uint(sbrdecEldGrid) != 0 {
		hHeaderData.TimeStep = 1
	} else {
		hHeaderData.TimeStep = 2
	}

	hHeaderData.NumberTimeSlots = uint8((samplesPerFrame / numAnalysisBands) >> (hHeaderData.TimeStep - 1))
	if hHeaderData.NumberTimeSlots > 16 {
		sbrErr = sbrdecUnsupportedConfig
	}

	hHeaderData.NumberOfAnalysisBands = uint8(numAnalysisBands)
	if (sampleRateOut >> 2) == sampleRateIn {
		hHeaderData.NumberTimeSlots <<= 1
	}
	return sbrErr
}

// initSbrPrevFrameData ports initSbrPrevFrameData (env_extr.cpp:350-370): clear
// the previous-frame energy/noise/inverse-filter state for a fresh stream.
func initSbrPrevFrameData(h *SbrPrevFrameData, timeSlots int) {
	for i := 0; i < maxFreqCoeffs; i++ {
		h.SfbNrgPrev[i] = 0
	}
	for i := 0; i < maxNoiseCoeffs; i++ {
		h.PrevNoiseLevel[i] = 0
	}
	for i := 0; i < maxInvfBands; i++ {
		h.SbrInvfMode[i] = invfOff
	}
	h.StopPos = uint8(timeSlots)
	h.Coupling = couplingOff
	h.AmpRes = 0
	h.PrevFrameInfo = FrameInfo{}
}
