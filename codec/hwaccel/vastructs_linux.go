//go:build linux

// The VA-API codec parameter-buffer structs, mirrored 1:1 from
// va/va.h, va/va_dec_hevc.h, va/va_enc_h264.h and va/va_enc_hevc.h for
// VA-API 1.22 (libva 2.22). C bitfield unions are represented by their
// backing uint32 `value` word (assembled with shifts in the decode/encode
// files); fixed arrays keep the header's exact extents and the trailing
// va_reserved padding is preserved so sizeof matches the driver's view.
//
// VA_PADDING_LOW = 4, VA_PADDING_MEDIUM = 8, VA_PADDING_HIGH = 16 uint32s.

package hwaccel

import "syscall"

// openRenderNodeSyscall opens the DRM render node O_RDWR|O_CLOEXEC.
func openRenderNodeSyscall(path string) (int, error) {
	return syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
}

// closeFD closes a file descriptor, ignoring the error (best-effort
// teardown on a failed-construction or Close path).
func closeFD(fd int) { _ = syscall.Close(fd) }

// ---- H.264 decode (va/va.h) ------------------------------------------

// vaPictureH264 mirrors VAPictureH264.
type vaPictureH264 struct {
	PictureID           uint32
	FrameIdx            uint32
	Flags               uint32
	TopFieldOrderCnt    int32
	BottomFieldOrderCnt int32
	vaReserved          [4]uint32
}

// vaPictureParameterBufferH264 mirrors VAPictureParameterBufferH264.
type vaPictureParameterBufferH264 struct {
	CurrPic                    vaPictureH264
	ReferenceFrames            [16]vaPictureH264
	PictureWidthInMbsMinus1    uint16
	PictureHeightInMbsMinus1   uint16
	BitDepthLumaMinus8         uint8
	BitDepthChromaMinus8       uint8
	NumRefFrames               uint8
	_                          uint8 // align seq_fields to 4 bytes
	SeqFields                  uint32
	NumSliceGroupsMinus1       uint8
	SliceGroupMapType          uint8
	SliceGroupChangeRateMinus1 uint16
	PicInitQPMinus26           int8
	PicInitQSMinus26           int8
	ChromaQPIndexOffset        int8
	SecondChromaQPIndexOffset  int8
	PicFields                  uint32
	FrameNum                   uint16
	_                          [2]uint8  // align reserved
	vaReserved                 [8]uint32 // VA_PADDING_MEDIUM
}

// vaIQMatrixBufferH264 mirrors VAIQMatrixBufferH264.
type vaIQMatrixBufferH264 struct {
	ScalingList4x4 [6][16]uint8
	ScalingList8x8 [2][64]uint8
	vaReserved     [4]uint32
}

// vaSliceParameterBufferH264 mirrors VASliceParameterBufferH264.
type vaSliceParameterBufferH264 struct {
	SliceDataSize              uint32
	SliceDataOffset            uint32
	SliceDataFlag              uint32
	SliceDataBitOffset         uint16
	FirstMbInSlice             uint16
	SliceType                  uint8
	DirectSpatialMvPredFlag    uint8
	NumRefIdxL0ActiveMinus1    uint8
	NumRefIdxL1ActiveMinus1    uint8
	CabacInitIdc               uint8
	SliceQPDelta               int8
	DisableDeblockingFilterIdc uint8
	SliceAlphaC0OffsetDiv2     int8
	SliceBetaOffsetDiv2        int8
	_                          uint8 // align RefPicList0 (vaPictureH264 is 4-aligned)
	RefPicList0                [32]vaPictureH264
	RefPicList1                [32]vaPictureH264
	LumaLog2WeightDenom        uint8
	ChromaLog2WeightDenom      uint8
	LumaWeightL0Flag           uint8
	_                          uint8 // align int16 array
	LumaWeightL0               [32]int16
	LumaOffsetL0               [32]int16
	ChromaWeightL0Flag         uint8
	_                          uint8
	ChromaWeightL0             [32][2]int16
	ChromaOffsetL0             [32][2]int16
	LumaWeightL1Flag           uint8
	_                          uint8
	LumaWeightL1               [32]int16
	LumaOffsetL1               [32]int16
	ChromaWeightL1Flag         uint8
	_                          uint8
	ChromaWeightL1             [32][2]int16
	ChromaOffsetL1             [32][2]int16
	vaReserved                 [4]uint32
}

// ---- HEVC decode (va/va.h + va/va_dec_hevc.h) ------------------------

