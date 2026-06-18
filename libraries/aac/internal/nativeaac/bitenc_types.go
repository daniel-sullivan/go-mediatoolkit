// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Bitstream-encode area: data structures and constants.
//
// This file declares the Go counterparts of the C structs, enums and #defines
// that the bitstream-encode functions (bitenc.go), the Huffman emitters
// (bit_cnt_encode.go) and the raw-data-block element list (bitenc_rom.go)
// read. Each declaration names its C counterpart as file:line so a future
// reader can diff against the vendored libfdk-aac. The area is integer-only:
// nothing here touches floating point, so there is no aac_strict / default FP
// split — every value is bit-identical regardless of build tag.

// --- psy_const.h: block types and window shapes -----------------------------

// Block (window-sequence) types (psy_const.h:120, the unnamed enum
// LONG_WINDOW/START_WINDOW/SHORT_WINDOW/STOP_WINDOW).
const (
	LongWindow  = 0
	StartWindow = 1
	ShortWindow = 2
	StopWindow  = 3
)

// Window shapes (psy_const.h:131, SINE_WINDOW/KBD_WINDOW/LOL_WINDOW).
const (
	SineWindow = 0
	KbdWindow  = 1
	LolWindow  = 2
)

// TransFac is the encoder short-to-long window ratio (psy_const.h:109,
// #define TRANS_FAC 8).
const TransFac = 8

// LogNormPCM is the PCM normalisation log exponent (psy_const.h:161,
// #define LOG_NORM_PCM -15).
const LogNormPCM = -15

// --- psy_const.h:139 / interface.h:112: MS-stereo signalling ----------------

// SI_MS_MASK_* are the on-wire ms_mask_present codes (psy_const.h:139).
const (
	SiMsMaskNone = 0
	SiMsMaskSome = 1
	SiMsMaskAll  = 2
)

// MS_* are the internal msDigest values (interface.h:112,
// enum { MS_NONE = 0, MS_SOME = 1, MS_ALL = 2 }).
const (
	MsNone = 0
	MsSome = 1
	MsAll  = 2
)

// MsOn is the per-sfb ms_used flag (interface.h:114, enum { MS_ON = 1 }).
const MsOn = 1

// --- dyn_bits.h: sectioning constants ---------------------------------------

const (
	// SectEscValLong is the long-block section-length escape value
	// (dyn_bits.h:112, #define SECT_ESC_VAL_LONG 31).
	SectEscValLong = 31
	// SectEscValShort is the short-block section-length escape value
	// (dyn_bits.h:113, #define SECT_ESC_VAL_SHORT 7).
	SectEscValShort = 7
	// SectBitsLong is the long-block section-length field width
	// (dyn_bits.h:115, #define SECT_BITS_LONG 5).
	SectBitsLong = 5
	// SectBitsShort is the short-block section-length field width
	// (dyn_bits.h:116, #define SECT_BITS_SHORT 3).
	SectBitsShort = 3
	// PnsPCMBits is the PCM-coded first PNS energy field width
	// (dyn_bits.h:117, #define PNS_PCM_BITS 9).
	PnsPCMBits = 9
)

// SectionInfo is the per-section sectioning record (dyn_bits.h:119,
// typedef struct { ... } SECTION_INFO).
type SectionInfo struct {
	CodeBook    int // codeBook
	SfbStart    int // sfbStart
	SfbCnt      int // sfbCnt
	SectionBits int // sectionBits (huff + si!)
}

// MaxSections bounds SectionData.HuffSection, mirroring C
// #define MAX_SECTIONS MAX_GROUPED_SFB (dyn_bits.h:111). MAX_GROUPED_SFB is the
// largest grouped scalefactor-band count and is large enough that the port's
// callers (which fill at most one section per sfb) never overflow it.
const MaxSections = 120

// SectionData is the per-channel sectioning + bit-accounting record
// (dyn_bits.h:126, typedef struct { ... } SECTION_DATA).
type SectionData struct {
	BlockType      int // blockType
	NoOfGroups     int // noOfGroups
	SfbCnt         int // sfbCnt
	MaxSfbPerGroup int // maxSfbPerGroup
	SfbPerGroup    int // sfbPerGroup
	NoOfSections   int // noOfSections
	HuffSection    [MaxSections]SectionInfo
	SideInfoBits   int // sideInfoBits (sectioning bits)
	HuffmanBits    int // huffmanBits  (huffman coded bits)
	ScalefacBits   int // scalefacBits (scalefac coded bits)
	NoiseNrgBits   int // noiseNrgBits (noiseEnergy coded bits)
	FirstScf       int // firstScf     (first scf to be coded)
}

