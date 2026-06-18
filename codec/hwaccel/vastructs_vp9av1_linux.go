//go:build linux

// VA-API VP9 and AV1 codec parameter-buffer structs, mirrored 1:1 from
// va/va_dec_vp9.h, va/va_dec_av1.h, va/va_enc_vp9.h and va/va_enc_av1.h for
// VA-API 1.22 (libva 2.22). As with the H.264/H.265 layouts in
// vastructs_linux.go, C bitfield unions are represented by their backing
// `value` word (a uint16/uint32/uint8 to match the union's storage type) and
// assembled with shifts in the decode/encode files; fixed arrays keep the
// header's exact extents and the trailing va_reserved padding is preserved so
// sizeof matches the driver's view byte-for-byte.
//
// Every layout in this file is verified against the on-box headers by an
// offsetof/sizeof C oracle (see vastructs_vp9av1_layout_linux_test.go), which
// asserts unsafe.Sizeof / unsafe.Offsetof equal the values the real libva
// 2.22.0 headers produce on the Arc. A silent mismatch corrupts the driver's
// view of the picture, so these layouts are not to be "tidied".
//
// VA_PADDING_LOW = 4, VA_PADDING_MEDIUM = 8, VA_PADDING_HIGH = 16 uint32s.

package hwaccel

import "unsafe"

// ---- VP9 decode (va/va_dec_vp9.h) ------------------------------------

// vaDecPictureParameterBufferVP9 mirrors VADecPictureParameterBufferVP9.
// pic_fields is a 32-bit bitfield union backed by PicFields. sizeof == 92.
type vaDecPictureParameterBufferVP9 struct {
	FrameWidth              uint16
	FrameHeight             uint16
	ReferenceFrames         [8]uint32 // VASurfaceID[8]
	PicFields               uint32    // pic_fields union (value word)
	FilterLevel             uint8
	SharpnessLevel          uint8
	Log2TileRows            uint8
	Log2TileColumns         uint8
	FrameHeaderLengthInByte uint8
	_                       uint8 // align uint16 first_partition_size
	FirstPartitionSize      uint16
	MbSegmentTreeProbs      [7]uint8
	SegmentPredProbs        [3]uint8
	Profile                 uint8
	BitDepth                uint8
	vaReserved              [8]uint32 // VA_PADDING_MEDIUM
}

// vaSegmentParameterVP9 mirrors VASegmentParameterVP9. segment_flags is a
// 16-bit bitfield union backed by SegmentFlags. sizeof == 36.
type vaSegmentParameterVP9 struct {
	SegmentFlags       uint16 // segment_flags union (value word)
	FilterLevel        [4][2]uint8
	LumaACQuantScale   int16
	LumaDCQuantScale   int16
	ChromaACQuantScale int16
	ChromaDCQuantScale int16
	vaReserved         [4]uint32 // VA_PADDING_LOW
}

// vaSliceParameterBufferVP9 mirrors VASliceParameterBufferVP9. sizeof == 316.
type vaSliceParameterBufferVP9 struct {
	SliceDataSize   uint32
	SliceDataOffset uint32
	SliceDataFlag   uint32
	SegParam        [8]vaSegmentParameterVP9
	vaReserved      [4]uint32 // VA_PADDING_LOW
}

// ---- AV1 decode (va/va_dec_av1.h) ------------------------------------

// vaSegmentationStructAV1 mirrors VASegmentationStructAV1. sizeof == 156.
type vaSegmentationStructAV1 struct {
	SegmentInfoFields uint32      // segment_info_fields union (value word)
	FeatureData       [8][8]int16 // feature_data[VA_AV1_MAX_SEGMENTS][VA_AV1_SEG_LVL_MAX]
	FeatureMask       [8]uint8
	vaReserved        [4]uint32 // VA_PADDING_LOW
}

// vaFilmGrainStructAV1 mirrors VAFilmGrainStructAV1. sizeof == 176.
type vaFilmGrainStructAV1 struct {
	FilmGrainInfoFields uint32 // film_grain_info_fields union (value word)
	GrainSeed           uint16
	NumYPoints          uint8
	PointYValue         [14]uint8
	PointYScaling       [14]uint8
	NumCbPoints         uint8
	PointCbValue        [10]uint8
	PointCbScaling      [10]uint8
	NumCrPoints         uint8
	PointCrValue        [10]uint8
	PointCrScaling      [10]uint8
	ArCoeffsY           [24]int8
	ArCoeffsCb          [25]int8
	ArCoeffsCr          [25]int8
	CbMult              uint8
	CbLumaMult          uint8
	CbOffset            uint16
	CrMult              uint8
	CrLumaMult          uint8
	CrOffset            uint16
	vaReserved          [4]uint32 // VA_PADDING_LOW
}

