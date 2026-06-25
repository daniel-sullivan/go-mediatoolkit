// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Noise-floor estimator — the 1:1 port of libSBRenc/src/nf_est.cpp. For each
// noise band it derives the noise level from the orig-vs-SBR tonality quotas
// (biased by the inverse-filtering decision + a per-band offset), smooths across
// frames, and quantises to the ld64 domain the envelope coder then DPCM-codes.
// fdk-aac SBR is FIXED-POINT — EXACT integer parity. Scope: HE-AAC v1.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

const (
	maxNumNoiseCoeffs       = 5 // MAX_NUM_NOISE_COEFFS (sbr_def.h)
	noiseFloorOffsetScaling = 4 // NOISE_FLOOR_OFFSET_SCALING (nf_est.cpp:125)
)

// SbrNoiseFloorEstimate is the 1:1 port of struct SBR_NOISE_FLOOR_ESTIMATE
// (nf_est.h).
type SbrNoiseFloorEstimate struct {
	PrevNoiseLevels  [nfSmoothingLength][encMaxNumNoiseValues]int32
	NoiseFloorOffset [encMaxNumNoiseValues]int32
	SmoothFilter     []int32
	AnaMaxLevel      int32
	WeightFac        int32
	FreqBandTableQmf [encMaxNumNoiseValues + 1]int
	NoNoiseBands     int
	NoiseBands       int
	TimeSlots        int
	DiffThres        InvfMode
}

// smoothFilterNF / quantOffsetNF (nf_est.cpp:110,115).
var (
	smoothFilterNF = []int32{0x077f813d, 0x19999995, 0x2bb3b1f5, 0x33333335}
	quantOffsetNF  = int32(-0x04000000) // 0xfc000000 == ld64(0.25)
)

// smoothingOfNoiseLevels is the 1:1 port of smoothingOfNoiseLevels
// (nf_est.cpp:137-178).
func smoothingOfNoiseLevels(noiseLevels []int32, nEnvelopes, noNoiseBands int,
	prevNoiseLevels *[nfSmoothingLength][encMaxNumNoiseValues]int32,
	pSmoothFilter []int32, transientFlag int) {

	for env := 0; env < nEnvelopes; env++ {
		if transientFlag != 0 {
			for i := 0; i < nfSmoothingLength; i++ {
				copy(prevNoiseLevels[i][:noNoiseBands], noiseLevels[env*noNoiseBands:env*noNoiseBands+noNoiseBands])
			}
		} else {
			for i := 1; i < nfSmoothingLength; i++ {
				copy(prevNoiseLevels[i-1][:noNoiseBands], prevNoiseLevels[i][:noNoiseBands])
			}
			copy(prevNoiseLevels[nfSmoothingLength-1][:noNoiseBands], noiseLevels[env*noNoiseBands:env*noNoiseBands+noNoiseBands])
		}

		for band := 0; band < noNoiseBands; band++ {
			var accu int32
			for i := 0; i < nfSmoothingLength; i++ {
				accu += nativeaac.FMultDiv2DD(pSmoothFilter[i], prevNoiseLevels[i][band])
			}
			noiseLevels[band+env*noNoiseBands] = accu << 1
		}
	}
}

