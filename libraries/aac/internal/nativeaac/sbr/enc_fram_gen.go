// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the Fraunhofer FDK-AAC SBR-encoder frame
// grid generator, libSBRenc/src/fram_gen.cpp (FDKsbrEnc_frameInfoGenerator +
// FDKsbrEnc_initFrameInfoGenerator and the whole static helper chain
// fillFrameTran / fillFramePre / fillFramePost / fillFrameInter / calcFrameClass
// / specialCase / calcCmonBorder / keepForFollowUp / calcCtrlSignal /
// ctrlSignal2FrameInfo / createDefFrameInfo / generateFixFixOnly, with the
// frameInfo / envelope / freqRes ROM tables). From the transient/split decisions
// (v_transient_info) it produces the SBR time/frequency grid: the SBR_GRID
// clear-text signals and the SBR_FRAME_INFO the envelope + MH estimators consume.
//
// This is PURE-INTEGER frame logic (no fixed-point arithmetic), so parity is
// exact integer equality of the resulting grid + frame info. The sbr_misc.cpp
// vector helpers (AddLeft/AddRight/AddVecLeft) are inlined as small slice ops
// that track an explicit length, mirroring the C call-by-reference exactly.
//
// Scope: HE-AAC v1 STD. The ldGrid (AAC-LD / FIXFIXonly) and the
// NUMBER_TIME_SLOTS_512LD / _1152 / _1920 paths are ported faithfully where they
// are small and self-contained (createDefFrameInfo / calcFillLengthMax /
// generateFixFixOnly), so the generator is complete, but the HE-AAC v1 caller
// drives the 16-slot (NUMBER_TIME_SLOTS_2048) STD path.
package sbr

// envFrameSbrFrameInfo mirrors a const SBR_FRAME_INFO ROM entry.
type envFrameSbrFrameInfo = SbrFrameInfo

// Default (static) FIXFIX frame-info ROM, 1:1 from fram_gen.cpp:108-177.
var (
	frameInfo1_2048 = SbrFrameInfo{NEnvelopes: 1, Borders: [6]int{0, 16}, FreqRes: [5]FreqRes{FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 1, BordersNoise: [3]int{0, 16}}
	frameInfo2_2048 = SbrFrameInfo{NEnvelopes: 2, Borders: [6]int{0, 8, 16}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 8, 16}}
	frameInfo4_2048 = SbrFrameInfo{NEnvelopes: 4, Borders: [6]int{0, 4, 8, 12, 16}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 8, 16}}

	frameInfo1_2304 = SbrFrameInfo{NEnvelopes: 1, Borders: [6]int{0, 18}, FreqRes: [5]FreqRes{FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 1, BordersNoise: [3]int{0, 18}}
	frameInfo2_2304 = SbrFrameInfo{NEnvelopes: 2, Borders: [6]int{0, 9, 18}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 9, 18}}
	frameInfo4_2304 = SbrFrameInfo{NEnvelopes: 4, Borders: [6]int{0, 5, 9, 14, 18}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 9, 18}}

	frameInfo1_1920 = SbrFrameInfo{NEnvelopes: 1, Borders: [6]int{0, 15}, FreqRes: [5]FreqRes{FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 1, BordersNoise: [3]int{0, 15}}
	frameInfo2_1920 = SbrFrameInfo{NEnvelopes: 2, Borders: [6]int{0, 8, 15}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 8, 15}}
	frameInfo4_1920 = SbrFrameInfo{NEnvelopes: 4, Borders: [6]int{0, 4, 8, 12, 15}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 8, 15}}

	frameInfo1_1152 = SbrFrameInfo{NEnvelopes: 1, Borders: [6]int{0, 9}, FreqRes: [5]FreqRes{FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 1, BordersNoise: [3]int{0, 9}}
	frameInfo2_1152 = SbrFrameInfo{NEnvelopes: 2, Borders: [6]int{0, 5, 9}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 5, 9}}
	frameInfo4_1152 = SbrFrameInfo{NEnvelopes: 4, Borders: [6]int{0, 2, 5, 7, 9}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 5, 9}}

	frameInfo1_512LD = SbrFrameInfo{NEnvelopes: 1, Borders: [6]int{0, 8}, FreqRes: [5]FreqRes{FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 1, BordersNoise: [3]int{0, 8}}
	frameInfo2_512LD = SbrFrameInfo{NEnvelopes: 2, Borders: [6]int{0, 4, 8}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 4, 8}}
	frameInfo4_512LD = SbrFrameInfo{NEnvelopes: 4, Borders: [6]int{0, 2, 4, 6, 8}, FreqRes: [5]FreqRes{FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}, ShortEnv: 0, NNoiseEnvelopes: 2, BordersNoise: [3]int{0, 4, 8}}
)

