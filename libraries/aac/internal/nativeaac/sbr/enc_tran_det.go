// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the Fraunhofer FDK-AAC SBR-encoder
// transient detector, libSBRenc/src/tran_det.cpp (the "standard" transient
// detector FDKsbrEnc_transientDetect, the fast detector
// FDKsbrEnc_fastTransientDetect, the FIXFIX-frame splitter
// FDKsbrEnc_frameSplitter, and the two inits, with their statics). It decides,
// from the per-slot QMF subband Energies and their block scalefactors, where (if
// anywhere) a transient lies in the frame and whether a FIXFIX frame should be
// split into two envelopes — the time-grid input the frame generator consumes.
//
// fdk-aac SBR is FIXED-POINT: every value is an int32 FIXP_DBL Q-format, so the
// reproducibility contract is EXACT integer equality. The shared libFDK
// fixed-point kernels (fMult, fLog2, sqrtFixp, invSqrtNorm2, scaleValue,
// fDivNorm, fMultNorm, GetInvInt, fMultI, fPow2, …) are reused bit-for-bit from
// internal/nativeaac (never re-ported).
//
// Scope: HE-AAC v1 STD. The LD-SBR (SBR_SYNTAX_LOW_DELAY) split-threshold
// exponent decrement in the transient-detector init is ported faithfully (a
// single conditional decrement); the frameShift>0 LD transient-prediction
// look-ahead in transientDetect is likewise a small self-contained tail kept
// 1:1. No other LD/ELD/PS branches are reached by these functions.
//
// File name carries an `enc_` prefix to keep it clearly separated from the
// concurrently-developed SBR decoder/HF-gen files in this shared package.
package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// ABS_THRES, NRG_SHIFT (tran_det.cpp:115,129) and LD_DATA_SHIFT.
const (
	tranAbsThres    = int32(16) // ABS_THRES ((FIXP_DBL)16)
	tranNrgShift    = 3         // NRG_SHIFT
	encLdDataShift2 = 6         // LD_DATA_SHIFT (fixpoint_math.h:114)
)

// fl2 narrows a float64 literal to FIXP_DBL Q1.31 exactly as the C
// FL2FXCONST_DBL macro the compiler folds, via the verified nativeaac narrowing
// kernel. Use this ONLY for C literals WITHOUT an `f` suffix (true double
// constants, e.g. FL2FXCONST_DBL(1.0 / 4.0)).
func fl2(v float64) int32 { return nativeaac.Fl2fxconstDBL(v) }

// fl2f narrows an `f`-suffixed C literal (a float32 value, e.g.
// FL2FXCONST_DBL(0.66f)). The FL2FXCONST_DBL macro casts (double)(val); when val
// is a float32 literal, that double carries the float32-rounded value, NOT the
// true double — so the Go side must round the constant through float32 first to
// match bit-for-bit. (0.66f vs 0.66 differ by ~56 in Q31, a load-bearing LSB.)
func fl2f(v float32) int32 { return nativeaac.Fl2fxconstDBL(float64(v)) }

// fMaxDBL is fMax over FIXP_DBL (common_fix.h:236), reused from the hf-gen export.
func fMaxDBL(a, b int32) int32 { return nativeaac.FMaxDBL(a, b) }

// cntLeadingZerosI is fNormz / CntLeadingZeros(FIXP_DBL) returning int. The SBR
// scale arithmetic calls fNormz on strictly-positive fMax(1,...) operands, so
// the positive-only nativeaac.CntLeadingZeros == fixnormz_D matches exactly.
func cntLeadingZerosI(x int32) int { return nativeaac.CntLeadingZeros(x) }