// vaWarpedMotionParamsAV1 mirrors VAWarpedMotionParamsAV1. wmtype is the
// VAAV1TransformationType enum (int, 4 bytes). sizeof == 56.
type vaWarpedMotionParamsAV1 struct {
	WMType     uint32 // VAAV1TransformationType
	WMMat      [8]int32
	Invalid    uint8
	_          [3]uint8  // align uint32 reserved
	vaReserved [4]uint32 // VA_PADDING_LOW
}

// vaDecPictureParameterBufferAV1 mirrors VADecPictureParameterBufferAV1.
// AnchorFramesList is a host pointer (VASurfaceID*, 8 bytes). The nested
// seg_info / film_grain_info structs are laid out inline. sizeof == 1160.
type vaDecPictureParameterBufferAV1 struct {
	Profile             uint8
	OrderHintBitsMinus1 uint8
	BitDepthIdx         uint8
	MatrixCoefficients  uint8

	SeqInfoFields uint32 // seq_info_fields union (value word)

	CurrentFrame          uint32 // VASurfaceID
	CurrentDisplayPicture uint32 // VASurfaceID
	AnchorFramesNum       uint8
	_                     [7]uint8 // align pointer (8-byte)
	AnchorFramesList      uintptr  // VASurfaceID*

	FrameWidthMinus1               uint16
	FrameHeightMinus1              uint16
	OutputFrameWidthInTilesMinus1  uint16
	OutputFrameHeightInTilesMinus1 uint16
	RefFrameMap                    [8]uint32 // VASurfaceID[8]
	RefFrameIdx                    [7]uint8
	PrimaryRefFrame                uint8
	OrderHint                      uint8
	_                              [3]uint8 // align nested seg_info (4-aligned)
	SegInfo                        vaSegmentationStructAV1
	FilmGrainInfo                  vaFilmGrainStructAV1
	TileCols                       uint8
	TileRows                       uint8
	WidthInSbsMinus1               [63]uint16
	HeightInSbsMinus1              [63]uint16
	TileCountMinus1                uint16
	ContextUpdateTileID            uint16

	PicInfoFields uint32 // pic_info_fields union (value word)

	SuperresScaleDenominator uint8
	InterpFilter             uint8
	FilterLevel              [2]uint8
	FilterLevelU             uint8
	FilterLevelV             uint8

	LoopFilterInfoFields uint8 // loop_filter_info_fields union (value byte)

	RefDeltas  [8]int8
	ModeDeltas [2]int8
	BaseQindex uint8
	YDCDeltaQ  int8
	UDCDeltaQ  int8
	UACDeltaQ  int8
	VDCDeltaQ  int8
	VACDeltaQ  int8

	QMatrixFields uint16 // qmatrix_fields union (value word)
	_             uint16 // align mode_control_fields (uint32)

	ModeControlFields uint32 // mode_control_fields union (value word)

	CdefDampingMinus3 uint8
	CdefBits          uint8
	CdefYStrengths    [8]uint8
	CdefUVStrengths   [8]uint8

	LoopRestorationFields uint16 // loop_restoration_fields union (value word)

	WM         [7]vaWarpedMotionParamsAV1
	vaReserved [8]uint32 // VA_PADDING_MEDIUM
}

// vaSliceParameterBufferAV1 mirrors VASliceParameterBufferAV1 (one tile).
// sizeof == 40.
type vaSliceParameterBufferAV1 struct {
	SliceDataSize     uint32
	SliceDataOffset   uint32
	SliceDataFlag     uint32
	TileRow           uint16
	TileColumn        uint16
	TgStart           uint16 // va_deprecated
	TgEnd             uint16 // va_deprecated
	AnchorFrameIdx    uint8
	_                 uint8 // align uint16 tile_idx_in_tile_list
	TileIdxInTileList uint16
	vaReserved        [4]uint32 // VA_PADDING_LOW
}

