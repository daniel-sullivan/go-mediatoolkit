//go:build linux

// The stateless-HEVC control payload structures, byte-exact against
// linux/v4l2-controls.h on the 6.12 kernel (verified sizes: sps=40,
// pps=64, slice_params=280, scaling_matrix=1000, decode_params=328). The
// userspace HEVC parser (v4l2_hevc_parse_linux.go) fills these and the
// stateless decoder (v4l2_stateless_hevc_linux.go) hands their addresses
// to VIDIOC_S_EXT_CTRLS attached to a per-frame request fd.
//
// Field order, integer widths (signedness matters: several are __s8),
// trailing reserved padding, and the embedded fixed arrays are all part
// of the kernel ABI and must match exactly.

package hwaccel

// V4L2_HEVC_DPB_ENTRIES_NUM_MAX.
const v4l2HEVCDPBEntriesMax = 16

// SPS flags (V4L2_HEVC_SPS_FLAG_*).
const (
	hevcSPSFlagSeparateColourPlane    = 1 << 0
	hevcSPSFlagScalingListEnabled     = 1 << 1
	hevcSPSFlagAmpEnabled             = 1 << 2
	hevcSPSFlagSampleAdaptiveOffset   = 1 << 3
	hevcSPSFlagPCMEnabled             = 1 << 4
	hevcSPSFlagPCMLoopFilterDisabled  = 1 << 5
	hevcSPSFlagLongTermRefPicsPresent = 1 << 6
	hevcSPSFlagSPSTemporalMvpEnabled  = 1 << 7
	hevcSPSFlagStrongIntraSmoothing   = 1 << 8
)

// PPS flags (V4L2_HEVC_PPS_FLAG_*).
const (
	hevcPPSFlagDependentSliceSegment       = 1 << 0
	hevcPPSFlagOutputFlagPresent           = 1 << 1
	hevcPPSFlagSignDataHiding              = 1 << 2
	hevcPPSFlagCabacInitPresent            = 1 << 3
	hevcPPSFlagConstrainedIntraPred        = 1 << 4
	hevcPPSFlagTransformSkipEnabled        = 1 << 5
	hevcPPSFlagCuQPDeltaEnabled            = 1 << 6
	hevcPPSFlagSliceChromaQPOffsetsPresent = 1 << 7
	hevcPPSFlagWeightedPred                = 1 << 8
	hevcPPSFlagWeightedBipred              = 1 << 9
	hevcPPSFlagTransquantBypassEnabled     = 1 << 10
	hevcPPSFlagTilesEnabled                = 1 << 11
	hevcPPSFlagEntropyCodingSyncEnabled    = 1 << 12
	hevcPPSFlagLoopFilterAcrossTiles       = 1 << 13
	hevcPPSFlagLoopFilterAcrossSlices      = 1 << 14
	hevcPPSFlagDeblockingFilterOverride    = 1 << 15
	hevcPPSFlagDisableDeblockingFilter     = 1 << 16
	hevcPPSFlagListsModificationPresent    = 1 << 17
	hevcPPSFlagSliceSegmentHeaderExtension = 1 << 18
	hevcPPSFlagDeblockingFilterControl     = 1 << 19
	hevcPPSFlagUniformSpacing              = 1 << 20
)

// Slice-params flags (V4L2_HEVC_SLICE_PARAMS_FLAG_*).
const (
	hevcSliceFlagSAOLuma                  = 1 << 0
	hevcSliceFlagSAOChroma                = 1 << 1
	hevcSliceFlagTemporalMvpEnabled       = 1 << 2
	hevcSliceFlagMvdL1Zero                = 1 << 3
	hevcSliceFlagCabacInit                = 1 << 4
	hevcSliceFlagCollocatedFromL0         = 1 << 5
	hevcSliceFlagUseIntegerMv             = 1 << 6
	hevcSliceFlagDeblockingFilterDisabled = 1 << 7
	hevcSliceFlagLoopFilterAcrossSlices   = 1 << 8
	hevcSliceFlagDependentSliceSegment    = 1 << 9
)

// Decode-params flags (V4L2_HEVC_DECODE_PARAM_FLAG_*).
const (
	hevcDecodeParamFlagIRAPPic       = 0x1
	hevcDecodeParamFlagIDRPic        = 0x2
	hevcDecodeParamFlagNoOutputPrior = 0x4
)

// Slice types (V4L2_HEVC_SLICE_TYPE_*).
const (
	hevcSliceTypeB = 0
	hevcSliceTypeP = 1
	hevcSliceTypeI = 2
)

// DPB entry long-term flag.
const hevcDPBEntryLongTermReference = 0x01

// v4l2CtrlHEVCSPS mirrors struct v4l2_ctrl_hevc_sps (40 bytes).
type v4l2CtrlHEVCSPS struct {
	VideoParameterSetID                  uint8
	SeqParameterSetID                    uint8
	PicWidthInLumaSamples                uint16
	PicHeightInLumaSamples               uint16
	BitDepthLumaMinus8                   uint8
	BitDepthChromaMinus8                 uint8
	Log2MaxPicOrderCntLsbMinus4          uint8
	SPSMaxDecPicBufferingMinus1          uint8
	SPSMaxNumReorderPics                 uint8
	SPSMaxLatencyIncreasePlus1           uint8
	Log2MinLumaCodingBlockSizeMinus3     uint8
	Log2DiffMaxMinLumaCodingBlockSize    uint8
	Log2MinLumaTransformBlockSizeMinus2  uint8
	Log2DiffMaxMinLumaTransformBlockSize uint8
	MaxTransformHierarchyDepthInter      uint8
	MaxTransformHierarchyDepthIntra      uint8
	PCMSampleBitDepthLumaMinus1          uint8
	PCMSampleBitDepthChromaMinus1        uint8
	Log2MinPCMLumaCodingBlockSizeMinus3  uint8
	Log2DiffMaxMinPCMLumaCodingBlockSize uint8
	NumShortTermRefPicSets               uint8
	NumLongTermRefPicsSPS                uint8
	ChromaFormatIDC                      uint8
	SPSMaxSubLayersMinus1                uint8
	Reserved                             [6]uint8
	Flags                                uint64
}