// spectralChange is the 1:1 port of spectralChange (tran_det.cpp:131-249): a
// measure of how good it would be to split the frame at border into two
// envelopes (delta_sum scaled by 1/64; result exponent returned second).
// energies is the combined-highband EnergiesM matrix [..][MAX_FREQ_COEFFS].
func spectralChange(energies [][]int32, scaleEnergies []int, energyTotal int32, nSfb, start, border, yBufferWriteOffset, stop int) (deltaSum int32, resultE int) {
	var energiesEDiff [numberTimeSlots2304]int8
	energiesE := 0
	energyTotalE := 19
	energiesEAdd := 0

	len1 := border - start
	len2 := stop - border

	posWeight := fl2f(0.5) - (int32(len1) * nativeaac.GetInvInt(len1+len2))
	posWeight = encMaxvalDBL - (nativeaac.FMultDD(posWeight, posWeight) << 2)

	energiesE = 19 - nativeaac.FMinI(scaleEnergies[0], scaleEnergies[1])

	if energiesE < -10 {
		energiesEAdd = -10 - energiesE
		energiesE = -10
	} else if energiesE > 17 {
		energiesEAdd = energiesE - 17
		energiesE = 17
	} else {
		energiesEAdd = 0
	}

	prevEnergiesEDiff := int8(scaleEnergies[0] - nativeaac.FMinI(scaleEnergies[0], scaleEnergies[1]) + energiesEAdd + tranNrgShift)
	newEnergiesEDiff := int8(scaleEnergies[1] - nativeaac.FMinI(scaleEnergies[0], scaleEnergies[1]) + energiesEAdd + tranNrgShift)

	prevEnergiesEDiff = int8(nativeaac.FMinI(int(prevEnergiesEDiff), dfractBits-1))
	newEnergiesEDiff = int8(nativeaac.FMinI(int(newEnergiesEDiff), dfractBits-1))

	for i := start; i < yBufferWriteOffset; i++ {
		energiesEDiff[i] = prevEnergiesEDiff
	}
	for i := yBufferWriteOffset; i < stop; i++ {
		energiesEDiff[i] = newEnergiesEDiff
	}

	for j := 0; j < nSfb; j++ {
		accu1 := int32(0)
		accu2 := int32(0)
		accuE := energiesE + 3

		for i := start; i < border; i++ {
			accu1 += nativeaac.ScaleValue(energies[i][j], int32(-int(energiesEDiff[i])))
		}
		for i := border; i < stop; i++ {
			accu2 += nativeaac.ScaleValue(energies[i][j], int32(-int(energiesEDiff[i])))
		}

		accu1 = fMaxDBL(accu1, int32(len1))
		accu2 = fMaxDBL(accu2, int32(len2))

		ln2 := fl2f(0.6931471806) // LN2
		tmp0 := nativeaac.FLog2(accu2, int32(accuE)) - nativeaac.FLog2(accu1, int32(accuE))
		tmp1 := nativeaac.FLog2(int32(len1), 31) - nativeaac.FLog2(int32(len2), 31)
		delta := nativeaac.FMultDD(ln2, (tmp0 + tmp1))
		delta = nativeaac.FixpAbs(delta)

		accuE++
		accu1 >>= 1
		accu2 >>= 1

		if accuE&1 != 0 {
			accuE++
			accu1 >>= 1
			accu2 >>= 1
		}

		deltaSum += nativeaac.FMultDD(nativeaac.SqrtFixp(accu1+accu2), delta)
		resultE = (accuE >> 1) + encLdDataShift2
	}

	if energyTotalE&1 != 0 {
		energyTotalE++
		energyTotal >>= 1
	}

	invM, tmpE := nativeaac.InvSqrtNorm2(energyTotal)
	deltaSum = nativeaac.FMultDD(deltaSum, invM)
	resultE = resultE + (int(tmpE) - (energyTotalE >> 1))

	return nativeaac.FMultDD(deltaSum, posWeight), resultE
}