// vaPictureHEVC mirrors VAPictureHEVC.
type vaPictureHEVC struct {
	PictureID   uint32
	PicOrderCnt int32
	Flags       uint32
	vaReserved  [4]uint32
}

// vaPictureParameterBufferHEVC mirrors VAPictureParameterBufferHEVC.
type vaPictureParameterBufferHEVC struct {
	CurrPic                              vaPictureHEVC
	ReferenceFrames                      [15]vaPictureHEVC
	PicWidthInLumaSamples                uint16
	PicHeightInLumaSamples               uint16
	PicFields                            uint32
	SpsMaxDecPicBufferingMinus1          uint8
	BitDepthLumaMinus8                   uint8
	BitDepthChromaMinus8                 uint8
	PcmSampleBitDepthLumaMinus1          uint8
	PcmSampleBitDepthChromaMinus1        uint8
	Log2MinLumaCodingBlockSizeMinus3     uint8
	Log2DiffMaxMinLumaCodingBlockSize    uint8
	Log2MinTransformBlockSizeMinus2      uint8
	Log2DiffMaxMinTransformBlockSize     uint8
	Log2MinPcmLumaCodingBlockSizeMinus3  uint8
	Log2DiffMaxMinPcmLumaCodingBlockSize uint8
	MaxTransformHierarchyDepthIntra      uint8
	MaxTransformHierarchyDepthInter      uint8
	InitQPMinus26                        int8
	DiffCuQPDeltaDepth                   uint8
	PpsCbQPOffset                        int8
	PpsCrQPOffset                        int8
	Log2ParallelMergeLevelMinus2         uint8
	NumTileColumnsMinus1                 uint8
	NumTileRowsMinus1                    uint8
	ColumnWidthMinus1                    [19]uint16
	RowHeightMinus1                      [21]uint16
	SliceParsingFields                   uint32
	Log2MaxPicOrderCntLsbMinus4          uint8
	NumShortTermRefPicSets               uint8
	NumLongTermRefPicSps                 uint8
	NumRefIdxL0DefaultActiveMinus1       uint8
	NumRefIdxL1DefaultActiveMinus1       uint8
	PpsBetaOffsetDiv2                    int8
	PpsTcOffsetDiv2                      int8
	NumExtraSliceHeaderBits              uint8
	StRpsBits                            uint32
	vaReserved                           [8]uint32 // VA_PADDING_MEDIUM
}

// vaSliceParameterBufferHEVC mirrors VASliceParameterBufferHEVC.
// Note the trailing reserved is VA_PADDING_LOW - 2 = 2 uint32s.
type vaSliceParameterBufferHEVC struct {
	SliceDataSize              uint32
	SliceDataOffset            uint32
	SliceDataFlag              uint32
	SliceDataByteOffset        uint32
	SliceSegmentAddress        uint32
	RefPicList                 [2][15]uint8
	LongSliceFlags             uint32
	CollocatedRefIdx           uint8
	NumRefIdxL0ActiveMinus1    uint8
	NumRefIdxL1ActiveMinus1    uint8
	SliceQPDelta               int8
	SliceCbQPOffset            int8
	SliceCrQPOffset            int8
	SliceBetaOffsetDiv2        int8
	SliceTcOffsetDiv2          int8
	LumaLog2WeightDenom        uint8
	DeltaChromaLog2WeightDenom int8
	DeltaLumaWeightL0          [15]int8
	LumaOffsetL0               [15]int8
	DeltaChromaWeightL0        [15][2]int8
	ChromaOffsetL0             [15][2]int8
	DeltaLumaWeightL1          [15]int8
	LumaOffsetL1               [15]int8
	DeltaChromaWeightL1        [15][2]int8
	ChromaOffsetL1             [15][2]int8
	FiveMinusMaxNumMergeCand   uint8
	_                          uint8 // align uint16 below
	NumEntryPointOffsets       uint16
	EntryOffsetToSubsetArray   uint16
	SliceDataNumEmuPrevnBytes  uint16
	vaReserved                 [2]uint32 // VA_PADDING_LOW - 2
}

// ---- H.264 encode (va/va_enc_h264.h) ---------------------------------

