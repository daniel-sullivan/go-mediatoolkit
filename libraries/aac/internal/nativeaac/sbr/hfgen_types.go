// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// HF-generation-specific structs, enums and consts for the SBR high-frequency-
// generation tools (lpp_tran.cpp / hbe.cpp / HFgen_preFlat.cpp /
// autocorr2nd.cpp). 1:1 ports of the libSBRdec / libFDK type declarations.
//
// HARD RULE (5) — one coherent definition per shared symbol: the shared SBR-
// decode constants from lpp_tran.h (maxNoiseEnvelopes, maxNoiseCoeffs,
// maxNumNoiseValues, maxNumLimiters, maxInvfBands, maxEnvelopes, maxFreqCoeffs)
// and the INVF_MODE enum (invfMode / invfOff..invfSwitched) are already owned by
// sbr_consts.go and types.go (the broader SBR-decode batch); this file REUSES
// them and only defines the symbols unique to HF generation.

// HF-generation-specific consts (the macros only the transposer / HBE use).
const (
	lpcOrder       = 2 // LPC_ORDER (lpp_tran.h:157)
	lpcScaleFactor = 2 // LPC_SCALE_FACTOR (lpp_tran.cpp:135)
	qmfOutScale    = 8 // QMF_OUT_SCALE (lpp_tran.h:118)

	maxNumPatches = 6 // MAX_NUM_PATCHES (lpp_tran.h:161)
	shiftStartSb  = 1 // SHIFT_START_SB: lowest source subband (lpp_tran.h:162)

	maxNumPatchesHBE   = 6   // MAX_NUM_PATCHES_HBE (hbe.h:116)
	maxStretchHBE      = 4   // MAX_STRETCH_HBE (hbe.h:118)
	qmfSynthChannels   = 64  // QMF_SYNTH_CHANNELS (hbe.h:109)
	hbeQmfStateAnaSize = 400 // HBE_QMF_FILTER_STATE_ANA_SIZE (hbe.h:112)
	hbeQmfStateSynSize = 200 // HBE_QMF_FILTER_STATE_SYN_SIZE (hbe.h:113)
)

// patchParam is PATCH_PARAM (lpp_tran.h:173-187): the parameter set for one
// patch (a contiguous run of high bands filled from a low-band source range).
type patchParam struct {
	sourceStartBand uint8 // sourceStartBand
	sourceStopBand  uint8 // sourceStopBand (exclusive)
	guardStartBand  uint8 // guardStartBand
	targetStartBand uint8 // targetStartBand
	targetBandOffs  uint8 // targetBandOffs == targetStartBand - sourceStartBand
	numBandsInPatch uint8 // numBandsInPatch
}

// whiteningFactors is WHITENING_FACTORS (lpp_tran.h:191-197): the pole-moving
// (bandwidth-expansion) factors for each whitening level, selected by crossover
// frequency.
type whiteningFactors struct {
	off             int32 // off
	transitionLevel int32 // transitionLevel
	lowLevel        int32 // lowLevel
	midLevel        int32 // midLevel
	highLevel       int32 // highLevel
}

// transposerSettings is TRANSPOSER_SETTINGS (lpp_tran.h:201-216): the
// header-reset-time transposer settings shared by both channels.
type transposerSettings struct {
	nCols           uint8                    // nCols: subsamples in a codec frame
	noOfPatches     uint8                    // noOfPatches
	lbStartPatching uint8                    // lbStartPatching
	lbStopPatching  uint8                    // lbStopPatching
	bwBorders       [maxNumNoiseValues]uint8 // bwBorders
	patchParam      [maxNumPatches + 1]patchParam
	whFactors       whiteningFactors // whFactors
	overlap         uint8            // overlap
}

// sbrLppTrans is SBR_LPP_TRANS (lpp_tran.h:218-232): the per-channel transposer
// state. The filter-state arrays are [LPC_ORDER + 3*4][32 or 64] FIXP_DBL.
type sbrLppTrans struct {
	pSettings *transposerSettings // pSettings

	bwVectorOld [maxNumPatches]int32 // bwVectorOld

	lpcFilterStatesRealLegSBR [lpcOrder + 3*4][32]int32 // lpcFilterStatesRealLegSBR
	lpcFilterStatesImagLegSBR [lpcOrder + 3*4][32]int32 // lpcFilterStatesImagLegSBR
	lpcFilterStatesRealHBE    [lpcOrder + 3*4][64]int32 // lpcFilterStatesRealHBE
	lpcFilterStatesImagHBE    [lpcOrder + 3*4][64]int32 // lpcFilterStatesImagHBE
}

// acorrCoefs is ACORR_COEFS (autocorr2nd.h:108-120): the 2nd-order
// autocorrelation coefficients and determinant the LPC analysis produces.
type acorrCoefs struct {
	r00r     int32 // r00r
	r11r     int32 // r11r
	r22r     int32 // r22r
	r01r     int32 // r01r
	r02r     int32 // r02r
	r12r     int32 // r12r
	r01i     int32 // r01i
	r02i     int32 // r02i
	r12i     int32 // r12i
	det      int32 // det
	detScale int   // det_scale
}
