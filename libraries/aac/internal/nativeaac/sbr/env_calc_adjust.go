// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// The per-subband / per-timeslot helpers calculateSbrEnvelope (env_calc.go) drives:
// energy estimation (calcNrgPerSubband/Sfb), gain/noise/sine derivation
// (calcSubbandGain), limiter average gain (calcAvgGain), the smoothing-buffer
// exponent equalizer (equalizeFiltBufferExp), the real-LP and complex-HQ
// timeslot adjustors (adjustTimeSlotLC / adjustTimeSlotHQ + the iTES split
// adjustTimeSlotHQ_GainAndNoise / adjustTimeSlotHQ_AddHarmonics), and inter-TES
// (apply_inter_tes). All ported 1:1 from libSBRdec/src/env_calc.cpp.
//
// The adjustTimeSlot* functions take the full QMF row slice + a base offset
// (== lowSubband) instead of a C `FIXP_DBL *ptrReal` pointing into the row at
// lowSubband, so the C `*(ptrReal-1)` / `*(ptrReal+1)` neighbour writes map to
// row[base-1] / row[base+...] exactly.

// shiftBeforeSquare is SHIFT_BEFORE_SQUARE (env_calc.cpp:1959).
const shiftBeforeSquare = 4

// envBufSize is the analysis-buffer scratch length ((1024/32*4/2)+(3*4))
// (env_calc.cpp:502 etc) used to size the temporary line buffers.
const envBufSize = (1024/32*4)/2 + (3 * 4)

// equalizeFiltBufferExp is the 1:1 port of equalizeFiltBufferExp
// (env_calc.cpp:1814-1855): equalize the exponents of the buffered gains and the
// new gains so the FIR smoothing addition can run.
//
// C counterpart: equalizeFiltBufferExp (env_calc.cpp:1814).
func equalizeFiltBufferExp(filtBuffer []int32, filtBufferE []int8, nrgGain []int32, nrgGainE []int8, subbands int) {
	for band := 0; band < subbands; band++ {
		diff := int(nrgGainE[band]) - int(filtBufferE[band])
		if diff > 0 {
			filtBuffer[band] >>= uint(nativeaac.FMinI(diff, dfractBits-1))
			filtBufferE[band] += int8(diff)
		} else if diff < 0 {
			reserve := nativeaac.CntLeadingZeros(nativeaac.FixpAbs(filtBuffer[band])) - 1

			if (-diff) <= reserve {
				filtBuffer[band] <<= uint(-diff)
				filtBufferE[band] += int8(diff)
			} else {
				filtBuffer[band] <<= uint(reserve)
				filtBufferE[band] -= int8(reserve)

				diff = -(reserve + diff)
				nrgGain[band] >>= uint(nativeaac.FMinI(diff, dfractBits-1))
				nrgGainE[band] += int8(diff)
			}
		}
	}
}

