// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Perceptual-entropy + reduction-value DRIVER core of the AAC encoder
// threshold-adjustment loop, ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/adj_thr.cpp. FDKaacEnc_peCalculation chains the preparePe /
// calcWeighting leaves and applies the ld64 energy/threshold weighting before the
// element pe sum; CalcRedValPower is the signed 2^(num/denum) reduction-value
// kernel; FDKaacEnc_adaptThresholdsToPe is the CBR reduction heart — two
// reduction-value guesses plus the Part IV correctThresh/reduceMinSnr/
// allowMoreHoles refinement chain that compresses thresholds to the granted pe.
//
// CBR/AAC-LC path only. The VBR sibling FDKaacEnc_AdaptThresholdsVBR /
// FDKaacEnc_reduceThresholdsVBR is excluded (noted at the AdjustThresholds seam).
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8; fDivNorm/f2Pow/fMultNorm/fMultI/scaleValue/fMax are
// the already-verified leaf kernels. f2Pow(value, *scaling, scaling) is the
// exponent-carrying f2PowWithExp form.

// peCalculation is the 1:1 port of FDKaacEnc_peCalculation (adj_thr.cpp:908-944):
// run preparePe + calcWeighting, weight the energies/thresholds by sfbEnFacLd, then
// compute the (un-reduced) element pe via calcPe.
func peCalculation(peData *peData, psyOutChannel []*PsyOutChannel,
	qcOutChannel []*QcOutChannel, toolsInfo *PsyOutToolsInfo,
	adjThrStateElement *atsElement, nChannels int) {
	// constants that will not change during successive pe calculations
	preparePe(peData, psyOutChannel, qcOutChannel, nChannels, adjThrStateElement.peOffset)

	// calculate weighting factor for threshold adjustment
	calcWeighting(peData, psyOutChannel, qcOutChannel, toolsInfo, adjThrStateElement, nChannels, 1)

	// weight energies and thresholds
	for ch := 0; ch < nChannels; ch++ {
		pQcOutCh := qcOutChannel[ch]
		for sfbGrp := 0; sfbGrp < psyOutChannel[ch].SfbCnt; sfbGrp += psyOutChannel[ch].SfbPerGroup {
			for sfb := 0; sfb < psyOutChannel[ch].MaxSfbPerGroup; sfb++ {
				pQcOutCh.SfbWeightedEnergyLdData[sfb+sfbGrp] =
					pQcOutCh.SfbEnergyLdData[sfb+sfbGrp] - pQcOutCh.SfbEnFacLd[sfb+sfbGrp]
				pQcOutCh.SfbThresholdLdData[sfb+sfbGrp] -= pQcOutCh.SfbEnFacLd[sfb+sfbGrp]
			}
		}
	}

	// pe without reduction
	calcPe(psyOutChannel, qcOutChannel, peData, nChannels)
}

// calcRedValPower is the 1:1 port of CalcRedValPower (adj_thr.cpp:1871-1882):
// 2^(num/denum), preserving the sign of num, returning the mantissa and (via the
// second return) the result exponent. fDivNorm/f2Pow are the exponent-carrying
// forms; the 3-arg f2Pow(value, *scaling, scaling) overload == f2PowWithExp.
func calcRedValPower(num, denum int32) (value int32, scaling int32) {
	var div, e int32
	if num >= fl2fxconstDBL(0.0) {
		div, e = fDivNorm(num, denum)
	} else {
		div, e = fDivNorm(-num, denum)
		div = -div
	}
	value, scaling = f2PowWithExp(div, e)
	return value, scaling
}

