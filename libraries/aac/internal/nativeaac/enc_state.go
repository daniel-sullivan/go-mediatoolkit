// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

// FDK-AAC-derived. See libfdk/COPYING (Fraunhofer FDK-AAC license). The whole
// AAC island is fenced behind the opt-in `aacfdk` build tag, so a default
// `go build ./...` (cgo or not) links none of this file.

package nativeaac

// Encoder struct universe: the persistent/per-frame state structures the AAC
// encoder ORCHESTRATION loops (FDKaacEnc_psyMain, FDKaacEnc_adjThr,
// FDKaacEnc_QCMain, FDKaacEnc_EncodeFrame and the config/init path) thread
// between the already-ported, parity-verified component kernels.
//
// This file completes the shared struct universe that bitenc_types.go and
// qc_main_types.go began (PSY_OUT*, QC_OUT*, QC_STATE, CHANNEL_MAPPING,
// ELEMENT_INFO, ELEMENT_BITS, PADDING). It adds the psychoacoustic-side
// PSY_STATIC / PSY_DATA / PSY_ELEMENT / PSY_DYNAMIC / PSY_INTERNAL and the
// top-level encoder config (AACENC_CONFIG) and handle (struct AAC_ENC). It
// declares ONLY the fields the orchestration actually reads/writes, each cited
// to its C struct as file:line so a future reader can diff against the vendored
// libfdk-aac. These structs are pure integer state (FIXP_DBL == int32, INT ==
// int); there is no floating point and therefore no aac_strict / default FP
// split — every value is bit-identical regardless of build tag.
//
// Single-coherent-definition rule: the FIXP_DBL SFB union the psy side shares
// with grp_data is the already-defined [sfbGrouped] (psy_grpdata.go); this file
// reuses it rather than introducing a parallel model. The maxGroupedSfb (=60,
// the genuine C MAX_GROUPED_SFB) and maxSfbLong (=51, MAX_SFB) and
// maxNoOfGroups / maxSfbShort / TransFac constants are likewise reused from
// their existing definitions.

// --- psy_const.h: input-buffer and SFB sizing reused by the psy state --------

// maxInputBufferSize is MAX_INPUT_BUFFER_SIZE (psy_const.h:156,
// #define MAX_INPUT_BUFFER_SIZE (2 * 1024) == 2048): the PSY_STATIC PCM input
// buffer length.
const maxInputBufferSize = 2 * 1024

// overlapAddBufferSize is the PSY_STATIC overlapAddBuffer length
// (psy_data.h:143, FIXP_DBL overlapAddBuffer[3 * 512 / 2] == 768).
const overlapAddBufferSize = 3 * 512 / 2

// MAX_SFB (psy_const.h:149, == MAX_SFB_LONG == 51) is the length of the
// PSY_STATIC pre-echo threshold history sfbThresholdnm1[MAX_SFB]; the existing
// maxSfbLong (dyn_bits.go) is reused for it directly below.

// --- psy_data.h:135-139: SFB_MAX_SCALE (the INT variant of the SFB union) -----

// sfbGroupedInt ports SFB_MAX_SCALE (psy_data.h:135):
//
//	typedef shouldBeUnion {
//	  INT Long[MAX_GROUPED_SFB];
//	  INT Short[TRANS_FAC][MAX_SFB_SHORT];
//	} SFB_MAX_SCALE;
//
// the INT-typed sibling of the FIXP_DBL [sfbGrouped] union (psy_grpdata.go).
// Long[] and Short[][] alias the same storage with the identical
// cell == w*MAX_SFB_SHORT+b layout, so the same flat-backing-array accessor
// model is used to keep the two unions coherent.
type sfbGroupedInt struct {
	cells [sfbGroupedCells]int
}

// Long returns the union's Long[i] (cell i).
func (s *sfbGroupedInt) Long(i int) int { return s.cells[i] }

// SetLong sets the union's Long[i] (cell i), aliasing Short storage as in C.
func (s *sfbGroupedInt) SetLong(i, v int) { s.cells[i] = v }

// Short returns the union's Short[w][b] (cell w*MAX_SFB_SHORT+b).
func (s *sfbGroupedInt) Short(w, b int) int { return s.cells[w*maxSfbShort+b] }

// SetShort sets the union's Short[w][b] (cell w*MAX_SFB_SHORT+b).
func (s *sfbGroupedInt) SetShort(w, b, v int) { s.cells[w*maxSfbShort+b] = v }

// Slice returns the backing union storage from cell `from` onward, modelling
// the C pointer `pSfbMaxScaleSpec[ch] + w*maxSfb`.
func (s *sfbGroupedInt) Slice(from int) []int { return s.cells[from:] }

// Cells returns the whole backing union storage (cell 0 onward).
func (s *sfbGroupedInt) Cells() []int { return s.cells[:] }

// --- psy_data.h:141: PSY_STATIC ---------------------------------------------