// calcNrgPerSubband is the 1:1 port of calcNrgPerSubband (env_calc.cpp:1984-2104):
// estimate the mean energy of each filter-bank channel over the envelope when
// interpolFreq is true. analysBufferImag may be nil (real-only LP).
//
// C counterpart: calcNrgPerSubband (env_calc.cpp:1984).
func calcNrgPerSubband(analysBufferReal, analysBufferImag [][]int32, lowSubband, highSubband, startPos, nextPos int, frameExp int8, nrgEst []int32, nrgEstE []int8) {
	invWidth := nativeaac.FxDbl2FxSgl(nativeaac.GetInvInt(nextPos - startPos))
	frameExp = frameExp << 1

	nrgEstIdx := 0
	for k := lowSubband; k < highSubband; k++ {
		var bufferReal [envBufSize]int32
		var bufferImag [envBufSize]int32
		var maxVal int32

		if analysBufferImag != nil {
			for l := startPos; l < nextPos; l++ {
				bufferImag[l] = analysBufferImag[l][k]
				maxVal |= bufferImag[l] ^ (bufferImag[l] >> (dfractBits - 1))
				bufferReal[l] = analysBufferReal[l][k]
				maxVal |= bufferReal[l] ^ (bufferReal[l] >> (dfractBits - 1))
			}
		} else {
			for l := startPos; l < nextPos; l++ {
				bufferReal[l] = analysBufferReal[l][k]
				maxVal |= bufferReal[l] ^ (bufferReal[l] >> (dfractBits - 1))
			}
		}

		if maxVal != 0 {
			preShift := int8(nativeaac.CntLeadingZeros(maxVal) - 1)
			preShift -= shiftBeforeSquare

			if preShift > 25 {
				preShift = 25
			}

			var accu int32
			if preShift >= 0 {
				if analysBufferImag != nil {
					for l := startPos; l < nextPos; l++ {
						temp1 := bufferReal[l] << uint(preShift)
						temp2 := bufferImag[l] << uint(preShift)
						accu = nativeaac.FPow2AddDiv2(accu, temp1)
						accu = nativeaac.FPow2AddDiv2(accu, temp2)
					}
				} else {
					for l := startPos; l < nextPos; l++ {
						temp := bufferReal[l] << uint(preShift)
						accu = nativeaac.FPow2AddDiv2(accu, temp)
					}
				}
			} else {
				negpreShift := -preShift
				if analysBufferImag != nil {
					for l := startPos; l < nextPos; l++ {
						temp1 := bufferReal[l] >> uint(negpreShift)
						temp2 := bufferImag[l] >> uint(negpreShift)
						accu = nativeaac.FPow2AddDiv2(accu, temp1)
						accu = nativeaac.FPow2AddDiv2(accu, temp2)
					}
				} else {
					for l := startPos; l < nextPos; l++ {
						temp := bufferReal[l] >> uint(negpreShift)
						accu = nativeaac.FPow2AddDiv2(accu, temp)
					}
				}
			}
			accu <<= 1

			shift := int8(nativeaac.CountLeadingBits(accu))
			sum := accu << uint(shift)

			nrgEst[nrgEstIdx] = fMultSD(invWidth, sum)
			shift += 2 * preShift
			if analysBufferImag != nil {
				nrgEstE[nrgEstIdx] = frameExp - shift
			} else {
				nrgEstE[nrgEstIdx] = frameExp - shift + 1
			}
		} else {
			nrgEst[nrgEstIdx] = 0
			nrgEstE[nrgEstIdx] = 0
		}
		nrgEstIdx++
	}
}

// calcNrgPerSfb is the 1:1 port of calcNrgPerSfb (env_calc.cpp:2112-2230):
// estimate the mean energy of each scale-factor band over the envelope when
// interpolFreq is false. analysBufferImag may be nil (real-only LP).
//
// C counterpart: calcNrgPerSfb (env_calc.cpp:2112).
func calcNrgPerSfb(analysBufferReal, analysBufferImag [][]int32, nSfb int, freqBandTable []uint8, startPos, nextPos int, inputE int8, nrgEst []int32, nrgEstE []int8) {
	invWidth := nativeaac.FxDbl2FxSgl(nativeaac.GetInvInt(nextPos - startPos))
	inputE = inputE << 1

	nrgEstIdx := 0
	for j := 0; j < nSfb; j++ {
		li := int(freqBandTable[j])
		ui := int(freqBandTable[j+1])

		maxVal := maxSubbandSample(analysBufferReal, analysBufferImag, li, ui, startPos, nextPos)

		var sum int32
		var sumE int8
		if maxVal != 0 {
			preShift := int8(nativeaac.CntLeadingZeros(maxVal) - 1)
			preShift -= shiftBeforeSquare

			var sumAll int32

			for k := li; k < ui; k++ {
				var sumLine int32

				if analysBufferImag != nil {
					if preShift >= 0 {
						for l := startPos; l < nextPos; l++ {
							temp := analysBufferReal[l][k] << uint(preShift)
							sumLine += nativeaac.FPow2Div2(temp)
							temp = analysBufferImag[l][k] << uint(preShift)
							sumLine += nativeaac.FPow2Div2(temp)
						}
					} else {
						for l := startPos; l < nextPos; l++ {
							temp := analysBufferReal[l][k] >> uint(-preShift)
							sumLine += nativeaac.FPow2Div2(temp)
							temp = analysBufferImag[l][k] >> uint(-preShift)
							sumLine += nativeaac.FPow2Div2(temp)
						}
					}
				} else {
					if preShift >= 0 {
						for l := startPos; l < nextPos; l++ {
							temp := analysBufferReal[l][k] << uint(preShift)
							sumLine += nativeaac.FPow2Div2(temp)
						}
					} else {
						for l := startPos; l < nextPos; l++ {
							temp := analysBufferReal[l][k] >> uint(-preShift)
							sumLine += nativeaac.FPow2Div2(temp)
						}
					}
				}

				sumLine = sumLine >> (4 - 1)
				sumAll += sumLine
			}

			shift := int8(nativeaac.CountLeadingBits(sumAll))
			sum = sumAll << uint(shift)

			sum = fMultSD(invWidth, sum)
			sum = fMultSD(nativeaac.FxDbl2FxSgl(nativeaac.GetInvInt(ui-li)), sum)

			if analysBufferImag != nil {
				sumE = inputE + 4 - shift
			} else {
				sumE = inputE + 4 + 1 - shift
			}

			sumE -= 2 * preShift
		} else {
			sum = 0
			sumE = 0
		}

		for k := li; k < ui; k++ {
			nrgEst[nrgEstIdx] = sum
			nrgEstE[nrgEstIdx] = sumE
			nrgEstIdx++
		}
	}
}

