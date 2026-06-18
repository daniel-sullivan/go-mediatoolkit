// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Continuation of the fram_gen.cpp 1:1 port (see enc_fram_gen.go): the control
// signal builder calcCtrlSignal, the grid->frame-info converter
// ctrlSignal2FrameInfo, the default FIXFIX frame-info copy createDefFrameInfo,
// the AAC-LD FIXFIXonly generator generateFixFixOnly, and the two public entry
// points InitFrameInfoGenerator / FrameInfoGenerator. Pure-integer; EXACT parity.
package sbr

// calcCtrlSignal is the 1:1 port of calcCtrlSignal (fram_gen.cpp:1513-1677).
func calcCtrlSignal(hSbrGrid *SbrGrid, frameClass FrameClass, vBord []int, lengthVBord int, vFreq []int, lengthVFreq, iCmon, iTran, spreadFlag, nL int) {
	vF := hSbrGrid.VF[:]
	vFLR := hSbrGrid.VFLR[:]
	vR := hSbrGrid.BsRelBord[:]
	vRL := hSbrGrid.BsRelBord0[:]
	vRR := hSbrGrid.BsRelBord1[:]

	lengthVR := 0
	lengthVRR := 0
	lengthVRL := 0

	switch frameClass {
	case Fixvar:
		a := vBord[iCmon]
		lengthVR = 0
		i := iCmon
		for i >= 1 {
			r := vBord[i] - vBord[i-1]
			addRight(vR, &lengthVR, r)
			i--
		}
		n := lengthVR
		for i = 0; i < iCmon; i++ {
			vF[i] = vFreq[iCmon-1-i]
		}
		vF[iCmon] = 1
		p := 0
		if iCmon >= iTran && iTran != emptyConst {
			p = iCmon - iTran + 1
		}
		hSbrGrid.FrameClass = frameClass
		hSbrGrid.BsAbsBord = a
		hSbrGrid.N = n
		hSbrGrid.P = p

	case Varfix:
		a := vBord[0]
		lengthVR = 0
		for i := 1; i < lengthVBord; i++ {
			r := vBord[i] - vBord[i-1]
			addRight(vR, &lengthVR, r)
		}
		n := lengthVR
		copy(vF[:lengthVFreq], vFreq[:lengthVFreq])
		p := 0
		if iTran >= 0 && iTran != emptyConst {
			p = iTran + 1
		}
		hSbrGrid.FrameClass = frameClass
		hSbrGrid.BsAbsBord = a
		hSbrGrid.N = n
		hSbrGrid.P = p

	case Varvar:
		var aL, aR, b, ntot, nmax, nR, p int
		if spreadFlag != 0 {
			b = lengthVBord
			aL = vBord[0]
			aR = vBord[b-1]
			ntot = b - 2
			nmax = 2
			if ntot > nmax {
				nL = nmax
				nR = ntot - nmax
			} else {
				nL = ntot
				nR = 0
			}
			lengthVRL = 0
			for i := 1; i <= nL; i++ {
				r := vBord[i] - vBord[i-1]
				addRight(vRL, &lengthVRL, r)
			}
			lengthVRR = 0
			i := b - 1
			for i >= b-nR {
				r := vBord[i] - vBord[i-1]
				addRight(vRR, &lengthVRR, r)
				i--
			}
			p = 0
			if iTran > 0 && iTran != emptyConst {
				p = b - iTran
			}
			for i := 0; i < b-1; i++ {
				vFLR[i] = vFreq[i]
			}
		} else {
			lengthVBord = iCmon + 1
			b = lengthVBord
			aL = vBord[0]
			aR = vBord[b-1]
			ntot = b - 2
			nR = ntot - nL
			lengthVRL = 0
			for i := 1; i <= nL; i++ {
				r := vBord[i] - vBord[i-1]
				addRight(vRL, &lengthVRL, r)
			}
			lengthVRR = 0
			i := b - 1
			for i >= b-nR {
				r := vBord[i] - vBord[i-1]
				addRight(vRR, &lengthVRR, r)
				i--
			}
			p = 0
			if iCmon >= iTran && iTran != emptyConst {
				p = iCmon - iTran + 1
			}
			for i := 0; i < b-1; i++ {
				vFLR[i] = vFreq[i]
			}
		}
		hSbrGrid.FrameClass = frameClass
		hSbrGrid.BsAbsBord0 = aL
		hSbrGrid.BsAbsBord1 = aR
		hSbrGrid.BsNumRel0 = nL
		hSbrGrid.BsNumRel1 = nR
		hSbrGrid.P = p

	default:
	}
	_ = lengthVRR
	_ = lengthVRL
}