// PsyStatic is the per-channel persistent psychoacoustic state carried across
// frames (psy_data.h:141, typedef struct { ... } PSY_STATIC). PsyInputBuffer
// is INT_PCM (int16 in the integer encoder build); MdctPers is the persistent
// forward-MDCT overlap state ([mdctT], mdct.go) that enc_transform.go's
// mdctBlockFwd / TransformReal thread; BlockSwitchingControl is the
// block-switch detector state (block_switch.go); SfbThresholdNm1 / MdctScaleNm1
// are the FDKaacEnc_PreEchoControl history.
type PsyStatic struct {
	PsyInputBuffer        [maxInputBufferSize]int16   // psyInputBuffer (INT_PCM*)
	OverlapAddBuffer      [overlapAddBufferSize]int32 // overlapAddBuffer[3*512/2] (FIXP_DBL)
	MdctPers              mdctT                       // mdctPers (MDCT persistent data)
	BlockSwitchingControl BlockSwitchingControl       // blockSwitchingControl
	SfbThresholdNm1       [maxSfbLong]int32           // sfbThresholdnm1[MAX_SFB] (FIXP_DBL)
	MdctScaleNm1          int                         // mdctScalenm1
	CalcPreEcho           int                         // calcPreEcho
	IsLFE                 int                         // isLFE
}

// --- psy_data.h:153: PSY_DATA -----------------------------------------------

// PsyData is the per-channel per-frame psychoacoustic working set
// (psy_data.h:153, typedef struct { ... } PSY_DATA). The SFB union members are
// the already-ported [sfbGrouped] (FIXP_DBL) / [sfbGroupedInt] (INT) so the psy
// orchestration shares ONE model with FDKaacEnc_groupShortData (psy_grpdata.go)
// — the Short[wnd][sfb] inputs and Long[i] regrouped outputs alias the same
// storage exactly as in C. MdctSpectrum is the granuleLength forward-MDCT
// output.
type PsyData struct {
	MdctSpectrum      []int32                // mdctSpectrum (FIXP_DBL*, granuleLength)
	SfbThreshold      sfbGrouped             // sfbThreshold (SFB_THRESHOLD)
	SfbEnergy         sfbGrouped             // sfbEnergy (SFB_ENERGY)
	SfbEnergyLdData   sfbGrouped             // sfbEnergyLdData (SFB_LD_ENERGY)
	SfbMaxScaleSpec   sfbGroupedInt          // sfbMaxScaleSpec (SFB_MAX_SCALE)
	SfbEnergyMS       sfbGrouped             // sfbEnergyMS (SFB_ENERGY)
	SfbEnergyMSLdData [maxGroupedSfb]int32   // sfbEnergyMSLdData[MAX_GROUPED_SFB] (FIXP_DBL)
	SfbSpreadEnergy   sfbGrouped             // sfbSpreadEnergy (SFB_ENERGY)
	MdctScale         int                    // mdctScale (exponent of mdctSpectrum)
	GroupedSfbOffset  [maxGroupedSfb + 1]int // groupedSfbOffset[MAX_GROUPED_SFB+1]
	SfbActive         int                    // sfbActive
	LowpassLine       int                    // lowpassLine
}

// --- psy_main.h:113: PSY_ELEMENT --------------------------------------------

// PsyElement is the per-element psychoacoustic static state
// (psy_main.h:113, typedef struct { ... } PSY_ELEMENT). PsyStatic[ch] points
// into the shared per-channel static pool (PsyInternal.PStaticChannels).
type PsyElement struct {
	PsyStatic [2]*PsyStatic // psyStatic[2]
}

// --- psy_main.h:118: PSY_DYNAMIC --------------------------------------------

// PsyDynamic is the per-element per-frame psychoacoustic working set
// (psy_main.h:118, typedef struct { ... } PSY_DYNAMIC). PsyData is the grouping
// / threshold / energy working set this struct universe owns; TnsData is the
// already-ported runtime TNS decision state ([TNSData], enc_tns_detect.go). The
// PNS_DATA member (pnsData[2]) carries the per-channel PNS detect/code state
// ([PNSData], aacenc_pns.go) that FDKaacEnc_psyMain's tonality/PNS chain fills.
// PSY_CONFIGURATION (psyConf) is a separate
// component-config type a later phase owns and is intentionally NOT modelled
// here — it is not part of this phase's listed struct universe.
type PsyDynamic struct {
	PsyData [2]PsyData // psyData[2]
	TnsData [2]TNSData // tnsData[2]
	PnsData [2]PNSData // pnsData[2]
}

// --- psy_main.h:125: PSY_INTERNAL -------------------------------------------