// calcSubbandGain is the 1:1 port of calcSubbandGain (env_calc.cpp:2237-2334):
// derive the gain, noise level, and sine level for one subband from the
// reference energy, estimated energy, and relative noise level.
//
// C counterpart: calcSubbandGain (env_calc.cpp:2237).
func calcSubbandGain(nrgRef int32, nrgRefE int8, nrgs *EnvCalcNrgs, i int, tmpNoise int32, tmpNoiseE int8, sinePresentFlag, sineMapped uint8, noNoiseFlag int) {
	nrgEst := nrgs.NrgEst[i]
	nrgEstE := nrgs.NrgEstE[i]

	const half = int32(0x40000000) // FL2FXCONST_DBL(0.5f)

	// Add 1 to prevent divisions by zero / overly high gains.
	bE := int(nrgEstE) - 1
	if bE >= 0 {
		nrgEst = (half >> uint(nativeaac.FMinI(bE+1, dfractBits-1))) + (nrgEst >> 1)
		nrgEstE += 1
	} else {
		nrgEst = (nrgEst >> uint(nativeaac.FMinI(-bE+1, dfractBits-1))) + (half >> 1)
		nrgEstE = 2
	}

	// A = NrgRef * TmpNoise
	a := nativeaac.FMultDD(nrgRef, tmpNoise)
	aE := nrgRefE + tmpNoiseE

	// B = 1 + TmpNoise
	var b int32
	var bEexp int8
	bE = int(tmpNoiseE) - 1
	if bE >= 0 {
		b = (half >> uint(nativeaac.FMinI(bE+1, dfractBits-1))) + (tmpNoise >> 1)
		bEexp = tmpNoiseE + 1
	} else {
		b = (tmpNoise >> uint(nativeaac.FMinI(-bE+1, dfractBits-1))) + (half >> 1)
		bEexp = 2
	}

	// noiseLevel = A / B
	nrgs.NoiseLevel[i], nrgs.NoiseLevelE[i] = fdkDivideMantExp(a, aE, b, bEexp)

	if sinePresentFlag != 0 {
		// C = (1 + TmpNoise) * NrgEst
		c := nativeaac.FMultDD(b, nrgEst)
		cE := bEexp + nrgEstE

		nrgs.NrgGain[i], nrgs.NrgGainE[i] = fdkDivideMantExp(a, aE, c, cE)

		if sineMapped != 0 {
			nrgs.NrgSine[i], nrgs.NrgSineE[i] = fdkDivideMantExp(nrgRef, nrgRefE, b, bEexp)
		}
	} else {
		if noNoiseFlag != 0 {
			b = nrgEst
			bEexp = nrgEstE
		} else {
			b = nativeaac.FMultDD(b, nrgEst)
			bEexp = bEexp + nrgEstE
		}

		gain, resultExp := nativeaac.FDivNorm(nrgRef, b)
		nrgs.NrgGain[i] = gain
		nrgs.NrgGainE[i] = int8(resultExp) + (nrgRefE - bEexp)

		headroom := nativeaac.CountLeadingBits(nrgs.NrgGain[i])
		nrgs.NrgGain[i] <<= uint(headroom)
		nrgs.NrgGainE[i] -= int8(headroom)
	}
}

