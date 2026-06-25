// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// 1:1 port of the SBR-encoder TOP-LEVEL ORCHESTRATION of
// libSBRenc/src/sbr_encoder.cpp: the per-element/per-channel state aggregates
// and the open/init/per-frame driver that wires the already-ported leaf kernels
// (QMF analysis, env_est driver, env_bit assembler, freq_sca band tables, the
// 2:1 time-domain downsampler) into a complete HE-AAC v1 SBR encoder:
//
//   - struct SBR_CHANNEL / SBR_ELEMENT / SBR_ENCODER       (sbr.h:129-192)
//   - updateFreqBandTable               (sbr_encoder.cpp:812-848)
//   - resetEnvChannel                   (sbr_encoder.cpp:859-889)
//   - FDKsbrEnc_SbrGetXOverFreq         (sbr_encoder.cpp:899-930)
//   - FDKsbrEnc_EnvEncodeFrame          (sbr_encoder.cpp:941-1193)
//   - FDKsbrEnc_Downsample              (sbr_encoder.cpp:1204-1275)
//   - createEnvChannel / initEnvChannel (sbr_encoder.cpp:1287-1492)
//   - FDKsbrEnc_CreateExtractSbrEnvelope/InitExtractSbrEnvelope (env_est.cpp:1857-1970)
//   - FDKsbrEnc_bsBufInit               (sbr_encoder.cpp:1637-1647)
//   - FDKsbrEnc_EnvInit                 (sbr_encoder.cpp:1658-1852)
//   - sbrEncoder_Init_delay             (sbr_encoder.cpp:1950-2056)
//   - FDKsbrEnc_DelayCompensation       (sbr_encoder.cpp:1867-1883)
//   - sbrEncoder_Init                   (sbr_encoder.cpp:2065-2381)
//   - sbrEncoder_EncodeFrame            (sbr_encoder.cpp:2383-2406)
//   - sbrEncoder_UpdateBuffers          (sbr_encoder.cpp:2408-2447)
//
// HE-AAC v1 only (downSampleFactor == 2, time-domain downsampling, AOT_SBR
// core). Excluded as taken-false / not referenced: PARAMETRIC STEREO (usePs /
// fParametricStereo / the QMF-downsampler PS path), USAC/HBE, ELD/LD
// (SBR_SYNTAX_LOW_DELAY, is212, AC_LD_MPS), DRM/PVC, SBR-CRC, dynamic bandwidth
// (dynBwEnabled defaults off), and the multi-element (8-element) loops are run
// for the single SCE/CPE element a GA HE-AAC v1 stream carries. fdk-aac SBR is
// FIXED-POINT — byte-identical bitstream.
package sbr

import "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

// Driver constants.
const (
	maxDelayFrames           = 2   // MAX_DELAY_FRAMES (sbr.h:124)
	qmfFilterPrototypeSize   = 640 // QMF_FILTER_PROTOTYPE_SIZE
	numberTimeSlots2048Slots = 16  // NUMBER_TIME_SLOTS_2048 (fram_gen.h:173)
)

// SbrEncChannel is the 1:1 port of struct SBR_CHANNEL (sbr.h:129-135): the
// per-channel envelope state plus its time-domain downsampler. (Named with an
// "Enc" prefix to avoid colliding with the decode-side SbrChannel in this
// package, which is a distinct struct from a distinct vendored header.)
type SbrEncChannel struct {
	HEnvChannel EnvChannel
	DownSampler Downsampler
}

// SbrElement is the 1:1 port of struct SBR_ELEMENT (sbr.h:138-152): one SCE/CPE
// SBR element — its 1-2 channels, the per-channel QMF analysis banks (with
// persistent filter states), the config/header/bitstream data, the common
// payload buffer and the per-frame payload delay line.
type SbrElement struct {
	SbrChannel    [2]*SbrEncChannel
	HQmfAnalysis  [2]*FilterBank
	qmfStates     [2][]int32 // persistent analysis polyphase delay lines
	SbrConfigData SbrConfigData
	SbrHeaderData EncSbrHeaderData
	SbrBitstrData SbrBitstreamData
	CmonData      EncCommonData
	ElInfo        SbrElementInfo

	// payloadDelayLine[1+MAX_DELAY_FRAMES][MAX_PAYLOAD_SIZE] bytes + bit sizes.
	PayloadDelayLine     [1 + maxDelayFrames][]byte
	PayloadDelayLineSize [1 + maxDelayFrames]int
}