// createDefFrameInfo is the 1:1 port of createDefFrameInfo (fram_gen.cpp:1695-1764):
// copy the default static FIXFIX frame-info for nEnv/nTimeSlots.
func createDefFrameInfo(hSbrFrameInfo *SbrFrameInfo, nEnv, nTimeSlots int) {
	switch nEnv {
	case 1:
		switch nTimeSlots {
		case 15:
			*hSbrFrameInfo = frameInfo1_1920
		case 16:
			*hSbrFrameInfo = frameInfo1_2048
		case 9:
			*hSbrFrameInfo = frameInfo1_1152
		case 18:
			*hSbrFrameInfo = frameInfo1_2304
		case 8:
			*hSbrFrameInfo = frameInfo1_512LD
		}
	case 2:
		switch nTimeSlots {
		case 15:
			*hSbrFrameInfo = frameInfo2_1920
		case 16:
			*hSbrFrameInfo = frameInfo2_2048
		case 9:
			*hSbrFrameInfo = frameInfo2_1152
		case 18:
			*hSbrFrameInfo = frameInfo2_2304
		case 8:
			*hSbrFrameInfo = frameInfo2_512LD
		}
	case 4:
		switch nTimeSlots {
		case 15:
			*hSbrFrameInfo = frameInfo4_1920
		case 16:
			*hSbrFrameInfo = frameInfo4_2048
		case 9:
			*hSbrFrameInfo = frameInfo4_1152
		case 18:
			*hSbrFrameInfo = frameInfo4_2304
		case 8:
			*hSbrFrameInfo = frameInfo4_512LD
		}
	}
}

