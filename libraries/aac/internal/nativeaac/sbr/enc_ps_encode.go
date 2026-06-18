// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Parametric-stereo parameter EXTRACTION + quantization + DPCM/rate decision —
// 1:1 port of libSBRenc/src/ps_encode.cpp (M. Neuendorf, N. Rettelbach,
// M. Multrus). Given the left/right hybrid sub-band data of a frame this
// computes the per-band L/R energies and cross-correlation, derives IID (in dB)
// and ICC (coherence), reduces the envelope count, quantizes IID/ICC, picks
// coarse-vs-fine and freq-vs-time DPCM per envelope by trial bit counts, and
// fills the PS_OUT for the bitstream writer.
//
// FDK-AAC-derived; see libfdk/COPYING. Fenced behind the aacfdk build tag.
// FIXED-POINT => exact-integer parity.
package sbr

// addFIXP_DBL ports FDKsbrEnc_addFIXP_DBL (ps_encode.cpp:116-119).
func addFIXP_DBL(x, y, z []int32, n int) {
	for i := 0; i < n; i++ {
		z[i] = (x[i] >> 1) + (y[i] >> 1)
	}
}

// initPSData ports InitPSData (ps_encode.cpp:172-210).
func initPSData(h *psData) {
	*h = psData{}
	for i := 0; i < psMaxBandsE; i++ {
		h.iidIdxLast[i] = 0
		h.iccIdxLast[i] = 0
	}
	h.iidEnable, h.iidEnableLast = 0, 0
	h.iccEnable, h.iccEnableLast = 0, 0
	h.iidQuantMode, h.iidQuantModeLast = psIidResCoarse, psIidResCoarse
	h.iccQuantMode, h.iccQuantModeLast = psIccRotA, psIccRotA

	for env := 0; env < psMaxEnvelopesE; env++ {
		h.iccDiffMode[env] = psDeltaFreq
		for i := 0; i < psMaxBandsE; i++ {
			h.iidIdx[env][i] = 0
			h.iccIdx[env][i] = 0
		}
	}
	h.nEnvelopesLast = 0
	h.headerCnt = maxPsNoHeaderCnt
	h.iidTimeCnt = maxTimeDiffFrames
	h.iccTimeCnt = maxTimeDiffFrames
	h.noEnvCnt = maxNoEnvCnt
}

// quantizeCoef ports quantizeCoef (ps_encode.cpp:212-233).
func quantizeCoef(input []int32, nBands int, quantTable []int32, idxOffset, nQuantSteps int, quantOut []int) int32 {
	quantErr := int32(0)
	for band := 0; band < nBands; band++ {
		idx := 0
		for idx = 0; idx < nQuantSteps-1; idx++ {
			if fAbs((input[band]>>1)-(quantTable[idx+1]>>1)) > fAbs((input[band]>>1)-(quantTable[idx]>>1)) {
				break
			}
		}
		quantErr += fAbs(input[band]-quantTable[idx]) >> psQuantScale
		quantOut[band] = idx - idxOffset
	}
	return quantErr
}

// getICCMode ports getICCMode (ps_encode.cpp:235-253).
func getICCMode(nBands, rotType int) int {
	mode := 0
	switch nBands {
	case psBandsCoarse:
		mode = psResCoarse
	case psBandsMid:
		mode = psResMid
	default:
		mode = 0
	}
	if rotType == psIccRotB {
		mode += 3
	}
	return mode
}

// getIIDMode ports getIIDMode (ps_encode.cpp:255-275).
func getIIDMode(nBands, iidRes int) int {
	mode := 0
	switch nBands {
	case psBandsCoarse:
		mode = psResCoarse
	case psBandsMid:
		mode = psResMid
	default:
		mode = 0
	}
	if iidRes == psIidResFine {
		mode += 3
	}
	return mode
}

