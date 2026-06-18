// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// Shared type scaffold for the SBR ENCODER driver / bitstream / coding batch:
// ONE coherent Go definition of every struct + enum the code_env, bit_sbr,
// env_bit, ton_corr/invf/nf and sbr_encoder ports thread through each other,
// so no port re-declares them. These are the encode-side counterparts of the
// decode structs already in this package; they come from distinct vendored
// headers (libSBRenc) and carry distinct names where the decoder defines a
// similarly-named concept. fdk-aac SBR is FIXED-POINT: every value is an int32
// FIXP_DBL (Q-format) — EXACT integer parity, no FP/aac_strict discipline.
//
// Scope: HE-AAC v1 only. PS / HE-AAC v2 (T_PARAMETRIC_STEREO), ELD/LD
// (SBR_SYNTAX_LOW_DELAY) and PVC are out of scope and the corresponding fields
// are retained only where a port reads them as constant 0.
package sbr

// SBR syntax-flag bits (sbr_encoder.h). Only the AAC (non-LD) path is in scope;
// SBR_SYNTAX_LOW_DELAY is retained for the ported branches that test it as 0.
const (
	sbrSyntaxLowDelay = 0x04 // SBR_SYNTAX_LOW_DELAY
	sbrSyntaxScalable = 0x02 // SBR_SYNTAX_SCALABLE
	sbrSyntaxDrm      = 0x10 // SBR_SYNTAX_DRM_CRC (unused in v1)
)

// Driver-wide constants (sbr_def.h).
const (
	maxNumChannels        = 2        // MAX_NUM_CHANNELS
	sbrGlobalTonalityVals = 2        // SBR_GLOBAL_TONALITY_VALUES
	sbrExtendedDataMaxCnt = 15 + 255 // SBR_EXTENDED_DATA_MAX_CNT
	siSbrAmpResBits       = 1        // SI_SBR_AMP_RES_BITS
	siSbrInvfModeBits     = 2        // SI_SBR_INVF_MODE_BITS
	maxPayloadSize        = 256      // MAX_PAYLOAD_SIZE
	invfSmoothingLength   = 2        // INVF_SMOOTHING_LENGTH (invf_est.h)
	nfSmoothingLength     = 4        // NF_SMOOTHING_LENGTH (nf_est.h)
	// relaxationShift (=19) and maxNumPatches (=6) are already defined by the
	// foundation (enc_mh_det.go / hfgen_types.go) and reused here.
)

// InvfMode is the 1:1 port of enum INVF_MODE (sbr_def.h:268-275).
type InvfMode int

const (
	InvfOff       InvfMode = 0 // INVF_OFF
	InvfLowLevel  InvfMode = 1 // INVF_LOW_LEVEL
	InvfMidLevel  InvfMode = 2 // INVF_MID_LEVEL
	InvfHighLevel InvfMode = 3 // INVF_HIGH_LEVEL
	InvfSwitched  InvfMode = 4 // INVF_SWITCHED
)

// XposMode is the 1:1 port of enum XPOS_MODE (sbr_def.h:260-267).
type XposMode int

const (
	XposMdct      XposMode = 0 // XPOS_MDCT
	XposMdctCross XposMode = 1 // XPOS_MDCT_CROSS
	XposLc        XposMode = 2 // XPOS_LC
	XposReserved  XposMode = 3 // XPOS_RESERVED
	XposSwitched  XposMode = 4 // XPOS_SWITCHED
)

// SbrStereoMode is the 1:1 port of enum SBR_STEREO_MODE (sbr_encoder.h).
type SbrStereoMode int

const (
	SbrMono      SbrStereoMode = 0 // SBR_MONO
	SbrLeftRight SbrStereoMode = 1 // SBR_LEFT_RIGHT
	SbrCoupling  SbrStereoMode = 2 // SBR_COUPLING
	SbrSwitchLrc SbrStereoMode = 3 // SBR_SWITCH_LRC
)

const (
	hiRes = 1 // HI
	loRes = 0 // LO
)

