// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the pure-Go 1:1 port of the threshold-adjustment state
// structures the FDK-AAC encoder rate-control / psychoacoustic-adaptation loop
// (adj_thr.cpp / adj_thr_data.h) operates on. Every struct/field/const carries
// its C counterpart. These are pure integer (FIXP_DBL == int32, INT == int)
// state; there is no floating point. The aacfdk fence keeps a default
// `go build ./...` from linking any FDK-derived code.

package nativeaac

// avoidHoleState mirrors enum _avoid_hole_state (adj_thr.cpp:307): the per-sfb
// avoid-hole flag the threshold-reduction loop maintains.
const (
	noAH       = 0 // NO_AH
	ahInactive = 1 // AH_INACTIVE
	ahActive   = 2 // AH_ACTIVE
)

// constPartHeadroom mirrors #define CONSTPART_HEADROOM 4 (adj_thr.cpp:950): the
// extra fractional headroom the calcPeNoAH accumulation keeps for the constPart
// sum before shifting back into the PE_CONSTPART_SHIFT domain.
const constPartHeadroom = 4

// bresParam is the 1:1 port of BRES_PARAM (adj_thr_data.h:119-124): the
// bit-reservoir control parameters FDKaacEnc_calcBitSave / FDKaacEnc_calcBitSpend
// read. Long/short blocks each carry one.
type bresParam struct {
	clipSaveLow   int32 // clipSaveLow (FIXP_DBL)
	clipSaveHigh  int32 // clipSaveHigh (FIXP_DBL)
	minBitSave    int32 // minBitSave (FIXP_DBL)
	maxBitSave    int32 // maxBitSave (FIXP_DBL)
	clipSpendLow  int32 // clipSpendLow (FIXP_DBL)
	clipSpendHigh int32 // clipSpendHigh (FIXP_DBL)
	minBitSpend   int32 // minBitSpend (FIXP_DBL)
	maxBitSpend   int32 // maxBitSpend (FIXP_DBL)
}

// ahParam is the 1:1 port of AH_PARAM (adj_thr_data.h:126-129): avoid-hole
// parameters.
type ahParam struct {
	modifyMinSnr int // modifyMinSnr
	startSfbL    int // startSfbL
	startSfbS    int // startSfbS
}

// minSnrAdaptParam is the 1:1 port of MINSNR_ADAPT_PARAM (adj_thr_data.h:131-137):
// the parameters FDKaacEnc_adaptMinSnr reduces the per-sfb minSnr requirement by.
type minSnrAdaptParam struct {
	maxRed      int32 // maxRed (FIXP_DBL)
	startRatio  int32 // startRatio (FIXP_DBL)
	maxRatio    int32 // maxRatio (FIXP_DBL)
	redRatioFac int32 // redRatioFac (FIXP_DBL)
	redOffs     int32 // redOffs (FIXP_DBL)
}

// atsElement is the 1:1 port of ATS_ELEMENT (adj_thr_data.h:139-166): the
// per-element persistent threshold-adjustment state. Only the AAC-LC path is
// modelled; the vbr fields are carried for struct fidelity but unused on the
// CBR path.
type atsElement struct {
	peMin int // peMin
	peMax int // peMax

	peOffset int // peOffset

	bits2PeFactorM int32 // bits2PeFactor_m (FIXP_DBL)
	bits2PeFactorE int   // bits2PeFactor_e

	ahParam          ahParam          // ahParam
	minSnrAdaptParam minSnrAdaptParam // minSnrAdaptParam

	peLast              int   // peLast
	dynBitsLast         int   // dynBitsLast
	peCorrectionFactorM int32 // peCorrectionFactor_m (FIXP_DBL)
	peCorrectionFactorE int   // peCorrectionFactor_e

	vbrQualFactor   int32 // vbrQualFactor (FIXP_DBL)
	chaosMeasureOld int32 // chaosMeasureOld (FIXP_DBL)

	chaosMeasureEnFac [2]int32 // chaosMeasureEnFac[2] (FIXP_DBL)
	lastEnFacPatch    [2]int   // lastEnFacPatch[2]
}

// adjThrState is the 1:1 port of ADJ_THR_STATE (adj_thr_data.h:168-173): the
// shared threshold-adjustment state across elements.
type adjThrState struct {
	bresParamLong       bresParam      // bresParamLong
	bresParamShort      bresParam      // bresParamShort
	adjThrStateElem     [8]*atsElement // adjThrStateElem[8]
	bitDistributionMode int            // bitDistributionMode (AACENC_BIT_DISTRIBUTION_MODE)
	maxIter2ndGuess     int            // maxIter2ndGuess
}
