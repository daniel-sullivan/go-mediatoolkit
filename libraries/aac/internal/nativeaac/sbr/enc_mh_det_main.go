// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Continuation of the mh_det.cpp 1:1 port (see enc_mh_det.go): transientCleanUp,
// detection, detectionWithPrediction, calculateCompVector, the public
// SbrMissingHarmonicsDetectorQmf entry point and InitSbrMissingHarmonicsDetector.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// transientCleanUp is the 1:1 port of transientCleanUp (mh_det.cpp:527-643).
func transientCleanUp(quotaBuffer [][]int32, nSfb int, detectionVectors [][]uint8, pAddHarmSfb, pPrevAddHarmSfb []uint8, signBuffer [][]int32, pFreqBandTable []uint8, start, stop, newDetectionAllowed int, pNrgVector []int32, mhThresh ThresHolds) {
	for est := start; est < stop; est++ {
		for i := 0; i < nSfb; i++ {
			if pAddHarmSfb[i] != 0 || detectionVectors[est][i] != 0 {
				pAddHarmSfb[i] = 1
			} else {
				pAddHarmSfb[i] = 0
			}
		}
	}

	if newDetectionAllowed == 1 {
		for i := 0; i < nSfb-1; i++ {
			if pAddHarmSfb[i] != 0 && pAddHarmSfb[i+1] != 0 {
				li := int(pFreqBandTable[i])
				ui := int(pFreqBandTable[i+1])

				maxPosTime1 := start
				maxPos1 := li
				maxVal1 := quotaBuffer[start][li]
				for est := start; est < stop; est++ {
					for j := li; j < ui; j++ {
						if quotaBuffer[est][j] > maxVal1 {
							maxVal1 = quotaBuffer[est][j]
							maxPos1 = j
							maxPosTime1 = est
						}
					}
				}

				li = int(pFreqBandTable[i+1])
				ui = int(pFreqBandTable[i+2])

				maxPosTime2 := start
				maxPos2 := li
				maxVal2 := quotaBuffer[start][li]
				for est := start; est < stop; est++ {
					for j := li; j < ui; j++ {
						if quotaBuffer[est][j] > maxVal2 {
							maxVal2 = quotaBuffer[est][j]
							maxPos2 = j
							maxPosTime2 = est
						}
					}
				}

				if maxPos2-maxPos1 < 2 {
					if pPrevAddHarmSfb[i] == 1 && pPrevAddHarmSfb[i+1] == 0 {
						pAddHarmSfb[i+1] = 0
						for est := start; est < stop; est++ {
							detectionVectors[est][i+1] = 0
						}
					} else {
						if pPrevAddHarmSfb[i] == 0 && pPrevAddHarmSfb[i+1] == 1 {
							pAddHarmSfb[i] = 0
							for est := start; est < stop; est++ {
								detectionVectors[est][i] = 0
							}
						} else {
							if maxVal1 > maxVal2 {
								if signBuffer[maxPosTime1][maxPos2] < 0 && signBuffer[maxPosTime1][maxPos1] > 0 {
									pAddHarmSfb[i+1] = 0
									for est := start; est < stop; est++ {
										detectionVectors[est][i+1] = 0
									}
								}
							} else {
								if signBuffer[maxPosTime2][maxPos2] < 0 && signBuffer[maxPosTime2][maxPos1] > 0 {
									pAddHarmSfb[i] = 0
									for est := start; est < stop; est++ {
										detectionVectors[est][i] = 0
									}
								}
							}
						}
					}
				}
			}
		}

		removeLowPassDetection(pAddHarmSfb, detectionVectors, start, stop, nSfb, pFreqBandTable, pNrgVector, mhThresh)
	} else {
		for i := 0; i < nSfb; i++ {
			if int(pAddHarmSfb[i])-int(pPrevAddHarmSfb[i]) > 0 {
				pAddHarmSfb[i] = 0
			}
		}
	}
}

