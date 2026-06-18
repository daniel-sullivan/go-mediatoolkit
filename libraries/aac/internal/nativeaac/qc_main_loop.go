// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the top-level rate-control DRIVER /
// orchestration tier of the FDK-AAC quantizer/coder, translated from
// libAACenc/src/qc_main.cpp. It wires together the already-ported leaf kernels —
// CalcFormFactor / peCalculation / ChannelElementWrite (QCMainPrepare),
// getMinimalStaticBitdemand / distributeElementDynBits / DistributeBits
// (prepareBitDistribution), EstimateScaleFactors / QuantizeSpectrum /
// calcMaxValueInSfb / dynBitCount / AdjustThresholds / BitResRedistribution /
// reduceBitConsumption / crashRecovery (QCMain), and FinalizeBitConsumption — to
// drive the quantization + bit-allocation convergence loop.
//
// Scope: AAC-LC CBR only. The VBR / two-pass / superframe (nSubFrames>1) /
// HE-AAC-SBR / ELD branches are excluded; this port carries the single-AU
// (nSubFrames==1) CBR-adjustment path the FDK takes for AOT_AAC_LC. Every value
// is a fixed-point INT in the integer domain (no FP), so the loop is bit-exact
// regardless of vectorization and asserts EXACT int equality against the genuine
// vendored fdk reference.
//
// 1:1 fidelity: control flow, the do/while convergence structure, the &~7 / %8
// alignment masks, the truncating integer divisions and the saturating clamps
// match the C exactly. Every function cites its C counterpart file:line.

package nativeaac

// maxQuantValue mirrors MAX_QUANT (quantize.h:110): the largest representable
// quantized spectral magnitude; an overflow forces a global-gain increase.
const maxQuantValue = 8191

// aacencDzqBrThr mirrors AACENC_DZQ_BR_THR (qc_main.cpp:115): dead-zone quantizer
// bitrate threshold.
const aacencDzqBrThr = 32000

// QCMainPrepare is the 1:1 port of FDKaacEnc_QCMainPrepare (qc_main.cpp:435): for
// one element compute the spectral form factor (CalcFormFactor), the perceptual
// entropy without reduction (peCalculation), and the static side-info bit demand
// (ChannelElementWrite, minCnt==0). The mdctSpectrum each channel's CalcFormFactor
// reads is QcOutChannel.MdctSpectrum.
func QCMainPrepare(elInfo *ElementInfo, adjThrStateElement *atsElement,
	psyOutElement *PsyOutElement, qcOutElement *QcOutElement, aot int,
	syntaxFlags uint32, epConfig int8) EncoderError {
	nChannels := elInfo.NChannelsInEl

	psyOutChannel := psyOutElement.PsyOutChannel[:]

	// CalcFormFactor reads qcOutChannel[j]->mdctSpectrum in C; the leaf port
	// takes the per-channel mdct buffers as an explicit slice argument.
	mdct := make([][]int32, nChannels)
	for ch := 0; ch < nChannels; ch++ {
		mdct[ch] = qcOutElement.QcOutChannel[ch].MdctSpectrum[:]
	}
	CalcFormFactor(qcOutElement.QcOutChannel[:], psyOutChannel, mdct, nChannels)

	// prepare and calculate PE without reduction
	peCalculation(&qcOutElement.PeData, psyOutChannel, qcOutElement.QcOutChannel[:],
		&psyOutElement.ToolsInfo, adjThrStateElement, nChannels)

	return ChannelElementWrite(nil, elInfo, nil, psyOutElement,
		psyOutElement.PsyOutChannel[:], syntaxFlags, aot, epConfig,
		&qcOutElement.StaticBitsUsed, 0)
}

