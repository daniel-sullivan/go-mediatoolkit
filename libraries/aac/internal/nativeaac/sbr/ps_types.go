// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// Parametric-stereo (HE-AAC v2) types and constants, ported 1:1 from the
// vendored Fraunhofer FDK-AAC psdec.h. This is the BASELINE MPEG PS decoder:
// it always uses the 20-stereo-band hybrid filter structure and does NOT
// implement IPD/OPD synthesis (the IPD/OPD bits are parsed and discarded). A
// 34-band IID/ICC config is mapped down to 20 bands. EXCLUDED everywhere:
// DRM PS (psdec_drm.cpp), the LD/ELD PS path, and the USAC MPS212 parametric
// stereo — only the classic HE-AAC v2 ER/GA PS is ported.

// PS decoder dimension constants (psdec.h:124-169).
const (
	psScalHeadroom = 2

	psExtensionSizeBits     = 4 // PS_EXTENSION_SIZE_BITS
	psExtensionEscCountBits = 8 // PS_EXTENSION_ESC_COUNT_BITS

	psNoQmfChannels = 64 // NO_QMF_CHANNELS
	psMaxNumCol     = 32 // MAX_NUM_COL

	psNoQmfBandsHybrid20 = 3  // NO_QMF_BANDS_HYBRID20
	psNoSubQmfChannels   = 12 // NO_SUB_QMF_CHANNELS
	psHybridFilterDelay  = 6  // HYBRID_FILTER_DELAY

	psMaxNoPsEnv = 4 + 1 // MAX_NO_PS_ENV (+1 for VAR_BORDER)

	psNoHiResBins  = 34 // NO_HI_RES_BINS
	psNoMidResBins = 20 // NO_MID_RES_BINS
	psNoLowResBins = 10 // NO_LOW_RES_BINS

	psNoHiResIidBins = psNoHiResBins
	psNoHiResIccBins = psNoHiResBins

	psNoMidResIidBins = psNoMidResBins
	psNoMidResIccBins = psNoMidResBins

	psNoLowResIidBins = psNoLowResBins
	psNoLowResIccBins = psNoLowResBins

	psSubQmfGroups = 10 // SUBQMF_GROUPS
	psQmfGroups    = 12 // QMF_GROUPS

	psNoIidGroups = psSubQmfGroups + psQmfGroups // NO_IID_GROUPS

	psNoIidSteps     = 7  // NO_IID_STEPS     (1 .. +7)
	psNoIidStepsFine = 15 // NO_IID_STEPS_FINE (1 .. +15)
	psNoIccSteps     = 8  // NO_ICC_STEPS     (0 .. +7)

	psNoIidLevels     = 2*psNoIidSteps + 1     // -7 .. +7
	psNoIidLevelsFine = 2*psNoIidStepsFine + 1 // -15 .. +15
	psNoIccLevels     = psNoIccSteps           // 0 .. +7
)

// fixpSqrt05 is FIXP_SQRT05 == 1/SQRT2 in Q31 (psdec.h:169).
const fixpSqrt05 = int32(0x5a827980)

// psPayloadType mirrors PS_PAYLOAD_TYPE (psdec.h:196). Only ppt_none / ppt_mpeg
// are reachable in the legacy HE-AAC v2 path (ppt_drm is DRM PS, excluded).
type psPayloadType uint8

const (
	pptNone psPayloadType = 0
	pptMpeg psPayloadType = 1
	pptDrm  psPayloadType = 2
)

// mpegPsBsData mirrors MPEG_PS_BS_DATA (psdec.h:198-245): all MPEG-specific PS
// data parsed from one bitstream slot.
type mpegPsBsData struct {
	BPsHeaderValid uint8 // set if a new header is available from the bitstream

	BEnableIid uint8 // IID parameters present
	BEnableIcc uint8 // ICC parameters present
	BEnableExt uint8 // PS extension layer (IPD/OPD) present

	ModeIid uint8 // IID config (iid_mode)
	ModeIcc uint8 // ICC config (icc_mode)

	FreqResIid uint8 // 0=low 1=mid 2=high freq resolution for IID
	FreqResIcc uint8 // 0=low 1=mid 2=high freq resolution for ICC

	BFineIidQ uint8 // use fine IID quantisation

	BFrameClass uint8 // 0=FIX_BORDERS, 1=VAR_BORDERS

	NoEnv         uint8                   // number of envelopes per frame
	AEnvStartStop [psMaxNoPsEnv + 1]uint8 // parameter border positions
	AbIidDtFlag   [psMaxNoPsEnv]int8      // delta time/freq flag for IID (0=freq)
	AbIccDtFlag   [psMaxNoPsEnv]int8      // delta time/freq flag for ICC (0=freq)
	AaIidIndex    [psMaxNoPsEnv][psNoHiResIidBins]int8
	AaIccIndex    [psMaxNoPsEnv][psNoHiResIccBins]int8
}

// psDecCoefficients mirrors PS_DEC_COEFFICIENTS (psdec.h:171-194): the temporal
// h-matrix coefficients (on reusable scratch) plus the 20<->34 mapped IID/ICC
// index arrays.
type psDecCoefficients struct {
	H11r [psNoIidGroups]int32
	H12r [psNoIidGroups]int32
	H21r [psNoIidGroups]int32
	H22r [psNoIidGroups]int32

	DeltaH11r [psNoIidGroups]int32
	DeltaH12r [psNoIidGroups]int32
	DeltaH21r [psNoIidGroups]int32
	DeltaH22r [psNoIidGroups]int32

	AaIidIndexMapped [psMaxNoPsEnv][psNoHiResIidBins]int8
	AaIccIndexMapped [psMaxNoPsEnv][psNoHiResIccBins]int8
}

// psDecMpegStatic mirrors the specificTo.mpeg static-data struct (psdec.h:272-306):
// the cross-frame PS decoder state (prev-frame indices, hybrid filter states,
// decorrelator instance, prev h-coefficients, and the scratch coefficient ptr).
type psDecMpegStatic struct {
	AIidPrevFrameIndex [psNoHiResIidBins]int8
	AIccPrevFrameIndex [psNoHiResIccBins]int8
	BPrevFrameFineIidQ uint8
	PrevFreqResIid     uint8
	PrevFreqResIcc     uint8
	LastUsb            uint8

	// PHybridAnaStatesLFdmx is the hybrid-analysis LF filter-state memory the
	// hybrid filterbank distributes its ringbuffers from (psdec.h:285).
	PHybridAnaStatesLFdmx [2 * 13 * psNoQmfBandsHybrid20]int32

	HybridAnalysis  fdkAnaHybFilter
	HybridSynthesis [2]fdkSynHybFilter

	ApDecor          decorrDec
	DecorrBufferCplx [2 * (825 + 373)]int32

	H11rPrev [psNoIidGroups]int32
	H12rPrev [psNoIidGroups]int32
	H21rPrev [psNoIidGroups]int32
	H22rPrev [psNoIidGroups]int32

	// pCoef points at the reusable scratch psDecCoefficients during a decode.
	PCoef *psDecCoefficients
}

// psDec mirrors struct PS_DEC (psdec.h:247-309). The bsData / bPsDataAvail
// arrays carry a [(1)+1]==2 slot delay line exactly as the C.
type psDec struct {
	NoSubSamples int8
	NoChannels   int8

	ProcFrameBased int8 // detect frame->slot processing switch

	BPsDataAvail [2]psPayloadType
	PsDecodedPrv uint8

	BsLastSlot  uint8
	BsReadSlot  uint8
	ProcessSlot uint8

	BsData [2]mpegPsBsData

	Mpeg psDecMpegStatic
}