// ctrlSignal2FrameInfo is the 1:1 port of ctrlSignal2FrameInfo
// (fram_gen.cpp:1784-1965): convert the clear-text SBR_GRID to SBR_FRAME_INFO.
func ctrlSignal2FrameInfo(hSbrGrid *SbrGrid, hSbrFrameInfo *SbrFrameInfo, freqResFixfix []FreqRes) {
	frameSplit := 0
	nEnv := 0
	border := 0
	var k, p int
	vR := hSbrGrid.BsRelBord[:]
	vF := hSbrGrid.VF[:]

	frameClass := hSbrGrid.FrameClass
	bufferFrameStart := hSbrGrid.BufferFrameStart
	numberTimeSlots := hSbrGrid.NumberTimeSlots

	switch frameClass {
	case Fixfix:
		createDefFrameInfo(hSbrFrameInfo, hSbrGrid.BsNumEnv, numberTimeSlots)
		frameSplit = 0
		if hSbrFrameInfo.NEnvelopes > 1 {
			frameSplit = 1
		}
		for i := 0; i < hSbrFrameInfo.NEnvelopes; i++ {
			hSbrFrameInfo.FreqRes[i] = freqResFixfix[frameSplit]
			hSbrGrid.VF[i] = int(freqResFixfix[frameSplit])
		}

	case Fixvar, Varfix:
		nEnv = hSbrGrid.N + 1
		hSbrFrameInfo.NEnvelopes = nEnv
		border = hSbrGrid.BsAbsBord
		if nEnv == 1 {
			hSbrFrameInfo.NNoiseEnvelopes = 1
		} else {
			hSbrFrameInfo.NNoiseEnvelopes = 2
		}
	default:
	}

	switch frameClass {
	case Fixvar:
		hSbrFrameInfo.Borders[0] = bufferFrameStart
		hSbrFrameInfo.Borders[nEnv] = border
		for k, i := 0, nEnv-1; k < nEnv-1; k, i = k+1, i-1 {
			border -= vR[k]
			hSbrFrameInfo.Borders[i] = border
		}
		p = hSbrGrid.P
		if p == 0 {
			hSbrFrameInfo.ShortEnv = 0
		} else {
			hSbrFrameInfo.ShortEnv = nEnv + 1 - p
		}
		for k, i := 0, nEnv-1; k < nEnv; k, i = k+1, i-1 {
			hSbrFrameInfo.FreqRes[i] = FreqRes(vF[k])
		}
		if p == 0 || p == 1 {
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[nEnv-1]
		} else {
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[hSbrFrameInfo.ShortEnv]
		}

	case Varfix:
		hSbrFrameInfo.Borders[0] = border
		for k = 0; k < nEnv-1; k++ {
			border += vR[k]
			hSbrFrameInfo.Borders[k+1] = border
		}
		hSbrFrameInfo.Borders[nEnv] = bufferFrameStart + numberTimeSlots
		p = hSbrGrid.P
		if p == 0 || p == 1 {
			hSbrFrameInfo.ShortEnv = 0
		} else {
			hSbrFrameInfo.ShortEnv = p - 1
		}
		for k = 0; k < nEnv; k++ {
			hSbrFrameInfo.FreqRes[k] = FreqRes(vF[k])
		}
		switch p {
		case 0:
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[1]
		case 1:
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[nEnv-1]
		default:
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[hSbrFrameInfo.ShortEnv]
		}

	case Varvar:
		nEnv = hSbrGrid.BsNumRel0 + hSbrGrid.BsNumRel1 + 1
		hSbrFrameInfo.NEnvelopes = nEnv
		border = hSbrGrid.BsAbsBord0
		hSbrFrameInfo.Borders[0] = border
		for k, i := 0, 1; k < hSbrGrid.BsNumRel0; k, i = k+1, i+1 {
			border += hSbrGrid.BsRelBord0[k]
			hSbrFrameInfo.Borders[i] = border
		}
		border = hSbrGrid.BsAbsBord1
		hSbrFrameInfo.Borders[nEnv] = border
		for k, i := 0, nEnv-1; k < hSbrGrid.BsNumRel1; k, i = k+1, i-1 {
			border -= hSbrGrid.BsRelBord1[k]
			hSbrFrameInfo.Borders[i] = border
		}
		p = hSbrGrid.P
		if p == 0 {
			hSbrFrameInfo.ShortEnv = 0
		} else {
			hSbrFrameInfo.ShortEnv = nEnv + 1 - p
		}
		for k = 0; k < nEnv; k++ {
			hSbrFrameInfo.FreqRes[k] = FreqRes(hSbrGrid.VFLR[k])
		}
		if nEnv == 1 {
			hSbrFrameInfo.NNoiseEnvelopes = 1
			hSbrFrameInfo.BordersNoise[0] = hSbrGrid.BsAbsBord0
			hSbrFrameInfo.BordersNoise[1] = hSbrGrid.BsAbsBord1
		} else {
			hSbrFrameInfo.NNoiseEnvelopes = 2
			hSbrFrameInfo.BordersNoise[0] = hSbrGrid.BsAbsBord0
			if p == 0 || p == 1 {
				hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[nEnv-1]
			} else {
				hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[hSbrFrameInfo.ShortEnv]
			}
			hSbrFrameInfo.BordersNoise[2] = hSbrGrid.BsAbsBord1
		}
	default:
	}

	if frameClass == Varfix || frameClass == Fixvar {
		hSbrFrameInfo.BordersNoise[0] = hSbrFrameInfo.Borders[0]
		if nEnv == 1 {
			hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[nEnv]
		} else {
			hSbrFrameInfo.BordersNoise[2] = hSbrFrameInfo.Borders[nEnv]
		}
	}
}

