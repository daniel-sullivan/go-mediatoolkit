// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Bit-reservoir + pe-correction DRIVER stage of the AAC encoder
// threshold-adjustment loop, ported 1:1 from the vendored FDK-AAC reference
// libAACenc/src/adj_thr.cpp. FDKaacEnc_bitresCalcBitFac maps the bit-reservoir
// fill level + perceptual entropy to a spend/save bit factor (driving
// calcBitSave/calcBitSpend leaves and the peMin/peMax smoothing);
// FDKaacEnc_FDKaacEnc_calcPeCorrection / FDKaacEnc_calcPeCorrectionLowBitRes adapt
// the pe-correction factor from the previous frame's granted-vs-used bit balance;
// FDKaacEnc_DistributeBits converts the granted dynamic-bit budget to a desired pe
// (corrected) for the current element/frame.
//
// CBR/AAC-LC path. The bitres-mode switch covers all three AACENC_BR_MODE values
// (FULL drives the high-bitres correction, DISABLED/REDUCED the low-bitres one).
//
// Pure fixed-point: every value is an int32 FIXP_DBL Q-format / INT with carried
// block exponents — bit-identical to the C, no float, no transcendental. fMult ==
// fixmul_DD == fixmulDDarm8; schurDiv/fDivNorm/fMultAddDiv2/fMult/fMin/fMax/
// scaleValue are the already-verified leaf kernels.

// bitresCalcBitFac is the 1:1 port of FDKaacEnc_bitresCalcBitFac
// (adj_thr.cpp:2347-2431): calculate the factor of dynamic bits to spend for one
// frame from the bit-reservoir fullness and pe, then smooth peMin/peMax. Returns
// (bitresFac, bitresFac_e). adjThrChan.peMin/peMax are updated in place.
func bitresCalcBitFac(bitresBits, maxBitresBits, pe, lastWindowSequence, avgBits int,
	maxBitFac int32, adjThr *adjThrState, adjThrChan *atsElement) (bitresFac int32, bitresFacE int) {
	var bresParam *bresParam
	var bitsaveSlope, bitspendSlope int32
	fillLevelFix := maxvalDBL

	if lastWindowSequence != encShortWindow {
		bresParam = &adjThr.bresParamLong
		bitsaveSlope = fl2fxconstDBL(0.466666666)
		bitspendSlope = fl2fxconstDBL(0.666666666)
	} else {
		bresParam = &adjThr.bresParamShort
		bitsaveSlope = int32(0x2E8BA2E9)
		bitspendSlope = int32(0x7fffffff)
	}

	// fillLevel = (bitresBits+avgBits) / (maxBitresBits + avgBits)
	//
	// C uses the 2-arg fDivNorm(bitresBits, maxBitresBits) overload
	// (fixpoint_math.cpp:481), which returns the ratio at exponent 0 — i.e. it
	// scaleValue()s the normalised 3-arg result by its exponent. The 3-arg
	// fDivNorm (fixpoint_pow.go) returns a left-normalised mantissa plus a
	// separate exponent; discarding that exponent yields half (or a quarter…) of
	// the true ratio whenever bitresBits < maxBitresBits, so fDivNorm2 (the 2-arg
	// port that applies scaleValue) is required here.
	if bitresBits < maxBitresBits {
		fillLevelFix = fDivNorm2(int32(bitresBits), int32(maxBitresBits))
	}

	pex := fixMax(pe, adjThrChan.peMin)
	pex = fixMin(pex, adjThrChan.peMax)

	bitSave := calcBitSave(fillLevelFix, bresParam.clipSaveLow, bresParam.clipSaveHigh,
		bresParam.minBitSave, bresParam.maxBitSave, bitsaveSlope)

	bitSpend := calcBitSpend(fillLevelFix, bresParam.clipSpendLow, bresParam.clipSpendHigh,
		bresParam.minBitSpend, bresParam.maxBitSpend, bitspendSlope)

	slope := schurDiv(int32(pex-adjThrChan.peMin), int32(adjThrChan.peMax-adjThrChan.peMin), 31)

	// scale down by 1 bit because the addition result can be > 1 (< 2)
	bitresFac = (maxvalDBL >> 1) - (bitSave >> 1)
	bitresFacE = 1                                               // exp=1
	bitresFac = fMultAddDiv2(bitresFac, slope, bitSpend+bitSave) // exp=1

	// limit bitresFac for small bitreservoir
	fillLevel, fillLevelE := fDivNorm(int32(bitresBits), int32(avgBits))
	if fillLevelE < 0 {
		fillLevel = scaleValue(fillLevel, fillLevelE)
		fillLevelE = 0
	}
	// shift down by 1 because of summation
	fillLevel >>= 1
	fillLevelE += 1
	// this summation:
	fillLevel += scaleValue(fl2fxconstDBL(0.7), -fillLevelE)
	// set bitresFac to same exponent as fillLevel
	if scaleValue(bitresFac, -fillLevelE+1) > fillLevel {
		bitresFac = fillLevel
		bitresFacE = int(fillLevelE)
	}

	// limit bitresFac for high bitrates
	if scaleValue(bitresFac, int32(bitresFacE)-(dfractBits-1-24)) > maxBitFac {
		bitresFac = maxBitFac
		bitresFacE = dfractBits - 1 - 24
	}

	adjustPeMinMax(pe, &adjThrChan.peMin, &adjThrChan.peMax)

	return bitresFac, bitresFacE
}