// addLowbandEnergies is the 1:1 port of addLowbandEnergies (tran_det.cpp:263-302):
// total lowband energy scaled by 2^19.
func addLowbandEnergies(energies [][]int32, scaleEnergies []int, yBufferWriteOffset, nrgSzShift, tranOff int, freqBandTable []uint8, slots int) int32 {
	accu1 := int32(0)
	accu2 := int32(0)
	tranOffdiv2 := tranOff >> nrgSzShift
	sc1 := dfractBits - cntLeadingZerosI(int32(nativeaac.FMaxI(1, int(freqBandTable[0])*(yBufferWriteOffset-tranOffdiv2)-1)))
	sc2 := dfractBits - cntLeadingZerosI(int32(nativeaac.FMaxI(1, int(freqBandTable[0])*(tranOffdiv2+(slots>>nrgSzShift)-yBufferWriteOffset)-1)))

	var ts int
	for ts = tranOffdiv2; ts < yBufferWriteOffset; ts++ {
		for k := 0; k < int(freqBandTable[0]); k++ {
			accu1 += energies[ts][k] >> uint(sc1)
		}
	}
	for ; ts < tranOffdiv2+(slots>>nrgSzShift); ts++ {
		for k := 0; k < int(freqBandTable[0]); k++ {
			accu2 += energies[ts][k] >> uint(sc2)
		}
	}

	nrgTotalM, nrgTotalE := nativeaac.FAddNorm(accu1, int32((sc1-5)-scaleEnergies[0]), accu2, int32((sc2-5)-scaleEnergies[1]))
	nrgTotalM = nativeaac.ScaleValueSaturate(nrgTotalM, nrgTotalE)
	return nrgTotalM
}

// addHighbandEnergies is the 1:1 port of addHighbandEnergies
// (tran_det.cpp:321-383): combine QMF energies into the SBR-resolution energiesM
// matrix (written in place) and return total highband energy scaled by 2^19.
func addHighbandEnergies(energies [][]int32, scaleEnergies []int, yBufferWriteOffset int, energiesM [][]int32, freqBandTable []uint8, nSfb, sbrSlots, timeStep int) int32 {
	var scale [2]int

	for slotOut := 0; slotOut < sbrSlots; slotOut++ {
		slotIn := slotOut
		for j := 0; j < nSfb; j++ {
			accu := int32(0)
			li := int(freqBandTable[j])
			ui := int(freqBandTable[j+1])
			for k := li; k < ui; k++ {
				for i := 0; i < timeStep; i++ {
					accu += energies[slotIn][k] >> 5
				}
			}
			energiesM[slotOut][j] = accu
		}
	}

	scale[0] = nativeaac.FMinI(8, scaleEnergies[0])
	scale[1] = nativeaac.FMinI(8, scaleEnergies[1])

	var nrgTotal int32
	if (scaleEnergies[0]-scale[0]) > (dfractBits-1) || (scaleEnergies[1]-scale[1]) > (dfractBits-1) {
		nrgTotal = 0
	} else {
		accu := int32(0)
		for slotOut := 0; slotOut < yBufferWriteOffset; slotOut++ {
			for j := 0; j < nSfb; j++ {
				accu += energiesM[slotOut][j] >> uint(scale[0])
			}
		}
		nrgTotal = accu >> uint(scaleEnergies[0]-scale[0])

		for slotOut := yBufferWriteOffset; slotOut < sbrSlots; slotOut++ {
			for j := 0; j < nSfb; j++ {
				accu += energiesM[slotOut][j] >> uint(scale[0])
			}
		}
		nrgTotal = nativeaac.FAddSaturate(nrgTotal, accu>>uint(scaleEnergies[1]-scale[1]))
	}
	return nrgTotal
}