// envelopeReducible ports envelopeReducible (ps_encode.cpp:277-327).
func envelopeReducible(iid, icc [][]int32, psBands, nEnvelopes int) int {
	reducible := 1
	var dIid, dIcc int32

	// iidErrThreshold = fMultDiv2(FL2FXCONST_DBL(6.5f*6.5f/(IID_SCALE_FT*IID_SCALE_FT)), psBands << (DFRACT_BITS-THRESH_SCALE))
	iidErrThreshold := fMultDiv2(fl2fxconstDBL(6.5*6.5/(iidScaleFt*iidScaleFt)), int32(psBands)<<(dfractBits-threshScale))
	iccErrThreshold := fMultDiv2(fl2fxconstDBL(0.75*0.75), int32(psBands)<<(dfractBits-threshScale))

	if nEnvelopes <= 1 {
		reducible = 0
	} else {
		for e := 0; (e < nEnvelopes/2) && (reducible != 0); e++ {
			iidMeanError := int32(0)
			iccMeanError := int32(0)
			for b := 0; b < psBands; b++ {
				dIid = (iid[2*e][b] >> 1) - (iid[2*e+1][b] >> 1)
				dIcc = (icc[2*e][b] >> 1) - (icc[2*e+1][b] >> 1)
				iidMeanError += fPow2Div2PS(dIid) >> (5 - 1)
				iccMeanError += fPow2Div2PS(dIcc) >> (5 - 1)
			}
			if (iidMeanError > iidErrThreshold) || (iccMeanError > iccErrThreshold) {
				reducible = 0
			}
		}
	}
	return reducible
}

