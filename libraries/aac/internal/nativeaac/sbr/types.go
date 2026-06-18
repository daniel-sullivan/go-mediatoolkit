// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package sbr

// This file holds the shared SBR decoder data structures, ported 1:1 from the
// vendored libSBRdec headers (env_extr.h, lpp_tran.h). They are the coherent
// single definition reused by the bitstream-parse, symbol-decode,
// frequency-band-mapping and envelope-gain-calculation files in this package.
// fdk-aac SBR is fixed-point: every numeric value is an int (UCHAR/SCHAR/INT) or
// an int16/int32 Q-format mantissa, so the structs mirror the C field types
// exactly (UCHAR -> uint8, SCHAR -> int8, INT -> int, FIXP_SGL -> int16,
// FIXP_DBL -> int32, ULONG -> uint32).
//
// HE-AAC v1 (STD) scope: the PS (parametric stereo), USAC-HBE/PVC and ELD-only
// fields are kept in the structs faithfully (so the layout matches the C and the
// later batches that fill them have a home), but this batch only reads/writes
// the AAC HE-AAC v1 subset.

// invfMode is the INVF_MODE enum (lpp_tran.h:164-170): strength of inverse
// filtering in the transposer.
type invfMode int

const (
	invfOff      invfMode = 0 // INVF_OFF
	invfLowLevel invfMode = 1 // INVF_LOW_LEVEL
	invfMidLevel invfMode = 2 // INVF_MID_LEVEL
	invfHighLvl  invfMode = 3 // INVF_HIGH_LEVEL
	invfSwitched invfMode = 4 // INVF_SWITCHED
)

// FrameInfo is FRAME_INFO (env_extr.h:299-313): the time grid for one SBR frame.
type FrameInfo struct {
	FrameClass    uint8                        // frameClass
	NEnvelopes    uint8                        // nEnvelopes
	Borders       [maxEnvelopes + 1]uint8      // borders (SBR-timeslots)
	FreqRes       [maxEnvelopes]uint8          // freqRes (0=low,1=high) per envelope
	TranEnv       int8                         // tranEnv (-1 if none)
	NNoiseEnv     uint8                        // nNoiseEnvelopes
	BordersNoise  [maxNoiseEnvelopes + 1]uint8 // bordersNoise
	PvcBorders    [maxPvcEnvelopes + 1]uint8   // pvcBorders
	NoisePosition uint8                        // noisePosition
	VarLength     uint8                        // varLength
}

// FreqBandData is FREQ_BAND_DATA (env_extr.h:177-200): the SBR<->QMF band mapping
// for one channel element.
type FreqBandData struct {
	NSfb           [2]uint8                  // nSfb[low,high freq-res]
	NNfb           uint8                     // nNfb (noise bands read from bitstream)
	NumMaster      uint8                     // numMaster (bands in v_k_master)
	LowSubband     uint8                     // lowSubband (QMF band where SBR starts)
	HighSubband    uint8                     // highSubband (QMF band where SBR ends)
	OvHighSubband  uint8                     // ov_highSubband
	LimiterBandTab [maxNumLimiters + 1]uint8 // limiterBandTable
	NoLimiterBands uint8                     // noLimiterBands
	NInvfBands     uint8                     // nInvfBands

	// freqBandTable[2] in C points at the two arrays below (Lo, Hi). The Go port
	// reads FreqBandTableLo/Hi directly instead of carrying the alias pointers.
	FreqBandTableLo    [maxFreqCoeffs/2 + 1]uint8 // freqBandTableLo
	FreqBandTableHi    [maxFreqCoeffs + 1]uint8   // freqBandTableHi
	FreqBandTableNoise [maxNoiseCoeffs + 1]uint8  // freqBandTableNoise
	VKMaster           [maxFreqCoeffs + 1]uint8   // v_k_master
}

// FreqBandTable returns the C freqBandTable[2] alias view (index 0 == Lo, 1 ==
// Hi) as slices over the in-struct arrays.
func (f *FreqBandData) FreqBandTable(i int) []uint8 {
	if i == 0 {
		return f.FreqBandTableLo[:]
	}
	return f.FreqBandTableHi[:]
}