// FrameSplitter is the 1:1 port of FDKsbrEnc_frameSplitter (tran_det.cpp:393-465).
func FrameSplitter(energies [][]int32, scaleEnergies []int, h *SbrTransientDetector, freqBandTable, tranVector []uint8, yBufferWriteOffset, yBufferSzShift, nSfb, timeStep, noCols int, tonality *int32) {
	if tranVector[1] != 0 {
		return
	}
	var delta int32
	var deltaE int
	sbrSlots := int(nativeaac.FMultI(nativeaac.GetInvInt(timeStep), int32(noCols)))

	flat := make([]int32, numberTimeSlots2304*encMaxFreqCoeffs)
	energiesM := make([][]int32, numberTimeSlots2304)
	for i := range energiesM {
		energiesM[i] = flat[i*encMaxFreqCoeffs : i*encMaxFreqCoeffs+encMaxFreqCoeffs]
	}

	newLowbandEnergy := addLowbandEnergies(energies, scaleEnergies, yBufferWriteOffset, yBufferSzShift, h.TranOff, freqBandTable, noCols)
	newHighbandEnergy := addHighbandEnergies(energies, scaleEnergies, yBufferWriteOffset, energiesM, freqBandTable, nSfb, sbrSlots, timeStep)

	energyTotal := (newLowbandEnergy >> 1) + (h.PrevLowBandEnergy >> 1)
	energyTotal = nativeaac.FAddSaturate(energyTotal, newHighbandEnergy)
	border := (sbrSlots + 1) >> 1

	if (energyTotal&int32(-32)) != 0 && (scaleEnergies[0] < 32 || scaleEnergies[1] < 32) {
		delta, deltaE = spectralChange(energiesM, scaleEnergies, energyTotal, nSfb, 0, border, yBufferWriteOffset, sbrSlots)
	} else {
		delta = 0
		deltaE = 0
		*tonality = 0
	}

	if nativeaac.FIsLessThan(h.SplitThrM, int32(h.SplitThrE), delta, int32(deltaE)) {
		tranVector[0] = 1
	} else {
		tranVector[0] = 0
	}

	h.PrevLowBandEnergy = newLowbandEnergy
	h.PrevHighBandEnergy = newHighbandEnergy
}

// calculateThresholds is the 1:1 port of calculateThresholds (tran_det.cpp:470-546).
func calculateThresholds(energies [][]int32, scaleEnergies []int, thresholds []int32, yBufferWriteOffset, yBufferSzShift, noCols, noRows, tranOff int) {
	iNoCols := nativeaac.GetInvInt(noCols+tranOff) << uint(yBufferSzShift)
	iNoCols1 := nativeaac.GetInvInt(noCols+tranOff-1) << uint(yBufferSzShift)

	commonScale := nativeaac.FMinI(scaleEnergies[0], scaleEnergies[1])
	scaleFactor0 := nativeaac.FMinI(scaleEnergies[0]-commonScale, dfractBits-1)
	scaleFactor1 := nativeaac.FMinI(scaleEnergies[1]-commonScale, dfractBits-1)

	for i := 0; i < noRows; i++ {
		startEnergy := tranOff >> yBufferSzShift
		endEnergy := (noCols >> yBufferSzShift) + tranOff

		accu0 := int32(0)
		accu1 := int32(0)
		var j int
		for j = startEnergy; j < yBufferWriteOffset; j++ {
			accu0 = nativeaac.FMultAddDiv2(accu0, energies[j][i], iNoCols)
		}
		for ; j < endEnergy; j++ {
			accu1 = nativeaac.FMultAddDiv2(accu1, energies[j][i], iNoCols)
		}

		meanVal := ((accu0 << 1) >> uint(scaleFactor0)) + ((accu1 << 1) >> uint(scaleFactor1))
		shift := nativeaac.FMaxI(0, int(nativeaac.CountLeadingBits(meanVal))-6)

		accu := int32(0)
		for j = startEnergy; j < yBufferWriteOffset; j++ {
			temp := (meanVal - (energies[j][i] >> uint(scaleFactor0))) << uint(shift)
			temp = nativeaac.FPow2Div2(temp)
			accu = nativeaac.FMultAddDiv2(accu, temp, iNoCols1)
		}
		for ; j < endEnergy; j++ {
			temp := (meanVal - (energies[j][i] >> uint(scaleFactor1))) << uint(shift)
			temp = nativeaac.FPow2Div2(temp)
			accu = nativeaac.FMultAddDiv2(accu, temp, iNoCols1)
		}
		accu <<= 2
		stdVal := nativeaac.SqrtFixp(accu) >> uint(shift)

		var temp int32
		if commonScale <= dfractBits-1 {
			temp = nativeaac.FMultDD(fl2f(0.66), thresholds[i]) + (nativeaac.FMultDD(fl2f(0.34), stdVal) >> uint(commonScale))
		} else {
			temp = 0
		}
		thresholds[i] = fMaxDBL(tranAbsThres, temp)
	}
}

