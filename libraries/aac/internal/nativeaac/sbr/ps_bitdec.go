// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Parametric-stereo bitstream decode, ported 1:1 from the vendored Fraunhofer
// FDK-AAC psbitdec.cpp: ReadPsData (the PS payload parse — header, frame
// borders, IID/ICC Huffman + delta coding), DecodePs (the per-envelope delta
// decode, FIX/VAR border handling, prev-frame update, and the 34<->20 stereo
// band mapping), and the deltaDecodeArray / map34IndexTo20 helpers. The IPD/OPD
// extension is parsed for bit accounting but discarded (baseline PS). The DRM
// (psdec_drm.cpp) PS path is EXCLUDED.

// psLimitMinMax clamps i to [min,max].
//
// C: limitMinMax (psbitdec.cpp:146-153).
func psLimitMinMax(i, minV, maxV int8) int8 {
	if i < minV {
		return minV
	}
	if i > maxV {
		return maxV
	}
	return i
}

// deltaDecodeArray decodes delta-coded ICC/IID indices in place and expands the
// half-resolution (stride==2) case. When delta coded in freq the first element
// is delta from zero; in time it is delta from the previous frame's index.
//
// C: deltaDecodeArray (psbitdec.cpp:166-200).
func deltaDecodeArray(enable int8, aIndex []int8, aPrevFrameIndex []int8, dtDf int8, nrElements, stride uint8, minIdx, maxIdx int8) {
	if enable == 1 {
		if dtDf == 0 { // delta coded in freq
			aIndex[0] = 0 + aIndex[0]
			aIndex[0] = psLimitMinMax(aIndex[0], minIdx, maxIdx)
			for i := 1; i < int(nrElements); i++ {
				aIndex[i] = aIndex[i-1] + aIndex[i]
				aIndex[i] = psLimitMinMax(aIndex[i], minIdx, maxIdx)
			}
		} else { // delta time
			for i := 0; i < int(nrElements); i++ {
				aIndex[i] = aPrevFrameIndex[i*int(stride)] + aIndex[i]
				aIndex[i] = psLimitMinMax(aIndex[i], minIdx, maxIdx)
			}
		}
	} else { // no data sent, set index to zero
		for i := 0; i < int(nrElements); i++ {
			aIndex[i] = 0
		}
	}
	if stride == 2 {
		for i := int(nrElements)*int(stride) - 1; i > 0; i-- {
			aIndex[i] = aIndex[i>>1]
		}
	}
}

// map34IndexTo20 maps 34-band ICC/IID parameters down to 20 stereo bands in
// place.
//
// C: map34IndexTo20 (psbitdec.cpp:209-236).
func map34IndexTo20(aIndex []int8, noBins uint8) {
	aIndex[0] = (2*aIndex[0] + aIndex[1]) / 3
	aIndex[1] = (aIndex[1] + 2*aIndex[2]) / 3
	aIndex[2] = (2*aIndex[3] + aIndex[4]) / 3
	aIndex[3] = (aIndex[4] + 2*aIndex[5]) / 3
	aIndex[4] = (aIndex[6] + aIndex[7]) / 2
	aIndex[5] = (aIndex[8] + aIndex[9]) / 2
	aIndex[6] = aIndex[10]
	aIndex[7] = aIndex[11]
	aIndex[8] = (aIndex[12] + aIndex[13]) / 2
	aIndex[9] = (aIndex[14] + aIndex[15]) / 2
	aIndex[10] = aIndex[16]
	// For IPD/OPD it stops here.

	if noBins == psNoHiResBins {
		aIndex[11] = aIndex[17]
		aIndex[12] = aIndex[18]
		aIndex[13] = aIndex[19]
		aIndex[14] = (aIndex[20] + aIndex[21]) / 2
		aIndex[15] = (aIndex[22] + aIndex[23]) / 2
		aIndex[16] = (aIndex[24] + aIndex[25]) / 2
		aIndex[17] = (aIndex[26] + aIndex[27]) / 2
		aIndex[18] = (aIndex[28] + aIndex[29] + aIndex[30] + aIndex[31]) / 4
		aIndex[19] = (aIndex[32] + aIndex[33]) / 2
	}
}