// v4l2CtrlHEVCPPS mirrors struct v4l2_ctrl_hevc_pps (64 bytes).
type v4l2CtrlHEVCPPS struct {
	PicParameterSetID              uint8
	NumExtraSliceHeaderBits        uint8
	NumRefIdxL0DefaultActiveMinus1 uint8
	NumRefIdxL1DefaultActiveMinus1 uint8
	InitQPMinus26                  int8
	DiffCuQPDeltaDepth             uint8
	PPSCbQPOffset                  int8
	PPSCrQPOffset                  int8
	NumTileColumnsMinus1           uint8
	NumTileRowsMinus1              uint8
	ColumnWidthMinus1              [20]uint8
	RowHeightMinus1                [22]uint8
	PPSBetaOffsetDiv2              int8
	PPSTcOffsetDiv2                int8
	Log2ParallelMergeLevelMinus2   uint8
	Reserved                       uint8
	Flags                          uint64
}

// v4l2HEVCDPBEntry mirrors struct v4l2_hevc_dpb_entry (16 bytes).
type v4l2HEVCDPBEntry struct {
	Timestamp      uint64
	Flags          uint8
	FieldPic       uint8
	Reserved       uint16
	PicOrderCntVal int32
}

// v4l2HEVCPredWeightTable mirrors struct v4l2_hevc_pred_weight_table
// (194 bytes; unaligned, embedded inside slice_params).
type v4l2HEVCPredWeightTable struct {
	DeltaLumaWeightL0          [v4l2HEVCDPBEntriesMax]int8
	LumaOffsetL0               [v4l2HEVCDPBEntriesMax]int8
	DeltaChromaWeightL0        [v4l2HEVCDPBEntriesMax][2]int8
	ChromaOffsetL0             [v4l2HEVCDPBEntriesMax][2]int8
	DeltaLumaWeightL1          [v4l2HEVCDPBEntriesMax]int8
	LumaOffsetL1               [v4l2HEVCDPBEntriesMax]int8
	DeltaChromaWeightL1        [v4l2HEVCDPBEntriesMax][2]int8
	ChromaOffsetL1             [v4l2HEVCDPBEntriesMax][2]int8
	LumaLog2WeightDenom        uint8
	DeltaChromaLog2WeightDenom int8
}

// v4l2CtrlHEVCSliceParams mirrors struct v4l2_ctrl_hevc_slice_params
// (280 bytes).
type v4l2CtrlHEVCSliceParams struct {
	BitSize                  uint32
	DataByteOffset           uint32
	NumEntryPointOffsets     uint32
	NalUnitType              uint8
	NuhTemporalIDPlus1       uint8
	SliceType                uint8
	ColourPlaneID            uint8
	SlicePicOrderCnt         int32
	NumRefIdxL0ActiveMinus1  uint8
	NumRefIdxL1ActiveMinus1  uint8
	CollocatedRefIdx         uint8
	FiveMinusMaxNumMergeCand uint8
	SliceQPDelta             int8
	SliceCbQPOffset          int8
	SliceCrQPOffset          int8
	SliceActYQPOffset        int8
	SliceActCbQPOffset       int8
	SliceActCrQPOffset       int8
	SliceBetaOffsetDiv2      int8
	SliceTcOffsetDiv2        int8
	PicStruct                uint8
	Reserved0                [3]uint8
	SliceSegmentAddr         uint32
	RefIdxL0                 [v4l2HEVCDPBEntriesMax]uint8
	RefIdxL1                 [v4l2HEVCDPBEntriesMax]uint8
	ShortTermRefPicSetSize   uint16
	LongTermRefPicSetSize    uint16
	PredWeightTable          v4l2HEVCPredWeightTable
	Reserved1                [2]uint8
	Flags                    uint64
}

// v4l2CtrlHEVCDecodeParams mirrors struct v4l2_ctrl_hevc_decode_params
// (328 bytes).
type v4l2CtrlHEVCDecodeParams struct {
	PicOrderCntVal          int32
	ShortTermRefPicSetSize  uint16
	LongTermRefPicSetSize   uint16
	NumActiveDPBEntries     uint8
	NumPocStCurrBefore      uint8
	NumPocStCurrAfter       uint8
	NumPocLtCurr            uint8
	PocStCurrBefore         [v4l2HEVCDPBEntriesMax]uint8
	PocStCurrAfter          [v4l2HEVCDPBEntriesMax]uint8
	PocLtCurr               [v4l2HEVCDPBEntriesMax]uint8
	NumDeltaPocsOfRefRpsIdx uint8
	Reserved                [3]uint8
	DPB                     [v4l2HEVCDPBEntriesMax]v4l2HEVCDPBEntry
	Flags                   uint64
}

// v4l2CtrlHEVCScalingMatrix mirrors struct v4l2_ctrl_hevc_scaling_matrix
// (1000 bytes).
type v4l2CtrlHEVCScalingMatrix struct {
	ScalingList4x4         [6][16]uint8
	ScalingList8x8         [6][64]uint8
	ScalingList16x16       [6][64]uint8
	ScalingList32x32       [2][64]uint8
	ScalingListDCCoef16x16 [6]uint8
	ScalingListDCCoef32x32 [2]uint8
}
