// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of libSBRenc/src/ton_corr.cpp — the SBR-encoder tonality-correction
// parameter extraction. It wires the already-ported inverse-filtering
// (enc_invf_est.go), noise-floor (enc_nf_est.go) and missing-harmonics
// (enc_mh_det.go) detectors together with the LPC-based tonality-quota
// estimation and the high-band patching used to predict the decoder's tonal /
// noise ratio.
//
// Ported functions (ton_corr.cpp):
//   - FDKsbrEnc_CalculateTonalityQuotas   (ton_corr.cpp:133-341)
//   - FDKsbrEnc_TonCorrParamExtr          (ton_corr.cpp:359-456)
//   - findClosestEntryEnc                    (ton_corr.cpp:468-489)
//   - resetPatch                          (ton_corr.cpp:501-632)
//   - FDKsbrEnc_CreateTonCorrParamExtr    (ton_corr.cpp:644-678)
//   - FDKsbrEnc_InitTonCorrParamExtr      (ton_corr.cpp:690-816)
//   - FDKsbrEnc_ResetTonCorrParamExtr     (ton_corr.cpp:828-868)
//
// HE-AAC v1 only: the SBR_SYNTAX_LOW_DELAY (ELD/LD) timeSlots cases and the
// LD-specific lpcLength/estimate counts are excluded (the only supported AAC
// path is NUMBER_TIME_SLOTS_2048 / _1920). PS/HE-AACv2, USAC/HBE are out of
// scope and not referenced here. fdk-aac SBR is FIXED-POINT: EXACT integer
// parity, no FP/aac_strict discipline.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// CalculateTonalityQuotas is the 1:1 port of FDKsbrEnc_CalculateTonalityQuotas
// (ton_corr.cpp:133-341): buffers the quota/sign matrices, then computes the
// per-band 2nd-order-LPC tonality quota for the current time steps using the
// complex autocorrelation. usb is the highest+1 QMF band in the SBR range;
// qmfScale the QMF-subsample scalefactor.
//
// sourceBufferReal/Imag are bufferLength rows of (usb + numVCombine) QMF bands.
func (h *SbrTonCorrEst) CalculateTonalityQuotas(sourceBufferReal, sourceBufferImag [][]int32, usb, qmfScale int) {
	startIndexMatrix := h.StartIndexMatrix
	totNoEst := h.NumberOfEstimates
	noEstPerFrame := h.NumberOfEstimatesPerFrame
	move := h.Move
	noQmfChannels := h.NoQmfChannels
	buffLen := h.BufferLength
	stepSize := h.StepSize
	pBlockLength := h.LpcLength
	signMatrix := h.SignMatrix
	nrgVector := h.NrgVector[:]
	quotaMatrix := h.QuotaMatrix
	pNrgVectorFreq := h.NrgVectorFreq[:]

	var ac acorrCoefs
	var alphar, alphai [2]int32
	var fac int32

	// realBufRef holds 2*BAND_V_SIZE*NUM_V_COMBINE FIXP_DBL; realBuf occupies the
	// first half, imagBuf the second. The "sliding" pointer arithmetic of the C
	// is reproduced with base offsets into a flat scratch.
	realBufRef := make([]int32, 2*bandVSize*numVCombine)
	const imagBase0 = bandVSize * numVCombine
	// realBuf / imagBuf are running base indices into realBufRef.
	realBuf := 0
	imagBuf := imagBase0

	// Buffering of the quotaMatrix and the quotaMatrixTransp.
	for i := 0; i < move; i++ {
		copy(quotaMatrix[i][:noQmfChannels], quotaMatrix[i+noEstPerFrame][:noQmfChannels])
		copy(signMatrix[i][:noQmfChannels], signMatrix[i+noEstPerFrame][:noQmfChannels])
	}

	copy(nrgVector[:move], nrgVector[noEstPerFrame:noEstPerFrame+move])
	for i := startIndexMatrix; i < totNoEst; i++ {
		nrgVector[i] = 0
	}
	for i := 0; i < noQmfChannels; i++ {
		pNrgVectorFreq[i] = 0
	}

	// Calculate the quotas for the current time steps.
	for r := 0; r < usb; r++ {
		k := h.NextSample // startSample
		timeIndex := startIndexMatrix

		// Copy as many as possible Band across all Slots at once.
		if realBuf != 0 {
			realBuf -= bandVSize
			imagBuf -= bandVSize
		} else {
			realBuf += bandVSize * (numVCombine - 1)
			imagBuf += bandVSize * (numVCombine - 1)

			for i := 0; i < buffLen; i++ {
				ptr := realBuf + i
				for v := 0; v < numVCombine; v++ {
					realBufRef[ptr] = sourceBufferReal[i][r+v]
					realBufRef[ptr+bandVSize*numVCombine] = sourceBufferImag[i][r+v]
					ptr -= bandVSize
				}
			}
		}

		blockLength := pBlockLength[0]

		for k <= buffLen-blockLength {
			autoCorrScaling := nativeaac.FMinI(
				nativeaac.GetScalefactor(realBufRef[realBuf+k-lpcOrder:], lpcOrder+blockLength),
				nativeaac.GetScalefactor(realBufRef[imagBuf+k-lpcOrder:], lpcOrder+blockLength))
			autoCorrScaling = nativeaac.FMaxI(0, autoCorrScaling-1)

			nativeaac.ScaleValues(realBufRef[realBuf+k-lpcOrder:], lpcOrder+blockLength, int32(autoCorrScaling))
			nativeaac.ScaleValues(realBufRef[imagBuf+k-lpcOrder:], lpcOrder+blockLength, int32(autoCorrScaling))

			autoCorrScaling <<= 1 // consider qmf buffer scaling twice
			// autoCorr2ndCplx takes a base index (== lpcOrder) into the slice
			// starting at realBuf+k-... ; pass the slices offset to realBuf+k and
			// base lpcOrder so reBuffer[base-2] reaches realBuf+k-2.
			autoCorrScaling += autoCorr2ndCplx(&ac, realBufRef[realBuf+k-lpcOrder:], realBufRef[imagBuf+k-lpcOrder:], lpcOrder, blockLength)

			if ac.det == 0 {
				alphar[1] = 0
				alphai[1] = 0
				alphar[0] = ac.r01r >> 2
				alphai[0] = ac.r01i >> 2
				fac = nativeaac.FMultDiv2DD(ac.r00r, ac.r11r) >> 1
			} else {
				alphar[1] = (nativeaac.FMultDiv2DD(ac.r01r, ac.r12r) >> 1) -
					(nativeaac.FMultDiv2DD(ac.r01i, ac.r12i) >> 1) -
					(nativeaac.FMultDiv2DD(ac.r02r, ac.r11r) >> 1)
				alphai[1] = (nativeaac.FMultDiv2DD(ac.r01i, ac.r12r) >> 1) +
					(nativeaac.FMultDiv2DD(ac.r01r, ac.r12i) >> 1) -
					(nativeaac.FMultDiv2DD(ac.r02i, ac.r11r) >> 1)

				alphar[0] = (nativeaac.FMultDiv2DD(ac.r01r, ac.det) >> uint(ac.detScale+1)) +
					nativeaac.FMultDD(alphar[1], ac.r12r) + nativeaac.FMultDD(alphai[1], ac.r12i)
				alphai[0] = (nativeaac.FMultDiv2DD(ac.r01i, ac.det) >> uint(ac.detScale+1)) +
					nativeaac.FMultDD(alphai[1], ac.r12r) - nativeaac.FMultDD(alphar[1], ac.r12i)

				fac = nativeaac.FMultDiv2DD(ac.r00r, nativeaac.FMultDD(ac.det, ac.r11r)) >> uint(ac.detScale+1)
			}

			if fac == 0 {
				quotaMatrix[timeIndex][r] = 0
				signMatrix[timeIndex][r] = 0
			} else {
				num := nativeaac.FMultDiv2DD(alphar[0], ac.r01r) + nativeaac.FMultDiv2DD(alphai[0], ac.r01i) -
					nativeaac.FMultDiv2DD(alphar[1], nativeaac.FMultDD(ac.r02r, ac.r11r)) -
					nativeaac.FMultDiv2DD(alphai[1], nativeaac.FMultDD(ac.r02i, ac.r11r))
				num = nativeaac.FixpAbs(num)

				denom := (fac >> 1) +
					(nativeaac.FMultDiv2DD(fac, relaxationFract()) >> relaxationShift) - num
				denom = nativeaac.FixpAbs(denom)

				num = nativeaac.FMultDD(num, relaxationFract())

				numShift := int(nativeaac.CountLeadingBits(num)) - 2
				num = nativeaac.ScaleValue(num, int32(numShift))

				denomShift := int(nativeaac.CountLeadingBits(denom))
				denom = denom << uint(denomShift)

				if num > 0 && denom != 0 {
					commonShift := nativeaac.FMinI(numShift-denomShift+relaxationShift, dfractBits-1)
					if commonShift < 0 {
						commonShift = -commonShift
						tmp := nativeaac.SchurDiv(num, denom, 16)
						commonShift = nativeaac.FMinI(commonShift, int(nativeaac.CountLeadingBits(tmp)))
						quotaMatrix[timeIndex][r] = tmp << uint(commonShift)
					} else {
						quotaMatrix[timeIndex][r] = nativeaac.SchurDiv(num, denom, 16) >> uint(commonShift)
					}
				} else {
					quotaMatrix[timeIndex][r] = 0
				}

				var sign int
				if ac.r11r != 0 {
					if (ac.r01r >= 0 && ac.r11r >= 0) || (ac.r01r < 0 && ac.r11r < 0) {
						sign = 1
					} else {
						sign = -1
					}
				} else {
					sign = 1
				}

				var r2 int
				if sign < 0 {
					r2 = r // (INT) pow(-1, band)
				} else {
					r2 = r + 1 // (INT) pow(-1, band+1)
				}
				signMatrix[timeIndex][r] = int32(1 - 2*(r2&0x1))
			}

			sh := uint(nativeaac.FMinI(dfractBits-1, 2*qmfScale+autoCorrScaling+scaleNrgvec))
			nrgVector[timeIndex] += ac.r00r >> sh
			// pNrgVectorFreq[r] finally divided by noEstPerFrame, replaced by >>1.
			pNrgVectorFreq[r] = pNrgVectorFreq[r] + (ac.r00r >> sh)

			blockLength = pBlockLength[1]
			k += stepSize
			timeIndex++
		}
	}
}