// SbrElementInfo is the SBR-relevant slice of struct SBR_ELEMENT_INFO
// (sbr_encoder.h:280-289). HE-AAC v1: fParametricStereo/fDualMono stay 0 except
// where a CPE explicitly requests dual-mono.
type SbrElementInfo struct {
	ElType            int // MP4_ELEMENT_ID (ID_SCE==0 / ID_CPE==1 in the SBR id-space)
	BitRate           int
	InstanceTag       int
	FParametricStereo uint8
	FDualMono         uint8
	NChannelsInEl     uint8
	ChannelIndex      [2]uint8
}

// SbrEncoder is the SBR-relevant slice of struct SBR_ENCODER (sbr.h:154-192):
// the single-element HE-AAC v1 SBR encoder. The PS / multi-element / LFE members
// are out of scope (lfeChIdx stays -1, noElements == 1).
type SbrEncoder struct {
	SbrElement [1]*SbrElement

	LfeChIdx           int
	NoElements         int
	NChannels          int
	FrameSize          int
	BufferOffset       int
	DownsampledOffset  int
	DownmixSize        int
	DownSampleFactor   int
	DownsamplingMethod int // SBRENC_DS_TYPE
	NBitstrDelay       int
	SbrDecDelay        int
	EstimateBitrate    int
	InputDataDelay     int

	// HE-AAC v2 parametric stereo (sbr.h hEnvEncoder->hParametricStereo +
	// qmfSynthesisPS). Both are nil/zero for HE-AAC v1.
	HParametricStereo *ParametricStereo
	QmfSynthesisPS    *FilterBank
	qmfSynthesisPSMem []int32 // qmfSynthesisPS.FilterStates backing
}

// SBRENC_DS_TYPE values (sbr.h:127).
const (
	sbrencDSNone = 0 // SBRENC_DS_NONE
	sbrencDSTime = 1 // SBRENC_DS_TIME
	sbrencDSQMF  = 2 // SBRENC_DS_QMF
)

// MP4_ELEMENT_ID in the SBR element-id space.
const (
	idSCE = 0 // ID_SCE
	idCPE = 1 // ID_CPE
)

// CreateExtractSbrEnvelope is the 1:1 port of FDKsbrEnc_CreateExtractSbrEnvelope
// (env_est.cpp:1857-1893). Allocates the YBuffer (32 rows of 64), the rBuffer
// and iBuffer (each 32 rows of 64). The C split between persistent and dynamic
// RAM is irrelevant in Go (one allocation); the row pointers are wired the same.
func CreateExtractSbrEnvelope(hSbrCut *SbrExtractEnvelope) {
	*hSbrCut = SbrExtractEnvelope{}

	hSbrCut.PYBuffer = make([]int32, 32*64)
	for i := 0; i < 32; i++ {
		hSbrCut.YBuffer[i] = hSbrCut.PYBuffer[i*64 : i*64+64]
	}

	rBuf := make([]int32, 32*64)
	iBuf := make([]int32, 32*64)
	for i := 0; i < 32; i++ {
		hSbrCut.RBuffer[i] = rBuf[i*64 : i*64+64]
		hSbrCut.IBuffer[i] = iBuf[i*64 : i*64+64]
	}
}

