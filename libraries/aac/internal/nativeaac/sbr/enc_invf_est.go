// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Inverse-filtering-level detector — the 1:1 port of libSBRenc/src/invf_est.cpp.
// For each detector band it computes smoothed tonality means over the original
// vs the SBR-patched copy, classifies them into regions, and decides the
// per-band INVF level (the bs_invf_mode the bitstream writer emits). fdk-aac SBR
// is FIXED-POINT — EXACT integer parity. Scope: HE-AAC v1 (the AAC + AAC-speech
// detector parameter sets; no ELD-specific tuning).
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

const (
	maxNumRegions  = 5 // MAX_NUM_REGIONS (invf_est.h)
	scaleFacQuoExp = 0 // (documentary)
)

// DetectorParametersInvf is the 1:1 port of struct DETECTOR_PARAMETERS
// (invf_est.h): the quantiser step tables, region maps and energy-compensation.
type DetectorParametersInvf struct {
	QuantStepsSbr        []int32
	QuantStepsOrig       []int32
	NrgBorders           []int32
	NumRegionsSbr        int
	NumRegionsOrig       int
	NumRegionsNrg        int
	RegionSpace          [5][5]InvfMode
	RegionSpaceTransient [5][5]InvfMode
	EnergyCompFactor     [5]int
}

// DetectorValuesInvf is the 1:1 port of struct DETECTOR_VALUES (invf_est.h).
type DetectorValuesInvf struct {
	OrigQuotaMean              [invfSmoothingLength + 1]int32
	SbrQuotaMean               [invfSmoothingLength + 1]int32
	OrigQuotaMeanStrongest     [invfSmoothingLength + 1]int32
	SbrQuotaMeanStrongest      [invfSmoothingLength + 1]int32
	OrigQuotaMeanFilt          int32
	SbrQuotaMeanFilt           int32
	OrigQuotaMeanStrongestFilt int32
	SbrQuotaMeanStrongestFilt  int32
	OrigQuotaMax               int32
	SbrQuotaMax                int32
	AvgNrg                     int32
}

// SbrInvFiltEst is the 1:1 port of struct SBR_INV_FILT_EST (invf_est.h).
type SbrInvFiltEst struct {
	NumberOfStrongest int

	PrevRegionSbr  [encMaxNumNoiseValues]int
	PrevRegionOrig [encMaxNumNoiseValues]int

	FreqBandTableInvFilt [encMaxNumNoiseValues + 1]int
	NoDetectorBands      int
	NoDetectorBandsMax   int

	DetectorParams *DetectorParametersInvf

	PrevInvfMode   [encMaxNumNoiseValues]InvfMode
	DetectorValues [encMaxNumNoiseValues]DetectorValuesInvf

	NrgAvg int32
	WmQmf  [encMaxNumNoiseValues]int32
}

// quantStepsSbrInvf / quantStepsOrigInvf / nrgBordersInvf (invf_est.cpp:120-128).
var (
	quantStepsSbrInvf  = []int32{0x00400000, 0x02800000, 0x03800000, 0x04c00000}
	quantStepsOrigInvf = []int32{0x00000000, 0x00c00000, 0x01c00000, 0x02800000}
	nrgBordersInvf     = []int32{0x0c800000, 0x0f000000, 0x11800000, 0x14000000}
	hysteresisInvf     = int32(0x00400000) // hysteresis (invf_est.cpp:167)
)