// calcPeCorrection is the 1:1 port of FDKaacEnc_FDKaacEnc_calcPeCorrection
// (adj_thr.cpp:2604-2662): adapt the pe-correction factor for high bitreservoir
// from the previous frame's pe vs bits-derived pe ratio, with a dead zone and
// asymmetric adaptation speed. Returns (correctionFac_m, correctionFac_e).
func calcPeCorrection(correctionFacMIn int32, peAct, peLast, bitsLast int,
	bits2PeFactorM int32, bits2PeFactorE int) (correctionFacM int32, correctionFacE int) {

	// (peAct < 1.5f * peLast) && (peAct > 0.7f * peLast) etc.
	if bitsLast > 0 && peAct < (peLast*3)/2 && 10*peAct > 7*peLast &&
		bits2pe2(bitsLast, fMult(fl2fxconstDBL(1.2/2.0), bits2PeFactorM), bits2PeFactorE+1) > peLast &&
		bits2pe2(bitsLast, fMult(fl2fxconstDBL(0.65), bits2PeFactorM), bits2PeFactorE) < peLast {

		corrFac := correctionFacMIn

		denum := int32(bits2pe2(bitsLast, bits2PeFactorM, bits2PeFactorE))
		newFac, scaling := fDivNorm(int32(peLast), denum)

		// dead zone, newFac and corrFac are scaled by 0.5
		if int32(peLast) <= denum { // ratio <= 1.f
			newFac = fMax(
				scaleValue(fMin(fMult(fl2fxconstDBL(1.1/2.0), newFac),
					scaleValue(fl2fxconstDBL(1.0/2.0), -scaling)), scaling),
				fl2fxconstDBL(0.85/2.0))
		} else { // ratio < 1.f
			newFac = fMax(
				fMin(scaleValue(fMult(fl2fxconstDBL(0.9/2.0), newFac), scaling),
					fl2fxconstDBL(1.15/2.0)),
				fl2fxconstDBL(1.0/2.0))
		}

		if (newFac > fl2fxconstDBL(1.0/2.0) && corrFac < fl2fxconstDBL(1.0/2.0)) ||
			(newFac < fl2fxconstDBL(1.0/2.0) && corrFac > fl2fxconstDBL(1.0/2.0)) {
			corrFac = fl2fxconstDBL(1.0 / 2.0)
		}

		// faster adaptation towards 1.0, slower in the other direction
		if (corrFac < fl2fxconstDBL(1.0/2.0) && newFac < corrFac) ||
			(corrFac > fl2fxconstDBL(1.0/2.0) && newFac > corrFac) {
			corrFac = fMult(fl2fxconstDBL(0.85), corrFac) + fMult(fl2fxconstDBL(0.15), newFac)
		} else {
			corrFac = fMult(fl2fxconstDBL(0.7), corrFac) + fMult(fl2fxconstDBL(0.3), newFac)
		}

		corrFac = fMax(fMin(corrFac, fl2fxconstDBL(1.15/2.0)), fl2fxconstDBL(0.85/2.0))

		return corrFac, 1
	}
	return fl2fxconstDBL(1.0 / 2.0), 1
}

