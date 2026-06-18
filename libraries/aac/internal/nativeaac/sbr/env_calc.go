// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

import "go-mediatoolkit/libraries/aac/internal/nativeaac"

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// SBR envelope-gain calculation, ported 1:1 from the vendored Fraunhofer FDK-AAC
// reference libSBRdec/src/env_calc.cpp — the largest SBR-decode module. The main
// entry point CalculateSbrEnvelope (calculateSbrEnvelope, env_calc.cpp:863)
// compares the energies present in the HF-generated highband (the transposed QMF
// bands from lpp_tran) against the reference energies dequantized from the
// bitstream (env_dec.cpp), and amplifies/attenuates each QMF band to the desired
// level, adding adaptive noise and synthetic sines. It produces the per-QMF-band
// complex gain adjustment written back into the analysis QMF buffer.
//
// HE-AAC v1 (STD) scope. The following C branches are deliberately EXCLUDED
// (out of HE-AAC v1 scope) and noted at their call sites:
//   - PVC (Predictive Vector Coding): the pvc_mode>0 paths in calculateSbrEnvelope
//     (expandPredEsg, mapSineFlagsPvc, the trailing-noise-envelope handling and
//     the prev*-band-table save at function end). For AAC, pPvcDynamicData->pvc_mode
//     is always 0, so those branches fold away exactly as in the C.
//   - ELD low-delay grid (adjustTimeSlot_EldGrid): the SBRDEC_ELD_GRID branch.
//     For HE-AAC v1 the flag is clear; only adjustTimeSlotLC (real LP) and the
//     adjustTimeSlotHQ family (complex) are reachable.
// Inter-TES (apply_inter_tes) IS in HE-AAC v1 scope and is ported faithfully.
//
// fdk-aac SBR is fixed-point: FIXP_DBL == int32 (Q-format mantissa), FIXP_SGL ==
// int16 (Q1.15), SCHAR == int8 exponent. Every op here is integer (the only
// "transcendental" is the table-based sqrtFixp_lookup), so this is bit-exact in
// any build — the parity oracle asserts EXACT integer equality.

// maxSfbNrgHeadroom is MAX_SFB_NRG_HEADROOM (env_calc.cpp:154).
const maxSfbNrgHeadroom = 1

// maxValNrgHeadroom is MAX_VAL_NRG_HEADROOM (env_calc.cpp:155):
// ((FIXP_DBL)MAXVAL_DBL) >> MAX_SFB_NRG_HEADROOM.
const maxValNrgHeadroom = int32(0x7FFFFFFF) >> maxSfbNrgHeadroom

// interTesSfChange is INTER_TES_SF_CHANGE (env_calc.cpp:499).
const interTesSfChange = 4

// boost4dB0p6279716 / boost4dB0p3139858 are the +4 dB boost-factor limits
// FL2FXCONST_DBL(0.6279716f) and FL2FXCONST_DBL(0.3139858f) (env_calc.cpp:1385/
// 1396). The C literals carry the float `f` suffix, so each is narrowed through
// float32 BEFORE the Q1.31 conversion (cf. the lpp_tran constants), which is
// load-bearing for bit-exactness. Materialised via Fl2fxconstDBLf so the
// constants are byte-identical to the f-suffixed C macro rather than a
// hand-transcribed hex literal.
var (
	boost4dB0p6279716 = nativeaac.Fl2fxconstDBLf(0.6279716) // 0x50615F80
	boost4dB0p3139858 = nativeaac.Fl2fxconstDBLf(0.3139858) // 0x2830AFC0
)

// maxGainExp / maxGainConcealExp (lpp_tran.h:147,150).
const (
	maxGainExp        = 34
	maxGainConcealExp = 1
)

// scale2Exp / exp2Scale are SCALE2EXP / EXP2SCALE (env_extr.h:414-415).
func scale2Exp(s int) int8 { return int8(15 - s) }
func exp2Scale(e int) int  { return 15 - int(e) }

// EnvCalcNrgs is the ENV_CALC_NRGS scratch struct (env_calc.cpp:157-171): the
// per-envelope reference/estimated/gain/noise/sine energies (mantissa + exponent
// arrays) calculateSbrEnvelope fills and the adjustTimeSlot* functions read.
type EnvCalcNrgs struct {
	NrgRef     [maxFreqCoeffs]int32 // nrgRef
	NrgEst     [maxFreqCoeffs]int32 // nrgEst
	NrgGain    [maxFreqCoeffs]int32 // nrgGain
	NoiseLevel [maxFreqCoeffs]int32 // noiseLevel
	NrgSine    [maxFreqCoeffs]int32 // nrgSine

	NrgRefE     [maxFreqCoeffs]int8 // nrgRef_e
	NrgEstE     [maxFreqCoeffs]int8 // nrgEst_e
	NrgGainE    [maxFreqCoeffs]int8 // nrgGain_e
	NoiseLevelE [maxFreqCoeffs]int8 // noiseLevel_e
	NrgSineE    [maxFreqCoeffs]int8 // nrgSine_e

	// exponent[0]: for ts < no_cols; [1]: for ts >= no_cols
	Exponent [2]int8 // exponent
}

