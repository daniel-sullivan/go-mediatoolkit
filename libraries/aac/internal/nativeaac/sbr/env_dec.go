// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// SBR envelope + noise-floor dequantization, ported 1:1 from the vendored
// Fraunhofer FDK-AAC reference libSBRdec/src/env_dec.cpp. The main entry point is
// DecodeSbrData (decodeSbrData, env_dec.cpp:230), which converts the raw delta-/
// PCM-coded envelope and noise scale-factor symbols (parsed by env_extr.cpp and
// Huffman-decoded by huff_dec.cpp) into the pseudo-float energy/noise level
// arrays the gain calculation (env_calc.cpp) consumes.
//
// HE-AAC v1 (STD) scope: the concealment paths (leanSbrConcealment,
// timeCompensateFirstEnvelope, checkEnvelopeData) and the ENV_EXP_FRACT!=0
// fractional-exponent branches of requantizeEnvelopeData are ported faithfully
// because decodeEnvelope drives them, but the PVC / USAC / DRM / ELD-specific
// surrounding logic is excluded (decodeSbrData only takes the pvc_mode==0 path
// for AAC). ENV_EXP_FRACT is 0 (env_extr.h:119), so the fractional branches
// compile out exactly as in the reference scalar build.
//
// fdk-aac SBR is fixed-point: FIXP_SGL == int16 (Q1.15), FIXP_DBL == int32, the
// pseudo-float pack stores a 6-bit exponent (low) + mantissa (high) in an int16.
// All operations here are integer (no transcendental), so this is bit-exact in
// any build.