// calcAvgGain is the 1:1 port of calcAvgGain (env_calc.cpp:2344-2381): the gain
// of the average magnitude over a limiter band, plus the summed reference energy.
//
// C counterpart: calcAvgGain (env_calc.cpp:2344).
func calcAvgGain(nrgs *EnvCalcNrgs, lowSubband, highSubband int) (sumRef int32, sumRefE int8, avgGain int32, avgGainE int8) {
	nrgRef := nrgs.NrgRef[:]
	nrgRefE := nrgs.NrgRefE[:]
	nrgEst := nrgs.NrgEst[:]
	nrgEstE := nrgs.NrgEstE[:]

	sumRef = 1
	sumEst := int32(1)
	sumRefE = -fractBits
	sumEstE := int8(-fractBits)

	for k := lowSubband; k < highSubband; k++ {
		sumRef, sumRefE = fdkAddMantExp(sumRef, sumRefE, nrgRef[k], nrgRefE[k])
		sumEst, sumEstE = fdkAddMantExp(sumEst, sumEstE, nrgEst[k], nrgEstE[k])
	}

	avgGain, avgGainE = fdkDivideMantExp(sumRef, sumRefE, sumEst, sumEstE)
	return sumRef, sumRefE, avgGain, avgGainE
}

// --- adjustTimeSlot constants (env_calc.cpp:2516-2517, 2407-2417) ------------

// c1Sgl is C1 == FL2FXCONST_SGL(2.f * 0.00815f) (env_calc.cpp:2516).
var c1Sgl int16

// harmonicPhase / harmonicPhaseX (env_calc.cpp:2407-2417).
var harmonicPhase = [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}
var harmonicPhaseX [4][2]int32
var harmonicPhaseXFloat = [4][2]float64{
	{2.0 * 1.245183154539139e-001, 2.0 * 1.245183154539139e-001},
	{2.0 * -1.123767859325028e-001, 2.0 * 1.123767859325028e-001},
	{2.0 * -1.245183154539139e-001, 2.0 * -1.245183154539139e-001},
	{2.0 * 1.123767859325028e-001, 2.0 * -1.123767859325028e-001},
}

func init() {
	c1Sgl = nativeaac.Fl2fxconstSGL(2.0 * 0.00815)
	for i := range harmonicPhaseXFloat {
		harmonicPhaseX[i][0] = nativeaac.Fl2fxconstDBL(harmonicPhaseXFloat[i][0])
		harmonicPhaseX[i][1] = nativeaac.Fl2fxconstDBL(harmonicPhaseXFloat[i][1])
	}
}