// SbrCalculateEnvelope is the SBR_CALCULATE_ENVELOPE state (env_calc.h:114-143):
// the per-channel smoothing buffers + persistent harmonic/phase state that carry
// across frames. Only the HE-AAC v1 (non-PVC) subset is used here; the prev*
// PVC-only fields are kept for layout fidelity but never read on the AAC path.
type SbrCalculateEnvelope struct {
	FiltBuffer       [maxFreqCoeffs]int32 // filtBuffer (previous gains)
	FiltBufferNoise  [maxFreqCoeffs]int32 // filtBufferNoise (previous noise levels)
	FiltBufferE      [maxFreqCoeffs]int8  // filtBuffer_e
	FiltBufferNoiseE int8                 // filtBufferNoise_e

	StartUp     int // startUp
	PhaseIndex  int // phaseIndex
	PrevTranEnv int // prevTranEnv

	HarmFlagsPrev   [addHarmonicsFlagsSz]uint32 // harmFlagsPrev
	HarmIndex       uint8                       // harmIndex
	SbrPatchingMode int                         // sbrPatchingMode

	PrevSbrNoiseFloorLevel [maxNoiseCoeffs]int16 // prevSbrNoiseFloorLevel
	PrevNNfb               uint8
	PrevNSfb               [2]uint8
	PrevLoSubband          uint8
	PrevHiSubband          uint8
	PrevOvHighSubband      uint8
	PrevFreqBandTableLo    [maxFreqCoeffs/2 + 1]uint8
	PrevFreqBandTableHi    [maxFreqCoeffs + 1]uint8
	PrevFreqBandTableNoise [maxNoiseCoeffs + 1]uint8
	SinusoidalPositionPrev int8
	HarmFlagsPrevActive    [addHarmonicsFlagsSz]uint32 // harmFlagsPrevActive
}

// mapSineFlags is the 1:1 port of mapSineFlags (env_calc.cpp:245-318): map the
// per-Sfb add-harmonics flags from the bitstream onto per-QMF-band sine start
// positions, updating harmFlagsPrev/harmFlagsPrevActive for the next frame.
//
// C counterpart: mapSineFlags (env_calc.cpp:245).
func mapSineFlags(freqBandTable []uint8, nSfb int, addHarmonics, harmFlagsPrev, harmFlagsPrevActive []uint32, tranEnv int, sineMapped []int8) {
	bitcount := 31
	var harmFlagsQmfBands [addHarmonicsFlagsSz]uint32
	curFlags := 0 // index into addHarmonics

	// Reset the output vector first (32 means 'no sine').
	for i := 0; i < maxFreqCoeffs; i++ {
		sineMapped[i] = 32
	}
	for i := range harmFlagsPrevActive {
		harmFlagsPrevActive[i] = 0
	}

	for i := 0; i < nSfb; i++ {
		maskSfb := uint32(1) << uint(bitcount)

		if addHarmonics[curFlags]&maskSfb != 0 { // There is a sine in this band.
			lsb := int(freqBandTable[0])
			qmfBand := (int(freqBandTable[i]) + int(freqBandTable[i+1])) >> 1
			qmfBandDiv32 := qmfBand >> 5
			maskQmfBand := uint32(1) << uint(qmfBand&31)

			harmFlagsQmfBands[qmfBandDiv32] |= maskQmfBand

			if harmFlagsPrev[qmfBandDiv32]&maskQmfBand != 0 {
				sineMapped[qmfBand-lsb] = 0
			} else {
				sineMapped[qmfBand-lsb] = int8(tranEnv)
			}
			if int(sineMapped[qmfBand-lsb]) < pvcNTimeslot {
				harmFlagsPrevActive[qmfBandDiv32] |= maskQmfBand
			}
		}

		if bitcount == 0 {
			bitcount = 31
			curFlags++
		} else {
			bitcount--
		}
	}
	copy(harmFlagsPrev, harmFlagsQmfBands[:])
}