// vaEncSequenceParameterBufferH264 mirrors VAEncSequenceParameterBufferH264.
type vaEncSequenceParameterBufferH264 struct {
	SeqParameterSetID              uint8
	LevelIdc                       uint8
	_                              [2]uint8 // align intra_period (uint32)
	IntraPeriod                    uint32
	IntraIdrPeriod                 uint32
	IpPeriod                       uint32
	BitsPerSecond                  uint32
	MaxNumRefFrames                uint32
	PictureWidthInMbs              uint16
	PictureHeightInMbs             uint16
	SeqFields                      uint32
	BitDepthLumaMinus8             uint8
	BitDepthChromaMinus8           uint8
	NumRefFramesInPicOrderCntCycle uint8
	_                              uint8 // align int32 below
	OffsetForNonRefPic             int32
	OffsetForTopToBottomField      int32
	OffsetForRefFrame              [256]int32
	FrameCroppingFlag              uint8
	_                              [3]uint8 // align uint32
	FrameCropLeftOffset            uint32
	FrameCropRightOffset           uint32
	FrameCropTopOffset             uint32
	FrameCropBottomOffset          uint32
	VuiParametersPresentFlag       uint8
	_                              [3]uint8 // align vui_fields (uint32)
	VuiFields                      uint32
	AspectRatioIdc                 uint8
	_                              [3]uint8 // align sar_width
	SarWidth                       uint32
	SarHeight                      uint32
	NumUnitsInTick                 uint32
	TimeScale                      uint32
	vaReserved                     [4]uint32
}

// vaEncPictureParameterBufferH264 mirrors VAEncPictureParameterBufferH264.
type vaEncPictureParameterBufferH264 struct {
	CurrPic                   vaPictureH264
	ReferenceFrames           [16]vaPictureH264
	CodedBuf                  uint32
	PicParameterSetID         uint8
	SeqParameterSetID         uint8
	LastPicture               uint8
	_                         uint8 // align frame_num (uint16)
	FrameNum                  uint16
	PicInitQP                 uint8
	NumRefIdxL0ActiveMinus1   uint8
	NumRefIdxL1ActiveMinus1   uint8
	ChromaQPIndexOffset       int8
	SecondChromaQPIndexOffset int8
	_                         uint8 // align pic_fields (uint32)
	PicFields                 uint32
	vaReserved                [4]uint32
}

// vaEncSliceParameterBufferH264 mirrors VAEncSliceParameterBufferH264.
type vaEncSliceParameterBufferH264 struct {
	MacroblockAddress           uint32
	NumMacroblocks              uint32
	MacroblockInfo              uint32
	SliceType                   uint8
	PicParameterSetID           uint8
	IdrPicID                    uint16
	PicOrderCntLsb              uint16
	_                           uint16 // align delta_pic_order_cnt_bottom (int32)
	DeltaPicOrderCntBottom      int32
	DeltaPicOrderCnt            [2]int32
	DirectSpatialMvPredFlag     uint8
	NumRefIdxActiveOverrideFlag uint8
	NumRefIdxL0ActiveMinus1     uint8
	NumRefIdxL1ActiveMinus1     uint8
	RefPicList0                 [32]vaPictureH264
	RefPicList1                 [32]vaPictureH264
	LumaLog2WeightDenom         uint8
	ChromaLog2WeightDenom       uint8
	LumaWeightL0Flag            uint8
	_                           uint8 // align int16
	LumaWeightL0                [32]int16
	LumaOffsetL0                [32]int16
	ChromaWeightL0Flag          uint8
	_                           uint8
	ChromaWeightL0              [32][2]int16
	ChromaOffsetL0              [32][2]int16
	LumaWeightL1Flag            uint8
	_                           uint8
	LumaWeightL1                [32]int16
	LumaOffsetL1                [32]int16
	ChromaWeightL1Flag          uint8
	_                           uint8
	ChromaWeightL1              [32][2]int16
	ChromaOffsetL1              [32][2]int16
	CabacInitIdc                uint8
	SliceQPDelta                int8
	DisableDeblockingFilterIdc  uint8
	SliceAlphaC0OffsetDiv2      int8
	SliceBetaOffsetDiv2         int8
	_                           uint8 // align reserved (uint32)
	vaReserved                  [4]uint32
}

// ---- HEVC encode (va/va_enc_hevc.h) ----------------------------------