// adjustTimeSlotLC is the 1:1 port of adjustTimeSlotLC (env_calc.cpp:2492-2672):
// amplify one timeslot of the real-valued (LP) signal with the calculated gains
// and add the noisefloor. row is the full QMF row; base == lowSubband offset.
//
// C counterpart: adjustTimeSlotLC (env_calc.cpp:2492).
func adjustTimeSlotLC(row []int32, base int, nrgs *EnvCalcNrgs, ptrHarmIndex *uint8, lowSubband, noSubbands, scaleChange, noNoiseFlag int, ptrPhaseIndex *int) {
	pGain := 0       // index into nrgs.NrgGain
	pNoiseLevel := 0 // index into nrgs.NoiseLevel
	pSineLevel := 0  // index into nrgs.NrgSine

	rp := base // "ptrReal" position in row
	index := *ptrPhaseIndex
	harmIndex := *ptrHarmIndex
	freqInvFlag := lowSubband & 1
	var signalReal, sineLevel, sineLevelNext, sineLevelPrev int32
	toneCount := 0
	sineSign := 1
	maxVal := maxValNrgHeadroom >> uint(scaleChange)
	minVal := -maxVal

	// First pass for k=0 pulled out of the loop.
	index = (index + 1) & (sbrNFNoRandomVal - 1)

	signalReal = nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(row[rp], nrgs.NrgGain[pGain]), maxVal), minVal) << uint(scaleChange)
	pGain++
	sineLevel = nrgs.NrgSine[pSineLevel]
	pSineLevel++
	if noSubbands > 1 {
		sineLevelNext = nrgs.NrgSine[pSineLevel]
	} else {
		sineLevelNext = 0
	}

	if sineLevel != 0 {
		toneCount++
	} else if noNoiseFlag == 0 {
		signalReal += fMultSD(sbrRandomPhase[index][0], nrgs.NoiseLevel[pNoiseLevel])
	}

	{
		if harmIndex&0x1 == 0 {
			if harmIndex&0x2 != 0 {
				signalReal += -sineLevel
			} else {
				signalReal += sineLevel
			}
			row[rp] = signalReal
			rp++
		} else {
			shift := scaleChange + 1
			if shift >= 0 {
				shift = nativeaac.FMinI(dfractBits-1, shift)
			} else {
				shift = nativeaac.FMaxI(-(dfractBits - 1), shift)
			}

			var tmp1 int32
			if shift >= 0 {
				tmp1 = fMultDiv2SD(c1Sgl, sineLevel) >> uint(shift)
			} else {
				tmp1 = fMultDiv2SD(c1Sgl, sineLevel) << uint(-shift)
			}
			tmp2 := fMultDiv2SD(c1Sgl, sineLevelNext)

			if (int(harmIndex>>1)&0x1)^freqInvFlag != 0 {
				row[rp-1] = nativeaac.FAddSaturate(row[rp-1], tmp1)
				signalReal -= tmp2
			} else {
				row[rp-1] = nativeaac.FAddSaturate(row[rp-1], -tmp1)
				signalReal += tmp2
			}
			row[rp] = signalReal
			rp++
			freqInvFlag = boolToInt(freqInvFlag == 0)
		}
	}

	pNoiseLevel++

	if noSubbands > 2 {
		if harmIndex&0x1 == 0 {
			if harmIndex == 0 {
				sineSign = 0
			}

			for k := noSubbands - 2; k != 0; k-- {
				sinelevel := nrgs.NrgSine[pSineLevel]
				pSineLevel++
				index++
				if sineSign != 0 {
					signalReal = -sinelevel
				} else {
					signalReal = sinelevel
				}
				if signalReal == 0 && noNoiseFlag == 0 {
					index &= (sbrNFNoRandomVal - 1)
					signalReal += fMultSD(sbrRandomPhase[index][0], nrgs.NoiseLevel[pNoiseLevel])
				}

				signalReal += nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(row[rp], nrgs.NrgGain[pGain]), maxVal), minVal) << uint(scaleChange)
				pGain++

				pNoiseLevel++
				row[rp] = signalReal
				rp++
			}
		} else {
			if harmIndex == 1 {
				freqInvFlag = boolToInt(freqInvFlag == 0)
			}

			for k := noSubbands - 2; k != 0; k-- {
				index++
				signalReal = nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(row[rp], nrgs.NrgGain[pGain]), maxVal), minVal) << uint(scaleChange)
				pGain++

				if nrgs.NrgSine[pSineLevel] != 0 {
					toneCount++
				} else if noNoiseFlag == 0 {
					index &= (sbrNFNoRandomVal - 1)
					signalReal += fMultSD(sbrRandomPhase[index][0], nrgs.NoiseLevel[pNoiseLevel])
				}
				pSineLevel++

				pNoiseLevel++

				if toneCount <= 16 {
					addSine := fMultDiv2SD(c1Sgl, nrgs.NrgSine[pSineLevel-2]-nrgs.NrgSine[pSineLevel])
					if freqInvFlag != 0 {
						signalReal += -addSine
					} else {
						signalReal += addSine
					}
				}

				row[rp] = signalReal
				rp++
				freqInvFlag = boolToInt(freqInvFlag == 0)
			}
		}
	}

	if noSubbands > -1 {
		index++
		signalReal = nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(row[rp], nrgs.NrgGain[pGain]), maxVal), minVal) << uint(scaleChange)
		sineLevelPrev = fMultDiv2SD(nativeaac.Fl2fxconstSGL(0.0163), nrgs.NrgSine[pSineLevel-1])
		sineLevel = nrgs.NrgSine[pSineLevel]

		if nrgs.NrgSine[pSineLevel] != 0 {
			toneCount++
		} else if noNoiseFlag == 0 {
			index &= (sbrNFNoRandomVal - 1)
			signalReal = signalReal + fMultSD(sbrRandomPhase[index][0], nrgs.NoiseLevel[pNoiseLevel])
		}

		if harmIndex&0x1 == 0 {
			if sineSign != 0 {
				row[rp] = signalReal - sineLevel
			} else {
				row[rp] = signalReal + sineLevel
			}
		} else {
			if toneCount <= 16 {
				if freqInvFlag != 0 {
					row[rp] = signalReal - sineLevelPrev
					rp++
					if noSubbands+lowSubband < 63 {
						row[rp] = row[rp] + fMultDiv2SD(c1Sgl, sineLevel)
					}
				} else {
					row[rp] = signalReal + sineLevelPrev
					rp++
					if noSubbands+lowSubband < 63 {
						row[rp] = row[rp] - fMultDiv2SD(c1Sgl, sineLevel)
					}
				}
			} else {
				row[rp] = signalReal
			}
		}
	}
	*ptrHarmIndex = (harmIndex + 1) & 3
	*ptrPhaseIndex = index & (sbrNFNoRandomVal - 1)
}