// ton_corr constants. maxNumPatches (=6), relaxationShift (=19),
// relaxationFract() and lpcOrder (=2) are already defined by the foundation
// (hfgen_types.go / enc_mh_det.go) and reused here.
const (
	scaleNrgvec         = 4  // SCALE_NRGVEC (ton_corr.h:116)
	bandVSize           = 32 // BAND_V_SIZE (ton_corr.cpp:110)
	maxNoOfEstimates    = 4  // MAX_NO_OF_ESTIMATES (sbr_def.h:163)
	noOfEstimatesLC     = 4  // NO_OF_ESTIMATES_LC (sbr_def.h:161)
	numberTimeSlots2048 = 16 // NUMBER_TIME_SLOTS_2048 (fram_gen.h:173)
	numberTimeSlots1920 = 15 // NUMBER_TIME_SLOTS_1920 (fram_gen.h:141)
	frameMiddleSlot2048 = 4  // FRAME_MIDDLE_SLOT_2048 (fram_gen.h:172)
	frameMiddleSlot1920 = 4  // FRAME_MIDDLE_SLOT_1920 (fram_gen.h:140)
)

// numVCombine == NUM_V_COMBINE (ton_corr.cpp:111). The vendored macro reduces to
// 2 on every supported FIXP_DBL (4-byte) config; pinned 1:1.
const numVCombine = 2

// EncPatchParam is the 1:1 port of struct PATCH_PARAM (ton_corr.h:118-131): the
// SBR-encoder patching parameter set. Distinct from the decode-side uint8
// patchParam (hfgen_types.go) — resetPatch does signed INT arithmetic
// (patchDistance can go negative), so every field is a plain int (C INT).
type EncPatchParam struct {
	SourceStartBand int // sourceStartBand
	SourceStopBand  int // sourceStopBand (exclusive)
	GuardStartBand  int // guardStartBand
	TargetStartBand int // targetStartBand
	TargetBandOffs  int // targetBandOffs == targetStartBand - sourceStartBand
	NumBandsInPatch int // numBandsInPatch
}

// SbrTonCorrEst is the 1:1 port of struct SBR_TON_CORR_EST (ton_corr.h:127-203):
// the tonality-correction parameter-extraction state. It owns the quota/sign
// matrices, the nrg vectors, the patch table + index vector, and the three
// already-ported detector sub-states (missing-harmonics, noise-floor,
// inverse-filtering). HE-AAC v1 only — the LD (SBR_SYNTAX_LOW_DELAY) fields are
// retained for the branches that read them as 0.
type SbrTonCorrEst struct {
	SwitchInverseFilt         int
	NoQmfChannels             int
	BufferLength              int
	StepSize                  int
	NumberOfEstimates         int
	NumberOfEstimatesPerFrame int
	FrameStartIndexInvfEst    int
	StartIndexMatrix          int
	Move                      int
	NextSample                int
	LpcLength                 [2]int
	FrameStartIndex           int
	PrevTransientFlag         int
	TransientNextFrame        int
	TransientPosOffset        int

	SignMatrix  [maxNoOfEstimates][]int32 // [MAX_NO_OF_ESTIMATES] rows of 64
	QuotaMatrix [maxNoOfEstimates][]int32 // [MAX_NO_OF_ESTIMATES] rows of 64

	NrgVector     [maxNoOfEstimates]int32
	NrgVectorFreq [64]int32

	IndexVector [64]int8

	PatchParam   [maxNumPatches]EncPatchParam
	Guard        int
	ShiftStartSb int
	NoOfPatches  int

	SbrMissingHarmonicsDetector SbrMissingHarmonicsDetector
	SbrNoiseFloorEstimate       SbrNoiseFloorEstimate
	SbrInvFilt                  SbrInvFiltEst
}