// TonCorrParamExtr is the 1:1 port of FDKsbrEnc_TonCorrParamExtr
// (ton_corr.cpp:359-456): runs the invf / missing-harmonics / noise-floor
// detectors over the buffered quota matrix and writes the per-band invf modes,
// noise levels, missing-harmonic flag/index and the envelope compensation.
//
// HE-AAC v1: xposType is always XPOS_LC here, so the missing-harmonics detector
// runs; the non-LC branch is retained 1:1.
func (h *SbrTonCorrEst) TonCorrParamExtr(infVec []InvfMode, noiseLevels []int32,
	missingHarmonicFlag *int, missingHarmonicsIndex, envelopeCompensation []uint8,
	frameInfo *SbrFrameInfo, transientInfo, freqBandTable []uint8, nSfb int,
	xposType XposMode, sbrSyntaxFlags uint) {

	transientFlag := int(transientInfo[1])
	transientPos := int(transientInfo[0])

	transientFrame := 0
	if h.TransientNextFrame != 0 {
		transientFrame = 1
		h.TransientNextFrame = 0
		if transientFlag != 0 {
			if transientPos+h.TransientPosOffset >= frameInfo.Borders[frameInfo.NEnvelopes] {
				h.TransientNextFrame = 1
			}
		}
	} else {
		if transientFlag != 0 {
			if transientPos+h.TransientPosOffset < frameInfo.Borders[frameInfo.NEnvelopes] {
				transientFrame = 1
				h.TransientNextFrame = 0
			} else {
				h.TransientNextFrame = 1
			}
		}
	}
	transientFrameInvfEst := transientFrame

	// Estimate the required inverse filtering level.
	if h.SwitchInverseFilt != 0 {
		QmfInverseFilteringDetector(&h.SbrInvFilt, h.quotaMatrixRows(), h.NrgVector[:],
			h.IndexVector[:], h.FrameStartIndexInvfEst,
			h.NumberOfEstimatesPerFrame+h.FrameStartIndexInvfEst, transientFrameInvfEst, infVec)
	}

	// Detect what tones will be missing.
	if xposType == XposLc {
		SbrMissingHarmonicsDetectorQmf(&h.SbrMissingHarmonicsDetector, h.quotaMatrixRows(),
			h.signMatrixRows(), h.IndexVector[:], frameInfo, transientInfo,
			missingHarmonicFlag, missingHarmonicsIndex, freqBandTable, nSfb,
			envelopeCompensation, h.NrgVectorFreq[:])
	} else {
		*missingHarmonicFlag = 0
		for i := 0; i < nSfb; i++ {
			missingHarmonicsIndex[i] = 0
		}
	}

	// Noise floor estimation.
	infVecPtr := h.SbrInvFilt.PrevInvfMode[:]

	SbrNoiseFloorEstimateQmf(&h.SbrNoiseFloorEstimate, frameInfo, noiseLevels,
		h.quotaMatrixRows(), h.IndexVector[:], *missingHarmonicFlag,
		h.FrameStartIndex, uint(h.NumberOfEstimatesPerFrame), transientFrame,
		infVecPtr, sbrSyntaxFlags)

	// Store the invfVec data for the next frame.
	for band := 0; band < h.SbrInvFilt.NoDetectorBands; band++ {
		h.SbrInvFilt.PrevInvfMode[band] = infVec[band]
	}
}