// detection is the 1:1 port of detection (mh_det.cpp:671-772).
func detection(quotaBuffer, pDiffVecScfb []int32, nSfb int, pHarmVec []uint8, pFreqBandTable []uint8, sfmOrig, sfmSbr []int32, guideVectors, newGuideVectors GuideVectors, mhThresh ThresHolds) {
	for i := 0; i < nSfb; i++ {
		var thresTemp int32
		if guideVectors.GuideVectorDiff[i] != 0 {
			thresTemp = nativeaac.FMaxDBL(nativeaac.FMultDD(mhThresh.DecayGuideDiff, guideVectors.GuideVectorDiff[i]), mhThresh.ThresHoldDiffGuide)
		} else {
			thresTemp = mhThresh.ThresHoldDiff
		}
		thresTemp = nativeaac.FMinDBL(thresTemp, mhThresh.ThresHoldDiff)

		if pDiffVecScfb[i] > thresTemp {
			pHarmVec[i] = 1
			newGuideVectors.GuideVectorDiff[i] = pDiffVecScfb[i]
		} else {
			if guideVectors.GuideVectorDiff[i] != 0 {
				guideVectors.GuideVectorOrig[i] = mhThresh.ThresHoldToneGuide
			}
		}
	}

	for i := 0; i < nSfb; i++ {
		ll := int(pFreqBandTable[i])
		lu := int(pFreqBandTable[i+1])

		thresOrig := nativeaac.FMaxDBL(nativeaac.FMultDD(guideVectors.GuideVectorOrig[i], mhThresh.DecayGuideOrig), mhThresh.ThresHoldToneGuide)
		thresOrig = nativeaac.FMinDBL(thresOrig, mhThresh.ThresHoldTone)

		if guideVectors.GuideVectorOrig[i] != 0 {
			for j := ll; j < lu; j++ {
				if quotaBuffer[j] > thresOrig {
					pHarmVec[i] = 1
					newGuideVectors.GuideVectorOrig[i] = quotaBuffer[j]
				}
			}
		}
	}

	thresOrig := mhThresh.ThresHoldTone

	for i := 0; i < nSfb; i++ {
		ll := int(pFreqBandTable[i])
		lu := int(pFreqBandTable[i+1])

		if pHarmVec[i] == 0 {
			if lu-ll > 1 {
				for j := ll; j < lu; j++ {
					if quotaBuffer[j] > thresOrig && (sfmSbr[i] > mhThresh.SfmThresSbr && sfmOrig[i] < mhThresh.SfmThresOrig) {
						pHarmVec[i] = 1
						newGuideVectors.GuideVectorOrig[i] = quotaBuffer[j]
					}
				}
			} else {
				if i < nSfb-1 {
					ll = int(pFreqBandTable[i])
					if i > 0 {
						if quotaBuffer[ll] > mhThresh.ThresHoldTone && (pDiffVecScfb[i+1] < mhThresh.InvThresHoldTone || pDiffVecScfb[i-1] < mhThresh.InvThresHoldTone) {
							pHarmVec[i] = 1
							newGuideVectors.GuideVectorOrig[i] = quotaBuffer[ll]
						}
					} else {
						if quotaBuffer[ll] > mhThresh.ThresHoldTone && pDiffVecScfb[i+1] < mhThresh.InvThresHoldTone {
							pHarmVec[i] = 1
							newGuideVectors.GuideVectorOrig[i] = quotaBuffer[ll]
						}
					}
				}
			}
		}
	}
}