// detectorParamsAAC is the 1:1 port of detectorParamsAAC (invf_est.cpp:130-165).
var detectorParamsAAC = DetectorParametersInvf{
	QuantStepsSbr:  quantStepsSbrInvf,
	QuantStepsOrig: quantStepsOrigInvf,
	NrgBorders:     nrgBordersInvf,
	NumRegionsSbr:  4,
	NumRegionsOrig: 4,
	NumRegionsNrg:  4,
	RegionSpace: [5][5]InvfMode{
		{InvfMidLevel, InvfLowLevel, InvfOff, InvfOff, InvfOff},
		{InvfMidLevel, InvfLowLevel, InvfOff, InvfOff, InvfOff},
		{InvfHighLevel, InvfMidLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
	},
	RegionSpaceTransient: [5][5]InvfMode{
		{InvfLowLevel, InvfLowLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfLowLevel, InvfLowLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfMidLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
	},
	EnergyCompFactor: [5]int{-4, -3, -2, -1, 0},
}

// detectorParamsAACSpeech is the 1:1 port (invf_est.cpp:173-208).
var detectorParamsAACSpeech = DetectorParametersInvf{
	QuantStepsSbr:  quantStepsSbrInvf,
	QuantStepsOrig: quantStepsOrigInvf,
	NrgBorders:     nrgBordersInvf,
	NumRegionsSbr:  4,
	NumRegionsOrig: 4,
	NumRegionsNrg:  4,
	RegionSpace: [5][5]InvfMode{
		{InvfMidLevel, InvfMidLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfMidLevel, InvfMidLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfMidLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
	},
	RegionSpaceTransient: [5][5]InvfMode{
		{InvfMidLevel, InvfMidLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfMidLevel, InvfMidLevel, InvfLowLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfMidLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
		{InvfHighLevel, InvfHighLevel, InvfMidLevel, InvfOff, InvfOff},
	},
	EnergyCompFactor: [5]int{-4, -3, -2, -1, 0},
}

// firTableInvf is the 1:1 port of fir_table (invf_est.cpp:215-227): the 5 FIR
// smoothing filters. firTableInvf[INVF_SMOOTHING_LENGTH] (==fir_2) is used.
var firTableInvf = [5][5]int32{
	{0x7fffffff, 0x00000000, 0x00000000, 0x00000000, 0x00000000}, // fir_0
	{0x2aaaaa80, 0x555554ff, 0x00000000, 0x00000000, 0x00000000}, // fir_1
	{0x10000000, 0x30000000, 0x40000000, 0x00000000, 0x00000000}, // fir_2
	{0x077f80e8, 0x199999a0, 0x2bb3b240, 0x33333340, 0x00000000}, // fir_3
	{0x04130598, 0x0ebdb000, 0x1becfa60, 0x2697a4c0, 0x2aaaaa80}, // fir_4
}

// shellsortFract is the 1:1 port of FDKsbrEnc_Shellsort_fract (sbr_misc.cpp:109).
func shellsortFract(in []int32, n int) {
	inc := 1
	for {
		inc = 3*inc + 1
		if inc > n {
			break
		}
	}
	for {
		inc = inc / 3
		for i := inc + 1; i <= n; i++ {
			v := in[i-1]
			j := i
			for in[j-inc-1] > v {
				in[j-1] = in[j-inc-1]
				j -= inc
				if j <= inc {
					break
				}
			}
			in[j-1] = v
		}
		if inc <= 1 {
			break
		}
	}
}

// calculateDetectorValues is the 1:1 port (invf_est.cpp:238-362). quotaMatrixOrig
// is [estimate][channel]; indexVector maps a highband channel to its lowband
// source (-1 == guard).
func calculateDetectorValues(quotaMatrixOrig [][]int32, indexVector []int8,
	nrgVector []int32, dv *DetectorValuesInvf, startChannel, stopChannel,
	startIndex, stopIndex, numberOfStrongest int) {

	filter := firTableInvf[invfSmoothingLength][:]
	var quotaVecOrig [64]int32
	var quotaVecSbr [64]int32

	invIndex := nativeaac.GetInvInt(stopIndex - startIndex)
	invChannel := nativeaac.GetInvInt(stopChannel - startChannel)

	dv.AvgNrg = 0
	for j := startIndex; j < stopIndex; j++ {
		for i := startChannel; i < stopChannel; i++ {
			quotaVecOrig[i] += nativeaac.FMultDD(quotaMatrixOrig[j][i], invIndex)
			if indexVector[i] != -1 {
				quotaVecSbr[i] += nativeaac.FMultDD(quotaMatrixOrig[j][indexVector[i]], invIndex)
			}
		}
		dv.AvgNrg += nativeaac.FMultDD(nrgVector[j], invIndex)
	}

	var origQuota, sbrQuota int32
	for i := startChannel; i < stopChannel; i++ {
		origQuota += nativeaac.FMultDiv2DD(quotaVecOrig[i], invChannel)
		sbrQuota += nativeaac.FMultDiv2DD(quotaVecSbr[i], invChannel)
	}

	shellsortFract(quotaVecOrig[startChannel:], stopChannel-startChannel)
	shellsortFract(quotaVecSbr[startChannel:], stopChannel-startChannel)

	var origQuotaMeanStrongest, sbrQuotaMeanStrongest int32
	temp := stopChannel - startChannel
	if numberOfStrongest < temp {
		temp = numberOfStrongest
	}
	invTemp := nativeaac.GetInvInt(temp)
	for i := 0; i < temp; i++ {
		origQuotaMeanStrongest += nativeaac.FMultDiv2DD(quotaVecOrig[i+stopChannel-temp], invTemp)
		sbrQuotaMeanStrongest += nativeaac.FMultDiv2DD(quotaVecSbr[i+stopChannel-temp], invTemp)
	}

	dv.OrigQuotaMax = quotaVecOrig[stopChannel-1]
	dv.SbrQuotaMax = quotaVecSbr[stopChannel-1]

	// shift the smoothing buffers (FDKmemmove of INVF_SMOOTHING_LENGTH elems)
	copy(dv.OrigQuotaMean[:invfSmoothingLength], dv.OrigQuotaMean[1:invfSmoothingLength+1])
	copy(dv.SbrQuotaMean[:invfSmoothingLength], dv.SbrQuotaMean[1:invfSmoothingLength+1])
	copy(dv.OrigQuotaMeanStrongest[:invfSmoothingLength], dv.OrigQuotaMeanStrongest[1:invfSmoothingLength+1])
	copy(dv.SbrQuotaMeanStrongest[:invfSmoothingLength], dv.SbrQuotaMeanStrongest[1:invfSmoothingLength+1])

	dv.OrigQuotaMean[invfSmoothingLength] = origQuota << 1
	dv.SbrQuotaMean[invfSmoothingLength] = sbrQuota << 1
	dv.OrigQuotaMeanStrongest[invfSmoothingLength] = origQuotaMeanStrongest << 1
	dv.SbrQuotaMeanStrongest[invfSmoothingLength] = sbrQuotaMeanStrongest << 1

	dv.OrigQuotaMeanFilt = 0
	dv.SbrQuotaMeanFilt = 0
	dv.OrigQuotaMeanStrongestFilt = 0
	dv.SbrQuotaMeanStrongestFilt = 0
	for i := 0; i < invfSmoothingLength+1; i++ {
		dv.OrigQuotaMeanFilt += nativeaac.FMultDD(dv.OrigQuotaMean[i], filter[i])
		dv.SbrQuotaMeanFilt += nativeaac.FMultDD(dv.SbrQuotaMean[i], filter[i])
		dv.OrigQuotaMeanStrongestFilt += nativeaac.FMultDD(dv.OrigQuotaMeanStrongest[i], filter[i])
		dv.SbrQuotaMeanStrongestFilt += nativeaac.FMultDD(dv.SbrQuotaMeanStrongest[i], filter[i])
	}
}

// findRegion is the 1:1 port of findRegion (invf_est.cpp:374-396).
func findRegion(currVal int32, borders []int32, numBorders int) int {
	if currVal < borders[0] {
		return 0
	}
	for i := 1; i < numBorders; i++ {
		if currVal >= borders[i-1] && currVal < borders[i] {
			return i
		}
	}
	if currVal >= borders[numBorders-1] {
		return numBorders
	}
	return 0
}

// decisionAlgorithm is the 1:1 port of decisionAlgorithm (invf_est.cpp:407-493).
func decisionAlgorithm(detectorParams *DetectorParametersInvf, dv *DetectorValuesInvf,
	transientFlag int, prevRegionSbr, prevRegionOrig *int) InvfMode {

	numRegionsSbr := detectorParams.NumRegionsSbr
	numRegionsOrig := detectorParams.NumRegionsOrig
	numRegionsNrg := detectorParams.NumRegionsNrg

	var quantStepsSbrTmp [maxNumRegions]int32
	var quantStepsOrigTmp [maxNumRegions]int32

	maxI32 := func(a, b int32) int32 {
		if a > b {
			return a
		}
		return b
	}

	// fMultDiv2(2*0.375, CalcLdData(max(filt,1)) + 0.31143075889)
	origQuotaMeanFilt := nativeaac.FMultDiv2DD(fl2f(2.0*0.375),
		nativeaac.CalcLdData(maxI32(dv.OrigQuotaMeanFilt, 1))+fl2f(0.31143075889))
	sbrQuotaMeanFilt := nativeaac.FMultDiv2DD(fl2f(2.0*0.375),
		nativeaac.CalcLdData(maxI32(dv.SbrQuotaMeanFilt, 1))+fl2f(0.31143075889))
	nrg := nativeaac.FMultDiv2DD(fl2f(2.0*0.375),
		nativeaac.CalcLdData(dv.AvgNrg+1)+fl2f(0.0625)+fl2f(0.6875))

	copy(quantStepsSbrTmp[:numRegionsSbr], detectorParams.QuantStepsSbr[:numRegionsSbr])
	copy(quantStepsOrigTmp[:numRegionsOrig], detectorParams.QuantStepsOrig[:numRegionsOrig])

	if *prevRegionSbr < numRegionsSbr {
		quantStepsSbrTmp[*prevRegionSbr] = detectorParams.QuantStepsSbr[*prevRegionSbr] + hysteresisInvf
	}
	if *prevRegionSbr > 0 {
		quantStepsSbrTmp[*prevRegionSbr-1] = detectorParams.QuantStepsSbr[*prevRegionSbr-1] - hysteresisInvf
	}
	if *prevRegionOrig < numRegionsOrig {
		quantStepsOrigTmp[*prevRegionOrig] = detectorParams.QuantStepsOrig[*prevRegionOrig] + hysteresisInvf
	}
	if *prevRegionOrig > 0 {
		quantStepsOrigTmp[*prevRegionOrig-1] = detectorParams.QuantStepsOrig[*prevRegionOrig-1] - hysteresisInvf
	}

	regionSbr := findRegion(sbrQuotaMeanFilt, quantStepsSbrTmp[:], numRegionsSbr)
	regionOrig := findRegion(origQuotaMeanFilt, quantStepsOrigTmp[:], numRegionsOrig)
	regionNrg := findRegion(nrg, detectorParams.NrgBorders, numRegionsNrg)

	*prevRegionSbr = regionSbr
	*prevRegionOrig = regionOrig

	var invFiltLevel int
	if transientFlag == 1 {
		invFiltLevel = int(detectorParams.RegionSpaceTransient[regionSbr][regionOrig])
	} else {
		invFiltLevel = int(detectorParams.RegionSpace[regionSbr][regionOrig])
	}
	invFiltLevel = invFiltLevel + detectorParams.EnergyCompFactor[regionNrg]
	if invFiltLevel < 0 {
		invFiltLevel = 0
	}
	return InvfMode(invFiltLevel)
}

// QmfInverseFilteringDetector is the 1:1 port of
// FDKsbrEnc_qmfInverseFilteringDetector (invf_est.cpp:508-539).
func QmfInverseFilteringDetector(h *SbrInvFiltEst, quotaMatrix [][]int32,
	nrgVector []int32, indexVector []int8, startIndex, stopIndex, transientFlag int,
	infVec []InvfMode) {

	for band := 0; band < h.NoDetectorBands; band++ {
		startChannel := h.FreqBandTableInvFilt[band]
		stopChannel := h.FreqBandTableInvFilt[band+1]

		calculateDetectorValues(quotaMatrix, indexVector, nrgVector,
			&h.DetectorValues[band], startChannel, stopChannel, startIndex,
			stopIndex, h.NumberOfStrongest)

		infVec[band] = decisionAlgorithm(h.DetectorParams, &h.DetectorValues[band],
			transientFlag, &h.PrevRegionSbr[band], &h.PrevRegionOrig[band])
	}
}

// InitInvFiltDetector is the 1:1 port of FDKsbrEnc_initInvFiltDetector
// (invf_est.cpp:550-585).
func InitInvFiltDetector(h *SbrInvFiltEst, freqBandTableDetector []int,
	numDetectorBands int, useSpeechConfig uint) int {

	*h = SbrInvFiltEst{}
	if useSpeechConfig != 0 {
		h.DetectorParams = &detectorParamsAACSpeech
	} else {
		h.DetectorParams = &detectorParamsAAC
	}
	h.NoDetectorBandsMax = numDetectorBands

	for i := 0; i < h.NoDetectorBandsMax; i++ {
		h.DetectorValues[i] = DetectorValuesInvf{}
		h.PrevInvfMode[i] = InvfOff
		h.PrevRegionOrig[i] = 0
		h.PrevRegionSbr[i] = 0
	}

	ResetInvFiltDetector(h, freqBandTableDetector, h.NoDetectorBandsMax)
	return 0
}

// ResetInvFiltDetector is the 1:1 port of FDKsbrEnc_resetInvFiltDetector
// (invf_est.cpp:597-609).
func ResetInvFiltDetector(h *SbrInvFiltEst, freqBandTableDetector []int, numDetectorBands int) int {
	h.NumberOfStrongest = 1
	copy(h.FreqBandTableInvFilt[:numDetectorBands+1], freqBandTableDetector[:numDetectorBands+1])
	h.NoDetectorBands = numDetectorBands
	return 0
}