// extractTransientCandidates is the 1:1 port of extractTransientCandidates
// (tran_det.cpp:551-647).
func extractTransientCandidates(energies [][]int32, scaleEnergies []int, thresholds, transients []int32, yBufferWriteOffset, yBufferSzShift, noCols, startBand, stopBand, tranOff, addPrevSamples int) {
	var iThres int32
	var energiesTemp [2 * 32]int32

	tmpScaleEnergies0 := nativeaac.FMinI(scaleEnergies[0], encMaxShiftDBL)
	tmpScaleEnergies1 := nativeaac.FMinI(scaleEnergies[1], encMaxShiftDBL)

	copy(transients[0:tranOff+addPrevSamples], transients[noCols-addPrevSamples:noCols-addPrevSamples+tranOff+addPrevSamples])
	for i := tranOff + addPrevSamples; i < tranOff+addPrevSamples+noCols; i++ {
		transients[i] = 0
	}

	endCond := noCols
	startEnerg := (tranOff - 3) >> yBufferSzShift
	endEnerg := ((noCols + (yBufferWriteOffset << yBufferSzShift)) - 1) >> yBufferSzShift

	for i := startBand; i < stopBand; i++ {
		thres := thresholds[i]

		if int64(thresholds[i]) >= 256 {
			iThres = int32(int64(encMaxvalDBL)/(int64(thresholds[i])+1)) << (32 - 24)
		} else {
			iThres = encMaxvalDBL
		}

		var j int
		if yBufferSzShift == 1 {
			for j = startEnerg; j < yBufferWriteOffset; j++ {
				v := energies[j][i] >> uint(tmpScaleEnergies0)
				energiesTemp[(j<<1)+1] = v
				energiesTemp[j<<1] = v
			}
			for ; j <= endEnerg; j++ {
				v := energies[j][i] >> uint(tmpScaleEnergies1)
				energiesTemp[(j<<1)+1] = v
				energiesTemp[j<<1] = v
			}
		} else {
			for j = startEnerg; j < yBufferWriteOffset; j++ {
				energiesTemp[j] = energies[j][i] >> uint(tmpScaleEnergies0)
			}
			for ; j <= endEnerg; j++ {
				energiesTemp[j] = energies[j][i] >> uint(tmpScaleEnergies1)
			}
		}

		jIndex := tranOff
		jpBM := jIndex + addPrevSamples

		for j = endCond; j > 0; j, jIndex, jpBM = j-1, jIndex+1, jpBM+1 {
			delta := int32(0)
			tran := int32(0)
			for d := 1; d < 4; d++ {
				delta += energiesTemp[jIndex+d]
				delta -= energiesTemp[jIndex-d]
				delta -= thres
				if delta > 0 {
					tran = nativeaac.FMultAddDiv2(tran, iThres, delta)
				}
			}
			transients[jpBM] += tran << 1
		}
	}
}