// decodePs decodes the delta-coded IID/ICC indices for the current frame,
// resolves the FIX/VAR envelope borders, updates the prev-frame state, copies
// the indices into the scratch coefficient struct, and applies the 34->20 stereo
// band mapping when a 34-band config was used. Returns the PS processing flag
// (1 = apply PS, 0 = conceal / skip).
//
// C: DecodePs (psbitdec.cpp:245-439).
func decodePs(h *psDec, frameError uint8, pScratch *psDecCoefficients) int {
	h.Mpeg.PCoef = pScratch

	pBsData := &h.BsData[h.ProcessSlot]
	bPsHeaderValid := pBsData.BPsHeaderValid
	bPsDataAvail := 0
	if h.BPsDataAvail[h.ProcessSlot] == pptMpeg {
		bPsDataAvail = 1
	}

	// Decide whether to process or conceal PS (psbitdec.cpp:264-272).
	if (h.PsDecodedPrv != 0 && frameError == 0 && bPsDataAvail == 0) ||
		(h.PsDecodedPrv == 0 && (frameError != 0 || bPsDataAvail == 0 || bPsHeaderValid == 0)) {
		pBsData.BPsHeaderValid = 0
		h.BPsDataAvail[h.ProcessSlot] = pptNone
		return 0
	}

	if frameError != 0 || bPsHeaderValid == 0 {
		// No new PS data — keep latest data constant (FIX with noEnv=0).
		pBsData.NoEnv = 0
	}

	// Decode bitstream payload or prepare for concealment (psbitdec.cpp:283-306).
	for env := 0; env < int(pBsData.NoEnv); env++ {
		var aPrevIidIndex, aPrevIccIndex []int8

		noIidSteps := int8(psNoIidSteps)
		if pBsData.BFineIidQ != 0 {
			noIidSteps = psNoIidStepsFine
		}

		if env == 0 {
			aPrevIidIndex = h.Mpeg.AIidPrevFrameIndex[:]
			aPrevIccIndex = h.Mpeg.AIccPrevFrameIndex[:]
		} else {
			aPrevIidIndex = pBsData.AaIidIndex[env-1][:]
			aPrevIccIndex = pBsData.AaIccIndex[env-1][:]
		}

		iidStride := uint8(2)
		if pBsData.FreqResIid != 0 {
			iidStride = 1
		}
		deltaDecodeArray(int8(pBsData.BEnableIid), pBsData.AaIidIndex[env][:],
			aPrevIidIndex, pBsData.AbIidDtFlag[env],
			psNoIidBins[pBsData.FreqResIid], iidStride, -noIidSteps, noIidSteps)

		iccStride := uint8(2)
		if pBsData.FreqResIcc != 0 {
			iccStride = 1
		}
		deltaDecodeArray(int8(pBsData.BEnableIcc), pBsData.AaIccIndex[env][:],
			aPrevIccIndex, pBsData.AbIccDtFlag[env],
			psNoIccBins[pBsData.FreqResIcc], iccStride, 0, psNoIccSteps-1)
	}

	// handling of FIX noEnv=0 (psbitdec.cpp:309-337).
	if pBsData.NoEnv == 0 {
		pBsData.NoEnv = 1

		if pBsData.BEnableIid != 0 {
			pBsData.BFineIidQ = h.Mpeg.BPrevFrameFineIidQ
			pBsData.FreqResIid = h.Mpeg.PrevFreqResIid
			for gr := 0; gr < psNoHiResIidBins; gr++ {
				pBsData.AaIidIndex[pBsData.NoEnv-1][gr] = h.Mpeg.AIidPrevFrameIndex[gr]
			}
		} else {
			for gr := 0; gr < psNoHiResIidBins; gr++ {
				pBsData.AaIidIndex[pBsData.NoEnv-1][gr] = 0
			}
		}

		if pBsData.BEnableIcc != 0 {
			pBsData.FreqResIcc = h.Mpeg.PrevFreqResIcc
			for gr := 0; gr < psNoHiResIccBins; gr++ {
				pBsData.AaIccIndex[pBsData.NoEnv-1][gr] = h.Mpeg.AIccPrevFrameIndex[gr]
			}
		} else {
			for gr := 0; gr < psNoHiResIccBins; gr++ {
				pBsData.AaIccIndex[pBsData.NoEnv-1][gr] = 0
			}
		}
	}

	// Update prev-frame state (psbitdec.cpp:339-356).
	h.Mpeg.BPrevFrameFineIidQ = pBsData.BFineIidQ
	h.Mpeg.PrevFreqResIid = pBsData.FreqResIid
	h.Mpeg.PrevFreqResIcc = pBsData.FreqResIcc
	for gr := 0; gr < psNoHiResIidBins; gr++ {
		h.Mpeg.AIidPrevFrameIndex[gr] = pBsData.AaIidIndex[pBsData.NoEnv-1][gr]
	}
	for gr := 0; gr < psNoHiResIccBins; gr++ {
		h.Mpeg.AIccPrevFrameIndex[gr] = pBsData.AaIccIndex[pBsData.NoEnv-1][gr]
	}

	h.BPsDataAvail[h.ProcessSlot] = pptNone

	// handling of env borders for FIX & VAR (psbitdec.cpp:362-404).
	if pBsData.BFrameClass == 0 {
		// FIX_BORDERS NoEnv=0,1,2,4.
		pBsData.AEnvStartStop[0] = 0
		for env := 1; env < int(pBsData.NoEnv); env++ {
			pBsData.AEnvStartStop[env] = uint8((env * int(h.NoSubSamples)) / int(pBsData.NoEnv))
		}
		pBsData.AEnvStartStop[pBsData.NoEnv] = uint8(h.NoSubSamples)
	} else {
		// VAR_BORDERS NoEnv=1,2,3,4.
		pBsData.AEnvStartStop[0] = 0

		if int(pBsData.AEnvStartStop[pBsData.NoEnv]) < int(h.NoSubSamples) {
			for gr := 0; gr < psNoHiResIidBins; gr++ {
				pBsData.AaIidIndex[pBsData.NoEnv][gr] = pBsData.AaIidIndex[pBsData.NoEnv-1][gr]
			}
			for gr := 0; gr < psNoHiResIccBins; gr++ {
				pBsData.AaIccIndex[pBsData.NoEnv][gr] = pBsData.AaIccIndex[pBsData.NoEnv-1][gr]
			}
			pBsData.NoEnv++
			pBsData.AEnvStartStop[pBsData.NoEnv] = uint8(h.NoSubSamples)
		}

		// enforce strictly monotonic increasing borders.
		for env := 1; env < int(pBsData.NoEnv); env++ {
			thr := uint8(int(h.NoSubSamples) - (int(pBsData.NoEnv) - env))
			if pBsData.AEnvStartStop[env] > thr {
				pBsData.AEnvStartStop[env] = thr
			} else {
				thr = pBsData.AEnvStartStop[env-1] + 1
				if pBsData.AEnvStartStop[env] < thr {
					pBsData.AEnvStartStop[env] = thr
				}
			}
		}
	}

	// copy data prior to possible 20<->34 in-place mapping (psbitdec.cpp:407-417).
	for env := 0; env < int(pBsData.NoEnv); env++ {
		for i := 0; i < psNoHiResIidBins; i++ {
			h.Mpeg.PCoef.AaIidIndexMapped[env][i] = pBsData.AaIidIndex[env][i]
		}
		for i := 0; i < psNoHiResIccBins; i++ {
			h.Mpeg.PCoef.AaIccIndexMapped[env][i] = pBsData.AaIccIndex[env][i]
		}
	}

	// MPEG baseline PS: 34-band IID/ICC mapped to 20 bands; IPD/OPD dropped.
	for env := 0; env < int(pBsData.NoEnv); env++ {
		if pBsData.FreqResIid == 2 {
			map34IndexTo20(h.Mpeg.PCoef.AaIidIndexMapped[env][:], psNoHiResIidBins)
		}
		if pBsData.FreqResIcc == 2 {
			map34IndexTo20(h.Mpeg.PCoef.AaIccIndexMapped[env][:], psNoHiResIccBins)
		}
	}

	return 1
}