// --- aacenc_tns.h: TNS data -------------------------------------------------

const (
	// MaxNumOfFilters bounds TnsInfo's per-window filter arrays
	// (aacenc_tns.h MAX_NUM_OF_FILTERS).
	MaxNumOfFilters = 3
	// TnsMaxOrder bounds a TNS filter's coefficient count
	// (aacenc_tns.h TNS_MAX_ORDER, 12 for AAC-LC).
	TnsMaxOrder = 12
)

// TnsInfo is the per-channel TNS configuration written into the bitstream
// (aacenc_tns.h:194, typedef struct { ... } TNS_INFO).
type TnsInfo struct {
	NumOfFilters [TransFac]int                                 // numOfFilters[TRANS_FAC]
	CoefRes      [TransFac]int                                 // coefRes[TRANS_FAC]
	Length       [TransFac][MaxNumOfFilters]int                // length[][MAX_NUM_OF_FILTERS]
	Order        [TransFac][MaxNumOfFilters]int                // order[][MAX_NUM_OF_FILTERS]
	Direction    [TransFac][MaxNumOfFilters]int                // direction[][MAX_NUM_OF_FILTERS]
	CoefCompress [TransFac][MaxNumOfFilters]int                // coefCompress[][MAX_NUM_OF_FILTERS]
	Coef         [TransFac][MaxNumOfFilters][TnsMaxOrder]int16 // coef[][][TNS_MAX_ORDER]
}

// --- FDK_audio.h: element ids and extension payload types -------------------

// MP4 element ids (FDK_audio.h:423, typedef enum { ... } MP4_ELEMENT_ID).
const (
	IDSCE = 0
	IDCPE = 1
	IDCCE = 2
	IDLFE = 3
	IDDSE = 4
	IDFIL = 6
	IDEND = 7
)

// Extension payload types (FDK_audio.h:470,
// typedef enum { ... } EXT_PAYLOAD_TYPE).
const (
	ExtFIL          = 0x00
	ExtFillData     = 0x01
	ExtDataElement  = 0x02
	ExtLdsacData    = 0x09
	ExtDynamicRange = 0x0b
	ExtSacData      = 0x0c
	ExtSbrData      = 0x0d
	ExtSbrDataCRC   = 0x0e
)

// --- FDK_audio.h: audio object types ----------------------------------------

// Audio object types relevant to the bitstream-encode element list
// (FDK_audio.h:163, AUDIO_OBJECT_TYPE).
const (
	AOTAACLC     = 2
	AOTSBR       = 5
	AOTPS        = 29
	AOTERAACLC   = 17
	AOTERAACLD   = 23
	AOTERAACSCAL = 20
	AOTERAACELD  = 39
	AOTUSAC      = 42
	AOTDRMAAC    = 26
)

// --- FDK_audio.h: audio codec (syntax) flags --------------------------------

// Audio codec syntax flags (FDK_audio.h:293, the AC_* bit masks). Only the
// subset the bitstream-encode area tests is declared.
const (
	ACERVCB11  = 0x000004 // AC_ER_VCB11
	ACERRVLC   = 0x000002 // AC_ER_RVLC (RVLC sub-flag bit)
	ACERHCR    = 0x000001 // AC_ER_HCR  (HCR sub-flag bit)
	ACScalable = 0x000008 // AC_SCALABLE
	ACELD      = 0x000010 // AC_ELD
	ACER       = 0x000040 // AC_ER
	ACDRM      = 0x080000 // AC_DRM
)

// --- bitenc.h: encoder limits and error codes -------------------------------

// ElIDBits is the element-id field width (bitenc.h:126, #define EL_ID_BITS 3).
const ElIDBits = 3

// EncoderError holds the AAC_ENCODER_ERROR codes the bitstream-encode entry
// points return (aacenc.h, enum AAC_ENCODER_ERROR). Only the subset this area
// produces is declared.
type EncoderError int