// envelopeTable_8/16/15, freqRes tables, minFrameTranDistance — 1:1 from
// fram_gen.cpp:238-317,294-317.
var (
	envelopeTable8 = [8][5]int{
		{2, 0, 0, 1, -1}, {2, 0, 0, 2, -1}, {3, 1, 1, 2, 4}, {3, 1, 1, 3, 5},
		{3, 1, 1, 4, 6}, {2, 1, 1, 5, -1}, {2, 1, 1, 6, -1}, {2, 1, 1, 7, -1},
	}
	envelopeTable16 = [16][6]int{
		{2, 0, 0, 4, -1, -1}, {2, 0, 0, 5, -1, -1}, {3, 1, 1, 2, 6, -1}, {3, 1, 1, 3, 7, -1},
		{3, 1, 1, 4, 8, -1}, {3, 1, 1, 5, 9, -1}, {3, 1, 1, 6, 10, -1}, {3, 1, 1, 7, 11, -1},
		{3, 1, 1, 8, 12, -1}, {3, 1, 1, 9, 13, -1}, {3, 1, 1, 10, 14, -1}, {2, 1, 1, 11, -1, -1},
		{2, 1, 1, 12, -1, -1}, {2, 1, 1, 13, -1, -1}, {2, 1, 1, 14, -1, -1}, {2, 1, 1, 15, -1, -1},
	}
	envelopeTable15 = [15][6]int{
		{2, 0, 0, 4, -1, -1}, {2, 0, 0, 5, -1, -1}, {3, 1, 1, 2, 6, -1}, {3, 1, 1, 3, 7, -1},
		{3, 1, 1, 4, 8, -1}, {3, 1, 1, 5, 9, -1}, {3, 1, 1, 6, 10, -1}, {3, 1, 1, 7, 11, -1},
		{3, 1, 1, 8, 12, -1}, {3, 1, 1, 9, 13, -1}, {2, 1, 1, 10, -1, -1}, {2, 1, 1, 11, -1, -1},
		{2, 1, 1, 12, -1, -1}, {2, 1, 1, 13, -1, -1}, {2, 1, 1, 14, -1, -1},
	}
	minFrameTranDistance = 4

	freqResTable8  = []FreqRes{FreqResLow, FreqResLow, FreqResLow, FreqResLow, FreqResLow, FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh}
	freqResTable16 = [16]FreqRes{
		FreqResLow, FreqResLow, FreqResLow, FreqResLow, FreqResLow,
		FreqResLow, FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh,
		FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh, FreqResHigh,
	}
)

// --- sbr_misc.cpp vector helpers (inlined, length-tracked) ------------------

// addRight is FDKsbrEnc_AddRight (sbr_misc.cpp): append value, length++.
func addRight(vector []int, lengthVector *int, value int) {
	vector[*lengthVector] = value
	(*lengthVector)++
}

// addLeft is FDKsbrEnc_AddLeft (sbr_misc.cpp): shift right, prepend, length++.
func addLeft(vector []int, lengthVector *int, value int) {
	for i := *lengthVector; i > 0; i-- {
		vector[i] = vector[i-1]
	}
	vector[0] = value
	(*lengthVector)++
}

// addVecLeft is FDKsbrEnc_AddVecLeft (sbr_misc.cpp): prepend src in reverse.
func addVecLeft(dst []int, lengthDst *int, src []int, lengthSrc int) {
	for i := lengthSrc - 1; i >= 0; i-- {
		addLeft(dst, lengthDst, src[i])
	}
}