// quotaMatrixRows / signMatrixRows present the [MAX_NO_OF_ESTIMATES][]int32
// matrices as [][]int32 for the detector calls (which index [estimate][band]).
func (h *SbrTonCorrEst) quotaMatrixRows() [][]int32 { return h.QuotaMatrix[:] }
func (h *SbrTonCorrEst) signMatrixRows() [][]int32  { return h.SignMatrix[:] }

// findClosestEntryEnc is the 1:1 port of findClosestEntryEnc (ton_corr.cpp:468-489).
func findClosestEntryEnc(goalSb int, vKMaster []uint8, numMaster, direction int) int {
	if goalSb <= int(vKMaster[0]) {
		return int(vKMaster[0])
	}
	if goalSb >= int(vKMaster[numMaster]) {
		return int(vKMaster[numMaster])
	}
	var index int
	if direction != 0 {
		index = 0
		for int(vKMaster[index]) < goalSb {
			index++
		}
	} else {
		index = numMaster
		for int(vKMaster[index]) > goalSb {
			index--
		}
	}
	return int(vKMaster[index])
}

// resetPatch is the 1:1 port of resetPatch (ton_corr.cpp:501-632): rebuilds the
// patch table and the index vector mapping high-band channels to their low-band
// source channel (-1 == guard band). Returns 1 on too-many-patches.
func (h *SbrTonCorrEst) resetPatch(xposctrl, highBandStartSb int, vKMaster []uint8, numMaster, fs, noChannels int) int {
	patchParam := h.PatchParam[:]
	sbGuard := h.Guard

	lsb := int(vKMaster[0])
	usb := int(vKMaster[numMaster])
	xoverOffset := highBandStartSb - int(vKMaster[0])

	if xposctrl == 1 {
		lsb += xoverOffset
		xoverOffset = 0
	}

	goalSb := (2*noChannels*16000 + (fs >> 1)) / fs // 16 kHz band
	goalSb = findClosestEntryEnc(goalSb, vKMaster, numMaster, 1)

	// First patch.
	sourceStartBand := h.ShiftStartSb + xoverOffset
	targetStopBand := lsb + xoverOffset

	patch := 0
	for targetStopBand < usb {
		if patch >= maxNumPatches {
			return 1
		}

		patchParam[patch].GuardStartBand = targetStopBand
		targetStopBand += sbGuard
		patchParam[patch].TargetStartBand = targetStopBand

		numBandsInPatch := goalSb - targetStopBand

		if numBandsInPatch >= lsb-sourceStartBand {
			patchDistance := targetStopBand - sourceStartBand
			patchDistance = patchDistance &^ 1
			numBandsInPatch = lsb - (targetStopBand - patchDistance)
			numBandsInPatch = findClosestEntryEnc(targetStopBand+numBandsInPatch, vKMaster, numMaster, 0) - targetStopBand
		}

		patchDistance := numBandsInPatch + targetStopBand - lsb
		patchDistance = (patchDistance + 1) &^ 1

		if numBandsInPatch <= 0 {
			patch--
		} else {
			patchParam[patch].SourceStartBand = targetStopBand - patchDistance
			patchParam[patch].TargetBandOffs = patchDistance
			patchParam[patch].NumBandsInPatch = numBandsInPatch
			patchParam[patch].SourceStopBand = patchParam[patch].SourceStartBand + numBandsInPatch
			targetStopBand += patchParam[patch].NumBandsInPatch
		}

		// All patches but first.
		sourceStartBand = h.ShiftStartSb

		if int(nativeaac.FixpAbs(int32(targetStopBand-goalSb))) < 3 {
			goalSb = usb
		}

		patch++
	}

	patch--

	// If highest patch contains less than three subband: skip it.
	if patchParam[patch].NumBandsInPatch < 3 && patch > 0 {
		patch--
	}

	h.NoOfPatches = patch + 1

	// Assign the index-vector. -1 represents a guard-band.
	for k := 0; k < h.PatchParam[0].GuardStartBand; k++ {
		h.IndexVector[k] = int8(k)
	}

	for i := 0; i < h.NoOfPatches; i++ {
		sourceStart := h.PatchParam[i].SourceStartBand
		targetStart := h.PatchParam[i].TargetStartBand
		numberOfBands := h.PatchParam[i].NumBandsInPatch
		startGuardBand := h.PatchParam[i].GuardStartBand

		for k := 0; k < (targetStart - startGuardBand); k++ {
			h.IndexVector[startGuardBand+k] = -1
		}
		for k := 0; k < numberOfBands; k++ {
			h.IndexVector[targetStart+k] = int8(sourceStart + k)
		}
	}

	return 0
}