// prepareBitDistribution is the 1:1 port of FDKaacEnc_prepareBitDistribution
// (qc_main.cpp:615): compute the granted/max dynamic bits for the sub frame,
// guard against a bit-reservoir underrun (crash-recovery escape hatch),
// distribute the dynamic bits across elements (distributeElementDynBits) and run
// DistributeBits per element to obtain its granted/corrected PE, accumulating the
// total available bits and total corrected granted PE.
func prepareBitDistribution(hQC *QcState, psyOut []*PsyOut, qcOut []*QcOut,
	cm *ChannelMapping, qcElement [][8]*QcOutElement, avgTotalBits int,
	totalAvailableBits, avgTotalDynBits *int) EncoderError {
	// get maximal allowed dynamic bits
	qcOut[0].GrantedDynBits = (fixMin(hQC.MaxBitsPerFrame, avgTotalBits) - hQC.GlobHdrBits) &^ 7
	qcOut[0].GrantedDynBits -= qcOut[0].GlobalExtBits + qcOut[0].StaticBits + qcOut[0].ElementExtBits
	qcOut[0].MaxDynBits = (hQC.MaxBitsPerFrame &^ 7) -
		(qcOut[0].GlobalExtBits + qcOut[0].StaticBits + qcOut[0].ElementExtBits)

	// assure that enough bits are available
	if (qcOut[0].GrantedDynBits + hQC.BitResTot) < 0 {
		// crash recovery allows to reduce static bits to a minimum
		if (qcOut[0].GrantedDynBits + hQC.BitResTot) <
			(getMinimalStaticBitdemand(cm, psyOut) - qcOut[0].StaticBits) {
			return AacEncBitresTooLow
		}
	}

	// distribute dynamic bits to each element
	distributeElementDynBits(hQC, qcElement[0], cm, qcOut[0].GrantedDynBits)

	*avgTotalDynBits = 0

	*totalAvailableBits = avgTotalBits

	// sum up corrected granted PE
	qcOut[0].TotalGrantedPeCorr = 0

	for i := 0; i < cm.NElements; i++ {
		elInfo := cm.ElInfo[i]
		nChannels := elInfo.NChannelsInEl

		if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
			grantedPe, grantedPeCorr := distributeBits(
				hQC.HAdjThr, hQC.HAdjThr.adjThrStateElem[i],
				psyOut[0].PsyOutElement[i].PsyOutChannel[:], &qcElement[0][i].PeData,
				nChannels, psyOut[0].PsyOutElement[i].CommonWindow,
				qcElement[0][i].GrantedDynBits, hQC.ElementBits[i].BitResLevelEl,
				hQC.ElementBits[i].MaxBitResBitsEl, hQC.MaxBitFac, hQC.BitResMode)
			qcElement[0][i].GrantedPe = grantedPe
			qcElement[0][i].GrantedPeCorr = grantedPeCorr

			*totalAvailableBits += hQC.ElementBits[i].BitResLevelEl
			// get total corrected granted PE
			qcOut[0].TotalGrantedPeCorr += qcElement[0][i].GrantedPeCorr
		}
	}

	*totalAvailableBits = fixMin(hQC.MaxBitsPerFrame, *totalAvailableBits)

	return AacEncOK
}

// reduceBitConsumption is the 1:1 port of FDKaacEnc_reduceBitConsumption
// (qc_main.cpp:1119): on a constraint miss, either bump the global gain of the
// offending channels (iterations < maxIterations), or at the iteration limit
// invoke crashRecovery (or a final +1 gain bump), recalculating quantization on
// the next pass. Returns AAC_ENC_QUANT_ERROR when no further reduction is
// possible.
func reduceBitConsumption(iterations *int, maxIterations, gainAdjustment int,
	chConstraintsFulfilled, calculateQuant []int, nChannels int,
	psyOutElement *PsyOutElement, qcOut *QcOut, qcOutElement *QcOutElement,
	elBits *ElementBits, aot int, syntaxFlags uint32, epConfig int8) EncoderError {
	// SOLVING PROBLEM
	if *iterations < maxIterations {
		// increase gain (+ next iteration)
		for ch := 0; ch < nChannels; ch++ {
			if chConstraintsFulfilled[ch] == 0 {
				qcOutElement.QcOutChannel[ch].GlobalGain += gainAdjustment
				calculateQuant[ch] = 1 // global gain changed, recalculate quantization next iteration
			}
		}
	} else if *iterations == maxIterations {
		if qcOutElement.DynBitsUsed == 0 {
			return AacEncQuantError
		}
		// crash recovery
		bitsToSave := fixMax(
			(qcOutElement.DynBitsUsed+8)-(elBits.BitResLevelEl+qcOutElement.GrantedDynBits),
			(qcOutElement.DynBitsUsed+qcOutElement.StaticBitsUsed+8)-elBits.MaxBitsEl)
		if bitsToSave > 0 {
			crashRecovery(nChannels, psyOutElement, qcOut, qcOutElement,
				bitsToSave, aot, syntaxFlags, epConfig)
		} else {
			for ch := 0; ch < nChannels; ch++ {
				qcOutElement.QcOutChannel[ch].GlobalGain += 1
			}
		}
		for ch := 0; ch < nChannels; ch++ {
			calculateQuant[ch] = 1
		}
	} else {
		// *iterations > maxIterations
		return AacEncQuantError
	}
	*iterations++

	return AacEncOK
}