// processIidData ports processIidData (ps_encode.cpp:329-513).
func processIidData(psd *psData, iid [][]int32, psBands, nEnvelopes int, quantErrorThreshold int32) {
	var iidIdxFine [psMaxEnvelopesE][psMaxBandsE]int
	var iidIdxCoarse [psMaxEnvelopesE][psMaxBandsE]int

	errIID := int32(0)
	errIIDFine := int32(0)
	bitsIidFreq := 0
	bitsIidTime := 0
	bitsFineTot := 0
	bitsCoarseTot := 0
	encErr := 0
	var diffMode [psMaxEnvelopesE]int
	var diffModeFine [psMaxEnvelopesE]int
	loudnDiff := 0
	iidTransmit := 0

	for env := 0; env < nEnvelopes; env++ {
		errIID += quantizeCoef(iid[env], psBands, iidQuantFx[:], 7, 15, iidIdxCoarse[env][:])
		errIIDFine += quantizeCoef(iid[env], psBands, iidQuantFineFx[:], 15, 31, iidIdxFine[env][:])
	}

	psd.iidEnable = 0
	for env := 0; env < nEnvelopes; env++ {
		for band := 0; band < psBands; band++ {
			loudnDiff += iAbs(iidIdxCoarse[env][band])
			iidTransmit++
		}
	}

	if int32(loudnDiff) > fMultIPS(fl2fxconstDBL(0.7), int32(iidTransmit)) {
		psd.iidEnable = 1
	}

	if psd.iidEnable == 0 {
		psd.iidTimeCnt = maxTimeDiffFrames
		for env := 0; env < nEnvelopes; env++ {
			psd.iidDiffMode[env] = psDeltaFreq
			for i := 0; i < psBands; i++ {
				psd.iidIdx[env][i] = 0
			}
		}
		return
	}

	// COARSE bits, first envelope
	bitsIidFreq = encodeIid(nil, iidIdxCoarse[0][:], nil, psBands, psIidResCoarse, psDeltaFreq, &encErr)

	if (psd.iidTimeCnt >= maxTimeDiffFrames) || (psd.iidQuantModeLast == psIidResFine) {
		bitsIidTime = doNotUseThisMode
	} else {
		bitsIidTime = encodeIid(nil, iidIdxCoarse[0][:], psd.iidIdxLast[:], psBands, psIidResCoarse, psDeltaTime, &encErr)
	}

	if bitsIidTime > bitsIidFreq {
		diffMode[0] = psDeltaFreq
		bitsCoarseTot = bitsIidFreq
	} else {
		diffMode[0] = psDeltaTime
		bitsCoarseTot = bitsIidTime
	}

	for env := 1; env < nEnvelopes; env++ {
		bitsIidFreq = encodeIid(nil, iidIdxCoarse[env][:], nil, psBands, psIidResCoarse, psDeltaFreq, &encErr)
		bitsIidTime = encodeIid(nil, iidIdxCoarse[env][:], iidIdxCoarse[env-1][:], psBands, psIidResCoarse, psDeltaTime, &encErr)
		if bitsIidTime > bitsIidFreq {
			diffMode[env] = psDeltaFreq
			bitsCoarseTot += bitsIidFreq
		} else {
			diffMode[env] = psDeltaTime
			bitsCoarseTot += bitsIidTime
		}
	}

	// FINE bits, first envelope
	bitsIidFreq = encodeIid(nil, iidIdxFine[0][:], nil, psBands, psIidResFine, psDeltaFreq, &encErr)

	if (psd.iidTimeCnt >= maxTimeDiffFrames) || (psd.iidQuantModeLast == psIidResCoarse) {
		bitsIidTime = doNotUseThisMode
	} else {
		bitsIidTime = encodeIid(nil, iidIdxFine[0][:], psd.iidIdxLast[:], psBands, psIidResFine, psDeltaTime, &encErr)
	}

	if bitsIidTime > bitsIidFreq {
		diffModeFine[0] = psDeltaFreq
		bitsFineTot = bitsIidFreq
	} else {
		diffModeFine[0] = psDeltaTime
		bitsFineTot = bitsIidTime
	}

	for env := 1; env < nEnvelopes; env++ {
		bitsIidFreq = encodeIid(nil, iidIdxFine[env][:], nil, psBands, psIidResFine, psDeltaFreq, &encErr)
		bitsIidTime = encodeIid(nil, iidIdxFine[env][:], iidIdxFine[env-1][:], psBands, psIidResFine, psDeltaTime, &encErr)
		if bitsIidTime > bitsIidFreq {
			diffModeFine[env] = psDeltaFreq
			bitsFineTot += bitsIidFreq
		} else {
			diffModeFine[env] = psDeltaTime
			bitsFineTot += bitsIidTime
		}
	}

	if bitsFineTot == bitsCoarseTot {
		if errIIDFine < errIID {
			bitsCoarseTot = doNotUseThisMode
		} else {
			bitsFineTot = doNotUseThisMode
		}
	} else {
		minThreshold := int32(0x00019999) * int32(psBands*nEnvelopes)

		if fixMaxI32(((errIIDFine>>1)+(minThreshold>>1))>>1, fMult(quantErrorThreshold, errIIDFine)) < (errIID >> 2) {
			bitsCoarseTot = doNotUseThisMode
		} else if fixMaxI32(((errIID>>1)+(minThreshold>>1))>>1, fMult(quantErrorThreshold, errIID)) < (errIIDFine >> 2) {
			bitsFineTot = doNotUseThisMode
		}
	}

	if bitsFineTot < bitsCoarseTot {
		psd.iidQuantMode = psIidResFine
		for env := 0; env < nEnvelopes; env++ {
			psd.iidDiffMode[env] = diffModeFine[env]
			copy(psd.iidIdx[env][:psBands], iidIdxFine[env][:psBands])
		}
	} else {
		psd.iidQuantMode = psIidResCoarse
		for env := 0; env < nEnvelopes; env++ {
			psd.iidDiffMode[env] = diffMode[env]
			copy(psd.iidIdx[env][:psBands], iidIdxCoarse[env][:psBands])
		}
	}

	for env := 0; env < nEnvelopes; env++ {
		if psd.iidDiffMode[env] == psDeltaTime {
			psd.iidTimeCnt++
		} else {
			psd.iidTimeCnt = 0
		}
	}
}