// SbrEnvData is the 1:1 port of struct SBR_ENV_DATA (bit_sbr.h). It carries the
// per-channel envelope/noise coding state the bitstream writer consumes. The
// const ROM-pointer fields hold slices into the enc_rom_huff.go tables (nil
// until InitSbrHuffmanTables runs).
type SbrEnvData struct {
	SbrXposCtrl    int
	FreqResFixfix  [2]FreqRes
	FResTransIsLow uint8

	SbrInvfMode    InvfMode
	SbrInvfModeVec [encMaxNumNoiseValues]InvfMode

	SbrXposMode XposMode

	Ienvelope [encMaxEnvelopes][encMaxFreqCoeffs]int

	CodeBookScfLavBalance int
	CodeBookScfLav        int
	HufftableTimeC        []int32
	HufftableFreqC        []int32
	HufftableTimeL        []uint8
	HufftableFreqL        []uint8

	HufftableLevelTimeC   []int32
	HufftableBalanceTimeC []int32
	HufftableLevelFreqC   []int32
	HufftableBalanceFreqC []int32
	HufftableLevelTimeL   []uint8
	HufftableBalanceTimeL []uint8
	HufftableLevelFreqL   []uint8
	HufftableBalanceFreqL []uint8

	HufftableNoiseTimeL []uint8
	HufftableNoiseTimeC []int32
	HufftableNoiseFreqL []uint8
	HufftableNoiseFreqC []int32

	HufftableNoiseLevelTimeC   []int32
	HufftableNoiseLevelTimeL   []uint8
	HufftableNoiseBalanceTimeC []int32
	HufftableNoiseBalanceTimeL []uint8
	HufftableNoiseLevelFreqC   []int32
	HufftableNoiseLevelFreqL   []uint8
	HufftableNoiseBalanceFreqC []int32
	HufftableNoiseBalanceFreqL []uint8

	HSbrBSGrid *SbrGrid

	NoHarmonics     int
	AddHarmonicFlag int
	AddHarmonic     [encMaxFreqCoeffs]uint8

	SiSbrStartEnvBitsBalance   int
	SiSbrStartEnvBits          int
	SiSbrStartNoiseBitsBalance int
	SiSbrStartNoiseBits        int

	NoOfEnvelopes  int
	NoScfBands     [encMaxEnvelopes]int
	DomainVec      [encMaxEnvelopes]int
	DomainVecNoise [encMaxEnvelopes]int
	SbrNoiseLevels [encMaxFreqCoeffs]int8
	NoOfnoisebands int

	Balance         int
	InitSbrAmpRes   AmpRes
	CurrentAmpResFF AmpRes
	TonHF           [sbrGlobalTonalityVals]int32
	GlobalTonality  int32

	ExtendedData       int
	ExtensionSize      int
	ExtensionID        int
	ExtendedDataBuffer [sbrExtendedDataMaxCnt]uint8

	LdGrid uint8
}

// SbrConfigData is the 1:1 port of struct SBR_CONFIG_DATA (sbr_encoder.h).
type SbrConfigData struct {
	SbrSyntaxFlags uint
	NChannels      int

	NSfb         [2]int
	NumMaster    int
	SampleFreq   int
	FrameSize    int
	XOverFreq    int
	DynXOverFreq int

	NoQmfBands int
	NoQmfSlots int

	FreqBandTable [2][]uint8
	VKMaster      []uint8

	StereoMode    SbrStereoMode
	NoEnvChannels int

	UseWaveCoding       int
	UseParametricCoding int
	XposCtrlSwitch      int
	SwitchTransposers   int
	InitAmpResFF        uint8
	ThresholdAmpResFFm  int32
	ThresholdAmpResFFe  int8
}

// EncSbrHeaderData is the 1:1 port of struct SBR_HEADER_DATA (bit_sbr.h). The
// "Enc" prefix distinguishes it from the decode-side SbrHeaderData already in
// this package (a different struct from a different vendored header).
type EncSbrHeaderData struct {
	SbrAmpRes          AmpRes
	SbrStartFrequency  int
	SbrStopFrequency   int
	SbrXoverBand       int
	SbrNoiseBands      int
	SbrDataExtra       int
	HeaderExtra1       int
	HeaderExtra2       int
	SbrLcStereoMode    int
	SbrLimiterBands    int
	SbrLimiterGains    int
	SbrInterpolFreq    int
	SbrSmoothingLength int
	AlterScale         int
	FreqScale          int

	Coupling     int
	PrevCoupling int
}

// SbrBitstreamData is the 1:1 port of struct SBR_BITSTREAM_DATA (bit_sbr.h).
type SbrBitstreamData struct {
	TotalBits           int
	PayloadBits         int
	FillBits            int
	HeaderActive        int
	HeaderActiveDelay   int
	NrSendHeaderData    int
	CountSendHeaderData int
	RightBorderFIX      int
}

// SbrEnvTempData is the 1:1 port of struct SBR_ENV_TEMP_DATA (env_est.h).
type SbrEnvTempData struct {
	FrameInfo          *SbrFrameInfo
	NoiseFloor         [encMaxNumNoiseValues]int32
	SfbNrgCoupling     [encMaxNumEnvelopeVals]int8
	SfbNrg             [encMaxNumEnvelopeVals]int8
	NoiseLevelCoupling [encMaxNumNoiseValues]int8
	NoiseLevel         [encMaxNumNoiseValues]int8
	TransientInfo      [3]uint8
	NEnvelopes         uint8
}

// SbrFrameTempData is the 1:1 port of struct SBR_FRAME_TEMP_DATA (env_est.h).
type SbrFrameTempData struct {
	Res           [encMaxNumNoiseValues]FreqRes
	MaxQuantError int
}