// readPsData reads the PS payload from the bitstream into the current read slot,
// returning the number of bits consumed. Mirrors the FDK delay-line bookkeeping:
// when bsReadSlot != bsLastSlot the previous header is copied forward first.
//
// C: ReadPsData (psbitdec.cpp:449-594).
func readPsData(h *psDec, hBitBuf *bitStream, nBitsLeft int) uint {
	if h == nil {
		return 0
	}

	pBsData := &h.BsData[h.BsReadSlot]

	if h.BsReadSlot != h.BsLastSlot {
		// Copy last header data.
		*pBsData = h.BsData[h.BsLastSlot]
	}

	// startbits / all the bit-accounting deltas are INT (int32) in the C, and the
	// return is (UINT)(startbits - (INT)FDKgetValidBits()). FDKgetValidBits can
	// underflow to a large UINT; (INT) reinterprets it as a negative int32, so the
	// subtraction must be done in int32 width (NOT Go's 64-bit int) to match.
	startbits := int32(hBitBuf.getValidBits())

	bEnableHeader := int8(hBitBuf.readBits(1))

	if bEnableHeader != 0 {
		pBsData.BPsHeaderValid = 1
		pBsData.BEnableIid = uint8(hBitBuf.readBits(1))
		if pBsData.BEnableIid != 0 {
			pBsData.ModeIid = uint8(hBitBuf.readBits(3))
		}
		pBsData.BEnableIcc = uint8(hBitBuf.readBits(1))
		if pBsData.BEnableIcc != 0 {
			pBsData.ModeIcc = uint8(hBitBuf.readBits(3))
		}
		pBsData.BEnableExt = uint8(hBitBuf.readBits(1))
	}

	pBsData.BFrameClass = uint8(hBitBuf.readBits(1))
	if pBsData.BFrameClass == 0 {
		// FIX_BORDERS NoEnv=0,1,2,4.
		pBsData.NoEnv = psFixNoEnvDecode[hBitBuf.readBits(2)]
	} else {
		// VAR_BORDERS NoEnv=1,2,3,4.
		pBsData.NoEnv = 1 + uint8(hBitBuf.readBits(2))
		for env := 1; env < int(pBsData.NoEnv)+1; env++ {
			pBsData.AEnvStartStop[env] = uint8(hBitBuf.readBits(5)) + 1
		}
	}

	// verify IID & ICC modes are supported.
	if pBsData.ModeIid > 5 || pBsData.ModeIcc > 5 {
		h.BPsDataAvail[h.BsReadSlot] = pptNone
		nbl := int32(nBitsLeft) - (startbits - int32(hBitBuf.getValidBits()))
		for nbl > 0 {
			i := nbl
			if i > 8 {
				i = 8
			}
			hBitBuf.readBits(uint32(i))
			nbl -= i
		}
		return uint(uint32(startbits - int32(hBitBuf.getValidBits())))
	}

	if pBsData.ModeIid > 2 {
		pBsData.FreqResIid = pBsData.ModeIid - 3
		pBsData.BFineIidQ = 1
	} else {
		pBsData.FreqResIid = pBsData.ModeIid
		pBsData.BFineIidQ = 0
	}

	if pBsData.ModeIcc > 2 {
		pBsData.FreqResIcc = pBsData.ModeIcc - 3
	} else {
		pBsData.FreqResIcc = pBsData.ModeIcc
	}

	// Extract IID data.
	if pBsData.BEnableIid != 0 {
		for env := 0; env < int(pBsData.NoEnv); env++ {
			dtFlag := int8(hBitBuf.readBits(1))
			var currentTable huffman
			if dtFlag == 0 {
				if pBsData.BFineIidQ != 0 {
					currentTable = aBookPsIidFineFreqDecode
				} else {
					currentTable = aBookPsIidFreqDecode
				}
			} else {
				if pBsData.BFineIidQ != 0 {
					currentTable = aBookPsIidFineTimeDecode
				} else {
					currentTable = aBookPsIidTimeDecode
				}
			}
			for gr := 0; gr < int(psNoIidBins[pBsData.FreqResIid]); gr++ {
				pBsData.AaIidIndex[env][gr] = int8(decodeHuffmanCW(currentTable, hBitBuf))
			}
			pBsData.AbIidDtFlag[env] = dtFlag
		}
	}

	// Extract ICC data.
	if pBsData.BEnableIcc != 0 {
		for env := 0; env < int(pBsData.NoEnv); env++ {
			dtFlag := int8(hBitBuf.readBits(1))
			var currentTable huffman
			if dtFlag == 0 {
				currentTable = aBookPsIccFreqDecode
			} else {
				currentTable = aBookPsIccTimeDecode
			}
			for gr := 0; gr < int(psNoIccBins[pBsData.FreqResIcc]); gr++ {
				pBsData.AaIccIndex[env][gr] = int8(decodeHuffmanCW(currentTable, hBitBuf))
			}
			pBsData.AbIccDtFlag[env] = dtFlag
		}
	}

	if pBsData.BEnableExt != 0 {
		// Baseline decoders ignore IPD/OPD but must parse the header bytes.
		cnt := int(hBitBuf.readBits(psExtensionSizeBits))
		if cnt == (1<<psExtensionSizeBits)-1 {
			cnt += int(hBitBuf.readBits(psExtensionEscCountBits))
		}
		for cnt > 0 {
			hBitBuf.readBits(8)
			cnt--
		}
	}

	h.BPsDataAvail[h.BsReadSlot] = pptMpeg

	return uint(uint32(startbits - int32(hBitBuf.getValidBits())))
}