// vaEncSequenceParameterBufferHEVC mirrors VAEncSequenceParameterBufferHEVC.
type vaEncSequenceParameterBufferHEVC struct {
	GeneralProfileIdc                   uint8
	GeneralLevelIdc                     uint8
	GeneralTierFlag                     uint8
	_                                   uint8 // align intra_period
	IntraPeriod                         uint32
	IntraIdrPeriod                      uint32
	IpPeriod                            uint32
	BitsPerSecond                       uint32
	PicWidthInLumaSamples               uint16
	PicHeightInLumaSamples              uint16
	SeqFields                           uint32
	Log2MinLumaCodingBlockSizeMinus3    uint8
	Log2DiffMaxMinLumaCodingBlockSize   uint8
	Log2MinTransformBlockSizeMinus2     uint8
	Log2DiffMaxMinTransformBlockSize    uint8
	MaxTransformHierarchyDepthInter     uint8
	MaxTransformHierarchyDepthIntra     uint8
	_                                   [2]uint8 // align uint32 below
	PcmSampleBitDepthLumaMinus1         uint32
	PcmSampleBitDepthChromaMinus1       uint32
	Log2MinPcmLumaCodingBlockSizeMinus3 uint32
	Log2MaxPcmLumaCodingBlockSizeMinus3 uint32
	VuiParametersPresentFlag            uint8
	_                                   [3]uint8 // align vui_fields
	VuiFields                           uint32
	AspectRatioIdc                      uint8
	_                                   [3]uint8 // align sar_width
	SarWidth                            uint32
	SarHeight                           uint32
	VuiNumUnitsInTick                   uint32
	VuiTimeScale                        uint32
	MinSpatialSegmentationIdc           uint16
	MaxBytesPerPicDenom                 uint8
	MaxBitsPerMinCuDenom                uint8
	SccFields                           uint32
	vaReserved                          [7]uint32 // VA_PADDING_MEDIUM - 1
}

// vaEncPictureParameterBufferHEVC mirrors VAEncPictureParameterBufferHEVC.
type vaEncPictureParameterBufferHEVC struct {
	DecodedCurrPic                 vaPictureHEVC
	ReferenceFrames                [15]vaPictureHEVC
	CodedBuf                       uint32
	CollocatedRefPicIndex          uint8
	LastPicture                    uint8
	PicInitQP                      uint8
	DiffCuQPDeltaDepth             uint8
	PpsCbQPOffset                  int8
	PpsCrQPOffset                  int8
	NumTileColumnsMinus1           uint8
	NumTileRowsMinus1              uint8
	ColumnWidthMinus1              [19]uint8
	RowHeightMinus1                [21]uint8
	Log2ParallelMergeLevelMinus2   uint8
	CtuMaxBitsizeAllowed           uint8
	NumRefIdxL0DefaultActiveMinus1 uint8
	NumRefIdxL1DefaultActiveMinus1 uint8
	SlicePicParameterSetID         uint8
	NalUnitType                    uint8
	_                              uint8 // align pic_fields (uint32): after 2 uint8 above -> need 1 to reach 4-align
	PicFields                      uint32
	HierarchicalLevelPlus1         uint8
	VaByteReserved                 uint8
	SccFields                      uint16     // 16-bit union
	vaReserved                     [15]uint32 // VA_PADDING_HIGH - 1
}

// vaEncSliceParameterBufferHEVC mirrors VAEncSliceParameterBufferHEVC.
type vaEncSliceParameterBufferHEVC struct {
	SliceSegmentAddress        uint32
	NumCtuInSlice              uint32
	SliceType                  uint8
	SlicePicParameterSetID     uint8
	NumRefIdxL0ActiveMinus1    uint8
	NumRefIdxL1ActiveMinus1    uint8
	RefPicList0                [15]vaPictureHEVC
	RefPicList1                [15]vaPictureHEVC
	LumaLog2WeightDenom        uint8
	DeltaChromaLog2WeightDenom int8
	DeltaLumaWeightL0          [15]int8
	LumaOffsetL0               [15]int8
	DeltaChromaWeightL0        [15][2]int8
	ChromaOffsetL0             [15][2]int8
	DeltaLumaWeightL1          [15]int8
	LumaOffsetL1               [15]int8
	DeltaChromaWeightL1        [15][2]int8
	ChromaOffsetL1             [15][2]int8
	MaxNumMergeCand            uint8
	SliceQPDelta               int8
	SliceCbQPOffset            int8
	SliceCrQPOffset            int8
	SliceBetaOffsetDiv2        int8
	SliceTcOffsetDiv2          int8
	SliceFields                uint32
	PredWeightTableBitOffset   uint32
	PredWeightTableBitLength   uint32
	vaReserved                 [6]uint32 // VA_PADDING_MEDIUM - 2
}