// InitExtractSbrEnvelope is the 1:1 port of FDKsbrEnc_InitExtractSbrEnvelope
// (env_est.cpp:1903-1970). HE-AAC v1: the SBR_SYNTAX_LOW_DELAY branch is
// taken-false. statesInitFlag clears the buffers on a fresh init.
func InitExtractSbrEnvelope(hSbrCut *SbrExtractEnvelope, noCols, noRows, startIndex,
	timeSlots, timeStep, tranOff int, statesInitFlag bool, sbrSyntaxFlags uint) {

	// !(SBR_SYNTAX_LOW_DELAY).
	hSbrCut.YBufferWriteOffset = tranOff * timeStep
	hSbrCut.RBufferReadOffset = 0

	yBufferLength := hSbrCut.YBufferWriteOffset + noCols
	rBufferLength := noCols

	hSbrCut.PreTransientInfo[0] = 0
	hSbrCut.PreTransientInfo[1] = 0

	hSbrCut.NoCols = noCols
	hSbrCut.NoRows = noRows
	hSbrCut.StartIndex = startIndex

	hSbrCut.TimeSlots = timeSlots
	hSbrCut.TimeStep = timeStep

	if timeStep >= 2 {
		hSbrCut.YBufferSzShift = 1
	} else {
		hSbrCut.YBufferSzShift = 0
	}

	yBufferLength >>= hSbrCut.YBufferSzShift
	hSbrCut.YBufferWriteOffset >>= hSbrCut.YBufferSzShift

	if statesInitFlag {
		for i := 0; i < yBufferLength; i++ {
			for k := 0; k < 64; k++ {
				hSbrCut.YBuffer[i][k] = 0
			}
		}
	}

	for i := 0; i < rBufferLength; i++ {
		for k := 0; k < 64; k++ {
			hSbrCut.RBuffer[i][k] = 0
			hSbrCut.IBuffer[i][k] = 0
		}
	}

	for i := range hSbrCut.EnvelopeCompensation {
		hSbrCut.EnvelopeCompensation[i] = 0
	}

	if statesInitFlag {
		hSbrCut.YBufferScale[0] = dfractBits - 1
		hSbrCut.YBufferScale[1] = dfractBits - 1
	}
}

// createEnvChannel is the 1:1 port of createEnvChannel (sbr_encoder.cpp:1287-1301).
func createEnvChannel(hEnv *EnvChannel, channel int) {
	*hEnv = EnvChannel{}
	hEnv.TonCorr.CreateTonCorrParamExtr(channel)
	CreateExtractSbrEnvelope(&hEnv.SbrExtractEnvelope)
}