// ---- VP9 encode (va/va_enc_vp9.h) ------------------------------------

// vaEncSequenceParameterBufferVP9 mirrors VAEncSequenceParameterBufferVP9.
// sizeof == 44.
type vaEncSequenceParameterBufferVP9 struct {
	MaxFrameWidth  uint32
	MaxFrameHeight uint32
	KfAuto         uint32
	KfMinDist      uint32
	KfMaxDist      uint32
	BitsPerSecond  uint32
	IntraPeriod    uint32
	vaReserved     [4]uint32 // VA_PADDING_LOW
}

// vaEncPictureParameterBufferVP9 mirrors VAEncPictureParameterBufferVP9.
// ref_flags and pic_flags are 32-bit bitfield unions backed by their value
// words. sizeof == 132.
type vaEncPictureParameterBufferVP9 struct {
	FrameWidthSrc      uint32
	FrameHeightSrc     uint32
	FrameWidthDst      uint32
	FrameHeightDst     uint32
	ReconstructedFrame uint32    // VASurfaceID
	ReferenceFrames    [8]uint32 // VASurfaceID[8]
	CodedBuf           uint32    // VABufferID

	RefFlags uint32 // ref_flags union (value word)
	PicFlags uint32 // pic_flags union (value word)

	RefreshFrameFlags   uint8
	LumaACQindex        uint8
	LumaDCQindexDelta   int8
	ChromaACQindexDelta int8
	ChromaDCQindexDelta int8
	FilterLevel         uint8
	SharpnessLevel      uint8
	RefLFDelta          [4]int8
	ModeLFDelta         [2]int8
	_                   uint8 // align uint16 bit_offset_ref_lf_delta

	BitOffsetRefLFDelta         uint16
	BitOffsetModeLFDelta        uint16
	BitOffsetLFLevel            uint16
	BitOffsetQindex             uint16
	BitOffsetFirstPartitionSize uint16
	BitOffsetSegmentation       uint16
	BitSizeSegmentation         uint16

	Log2TileRows     uint8
	Log2TileColumns  uint8
	SkipFrameFlag    uint8
	NumberSkipFrames uint8
	SkipFramesSize   uint32

	vaReserved [8]uint32 // VA_PADDING_MEDIUM
}

// vaEncSegParamVP9 mirrors VAEncSegParamVP9. seg_flags is an 8-bit bitfield
// union backed by SegFlags. sizeof == 20.
type vaEncSegParamVP9 struct {
	SegFlags            uint8 // seg_flags union (value byte)
	SegmentLFLevelDelta int8
	SegmentQindexDelta  int16
	vaReserved          [4]uint32 // VA_PADDING_LOW
}

// vaEncMiscParameterTypeVP9PerSegmantParam mirrors the like-named struct.
// sizeof == 176.
type vaEncMiscParameterTypeVP9PerSegmantParam struct {
	SegData    [8]vaEncSegParamVP9
	vaReserved [4]uint32 // VA_PADDING_LOW
}

// ---- AV1 encode (va/va_enc_av1.h) ------------------------------------

// vaEncSequenceParameterBufferAV1 mirrors VAEncSequenceParameterBufferAV1.
// seq_fields is a 32-bit bitfield union. sizeof == 88.
type vaEncSequenceParameterBufferAV1 struct {
	SeqProfile       uint8
	SeqLevelIdx      uint8
	SeqTier          uint8
	HierarchicalFlag uint8
	IntraPeriod      uint32
	IpPeriod         uint32
	BitsPerSecond    uint32

	SeqFields uint32 // seq_fields union (value word)

	OrderHintBitsMinus1 uint8
	_                   [3]uint8   // align va_reserved
	vaReserved          [16]uint32 // VA_PADDING_HIGH
}

// vaEncSegParamAV1 mirrors VAEncSegParamAV1. seg_flags is an 8-bit bitfield
// union. sizeof == 156.
type vaEncSegParamAV1 struct {
	SegFlags      uint8 // seg_flags union (value byte)
	SegmentNumber uint8
	_             [2]uint8    // align int16 feature_data
	FeatureData   [8][8]int16 // feature_data[VA_AV1_MAX_SEGMENTS][VA_AV1_SEG_LVL_MAX]
	FeatureMask   [8]uint8
	vaReserved    [4]uint32 // VA_PADDING_LOW
}