// generateFixFixOnly is the 1:1 port of generateFixFixOnly (fram_gen.cpp:661-719):
// the AAC-LD FIXFIXonly grid (ldGrid). Out of HE-AAC v1 STD scope but ported for
// completeness; only reached when ldGrid != 0.
func generateFixFixOnly(hSbrFrameInfo *SbrFrameInfo, hSbrGrid *SbrGrid, tranPosInternal, numberTimeSlots int, fResTransIsLow uint8) {
	var pTable []int
	var freqResTable []FreqRes

	switch numberTimeSlots {
	case 8:
		pTable = envelopeTable8[tranPosInternal][:]
		freqResTable = freqResTable8
	case 15:
		pTable = envelopeTable15[tranPosInternal][:]
		freqResTable = freqResTable16[:]
	case 16:
		pTable = envelopeTable16[tranPosInternal][:]
		freqResTable = freqResTable16[:]
	}

	nEnv := pTable[0]
	for i := 1; i < nEnv; i++ {
		hSbrFrameInfo.Borders[i] = pTable[i+2]
	}
	hSbrFrameInfo.Borders[0] = 0
	hSbrFrameInfo.Borders[nEnv] = numberTimeSlots

	for i := 0; i < nEnv; i++ {
		k := hSbrFrameInfo.Borders[i+1] - hSbrFrameInfo.Borders[i]
		if fResTransIsLow == 0 {
			hSbrFrameInfo.FreqRes[i] = freqResTable[k]
		} else {
			hSbrFrameInfo.FreqRes[i] = FreqResLow
		}
		hSbrGrid.VF[i] = int(hSbrFrameInfo.FreqRes[i])
	}

	hSbrFrameInfo.NEnvelopes = nEnv
	hSbrFrameInfo.ShortEnv = pTable[2]
	tranIdx := pTable[1]

	hSbrFrameInfo.BordersNoise[0] = 0
	if tranIdx != 0 {
		hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[tranIdx]
	} else {
		hSbrFrameInfo.BordersNoise[1] = hSbrFrameInfo.Borders[1]
	}
	hSbrFrameInfo.BordersNoise[2] = numberTimeSlots
	hSbrFrameInfo.NNoiseEnvelopes = 2

	hSbrGrid.FrameClass = Fixfixonly
	hSbrGrid.BsAbsBord = tranPosInternal
	hSbrGrid.BsNumEnv = nEnv
}

// InitFrameInfoGenerator is the 1:1 port of FDKsbrEnc_initFrameInfoGenerator
// (fram_gen.cpp:735-801).
func InitFrameInfoGenerator(h *SbrEnvelopeFrame, allowSpread, numEnvStatic, staticFraming, timeSlots int, freqResFixfix []FreqRes, fResTransIsLow uint8, ldGrid int) {
	*h = SbrEnvelopeFrame{}

	h.FrameClassOld = Fixfix
	h.SpreadFlag = 0
	h.AllowSpread = allowSpread
	h.NumEnvStatic = numEnvStatic
	h.StaticFraming = staticFraming
	h.FreqResFixfix[0] = freqResFixfix[0]
	h.FreqResFixfix[1] = freqResFixfix[1]
	h.FResTransIsLow = fResTransIsLow

	h.LengthVBord = 0
	h.LengthVBordFollow = 0
	h.LengthVFreq = 0
	h.LengthVFreqFollow = 0
	h.ITranFollow = 0
	h.IFillFollow = 0

	h.SbrGrid.NumberTimeSlots = timeSlots

	if ldGrid != 0 {
		h.Dmin = 2
		h.Dmax = 16
		h.FrameMiddleSlot = 4 // FRAME_MIDDLE_SLOT_512LD
		h.SbrGrid.BufferFrameStart = 0
	} else {
		switch timeSlots {
		case 15: // NUMBER_TIME_SLOTS_1920
			h.Dmin = 4
			h.Dmax = 12
			h.SbrGrid.BufferFrameStart = 0
			h.FrameMiddleSlot = 4 // FRAME_MIDDLE_SLOT_1920
		case 16: // NUMBER_TIME_SLOTS_2048
			h.Dmin = 4
			h.Dmax = 12
			h.SbrGrid.BufferFrameStart = 0
			h.FrameMiddleSlot = 4 // FRAME_MIDDLE_SLOT_2048
		case 9: // NUMBER_TIME_SLOTS_1152
			h.Dmin = 2
			h.Dmax = 8
			h.SbrGrid.BufferFrameStart = 0
			h.FrameMiddleSlot = 4 // FRAME_MIDDLE_SLOT_1152
		case 18: // NUMBER_TIME_SLOTS_2304
			h.Dmin = 4
			h.Dmax = 15
			h.SbrGrid.BufferFrameStart = 0
			h.FrameMiddleSlot = 8 // FRAME_MIDDLE_SLOT_2304
		}
	}
}