// calcPeCorrectionLowBitRes is the 1:1 port of
// FDKaacEnc_calcPeCorrectionLowBitRes (adj_thr.cpp:2664-2721): adapt the
// pe-correction factor for low/disabled bitreservoir from the previous frame's
// granted-vs-used dynamic-bit balance. Returns (correctionFac_m, correctionFac_e).
func calcPeCorrectionLowBitRes(correctionFacMIn int32, peLast, bitsLast, bitresLevel, nChannels int,
	bits2PeFactorM int32, bits2PeFactorE int) (correctionFacM int32, correctionFacE int) {

	amp := fl2fxconstDBL(0.005)    // FL2FXCONST_DBL(0.005)
	maxDiff := fl2fxconstDBL(0.25) // FL2FXCONST_DBL(0.25f)

	if bitsLast > 0 {
		// Estimate deviation of granted and used dynamic bits in previous frame
		bitsBalLast := peLast - bits2pe2(bitsLast, bits2PeFactorM, bits2PeFactorE)

		// reserve n bits per channel
		headroom := 100 * nChannels
		if bitresLevel >= 50*nChannels {
			headroom = 0
		}
		headroom = bits2pe2(headroom, bits2PeFactorM, bits2PeFactorE)

		denominator := int32(bits2pe2(bitresLevel, bits2PeFactorM, bits2PeFactorE)) + int32(headroom)

		var diff int32
		var scaling int32
		if bitsBalLast >= headroom {
			d, sc := fDivNorm(int32(bitsBalLast-headroom), denominator)
			diff = fMult(amp, d)
			scaling = sc
		} else {
			d, sc := fDivNorm(-int32(bitsBalLast-headroom), denominator)
			diff = -fMult(amp, d)
			scaling = sc
		}

		scaling -= 1 // divide by 2

		if scaling <= 0 {
			diff = fMax(fMin(diff>>uint(-scaling), maxDiff>>1), -maxDiff>>1)
		} else {
			diff = fMax(fMin(diff, maxDiff>>uint(1+scaling)), -maxDiff>>uint(1+scaling)) << uint(scaling)
		}

		// corrFac += diff; corrFac = max ( min ( corrFac/2, 1/2, 0.75/2 ) )
		correctionFacM = fMax(fMin(correctionFacMIn+diff, fl2fxconstDBL(1.0/2.0)), fl2fxconstDBL(0.75/2.0))
		return correctionFacM, 1
	}
	return fl2fxconstDBL(0.75 / 2.0), 1
}

// distributeBits is the 1:1 port of FDKaacEnc_DistributeBits
// (adj_thr.cpp:2723-2798): convert the granted dynamic-bit budget to a desired pe
// (grantedPe) and its corrected variant (grantedPeCorr), updating the per-element
// pe-correction factor and peLast/dynBitsLast.
func distributeBits(adjThrState *adjThrState, adjThrStateElement *atsElement,
	psyOutChannel []*PsyOutChannel, peData *peData, nChannels, commonWindow,
	grantedDynBits, bitresBits, maxBitresBits int, maxBitFac int32,
	bitResMode AacencBitresMode) (grantedPe, grantedPeCorr int) {

	noRedPe := int(peData.pe)

	// prefer short windows for calculation of bitFactor
	curWindowSequence := encLongWindow
	if nChannels == 2 {
		if psyOutChannel[0].LastWindowSequence == encShortWindow ||
			psyOutChannel[1].LastWindowSequence == encShortWindow {
			curWindowSequence = encShortWindow
		}
	} else {
		curWindowSequence = psyOutChannel[0].LastWindowSequence
	}

	if grantedDynBits >= 1 {
		if bitResMode != AacencBrModeFull {
			// small or disabled bitreservoir
			grantedPe = bits2pe2(grantedDynBits,
				adjThrStateElement.bits2PeFactorM, adjThrStateElement.bits2PeFactorE)
		} else {
			// factor dependent on current fill level and pe
			bitFactor, bitFactorE := bitresCalcBitFac(bitresBits, maxBitresBits, noRedPe,
				curWindowSequence, grantedDynBits, maxBitFac, adjThrState, adjThrStateElement)

			// desired pe for actual frame
			grantedPe = bits2pe2(grantedDynBits,
				fMult(bitFactor, adjThrStateElement.bits2PeFactorM),
				adjThrStateElement.bits2PeFactorE+bitFactorE)
		}
	} else {
		grantedPe = 0 // prevent division by 0
	}

	// correction of pe value
	switch bitResMode {
	case AacencBrModeDisabled, AacencBrModeReduced:
		// correction of pe value for low bitres
		adjThrStateElement.peCorrectionFactorM, adjThrStateElement.peCorrectionFactorE =
			calcPeCorrectionLowBitRes(adjThrStateElement.peCorrectionFactorM,
				adjThrStateElement.peLast, adjThrStateElement.dynBitsLast, bitresBits, nChannels,
				adjThrStateElement.bits2PeFactorM, adjThrStateElement.bits2PeFactorE)
	default: // AacencBrModeFull
		// correction of pe value for high bitres
		adjThrStateElement.peCorrectionFactorM, adjThrStateElement.peCorrectionFactorE =
			calcPeCorrection(adjThrStateElement.peCorrectionFactorM,
				fixMin(grantedPe, noRedPe), adjThrStateElement.peLast, adjThrStateElement.dynBitsLast,
				adjThrStateElement.bits2PeFactorM, adjThrStateElement.bits2PeFactorE)
	}

	grantedPeCorr = int(fMult(int32(grantedPe)<<qAvgBits, adjThrStateElement.peCorrectionFactorM) >>
		uint(qAvgBits-adjThrStateElement.peCorrectionFactorE))

	// update last pe
	adjThrStateElement.peLast = grantedPe
	adjThrStateElement.dynBitsLast = -1

	return grantedPe, grantedPeCorr
}