// adjustTimeSlotHQGainAndNoise is the 1:1 port of adjustTimeSlotHQ_GainAndNoise
// (env_calc.cpp:2674-2802): the inter-TES variant that applies gain + noise but
// no additional harmonics. rowRe/rowIm are full QMF rows; base == lowSubband.
//
// C counterpart: adjustTimeSlotHQ_GainAndNoise (env_calc.cpp:2674).
func adjustTimeSlotHQGainAndNoise(rowRe, rowIm []int32, base int, hSbrCalEnv *SbrCalculateEnvelope, nrgs *EnvCalcNrgs, lowSubband, noSubbands, scaleChange int, smoothRatio int16, noNoiseFlag, filtBufferNoiseShift int) {
	gain := nrgs.NrgGain[:]
	noiseLevel := nrgs.NoiseLevel[:]
	pSineLevel := nrgs.NrgSine[:]

	filtBuffer := hSbrCalEnv.FiltBuffer[:]
	filtBufferNoise := hSbrCalEnv.FiltBufferNoise[:]
	ptrPhaseIndex := &hSbrCalEnv.PhaseIndex

	rp := base
	ip := base
	directRatio := int16(0x7FFF) - smoothRatio // MAXVAL_SGL - smooth_ratio
	index := *ptrPhaseIndex
	var shift int
	var maxValNoise, minValNoise int32
	maxVal := maxValNrgHeadroom >> uint(scaleChange)
	minVal := -maxVal

	*ptrPhaseIndex = (index + noSubbands) & (sbrNFNoRandomVal - 1)

	filtBufferNoiseShift++
	if filtBufferNoiseShift < 0 {
		shift = nativeaac.FMinI(dfractBits-1, -filtBufferNoiseShift)
	} else {
		shift = nativeaac.FMinI(dfractBits-1, filtBufferNoiseShift)
		maxValNoise = maxValNrgHeadroom >> uint(shift)
		minValNoise = -maxValNoise
	}

	if smoothRatio > 0 {
		for k := 0; k < noSubbands; k++ {
			smoothedGain := fMultSD(smoothRatio, filtBuffer[k]) + fMultSD(directRatio, gain[k])

			var smoothedNoise int32
			if filtBufferNoiseShift < 0 {
				smoothedNoise = (fMultDiv2SD(smoothRatio, filtBufferNoise[k]) >> uint(shift)) + fMultSD(directRatio, noiseLevel[k])
			} else {
				smoothedNoise = fMultDiv2SD(smoothRatio, filtBufferNoise[k])
				smoothedNoise = (nativeaac.FMaxDBL(nativeaac.FMinDBL(smoothedNoise, maxValNoise), minValNoise) << uint(shift)) + fMultSD(directRatio, noiseLevel[k])
			}

			smoothedNoise = nativeaac.FMaxDBL(nativeaac.FMinDBL(smoothedNoise, int32(0x7FFFFFFF/2)), int32(-0x80000000/2))

			signalReal := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowRe[rp], smoothedGain), maxVal), minVal) << uint(scaleChange)
			signalImag := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowIm[ip], smoothedGain), maxVal), minVal) << uint(scaleChange)

			index++

			if pSineLevel[k] != 0 || noNoiseFlag != 0 {
				rowRe[rp] = signalReal
				rp++
				rowIm[ip] = signalImag
				ip++
			} else {
				index &= (sbrNFNoRandomVal - 1)
				noiseReal := fMultSD(sbrRandomPhase[index][0], smoothedNoise)
				noiseImag := fMultSD(sbrRandomPhase[index][1], smoothedNoise)
				rowRe[rp] = signalReal + noiseReal
				rp++
				rowIm[ip] = signalImag + noiseImag
				ip++
			}
		}
	} else {
		for k := 0; k < noSubbands; k++ {
			smoothedGain := gain[k]
			signalReal := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowRe[rp], smoothedGain), maxVal), minVal) << uint(scaleChange)
			signalImag := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowIm[ip], smoothedGain), maxVal), minVal) << uint(scaleChange)

			index++

			if pSineLevel[k] == 0 && noNoiseFlag == 0 {
				smoothedNoise := noiseLevel[k]
				index &= (sbrNFNoRandomVal - 1)
				noiseReal := fMultSD(sbrRandomPhase[index][0], smoothedNoise)
				noiseImag := fMultSD(sbrRandomPhase[index][1], smoothedNoise)
				signalReal += noiseReal
				signalImag += noiseImag
			}
			rowRe[rp] = signalReal
			rp++
			rowIm[ip] = signalImag
			ip++
		}
	}
}