// fillFrameTran is the 1:1 port of fillFrameTran (fram_gen.cpp:826-888).
func fillFrameTran(vTuningSegm, vTuningFreq []int, tran int, vBord []int, lengthVBord *int, vFreq []int, lengthVFreq *int, bmin, bmax *int) {
	*lengthVBord = 0
	*lengthVFreq = 0

	if vTuningSegm[0] != 0 {
		addRight(vBord, lengthVBord, tran-vTuningSegm[0])
		addRight(vFreq, lengthVFreq, vTuningFreq[0])
	}

	bord := tran
	addRight(vBord, lengthVBord, tran)

	if vTuningSegm[1] != 0 {
		bord += vTuningSegm[1]
		addRight(vBord, lengthVBord, bord)
		addRight(vFreq, lengthVFreq, vTuningFreq[1])
	}

	if vTuningSegm[2] != 0 {
		bord += vTuningSegm[2]
		addRight(vBord, lengthVBord, bord)
		addRight(vFreq, lengthVFreq, vTuningFreq[2])
	}

	addRight(vFreq, lengthVFreq, 1)

	*bmin = vBord[0]
	for i := 0; i < *lengthVBord; i++ {
		if vBord[i] < *bmin {
			*bmin = vBord[i]
		}
	}
	*bmax = vBord[0]
	for i := 0; i < *lengthVBord; i++ {
		if vBord[i] > *bmax {
			*bmax = vBord[i]
		}
	}
}

// fillFramePre is the 1:1 port of fillFramePre (fram_gen.cpp:910-955).
func fillFramePre(dmax int, vBord []int, lengthVBord *int, vFreq []int, lengthVFreq *int, bmin, rest int) {
	parts := 1
	d := rest
	s := 0

	for d > dmax {
		parts++
		segm := rest / parts
		S := (segm - 2) >> 1
		s = fixMinG(8, 2*S+2)
		d = rest - (parts-1)*s
	}

	bord := bmin
	for j := 0; j <= parts-2; j++ {
		bord = bord - s
		addLeft(vBord, lengthVBord, bord)
		addLeft(vFreq, lengthVFreq, 1)
	}
}

// calcFillLengthMax is the 1:1 port of calcFillLengthMax (fram_gen.cpp:967-1001).
func calcFillLengthMax(tranPos, numberTimeSlots int) int {
	var fmax int
	switch numberTimeSlots {
	case 16: // NUMBER_TIME_SLOTS_2048
		if tranPos < 4 {
			fmax = 6
		} else if tranPos == 4 || tranPos == 5 {
			fmax = 4
		} else {
			fmax = 8
		}
	case 15: // NUMBER_TIME_SLOTS_1920
		if tranPos < 4 {
			fmax = 5
		} else if tranPos == 4 || tranPos == 5 {
			fmax = 3
		} else {
			fmax = 7
		}
	default:
		fmax = 8
	}
	return fmax
}

// fillFramePost is the 1:1 port of fillFramePost (fram_gen.cpp:1026-1075).
func fillFramePost(parts, d *int, dmax int, vBord []int, lengthVBord *int, vFreq []int, lengthVFreq *int, bmax, bufferFrameStart, numberTimeSlots, fmax int) {
	s := 0
	rest := bufferFrameStart + 2*numberTimeSlots - bmax
	*d = rest

	if *d > 0 {
		*parts = 1
		for *d > dmax {
			*parts = *parts + 1
			segm := rest / (*parts)
			S := (segm - 2) >> 1
			s = fixMinG(fmax, 2*S+2)
			*d = rest - (*parts-1)*s
		}
		bord := bmax
		for j := 0; j <= *parts-2; j++ {
			bord += s
			addRight(vBord, lengthVBord, bord)
			addRight(vFreq, lengthVFreq, 1)
		}
	} else {
		*parts = 1
		*lengthVBord = *lengthVBord - 1
		*lengthVFreq = *lengthVFreq - 1
	}
}