// aliasingReduction is the 1:1 port of aliasingReduction (env_calc.cpp:377-497):
// reduce gain-adjustment-induced aliasing for the real-valued (LP) filterbank.
//
// C counterpart: aliasingReduction (env_calc.cpp:377).
func aliasingReduction(degreeAlias []int32, nrgs *EnvCalcNrgs, useAliasReduction []uint8, noSubbands int) {
	nrgGain := nrgs.NrgGain[:]
	nrgGainE := nrgs.NrgGainE[:]
	nrgEst := nrgs.NrgEst[:]
	nrgEstE := nrgs.NrgEstE[:]
	grouping, index := 0, 0
	var groupVector [maxFreqCoeffs]int

	// Calculate grouping.
	for k := 0; k < noSubbands-1; k++ {
		if degreeAlias[k+1] != 0 && useAliasReduction[k] != 0 {
			if grouping == 0 {
				groupVector[index] = k
				index++
				grouping = 1
			} else {
				if groupVector[index-1]+3 == k {
					groupVector[index] = k + 1
					index++
					grouping = 0
				}
			}
		} else {
			if grouping != 0 {
				if useAliasReduction[k] != 0 {
					groupVector[index] = k + 1
				} else {
					groupVector[index] = k
				}
				index++
				grouping = 0
			}
		}
	}

	if grouping != 0 {
		groupVector[index] = noSubbands
		index++
	}
	noGroups := index >> 1

	// Calculate new gain.
	for group := 0; group < noGroups; group++ {
		var nrgOrig int32
		var nrgOrigE int8
		var nrgAmp int32
		var nrgAmpE int8
		var nrgMod int32
		var nrgModE int8

		startGroup := groupVector[2*group]
		stopGroup := groupVector[2*group+1]

		for k := startGroup; k < stopGroup; k++ {
			tmp := nrgEst[k]
			tmpE := nrgEstE[k]

			nrgOrig, nrgOrigE = fdkAddMantExp(tmp, tmpE, nrgOrig, nrgOrigE)

			tmp = nativeaac.FMultDD(tmp, nrgGain[k])
			tmpE = tmpE + nrgGainE[k]

			nrgAmp, nrgAmpE = fdkAddMantExp(tmp, tmpE, nrgAmp, nrgAmpE)
		}

		groupGain, groupGainE := fdkDivideMantExp(nrgAmp, nrgAmpE, nrgOrig, nrgOrigE)

		for k := startGroup; k < stopGroup; k++ {
			alpha := degreeAlias[k]
			if k < noSubbands-1 {
				if degreeAlias[k+1] > alpha {
					alpha = degreeAlias[k+1]
				}
			}

			// Modify gain depending on the degree of aliasing.
			nrgGain[k], nrgGainE[k] = fdkAddMantExp(
				nativeaac.FMultDD(alpha, groupGain), groupGainE,
				nativeaac.FMultDD(int32(0x7FFFFFFF)-alpha, nrgGain[k]),
				nrgGainE[k])

			tmp := nativeaac.FMultDD(nrgGain[k], nrgEst[k])
			tmpE := nrgGainE[k] + nrgEstE[k]

			nrgMod, nrgModE = fdkAddMantExp(tmp, tmpE, nrgMod, nrgModE)
		}

		compensation, compensationE := fdkDivideMantExp(nrgAmp, nrgAmpE, nrgMod, nrgModE)

		for k := startGroup; k < stopGroup; k++ {
			nrgGain[k] = nativeaac.FMultDD(nrgGain[k], compensation)
			nrgGainE[k] = nrgGainE[k] + compensationE
		}
	}
}