// PsyInternal is the psychoacoustic kernel handle the encoder owns across its
// lifetime (psy_main.h:125, typedef struct { ... } PSY_INTERNAL). PsyElement /
// PStaticChannels are the per-element and per-channel static pools; PsyDynamic
// is the per-frame working set; GranuleLength is the per-window transform
// length (1024 for AAC-LC). PsyConf[2] (PSY_CONFIGURATION, [0]=LONG / [1]=SHORT)
// is the per-block-type psychoacoustic configuration FDKaacEnc_psyMainInit fills
// via FDKaacEnc_InitPsyConfiguration / FDKaacEnc_InitTnsConfiguration /
// FDKaacEnc_InitPnsConfiguration. It was deferred until those leaf inits were
// ported; now that InitTnsConfiguration is in place the init driver tier
// (psy_main_init.go) populates it 1:1.
type PsyInternal struct {
	PsyConf         [2]PsyConfiguration // psyConf[2] (LONG / SHORT)
	PsyElement      [8]*PsyElement      // psyElement[8]
	PStaticChannels [8]*PsyStatic       // pStaticChannels[8]
	PsyDynamic      *PsyDynamic         // psyDynamic
	GranuleLength   int                 // granuleLength
}

// --- aacenc.h:217: AACENC_CONFIG --------------------------------------------

// AacencConfig is the encoder configuration the init path fills and the
// orchestration reads (aacenc.h:217, struct AACENC_CONFIG). Only the fields the
// AAC-LC CBR orchestration consults are declared.
type AacencConfig struct {
	SampleRate       int               // sampleRate
	BitRate          int               // bitRate
	AncDataBitRate   int               // ancDataBitRate
	NSubFrames       int               // nSubFrames
	AudioObjectType  int               // audioObjectType (AUDIO_OBJECT_TYPE)
	AverageBits      int               // averageBits
	BitrateMode      AacencBitrateMode // bitrateMode
	NChannels        int               // nChannels
	BandWidth        int               // bandWidth
	ChannelMode      ChannelMode       // channelMode
	FrameLength      int               // framelength
	SyntaxFlags      uint              // syntaxFlags
	EpConfig         int               // epConfig (SCHAR)
	AncRate          int               // anc_Rate
	MaxAncBytesPerAU uint              // maxAncBytesPerAU
	MinBitsPerFrame  int               // minBitsPerFrame
	MaxBitsPerFrame  int               // maxBitsPerFrame
	AudioMuxVersion  int               // audioMuxVersion
	UseTns           bool              // useTns (UCHAR flag)
	UsePns           bool              // usePns (UCHAR flag)
	UseIS            bool              // useIS (UCHAR flag)
	UseMS            bool              // useMS (UCHAR flag)
	UseRequant       bool              // useRequant (UCHAR flag)
	DownscaleFactor  uint              // downscaleFactor
}

// AacencBitrateMode mirrors AACENC_BITRATE_MODE (aacenc.h:201): the
// public-API bitrate-mode selector the config carries (distinct from the
// internal [QcdataBrMode]).
type AacencBitrateMode int

const (
	AacBitrateModeInvalid AacencBitrateMode = -1 // AACENC_BR_MODE_INVALID
	AacBitrateModeCBR     AacencBitrateMode = 0  // AACENC_BR_MODE_CBR
	AacBitrateModeVBR1    AacencBitrateMode = 1  // AACENC_BR_MODE_VBR_1
	AacBitrateModeVBR2    AacencBitrateMode = 2  // AACENC_BR_MODE_VBR_2
	AacBitrateModeVBR3    AacencBitrateMode = 3  // AACENC_BR_MODE_VBR_3
	AacBitrateModeVBR4    AacencBitrateMode = 4  // AACENC_BR_MODE_VBR_4
	AacBitrateModeVBR5    AacencBitrateMode = 5  // AACENC_BR_MODE_VBR_5
	AacBitrateModeFF      AacencBitrateMode = 6  // AACENC_BR_MODE_FF (fixed frame)
	AacBitrateModeSFR     AacencBitrateMode = 7  // AACENC_BR_MODE_SFR (superframe)
)

// --- aacEnc_ram.h:138: struct AAC_ENC ---------------------------------------

// AacEnc is the top-level encoder handle the orchestration loops thread
// (aacEnc_ram.h:138, struct AAC_ENC). Config is the encoder configuration;
// ChannelMapping the per-AU element layout; QcKernel/QcOut the
// quantizer/rate-control persistent state and per-AU output; PsyOut/PsyKernel
// the psychoacoustic per-AU output and persistent kernel; the lifetime vars
// (EncoderMode, Bandwidth90dB, BitrateMode, Aot) configure the per-frame
// orchestration. Only the fields the AAC-LC encode path reads/writes are
// declared.
type AacEnc struct {
	Config                *AacencConfig     // config
	AncillaryBitsPerFrame int               // ancillaryBitsPerFrame
	ChannelMapping        ChannelMapping    // channelMapping
	QcKernel              *QcState          // qcKernel
	QcOut                 [1]*QcOut         // qcOut[1]
	PsyOut                [1]*PsyOut        // psyOut[1]
	PsyKernel             *PsyInternal      // psyKernel
	EncoderMode           ChannelMode       // encoderMode
	Bandwidth90dB         int               // bandwidth90dB
	BitrateMode           AacencBitrateMode // bitrateMode
	DontWriteAdif         int               // dontWriteAdif
	MaxChannels           int               // maxChannels
	MaxElements           int               // maxElements
	MaxFrames             int               // maxFrames
	Aot                   int               // aot (AUDIO_OBJECT_TYPE)
}
