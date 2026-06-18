// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Inter-TES (temporal envelope shaping) processing, ported 1:1 from
// apply_inter_tes (libSBRdec/src/env_calc.cpp:509-775). Inter-TES is in HE-AAC v1
// scope; calculateSbrEnvelope calls it (when hFrameData->iTESactive) after the
// gain/noise/harmonic adjustment, reshaping the per-timeslot QMF energy of the
// high band using a gamma-indexed gain. It is pure fixed-point (the only
// "transcendental" is the table sqrtFixp_lookup), so bit-exact in any build.

// itesGammaSf maps gamma_idx -> gamma scale factor: gamma[gamma_idx] = {0,1,2,4}
// so the scale is gamma_idx - 1 (env_calc.cpp:524).

// applyInterTes is the 1:1 port of apply_inter_tes (env_calc.cpp:509-775).
// qmfReal/qmfImag are slices of per-timeslot QMF rows; exp is the pNrgs->exponent
// pair; rate == timeStep.
//
// C counterpart: apply_inter_tes (env_calc.cpp:509).
func applyInterTes(qmfReal, qmfImag [][]int32, sbrScaleFactor *ScaleFactor, exp [2]int8, rate, startPos, stopPos, lowSubband, nbSubband int, gammaIdx uint8) {
	highSubband := lowSubband + nbSubband
	var totalPowerHigh, totalPowerLow int32

	var gainSf [envBufSize]int
	gammaSf := int(gammaIdx) - 1

	nbSubsample := stopPos - startPos

	var subsamplePowerHigh, subsamplePowerLow, gain [envBufSize]int32
	var subsamplePowerHighSf, subsamplePowerLowSf [envBufSize]int8

	if gammaIdx > 0 {
		preShift2 := 32 - nativeaac.CntLeadingZeros(int32(nbSubsample))
		totalPowerLowSf := 1 - dfractBits
		totalPowerHighSf := 1 - dfractBits

		for i := 0; i < nbSubsample; i++ {
			var bufferReal, bufferImag [envBufSize]int32
			var maxVal int32

			ts := startPos + i

			lowSf := sbrScaleFactor.LbScale
			if ts < 3*rate {
				lowSf = sbrScaleFactor.OvLbScale
			}
			lowSf = 15 - lowSf

			for j := 0; j < lowSubband; j++ {
				bufferImag[j] = qmfImag[startPos+i][j]
				maxVal |= bufferImag[j] ^ (bufferImag[j] >> (dfractBits - 1))
				bufferReal[j] = qmfReal[startPos+i][j]
				maxVal |= bufferReal[j] ^ (bufferReal[j] >> (dfractBits - 1))
			}

			subsamplePowerLow[i] = 0
			subsamplePowerLowSf[i] = 0

			if maxVal != 0 {
				preShift := 1 - nativeaac.CntLeadingZeros(maxVal)
				postShift := 32 - nativeaac.CntLeadingZeros(int32(lowSubband))

				if preShift != 0 {
					preShift++
				}

				subsamplePowerLowSf[i] += int8((lowSf+preShift)*2 + postShift + 1)

				nativeaac.ScaleValues(bufferReal[:lowSubband], lowSubband, int32(-preShift))
				nativeaac.ScaleValues(bufferImag[:lowSubband], lowSubband, int32(-preShift))
				for j := 0; j < lowSubband; j++ {
					addme := nativeaac.FPow2Div2(bufferReal[j])
					subsamplePowerLow[i] += addme >> uint(postShift)
					addme = nativeaac.FPow2Div2(bufferImag[j])
					subsamplePowerLow[i] += addme >> uint(postShift)
				}
			}

			// now get high
			maxVal = 0

			highSf := int(exp[0])
			if ts >= 16*rate {
				highSf = int(exp[1])
			}

			for j := lowSubband; j < highSubband; j++ {
				bufferImag[j] = qmfImag[startPos+i][j]
				maxVal |= bufferImag[j] ^ (bufferImag[j] >> (dfractBits - 1))
				bufferReal[j] = qmfReal[startPos+i][j]
				maxVal |= bufferReal[j] ^ (bufferReal[j] >> (dfractBits - 1))
			}

			subsamplePowerHigh[i] = 0
			subsamplePowerHighSf[i] = 0

			if maxVal != 0 {
				preShift := 1 - nativeaac.CntLeadingZeros(maxVal)
				if preShift != 0 {
					preShift++
				}

				postShift := 32 - nativeaac.CntLeadingZeros(int32(highSubband-lowSubband))
				subsamplePowerHighSf[i] += int8((highSf+preShift)*2 + postShift + 1)

				nativeaac.ScaleValues(bufferReal[lowSubband:highSubband], highSubband-lowSubband, int32(-preShift))
				nativeaac.ScaleValues(bufferImag[lowSubband:highSubband], highSubband-lowSubband, int32(-preShift))
				for j := lowSubband; j < highSubband; j++ {
					subsamplePowerHigh[i] += nativeaac.FPow2Div2(bufferReal[j]) >> uint(postShift)
					subsamplePowerHigh[i] += nativeaac.FPow2Div2(bufferImag[j]) >> uint(postShift)
				}
			}

			// sum all together
			newSummand := subsamplePowerLow[i]
			newSummandSf := int(subsamplePowerLowSf[i])

			if newSummandSf > totalPowerLowSf {
				diff := nativeaac.FMinI(dfractBits-1, newSummandSf-totalPowerLowSf)
				totalPowerLow >>= uint(diff)
				totalPowerLowSf = newSummandSf
			} else if newSummandSf < totalPowerLowSf {
				newSummand >>= uint(nativeaac.FMinI(dfractBits-1, totalPowerLowSf-newSummandSf))
			}

			totalPowerLow += newSummand >> uint(preShift2)

			newSummand = subsamplePowerHigh[i]
			newSummandSf = int(subsamplePowerHighSf[i])
			if newSummandSf > totalPowerHighSf {
				totalPowerHigh >>= uint(nativeaac.FMinI(dfractBits-1, newSummandSf-totalPowerHighSf))
				totalPowerHighSf = newSummandSf
			} else if newSummandSf < totalPowerHighSf {
				newSummand >>= uint(nativeaac.FMinI(dfractBits-1, totalPowerHighSf-newSummandSf))
			}

			totalPowerHigh += newSummand >> uint(preShift2)
		}

		totalPowerLowSf += preShift2
		totalPowerHighSf += preShift2

		// gain[i] = e_LOW[i]
		for i := 0; i < nbSubsample; i++ {
			mult, sf2 := nativeaac.FMultNorm(subsamplePowerLow[i], int32(nbSubsample))
			multSf := int(subsamplePowerLowSf[i]) + dfractBits - 1 + int(sf2)

			if totalPowerLow != 0 {
				var divE int32
				gain[i], divE = nativeaac.FDivNorm(mult, totalPowerLow)
				gainSf[i] = multSf - totalPowerLowSf + int(divE)
				gain[i], gainSf[i] = sqrtFixpLookupE(gain[i], gainSf[i])
				if gainSf[i] < 0 {
					gain[i] >>= uint(nativeaac.FMinI(dfractBits-1, -gainSf[i]))
					gainSf[i] = 0
				}
			} else {
				if mult == 0 {
					gain[i] = 0
					gainSf[i] = 0
				} else {
					gain[i] = 0x7FFFFFFF // MAXVAL_DBL
					gainSf[i] = 0
				}
			}
		}

		var totalPowerHighAfter int32
		totalPowerHighAfterSf := 1 - dfractBits

		// gain[i] = g_inter[i]
		for i := 0; i < nbSubsample; i++ {
			// gain[i] = 1.0f + gamma * (gain[i] - 1.0f);
			one := int32(0x7FFFFFFF) >> uint(gainSf[i]) // (FIXP_DBL)MAXVAL_DBL >> gain_sf[i]

			mult := (gain[i] - one) >> 1
			multSf := gainSf[i] + gammaSf

			one = int32(0x40000000) >> uint(multSf) // FL2FXCONST_DBL(0.5f) >> mult_sf
			gain[i] = one + mult
			gainSf[i] += gammaSf + 1

			var gainPow2 int32
			var gainPow2Sf int

			if nativeaac.FIsLessThan(gain[i], int32(gainSf[i]), nativeaac.Fl2fxconstDBL(0.2), 0) {
				gain[i] = nativeaac.Fl2fxconstDBL(0.8)
				gainSf[i] = -2
				gainPow2 = nativeaac.Fl2fxconstDBL(0.64)
				gainPow2Sf = -4
			} else {
				r := nativeaac.CountLeadingBits(gain[i])
				gain[i] <<= uint(r)
				gainSf[i] -= r

				gainPow2 = nativeaac.FPow2(gain[i])
				gainPow2Sf = gainSf[i] << 1
			}

			var room int32
			subsamplePowerHigh[i], room = nativeaac.FMultNorm(subsamplePowerHigh[i], gainPow2)
			subsamplePowerHighSf[i] = int8(int(subsamplePowerHighSf[i]) + gainPow2Sf + int(room))

			newSummandSf := int(subsamplePowerHighSf[i])
			if newSummandSf > totalPowerHighAfterSf {
				totalPowerHighAfter >>= uint(nativeaac.FMinI(dfractBits-1, newSummandSf-totalPowerHighAfterSf))
				totalPowerHighAfterSf = newSummandSf
			} else if newSummandSf < totalPowerHighAfterSf {
				subsamplePowerHigh[i] >>= uint(nativeaac.FMinI(dfractBits-1, totalPowerHighAfterSf-newSummandSf))
			}
			totalPowerHighAfter += subsamplePowerHigh[i] >> uint(preShift2)
		}

		totalPowerHighAfterSf += preShift2

		gainAdj2 := int32(0x40000000) // FL2FX_DBL(0.5f)
		gainAdj2Sf := 1

		if totalPowerHigh != 0 && totalPowerHighAfter != 0 {
			var sf2 int32
			gainAdj2, sf2 = nativeaac.FDivNorm(totalPowerHigh, totalPowerHighAfter)
			gainAdj2Sf = totalPowerHighSf - totalPowerHighAfterSf + int(sf2)
		}

		gainAdj, gainAdjSf := sqrtFixpLookupE(gainAdj2, gainAdj2Sf)

		for i := 0; i < nbSubsample; i++ {
			gainE := nativeaac.FMaxI(nativeaac.FMinI(gainSf[i]+gainAdjSf-interTesSfChange, dfractBits-1), -(dfractBits - 1))
			gainFinal := nativeaac.FMultDD(gain[i], gainAdj)
			gainFinal = nativeaac.ScaleValueSaturate(gainFinal, int32(gainE))

			for j := lowSubband; j < highSubband; j++ {
				qmfReal[startPos+i][j] = nativeaac.FMultDD(qmfReal[startPos+i][j], gainFinal)
				qmfImag[startPos+i][j] = nativeaac.FMultDD(qmfImag[startPos+i][j], gainFinal)
			}
		}
	} else { // gamma_idx == 0
		for i := 0; i < nbSubsample; i++ {
			for j := lowSubband; j < highSubband; j++ {
				qmfReal[startPos+i][j] >>= interTesSfChange
				qmfImag[startPos+i][j] >>= interTesSfChange
			}
		}
	}
}