// FrameInfoGenerator is the 1:1 port of FDKsbrEnc_frameInfoGenerator
// (fram_gen.cpp:336-652). It produces the SBR_FRAME_INFO (and fills SbrGrid) for
// the current frame from the transient info, returning a pointer to the frame's
// SbrFrameInfo (== &h.SbrFrameInfo, as the C returns).
func FrameInfoGenerator(h *SbrEnvelopeFrame, vTransientInfo []uint8, rightBorderFIX int, vTransientInfoPre []uint8, ldGrid int, vTuning []int) *SbrFrameInfo {
	numEnv := 0
	tranPosInternal := 0
	bmin := 0
	bmax := 0
	parts := 0
	d := 0
	iCmon := 0
	iTran := 0
	nL := 0
	fmax := 0

	vBord := h.VBord[:]
	vFreq := h.VFreq[:]
	vBordFollow := h.VBordFollow[:]
	vFreqFollow := h.VFreqFollow[:]

	lengthVBordFollow := &h.LengthVBordFollow
	lengthVFreqFollow := &h.LengthVFreqFollow
	lengthVBord := &h.LengthVBord
	lengthVFreq := &h.LengthVFreq
	spreadFlag := &h.SpreadFlag
	iTranFollow := &h.ITranFollow
	iFillFollow := &h.IFillFollow
	frameClassOld := &h.FrameClassOld
	frameClass := Fixfix

	allowSpread := h.AllowSpread
	numEnvStatic := h.NumEnvStatic
	staticFraming := h.StaticFraming
	dmin := h.Dmin
	dmax := h.Dmax

	bufferFrameStart := h.SbrGrid.BufferFrameStart
	numberTimeSlots := h.SbrGrid.NumberTimeSlots
	frameMiddleSlot := h.FrameMiddleSlot

	tranPos := int(vTransientInfo[0])
	tranFlag := int(vTransientInfo[1])

	vTuningSegm := vTuning[0:]
	vTuningFreq := vTuning[3:]
	h.VTuningSegm = vTuningSegm

	if ldGrid != 0 {
		if tranFlag == 0 && vTransientInfoPre[1] != 0 && (numberTimeSlots-int(vTransientInfoPre[0]) < minFrameTranDistance) {
			tranFlag = 1
			tranPos = 0
		}
	}

	if staticFraming != 0 {
		frameClass = Fixfix
		numEnv = numEnvStatic
		*frameClassOld = Fixfix
		h.SbrGrid.BsNumEnv = numEnv
		h.SbrGrid.FrameClass = frameClass
	} else {
		if rightBorderFIX != 0 {
			tranFlag = 0
			*spreadFlag = 0
		}
		calcFrameClass(&frameClass, frameClassOld, tranFlag, spreadFlag)

		if tranFlag != 0 && ldGrid != 0 {
			frameClass = Fixfixonly
			*frameClassOld = Fixfix
		}

		if tranFlag != 0 {
			tranPosInternal = frameMiddleSlot + tranPos + bufferFrameStart
			fillFrameTran(vTuningSegm, vTuningFreq, tranPosInternal, vBord, lengthVBord, vFreq, lengthVFreq, &bmin, &bmax)
			fmax = calcFillLengthMax(tranPos, numberTimeSlots)
		}

		switch frameClass {
		case Fixfixonly:
			tranPosInternal = tranPos
			generateFixFixOnly(&h.SbrFrameInfo, &h.SbrGrid, tranPosInternal, numberTimeSlots, h.FResTransIsLow)
			return &h.SbrFrameInfo

		case Fixvar:
			fillFramePre(dmax, vBord, lengthVBord, vFreq, lengthVFreq, bmin, bmin-bufferFrameStart)
			fillFramePost(&parts, &d, dmax, vBord, lengthVBord, vFreq, lengthVFreq, bmax, bufferFrameStart, numberTimeSlots, fmax)
			if parts == 1 && d < dmin {
				specialCase(spreadFlag, allowSpread, vBord, lengthVBord, vFreq, lengthVFreq, &parts, d)
			}
			calcCmonBorder(&iCmon, &iTran, vBord, lengthVBord, tranPosInternal, bufferFrameStart, numberTimeSlots)
			keepForFollowUp(vBordFollow, lengthVBordFollow, vFreqFollow, lengthVFreqFollow, iTranFollow, iFillFollow, vBord, lengthVBord, vFreq, iCmon, iTran, parts, numberTimeSlots)
			calcCtrlSignal(&h.SbrGrid, frameClass, vBord, *lengthVBord, vFreq, *lengthVFreq, iCmon, iTran, *spreadFlag, dcConst)

		case Varfix:
			calcCtrlSignal(&h.SbrGrid, frameClass, vBordFollow, *lengthVBordFollow, vFreqFollow, *lengthVFreqFollow, dcConst, *iTranFollow, *spreadFlag, dcConst)

		case Varvar:
			if *spreadFlag != 0 {
				calcCtrlSignal(&h.SbrGrid, frameClass, vBordFollow, *lengthVBordFollow, vFreqFollow, *lengthVFreqFollow, dcConst, *iTranFollow, *spreadFlag, dcConst)
				*spreadFlag = 0
				vBordFollow[0] = h.SbrGrid.BsAbsBord1 - numberTimeSlots
				vFreqFollow[0] = 1
				*lengthVBordFollow = 1
				*lengthVFreqFollow = 1
				*iTranFollow = -dcConst
				*iFillFollow = -dcConst
			} else {
				fillFrameInter(&nL, vTuningSegm, vBord, lengthVBord, bmin, vFreq, lengthVFreq, vBordFollow, lengthVBordFollow, vFreqFollow, lengthVFreqFollow, *iFillFollow, dmin, dmax, numberTimeSlots)
				fillFramePost(&parts, &d, dmax, vBord, lengthVBord, vFreq, lengthVFreq, bmax, bufferFrameStart, numberTimeSlots, fmax)
				if parts == 1 && d < dmin {
					specialCase(spreadFlag, allowSpread, vBord, lengthVBord, vFreq, lengthVFreq, &parts, d)
				}
				calcCmonBorder(&iCmon, &iTran, vBord, lengthVBord, tranPosInternal, bufferFrameStart, numberTimeSlots)
				keepForFollowUp(vBordFollow, lengthVBordFollow, vFreqFollow, lengthVFreqFollow, iTranFollow, iFillFollow, vBord, lengthVBord, vFreq, iCmon, iTran, parts, numberTimeSlots)
				calcCtrlSignal(&h.SbrGrid, frameClass, vBord, *lengthVBord, vFreq, *lengthVFreq, iCmon, iTran, 0, nL)
			}

		case Fixfix:
			if tranPos == 0 {
				numEnv = 1
			} else {
				numEnv = 2
			}
			h.SbrGrid.BsNumEnv = numEnv
			h.SbrGrid.FrameClass = frameClass
		}
	}

	ctrlSignal2FrameInfo(&h.SbrGrid, &h.SbrFrameInfo, h.FreqResFixfix[:])
	return &h.SbrFrameInfo
}