// adjustTimeSlotHQAddHarmonics is the 1:1 port of adjustTimeSlotHQ_AddHarmonics
// (env_calc.cpp:2804-2848): the inter-TES variant that only adds the additional
// harmonics. rowRe/rowIm are full QMF rows; base == lowSubband.
//
// C counterpart: adjustTimeSlotHQ_AddHarmonics (env_calc.cpp:2804).
func adjustTimeSlotHQAddHarmonics(rowRe, rowIm []int32, base int, hSbrCalEnv *SbrCalculateEnvelope, nrgs *EnvCalcNrgs, lowSubband, noSubbands, scaleChange int) {
	pSineLevel := nrgs.NrgSine[:]
	ptrHarmIndex := &hSbrCalEnv.HarmIndex

	harmIndex := *ptrHarmIndex
	freqInvFlag := lowSubband & 1

	*ptrHarmIndex = (harmIndex + 1) & 3

	for k := 0; k < noSubbands; k++ {
		sineLevel := pSineLevel[k]
		freqInvFlag ^= 1
		if sineLevel != 0 {
			signalReal := rowRe[base+k]
			signalImag := rowIm[base+k]
			sineLevel = nativeaac.ScaleValue(sineLevel, int32(scaleChange))
			if harmIndex&2 != 0 {
				sineLevel = -sineLevel
			}
			if harmIndex&1 == 0 {
				rowRe[base+k] = signalReal + sineLevel
			} else {
				if freqInvFlag == 0 {
					sineLevel = -sineLevel
				}
				rowIm[base+k] = signalImag + sineLevel
			}
		}
	}
}