// vaEncWarpedMotionParamsAV1 mirrors VAEncWarpedMotionParamsAV1. sizeof == 56.
type vaEncWarpedMotionParamsAV1 struct {
	WMType     uint32 // VAEncTransformationTypeAV1
	WMMat      [8]int32
	Invalid    uint8
	_          [3]uint8
	vaReserved [4]uint32 // VA_PADDING_LOW
}

// vaEncPictureParameterBufferAV1 mirrors VAEncPictureParameterBufferAV1.
// ref_frame_ctrl_l0/l1 are VARefFrameCtrlAV1 (32-bit union value words);
// picture_flags / loop_filter_flags / qmatrix_flags / mode_control_flags /
// loop_restoration_flags / tile_group_obu_hdr_info are bitfield unions backed
// by their value words. The nested seg / wm structs are inline. sizeof == 1032.
type vaEncPictureParameterBufferAV1 struct {
	FrameWidthMinus1   uint16
	FrameHeightMinus1  uint16
	ReconstructedFrame uint32    // VASurfaceID
	CodedBuf           uint32    // VABufferID
	ReferenceFrames    [8]uint32 // VASurfaceID[8]
	RefFrameIdx        [7]uint8

	HierarchicalLevelPlus1 uint8
	PrimaryRefFrame        uint8
	OrderHint              uint8
	RefreshFrameFlags      uint8
	Reserved8bits1         uint8

	RefFrameCtrlL0 uint32 // VARefFrameCtrlAV1 (value word)
	RefFrameCtrlL1 uint32 // VARefFrameCtrlAV1 (value word)

	PictureFlags uint32 // picture_flags union (value word)

	SegIDBlockSize      uint8
	NumTileGroupsMinus1 uint8
	TemporalID          uint8
	FilterLevel         [2]uint8
	FilterLevelU        uint8
	FilterLevelV        uint8

	LoopFilterFlags uint8 // loop_filter_flags union (value byte)

	SuperresScaleDenominator uint8
	InterpolationFilter      uint8
	RefDeltas                [8]int8
	ModeDeltas               [2]int8
	BaseQindex               uint8
	YDCDeltaQ                int8
	UDCDeltaQ                int8
	UACDeltaQ                int8
	VDCDeltaQ                int8
	VACDeltaQ                int8
	MinBaseQindex            uint8
	MaxBaseQindex            uint8

	QMatrixFlags    uint16 // qmatrix_flags union (value word)
	Reserved16bits1 uint16

	ModeControlFlags uint32 // mode_control_flags union (value word)

	Segments vaEncSegParamAV1

	TileCols            uint8
	TileRows            uint8
	Reserved16bits2     uint16
	WidthInSbsMinus1    [63]uint16
	HeightInSbsMinus1   [63]uint16
	ContextUpdateTileID uint16

	CdefDampingMinus3 uint8
	CdefBits          uint8
	CdefYStrengths    [8]uint8
	CdefUVStrengths   [8]uint8

	LoopRestorationFlags uint16 // loop_restoration_flags union (value word)

	WM [7]vaEncWarpedMotionParamsAV1

	BitOffsetQindex           uint32
	BitOffsetSegmentation     uint32
	BitOffsetLoopfilterParams uint32
	BitOffsetCdefParams       uint32
	SizeInBitsCdefParams      uint32
	ByteOffsetFrameHdrObuSize uint32
	SizeInBitsFrameHdrObu     uint32

	TileGroupObuHdrInfo uint8 // tile_group_obu_hdr_info union (value byte)

	NumberSkipFrames      uint8
	Reserved16bits3       uint16
	SkipFramesReducedSize int32

	vaReserved [16]uint32 // VA_PADDING_HIGH
}

// vaEncTileGroupBufferAV1 mirrors VAEncTileGroupBufferAV1. sizeof == 20.
type vaEncTileGroupBufferAV1 struct {
	TgStart    uint8
	TgEnd      uint8
	_          [2]uint8  // align va_reserved
	vaReserved [4]uint32 // VA_PADDING_LOW
}

// compile-time anchor: keep unsafe imported for the layout test's use even if
// future edits drop the only direct reference here.
var _ = unsafe.Sizeof(vaDecPictureParameterBufferVP9{})