// qmfBasedNoiseFloorDetection is the 1:1 port (nf_est.cpp:190-315).
func qmfBasedNoiseFloorDetection(quotaMatrixOrig [][]int32, indexVector []int8,
	startIndex, stopIndex, startChannel, stopChannel int, anaMaxLevel,
	noiseFloorOffset int32, missingHarmonicFlag int, weightFac int32,
	diffThres, inverseFilteringLevel InvfMode) int32 {

	var meanOrig, meanSbr, diff int32
	invIndex := nativeaac.GetInvInt(stopIndex - startIndex)
	invChannel := nativeaac.GetInvInt(stopChannel - startChannel)

	if missingHarmonicFlag == 1 {
		for l := startChannel; l < stopChannel; l++ {
			var accu int32
			for k := startIndex; k < stopIndex; k++ {
				accu += nativeaac.FMultDiv2DD(quotaMatrixOrig[k][l], invIndex)
			}
			meanOrig = nativeaac.FMaxDBL(meanOrig, accu<<1)

			accu = 0
			for k := startIndex; k < stopIndex; k++ {
				accu += nativeaac.FMultDiv2DD(quotaMatrixOrig[k][indexVector[l]], invIndex)
			}
			meanSbr = nativeaac.FMaxDBL(meanSbr, accu<<1)
		}
	} else {
		for l := startChannel; l < stopChannel; l++ {
			var accu int32
			for k := startIndex; k < stopIndex; k++ {
				accu += nativeaac.FMultDiv2DD(quotaMatrixOrig[k][l], invIndex)
			}
			meanOrig += nativeaac.FMultDD(accu<<1, invChannel)

			accu = 0
			for k := startIndex; k < stopIndex; k++ {
				accu += nativeaac.FMultDiv2DD(quotaMatrixOrig[k][indexVector[l]], invIndex)
			}
			meanSbr += nativeaac.FMultDD(accu<<1, invChannel)
		}
	}

	// Small fix to avoid noise during silent passages.
	if meanOrig <= fl2f(0.000976562*relaxationFloat) && meanSbr <= fl2f(0.000976562*relaxationFloat) {
		meanOrig = fl2f(101.5936673 * relaxationFloat)
		meanSbr = fl2f(101.5936673 * relaxationFloat)
	}

	meanOrig = nativeaac.FMaxDBL(meanOrig, relaxation())
	meanSbr = nativeaac.FMaxDBL(meanSbr, relaxation())

	if missingHarmonicFlag == 1 || inverseFilteringLevel == InvfMidLevel ||
		inverseFilteringLevel == InvfLowLevel || inverseFilteringLevel == InvfOff ||
		inverseFilteringLevel <= diffThres {
		diff = relaxation()
	} else {
		accu, scale := nativeaac.FDivNorm(meanSbr, meanOrig)
		// fMax(RELAXATION, fMult(RELAXATION_FRACT, fMult(weightFac, accu)) >> (RELAXATION_SHIFT - scale))
		prod := nativeaac.FMultDD(relaxationFract(), nativeaac.FMultDD(weightFac, accu))
		sh := relaxationShift - int(scale)
		diff = nativeaac.FMaxDBL(relaxation(), shrSigned(prod, sh))
	}

	// change sign: noiseLevel = diff / meanOrig
	accu, scale := nativeaac.FDivNorm(diff, meanOrig)
	scale -= 2

	var noiseLevel int32
	if scale > 0 && accu > (encMaxvalDBL>>uint(scale)) {
		noiseLevel = encMaxvalDBL
	} else {
		noiseLevel = nativeaac.ScaleValue(accu, scale)
	}

	if missingHarmonicFlag == 0 {
		noiseLevel = fixMinDBL(nativeaac.FMultDD(noiseLevel, noiseFloorOffset),
			encMaxvalDBL>>noiseFloorOffsetScaling) << noiseFloorOffsetScaling
	}

	noiseLevel = fixMinDBL(noiseLevel, anaMaxLevel)
	return noiseLevel
}

// shrSigned is a C arithmetic-shift of a FIXP_DBL by sh (sh may be negative ->
// left shift), matching `value >> (RELAXATION_SHIFT - scale)` when that exponent
// is computed; the C code only ever uses non-negative shifts here, but we mirror
// the >> semantics exactly for an int32.
func shrSigned(v int32, sh int) int32 {
	if sh >= 0 {
		return v >> uint(sh)
	}
	return v << uint(-sh)
}