// adjustTimeSlotHQ is the 1:1 port of adjustTimeSlotHQ (env_calc.cpp:2850-3050):
// amplify one timeslot of the complex (HQ) signal with the calculated gains, add
// adaptive noise, and add synthetic sines. rowRe/rowIm are full QMF rows; base
// == lowSubband.
//
// C counterpart: adjustTimeSlotHQ (env_calc.cpp:2850).
func adjustTimeSlotHQ(rowRe, rowIm []int32, base int, hSbrCalEnv *SbrCalculateEnvelope, nrgs *EnvCalcNrgs, lowSubband, noSubbands, scaleChange int, smoothRatio int16, noNoiseFlag, filtBufferNoiseShift int) {
	gain := nrgs.NrgGain[:]
	noiseLevel := nrgs.NoiseLevel[:]
	pSineLevel := nrgs.NrgSine[:]

	filtBuffer := hSbrCalEnv.FiltBuffer[:]
	filtBufferNoise := hSbrCalEnv.FiltBufferNoise[:]
	ptrHarmIndex := &hSbrCalEnv.HarmIndex
	ptrPhaseIndex := &hSbrCalEnv.PhaseIndex

	rp := base
	ip := base
	directRatio := int16(0x7FFF) - smoothRatio
	index := *ptrPhaseIndex
	harmIndex := *ptrHarmIndex
	freqInvFlag := lowSubband & 1
	var sineLevel int32
	var shift int
	var maxValNoise, minValNoise int32
	maxVal := maxValNrgHeadroom >> uint(scaleChange)
	minVal := -maxVal

	*ptrPhaseIndex = (index + noSubbands) & (sbrNFNoRandomVal - 1)
	*ptrHarmIndex = (harmIndex + 1) & 3

	filtBufferNoiseShift++
	if filtBufferNoiseShift < 0 {
		shift = nativeaac.FMinI(dfractBits-1, -filtBufferNoiseShift)
	} else {
		shift = nativeaac.FMinI(dfractBits-1, filtBufferNoiseShift)
		maxValNoise = maxValNrgHeadroom >> uint(shift)
		minValNoise = -maxValNoise
	}

	if smoothRatio > 0 {
		for k := 0; k < noSubbands; k++ {
			smoothedGain := fMultSD(smoothRatio, filtBuffer[k]) + fMultSD(directRatio, gain[k])

			var smoothedNoise int32
			if filtBufferNoiseShift < 0 {
				smoothedNoise = (fMultDiv2SD(smoothRatio, filtBufferNoise[k]) >> uint(shift)) + fMultSD(directRatio, noiseLevel[k])
			} else {
				smoothedNoise = fMultDiv2SD(smoothRatio, filtBufferNoise[k])
				smoothedNoise = (nativeaac.FMaxDBL(nativeaac.FMinDBL(smoothedNoise, maxValNoise), minValNoise) << uint(shift)) + fMultSD(directRatio, noiseLevel[k])
			}

			smoothedNoise = nativeaac.FMaxDBL(nativeaac.FMinDBL(smoothedNoise, int32(0x7FFFFFFF/2)), int32(-0x80000000/2))

			signalReal := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowRe[rp], smoothedGain), maxVal), minVal) << uint(scaleChange)
			signalImag := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowIm[ip], smoothedGain), maxVal), minVal) << uint(scaleChange)

			index++

			if pSineLevel[k] != 0 {
				sineLevel = pSineLevel[k]
				switch harmIndex {
				case 0:
					rowRe[rp] = signalReal + sineLevel
					rp++
					rowIm[ip] = signalImag
					ip++
				case 2:
					rowRe[rp] = signalReal - sineLevel
					rp++
					rowIm[ip] = signalImag
					ip++
				case 1:
					rowRe[rp] = signalReal
					rp++
					if freqInvFlag != 0 {
						rowIm[ip] = signalImag - sineLevel
					} else {
						rowIm[ip] = signalImag + sineLevel
					}
					ip++
				case 3:
					rowRe[rp] = signalReal
					rp++
					if freqInvFlag != 0 {
						rowIm[ip] = signalImag + sineLevel
					} else {
						rowIm[ip] = signalImag - sineLevel
					}
					ip++
				}
			} else {
				if noNoiseFlag != 0 {
					rowRe[rp] = signalReal
					rp++
					rowIm[ip] = signalImag
					ip++
				} else {
					index &= (sbrNFNoRandomVal - 1)
					noiseReal := fMultSD(sbrRandomPhase[index][0], smoothedNoise)
					noiseImag := fMultSD(sbrRandomPhase[index][1], smoothedNoise)
					rowRe[rp] = signalReal + noiseReal
					rp++
					rowIm[ip] = signalImag + noiseImag
					ip++
				}
			}
			freqInvFlag ^= 1
		}
	} else {
		for k := 0; k < noSubbands; k++ {
			smoothedGain := gain[k]
			signalReal := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowRe[rp], smoothedGain), maxVal), minVal) << uint(scaleChange)
			signalImag := nativeaac.FMaxDBL(nativeaac.FMinDBL(nativeaac.FMultDiv2DD(rowIm[ip], smoothedGain), maxVal), minVal) << uint(scaleChange)

			index++

			if sineLevel = pSineLevel[k]; sineLevel != 0 {
				switch harmIndex {
				case 0:
					signalReal += sineLevel
				case 1:
					if freqInvFlag != 0 {
						signalImag -= sineLevel
					} else {
						signalImag += sineLevel
					}
				case 2:
					signalReal -= sineLevel
				case 3:
					if freqInvFlag != 0 {
						signalImag += sineLevel
					} else {
						signalImag -= sineLevel
					}
				}
			} else {
				if noNoiseFlag == 0 {
					smoothedNoise := noiseLevel[k]
					index &= (sbrNFNoRandomVal - 1)
					noiseReal := fMultSD(sbrRandomPhase[index][0], smoothedNoise)
					noiseImag := fMultSD(sbrRandomPhase[index][1], smoothedNoise)
					signalReal += noiseReal
					signalImag += noiseImag
				}
			}
			rowRe[rp] = signalReal
			rp++
			rowIm[ip] = signalImag
			ip++

			freqInvFlag ^= 1
		}
	}
}

// boolToInt mirrors the C `!x` / ternary patterns that toggle freqInvFlag.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