// similarIid ports similarIid (ps_encode.cpp:515-543).
func similarIid(psd *psData, psBands, nEnvelopes int) int {
	diffThr := 3
	if psd.iidQuantMode == psIidResCoarse {
		diffThr = 2
	}
	sumDiffThr := diffThr * psBands / 4
	similar := 0
	if (nEnvelopes == psd.nEnvelopesLast) && (nEnvelopes == 1) {
		similar = 1
		for env := 0; env < nEnvelopes; env++ {
			sumDiff := 0
			b := 0
			for {
				diff := iAbs(psd.iidIdx[env][b] - psd.iidIdxLast[b])
				sumDiff += diff
				if (diff > diffThr) || (sumDiff > sumDiffThr) {
					similar = 0
				}
				b++
				if !((b < psBands) && (similar > 0)) {
					break
				}
			}
		}
	}
	return similar
}

// similarIcc ports similarIcc (ps_encode.cpp:545-573).
func similarIcc(psd *psData, psBands, nEnvelopes int) int {
	diffThr := 2
	sumDiffThr := diffThr * psBands / 4
	similar := 0
	if (nEnvelopes == psd.nEnvelopesLast) && (nEnvelopes == 1) {
		similar = 1
		for env := 0; env < nEnvelopes; env++ {
			sumDiff := 0
			b := 0
			for {
				diff := iAbs(psd.iccIdx[env][b] - psd.iccIdxLast[b])
				sumDiff += diff
				if (diff > diffThr) || (sumDiff > sumDiffThr) {
					similar = 0
				}
				b++
				if !((b < psBands) && (similar > 0)) {
					break
				}
			}
		}
	}
	return similar
}

// processIccData ports processIccData (ps_encode.cpp:575-640).
func processIccData(psd *psData, icc [][]int32, psBands, nEnvelopes int) {
	errICC := int32(0)
	encErr := 0
	inCoherence := 0
	iccTransmit := 0

	iccIdxLast := psd.iccIdxLast[:]

	for env := 0; env < nEnvelopes; env++ {
		errICC += quantizeCoef(icc[env], psBands, iccQuant[:], 0, 8, psd.iccIdx[env][:])
	}
	_ = errICC

	psd.iccEnable = 0
	for env := 0; env < nEnvelopes; env++ {
		for band := 0; band < psBands; band++ {
			inCoherence += psd.iccIdx[env][band]
			iccTransmit++
		}
	}
	if int32(inCoherence) > fMultIPS(fl2fxconstDBL(0.5), int32(iccTransmit)) {
		psd.iccEnable = 1
	}

	if psd.iccEnable == 0 {
		psd.iccTimeCnt = maxTimeDiffFrames
		for env := 0; env < nEnvelopes; env++ {
			psd.iccDiffMode[env] = psDeltaFreq
			for i := 0; i < psBands; i++ {
				psd.iccIdx[env][i] = 0
			}
		}
		return
	}

	for env := 0; env < nEnvelopes; env++ {
		bitsIccFreq := encodeIcc(nil, psd.iccIdx[env][:], nil, psBands, psDeltaFreq, &encErr)

		bitsIccTime := doNotUseThisMode
		if psd.iccTimeCnt < maxTimeDiffFrames {
			bitsIccTime = encodeIcc(nil, psd.iccIdx[env][:], iccIdxLast, psBands, psDeltaTime, &encErr)
		}

		if bitsIccFreq > bitsIccTime {
			psd.iccDiffMode[env] = psDeltaTime
			psd.iccTimeCnt++
		} else {
			psd.iccDiffMode[env] = psDeltaFreq
			psd.iccTimeCnt = 0
		}
		iccIdxLast = psd.iccIdx[env][:]
	}
}

