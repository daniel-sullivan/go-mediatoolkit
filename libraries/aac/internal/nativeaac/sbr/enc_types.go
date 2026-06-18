// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// This file is the shared type + constant scaffold for the SBR ENCODER analysis
// batch (sbr-enc-analysis): the envelope estimator (env_est.cpp), frame grid
// generator (fram_gen.cpp), transient detector (tran_det.cpp) and
// missing-harmonics detector (mh_det.cpp). It holds ONE coherent definition of
// every struct, enum and constant those four 1:1 ports share, so no port
// re-declares them. fdk-aac SBR is FIXED-POINT: every value is an int32 FIXP_DBL
// (Q-format) — EXACT integer parity, no FP/aac_strict discipline.
//
// Scope: HE-AAC v1 only. PS / HE-AAC v2, ELD/LD (SBR_SYNTAX_LOW_DELAY) grid
// variants, DRC and PVC are out of scope for this batch and the corresponding
// branches are excluded (and noted) in the ports.
package sbr

// SBR-ENCODER (libSBRenc/src/sbr_def.h) constants. These deliberately carry an
// `enc` prefix: the libSBRenc limits differ in value from the libSBRdec limits
// the decoder batch already defines in this package (e.g. MAX_FREQ_COEFFS is 48
// here vs 56 in the decoder, MAX_ENVELOPES 5 vs 8) — they are distinct concepts
// from distinct vendored headers, so they get distinct Go names to keep ONE
// coherent definition each. `dfractBits` is genuinely shared (==32) and reused
// from the QMF files, not redeclared.
const (
	encMaxFreqCoeffs      = 48 // MAX_FREQ_COEFFS  (libSBRenc/src/sbr_def.h:156)
	encMaxEnvelopes       = 5  // MAX_ENVELOPES    (sbr_def.h:155)
	encMaxNoiseEnvelopes  = 2  // MAX_NOISE_ENVELOPES (sbr_def.h:150)
	encMaxNumNoiseCoeffs  = 5  // MAX_NUM_NOISE_COEFFS (sbr_def.h:151)
	encMaxNumNoiseValues  = encMaxNumNoiseCoeffs * encMaxNoiseEnvelopes
	encMaxNumEnvelopeVals = encMaxEnvelopes * encMaxFreqCoeffs // MAX_NUM_ENVELOPE_VALUES
	encMaxNoOfEstimates   = 4                                  // MAX_NO_OF_ESTIMATES (sbr_def.h:163)
	encQmfScaleOffset     = 7                                  // QMF_SCALE_OFFSET   (sbr_def.h:248)
	encNoiseFloorOffset   = 6                                  // NOISE_FLOOR_OFFSET (sbr_def.h:165)
	encLdDataShift        = 6                                  // LD_DATA_SHIFT (fixpoint_math.h)
	encMaxShiftDBL        = 30                                 // MAX_SHIFT_DBL (scale.h)
	encMaxvalDBL          = int32(0x7FFFFFFF)                  // MAXVAL_DBL
	numberTimeSlots2304   = 18                                 // NUMBER_TIME_SLOTS_2304 (fram_gen.h:203)
)

// fram_gen.h frame-grid constants.
const (
	encMaxEnvelopesVarvar       = encMaxEnvelopes // MAX_ENVELOPES_VARVAR (fram_gen.h:113)
	encMaxEnvelopesFixvarVarfix = 4               // MAX_ENVELOPES_FIXVAR_VARFIX (fram_gen.h:115)
	encMaxNumRel                = 3               // MAX_NUM_REL (fram_gen.h:117)
	dcConst                     = 4711            // DC    (fram_gen.h:136)
	emptyConst                  = -99             // EMPTY (fram_gen.h:137)
	ldPretranOff                = 3               // LD_PRETRAN_OFF (fram_gen.h:143)
)

// tran_det.h transient-detector constants.
const (
	tranDetLookahead    = 2     // TRAN_DET_LOOKAHEAD (tran_det.h:132)
	tranDetStartFreq    = 4500  // TRAN_DET_START_FREQ
	tranDetStopFreq     = 13500 // TRAN_DET_STOP_FREQ
	tranDetMinQmfBands  = 4     // TRAN_DET_MIN_QMFBANDS
	tranDetThrshldScale = 3     // TRAN_DET_THRSHLD_SCALE (tran_det.h:141)
	energyScalingSize   = 32    // ENERGY_SCALING_SIZE (tran_det.cpp:788)
)

// FreqRes is FREQ_RES (sbr_encoder.h:146).
type FreqRes int