// QCMain is the 1:1 port of FDKaacEnc_QCMain (qc_main.cpp:788): the top-level
// quantization + rate-control convergence loop for one access unit. It
// redistributes the bit reservoir (BitResRedistribution), computes granted
// dynamic bits and per-element PE (prepareBitDistribution), adjusts thresholds
// (AdjustThresholds), then iterates EstimateScaleFactors -> QuantizeSpectrum ->
// calcMaxValueInSfb -> dynBitCount until the quantized spectrum fits the frame
// budget (reduceBitConsumption / crashRecovery escalate on a miss). nSubFrames is
// fixed to 1 (single-AU AAC-LC); the superframe path is excluded.
func QCMain(hQC *QcState, psyOut []*PsyOut, qcOut []*QcOut, avgTotalBits int,
	cm *ChannelMapping, aot int, syntaxFlags uint32, epConfig int8) EncoderError {
	var errorStatus EncoderError = AacEncOK
	avgTotalDynBits := 0 // maximal allowed dynamic bits for all frames
	totalAvailableBits := 0
	const nSubFrames = 1
	isCBRAdjustment := 0
	if isConstantBitrateMode(hQC.BitrateMode) || hQC.BitResMode != AacencBrModeFull {
		isCBRAdjustment = 1
	}

	// redistribute total bitreservoir to elements
	bitResAvgBits := avgTotalBits
	if isCBRAdjustment == 0 {
		bitResAvgBits = hQC.MaxBitsPerFrame
	}
	errorStatus = BitResRedistribution(hQC, cm, bitResAvgBits)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	// helper pointer: work on a copy of qcChannel and qcElement
	var qcElement [nSubFrames][8]*QcOutElement
	for i := 0; i < cm.NElements; i++ {
		elInfo := cm.ElInfo[i]
		if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
			for c := 0; c < nSubFrames; c++ {
				qcElement[c][i] = qcOut[c].QcElement[i]
			}
		}
	}

	// calc granted dynamic bits for sub frame and distribute it to each element
	prepBits := avgTotalBits
	if isCBRAdjustment == 0 {
		prepBits = hQC.MaxBitsPerFrame
	}
	errorStatus = prepareBitDistribution(hQC, psyOut, qcOut, cm, qcElement[:],
		prepBits, &totalAvailableBits, &avgTotalDynBits)
	if errorStatus != AacEncOK {
		return errorStatus
	}

	for c := 0; c < nSubFrames; c++ {
		// for CBR and VBR mode
		adjustThresholds(hQC.HAdjThr, qcElement[c][:], qcOut[c],
			psyOut[c].PsyOutElement[:], isCBRAdjustment, cm)
	}

	var iterations [nSubFrames][8]int
	var chConstraintsFulfilled [nSubFrames][8][2]int
	var calculateQuant [nSubFrames][8][2]int
	var constraintsFulfilled [nSubFrames][8]int

	for c := 0; c < nSubFrames; c++ {
		for i := 0; i < cm.NElements; i++ {
			elInfo := cm.ElInfo[i]
			nChannels := elInfo.NChannelsInEl

			if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
				// Turn thresholds into scalefactors, optimize bit consumption and verify conformance
				mdct := make([][]int32, nChannels)
				for ch := 0; ch < nChannels; ch++ {
					mdct[ch] = qcElement[c][i].QcOutChannel[ch].MdctSpectrum[:]
				}
				EstimateScaleFactors(
					psyOut[c].PsyOutElement[i].PsyOutChannel[:],
					qcElement[c][i].QcOutChannel[:], mdct, hQC.InvQuant,
					hQC.DZoneQuantEnable != 0, cm.ElInfo[i].NChannelsInEl)

				constraintsFulfilled[c][i] = 1
				iterations[c][i] = 0

				for ch := 0; ch < nChannels; ch++ {
					chConstraintsFulfilled[c][i][ch] = 1
					calculateQuant[c][i][ch] = 1
				}
			}
		}

		qcOut[c].UsedDynBits = -1
	}

	quantizationDone := 0
	sumDynBitsConsumedTotal := 0
	decreaseBitConsumption := -1 // no direction yet!

	// -start- Quantization loop ...
	for {
		quantizationDone = 0

		c := 0 // get frame to process

		for i := 0; i < cm.NElements; i++ {
			elInfo := cm.ElInfo[i]
			nChannels := elInfo.NChannelsInEl

			if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
				for { // until element bits < nChannels*MIN_BUFSIZE_PER_EFF_CHAN
					for { // until spectral values < MAX_QUANT
						if constraintsFulfilled[c][i] == 0 {
							gainAdjustment := -1
							if decreaseBitConsumption != 0 {
								gainAdjustment = 1
							}
							if errorStatus = reduceBitConsumption(
								&iterations[c][i], hQC.MaxIterations,
								gainAdjustment,
								chConstraintsFulfilled[c][i][:], calculateQuant[c][i][:],
								nChannels, psyOut[c].PsyOutElement[i], qcOut[c],
								qcElement[c][i], hQC.ElementBits[i], aot, syntaxFlags,
								epConfig); errorStatus != AacEncOK {
								return errorStatus
							}
						}

						constraintsFulfilled[c][i] = 1

						// quantize spectrum (per each channel)
						for ch := 0; ch < nChannels; ch++ {
							chConstraintsFulfilled[c][i][ch] = 1

							if calculateQuant[c][i][ch] != 0 {
								qcOutCh := qcElement[c][i].QcOutChannel[ch]
								psyOutCh := psyOut[c].PsyOutElement[i].PsyOutChannel[ch]

								calculateQuant[c][i][ch] = 0 // calculate quantization only if necessary

								fdkaacEncQuantizeSpectrum(
									psyOutCh.SfbCnt, psyOutCh.MaxSfbPerGroup,
									psyOutCh.SfbPerGroup, psyOutCh.SfbOffsets[:],
									qcOutCh.MdctSpectrum[:], qcOutCh.GlobalGain, qcOutCh.Scf[:],
									qcOutCh.QuantSpec[:], hQC.DZoneQuantEnable != 0)

								if calcMaxValueInSfb(
									psyOutCh.SfbCnt, psyOutCh.MaxSfbPerGroup,
									psyOutCh.SfbPerGroup, psyOutCh.SfbOffsets[:],
									qcOutCh.QuantSpec[:],
									qcOutCh.MaxValueInSfb[:]) > maxQuantValue {
									chConstraintsFulfilled[c][i][ch] = 0
									constraintsFulfilled[c][i] = 0
									// if quantized value out of range; increase global gain!
									decreaseBitConsumption = 1
								}
							}
						}

						if constraintsFulfilled[c][i] != 0 {
							break
						}
					} // does not regard bit consumption

					qcElement[c][i].DynBitsUsed = 0 // reset dynamic bits

					// quantization valid in current channel!
					for ch := 0; ch < nChannels; ch++ {
						qcOutCh := qcElement[c][i].QcOutChannel[ch]
						psyOutCh := psyOut[c].PsyOutElement[i].PsyOutChannel[ch]

						// count dynamic bits
						chDynBits := dynBitCount(
							hQC.HBitCounter, qcOutCh.QuantSpec[:], qcOutCh.MaxValueInSfb[:],
							qcOutCh.Scf[:], psyOutCh.LastWindowSequence, psyOutCh.SfbCnt,
							psyOutCh.MaxSfbPerGroup, psyOutCh.SfbPerGroup,
							psyOutCh.SfbOffsets[:], &qcOutCh.SectionData, psyOutCh.NoiseNrg[:],
							psyOutCh.IsBook[:], psyOutCh.IsScale[:], uint(syntaxFlags))

						// sum up dynamic channel bits
						qcElement[c][i].DynBitsUsed += chDynBits
					}

					// save dynBitsUsed for correction of bits2pe relation
					if hQC.HAdjThr.adjThrStateElem[i].dynBitsLast == -1 {
						hQC.HAdjThr.adjThrStateElem[i].dynBitsLast = qcElement[c][i].DynBitsUsed
					}

					// hold total bit consumption in present element below maximum allowed
					if qcElement[c][i].DynBitsUsed >
						((nChannels * minBufsizePerEffChan) -
							qcElement[c][i].StaticBitsUsed -
							qcElement[c][i].ExtBitsUsed) {
						constraintsFulfilled[c][i] = 0
					}

					if constraintsFulfilled[c][i] != 0 {
						break
					}
				}
			}
		}

		// update dynBits of current subFrame
		updateUsedDynBits(&qcOut[c].UsedDynBits, qcElement[c], cm)

		// get total consumed bits, dyn bits in all sub frames have to be valid
		sumDynBitsConsumedTotal = getTotalConsumedDynBits(qcOut, nSubFrames)

		if sumDynBitsConsumedTotal == -1 {
			quantizationDone = 0 // bit consumption not valid in all sub frames
		} else {
			sumBitsConsumedTotal := getTotalConsumedBits(
				qcOut, qcElement[:], cm, hQC.GlobHdrBits, nSubFrames)

			// in all frames are valid dynamic bits
			if ((sumBitsConsumedTotal < totalAvailableBits) ||
				sumDynBitsConsumedTotal == 0) &&
				(decreaseBitConsumption == 1) &&
				checkMinFrameBitsDemand(qcOut, hQC.MinBitsPerFrame, nSubFrames) != 0 {
				quantizationDone = 1 // exit bit adjustment
			}
			if sumBitsConsumedTotal > totalAvailableBits &&
				(decreaseBitConsumption == 0) {
				quantizationDone = 0 // reset!
			}
		}

		emergencyIterations := 1
		dynBitsOvershoot := 0

		for c = 0; c < nSubFrames; c++ {
			for i := 0; i < cm.NElements; i++ {
				elInfo := cm.ElInfo[i]
				if elInfo.ElType == IDSCE || elInfo.ElType == IDCPE || elInfo.ElType == IDLFE {
					// iteration limitation
					v := 1
					if iterations[c][i] < hQC.MaxIterations {
						v = 0
					}
					emergencyIterations &= v
				}
			}
			// detection if used dyn bits exceeds the maximal allowed criterion
			if qcOut[c].UsedDynBits > qcOut[c].MaxDynBits {
				dynBitsOvershoot |= 1
			}
		}

		if quantizationDone == 0 || dynBitsOvershoot != 0 {
			sumBitsConsumedTotal := getTotalConsumedBits(
				qcOut, qcElement[:], cm, hQC.GlobHdrBits, nSubFrames)

			if (sumDynBitsConsumedTotal >= avgTotalDynBits) ||
				(sumDynBitsConsumedTotal == 0) {
				quantizationDone = 1
			}
			if emergencyIterations != 0 && (sumBitsConsumedTotal < totalAvailableBits) {
				quantizationDone = 1
			}
			if (sumBitsConsumedTotal > totalAvailableBits) ||
				checkMinFrameBitsDemand(qcOut, hQC.MinBitsPerFrame, nSubFrames) == 0 {
				quantizationDone = 0
			}
			if (sumBitsConsumedTotal < totalAvailableBits) &&
				checkMinFrameBitsDemand(qcOut, hQC.MinBitsPerFrame, nSubFrames) != 0 {
				decreaseBitConsumption = 0
			} else {
				decreaseBitConsumption = 1
			}

			if dynBitsOvershoot != 0 {
				quantizationDone = 0
				decreaseBitConsumption = 1
			}

			// reset constraints fullfilled flags
			for a := range constraintsFulfilled {
				for b := range constraintsFulfilled[a] {
					constraintsFulfilled[a][b] = 0
				}
			}
			for a := range chConstraintsFulfilled {
				for b := range chConstraintsFulfilled[a] {
					for d := range chConstraintsFulfilled[a][b] {
						chConstraintsFulfilled[a][b][d] = 0
					}
				}
			}
		}

		if quantizationDone != 0 {
			break
		}
	}

	// ... -end- Quantization loop

	return AacEncOK
}