// SbrHeaderDataBSInfo is SBR_HEADER_DATA_BS_INFO (env_extr.h:242-250).
type SbrHeaderDataBSInfo struct {
	AmpResolution    uint8 // ampResolution (0:1.5dB, 1:3dB)
	XoverBand        uint8 // xover_band
	SbrPreprocessing uint8 // sbr_preprocessing (prewhitening)
	PvcMode          uint8 // pvc_mode
}

// SbrHeaderDataBS is SBR_HEADER_DATA_BS (env_extr.h:252-267).
type SbrHeaderDataBS struct {
	StartFreq  uint8 // startFreq
	StopFreq   uint8 // stopFreq
	FreqScale  uint8 // freqScale (0:linear, 1-3:log)
	AlterScale uint8 // alterScale
	NoiseBands uint8 // noise_bands (per octave, from bitstream)

	LimiterBands    uint8 // limiterBands
	LimiterGains    uint8 // limiterGains
	InterpolFreq    uint8 // interpolFreq
	SmoothingLength uint8 // smoothingLength
}

// SbrHeaderData is SBR_HEADER_DATA (env_extr.h:269-295).
type SbrHeaderData struct {
	SyncState  SbrSyncState // syncState
	Status     uint8        // status
	FrameError uint8        // frameErrorFlag

	NumberTimeSlots       uint8 // numberTimeSlots (AAC: 16,15)
	NumberOfAnalysisBands uint8 // numberOfAnalysisBands
	TimeStep              uint8 // timeStep
	SbrProcSmplRate       uint  // sbrProcSmplRate

	BsData SbrHeaderDataBS     // bs_data (current header)
	BsDflt SbrHeaderDataBS     // bs_dflt (default header)
	BsInfo SbrHeaderDataBSInfo // bs_info

	FreqBandData FreqBandData // freqBandData
	PvcIDPrev    uint8        // pvcIDprev
}

// SbrSyncState is SBR_SYNC_STATE (env_extr.h:168-173).
type SbrSyncState int

// SbrPrevFrameData is SBR_PREV_FRAME_DATA (env_extr.h:315-329).
type SbrPrevFrameData struct {
	SfbNrgPrev     [maxFreqCoeffs]int16   // sfb_nrg_prev (FIXP_SGL)
	PrevNoiseLevel [maxNoiseCoeffs]int16  // prevNoiseLevel (FIXP_SGL)
	Coupling       int                    // coupling (COUPLING_MODE)
	SbrInvfMode    [maxInvfBands]invfMode // sbr_invf_mode
	AmpRes         uint8                  // ampRes
	StopPos        uint8                  // stopPos
	FrameError     uint8                  // frameErrorFlag
	PrevSbrPitch   uint8                  // prevSbrPitchInBins
	PrevFrameInfo  FrameInfo              // prevFrameInfo
}

// SbrFrameData is SBR_FRAME_DATA (env_extr.h:333-366).
type SbrFrameData struct {
	NScaleFactors int // nScaleFactors

	FrameInfo      FrameInfo                // frameInfo
	DomainVec      [maxEnvelopes]uint8      // domain_vec (0:freq, 1:time)
	DomainVecNoise [maxNoiseEnvelopes]uint8 // domain_vec_noise

	SbrInvfMode            [maxInvfBands]invfMode // sbr_invf_mode
	Coupling               int                    // coupling (COUPLING_MODE)
	AmpResolutionCurrFrame int                    // ampResolutionCurrentFrame

	AddHarmonics [addHarmonicsFlagsSz]uint32 // addHarmonics (aligned to MSB)

	IEnvelope          [maxNumEnvelopeValues]int16 // iEnvelope (FIXP_SGL)
	SbrNoiseFloorLevel [maxNumNoiseValues]int16    // sbrNoiseFloorLevel (FIXP_SGL)
	ITESactive         uint8                       // iTESactive
	InterTempShapeMode [maxEnvelopes]uint8         // interTempShapeMode
	PvcID              [pvcNTimeslot]uint8         // pvcID
	Ns                 uint8                       // ns
	SinusoidalPosition uint8                       // sinusoidal_position

	SbrPatchingMode     uint8 // sbrPatchingMode
	SbrOversamplingFlag uint8 // sbrOversamplingFlag
	SbrPitchInBins      uint8 // sbrPitchInBins
}