const (
	FreqResLow  FreqRes = 0 // FREQ_RES_LOW
	FreqResHigh FreqRes = 1 // FREQ_RES_HIGH
)

// FrameClass is FRAME_CLASS (fram_gen.h:121-133).
type FrameClass int

const (
	Fixfix     FrameClass = iota // FIXFIX
	Fixvar                       // FIXVAR
	Varfix                       // VARFIX
	Varvar                       // VARVAR
	Fixfixonly                   // FIXFIXonly
)

// SbrFrameInfo is the 1:1 port of struct SBR_FRAME_INFO (fram_gen.h:251-260):
// the time/frequency grid description the frame generator emits and the
// envelope estimator + MH detector consume.
type SbrFrameInfo struct {
	NEnvelopes      int                           // nEnvelopes
	Borders         [encMaxEnvelopes + 1]int      // borders (SBR timeslots)
	FreqRes         [encMaxEnvelopes]FreqRes      // freqRes
	ShortEnv        int                           // shortEnv
	NNoiseEnvelopes int                           // nNoiseEnvelopes
	BordersNoise    [encMaxNoiseEnvelopes + 1]int // bordersNoise
}

// SbrGrid is the 1:1 port of struct SBR_GRID (fram_gen.h:212-244): the
// sbr_grid() signals in clear text that the frame generator fills.
type SbrGrid struct {
	BufferFrameStart int
	NumberTimeSlots  int

	FrameClass FrameClass
	BsNumEnv   int
	BsAbsBord  int
	N          int
	P          int
	BsRelBord  [encMaxNumRel]int
	VF         [encMaxEnvelopesFixvarVarfix]int

	BsAbsBord0 int
	BsAbsBord1 int
	BsNumRel0  int
	BsNumRel1  int
	BsRelBord0 [encMaxNumRel]int
	BsRelBord1 [encMaxNumRel]int
	VFLR       [encMaxEnvelopesVarvar]int
}

// SbrEnvelopeFrame is the 1:1 port of struct SBR_ENVELOPE_FRAME
// (fram_gen.h:274-328): the frame generator's main state struct (tuning
// parameters, internal border/freq vectors, and the emitted SbrGrid/SbrFrameInfo).
type SbrEnvelopeFrame struct {
	FrameMiddleSlot int

	StaticFraming  int
	NumEnvStatic   int
	FreqResFixfix  [2]FreqRes
	FResTransIsLow uint8

	VTuningSegm []int
	VTuningFreq []int
	Dmin        int
	Dmax        int
	AllowSpread int

	FrameClassOld FrameClass
	SpreadFlag    int

	VBord       [2*encMaxEnvelopesVarvar + 1]int
	LengthVBord int
	VFreq       [2*encMaxEnvelopesVarvar + 1]int
	LengthVFreq int

	VBordFollow       [encMaxEnvelopesVarvar]int
	LengthVBordFollow int
	ITranFollow       int
	IFillFollow       int
	VFreqFollow       [encMaxEnvelopesVarvar]int
	LengthVFreqFollow int

	SbrGrid      SbrGrid
	SbrFrameInfo SbrFrameInfo
}

// SbrTransientDetector is the 1:1 port of struct SBR_TRANSIENT_DETECTOR
// (tran_det.h:113-128).
type SbrTransientDetector struct {
	Transients         [32 + 32/2]int32 // transients[32 + (32/2)]
	Thresholds         [64]int32        // thresholds[64]
	TranThr            int32            // tran_thr
	SplitThrM          int32            // split_thr_m
	SplitThrE          int              // split_thr_e
	PrevLowBandEnergy  int32            // prevLowBandEnergy
	PrevHighBandEnergy int32            // prevHighBandEnergy
	TranFc             int              // tran_fc
	NoCols             int              // no_cols
	NoRows             int              // no_rows
	Mode               int              // mode

	FrameShift int // frameShift
	TranOff    int // tran_off
}

// FastTranDetector is the 1:1 port of struct FAST_TRAN_DETECTOR
// (tran_det.h:143-161).
type FastTranDetector struct {
	TransientCandidates [32 + tranDetLookahead]int // transientCandidates
	NTimeSlots          int                        // nTimeSlots
	Lookahead           int                        // lookahead
	StartBand           int                        // startBand
	StopBand            int                        // stopBand

	DBfM [64]int32 // dBf_m
	DBfE [64]int   // dBf_e

	EnergyTimeSlots      [32 + tranDetLookahead]int32 // energy_timeSlots
	EnergyTimeSlotsScale [32 + tranDetLookahead]int   // energy_timeSlots_scale

	DeltaEnergy      [32 + tranDetLookahead]int32 // delta_energy
	DeltaEnergyScale [32 + tranDetLookahead]int   // delta_energy_scale

	LowpassEnergy      [32 + tranDetLookahead]int32 // lowpass_energy
	LowpassEnergyScale [32 + tranDetLookahead]int   // lowpass_energy_scale
}