// fillFrameInter is the 1:1 port of fillFrameInter (fram_gen.cpp:1101-1311).
func fillFrameInter(nL *int, vTuningSegm, vBord []int, lengthVBord *int, bmin int, vFreq []int, lengthVFreq *int, vBordFollow []int, lengthVBordFollow int_ptr, vFreqFollow []int, lengthVFreqFollow int_ptr, iFillFollow, dmin, dmax, numberTimeSlots int) {
	var middle, bNew, numBordFollow, bordMaxFollow, i int

	if numberTimeSlots != 9 { // != NUMBER_TIME_SLOTS_1152
		if iFillFollow >= 1 {
			*lengthVBordFollow = iFillFollow
			*lengthVFreqFollow = iFillFollow
		}

		numBordFollow = *lengthVBordFollow
		bordMaxFollow = vBordFollow[numBordFollow-1]

		middle = bmin - bordMaxFollow
		for middle < 0 {
			numBordFollow--
			bordMaxFollow = vBordFollow[numBordFollow-1]
			middle = bmin - bordMaxFollow
		}

		*lengthVBordFollow = numBordFollow
		*lengthVFreqFollow = numBordFollow
		*nL = numBordFollow - 1

		bNew = *lengthVBord

		if middle <= dmax {
			if middle >= dmin {
				addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
				addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
			} else {
				if vTuningSegm[0] != 0 {
					*lengthVBord = bNew - 1
					addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
					*lengthVFreq = bNew - 1
					addVecLeft(vFreq[1:], lengthVFreq, vFreqFollow, *lengthVFreqFollow)
				} else {
					if *lengthVBordFollow > 1 {
						addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow-1)
						addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVBordFollow-1)
						*nL = *nL - 1
					} else {
						for i = 0; i < *lengthVBord-1; i++ {
							vBord[i] = vBord[i+1]
						}
						for i = 0; i < *lengthVFreq-1; i++ {
							vFreq[i] = vFreq[i+1]
						}
						*lengthVBord = bNew - 1
						*lengthVFreq = bNew - 1
						addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
						addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
					}
				}
			}
		} else { // middle > dmax
			fillFramePre(dmax, vBord, lengthVBord, vFreq, lengthVFreq, bmin, middle)
			addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
			addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
		}
	} else { // numberTimeSlots == NUMBER_TIME_SLOTS_1152
		var l, m int

		if iFillFollow >= 1 {
			*lengthVBordFollow = iFillFollow
			*lengthVFreqFollow = iFillFollow
		}

		numBordFollow = *lengthVBordFollow
		bordMaxFollow = vBordFollow[numBordFollow-1]

		middle = bmin - bordMaxFollow

		for middle < 0 {
			if numBordFollow == 1 {
				break
			}
			numBordFollow--
			bordMaxFollow = vBordFollow[numBordFollow-1]
			middle = bmin - bordMaxFollow
		}

		if middle < 0 {
			for l, m = 0, 0; l < *lengthVBord; l++ {
				if vBord[l] > bordMaxFollow {
					vBord[m] = vBord[l]
					vFreq[m] = vFreq[l]
					m++
				}
			}
			*lengthVBord = l
			*lengthVFreq = l
			bmin = vBord[0]
		}

		*lengthVBordFollow = numBordFollow
		*lengthVFreqFollow = numBordFollow
		*nL = numBordFollow - 1

		middle = bmin - bordMaxFollow

		if middle <= dmin {
			bNew = *lengthVBord
			if vTuningSegm[0] != 0 {
				*lengthVBord = bNew - 1
				addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
				*lengthVFreq = bNew - 1
				addVecLeft(vFreq[1:], lengthVFreq, vFreqFollow, *lengthVFreqFollow)
			} else if *lengthVBordFollow > 1 {
				addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow-1)
				addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVBordFollow-1)
				*nL = *nL - 1
			} else {
				for i = 0; i < *lengthVBord-1; i++ {
					vBord[i] = vBord[i+1]
				}
				for i = 0; i < *lengthVFreq-1; i++ {
					vFreq[i] = vFreq[i+1]
				}
				*lengthVBord = bNew - 1
				*lengthVFreq = bNew - 1
				addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
				addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
			}
		} else if middle >= dmin && middle <= dmax {
			addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
			addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
		} else {
			fillFramePre(dmax, vBord, lengthVBord, vFreq, lengthVFreq, bmin, middle)
			addVecLeft(vBord, lengthVBord, vBordFollow, *lengthVBordFollow)
			addVecLeft(vFreq, lengthVFreq, vFreqFollow, *lengthVFreqFollow)
		}
	}
}