// CalculateSbrEnvelope is the 1:1 port of calculateSbrEnvelope
// (env_calc.cpp:863-1729) for the HE-AAC v1 (non-PVC) path. It applies the
// spectral envelope to the QMF analysis buffer in place (analysBufferReal /
// analysBufferImag), using the smoothing/harmonic state in hSbrCalEnv. useLP
// selects the real-only LP filterbank (analysBufferImag == nil); flags carries
// the SBR syntax flags (ELD grid excluded). frameErrorFlag selects the
// concealment gain limit.
//
// C counterpart: calculateSbrEnvelope (env_calc.cpp:863).
func CalculateSbrEnvelope(
	sbrScaleFactor *ScaleFactor,
	hSbrCalEnv *SbrCalculateEnvelope,
	hHeaderData *SbrHeaderData,
	hFrameData *SbrFrameData,
	analysBufferReal, analysBufferImag [][]int32,
	useLP bool,
	degreeAlias []int32,
	flags uint, frameErrorFlag bool,
) {
	var i, iStop, j, envNoise int
	borders := hFrameData.FrameInfo.Borders[:]
	firstStart := int(borders[0]) * int(hHeaderData.TimeStep)
	noiseLevels := hFrameData.SbrNoiseFloorLevel[:] // FIXP_SGL slice
	noiseLevelsOff := 0                             // index offset into noiseLevels
	hFreq := &hHeaderData.FreqBandData

	lowSubband := int(hFreq.LowSubband)
	highSubband := int(hFreq.HighSubband)
	noSubbands := highSubband - lowSubband

	ovHighSubband := int(hFreq.HighSubband)

	noNoiseBands := int(hFreq.NNfb)
	noSubFrameBands := hFreq.NSfb[:]
	noCols := int(hHeaderData.NumberTimeSlots) * int(hHeaderData.TimeStep)

	var sineMapped [maxFreqCoeffs]int8
	ovAdjE := scale2Exp(sbrScaleFactor.OvHbScale)
	var adjE int8
	var outputE int8
	var finalE int8
	iTESenable := hFrameData.ITESactive != 0
	iTESscaleChange := 0
	if iTESenable {
		iTESscaleChange = interTesSfChange
	}
	maxGainLimitE := int8(maxGainExp)
	if frameErrorFlag {
		maxGainLimitE = int8(maxGainConcealExp)
	}

	var smoothLength uint8

	pIenv := 0 // index into hFrameData.IEnvelope

	var useAliasReduction [64]uint8

	if int(hFreq.HighSubband) < int(hFreq.OvHighSubband) {
		ovHighSubband = int(hFreq.OvHighSubband)
	}

	// pvc_mode == 0 (HE-AAC v1): extract sine flags for all QMF bands.
	mapSineFlags(hFreq.FreqBandTable(1), int(noSubFrameBands[1]),
		hFrameData.AddHarmonics[:], hSbrCalEnv.HarmFlagsPrev[:],
		hSbrCalEnv.HarmFlagsPrevActive[:],
		int(hFrameData.FrameInfo.TranEnv), sineMapped[:])

	// Scan for maximum in buffered noise levels.
	if !useLP {
		adjE = hSbrCalEnv.FiltBufferNoiseE -
			int8(nativeaac.GetScalefactor(hSbrCalEnv.FiltBufferNoise[:noSubbands], noSubbands)) +
			int8(maxSfbNrgHeadroom)
	}

	// Scan for maximum reference energy to select adj_e and final_e (pvc_mode==0).
	for i = 0; i < int(hFrameData.FrameInfo.NEnvelopes); i++ {
		maxSfbNrgE := -fractBits + nrgExpOffset // start value for maximum search

		// Fetch frequency resolution for current envelope.
		for jj := int(noSubFrameBands[hFrameData.FrameInfo.FreqRes[i]]); jj != 0; jj-- {
			maxSfbNrgE = nativeaac.FMaxI(maxSfbNrgE, int(int32(hFrameData.IEnvelope[pIenv])&maskE))
			pIenv++
		}
		maxSfbNrgE -= nrgExpOffset

		// Energy -> magnitude (sqrt halves exponent).
		maxSfbNrgE = (maxSfbNrgE + 1) >> 1

		maxSfbNrgE += 6 + maxSfbNrgHeadroom

		if int(borders[i]) < int(hHeaderData.NumberTimeSlots) {
			adjE = int8(nativeaac.FMaxI(maxSfbNrgE, int(adjE)))
		}

		if int(borders[i+1]) > int(hHeaderData.NumberTimeSlots) {
			finalE = int8(nativeaac.FMaxI(maxSfbNrgE, int(finalE)))
		}
	}

	// Calculate adjustment factors and apply them for every envelope.
	pIenv = 0

	i = 0
	iStop = int(hFrameData.FrameInfo.NEnvelopes)
	for ; i < iStop; i++ {
		noiseE := int8(0)
		inputE := scale2Exp(sbrScaleFactor.HbScale)
		var pNrgs EnvCalcNrgs // C_ALLOC_SCRATCH_START + FDKmemclear

		startPos := int(hHeaderData.TimeStep) * int(borders[i])
		stopPos := int(hHeaderData.TimeStep) * int(borders[i+1])
		freqRes := int(hFrameData.FrameInfo.FreqRes[i])

		// If the start-pos of the current envelope equals the stop pos of the
		// current noise envelope, choose the next noise-floor.
		if int(borders[i]) == int(hFrameData.FrameInfo.BordersNoise[envNoise+1]) {
			noiseLevelsOff += noNoiseBands
			envNoise++
		}

		var noNoiseFlag int
		if i == int(hFrameData.FrameInfo.TranEnv) || i == hSbrCalEnv.PrevTranEnv { // attack
			noNoiseFlag = 1
			if !useLP {
				smoothLength = 0 // No smoothing on attacks!
			}
		} else {
			noNoiseFlag = 0
			if !useLP {
				smoothLength = (1 - hHeaderData.BsData.SmoothingLength) << 2 // 0 or 4
			}
		}

		// Energy estimation in transposed highband.
		var imagArg [][]int32
		if !useLP {
			imagArg = analysBufferImag
		}
		if hHeaderData.BsData.InterpolFreq != 0 {
			calcNrgPerSubband(analysBufferReal, imagArg, lowSubband, highSubband,
				startPos, stopPos, inputE, pNrgs.NrgEst[:], pNrgs.NrgEstE[:])
		} else {
			calcNrgPerSfb(analysBufferReal, imagArg, int(noSubFrameBands[freqRes]),
				hFreq.FreqBandTable(freqRes), startPos, stopPos, inputE,
				pNrgs.NrgEst[:], pNrgs.NrgEstE[:])
		}

		// Calculate subband gains.
		{
			table := hFreq.FreqBandTable(freqRes)
			pUiNoise := 1 // index into hFreq.FreqBandTableNoise (start at [1])

			pNoiseLevels := noiseLevelsOff // index into noiseLevels

			tmpNoise := int32(int16(int32(noiseLevels[pNoiseLevels])&maskM)) << 16 // FX_SGL2FX_DBL
			tmpNoiseE := int8((int32(noiseLevels[pNoiseLevels]) & maskE) - noiseExpOff)
			pNoiseLevels++

			cc := 0
			c := 0
			for jj := 0; jj < int(noSubFrameBands[freqRes]); jj++ {
				refNrg := int32(int16(int32(hFrameData.IEnvelope[pIenv])&maskM)) << 16 // FX_SGL2FX_DBL
				refNrgE := int8((int32(hFrameData.IEnvelope[pIenv]) & maskE) - nrgExpOffset)

				var sinePresentFlag uint8
				li := int(table[jj])
				ui := int(table[jj+1])

				for k := li; k < ui; k++ {
					if i >= int(sineMapped[cc]) {
						sinePresentFlag |= 1
					}
					cc++
				}

				for k := li; k < ui; k++ {
					if k >= int(hFreq.FreqBandTableNoise[pUiNoise]) {
						tmpNoise = int32(int16(int32(noiseLevels[pNoiseLevels])&maskM)) << 16 // FX_SGL2FX_DBL
						tmpNoiseE = int8((int32(noiseLevels[pNoiseLevels]) & maskE) - noiseExpOff)
						pNoiseLevels++
						pUiNoise++
					}

					if useLP {
						if sinePresentFlag != 0 {
							useAliasReduction[k-lowSubband] = 0
						} else {
							useAliasReduction[k-lowSubband] = 1
						}
					}

					pNrgs.NrgSine[c] = 0
					pNrgs.NrgSineE[c] = 0

					sineMappedFlag := uint8(0)
					if i >= int(sineMapped[c]) {
						sineMappedFlag = 1
					}
					calcSubbandGain(refNrg, refNrgE, &pNrgs, c, tmpNoise, tmpNoiseE,
						sinePresentFlag, sineMappedFlag, noNoiseFlag)

					pNrgs.NrgRef[c] = refNrg
					pNrgs.NrgRefE[c] = refNrgE

					c++
				}
				pIenv++
			}
		}

		// Noise limiting.
		for c := 0; c < int(hFreq.NoLimiterBands); c++ {
			var accu int32
			var accuE int8

			sumRef, sumRefE, maxGain, maxGainE := calcAvgGain(&pNrgs,
				int(hFreq.LimiterBandTab[c]), int(hFreq.LimiterBandTab[c+1]))

			// Multiply maxGain with limiterGain.
			maxGain = fMultSD(sbrLimGainsM[hHeaderData.BsData.LimiterGains], maxGain) // fixmul_DS commutes to _SD
			maxGainLimGainSumE := int(maxGainE) + int(sbrLimGainsE[hHeaderData.BsData.LimiterGains])
			if maxGainLimGainSumE > 127 {
				maxGainE = 127
			} else {
				maxGainE = int8(maxGainLimGainSumE)
			}

			// Scale mantissa of MaxGain into range between 0.5 and 1.
			if maxGain == 0 {
				maxGainE = -fractBits
			} else {
				charTemp := int8(nativeaac.CountLeadingBits(maxGain))
				maxGainE -= charTemp
				maxGain <<= uint(charTemp)
			}

			if maxGainE >= maxGainLimitE { // upper limit (e.g. 96 dB)
				maxGain = 0x40000000 // FL2FXCONST_DBL(0.5f)
				maxGainE = maxGainLimitE
			}

			// Every subband gain is compared to the scaled average gain and limited.
			for k := int(hFreq.LimiterBandTab[c]); k < int(hFreq.LimiterBandTab[c+1]); k++ {
				if pNrgs.NrgGainE[k] > maxGainE ||
					(pNrgs.NrgGainE[k] == maxGainE && pNrgs.NrgGain[k] > maxGain) {
					noiseAmp, noiseAmpE := fdkDivideMantExp(maxGain, maxGainE,
						pNrgs.NrgGain[k], pNrgs.NrgGainE[k])
					pNrgs.NoiseLevel[k] = nativeaac.FMultDD(pNrgs.NoiseLevel[k], noiseAmp)
					pNrgs.NoiseLevelE[k] += noiseAmpE
					pNrgs.NrgGain[k] = maxGain
					pNrgs.NrgGainE[k] = maxGainE
				}
			}

			// Boost gain.
			for k := int(hFreq.LimiterBandTab[c]); k < int(hFreq.LimiterBandTab[c+1]); k++ {
				tmp := nativeaac.FMultDD(pNrgs.NrgGain[k], pNrgs.NrgEst[k])
				tmpE := pNrgs.NrgGainE[k] + pNrgs.NrgEstE[k]
				accu, accuE = fdkAddMantExp(tmp, tmpE, accu, accuE)

				if pNrgs.NrgSine[k] != 0 {
					accu, accuE = fdkAddMantExp(pNrgs.NrgSine[k], pNrgs.NrgSineE[k], accu, accuE)
				} else {
					if noNoiseFlag == 0 {
						accu, accuE = fdkAddMantExp(pNrgs.NoiseLevel[k], pNrgs.NoiseLevelE[k], accu, accuE)
					}
				}
			}

			var boostGain int32
			var boostGainE int8
			if accu == 0 { // If divisor is 0, limit quotient to +4 dB.
				boostGain = boost4dB0p6279716
				boostGainE = 2
			} else {
				div, divE := nativeaac.FDivNorm(sumRef, accu)
				boostGain = div
				boostGainE = sumRefE - accuE + int8(divE)
			}

			// Result too high? --> Limit the boost factor to +4 dB.
			if boostGainE > 3 ||
				(boostGainE == 2 && boostGain > boost4dB0p6279716) ||
				(boostGainE == 3 && boostGain > boost4dB0p3139858) {
				boostGain = boost4dB0p6279716
				boostGainE = 2
			}
			// Multiply all signal components with the boost factor.
			for k := int(hFreq.LimiterBandTab[c]); k < int(hFreq.LimiterBandTab[c+1]); k++ {
				pNrgs.NrgGain[k] = nativeaac.FMultDiv2DD(pNrgs.NrgGain[k], boostGain)
				pNrgs.NrgGainE[k] = pNrgs.NrgGainE[k] + boostGainE + 1

				pNrgs.NrgSine[k] = nativeaac.FMultDiv2DD(pNrgs.NrgSine[k], boostGain)
				pNrgs.NrgSineE[k] = pNrgs.NrgSineE[k] + boostGainE + 1

				pNrgs.NoiseLevel[k] = nativeaac.FMultDiv2DD(pNrgs.NoiseLevel[k], boostGain)
				pNrgs.NoiseLevelE[k] = pNrgs.NoiseLevelE[k] + boostGainE + 1
			}
		}
		// End of noise limiting.

		if useLP {
			aliasingReduction(degreeAlias[lowSubband:], &pNrgs, useAliasReduction[:], noSubbands)
		}

		if startPos < noCols {
			noiseE = adjE
		} else {
			noiseE = finalE
		}

		if startPos >= noCols {
			diff := int(hSbrCalEnv.FiltBufferNoiseE) - int(noiseE)
			if diff > 0 {
				s := int(nativeaac.GetScalefactor(hSbrCalEnv.FiltBufferNoise[:noSubbands], noSubbands))
				if diff > s {
					finalE += int8(diff - s)
					noiseE = finalE
				}
			}
		}

		// Convert energies to amplitude levels.
		for k := 0; k < noSubbands; k++ {
			pNrgs.NrgSine[k], pNrgs.NrgSineE[k] = fdkSqrtMantExp(pNrgs.NrgSine[k], pNrgs.NrgSineE[k], &noiseE)
			pNrgs.NrgGain[k], pNrgs.NrgGainE[k] = fdkSqrtMantExp(pNrgs.NrgGain[k], pNrgs.NrgGainE[k], nil)
			pNrgs.NoiseLevel[k], pNrgs.NoiseLevelE[k] = fdkSqrtMantExp(pNrgs.NoiseLevel[k], pNrgs.NoiseLevelE[k], &noiseE)
		}

		// Apply calculated gains and adaptive noise (assembleHfSignals).
		{
			var scaleChange, scChange int
			var smoothRatio int16
			filtBufferNoiseShift := 0

			// Initialize smoothing buffers with the first valid values.
			if hSbrCalEnv.StartUp != 0 {
				if !useLP {
					hSbrCalEnv.FiltBufferNoiseE = noiseE
					copy(hSbrCalEnv.FiltBufferE[:noSubbands], pNrgs.NrgGainE[:noSubbands])
					copy(hSbrCalEnv.FiltBufferNoise[:noSubbands], pNrgs.NoiseLevel[:noSubbands])
					copy(hSbrCalEnv.FiltBuffer[:noSubbands], pNrgs.NrgGain[:noSubbands])
				}
				hSbrCalEnv.StartUp = 0
			}

			if !useLP {
				equalizeFiltBufferExp(hSbrCalEnv.FiltBuffer[:], hSbrCalEnv.FiltBufferE[:],
					pNrgs.NrgGain[:], pNrgs.NrgGainE[:], noSubbands)

				if int(hSbrCalEnv.FiltBufferNoiseE)-int(noiseE) >= 0 {
					shift := nativeaac.FMinI(dfractBits-1, int(hSbrCalEnv.FiltBufferNoiseE)-int(noiseE))
					for k := 0; k < noSubbands; k++ {
						hSbrCalEnv.FiltBufferNoise[k] <<= uint(shift)
					}
				} else {
					shift := nativeaac.FMinI(dfractBits-1, -(int(hSbrCalEnv.FiltBufferNoiseE) - int(noiseE)))
					for k := 0; k < noSubbands; k++ {
						hSbrCalEnv.FiltBufferNoise[k] >>= uint(shift)
					}
				}
				hSbrCalEnv.FiltBufferNoiseE = noiseE
			}

			// Find best scaling.
			scaleChange = -(dfractBits - 1)
			for k := 0; k < noSubbands; k++ {
				scaleChange = nativeaac.FMaxI(scaleChange, int(pNrgs.NrgGainE[k]))
			}
			if startPos < noCols {
				scChange = int(adjE) - int(inputE)
			} else {
				scChange = int(finalE) - int(inputE)
			}

			if (scaleChange - scChange + 1) < 0 {
				scaleChange -= scaleChange - scChange + 1
			}

			scaleChange = (scaleChange - scChange) + 1

			for k := 0; k < noSubbands; k++ {
				sc := scaleChange - int(pNrgs.NrgGainE[k]) + (scChange - 1)
				pNrgs.NrgGain[k] >>= uint(nativeaac.FMinI(sc, dfractBits-1))
				pNrgs.NrgGainE[k] += int8(sc)
			}

			if !useLP {
				for k := 0; k < noSubbands; k++ {
					sc := scaleChange - int(hSbrCalEnv.FiltBufferE[k]) + (scChange - 1)
					hSbrCalEnv.FiltBuffer[k] >>= uint(nativeaac.FMinI(sc, dfractBits-1))
				}
			}

			for j = startPos; j < stopPos; j++ {
				if j == noCols && startPos < noCols {
					shift := int(noiseE) - int(finalE)
					if !useLP {
						filtBufferNoiseShift = shift
					}
					if shift >= 0 {
						shift = nativeaac.FMinI(dfractBits-1, shift)
						for k := 0; k < noSubbands; k++ {
							pNrgs.NrgSine[k] <<= uint(shift)
							pNrgs.NoiseLevel[k] <<= uint(shift)
						}
					} else {
						shift = nativeaac.FMinI(dfractBits-1, -shift)
						for k := 0; k < noSubbands; k++ {
							pNrgs.NrgSine[k] >>= uint(shift)
							pNrgs.NoiseLevel[k] >>= uint(shift)
						}
					}

					noiseE = finalE
					if !useLP {
						hSbrCalEnv.FiltBufferNoiseE = noiseE
					}

					scChange -= int(finalE) - int(inputE)

					if scChange < 0 {
						for k := 0; k < noSubbands; k++ {
							pNrgs.NrgGain[k] >>= uint(-scChange)
							pNrgs.NrgGainE[k] += int8(-scChange)
						}
						if !useLP {
							for k := 0; k < noSubbands; k++ {
								hSbrCalEnv.FiltBuffer[k] >>= uint(-scChange)
							}
						}
					} else {
						scaleChange += scChange
					}
				} // if

				if !useLP {
					// Prevent the smoothing filter from running on constant levels.
					if j-startPos < int(smoothLength) {
						smoothRatio = sbrSmoothFilter[j-startPos]
					} else {
						smoothRatio = 0
					}

					if iTESenable {
						adjustTimeSlotHQGainAndNoise(
							analysBufferReal[j], analysBufferImag[j], lowSubband,
							hSbrCalEnv, &pNrgs, lowSubband, noSubbands,
							nativeaac.FMinI(scaleChange, dfractBits-1),
							smoothRatio, noNoiseFlag, filtBufferNoiseShift)
					} else {
						adjustTimeSlotHQ(
							analysBufferReal[j], analysBufferImag[j], lowSubband,
							hSbrCalEnv, &pNrgs, lowSubband, noSubbands,
							nativeaac.FMinI(scaleChange, dfractBits-1),
							smoothRatio, noNoiseFlag, filtBufferNoiseShift)
					}
				} else {
					// ELD grid excluded (HE-AAC v1): only adjustTimeSlotLC.
					adjustTimeSlotLC(analysBufferReal[j], lowSubband, &pNrgs,
						&hSbrCalEnv.HarmIndex, lowSubband, noSubbands,
						nativeaac.FMinI(scaleChange, dfractBits-1), noNoiseFlag,
						&hSbrCalEnv.PhaseIndex)
				}
				idx := 0
				if j >= noCols {
					idx = 1
				}
				pNrgs.Exponent[idx] = int8((15 - sbrScaleFactor.HbScale) + int(pNrgs.NrgGainE[0]) + 1 - scaleChange)
			} // for

			if iTESenable {
				applyInterTes(analysBufferReal, analysBufferImag, sbrScaleFactor,
					pNrgs.Exponent, int(hHeaderData.TimeStep), startPos, stopPos,
					lowSubband, noSubbands, hFrameData.InterTempShapeMode[i])

				// add additional harmonics
				for j = startPos; j < stopPos; j++ {
					scaleChange = 0
					if startPos <= noCols && stopPos > noCols {
						scaleChange = int(pNrgs.Exponent[1]) - int(pNrgs.Exponent[0])
					}
					extra := 0
					if j < noCols {
						extra = scaleChange
					}
					adjustTimeSlotHQAddHarmonics(
						analysBufferReal[j], analysBufferImag[j], lowSubband,
						hSbrCalEnv, &pNrgs, lowSubband, noSubbands,
						-iTESscaleChange+extra)
				}
			}

			if !useLP {
				copy(hSbrCalEnv.FiltBuffer[:noSubbands], pNrgs.NrgGain[:noSubbands])
				copy(hSbrCalEnv.FiltBufferE[:noSubbands], pNrgs.NrgGainE[:noSubbands])
				copy(hSbrCalEnv.FiltBufferNoise[:noSubbands], pNrgs.NoiseLevel[:noSubbands])
			}
		}
	}

	// adapt adj_e to the scale change caused by apply_inter_tes()
	adjE += int8(iTESscaleChange)

	// Rescale output samples.
	{
		var imagArg [][]int32
		if !useLP {
			imagArg = analysBufferImag
		}
		maxVal := maxSubbandSample(analysBufferReal, imagArg, lowSubband, ovHighSubband, 0, firstStart)
		ovReserve := int(nativeaac.CountLeadingBits(maxVal))

		maxVal = maxSubbandSample(analysBufferReal, imagArg, lowSubband, highSubband, firstStart, noCols)
		reserve := int(nativeaac.CountLeadingBits(maxVal))

		outputE = int8(nativeaac.FMaxI(int(ovAdjE)-ovReserve, int(adjE)-reserve))

		rescaleSubbandSamples(analysBufferReal, imagArg, lowSubband, ovHighSubband, 0, firstStart, int(ovAdjE)-int(outputE))
		rescaleSubbandSamples(analysBufferReal, imagArg, lowSubband, highSubband, firstStart, noCols, int(adjE)-int(outputE))
	}

	// Update hb_scale.
	sbrScaleFactor.HbScale = exp2Scale(int(outputE))

	// Save the current final exponent for the next frame.
	sbrScaleFactor.OvHbScale = exp2Scale(int(finalE) + iTESscaleChange)

	// Remember to the next frame that the transient will occur in the first
	// envelope (if tranEnv == nEnvelopes).
	if int(hFrameData.FrameInfo.TranEnv) == int(hFrameData.FrameInfo.NEnvelopes) {
		hSbrCalEnv.PrevTranEnv = 0
	} else {
		hSbrCalEnv.PrevTranEnv = -1
	}
}