import (
	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// --- pseudo-float pack masks + rounding (env_extr.h:137-150) ----------------

// maskM extracts the mantissa of a pseudo-float envelope value: the high
// (FRACT_BITS-EXP_BITS) bits. MASK_M = (((1<<(FRACT_BITS-EXP_BITS))-1)<<EXP_BITS)
// with FRACT_BITS==16, EXP_BITS==6.
const maskM = (((1 << (fractBits - expBits)) - 1) << expBits) // env_extr.h:137

// rounding is the 0.5-offset for rounding the mantissa, ROUNDING ==
// (FIXP_SGL)(1<<(EXP_BITS-1)) (env_extr.h:148).
const rounding = int16(1 << (expBits - 1))

// fractBits (==FRACT_BITS) and dfractBits (==DFRACT_BITS) are declared in
// freq_sca.go / qmf_synthesis.go respectively; reused here.

// maxvalSGL is MAXVAL_SGL (common_fix.h:151), the FIXP_SGL saturation max.
const maxvalSGL = int16(0x7FFF)

// packEnvVal packs a mantissa (FIXP_SGL) and an exponent into one pseudo-float
// iEnvelope/noiseFloor int16, mirroring the C idiom
//
//	((FIXP_SGL)((SHORT)(FIXP_SGL)mant & MASK_M)) + (FIXP_SGL)((SHORT)(FIXP_SGL)exp & MASK_E)
//
// (env_dec.cpp:329-334, 363-368, 758-760). The (SHORT)& happens in int after
// sign-extension; the sum is then narrowed back to FIXP_SGL.
func packEnvVal(mant int16, exp int) int16 {
	return int16((int(mant) & maskM) + (int(int16(exp)) & maskE))
}

// SBR energy/decay constants (env_dec.cpp:141-152). ENV_EXP_FRACT==0, so
// SBR_ENERGY_PAN_OFFSET == 12, SBR_MAX_ENERGY == 35, DECAY == 1, DECAY_COUPLING
// == 1 (the #else branch since ENV_EXP_FRACT is 0).
const (
	sbrEnergyPanOffset = 12 << envExpFract // SBR_ENERGY_PAN_OFFSET
	sbrMaxEnergy       = 35 << envExpFract // SBR_MAX_ENERGY
	decay              = 1 << envExpFract  // DECAY
	decayCoupling      = 1                 // DECAY_COUPLING (#else, ENV_EXP_FRACT==0)
)

// --- mant/exp helpers (transcendent.h, FIXP_SGL variants) -------------------

// addMantExpSGL is the FIXP_SGL FDK_add_MantExp (transcendent.h:138-175):
// add a = a_m*2^a_e and b = b_m*2^b_e, returning the result as mantissa/exponent.
//
// C counterpart: FDK_add_MantExp (transcendent.h:138).
func addMantExpSGL(aM int16, aE int8, bM int16, bE int8) (sumM int16, sumE int8) {
	shift := int(aE) - int(bE)

	shiftAbs := shift
	if shiftAbs < 0 {
		shiftAbs = -shiftAbs
	}
	if shiftAbs >= dfractBits-1 {
		shiftAbs = dfractBits - 1
	}

	var shiftedMantissa, otherMantissa int32
	if shift > 0 {
		shiftedMantissa = fxSgl2FxDbl(bM) >> uint(shiftAbs)
		otherMantissa = fxSgl2FxDbl(aM)
		sumE = aE
	} else {
		shiftedMantissa = fxSgl2FxDbl(aM) >> uint(shiftAbs)
		otherMantissa = fxSgl2FxDbl(bM)
		sumE = bE
	}

	// shift by 1 bit to avoid overflow
	accu := (shiftedMantissa >> 1) + (otherMantissa >> 1)

	if accu >= (nativeaac.Fl2fxconstDBL(0.5)-1) || accu <= nativeaac.Fl2fxconstDBL(-0.5) {
		sumE++
	} else {
		accu = shiftedMantissa + otherMantissa
	}

	sumM = fxDbl2FxSgl(accu)
	return sumM, sumE
}

// divideMantExpSGL is the FIXP_SGL FDK_divide_MantExp (transcendent.h:226-280):
// compute a/b for a = a_m*2^a_e, b = b_m*2^b_e via the FDK_sbrDecoder_invTable
// lookup. a, b are energies (non-negative).
//
// C counterpart: FDK_divide_MantExp (transcendent.h:226).
func divideMantExpSGL(aM int16, aE int8, bM int16, bE int8) (resultM int16, resultE int8) {
	var bInvM int16 = 0 // FL2FXCONST_SGL(0.0f)

	preShift := nativeaac.CntLeadingZeros(fxSgl2FxDbl(bM)) // CntLeadingZeros(FX_SGL2FX_DBL(b_m))

	// shift = FRACT_BITS - 2 - INV_TABLE_BITS - preShift
	shift := fractBits - 2 - invTableBits - preShift

	var index int
	if shift < 0 {
		index = int(int32(bM) << uint(-shift))
	} else {
		index = int(int32(bM) >> uint(shift))
	}

	index &= (1 << (invTableBits + 1)) - 1
	index-- // remove offset of half an interval
	index = index >> 1

	if index >= 0 {
		bInvM = sbrInvTable[index]
	}

	// ratio_m = (index<0) ? FX_SGL2FX_DBL(a_m>>1) : fMultDiv2(bInv_m, a_m)
	var ratioM int32
	if index < 0 {
		ratioM = fxSgl2FxDbl(aM >> 1)
	} else {
		// fMultDiv2(FIXP_SGL, FIXP_SGL) == fixmuldiv2_SS == (a*b)
		ratioM = int32(bInvM) * int32(aM)
	}

	postShift := nativeaac.CntLeadingZeros(ratioM) - 1

	resultM = fxDbl2FxSgl(ratioM << uint(postShift))
	resultE = int8(int(aE) - int(bE) + 1 + preShift - postShift)
	return resultM, resultE
}

// --- table-index helpers (env_dec.cpp:157-215) ------------------------------

// indexLow2High converts a low-res scalefactor-band index to the high-res index.
//
// C counterpart: indexLow2High (env_dec.cpp:157).
func indexLow2High(offset, index, res int) int {
	if res == 0 {
		if offset >= 0 {
			if index < offset {
				return index
			}
			return 2*index - offset
		}
		offset = -offset
		if index < offset {
			return 2*index + index
		}
		return 2*index + offset
	}
	return index
}

// mapLowResEnergyVal stores currVal into the high-res prevData[] for delta
// coding in the next frame, mapping low-res bands to their high-res slots.
//
// C counterpart: mapLowResEnergyVal (env_dec.cpp:187).
func mapLowResEnergyVal(currVal int16, prevData []int16, offset, index, res int) {
	if res == 0 {
		if offset >= 0 {
			if index < offset {
				prevData[index] = currVal
			} else {
				prevData[2*index-offset] = currVal
				prevData[2*index+1-offset] = currVal
			}
		} else {
			offset = -offset
			if index < offset {
				prevData[3*index] = currVal
				prevData[3*index+1] = currVal
				prevData[3*index+2] = currVal
			} else {
				prevData[2*index+offset] = currVal
				prevData[2*index+1+offset] = currVal
			}
		}
	} else {
		prevData[index] = currVal
	}
}

// --- decodeSbrData entry point (env_dec.cpp:230) ----------------------------

// DecodeSbrData converts the raw envelope and noise-floor data of a parsed SBR
// frame to dequantized energy levels, applying delta decoding, requantization,
// concealment and (in coupling mode) channel unmapping. hDataRight/
// hPrevDataRight are nil for a single (mono) channel element.
//
// C counterpart: decodeSbrData (env_dec.cpp:230).
func DecodeSbrData(
	hHeaderData *SbrHeaderData,
	hDataLeft *SbrFrameData, hPrevDataLeft *SbrPrevFrameData,
	hDataRight *SbrFrameData, hPrevDataRight *SbrPrevFrameData,
) {
	var tempSfbNrgPrev [maxFreqCoeffs]int16

	// Save previous energy values to be able to reuse them later for concealment.
	copy(tempSfbNrgPrev[:], hPrevDataLeft.SfbNrgPrev[:])

	if hHeaderData.FrameError != 0 || hHeaderData.BsInfo.PvcMode == 0 {
		decodeEnvelope(hHeaderData, hDataLeft, hPrevDataLeft, hPrevDataRight)
	}
	// else: PVC mode (out of HE-AAC v1 scope; h_data_right asserted NULL).

	decodeNoiseFloorlevels(hHeaderData, hDataLeft, hPrevDataLeft)

	if hDataRight != nil {
		errLeft := hHeaderData.FrameError
		decodeEnvelope(hHeaderData, hDataRight, hPrevDataRight, hPrevDataLeft)
		decodeNoiseFloorlevels(hHeaderData, hDataRight, hPrevDataRight)

		if errLeft == 0 && hHeaderData.FrameError != 0 {
			// If an error occurs in the right channel where the left seemed ok,
			// apply concealment also on the left channel. Restore previous energy
			// values overwritten by the first decodeEnvelope() call.
			copy(hPrevDataLeft.SfbNrgPrev[:], tempSfbNrgPrev[:])
			decodeEnvelope(hHeaderData, hDataLeft, hPrevDataLeft, hPrevDataRight)
		}

		if hDataLeft.Coupling != couplingOff {
			sbrEnvelopeUnmapping(hHeaderData, hDataLeft, hDataRight)
		}
	}
}

// sbrEnvelopeUnmapping converts coupled (level/balance) channel energies and
// noise levels back to independent L/R data.
//
// C counterpart: sbr_envelope_unmapping (env_dec.cpp:292).
func sbrEnvelopeUnmapping(hHeaderData *SbrHeaderData, hDataLeft, hDataRight *SbrFrameData) {
	// 1. Unmap (already dequantized) coupled envelope energies.
	for i := 0; i < hDataLeft.NScaleFactors; i++ {
		tempRM := int16(int32(hDataRight.IEnvelope[i]) & maskM)
		tempRE := int8(int32(hDataRight.IEnvelope[i]) & maskE)

		tempRE -= 18 + nrgExpOffset // -18 = ld(UNMAPPING_SCALE / nChannels)
		tempLM := int16(int32(hDataLeft.IEnvelope[i]) & maskM)
		tempLE := int8(int32(hDataLeft.IEnvelope[i]) & maskE)

		tempLE -= nrgExpOffset

		// Calculate tempRight+1
		tempRplus1M, tempRplus1E := addMantExpSGL(tempRM, tempRE, nativeaac.Fl2fxconstSGL(0.5), 1)

		// 2 * tempLeft / (tempR+1)
		newRM, newRE := divideMantExpSGL(tempLM, tempLE+1, tempRplus1M, tempRplus1E)

		if newRM >= (maxvalSGL - rounding) {
			newRM >>= 1
			newRE += 1
		}

		// L = tempR * R
		newLM := fxDbl2FxSgl(nativeaac.FMultSS(tempRM, newRM))
		newLE := tempRE + newRE

		hDataRight.IEnvelope[i] = packEnvVal(newRM+rounding, int(newRE)+nrgExpOffset)
		hDataLeft.IEnvelope[i] = packEnvVal(newLM+rounding, int(newLE)+nrgExpOffset)
	}

	// 2. Dequantize and unmap coupled noise floor levels.
	n := int(hHeaderData.FreqBandData.NNfb) * int(hDataLeft.FrameInfo.NNoiseEnv)
	for i := 0; i < n; i++ {
		tempLE := int8(6 - int32(hDataLeft.SbrNoiseFloorLevel[i]))
		tempRE := int8(int32(hDataRight.SbrNoiseFloorLevel[i]) - 12) // SBR_ENERGY_PAN_OFFSET

		// Calculate tempR+1
		tempRplus1M, tempRplus1E := addMantExpSGL(nativeaac.Fl2fxconstSGL(0.5), 1+tempRE,
			nativeaac.Fl2fxconstSGL(0.5), 1)

		// 2*tempLeft/(tempR+1)
		newRM, newRE := divideMantExpSGL(nativeaac.Fl2fxconstSGL(0.5), tempLE+2,
			tempRplus1M, tempRplus1E)

		// L = tempR * R
		newLM := newRM
		newLE := newRE + tempRE
		hDataRight.SbrNoiseFloorLevel[i] = packEnvVal(newRM+rounding, int(newRE)+noiseExpOff)
		hDataLeft.SbrNoiseFloorLevel[i] = packEnvVal(newLM+rounding, int(newLE)+noiseExpOff)
	}
}

// leanSbrConcealment constructs a single-envelope (FIX-FIX) replacement frame on
// frame loss, fading the delta-coded energies down.
//
// C counterpart: leanSbrConcealment (env_dec.cpp:380).
func leanSbrConcealment(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData, hPrevData *SbrPrevFrameData) {
	currentStartPos := nativeaac.FMaxI(0, int(hPrevData.StopPos)-int(hHeaderData.NumberTimeSlots))
	currentStopPos := int(hHeaderData.NumberTimeSlots)

	// Use some settings of the previous frame.
	hSbrData.AmpResolutionCurrFrame = int(hPrevData.AmpRes)
	hSbrData.Coupling = hPrevData.Coupling
	for i := 0; i < maxInvfBands; i++ {
		hSbrData.SbrInvfMode[i] = hPrevData.SbrInvfMode[i]
	}

	// Generate concealing control data.
	hSbrData.FrameInfo.NEnvelopes = 1
	hSbrData.FrameInfo.Borders[0] = uint8(currentStartPos)
	hSbrData.FrameInfo.Borders[1] = uint8(currentStopPos)
	hSbrData.FrameInfo.FreqRes[0] = 1
	hSbrData.FrameInfo.TranEnv = -1 // no transient
	hSbrData.FrameInfo.NNoiseEnv = 1
	hSbrData.FrameInfo.BordersNoise[0] = uint8(currentStartPos)
	hSbrData.FrameInfo.BordersNoise[1] = uint8(currentStopPos)

	hSbrData.NScaleFactors = int(hHeaderData.FreqBandData.NSfb[1])

	// Generate fake envelope data.
	hSbrData.DomainVec[0] = 1

	var target, step int16
	if hSbrData.Coupling == couplingBal {
		target = int16(sbrEnergyPanOffset)
		step = int16(decayCoupling)
	} else {
		target = nativeaac.Fl2fxconstSGL(0.0)
		step = int16(decay)
	}
	if hHeaderData.BsInfo.AmpResolution == 0 {
		target <<= 1
		step <<= 1
	}

	for i := 0; i < hSbrData.NScaleFactors; i++ {
		if hPrevData.SfbNrgPrev[i] > target {
			hSbrData.IEnvelope[i] = -step
		} else {
			hSbrData.IEnvelope[i] = step
		}
	}

	// Noisefloor levels are always cleared ...
	hSbrData.DomainVecNoise[0] = 1
	for i := range hSbrData.SbrNoiseFloorLevel {
		hSbrData.SbrNoiseFloorLevel[i] = 0
	}

	// ... and so are the sines.
	for i := range hSbrData.AddHarmonics {
		hSbrData.AddHarmonics[i] = 0
	}
}

// decodeEnvelope builds reference energies and noise levels from the bitstream
// elements, applying delta-time/frequency decoding, error concealment and
// requantization.
//
// C counterpart: decodeEnvelope (env_dec.cpp:449).
func decodeEnvelope(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData,
	hPrevData *SbrPrevFrameData, otherChannel *SbrPrevFrameData) {
	fFrameError := hHeaderData.FrameError
	var tempSfbNrgPrev [maxFreqCoeffs]int16

	if fFrameError == 0 {
		// To avoid distortions after bad frames, set the error flag if delta coding
		// in time occurs.
		if hPrevData.FrameError != 0 {
			if hSbrData.DomainVec[0] != 0 {
				fFrameError = 1
			}
		} else {
			// Check that the previous stop position and the current start position
			// match.
			if int(hSbrData.FrameInfo.Borders[0]) !=
				int(hPrevData.StopPos)-int(hHeaderData.NumberTimeSlots) {
				// Both frames flagged ok but they do not match: prefer concealment
				// over delta-time coding (both branches set fFrameError=1).
				fFrameError = 1
			}
		}
	}

	if fFrameError != 0 { // Error is detected
		leanSbrConcealment(hHeaderData, hSbrData, hPrevData)

		// decode the envelope data to linear PCM
		deltaToLinearPcmEnvelopeDecoding(hHeaderData, hSbrData, hPrevData)
	} else { // dummy decoding + range check
		if hPrevData.FrameError != 0 {
			timeCompensateFirstEnvelope(hHeaderData, hSbrData, hPrevData)
			if hSbrData.Coupling != hPrevData.Coupling {
				// Coupling mode changed during concealment: convert stored energies.
				for i := 0; i < int(hHeaderData.FreqBandData.NSfb[1]); i++ {
					if hPrevData.Coupling == couplingBal {
						if otherChannel != nil {
							hPrevData.SfbNrgPrev[i] = otherChannel.SfbNrgPrev[i]
						} else {
							hPrevData.SfbNrgPrev[i] = int16(sbrEnergyPanOffset)
						}
					} else if hSbrData.Coupling == couplingLevel && otherChannel != nil {
						hPrevData.SfbNrgPrev[i] = (hPrevData.SfbNrgPrev[i] + otherChannel.SfbNrgPrev[i]) >> 1
					} else if hSbrData.Coupling == couplingBal {
						hPrevData.SfbNrgPrev[i] = int16(sbrEnergyPanOffset)
					}
				}
			}
		}
		copy(tempSfbNrgPrev[:], hPrevData.SfbNrgPrev[:])

		deltaToLinearPcmEnvelopeDecoding(hHeaderData, hSbrData, hPrevData)

		fFrameError = uint8(checkEnvelopeData(hHeaderData, hSbrData, hPrevData))

		if fFrameError != 0 {
			hHeaderData.FrameError = 1
			copy(hPrevData.SfbNrgPrev[:], tempSfbNrgPrev[:])
			decodeEnvelope(hHeaderData, hSbrData, hPrevData, otherChannel)
			return
		}
	}

	requantizeEnvelopeData(hSbrData, hSbrData.AmpResolutionCurrFrame)

	hHeaderData.FrameError = fFrameError
}

// checkEnvelopeData verifies that envelope energies are within the allowed
// range, returning 1 if any value was out of range and clamping previous
// energies.
//
// C counterpart: checkEnvelopeData (env_dec.cpp:551).
func checkEnvelopeData(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData, hPrevData *SbrPrevFrameData) int {
	iEnvelope := hSbrData.IEnvelope[:]
	sfbNrgPrev := hPrevData.SfbNrgPrev[:]
	errorFlag := 0
	var sbrMaxEnergyVal int16 = sbrMaxEnergy
	if hSbrData.AmpResolutionCurrFrame != 1 {
		sbrMaxEnergyVal = sbrMaxEnergy << 1
	}

	// Range check for current energies.
	for i := 0; i < hSbrData.NScaleFactors; i++ {
		if iEnvelope[i] > sbrMaxEnergyVal {
			errorFlag = 1
		}
		if iEnvelope[i] < 0 { // FL2FXCONST_SGL(0.0f)
			errorFlag = 1
		}
	}

	// Range check for previous energies.
	for i := 0; i < int(hHeaderData.FreqBandData.NSfb[1]); i++ {
		if sfbNrgPrev[i] < 0 {
			sfbNrgPrev[i] = 0
		}
		if sfbNrgPrev[i] > sbrMaxEnergyVal {
			sfbNrgPrev[i] = sbrMaxEnergyVal
		}
	}

	return errorFlag
}

// limitNoiseLevels limits the current noise levels to the allowed range.
//
// C counterpart: limitNoiseLevels (env_dec.cpp:594).
func limitNoiseLevels(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData) {
	nNfb := int(hHeaderData.FreqBandData.NNfb)

	// lowerLimit==0 (highest noise energy), upperLimit==35 (lowest noise energy).
	const lowerLimit = int16(0)
	const upperLimit = int16(35)

	for i := 0; i < int(hSbrData.FrameInfo.NNoiseEnv)*nNfb; i++ {
		if hSbrData.SbrNoiseFloorLevel[i] > upperLimit {
			hSbrData.SbrNoiseFloorLevel[i] = upperLimit
		}
		if hSbrData.SbrNoiseFloorLevel[i] < lowerLimit {
			hSbrData.SbrNoiseFloorLevel[i] = lowerLimit
		}
	}
}

// timeCompensateFirstEnvelope compensates for the wrong timing that might occur
// after a frame error by adjusting the first envelope's length/level.
//
// C counterpart: timeCompensateFirstEnvelope (env_dec.cpp:625).
func timeCompensateFirstEnvelope(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData, hPrevData *SbrPrevFrameData) {
	pFrameInfo := &hSbrData.FrameInfo
	nSfb := hHeaderData.FreqBandData.NSfb
	estimatedStartPos := nativeaac.FMaxI(0, int(hPrevData.StopPos)-int(hHeaderData.NumberTimeSlots))

	// Original length of first envelope according to bitstream.
	refLen := int(pFrameInfo.Borders[1]) - int(pFrameInfo.Borders[0])
	// Corrected length (concealing can make the first envelope longer).
	newLen := int(pFrameInfo.Borders[1]) - estimatedStartPos

	if newLen <= 0 {
		newLen = refLen
		estimatedStartPos = int(pFrameInfo.Borders[0])
	}

	deltaExp := getNumOctavesDiv8(newLen, refLen)

	// Shift by -3 to rescale ld-table, ampRes-1 to enable coarser steps.
	shift := uint(fractBits - 1 - envExpFract - 1 + hSbrData.AmpResolutionCurrFrame - 3)
	deltaExp = deltaExp >> shift
	pFrameInfo.Borders[0] = uint8(estimatedStartPos)
	pFrameInfo.BordersNoise[0] = uint8(estimatedStartPos)

	if hSbrData.Coupling != couplingBal {
		var nScalefactors int
		if pFrameInfo.FreqRes[0] != 0 {
			nScalefactors = int(nSfb[1])
		} else {
			nScalefactors = int(nSfb[0])
		}
		for i := 0; i < nScalefactors; i++ {
			hSbrData.IEnvelope[i] = hSbrData.IEnvelope[i] + deltaExp
		}
	}
}

// requantizeEnvelopeData converts each envelope value from the logarithmic
// (exponent) domain to the linear pseudo-float (mantissa+exponent) domain in
// place. ENV_EXP_FRACT is 0, so the fractional-mantissa branch is compiled out.
//
// C counterpart: requantizeEnvelopeData (env_dec.cpp:690).
func requantizeEnvelopeData(hSbrData *SbrFrameData, ampResolution int) {
	ampShift := 1 - ampResolution

	for i := 0; i < hSbrData.NScaleFactors; i++ {
		exponent := int(hSbrData.IEnvelope[i])

		// ENV_EXP_FRACT==0: high-amplitude resolution loses 1 bit of the exponent
		// by the shift; compensate with mantissa 0.5*sqrt(2) instead of 0.5 if set.
		var mantissa int16
		if exponent&ampShift != 0 {
			mantissa = nativeaac.Fl2fxconstSGL(0.707106781186548)
		} else {
			mantissa = nativeaac.Fl2fxconstSGL(0.5)
		}
		exponent = exponent >> uint(ampShift)

		// Mantissa is 0.5 (instead of 1.0) => +1 exponent. Multiply by L=64 => +6.
		exponent += 7 + nrgExpOffset

		hSbrData.IEnvelope[i] = packEnvVal(mantissa, exponent)
	}
}

// deltaToLinearPcmEnvelopeDecoding builds new reference energies from old ones
// and the delta-coded bitstream data (delta-time or delta-freq per envelope).
//
// C counterpart: deltaToLinearPcmEnvelopeDecoding (env_dec.cpp:767).
func deltaToLinearPcmEnvelopeDecoding(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData, hPrevData *SbrPrevFrameData) {
	sfbNrgPrev := hPrevData.SfbNrgPrev[:]
	ptr := 0 // index into hSbrData.IEnvelope (ptr_nrg)

	offset := 2*int(hHeaderData.FreqBandData.NSfb[0]) - int(hHeaderData.FreqBandData.NSfb[1])

	for i := 0; i < int(hSbrData.FrameInfo.NEnvelopes); i++ {
		domain := int(hSbrData.DomainVec[i])
		freqRes := int(hSbrData.FrameInfo.FreqRes[i])

		noOfBands := int(hHeaderData.FreqBandData.NSfb[freqRes])

		if domain == 0 {
			mapLowResEnergyVal(hSbrData.IEnvelope[ptr], sfbNrgPrev, offset, 0, freqRes)
			ptr++
			for band := 1; band < noOfBands; band++ {
				hSbrData.IEnvelope[ptr] = hSbrData.IEnvelope[ptr] + hSbrData.IEnvelope[ptr-1]
				mapLowResEnergyVal(hSbrData.IEnvelope[ptr], sfbNrgPrev, offset, band, freqRes)
				ptr++
			}
		} else {
			for band := 0; band < noOfBands; band++ {
				hSbrData.IEnvelope[ptr] = hSbrData.IEnvelope[ptr] +
					sfbNrgPrev[indexLow2High(offset, band, freqRes)]
				mapLowResEnergyVal(hSbrData.IEnvelope[ptr], sfbNrgPrev, offset, band, freqRes)
				ptr++
			}
		}
	}
}

// decodeNoiseFloorlevels builds new noise levels from old ones and delta-coded
// data, limits them, updates prevNoiseLevel and requantizes in COUPLING_OFF mode.
//
// C counterpart: decodeNoiseFloorlevels (env_dec.cpp:812).
func decodeNoiseFloorlevels(hHeaderData *SbrHeaderData, hSbrData *SbrFrameData, hPrevData *SbrPrevFrameData) {
	nNfb := int(hHeaderData.FreqBandData.NNfb)
	nNoiseFloorEnvelopes := int(hSbrData.FrameInfo.NNoiseEnv)

	// Decode first noise envelope.
	if hSbrData.DomainVecNoise[0] == 0 {
		noiseLevel := hSbrData.SbrNoiseFloorLevel[0]
		for i := 1; i < nNfb; i++ {
			noiseLevel += hSbrData.SbrNoiseFloorLevel[i]
			hSbrData.SbrNoiseFloorLevel[i] = noiseLevel
		}
	} else {
		for i := 0; i < nNfb; i++ {
			hSbrData.SbrNoiseFloorLevel[i] += hPrevData.PrevNoiseLevel[i]
		}
	}

	// If present, decode the second noise envelope (nNoiseFloorEnvelopes is 1 or 2).
	if nNoiseFloorEnvelopes > 1 {
		if hSbrData.DomainVecNoise[1] == 0 {
			noiseLevel := hSbrData.SbrNoiseFloorLevel[nNfb]
			for i := nNfb + 1; i < 2*nNfb; i++ {
				noiseLevel += hSbrData.SbrNoiseFloorLevel[i]
				hSbrData.SbrNoiseFloorLevel[i] = noiseLevel
			}
		} else {
			for i := 0; i < nNfb; i++ {
				hSbrData.SbrNoiseFloorLevel[i+nNfb] += hSbrData.SbrNoiseFloorLevel[i]
			}
		}
	}

	limitNoiseLevels(hHeaderData, hSbrData)

	// Update prevNoiseLevel with the last noise envelope.
	for i := 0; i < nNfb; i++ {
		hPrevData.PrevNoiseLevel[i] = hSbrData.SbrNoiseFloorLevel[i+nNfb*(nNoiseFloorEnvelopes-1)]
	}

	// Requantize the noise floor levels in COUPLING_OFF mode.
	if hSbrData.Coupling == couplingOff {
		for i := 0; i < nNoiseFloorEnvelopes*nNfb; i++ {
			nfE := 6 - int(hSbrData.SbrNoiseFloorLevel[i]) + 1 + noiseExpOff
			// +1 to compensate for a mantissa of 0.5 instead of 1.0

			hSbrData.SbrNoiseFloorLevel[i] = int16(int(nativeaac.Fl2fxconstSGL(0.5)) + // mantissa
				(nfE & maskE)) // exponent
		}
	}
}