// calculateIID ports calculateIID (ps_encode.cpp:642-660).
func calculateIID(ldPwrL, ldPwrR, iid [][]int32, nEnvelopes, psBands int) {
	for env := 0; env < nEnvelopes; env++ {
		for i := 0; i < psBands; i++ {
			IID := fMultDiv2(fl2fxconstDBL(log102_10/iidScaleFt), ldPwrL[env][i]-ldPwrR[env][i])
			IID = fixMinI32(IID, int32(maxvalDBL>>(ldDataShift+1)))
			IID = fixMaxI32(IID, int32(minvalDBL>>(ldDataShift+1)))
			iid[env][i] = IID << (ldDataShift + 1)
		}
	}
}

// calculateICC ports calculateICC (ps_encode.cpp:662-717).
func calculateICC(pwrL, pwrR, pwrCr, pwrCi, icc [][]int32, nEnvelopes, psBands int) {
	border := psBands
	switch psBands {
	case psBandsCoarse:
		border = 5
	case psBandsMid:
		border = 11
	}

	for env := 0; env < nEnvelopes; env++ {
		i := 0
		for ; i < border; i++ {
			invNrg, scale := invSqrtNorm2PS(fMaxPS(fMult(pwrL[env][i], pwrR[env][i]), int32(1)))
			icc[env][i] = nativeaacSaturateLeftShift(fMult(pwrCr[env][i], invNrg), scale)
		}

		for ; i < psBands; i++ {
			denomM, denomE := fMultNormPS(pwrL[env][i], pwrR[env][i])

			if denomM == int32(0) {
				icc[env][i] = maxvalDBL
			} else {
				numE := countLeadingBitsPS(fixMaxI32(fAbs(pwrCr[env][i]), fAbs(pwrCi[env][i])))
				numM := fPow2Div2PS(pwrCr[env][i]<<uint(numE)) + fPow2Div2PS(pwrCi[env][i]<<uint(numE))

				resultM, resultE := fDivNormPS(numM, denomM)
				resultE += int32(-2*numE+1) - denomE
				icc[env][i] = scaleValueSaturatePS(sqrtFixpPS(resultM>>uint(resultE&1)), (resultE+(resultE&1))>>1)
			}
		}
	}
}

// initPsBandNrgScale ports FDKsbrEnc_initPsBandNrgScale (ps_encode.cpp:719-741).
func initPsBandNrgScale(h *psEncode) {
	nIidGroups := h.nQmfIidGroups + h.nSubQmfIidGroups
	for i := range h.psBandNrgScale {
		h.psBandNrgScale[i] = 0
	}

	for group := 0; group < nIidGroups; group++ {
		bin := int(h.subband2parameterIndex[group])
		if h.psEncMode == psBandsCoarse {
			bin = bin >> 1
		}
		if h.psBandNrgScale[bin] == 0 {
			h.psBandNrgScale[bin] = int8(h.iidGroupWidthLd[group] + 5)
		} else {
			m := int8(h.iidGroupWidthLd[group])
			if h.psBandNrgScale[bin] > m {
				m = h.psBandNrgScale[bin]
			}
			h.psBandNrgScale[bin] = m + 1
		}
	}
}

// createPSEncode ports FDKsbrEnc_CreatePSEncode (ps_encode.cpp:743-759).
func createPSEncode() *psEncode {
	return new(psEncode)
}