// detectionWithPrediction is the 1:1 port of detectionWithPrediction
// (mh_det.cpp:783-891).
func detectionWithPrediction(quotaBuffer, pDiffVecScfb [][]int32, signBuffer [][]int32, nSfb int, pFreqBandTable []uint8, sfmOrig, sfmSbr [][]int32, detectionVectors [][]uint8, pPrevAddHarmSfb []uint8, guideVectors []GuideVectors, noEstPerFrame, detectionStart, totNoEst, newDetectionAllowed int, pAddHarmFlag *int, pAddHarmSfb []uint8, pNrgVector []int32, mhParams *DetectorParametersMH) {
	start := 0

	for i := 0; i < nSfb; i++ {
		pAddHarmSfb[i] = 0
	}

	if newDetectionAllowed != 0 {
		if totNoEst > 1 {
			start = detectionStart + 1
			if start != 0 {
				copy(guideVectors[start].GuideVectorDiff[:nSfb], guideVectors[0].GuideVectorDiff[:nSfb])
				copy(guideVectors[start].GuideVectorOrig[:nSfb], guideVectors[0].GuideVectorOrig[:nSfb])
				for k := 0; k < nSfb; k++ {
					guideVectors[start-1].GuideVectorDetected[k] = 0
				}
			}
		} else {
			start = 0
		}
	} else {
		start = 0
	}

	for est := start; est < totNoEst; est++ {
		if est > 0 {
			copy(guideVectors[est].GuideVectorDetected[:nSfb], detectionVectors[est-1][:nSfb])
		}
		for k := 0; k < nSfb; k++ {
			detectionVectors[est][k] = 0
		}

		if est < totNoEst-1 {
			for k := 0; k < nSfb; k++ {
				guideVectors[est+1].GuideVectorDiff[k] = 0
				guideVectors[est+1].GuideVectorOrig[k] = 0
				guideVectors[est+1].GuideVectorDetected[k] = 0
			}
			detection(quotaBuffer[est], pDiffVecScfb[est], nSfb, detectionVectors[est], pFreqBandTable, sfmOrig[est], sfmSbr[est], guideVectors[est], guideVectors[est+1], mhParams.ThresHolds)
		} else {
			for k := 0; k < nSfb; k++ {
				guideVectors[est].GuideVectorDiff[k] = 0
				guideVectors[est].GuideVectorOrig[k] = 0
				guideVectors[est].GuideVectorDetected[k] = 0
			}
			detection(quotaBuffer[est], pDiffVecScfb[est], nSfb, detectionVectors[est], pFreqBandTable, sfmOrig[est], sfmSbr[est], guideVectors[est], guideVectors[est], mhParams.ThresHolds)
		}
	}

	transientCleanUp(quotaBuffer, nSfb, detectionVectors, pAddHarmSfb, pPrevAddHarmSfb, signBuffer, pFreqBandTable, start, totNoEst, newDetectionAllowed, pNrgVector, mhParams.ThresHolds)

	*pAddHarmFlag = 0
	for i := 0; i < nSfb; i++ {
		if pAddHarmSfb[i] != 0 {
			*pAddHarmFlag = 1
			break
		}
	}

	copy(pPrevAddHarmSfb[:nSfb], pAddHarmSfb[:nSfb])
	copy(guideVectors[0].GuideVectorDetected[:nSfb], pAddHarmSfb[:nSfb])

	for i := 0; i < nSfb; i++ {
		guideVectors[0].GuideVectorDiff[i] = 0
		guideVectors[0].GuideVectorOrig[i] = 0

		if pAddHarmSfb[i] == 1 {
			for est := start; est < totNoEst; est++ {
				if guideVectors[est].GuideVectorDiff[i] != 0 {
					guideVectors[0].GuideVectorDiff[i] = guideVectors[est].GuideVectorDiff[i]
				}
				if guideVectors[est].GuideVectorOrig[i] != 0 {
					guideVectors[0].GuideVectorOrig[i] = guideVectors[est].GuideVectorOrig[i]
				}
			}
		}
	}
}

// calculateCompVector is the 1:1 port of calculateCompVector (mh_det.cpp:909-1003).
func calculateCompVector(pAddHarmSfb []uint8, pTonalityMatrix [][]int32, pSignMatrix [][]int32, pEnvComp []uint8, nSfb int, freqBandTable []uint8, totNoEst, maxComp int, pPrevEnvComp []uint8, newDetectionAllowed int) {
	for i := 0; i < nSfb; i++ {
		pEnvComp[i] = 0
	}

	for scfBand := 0; scfBand < nSfb; scfBand++ {
		if pAddHarmSfb[scfBand] != 0 {
			ll := int(freqBandTable[scfBand])
			lu := int(freqBandTable[scfBand+1])

			maxPosF := 0
			maxPosT := 0
			maxVal := int32(0)

			for est := 0; est < totNoEst; est++ {
				for l := ll; l < lu; l++ {
					if pTonalityMatrix[est][l] > maxVal {
						maxVal = pTonalityMatrix[est][l]
						maxPosF = l
						maxPosT = est
					}
				}
			}

			if maxPosF == ll && scfBand != 0 {
				if pAddHarmSfb[scfBand-1] == 0 {
					if pSignMatrix[maxPosT][maxPosF-1] > 0 && pSignMatrix[maxPosT][maxPosF] < 0 {
						tmp := nativeaac.FixpAbs(nativeaac.CalcLdData(pTonalityMatrix[maxPosT][maxPosF-1]) + relaxationLd64())
						tmp = (tmp >> (dfractBits - 1 - relaxationLdDataS - 1)) + 1
						compValue := int(tmp) >> 1
						if compValue > maxComp {
							compValue = maxComp
						}
						pEnvComp[scfBand-1] = uint8(compValue)
					}
				}
			}

			if maxPosF == lu-1 && scfBand+1 < nSfb {
				if pAddHarmSfb[scfBand+1] == 0 {
					if pSignMatrix[maxPosT][maxPosF] > 0 && pSignMatrix[maxPosT][maxPosF+1] < 0 {
						tmp := nativeaac.FixpAbs(nativeaac.CalcLdData(pTonalityMatrix[maxPosT][maxPosF+1]) + relaxationLd64())
						tmp = (tmp >> (dfractBits - 1 - relaxationLdDataS - 1)) + 1
						compValue := int(tmp) >> 1
						if compValue > maxComp {
							compValue = maxComp
						}
						pEnvComp[scfBand+1] = uint8(compValue)
					}
				}
			}
		}
	}

	if newDetectionAllowed == 0 {
		for scfBand := 0; scfBand < nSfb; scfBand++ {
			if pEnvComp[scfBand] != 0 && pPrevEnvComp[scfBand] == 0 {
				pEnvComp[scfBand] = 0
			}
		}
	}

	copy(pPrevEnvComp[:nSfb], pEnvComp[:nSfb])
}