// ThresHolds is the 1:1 port of struct THRES_HOLDS (mh_det.h:114-136): the
// missing-harmonics-detector tonality/flatness thresholds.
type ThresHolds struct {
	ThresHoldDiff       int32 // thresHoldDiff
	ThresHoldDiffGuide  int32 // thresHoldDiffGuide
	ThresHoldTone       int32 // thresHoldTone
	InvThresHoldTone    int32 // invThresHoldTone
	ThresHoldToneGuide  int32 // thresHoldToneGuide
	SfmThresSbr         int32 // sfmThresSbr
	SfmThresOrig        int32 // sfmThresOrig
	DecayGuideOrig      int32 // decayGuideOrig
	DecayGuideDiff      int32 // decayGuideDiff
	DerivThresMaxLD64   int32 // derivThresMaxLD64
	DerivThresBelowLD64 int32 // derivThresBelowLD64
	DerivThresAboveLD64 int32 // derivThresAboveLD64
}

// DetectorParametersMH is the 1:1 port of struct DETECTOR_PARAMETERS_MH
// (mh_det.h:138-145).
type DetectorParametersMH struct {
	DeltaTime  int        // deltaTime
	ThresHolds ThresHolds // thresHolds
	MaxComp    int        // maxComp
}

// GuideVectors is the 1:1 port of struct GUIDE_VECTORS (mh_det.h:147-151).
type GuideVectors struct {
	GuideVectorDiff     []int32 // guideVectorDiff
	GuideVectorOrig     []int32 // guideVectorOrig
	GuideVectorDetected []uint8 // guideVectorDetected
}

// SbrMissingHarmonicsDetector is the 1:1 port of struct
// SBR_MISSING_HARMONICS_DETECTOR (mh_det.h:153-177).
type SbrMissingHarmonicsDetector struct {
	QmfNoChannels          int // qmfNoChannels
	NSfb                   int // nSfb
	SampleFreq             int // sampleFreq
	PreviousTransientFlag  int // previousTransientFlag
	PreviousTransientFrame int // previousTransientFrame
	PreviousTransientPos   int // previousTransientPos

	NoVecPerFrame      int // noVecPerFrame
	TransientPosOffset int // transientPosOffset

	Move          int // move
	TotNoEst      int // totNoEst
	NoEstPerFrame int // noEstPerFrame
	TimeSlots     int // timeSlots

	GuideScfb                []uint8                      // guideScfb
	PrevEnvelopeCompensation []uint8                      // prevEnvelopeCompensation
	DetectionVectors         [encMaxNoOfEstimates][]uint8 // detectionVectors
	TonalityDiff             [encMaxNoOfEstimates / 2][encMaxFreqCoeffs]int32
	SfmOrig                  [encMaxNoOfEstimates / 2][encMaxFreqCoeffs]int32
	SfmSbr                   [encMaxNoOfEstimates / 2][encMaxFreqCoeffs]int32
	MhParams                 *DetectorParametersMH             // mhParams
	GuideVectors             [encMaxNoOfEstimates]GuideVectors // guideVectors
}

// SbrExtractEnvelope is the 1:1 port of struct SBR_EXTRACT_ENVELOPE
// (env_est.h:119-141): the QMF-derived feature buffers the envelope estimator
// reads and writes.
type SbrExtractEnvelope struct {
	RBuffer [32][]int32 // rBuffer
	IBuffer [32][]int32 // iBuffer

	PYBuffer []int32 // p_YBuffer

	YBuffer      [32][]int32 // YBuffer
	YBufferScale [2]int      // YBufferScale

	EnvelopeCompensation [encMaxFreqCoeffs]uint8 // envelopeCompensation
	PreTransientInfo     [2]uint8                // pre_transient_info

	YBufferWriteOffset int // YBufferWriteOffset
	YBufferSzShift     int // YBufferSzShift
	RBufferReadOffset  int // rBufferReadOffset

	NoCols     int // no_cols
	NoRows     int // no_rows
	StartIndex int // start_index

	TimeSlots int // time_slots
	TimeStep  int // time_step
}