// initPSEncode ports FDKsbrEnc_InitPSEncode (ps_encode.cpp:761-799).
func initPSEncode(h *psEncode, psEncMode int, iidQuantErrorThreshold int32) int {
	initPSData(&h.psData)

	switch psEncMode {
	case psBandsCoarse, psBandsMid:
		h.nQmfIidGroups = qmfGroupsLoRes
		h.nSubQmfIidGroups = subqmfGroupsLo
		copy(h.iidGroupBorders[:h.nQmfIidGroups+h.nSubQmfIidGroups+1], iidGroupBordersLoRes[:])
		copy(h.subband2parameterIndex[:h.nQmfIidGroups+h.nSubQmfIidGroups], subband2parameter20[:])
		copy(h.iidGroupWidthLd[:h.nQmfIidGroups+h.nSubQmfIidGroups], iidGroupWidthLdLoRes[:])
	default:
		return psencInitError
	}

	h.psEncMode = psEncMode
	h.iidQuantErrorThreshold = iidQuantErrorThreshold
	initPsBandNrgScale(h)
	return psencOK
}

// psPwrData mirrors PS_PWR_DATA (ps_encode.cpp:811-819).
type psPwrData struct {
	pwrL   [psMaxEnvelopesE][psMaxBandsE]int32
	pwrR   [psMaxEnvelopesE][psMaxBandsE]int32
	ldPwrL [psMaxEnvelopesE][psMaxBandsE]int32
	ldPwrR [psMaxEnvelopesE][psMaxBandsE]int32
	pwrCr  [psMaxEnvelopesE][psMaxBandsE]int32
	pwrCi  [psMaxEnvelopesE][psMaxBandsE]int32
}