// TransientDetect is the 1:1 port of FDKsbrEnc_transientDetect (tran_det.cpp:649-727).
func TransientDetect(h *SbrTransientDetector, energies [][]int32, scaleEnergies []int, transientInfo []uint8, yBufferWriteOffset, yBufferSzShift, timeStep, frameMiddleBorder int) {
	noCols := h.NoCols
	timeStepShift := 0

	qmfStartSample := timeStep * frameMiddleBorder
	addPrevSamples := 0
	if qmfStartSample <= 0 {
		addPrevSamples = 1
	}

	switch timeStep {
	case 1:
		timeStepShift = 0
	case 2:
		timeStepShift = 1
	case 4:
		timeStepShift = 2
	}

	calculateThresholds(energies, scaleEnergies, h.Thresholds[:], yBufferWriteOffset, yBufferSzShift, h.NoCols, h.NoRows, h.TranOff)
	extractTransientCandidates(energies, scaleEnergies, h.Thresholds[:], h.Transients[:], yBufferWriteOffset, yBufferSzShift, h.NoCols, 0, h.NoRows, h.TranOff, addPrevSamples)

	transientInfo[0] = 0
	transientInfo[1] = 0
	transientInfo[2] = 0

	qmfStartSample += addPrevSamples

	for i := qmfStartSample; i < qmfStartSample+noCols; i++ {
		cond := h.Transients[i] < nativeaac.FMultDD(fl2f(0.9), h.Transients[i-1]) && h.Transients[i-1] > h.TranThr
		if cond {
			transientInfo[0] = uint8((i - qmfStartSample) >> timeStepShift)
			transientInfo[1] = 1
			break
		}
	}

	if h.FrameShift != 0 {
		for i := qmfStartSample + noCols; i < qmfStartSample+noCols+h.FrameShift; i++ {
			cond := h.Transients[i] < nativeaac.FMultDD(fl2f(0.9), h.Transients[i-1]) && h.Transients[i-1] > h.TranThr
			if cond {
				pos := (i - qmfStartSample - noCols) >> timeStepShift
				if pos < 3 && transientInfo[1] == 0 {
					transientInfo[2] = 1
				}
				break
			}
		}
	}
}