// initEnvChannel is the 1:1 port of initEnvChannel (sbr_encoder.cpp:1312-1492).
// HE-AAC v1: the SBR_SYNTAX_LOW_DELAY (fast transient detector, LD frame shift)
// and XPOS_SWITCHED branches are excluded/taken-false. Returns 1 on error.
func initEnvChannel(sbrConfigData *SbrConfigData, sbrHeaderData *EncSbrHeaderData,
	hEnv *EnvChannel, params *SbrConfiguration, statesInitFlag bool) int {

	e := 1 << params.E

	hEnv.EncEnvData.FreqResFixfix[0] = params.FreqResFixfix[0]
	hEnv.EncEnvData.FreqResFixfix[1] = params.FreqResFixfix[1]
	hEnv.EncEnvData.FResTransIsLow = params.FResTransIsLow

	hEnv.FLevelProtect = 0

	// ldGrid = 0 (non-LD).
	hEnv.EncEnvData.LdGrid = 0

	hEnv.EncEnvData.SbrXposMode = XposMode(params.SbrXposMode)
	if hEnv.EncEnvData.SbrXposMode == XposSwitched {
		sbrConfigData.SwitchTransposers = 1
		hEnv.EncEnvData.SbrXposMode = XposMdct
	} else {
		sbrConfigData.SwitchTransposers = 0
	}

	hEnv.EncEnvData.SbrXposCtrl = params.SbrXposCtrl

	if params.ParametricCoding != 0 {
		hEnv.EncEnvData.ExtendedData = 1
	} else {
		hEnv.EncEnvData.ExtendedData = 0
	}
	hEnv.EncEnvData.ExtensionSize = 0

	startIndex := qmfFilterPrototypeSize - sbrConfigData.NoQmfBands

	var timeSlots int
	switch params.SbrFrameSize {
	case 2304:
		timeSlots = 18
	case 2048, 1024, 512:
		timeSlots = 16
	case 1920, 960, 480:
		timeSlots = 15
	case 1152:
		timeSlots = 9
	default:
		return 1
	}

	timeStep := sbrConfigData.NoQmfSlots / timeSlots

	if hEnv.TonCorr.InitTonCorrParamExtr(params.SbrFrameSize, sbrConfigData, timeSlots,
		params.SbrXposCtrl, params.AnaMaxLevel, sbrHeaderData.SbrNoiseBands,
		params.NoiseFloorOffset, params.UseSpeechConfig) != 0 {
		return 1
	}

	hEnv.EncEnvData.NoOfnoisebands = hEnv.TonCorr.SbrNoiseFloorEstimate.NoNoiseBands
	noiseBands := [2]int{hEnv.EncEnvData.NoOfnoisebands, hEnv.EncEnvData.NoOfnoisebands}

	hEnv.EncEnvData.SbrInvfMode = InvfMode(params.SbrInvfMode)
	if hEnv.EncEnvData.SbrInvfMode == InvfSwitched {
		hEnv.EncEnvData.SbrInvfMode = InvfMidLevel
		hEnv.TonCorr.SwitchInverseFilt = 1
	} else {
		hEnv.TonCorr.SwitchInverseFilt = 0
	}

	tranFc := params.TranFc
	if tranFc == 0 {
		tranFc = nativeaac.FMinI(5000, GetSbrStartFreqRAW(sbrHeaderData.SbrStartFrequency, params.SampleFreq))
	}
	tranFc = (tranFc*4*sbrConfigData.NoQmfBands/sbrConfigData.SampleFreq + 1) >> 1

	// !(SBR_SYNTAX_LOW_DELAY): frameShift = 0, tran_off per time-slot count.
	frameShift := 0
	var tranOff int
	switch timeSlots {
	case numberTimeSlots2048:
		tranOff = 8 + frameMiddleSlot2048*timeStep
	case numberTimeSlots1920:
		tranOff = 7 + frameMiddleSlot1920*timeStep
	default:
		return 1
	}

	InitExtractSbrEnvelope(&hEnv.SbrExtractEnvelope, sbrConfigData.NoQmfSlots,
		sbrConfigData.NoQmfBands, startIndex, timeSlots, timeStep, tranOff,
		statesInitFlag, sbrConfigData.SbrSyntaxFlags)

	if InitSbrCodeEnvelope(&hEnv.SbrCodeEnvelope, sbrConfigData.NSfb[:],
		params.DeltaTAcross, params.DFEdge1stEnv, params.DFEdgeIncr) != 0 {
		return 1
	}

	if InitSbrCodeEnvelope(&hEnv.SbrCodeNoiseFloor, noiseBands[:],
		params.DeltaTAcross, 0, 0) != 0 {
		return 1
	}

	if InitSbrHuffmanTables(&hEnv.EncEnvData, &hEnv.SbrCodeEnvelope,
		&hEnv.SbrCodeNoiseFloor, sbrHeaderData.SbrAmpRes) != 0 {
		return 1
	}

	InitFrameInfoGenerator(&hEnv.SbrEnvFrame, params.Spread, e, params.Stat, timeSlots,
		hEnv.EncEnvData.FreqResFixfix[:], hEnv.EncEnvData.FResTransIsLow, int(hEnv.EncEnvData.LdGrid))

	// !(SBR_SYNTAX_LOW_DELAY): the fast transient detector is not initialised.

	if InitSbrTransientDetector(&hEnv.SbrTransientDetector, false, sbrConfigData.FrameSize,
		sbrConfigData.SampleFreq, params.StandardBitrate, sbrConfigData.NChannels,
		params.BitRate, params.TranThr, params.TranDetMode, tranFc,
		sbrConfigData.NoQmfSlots, sbrConfigData.NoQmfBands, frameShift, tranOff) != 0 {
		return 1
	}

	sbrConfigData.XposCtrlSwitch = params.SbrXposCtrl

	hEnv.EncEnvData.NoHarmonics = sbrConfigData.NSfb[hiRes]
	hEnv.EncEnvData.AddHarmonicFlag = 0

	return 0
}