const (
	AacEncOK                     EncoderError = 0x0000 // AAC_ENC_OK
	AacEncUnknown                EncoderError = 0x0002 // AAC_ENC_UNKNOWN
	AacEncBitresTooLow           EncoderError = 0x40a0 // AAC_ENC_BITRES_TOO_LOW
	AacEncBitresTooHigh          EncoderError = 0x40a1 // AAC_ENC_BITRES_TOO_HIGH
	AacEncQuantError             EncoderError = 0x4020 // AAC_ENC_QUANT_ERROR
	AacEncUnsupportedAOT         EncoderError = 0x3000 // AAC_ENC_UNSUPPORTED_AOT
	AacEncUnsupportedBitrateMode EncoderError = 0x3028 // AAC_ENC_UNSUPPORTED_BITRATE_MODE
	AacEncUnsupportedChannelconf EncoderError = 0x30e0 // AAC_ENC_UNSUPPORTED_CHANNELCONFIG
	AacEncInvalidChannelBitrate  EncoderError = 0x4100 // AAC_ENC_INVALID_CHANNEL_BITRATE
	AacEncWrittenBitsError       EncoderError = 0x4040 // AAC_ENC_WRITTEN_BITS_ERROR
	AacEncInvalidElementInfoType EncoderError = 0x4120 // AAC_ENC_INVALID_ELEMENTINFO_TYPE
	AacEncWriteScalError         EncoderError = 0x41e0 // AAC_ENC_WRITE_SCAL_ERROR
	AacEncWriteSecError          EncoderError = 0x4200 // AAC_ENC_WRITE_SEC_ERROR
	AacEncWriteSpecError         EncoderError = 0x4220 // AAC_ENC_WRITE_SPEC_ERROR
	// init-path errors FDKaacEnc_Initialize returns (aacenc.h AAC_ENCODER_ERROR).
	AacEncInvalidHandle         EncoderError = 0x2020 // AAC_ENC_INVALID_HANDLE
	AacEncInvalidFrameLength    EncoderError = 0x2080 // AAC_ENC_INVALID_FRAME_LENGTH
	AacEncUnsupportedFilterbank EncoderError = 0x3010 // AAC_ENC_UNSUPPORTED_FILTERBANK
	AacEncUnsupportedBitrate    EncoderError = 0x3020 // AAC_ENC_UNSUPPORTED_BITRATE
	AacEncUnsupportedERFmt      EncoderError = 0x30a0 // AAC_ENC_UNSUPPORTED_ER_FORMAT
	AacEncUnsupportedSampRate   EncoderError = 0x3100 // AAC_ENC_UNSUPPORTED_SAMPLINGRATE
)

// --- qc_data.h: quantizer/coder output structures ---------------------------

// MaxGroupedSFB bounds the per-channel grouped-sfb arrays
// (psy_const.h MAX_GROUPED_SFB). Matches MaxSections.
const MaxGroupedSFB = MaxSections

// ElementInfo describes one channel element (qc_data.h:127,
// typedef struct { ... } ELEMENT_INFO).
type ElementInfo struct {
	ElType        int    // elType (MP4_ELEMENT_ID)
	InstanceTag   int    // instanceTag
	NChannelsInEl int    // nChannelsInEl
	ChannelIndex  [2]int // ChannelIndex[2]
	RelativeBits  int32  // relativeBits (FIXP_DBL)
}

// QcOutChannel is the per-channel quantizer/coder output that the bitstream
// writer reads (qc_data.h:177, typedef struct { ... } QC_OUT_CHANNEL). Only
// the fields the bitstream-encode area touches are declared.
type QcOutChannel struct {
	// MdctSpectrum is the per-channel MDCT line buffer (qc_data.h:178,
	// FIXP_DBL mdctSpectrum[1024]) the quantizer/coder reads: CalcFormFactor,
	// EstimateScaleFactors and QuantizeSpectrum all operate on it. The leaf
	// ports take this buffer as an explicit slice parameter; the driver tier
	// passes QcOutChannel.MdctSpectrum[:].
	MdctSpectrum  [1024]int32         // mdctSpectrum[1024] (FIXP_DBL)
	QuantSpec     [1024]int16         // quantSpec[1024]
	MaxValueInSfb [MaxGroupedSFB]uint // maxValueInSfb[MAX_GROUPED_SFB]
	Scf           [MaxGroupedSFB]int  // scf[MAX_GROUPED_SFB]
	GlobalGain    int                 // globalGain
	SectionData   SectionData         // sectionData

	// Rate-loop / threshold-adjustment fields (qc_data.h:187-197). These are
	// the LD-domain (ld64 log) per-sfb energy/threshold/minSnr figures the
	// scalefactor estimator and the adj_thr threshold-adjustment loop read and
	// write. Declared here so there is ONE coherent QC_OUT_CHANNEL model.
	SfbFormFactorLdData     [MaxGroupedSFB]int32 // sfbFormFactorLdData (FIXP_DBL)
	SfbThresholdLdData      [MaxGroupedSFB]int32 // sfbThresholdLdData (FIXP_DBL)
	SfbMinSnrLdData         [MaxGroupedSFB]int32 // sfbMinSnrLdData (FIXP_DBL)
	SfbEnergyLdData         [MaxGroupedSFB]int32 // sfbEnergyLdData (FIXP_DBL)
	SfbEnergy               [MaxGroupedSFB]int32 // sfbEnergy (FIXP_DBL)
	SfbWeightedEnergyLdData [MaxGroupedSFB]int32 // sfbWeightedEnergyLdData (FIXP_DBL)
	SfbEnFacLd              [MaxGroupedSFB]int32 // sfbEnFacLd (FIXP_DBL)
	SfbSpreadEnergy         [MaxGroupedSFB]int32 // sfbSpreadEnergy (FIXP_DBL)
}