// SbrMissingHarmonicsDetectorQmf is the 1:1 port of
// FDKsbrEnc_SbrMissingHarmonicsDetectorQmf (mh_det.cpp:1015-1107).
func SbrMissingHarmonicsDetectorQmf(h *SbrMissingHarmonicsDetector, pQuotaBuffer [][]int32, pSignBuffer [][]int32, indexVector []int8, pFrameInfo *SbrFrameInfo, pTranInfo []uint8, pAddHarmonicsFlag *int, pAddHarmonicsScaleFactorBands []uint8, freqBandTable []uint8, nSfb int, envelopeCompensation []uint8, pNrgVector []int32) {
	transientFlag := int(pTranInfo[1])
	transientPos := int(pTranInfo[0])
	transientDetStart := 0

	detectionVectors := h.DetectionVectors[:]
	move := h.Move
	noEstPerFrame := h.NoEstPerFrame
	totNoEst := h.TotNoEst
	prevTransientFlag := h.PreviousTransientFlag
	prevTransientFrame := h.PreviousTransientFrame
	transientPosOffset := h.TransientPosOffset
	prevTransientPos := h.PreviousTransientPos
	guideVectors := h.GuideVectors[:]
	deltaTime := h.MhParams.DeltaTime
	maxComp := h.MhParams.MaxComp

	// sfmSbr / sfmOrig / tonalityDiff: first MAX_NO_OF_ESTIMATES/2 alias the
	// detector state; the rest are scratch.
	sfmSbr := make([][]int32, encMaxNoOfEstimates)
	sfmOrig := make([][]int32, encMaxNoOfEstimates)
	tonalityDiff := make([][]int32, encMaxNoOfEstimates)
	for est := 0; est < encMaxNoOfEstimates/2; est++ {
		sfmSbr[est] = h.SfmSbr[est][:]
		sfmOrig[est] = h.SfmOrig[est][:]
		tonalityDiff[est] = h.TonalityDiff[est][:]
	}
	for est := encMaxNoOfEstimates / 2; est < encMaxNoOfEstimates; est++ {
		sfmSbr[est] = make([]int32, encMaxFreqCoeffs)
		sfmOrig[est] = make([]int32, encMaxFreqCoeffs)
		tonalityDiff[est] = make([]int32, encMaxFreqCoeffs)
	}

	newDetectionAllowed := isDetectionOfNewToneAllowed(pFrameInfo, &transientDetStart, noEstPerFrame, prevTransientFrame, prevTransientPos, prevTransientFlag, transientPosOffset, transientFlag, transientPos, deltaTime, h)

	calculateDetectorInput(pQuotaBuffer, indexVector, tonalityDiff, sfmOrig, sfmSbr, freqBandTable, nSfb, noEstPerFrame, move)

	detectionWithPrediction(pQuotaBuffer, tonalityDiff, pSignBuffer, nSfb, freqBandTable, sfmOrig, sfmSbr, detectionVectors, h.GuideScfb, guideVectors, noEstPerFrame, transientDetStart, totNoEst, newDetectionAllowed, pAddHarmonicsFlag, pAddHarmonicsScaleFactorBands, pNrgVector, h.MhParams)

	calculateCompVector(pAddHarmonicsScaleFactorBands, pQuotaBuffer, pSignBuffer, envelopeCompensation, nSfb, freqBandTable, totNoEst, maxComp, h.PrevEnvelopeCompensation, newDetectionAllowed)

	for est := 0; est < move; est++ {
		copy(tonalityDiff[est][:encMaxFreqCoeffs], tonalityDiff[est+noEstPerFrame][:encMaxFreqCoeffs])
		copy(sfmOrig[est][:encMaxFreqCoeffs], sfmOrig[est+noEstPerFrame][:encMaxFreqCoeffs])
		copy(sfmSbr[est][:encMaxFreqCoeffs], sfmSbr[est+noEstPerFrame][:encMaxFreqCoeffs])
	}
}