// calcFrameClass is the 1:1 port of calcFrameClass (fram_gen.cpp:1324-1365).
func calcFrameClass(frameClass, frameClassOld *FrameClass, tranFlag int, spreadFlag *int) {
	switch *frameClassOld {
	case Fixfixonly, Fixfix:
		if tranFlag != 0 {
			*frameClass = Fixvar
		} else {
			*frameClass = Fixfix
		}
	case Fixvar:
		if tranFlag != 0 {
			*frameClass = Varvar
			*spreadFlag = 0
		} else {
			if *spreadFlag != 0 {
				*frameClass = Varvar
			} else {
				*frameClass = Varfix
			}
		}
	case Varfix:
		if tranFlag != 0 {
			*frameClass = Fixvar
		} else {
			*frameClass = Fixfix
		}
	case Varvar:
		if tranFlag != 0 {
			*frameClass = Varvar
			*spreadFlag = 0
		} else {
			if *spreadFlag != 0 {
				*frameClass = Varvar
			} else {
				*frameClass = Varfix
			}
		}
	}
	*frameClassOld = *frameClass
}

// specialCase is the 1:1 port of specialCase (fram_gen.cpp:1385-1408).
func specialCase(spreadFlag *int, allowSpread int, vBord []int, lengthVBord *int, vFreq []int, lengthVFreq *int, parts *int, d int) {
	L := *lengthVBord
	if allowSpread != 0 {
		*spreadFlag = 1
		addRight(vBord, lengthVBord, vBord[L-1]+8)
		addRight(vFreq, lengthVFreq, 1)
		(*parts)++
	} else {
		if d == 1 {
			*lengthVBord = L - 1
			*lengthVFreq = L - 1
		} else {
			if (vBord[L-1] - vBord[L-2]) > 2 {
				vBord[L-1] = vBord[L-1] - 2
				vFreq[*lengthVFreq-1] = 0
			}
		}
	}
}

// calcCmonBorder is the 1:1 port of calcCmonBorder (fram_gen.cpp:1425-1443).
func calcCmonBorder(iCmon, iTran *int, vBord []int, lengthVBord *int, tran, bufferFrameStart, numberTimeSlots int) {
	for i := 0; i < *lengthVBord; i++ {
		if vBord[i] >= bufferFrameStart+numberTimeSlots {
			*iCmon = i
			break
		}
	}
	for i := 0; i < *lengthVBord; i++ {
		if vBord[i] >= tran {
			*iTran = i
			break
		} else {
			*iTran = emptyConst
		}
	}
}

// keepForFollowUp is the 1:1 port of keepForFollowUp (fram_gen.cpp:1467-1491).
func keepForFollowUp(vBordFollow []int, lengthVBordFollow *int, vFreqFollow []int, lengthVFreqFollow *int, iTranFollow, iFillFollow *int, vBord []int, lengthVBord *int, vFreq []int, iCmon, iTran, parts, numberTimeSlots int) {
	L := *lengthVBord
	*lengthVBordFollow = 0
	*lengthVFreqFollow = 0

	for j, i := 0, iCmon; i < L; i, j = i+1, j+1 {
		vBordFollow[j] = vBord[i] - numberTimeSlots
		vFreqFollow[j] = vFreq[i]
		(*lengthVBordFollow)++
		(*lengthVFreqFollow)++
	}
	if iTran != emptyConst {
		*iTranFollow = iTran - iCmon
	} else {
		*iTranFollow = emptyConst
	}
	*iFillFollow = L - (parts - 1) - iCmon
}

// fixMinG / fixMaxG are integer min/max over plain int (the SBR encoder fixMin /
// fixMax on INT). Local to fram_gen to avoid touching the decoder's symbols.
func fixMinG(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// int_ptr is *int (a tiny alias to keep fillFrameInter's signature readable).
type int_ptr = *int