// QcOutExtension is one extension payload (qc_data.h:201,
// typedef struct { ... } QC_OUT_EXTENSION).
type QcOutExtension struct {
	Type         int    // type (EXT_PAYLOAD_TYPE)
	NPayloadBits int    // nPayloadBits
	Payload      []byte // pPayload
}

// QcOutElement is the per-element quantizer/coder output (qc_data.h:208,
// typedef struct { ... } QC_OUT_ELEMENT). The bitstream-writer fields were
// declared first; the rate-loop fields (staticBitsUsed, dynBitsUsed,
// extBitsUsed, grantedDynBits, grantedPe, grantedPeCorr, peData) that
// FDKaacEnc_QCMain and its helpers read/write follow.
type QcOutElement struct {
	StaticBitsUsed int               // staticBitsUsed
	DynBitsUsed    int               // dynBitsUsed
	ExtBitsUsed    int               // extBitsUsed
	NExtensions    int               // nExtensions
	Extension      [1]QcOutExtension // extension[1]
	GrantedDynBits int               // grantedDynBits
	GrantedPe      int               // grantedPe
	GrantedPeCorr  int               // grantedPeCorr
	PeData         peData            // peData
	QcOutChannel   [2]*QcOutChannel  // qcOutChannel[2]
}

// QcOut is the per-access-unit quantizer/coder output (qc_data.h:237,
// typedef struct { ... } QC_OUT). The bitstream-writer fields were declared
// first; the rate-loop fields FDKaacEnc_QCMain and its helpers maintain follow.
type QcOut struct {
	QcElement          [8]*QcOutElement      // qcElement[8]
	PQcOutChannels     [8]*QcOutChannel      // pQcOutChannels[8] (per-AU channel pool)
	Extension          [2 + 2]QcOutExtension // extension[2+2] (global extension payload)
	NExtensions        int                   // nExtensions
	MaxDynBits         int                   // maxDynBits
	GrantedDynBits     int                   // grantedDynBits
	TotFillBits        int                   // totFillBits
	ElementExtBits     int                   // elementExtBits
	GlobalExtBits      int                   // globalExtBits
	StaticBits         int                   // staticBits
	TotalNoRedPe       int                   // totalNoRedPe
	TotalGrantedPeCorr int                   // totalGrantedPeCorr
	UsedDynBits        int                   // usedDynBits
	AlignBits          int                   // alignBits
	TotalBits          int                   // totalBits
}

// QcState is the persistent quantizer/coder state (qc_data.h:299,
// typedef struct { ... } QC_STATE). The bitstream writer reads only
// globHdrBits; the rate-loop fields FDKaacEnc_QCMain and its helpers read
// follow.
type QcState struct {
	GlobHdrBits      int              // globHdrBits
	MaxBitsPerFrame  int              // maxBitsPerFrame
	MinBitsPerFrame  int              // minBitsPerFrame
	NElements        int              // nElements
	BitrateMode      QcdataBrMode     // bitrateMode
	BitResMode       AacencBitresMode // bitResMode
	BitResTot        int              // bitResTot
	BitResTotMax     int              // bitResTotMax
	MaxIterations    int              // maxIterations
	InvQuant         int              // invQuant
	MaxBitFac        int32            // maxBitFac (FIXP_DBL)
	Padding          Padding          // padding
	ElementBits      [8]*ElementBits  // elementBits[8]
	DZoneQuantEnable int              // dZoneQuantEnable

	// VbrQualFactor (qc_data.h: FIXP_DBL vbrQualFactor) is the VBR quality
	// factor QCInit looks up from tableVbrQualFactor and forwards to AdjThrInit.
	// On the CBR path it stays 0.
	VbrQualFactor int32 // vbrQualFactor (FIXP_DBL)

	// HAdjThr / HBitCounter are the ADJ_THR_STATE and the dyn-bit counter the
	// rate-control loop (FDKaacEnc_QCMain) drives. In C they are the hAdjThr /
	// hBitCounter handles GetRam_-allocated by FDKaacEnc_QCNew; the Go port
	// embeds the equivalent state graph.
	HAdjThr     *adjThrState  // hAdjThr (ADJ_THR_STATE*)
	HBitCounter *bitCntrState // hBitCounter (BITCNTR_STATE*)
}