// InitSbrMissingHarmonicsDetector is the 1:1 port of
// FDKsbrEnc_InitSbrMissingHarmonicsDetector (mh_det.cpp:1170-1249) fused with the
// RAM allocation FDKsbrEnc_CreateSbrMissingHarmonicsDetector does (Go slices
// instead of the RAM pool). lowDelay selects paramsAacLd.
func InitSbrMissingHarmonicsDetector(h *SbrMissingHarmonicsDetector, lowDelay bool, sampleFreq, frameSize, nSfb, qmfNoChannels, totNoEst, move, noEstPerFrame int) int {
	*h = SbrMissingHarmonicsDetector{}

	// Allocate the per-estimate vectors (FDKsbrEnc_CreateSbrMissingHarmonicsDetector).
	for i := 0; i < encMaxNoOfEstimates; i++ {
		h.GuideVectors[i].GuideVectorDiff = make([]int32, encMaxFreqCoeffs)
		h.GuideVectors[i].GuideVectorOrig = make([]int32, encMaxFreqCoeffs)
		h.GuideVectors[i].GuideVectorDetected = make([]uint8, encMaxFreqCoeffs)
		h.DetectionVectors[i] = make([]uint8, encMaxFreqCoeffs)
	}
	h.PrevEnvelopeCompensation = make([]uint8, encMaxFreqCoeffs)
	h.GuideScfb = make([]uint8, encMaxFreqCoeffs)

	if lowDelay {
		switch frameSize {
		case 1024, 512:
			h.TransientPosOffset = 4 // FRAME_MIDDLE_SLOT_512LD
			h.TimeSlots = 16
		case 960, 480:
			h.TransientPosOffset = 4
			h.TimeSlots = 15
		default:
			return -1
		}
	} else {
		switch frameSize {
		case 2048, 1024:
			h.TransientPosOffset = 4 // FRAME_MIDDLE_SLOT_2048
			h.TimeSlots = 16         // NUMBER_TIME_SLOTS_2048
		case 1920, 960:
			h.TransientPosOffset = 4 // FRAME_MIDDLE_SLOT_1920
			h.TimeSlots = 15         // NUMBER_TIME_SLOTS_1920
		default:
			return -1
		}
	}

	if lowDelay {
		h.MhParams = paramsAacLd()
	} else {
		h.MhParams = paramsAac()
	}

	h.QmfNoChannels = qmfNoChannels
	h.SampleFreq = sampleFreq
	h.NSfb = nSfb
	h.TotNoEst = totNoEst
	h.Move = move
	h.NoEstPerFrame = noEstPerFrame

	// Vectors are already zeroed by make; the C re-clears them here (no-op).
	h.PreviousTransientFlag = 0
	h.PreviousTransientFrame = 0
	h.PreviousTransientPos = 0

	return 0
}

// ResetSbrMissingHarmonicsDetector is the 1:1 port of
// FDKsbrEnc_ResetSbrMissingHarmonicsDetector (mh_det.cpp:1283-1393): re-aligns
// the guide vectors / prev-compensation to a new nSfb, shifting existing entries
// so the high end stays aligned (the SBR range grows/shrinks from the bottom).
func ResetSbrMissingHarmonicsDetector(h *SbrMissingHarmonicsDetector, nSfb int) int {
	var tempGuide [encMaxFreqCoeffs]int32
	var tempGuideInt [encMaxFreqCoeffs]uint8

	nSfbPrev := h.NSfb
	h.NSfb = nSfb

	reuint := func(dst []uint8) {
		copy(tempGuideInt[:nSfbPrev], dst[:nSfbPrev])
		if nSfb > nSfbPrev {
			for i := 0; i < nSfb-nSfbPrev; i++ {
				dst[i] = 0
			}
			for i := 0; i < nSfbPrev; i++ {
				dst[i+(nSfb-nSfbPrev)] = tempGuideInt[i]
			}
		} else {
			for i := 0; i < nSfb; i++ {
				dst[i] = tempGuideInt[i+(nSfbPrev-nSfb)]
			}
		}
	}
	reint := func(dst []int32) {
		copy(tempGuide[:nSfbPrev], dst[:nSfbPrev])
		if nSfb > nSfbPrev {
			for i := 0; i < nSfb-nSfbPrev; i++ {
				dst[i] = 0
			}
			for i := 0; i < nSfbPrev; i++ {
				dst[i+(nSfb-nSfbPrev)] = tempGuide[i]
			}
		} else {
			for i := 0; i < nSfb; i++ {
				dst[i] = tempGuide[i+(nSfbPrev-nSfb)]
			}
		}
	}

	reuint(h.GuideScfb)
	reint(h.GuideVectors[0].GuideVectorDiff)
	reint(h.GuideVectors[0].GuideVectorOrig)
	reuint(h.GuideVectors[0].GuideVectorDetected)
	reuint(h.PrevEnvelopeCompensation)

	return 0
}