// CreateTonCorrParamExtr is the 1:1 port of FDKsbrEnc_CreateTonCorrParamExtr
// (ton_corr.cpp:644-678): allocates the quota/sign matrix rows (chan-specific
// RAM) and creates the missing-harmonics detector. The Go port allocates the 64
// columns × MAX_NO_OF_ESTIMATES rows directly rather than from a RAM pool.
func (h *SbrTonCorrEst) CreateTonCorrParamExtr(chan_ int) int {
	*h = SbrTonCorrEst{}
	for i := 0; i < maxNoOfEstimates; i++ {
		h.QuotaMatrix[i] = make([]int32, 64)
		h.SignMatrix[i] = make([]int32, 64)
	}
	return 0
}

// InitTonCorrParamExtr is the 1:1 port of FDKsbrEnc_InitTonCorrParamExtr
// (ton_corr.cpp:690-816). HE-AAC v1: only the !SBR_SYNTAX_LOW_DELAY timeSlots
// cases (2048 / 1920) are supported.
func (h *SbrTonCorrEst) InitTonCorrParamExtr(frameSize int, sbrCfg *SbrConfigData,
	timeSlots, xposCtrl, anaMaxLevel, noiseBands, noiseFloorOffset int, useSpeechConfig uint) int {

	nCols := sbrCfg.NoQmfSlots
	fs := sbrCfg.SampleFreq
	noQmfChannels := sbrCfg.NoQmfBands

	highBandStartSb := int(sbrCfg.FreqBandTable[loRes][0])
	vKMaster := sbrCfg.VKMaster
	numMaster := sbrCfg.NumMaster

	freqBandTable := sbrCfg.FreqBandTable
	nSfb := sbrCfg.NSfb

	// HE-AAC v1 (non-LD) only.
	switch timeSlots {
	case numberTimeSlots2048:
		h.LpcLength[0] = 16 - lpcOrder
		h.LpcLength[1] = 16 - lpcOrder
		h.NumberOfEstimates = noOfEstimatesLC
		h.NumberOfEstimatesPerFrame = sbrCfg.NoQmfSlots / 16
		h.FrameStartIndexInvfEst = 0
		h.TransientPosOffset = frameMiddleSlot2048
	case numberTimeSlots1920:
		h.LpcLength[0] = 15 - lpcOrder
		h.LpcLength[1] = 15 - lpcOrder
		h.NumberOfEstimates = noOfEstimatesLC
		h.NumberOfEstimatesPerFrame = sbrCfg.NoQmfSlots / 15
		h.FrameStartIndexInvfEst = 0
		h.TransientPosOffset = frameMiddleSlot1920
	default:
		return -1
	}

	h.BufferLength = nCols
	h.StepSize = h.LpcLength[0] + lpcOrder // stepSize[0] implicitly 0.

	h.NextSample = lpcOrder
	h.Move = h.NumberOfEstimates - h.NumberOfEstimatesPerFrame
	if h.Move < 0 {
		return -1
	}
	h.StartIndexMatrix = h.NumberOfEstimates - h.NumberOfEstimatesPerFrame
	h.FrameStartIndex = 0
	h.PrevTransientFlag = 0
	h.TransientNextFrame = 0

	h.NoQmfChannels = noQmfChannels

	for i := 0; i < h.NumberOfEstimates; i++ {
		for j := 0; j < noQmfChannels; j++ {
			h.QuotaMatrix[i][j] = 0
			h.SignMatrix[i][j] = 0
		}
	}

	// Reset the patch.
	h.Guard = 0
	h.ShiftStartSb = 1

	if h.resetPatch(xposCtrl, highBandStartSb, vKMaster, numMaster, fs, noQmfChannels) != 0 {
		return 1
	}

	if InitSbrNoiseFloorEstimate(&h.SbrNoiseFloorEstimate, anaMaxLevel, freqBandTable[loRes],
		nSfb[loRes], noiseBands, noiseFloorOffset, timeSlots, useSpeechConfig) != 0 {
		return 1
	}

	if InitInvFiltDetector(&h.SbrInvFilt, h.SbrNoiseFloorEstimate.FreqBandTableQmf[:],
		h.SbrNoiseFloorEstimate.NoNoiseBands, useSpeechConfig) != 0 {
		return 1
	}

	lowDelay := sbrCfg.SbrSyntaxFlags&sbrSyntaxLowDelay != 0
	if InitSbrMissingHarmonicsDetector(&h.SbrMissingHarmonicsDetector, lowDelay, fs, frameSize,
		nSfb[hiRes], noQmfChannels, h.NumberOfEstimates, h.Move, h.NumberOfEstimatesPerFrame) != 0 {
		return 1
	}

	return 0
}