// ChannelMapping describes the elements in the access unit (qc_data.h:135,
// typedef struct { ... } CHANNEL_MAPPING).
type ChannelMapping struct {
	EncMode      ChannelMode    // encMode
	NChannels    int            // nChannels
	NChannelsEff int            // nChannelsEff
	NElements    int            // nElements
	ElInfo       [8]ElementInfo // elInfo[8]
}

// --- interface.h: PSY output structures -------------------------------------

// PsyOutChannel is the per-channel psychoacoustic output that the bitstream
// writer reads (interface.h, PSY_OUT_CHANNEL). Only the fields the
// bitstream-encode area touches are declared.
type PsyOutChannel struct {
	SfbCnt             int                // sfbCnt
	SfbPerGroup        int                // sfbPerGroup
	MaxSfbPerGroup     int                // maxSfbPerGroup
	WindowShape        int                // windowShape
	LastWindowSequence int                // lastWindowSequence
	GroupingMask       int                // groupingMask
	MdctScale          int                // mdctScale
	SfbOffsets         [1024 + 1]int      // sfbOffsets[]
	NoiseNrg           [MaxGroupedSFB]int // noiseNrg[]
	IsBook             [MaxGroupedSFB]int // isBook[]
	IsScale            [MaxGroupedSFB]int // isScale[]
	TnsInfo            TnsInfo            // tnsInfo

	// GroupLen mirrors PSY_OUT_CHANNEL.groupLen[MAX_NO_OF_GROUPS]
	// (interface.h:131): the per-group window count the bitstream ics_info uses.
	GroupLen [maxNoOfGroups]int // groupLen[MAX_NO_OF_GROUPS]

	// The six FIXP_DBL* members below (interface.h:139-144) are, in C, pointers
	// aliased into QC_OUT_CHANNEL memory (FDKaacEnc_EncodeFrame assigns
	// psyOutChan->X = qcOutChan->X before psyMain). FDKaacEnc_psyMain writes
	// through them; QCMainPrepare/QCMain/the bitstream writer then read the same
	// QC_OUT_CHANNEL cells. The Go port keeps PSY_OUT_CHANNEL's own copies here
	// (psyMain writes them) and FDKaacEnc_EncodeFrame copies them into
	// QcOutChannel to reproduce the aliasing exactly.
	MdctSpectrum       [1024]int32          // mdctSpectrum (FIXP_DBL*)
	SfbEnergy          [MaxGroupedSFB]int32 // sfbEnergy (FIXP_DBL*)
	SfbSpreadEnergy    [MaxGroupedSFB]int32 // sfbSpreadEnergy (FIXP_DBL*)
	SfbThresholdLdData [MaxGroupedSFB]int32 // sfbThresholdLdData (FIXP_DBL*)
	SfbMinSnrLdData    [MaxGroupedSFB]int32 // sfbMinSnrLdData (FIXP_DBL*)
	SfbEnergyLdData    [MaxGroupedSFB]int32 // sfbEnergyLdData (FIXP_DBL*)
}

// PsyOutToolsInfo carries the joint-stereo tool decisions (interface.h,
// TOOLSINFO). Only the MS fields the bitstream writer reads are declared.
type PsyOutToolsInfo struct {
	MsDigest int                // msDigest
	MsMask   [MaxGroupedSFB]int // msMask[]
}

// PsyOutElement is the per-element psychoacoustic output (interface.h,
// PSY_OUT_ELEMENT). Only the fields the bitstream writer touches are declared.
type PsyOutElement struct {
	CommonWindow  int               // commonWindow
	ToolsInfo     PsyOutToolsInfo   // toolsInfo
	PsyOutChannel [2]*PsyOutChannel // psyOutChannel[2]
}

// PsyOut is the per-access-unit psychoacoustic output (interface.h, PSY_OUT).
type PsyOut struct {
	PsyOutElement   [8]*PsyOutElement // psyOutElement[8]
	PPsyOutChannels [8]*PsyOutChannel // pPsyOutChannels[8] (per-AU channel pool)
}