// fixMinDBL is fixMin on FIXP_DBL.
func fixMinDBL(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// SbrNoiseFloorEstimateQmf is the 1:1 port of FDKsbrEnc_sbrNoiseFloorEstimateQmf
// (nf_est.cpp:329-403).
func SbrNoiseFloorEstimateQmf(h *SbrNoiseFloorEstimate, frameInfo *SbrFrameInfo,
	noiseLevels []int32, quotaMatrixOrig [][]int32, indexVector []int8,
	missingHarmonicsFlag, startIndex int, numberOfEstimatesPerFrame uint,
	transientFrame int, pInvFiltLevels []InvfMode, sbrSyntaxFlags uint) {

	var startPos, stopPos [2]int
	noNoiseBands := h.NoNoiseBands
	freqBandTable := h.FreqBandTableQmf[:]
	nNoiseEnvelopes := frameInfo.NNoiseEnvelopes

	startPos[0] = startIndex
	minEst := int(numberOfEstimatesPerFrame)
	if minEst > 2 {
		minEst = 2
	}
	if nNoiseEnvelopes == 1 {
		stopPos[0] = startIndex + minEst
	} else {
		stopPos[0] = startIndex + 1
		startPos[1] = startIndex + 1
		stopPos[1] = startIndex + minEst
	}

	for env := 0; env < nNoiseEnvelopes; env++ {
		for band := 0; band < noNoiseBands; band++ {
			noiseLevels[band+env*noNoiseBands] = qmfBasedNoiseFloorDetection(
				quotaMatrixOrig, indexVector, startPos[env], stopPos[env],
				freqBandTable[band], freqBandTable[band+1], h.AnaMaxLevel,
				h.NoiseFloorOffset[band], missingHarmonicsFlag, h.WeightFac,
				h.DiffThres, pInvFiltLevels[band])
		}
	}

	smoothingOfNoiseLevels(noiseLevels, nNoiseEnvelopes, h.NoNoiseBands,
		&h.PrevNoiseLevels, h.SmoothFilter, transientFrame)

	for env := 0; env < nNoiseEnvelopes; env++ {
		for band := 0; band < noNoiseBands; band++ {
			noiseLevels[band+env*noNoiseBands] = noiseFloorOffset64() -
				nativeaac.CalcLdData(noiseLevels[band+env*noNoiseBands]+1) + quantOffsetNF
		}
	}
}

// downSampleLoRes is the 1:1 port of downSampleLoRes (nf_est.cpp:414-449).
func downSampleLoRes(vResult []int, numResult int, freqBandTableRef []uint8, numRef int) int {
	var vIndex [encMaxFreqCoeffs / 2]int
	orgLength := numRef
	resultLength := numResult
	vIndex[0] = 0
	i := 0
	for orgLength > 0 {
		i++
		step := orgLength / resultLength
		orgLength = orgLength - step
		resultLength--
		vIndex[i] = vIndex[i-1] + step
	}
	if i != numResult {
		return 1
	}
	for j := 0; j <= i; j++ {
		vResult[j] = int(freqBandTableRef[vIndex[j]])
	}
	return 0
}

// InitSbrNoiseFloorEstimate is the 1:1 port of FDKsbrEnc_InitSbrNoiseFloorEstimate
// (nf_est.cpp:460-534).
func InitSbrNoiseFloorEstimate(h *SbrNoiseFloorEstimate, anaMaxLevel int,
	freqBandTable []uint8, nSfb, noiseBands, noiseFloorOffset, timeSlots int,
	useSpeechConfig uint) int {

	*h = SbrNoiseFloorEstimate{}
	h.SmoothFilter = smoothFilterNF
	if useSpeechConfig != 0 {
		h.WeightFac = encMaxvalDBL
		h.DiffThres = InvfLowLevel
	} else {
		h.WeightFac = fl2f(0.25)
		h.DiffThres = InvfMidLevel
	}

	h.TimeSlots = timeSlots
	h.NoiseBands = noiseBands

	switch anaMaxLevel {
	case 6:
		h.AnaMaxLevel = encMaxvalDBL
	case 3:
		h.AnaMaxLevel = fl2f(0.5)
	case -3:
		h.AnaMaxLevel = fl2f(0.125)
	default:
		h.AnaMaxLevel = encMaxvalDBL
	}

	if ResetSbrNoiseFloorEstimate(h, freqBandTable, nSfb) != 0 {
		return 1
	}

	var tmp int32
	if noiseFloorOffset == 0 {
		tmp = encMaxvalDBL >> noiseFloorOffsetScaling
	} else {
		exp, qexp := nativeaac.FDivNorm(int32(noiseFloorOffset), 3)
		p, qtmp := nativeaac.FPow(2, dfractBitsCE-1, exp, qexp)
		tmp = nativeaac.ScaleValue(p, qtmp-noiseFloorOffsetScaling)
	}
	for i := 0; i < h.NoNoiseBands; i++ {
		h.NoiseFloorOffset[i] = tmp
	}
	return 0
}

// ResetSbrNoiseFloorEstimate is the 1:1 port of FDKsbrEnc_resetSbrNoiseFloorEstimate
// (nf_est.cpp:546-590).
func ResetSbrNoiseFloorEstimate(h *SbrNoiseFloorEstimate, freqBandTable []uint8, nSfb int) int {
	k2 := int(freqBandTable[nSfb])
	kx := int(freqBandTable[0])
	if h.NoiseBands == 0 {
		h.NoNoiseBands = 1
	} else {
		ratio, ratioE := nativeaac.FDivNorm(int32(k2), int32(kx))
		lg2, qlg2 := nativeaac.CalcLog2(ratio, ratioE)
		tmp := nativeaac.FMultDD(int32(h.NoiseBands)<<24, lg2)
		tmp = nativeaac.ScaleValue(tmp, qlg2-23)
		nNoiseBands := int((tmp + 1) >> 1)
		if nNoiseBands > maxNumNoiseCoeffs {
			nNoiseBands = maxNumNoiseCoeffs
		}
		if nNoiseBands == 0 {
			nNoiseBands = 1
		}
		h.NoNoiseBands = nNoiseBands
	}
	return downSampleLoRes(h.FreqBandTableQmf[:], h.NoNoiseBands, freqBandTable, nSfb)
}