// updateFreqBandTable is the 1:1 port of updateFreqBandTable
// (sbr_encoder.cpp:812-848). Returns 1 on error.
func updateFreqBandTable(sbrConfigData *SbrConfigData, sbrHeaderData *EncSbrHeaderData, downSampleFactor int) int {
	k0, k2, err := FindStartAndStopBand(sbrConfigData.SampleFreq,
		sbrConfigData.SampleFreq>>(downSampleFactor-1), sbrConfigData.NoQmfBands,
		sbrHeaderData.SbrStartFrequency, sbrHeaderData.SbrStopFrequency)
	if err != 0 {
		return 1
	}

	numMaster, errc := UpdateFreqScale(sbrConfigData.VKMaster, k0, k2,
		sbrHeaderData.FreqScale, sbrHeaderData.AlterScale)
	if errc != 0 {
		return 1
	}
	sbrConfigData.NumMaster = numMaster

	sbrHeaderData.SbrXoverBand = 0

	nHi, xover, errh := UpdateHiRes(sbrConfigData.FreqBandTable[hiRes], sbrConfigData.VKMaster,
		sbrConfigData.NumMaster, sbrHeaderData.SbrXoverBand)
	if errh != 0 {
		return 1
	}
	sbrConfigData.NSfb[hiRes] = nHi
	sbrHeaderData.SbrXoverBand = xover

	sbrConfigData.NSfb[loRes] = UpdateLoRes(sbrConfigData.FreqBandTable[loRes],
		sbrConfigData.FreqBandTable[hiRes], sbrConfigData.NSfb[hiRes])

	sbrConfigData.XOverFreq = (int(sbrConfigData.FreqBandTable[loRes][0])*sbrConfigData.SampleFreq/
		sbrConfigData.NoQmfBands + 1) >> 1

	return 0
}

// resetEnvChannel is the 1:1 port of resetEnvChannel (sbr_encoder.cpp:859-889).
func resetEnvChannel(sbrConfigData *SbrConfigData, sbrHeaderData *EncSbrHeaderData, hEnv *EnvChannel) int {
	hEnv.TonCorr.SbrNoiseFloorEstimate.NoiseBands = sbrHeaderData.SbrNoiseBands

	if hEnv.TonCorr.ResetTonCorrParamExtr(sbrConfigData.XposCtrlSwitch,
		int(sbrConfigData.FreqBandTable[hiRes][0]), sbrConfigData.VKMaster,
		sbrConfigData.NumMaster, sbrConfigData.SampleFreq, sbrConfigData.FreqBandTable,
		sbrConfigData.NSfb[:], sbrConfigData.NoQmfBands) != 0 {
		return 1
	}

	hEnv.SbrCodeNoiseFloor.NSfb[loRes] = hEnv.TonCorr.SbrNoiseFloorEstimate.NoNoiseBands
	hEnv.SbrCodeNoiseFloor.NSfb[hiRes] = hEnv.TonCorr.SbrNoiseFloorEstimate.NoNoiseBands

	hEnv.SbrCodeEnvelope.NSfb[loRes] = sbrConfigData.NSfb[loRes]
	hEnv.SbrCodeEnvelope.NSfb[hiRes] = sbrConfigData.NSfb[hiRes]

	hEnv.EncEnvData.NoHarmonics = sbrConfigData.NSfb[hiRes]

	hEnv.SbrCodeEnvelope.UpDate = 0
	hEnv.SbrCodeNoiseFloor.UpDate = 0

	return 0
}

// sbrGetXOverFreq is the 1:1 port of FDKsbrEnc_SbrGetXOverFreq
// (sbr_encoder.cpp:899-930).
func sbrGetXOverFreq(hEl *SbrElement, xoverFreq int) int {
	pVKMaster := hEl.SbrConfigData.VKMaster
	cutoffSb := (4*xoverFreq*hEl.SbrConfigData.NoQmfBands/hEl.SbrConfigData.SampleFreq + 1) >> 1
	lastDiff := cutoffSb
	band := 0
	for band = 0; band < hEl.SbrConfigData.NumMaster; band++ {
		newDiff := int(nativeaac.FixpAbs(int32(int(pVKMaster[band]) - cutoffSb)))
		if newDiff >= lastDiff {
			band--
			break
		}
		lastDiff = newDiff
	}
	if band >= hEl.SbrConfigData.NumMaster {
		band = hEl.SbrConfigData.NumMaster - 1
	}
	return (int(pVKMaster[band])*hEl.SbrConfigData.SampleFreq/hEl.SbrConfigData.NoQmfBands + 1) >> 1
}