// FastTransientDetect is the 1:1 port of FDKsbrEnc_fastTransientDetect
// (tran_det.cpp:907-1092).
func FastTransientDetect(h *FastTranDetector, energies [][]int32, scaleEnergies []int, yBufferWriteOffset int, tranVector []uint8) {
	maxDeltaEnergy := int32(0)
	maxDeltaEnergyScale := 0
	indMax := 0
	isTransientInFrame := 0

	nTimeSlots := h.NTimeSlots
	lookahead := h.Lookahead
	startBand := h.StartBand
	stopBand := h.StopBand

	transientCandidates := h.TransientCandidates[:]
	energyTimeSlots := h.EnergyTimeSlots[:]
	energyTimeSlotsScale := h.EnergyTimeSlotsScale[:]
	deltaEnergy := h.DeltaEnergy[:]
	deltaEnergyScale := h.DeltaEnergyScale[:]

	thr := fl2f(float32(5.0) / float32(8.0)) // TRAN_DET_THRSHLD
	thrScale := tranDetThrshldScale

	tranVector[2] = 0

	for i := lookahead; i < nTimeSlots+lookahead; i++ {
		transientCandidates[i] = 0
	}

	for timeSlot := lookahead; timeSlot < nTimeSlots+lookahead; timeSlot++ {
		tmpE := int32(0)
		headroomEnSlot := dfractBits - 1
		smallNRG := fl2f(1e-2)

		for band := startBand; band < stopBand; band++ {
			tmpHeadroom := nativeaac.CntLeadingZeros(energies[timeSlot][band]) - 1
			if tmpHeadroom < headroomEnSlot {
				headroomEnSlot = tmpHeadroom
			}
		}

		i := 0
		for band := startBand; band < stopBand; band, i = band+1, i+1 {
			weightedEnergy := nativeaac.FMultDD(energies[timeSlot][band]<<uint(headroomEnSlot), h.DBfM[i])
			tmpE += weightedEnergy >> uint(6+(10-h.DBfE[i]))
		}

		energyTimeSlots[timeSlot] = tmpE

		if timeSlot < yBufferWriteOffset {
			energyTimeSlotsScale[timeSlot] = (-scaleEnergies[0] + 2*encQmfScaleOffset) + (10 + 6) - headroomEnSlot
		} else {
			energyTimeSlotsScale[timeSlot] = (-scaleEnergies[1] + 2*encQmfScaleOffset) + (10 + 6) - headroomEnSlot
		}

		var denominator int32
		var denominatorScale int
		if -energyTimeSlotsScale[timeSlot-1]+1 > 5 {
			denominator = smallNRG
			denominatorScale = 0
		} else {
			smallNRG = nativeaac.ScaleValue(smallNRG, int32(-(energyTimeSlotsScale[timeSlot-1] + 1)))
			denominator = (energyTimeSlots[timeSlot-1] >> 1) + smallNRG
			denominatorScale = energyTimeSlotsScale[timeSlot-1] + 1
		}

		divM, norm := nativeaac.FDivNorm(energyTimeSlots[timeSlot], denominator)
		deltaEnergy[timeSlot] = divM
		deltaEnergyScale[timeSlot] = energyTimeSlotsScale[timeSlot] - denominatorScale + int(norm)
	}

	for timeSlot := lookahead; timeSlot < nTimeSlots+lookahead; timeSlot++ {
		energyCurSlotWeighted := nativeaac.FMultDD(energyTimeSlots[timeSlot], fl2f(float32(1.0)/float32(1.4)))
		if !nativeaac.FIsLessThan(deltaEnergy[timeSlot], int32(deltaEnergyScale[timeSlot]), thr, int32(thrScale)) &&
			(((transientCandidates[timeSlot-2] == 0) && (transientCandidates[timeSlot-1] == 0)) ||
				!nativeaac.FIsLessThan(energyCurSlotWeighted, int32(energyTimeSlotsScale[timeSlot]), energyTimeSlots[timeSlot-1], int32(energyTimeSlotsScale[timeSlot-1])) ||
				!nativeaac.FIsLessThan(energyCurSlotWeighted, int32(energyTimeSlotsScale[timeSlot]), energyTimeSlots[timeSlot-2], int32(energyTimeSlotsScale[timeSlot-2]))) {
			transientCandidates[timeSlot] = 1
		}
	}

	for timeSlot := 0; timeSlot < nTimeSlots; timeSlot++ {
		scale := nativeaac.FMaxI(deltaEnergyScale[timeSlot], maxDeltaEnergyScale)
		if transientCandidates[timeSlot] != 0 &&
			((deltaEnergy[timeSlot] >> uint(scale-deltaEnergyScale[timeSlot])) > (maxDeltaEnergy >> uint(scale-maxDeltaEnergyScale))) {
			maxDeltaEnergy = deltaEnergy[timeSlot]
			maxDeltaEnergyScale = scale
			indMax = timeSlot
			isTransientInFrame = 1
		}
	}

	if isTransientInFrame != 0 {
		tranVector[0] = uint8(indMax)
		tranVector[1] = 1
	} else {
		tranVector[0] = 0
		tranVector[1] = 0
	}

	for timeSlot := nTimeSlots; timeSlot < nTimeSlots+lookahead; timeSlot++ {
		if transientCandidates[timeSlot] != 0 {
			tranVector[2] = 1
		}
	}

	for timeSlot := 0; timeSlot < lookahead; timeSlot++ {
		transientCandidates[timeSlot] = transientCandidates[nTimeSlots+timeSlot]
		energyTimeSlots[timeSlot] = energyTimeSlots[nTimeSlots+timeSlot]
		energyTimeSlotsScale[timeSlot] = energyTimeSlotsScale[nTimeSlots+timeSlot]
		deltaEnergy[timeSlot] = deltaEnergy[nTimeSlots+timeSlot]
		deltaEnergyScale[timeSlot] = deltaEnergyScale[nTimeSlots+timeSlot]
	}
}