// psEncodeRun ports FDKsbrEnc_PSEncode (ps_encode.cpp:821-1031). hybridData is
// indexed [col][ch][reim][band], col in [0, HYBRID_FRAMESIZE).
func psEncodeRun(h *psEncode, out *psOut, dynBandScale []uint8, maxEnvelopes uint,
	hybridData [hybridFramesize][maxPsChannels][2][]int32, frameSize, sendHeader int) int {

	hPsData := &h.psData
	var iidArr [psMaxEnvelopesE][psMaxBandsE]int32
	var iccArr [psMaxEnvelopesE][psMaxBandsE]int32
	var envBorder [psMaxEnvelopesE + 1]int

	psBands := h.psEncMode
	nIidGroups := h.nQmfIidGroups + h.nSubQmfIidGroups
	nEnvelopes := int(maxEnvelopes)
	if nEnvelopes > psMaxEnvelopesE {
		nEnvelopes = psMaxEnvelopesE
	}

	pwrData := new(psPwrData)

	// 2-D row views over the flat arrays for the helper funcs.
	iid := make([][]int32, psMaxEnvelopesE)
	icc := make([][]int32, psMaxEnvelopesE)
	ldPwrL := make([][]int32, psMaxEnvelopesE)
	ldPwrR := make([][]int32, psMaxEnvelopesE)
	pwrL := make([][]int32, psMaxEnvelopesE)
	pwrR := make([][]int32, psMaxEnvelopesE)
	pwrCr := make([][]int32, psMaxEnvelopesE)
	pwrCi := make([][]int32, psMaxEnvelopesE)
	for e := 0; e < psMaxEnvelopesE; e++ {
		iid[e] = iidArr[e][:]
		icc[e] = iccArr[e][:]
		ldPwrL[e] = pwrData.ldPwrL[e][:]
		ldPwrR[e] = pwrData.ldPwrR[e][:]
		pwrL[e] = pwrData.pwrL[e][:]
		pwrR[e] = pwrData.pwrR[e][:]
		pwrCr[e] = pwrData.pwrCr[e][:]
		pwrCi[e] = pwrData.pwrCi[e][:]
	}

	for env := 0; env < nEnvelopes+1; env++ {
		envBorder[env] = int(fMultIPS(getInvIntPS(nEnvelopes), int32(frameSize*env)))
	}

	for env := 0; env < nEnvelopes; env++ {
		for band := 0; band < psBands; band++ {
			pwrData.pwrL[env][band] = 1
			pwrData.pwrR[env][band] = 1
			pwrData.pwrCr[env][band] = 1
			pwrData.pwrCi[env][band] = 1
		}

		for group := 0; group < nIidGroups; group++ {
			bin := int(h.subband2parameterIndex[group])
			if h.psEncMode == psBandsCoarse {
				bin >>= 1
			}

			bScale := int(h.psBandNrgScale[bin])

			pwrLEnvBin := pwrData.pwrL[env][bin]
			pwrREnvBin := pwrData.pwrR[env][bin]
			pwrCrEnvBin := pwrData.pwrCr[env][bin]
			pwrCiEnvBin := pwrData.pwrCi[env][bin]

			scale := int(dynBandScale[bin])
			for col := envBorder[env]; col < envBorder[env+1]; col++ {
				for subband := int(h.iidGroupBorders[group]); subband < int(h.iidGroupBorders[group+1]); subband++ {
					lReal := hybridData[col][0][0][subband] << uint(scale)
					lImag := hybridData[col][0][1][subband] << uint(scale)
					rReal := hybridData[col][1][0][subband] << uint(scale)
					rImag := hybridData[col][1][1][subband] << uint(scale)

					pwrLEnvBin += (fPow2Div2PS(lReal) + fPow2Div2PS(lImag)) >> uint(bScale)
					pwrREnvBin += (fPow2Div2PS(rReal) + fPow2Div2PS(rImag)) >> uint(bScale)
					pwrCrEnvBin += (fMultDiv2(lReal, rReal) + fMultDiv2(lImag, rImag)) >> uint(bScale)
					pwrCiEnvBin += (fMultDiv2(rReal, lImag) - fMultDiv2(lReal, rImag)) >> uint(bScale)
				}
			}
			pwrData.pwrL[env][bin] = fixMaxI32(0, pwrLEnvBin)
			pwrData.pwrR[env][bin] = fixMaxI32(0, pwrREnvBin)
			pwrData.pwrCr[env][bin] = pwrCrEnvBin
			pwrData.pwrCi[env][bin] = pwrCiEnvBin
		}

		ldDataVectorPS(pwrData.pwrL[env][:], pwrData.ldPwrL[env][:], psBands)
		ldDataVectorPS(pwrData.pwrR[env][:], pwrData.ldPwrR[env][:], psBands)
	}

	calculateIID(ldPwrL, ldPwrR, iid, nEnvelopes, psBands)
	calculateICC(pwrL, pwrR, pwrCr, pwrCi, icc, nEnvelopes, psBands)

	for envelopeReducible(iid, icc, psBands, nEnvelopes) != 0 {
		nEnvelopes >>= 1
		for e := 0; e < nEnvelopes; e++ {
			addFIXP_DBL(pwrData.pwrL[2*e][:], pwrData.pwrL[2*e+1][:], pwrData.pwrL[e][:], psBands)
			addFIXP_DBL(pwrData.pwrR[2*e][:], pwrData.pwrR[2*e+1][:], pwrData.pwrR[e][:], psBands)
			addFIXP_DBL(pwrData.pwrCr[2*e][:], pwrData.pwrCr[2*e+1][:], pwrData.pwrCr[e][:], psBands)
			addFIXP_DBL(pwrData.pwrCi[2*e][:], pwrData.pwrCi[2*e+1][:], pwrData.pwrCi[e][:], psBands)

			ldDataVectorPS(pwrData.pwrL[e][:], pwrData.ldPwrL[e][:], psBands)
			ldDataVectorPS(pwrData.pwrR[e][:], pwrData.ldPwrR[e][:], psBands)

			envBorder[e] = envBorder[2*e]
		}
		envBorder[nEnvelopes] = envBorder[2*nEnvelopes]

		calculateIID(ldPwrL, ldPwrR, iid, nEnvelopes, psBands)
		calculateICC(pwrL, pwrR, pwrCr, pwrCi, icc, nEnvelopes, psBands)
	}

	if sendHeader != 0 {
		hPsData.headerCnt = maxPsNoHeaderCnt
		hPsData.iidTimeCnt = maxTimeDiffFrames
		hPsData.iccTimeCnt = maxTimeDiffFrames
		hPsData.noEnvCnt = maxNoEnvCnt
	}

	processIidData(hPsData, iid, psBands, nEnvelopes, h.iidQuantErrorThreshold)
	processIccData(hPsData, icc, psBands, nEnvelopes)

	// PS Header on/off
	if (hPsData.headerCnt < maxPsNoHeaderCnt) &&
		((hPsData.iidQuantMode == hPsData.iidQuantModeLast) && (hPsData.iccQuantMode == hPsData.iccQuantModeLast)) &&
		((hPsData.iidEnable == hPsData.iidEnableLast) && (hPsData.iccEnable == hPsData.iccEnableLast)) {
		out.enablePSHeader = 0
	} else {
		out.enablePSHeader = 1
		hPsData.headerCnt = 0
	}

	// nEnvelopes = 0 ?
	if (hPsData.noEnvCnt < maxNoEnvCnt) &&
		(similarIid(hPsData, psBands, nEnvelopes) != 0) &&
		(similarIcc(hPsData, psBands, nEnvelopes) != 0) {
		nEnvelopes = 0
		out.nEnvelopes = 0
		hPsData.noEnvCnt++
	} else {
		hPsData.noEnvCnt = 0
	}

	if nEnvelopes > 0 {
		out.enableIID = hPsData.iidEnable
		out.iidMode = getIIDMode(psBands, hPsData.iidQuantMode)

		out.enableICC = hPsData.iccEnable
		out.iccMode = getICCMode(psBands, hPsData.iccQuantMode)

		out.enableIpdOpd = 0
		out.frameClass = 0
		out.nEnvelopes = nEnvelopes

		for env := 0; env < nEnvelopes; env++ {
			out.frameBorder[env] = envBorder[env+1]
			out.deltaIID[env] = hPsData.iidDiffMode[env]
			out.deltaICC[env] = hPsData.iccDiffMode[env]
			for band := 0; band < psBands; band++ {
				out.iid[env][band] = hPsData.iidIdx[env][band]
				out.icc[env][band] = hPsData.iccIdx[env][band]
			}
		}

		// IPD/OPD not supported.
		for env := 0; env < psMaxEnvelopesE; env++ {
			for b := 0; b < psMaxBandsE; b++ {
				out.ipd[env][b] = 0
			}
			out.deltaIPD[env] = psDeltaFreq
			out.deltaOPD[env] = psDeltaFreq
		}
		for b := 0; b < psMaxBandsE; b++ {
			out.ipdLast[b] = 0
			out.opdLast[b] = 0
		}

		for band := 0; band < psMaxBandsE; band++ {
			out.iidLast[band] = hPsData.iidIdxLast[band]
			out.iccLast[band] = hPsData.iccIdxLast[band]
		}

		hPsData.nEnvelopesLast = nEnvelopes
		hPsData.iidEnableLast = hPsData.iidEnable
		hPsData.iccEnableLast = hPsData.iccEnable
		hPsData.iidQuantModeLast = hPsData.iidQuantMode
		hPsData.iccQuantModeLast = hPsData.iccQuantMode
		for i := 0; i < psBands; i++ {
			hPsData.iidIdxLast[i] = hPsData.iidIdx[nEnvelopes-1][i]
			hPsData.iccIdxLast[i] = hPsData.iccIdx[nEnvelopes-1][i]
		}
	}

	return psencOK
}

func iAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// nativeaacSaturateLeftShift is the SATURATE_LEFT_SHIFT(value, scale, DFRACT_BITS)
// macro used in calculateICC; scale (from invSqrtNorm2) is non-negative.
func nativeaacSaturateLeftShift(value, scale int32) int32 {
	thresh := maxvalDBL >> uint(scale)
	if value > thresh {
		return maxvalDBL
	}
	if value < ^thresh {
		return minvalDBL
	}
	return value << uint(scale)
}