// ResetTonCorrParamExtr is the 1:1 port of FDKsbrEnc_ResetTonCorrParamExtr
// (ton_corr.cpp:828-868). The missing-harmonics detector has no standalone
// reset; its Init re-allocates+clears (equivalent to the C reset for a fresh
// nSfb), matching the C FDKsbrEnc_ResetSbrMissingHarmonicsDetector contract.
func (h *SbrTonCorrEst) ResetTonCorrParamExtr(xposctrl, highBandStartSb int,
	vKMaster []uint8, numMaster, fs int, freqBandTable [2][]uint8, nSfb []int, noQmfChannels int) int {

	h.Guard = 0
	h.ShiftStartSb = 1

	if h.resetPatch(xposctrl, highBandStartSb, vKMaster, numMaster, fs, noQmfChannels) != 0 {
		return 1
	}

	if ResetSbrNoiseFloorEstimate(&h.SbrNoiseFloorEstimate, freqBandTable[loRes], nSfb[loRes]) != 0 {
		return 1
	}

	if ResetInvFiltDetector(&h.SbrInvFilt, h.SbrNoiseFloorEstimate.FreqBandTableQmf[:],
		h.SbrNoiseFloorEstimate.NoNoiseBands) != 0 {
		return 1
	}

	if ResetSbrMissingHarmonicsDetector(&h.SbrMissingHarmonicsDetector, nSfb[hiRes]) != 0 {
		return 1
	}

	return 0
}