// InitSbrTransientDetector is the 1:1 port of FDKsbrEnc_InitSbrTransientDetector
// (tran_det.cpp:729-786). The sbrConfiguration fields are passed as the scalars
// it actually reads (standardBitrate, nChannels, bitRate from codecSettings, plus
// tran_thr and tran_det_mode) — the closure is fully static, no function-pointer
// dispatch. lowDelay selects the SBR_SYNTAX_LOW_DELAY exponent decrement.
func InitSbrTransientDetector(h *SbrTransientDetector, lowDelay bool, frameSize, sampleFreq, standardBitrate, nChannels, codecBitrate, tranThr, tranDetMode, tranFc, noCols, noRows, frameShift, tranOff int) int {
	totalBitrate := standardBitrate * nChannels

	*h = SbrTransientDetector{}
	h.FrameShift = frameShift
	h.TranOff = tranOff

	var bitrateFactorM int32
	var bitrateFactorE int
	if codecBitrate != 0 {
		m, e := nativeaac.FDivNorm(int32(totalBitrate), int32(codecBitrate<<2))
		bitrateFactorM = m
		bitrateFactorE = int(e) + 2
	} else {
		bitrateFactorM = fl2(1.0 / 4.0)
		bitrateFactorE = 2
	}

	framedurFix := nativeaac.FDivNorm0(int32(frameSize), int32(sampleFreq))

	tmp := framedurFix - fl2(0.010)
	tmp = fMaxDBL(tmp, fl2(0.0001))
	tmpM, tmpE := nativeaac.FDivNorm(fl2(0.000075), nativeaac.FPow2(tmp))
	tmp = tmpM

	bitrateFactorE = int(tmpE) + bitrateFactorE

	if lowDelay {
		bitrateFactorE-- // divide by 2
	}

	h.NoCols = noCols
	h.TranThr = int32((tranThr << (32 - 24 - 1)) / noRows)
	h.TranFc = tranFc
	h.SplitThrM = nativeaac.FMultDD(tmp, bitrateFactorM)
	h.SplitThrE = bitrateFactorE
	h.NoRows = noRows
	h.Mode = tranDetMode
	h.PrevLowBandEnergy = 0

	return 0
}

// InitSbrFastTransientDetector is the 1:1 port of
// FDKsbrEnc_InitSbrFastTransientDetector (tran_det.cpp:790-905): it precomputes
// the per-band high-pass weighting dBf_m/dBf_e ROM. EXP_E == 7.
func InitSbrFastTransientDetector(h *FastTranDetector, timeSlotsPerFrame, bandwidthQmfSlot, noQmfChannels, sbrQmf1stBand int) int {
	const expE = 7 // EXP_E (tran_det.cpp:833)

	h.Lookahead = tranDetLookahead
	h.NTimeSlots = timeSlotsPerFrame

	buffSize := h.NTimeSlots + h.Lookahead
	for i := 0; i < buffSize; i++ {
		h.DeltaEnergy[i] = 0
		h.EnergyTimeSlots[i] = 0
		h.LowpassEnergy[i] = 0
		h.TransientCandidates[i] = 0
	}

	h.StopBand = nativeaac.FMinI(tranDetStopFreq/bandwidthQmfSlot, noQmfChannels)
	h.StartBand = nativeaac.FMinI(sbrQmf1stBand, h.StopBand-tranDetMinQmfBands)

	// QMF_HP_dBd_SLOPE_FIX == FL2FXCONST_DBL(0.00075275f) (tran_det.h:138-139).
	qmfHpSlope := fl2f(0.00075275)
	myExp := nativeaac.FMultNorm5(qmfHpSlope, 0, int32(bandwidthQmfSlot), dfractBits-1, expE)
	myExpSlot := myExp

	for i := 0; i < 64; i++ {
		var dBfM int32
		var dBfE int

		// Round up to next integer: (myExpSlot & 0xfe000000) + 0x02000000.
		myExpInt := (myExpSlot & int32(-0x02000000)) + int32(0x02000000)
		myExpFract := myExpInt - myExpSlot

		dBfInt := nativeaac.CalcInvLdData(myExpInt)

		if dBfInt <= 46340 {
			dBfInt *= dBfInt

			dBfFract := nativeaac.CalcInvLdData(-myExpFract)
			dBfFract, tmp := nativeaac.FMultNorm(dBfFract, dBfFract)

			dBfE = int((dfractBits - 1 - tmp)) - int(nativeaac.CountLeadingBits(dBfInt))

			dBfM = nativeaac.FMultNorm5(dBfInt, dfractBits-1, dBfFract, tmp, int32(dBfE))

			myExpSlot += myExp
		} else {
			dBfM = 0
			dBfE = 0
		}

		h.DBfM[i] = dBfM
		h.DBfE[i] = dBfE
	}

	return 0
}