// adaptThresholdsToPe is the 1:1 port of FDKaacEnc_adaptThresholdsToPe
// (adj_thr.cpp:1889-2157): two guesses for the reduction value plus one final
// correction of the thresholds (CBR). pAhFlag/pThrExp are the scratch
// [8][2][MAX_GROUPED_SFB] matrices the C aliases onto dynMem_Ah_Flag /
// dynMem_Thr_Exp; the Go port models them with plain per-element slices.
func adaptThresholdsToPe(cm *ChannelMapping, adjThrStateElement []*atsElement,
	qcElement []*QcOutElement, psyOutElement []*PsyOutElement, desiredPe,
	maxIter2ndGuess, processElements, elementOffset int) {

	var reductionValueM int32
	var reductionValueE int

	constPartGlobal, noRedPeGlobal, nActiveLinesGlobal, redPeGlobal := 0, 0, 0, 0

	nElements := elementOffset + processElements
	if nElements > cm.NElements {
		nElements = cm.NElements
	}

	// scratch: pAhFlag[el][ch][sfb], pThrExp[el][ch][sfb]
	pAhFlag := make([][][]uint8, len(qcElement))
	pThrExp := make([][][]int32, len(qcElement))
	for el := range qcElement {
		pAhFlag[el] = make([][]uint8, 2)
		pThrExp[el] = make([][]int32, 2)
		for ch := 0; ch < 2; ch++ {
			pAhFlag[el][ch] = make([]uint8, maxGroupedSFB)
			pThrExp[el][ch] = make([]int32, maxGroupedSFB)
		}
	}

	// Part I: Initialize data structures and variables
	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		nChannels := cm.ElInfo[elementId].NChannelsInEl
		peData := &qcElement[elementId].PeData

		// thresholds to the power of redExp
		calcThreshExp(pThrExp[elementId], qcElement[elementId].QcOutChannel[:],
			psyOutElement[elementId].PsyOutChannel[:], nChannels)

		// lower the minSnr requirements for low energies
		adaptMinSnr(qcElement[elementId].QcOutChannel[:],
			psyOutElement[elementId].PsyOutChannel[:],
			&adjThrStateElement[elementId].minSnrAdaptParam, nChannels)

		// init ahFlag (0: no ah necessary, 1: ah possible, 2: ah active)
		initAvoidHoleFlag(qcElement[elementId].QcOutChannel[:],
			psyOutElement[elementId].PsyOutChannel[:], pAhFlag[elementId],
			&psyOutElement[elementId].ToolsInfo, nChannels,
			&adjThrStateElement[elementId].ahParam)

		// sum up
		constPartGlobal += int(peData.constPart)
		noRedPeGlobal += int(peData.pe)
		nActiveLinesGlobal += fixMax(int(peData.nActiveLines), 1)
	}

	// First guess of reduction value
	redValM, redValE := calcRedValPower(int32(constPartGlobal-desiredPe), int32(4*nActiveLinesGlobal))
	avgThrExpM, avgThrExpE := calcRedValPower(int32(constPartGlobal-noRedPeGlobal), int32(4*nActiveLinesGlobal))
	resultE := int32(fixMax(int(redValE), int(avgThrExpE))) + 1

	reductionValueM = fMax(fl2fxconstDBL(0.0),
		scaleValue(redValM, redValE-resultE)-scaleValue(avgThrExpM, avgThrExpE-resultE))
	reductionValueE = int(resultE)

	// Part II: Calculate bit consumption of initial bit constraints setup
	for elementId := elementOffset; elementId < nElements; elementId++ {
		if cm.ElInfo[elementId].ElType == IDDSE {
			continue
		}
		nChannels := cm.ElInfo[elementId].NChannelsInEl
		peData := &qcElement[elementId].PeData

		reduceThresholdsCBR(qcElement[elementId].QcOutChannel[:],
			psyOutElement[elementId].PsyOutChannel[:], pAhFlag[elementId],
			pThrExp[elementId], nChannels, reductionValueM, int32(reductionValueE))

		calcPe(psyOutElement[elementId].PsyOutChannel[:],
			qcElement[elementId].QcOutChannel[:], peData, nChannels)

		redPeGlobal += int(peData.pe)
	}

	// Part III: Iterate until bit constraints are met
	iter := 0
	for fixpAbsInt(redPeGlobal-desiredPe) > int(fMultI(fl2fxconstDBL(0.05), int32(desiredPe))) &&
		iter < maxIter2ndGuess {
		redPeNoAHGlobal := 0
		constPartNoAHGlobal := 0
		nActiveLinesNoAHGlobal := 0

		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			nChannels := cm.ElInfo[elementId].NChannelsInEl
			peData := &qcElement[elementId].PeData

			// pe for bands where avoid hole is inactive
			redPeNoAH, constPartNoAH, nActiveLinesNoAH :=
				calcPeNoAH(peData, pAhFlag[elementId], psyOutElement[elementId].PsyOutChannel[:], nChannels)

			redPeNoAHGlobal += redPeNoAH
			constPartNoAHGlobal += constPartNoAH
			nActiveLinesNoAHGlobal += nActiveLinesNoAH
		}

		// Calculate new redVal ...
		if desiredPe < redPeGlobal {
			// new desired pe without bands where avoid hole is active
			desiredPeNoAHGlobal := desiredPe - (redPeGlobal - redPeNoAHGlobal)
			desiredPeNoAHGlobal = fixMax(0, desiredPeNoAHGlobal)

			// second guess
			if nActiveLinesNoAHGlobal > 0 {
				redValM, redValE = calcRedValPower(int32(constPartNoAHGlobal-desiredPeNoAHGlobal),
					int32(4*nActiveLinesNoAHGlobal))
				avgThrExpM, avgThrExpE = calcRedValPower(int32(constPartNoAHGlobal-redPeNoAHGlobal),
					int32(4*nActiveLinesNoAHGlobal))
				resultE = int32(fixMax(reductionValueE, fixMax(int(redValE), int(avgThrExpE))+1)) + 1

				reductionValueM = fMax(fl2fxconstDBL(0.0),
					scaleValue(reductionValueM, int32(reductionValueE)-resultE)+
						scaleValue(redValM, redValE-resultE)-
						scaleValue(avgThrExpM, avgThrExpE-resultE))
				reductionValueE = int(resultE)
			}
		} else {
			// redVal *= redPeGlobal/desiredPe;
			div, sc0 := fDivNorm(int32(redPeGlobal), int32(desiredPe))
			prod, sc1 := fMultNorm(reductionValueM, div)
			reductionValueM = prod
			reductionValueE += int(sc0) + int(sc1)

			for elementId := elementOffset; elementId < nElements; elementId++ {
				if cm.ElInfo[elementId].ElType == IDDSE {
					continue
				}
				resetAHFlags(pAhFlag[elementId], cm.ElInfo[elementId].NChannelsInEl,
					psyOutElement[elementId].PsyOutChannel[:])
			}
		}

		redPeGlobal = 0
		// Calculate new redVal's PE...
		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			nChannels := cm.ElInfo[elementId].NChannelsInEl
			peData := &qcElement[elementId].PeData

			reduceThresholdsCBR(qcElement[elementId].QcOutChannel[:],
				psyOutElement[elementId].PsyOutChannel[:], pAhFlag[elementId],
				pThrExp[elementId], nChannels, reductionValueM, int32(reductionValueE))

			calcPe(psyOutElement[elementId].PsyOutChannel[:],
				qcElement[elementId].QcOutChannel[:], peData, nChannels)
			redPeGlobal += int(peData.pe)
		}

		iter++
	}

	// Part IV: if still required, further reduce constraints
	// correct thresholds to get closer to the desired pe
	if redPeGlobal > desiredPe {
		correctThresh(cm, qcElement, psyOutElement, pAhFlag, pThrExp,
			reductionValueM, reductionValueE, desiredPe-redPeGlobal, processElements, elementOffset)

		// update PE
		redPeGlobal = 0
		for elementId := elementOffset; elementId < nElements; elementId++ {
			if cm.ElInfo[elementId].ElType == IDDSE {
				continue
			}
			nChannels := cm.ElInfo[elementId].NChannelsInEl
			peData := &qcElement[elementId].PeData

			calcPe(psyOutElement[elementId].PsyOutChannel[:],
				qcElement[elementId].QcOutChannel[:], peData, nChannels)
			redPeGlobal += int(peData.pe)
		}
	}

	if redPeGlobal > desiredPe {
		// reduce pe by reducing minSnr requirements
		reduceMinSnr(cm, qcElement, psyOutElement, pAhFlag,
			int(fMultI(fl2fxconstDBL(0.15), int32(desiredPe)))+desiredPe, &redPeGlobal,
			processElements, elementOffset)

		// reduce pe by allowing additional spectral holes
		allowMoreHoles(cm, qcElement, psyOutElement, adjThrStateElement,
			pAhFlag, desiredPe, redPeGlobal, processElements, elementOffset)
	}
}
