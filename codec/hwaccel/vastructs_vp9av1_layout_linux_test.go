//go:build linux

package hwaccel

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
)

// TestVAVP9AV1StructLayout pins the Go mirrors of the VP9/AV1 VA-API
// parameter buffers to the byte-exact sizes and field offsets produced by the
// real libva 2.22.0 headers (va_dec_vp9.h, va_dec_av1.h, va_enc_vp9.h,
// va_enc_av1.h) on the Arc box. The expected numbers below are the output of
// an offsetof/sizeof C oracle compiled against those headers; a mismatch here
// means the struct would silently corrupt the driver's view of the picture.
func TestVAVP9AV1StructLayout(t *testing.T) {
	// ---- VP9 decode ----
	assert.EqualValues(t, 92, unsafe.Sizeof(vaDecPictureParameterBufferVP9{}), "VADecPictureParameterBufferVP9 size")
	var dpv9 vaDecPictureParameterBufferVP9
	assert.EqualValues(t, 0, unsafe.Offsetof(dpv9.FrameWidth))
	assert.EqualValues(t, 4, unsafe.Offsetof(dpv9.ReferenceFrames))
	assert.EqualValues(t, 36, unsafe.Offsetof(dpv9.PicFields))
	assert.EqualValues(t, 40, unsafe.Offsetof(dpv9.FilterLevel))
	assert.EqualValues(t, 46, unsafe.Offsetof(dpv9.FirstPartitionSize))
	assert.EqualValues(t, 48, unsafe.Offsetof(dpv9.MbSegmentTreeProbs))
	assert.EqualValues(t, 55, unsafe.Offsetof(dpv9.SegmentPredProbs))
	assert.EqualValues(t, 58, unsafe.Offsetof(dpv9.Profile))
	assert.EqualValues(t, 59, unsafe.Offsetof(dpv9.BitDepth))

	assert.EqualValues(t, 36, unsafe.Sizeof(vaSegmentParameterVP9{}), "VASegmentParameterVP9 size")
	var spv9 vaSegmentParameterVP9
	assert.EqualValues(t, 0, unsafe.Offsetof(spv9.SegmentFlags))
	assert.EqualValues(t, 2, unsafe.Offsetof(spv9.FilterLevel))
	assert.EqualValues(t, 10, unsafe.Offsetof(spv9.LumaACQuantScale))

	assert.EqualValues(t, 316, unsafe.Sizeof(vaSliceParameterBufferVP9{}), "VASliceParameterBufferVP9 size")
	var slv9 vaSliceParameterBufferVP9
	assert.EqualValues(t, 12, unsafe.Offsetof(slv9.SegParam))

	// ---- AV1 decode ----
	assert.EqualValues(t, 156, unsafe.Sizeof(vaSegmentationStructAV1{}), "VASegmentationStructAV1 size")
	var segA vaSegmentationStructAV1
	assert.EqualValues(t, 4, unsafe.Offsetof(segA.FeatureData))
	assert.EqualValues(t, 132, unsafe.Offsetof(segA.FeatureMask))

	assert.EqualValues(t, 176, unsafe.Sizeof(vaFilmGrainStructAV1{}), "VAFilmGrainStructAV1 size")
	assert.EqualValues(t, 56, unsafe.Sizeof(vaWarpedMotionParamsAV1{}), "VAWarpedMotionParamsAV1 size")
	var wmA vaWarpedMotionParamsAV1
	assert.EqualValues(t, 4, unsafe.Offsetof(wmA.WMMat))
	assert.EqualValues(t, 36, unsafe.Offsetof(wmA.Invalid))

	assert.EqualValues(t, 1160, unsafe.Sizeof(vaDecPictureParameterBufferAV1{}), "VADecPictureParameterBufferAV1 size")
	var dpA vaDecPictureParameterBufferAV1
	assert.EqualValues(t, 4, unsafe.Offsetof(dpA.SeqInfoFields))
	assert.EqualValues(t, 8, unsafe.Offsetof(dpA.CurrentFrame))
	assert.EqualValues(t, 16, unsafe.Offsetof(dpA.AnchorFramesNum))
	assert.EqualValues(t, 24, unsafe.Offsetof(dpA.AnchorFramesList))
	assert.EqualValues(t, 32, unsafe.Offsetof(dpA.FrameWidthMinus1))
	assert.EqualValues(t, 40, unsafe.Offsetof(dpA.RefFrameMap))
	assert.EqualValues(t, 72, unsafe.Offsetof(dpA.RefFrameIdx))
	assert.EqualValues(t, 79, unsafe.Offsetof(dpA.PrimaryRefFrame))
	assert.EqualValues(t, 84, unsafe.Offsetof(dpA.SegInfo))
	assert.EqualValues(t, 240, unsafe.Offsetof(dpA.FilmGrainInfo))
	assert.EqualValues(t, 416, unsafe.Offsetof(dpA.TileCols))
	assert.EqualValues(t, 418, unsafe.Offsetof(dpA.WidthInSbsMinus1))
	assert.EqualValues(t, 544, unsafe.Offsetof(dpA.HeightInSbsMinus1))
	assert.EqualValues(t, 670, unsafe.Offsetof(dpA.TileCountMinus1))
	assert.EqualValues(t, 672, unsafe.Offsetof(dpA.ContextUpdateTileID))
	assert.EqualValues(t, 676, unsafe.Offsetof(dpA.PicInfoFields))
	assert.EqualValues(t, 680, unsafe.Offsetof(dpA.SuperresScaleDenominator))
	assert.EqualValues(t, 682, unsafe.Offsetof(dpA.FilterLevel))
	assert.EqualValues(t, 686, unsafe.Offsetof(dpA.LoopFilterInfoFields))
	assert.EqualValues(t, 687, unsafe.Offsetof(dpA.RefDeltas))
	assert.EqualValues(t, 695, unsafe.Offsetof(dpA.ModeDeltas))
	assert.EqualValues(t, 697, unsafe.Offsetof(dpA.BaseQindex))
	assert.EqualValues(t, 704, unsafe.Offsetof(dpA.QMatrixFields))
	assert.EqualValues(t, 708, unsafe.Offsetof(dpA.ModeControlFields))
	assert.EqualValues(t, 712, unsafe.Offsetof(dpA.CdefDampingMinus3))
	assert.EqualValues(t, 714, unsafe.Offsetof(dpA.CdefYStrengths))
	assert.EqualValues(t, 730, unsafe.Offsetof(dpA.LoopRestorationFields))
	assert.EqualValues(t, 732, unsafe.Offsetof(dpA.WM))

	assert.EqualValues(t, 40, unsafe.Sizeof(vaSliceParameterBufferAV1{}), "VASliceParameterBufferAV1 size")
	var slA vaSliceParameterBufferAV1
	assert.EqualValues(t, 12, unsafe.Offsetof(slA.TileRow))
	assert.EqualValues(t, 20, unsafe.Offsetof(slA.AnchorFrameIdx))
	assert.EqualValues(t, 22, unsafe.Offsetof(slA.TileIdxInTileList))

	// ---- VP9 encode ----
	assert.EqualValues(t, 44, unsafe.Sizeof(vaEncSequenceParameterBufferVP9{}), "VAEncSequenceParameterBufferVP9 size")
	assert.EqualValues(t, 132, unsafe.Sizeof(vaEncPictureParameterBufferVP9{}), "VAEncPictureParameterBufferVP9 size")
	var epv9 vaEncPictureParameterBufferVP9
	assert.EqualValues(t, 16, unsafe.Offsetof(epv9.ReconstructedFrame))
	assert.EqualValues(t, 20, unsafe.Offsetof(epv9.ReferenceFrames))
	assert.EqualValues(t, 52, unsafe.Offsetof(epv9.CodedBuf))
	assert.EqualValues(t, 56, unsafe.Offsetof(epv9.RefFlags))
	assert.EqualValues(t, 60, unsafe.Offsetof(epv9.PicFlags))
	assert.EqualValues(t, 64, unsafe.Offsetof(epv9.RefreshFrameFlags))
	assert.EqualValues(t, 65, unsafe.Offsetof(epv9.LumaACQindex))
	assert.EqualValues(t, 69, unsafe.Offsetof(epv9.FilterLevel))
	assert.EqualValues(t, 71, unsafe.Offsetof(epv9.RefLFDelta))
	assert.EqualValues(t, 78, unsafe.Offsetof(epv9.BitOffsetRefLFDelta))
	assert.EqualValues(t, 92, unsafe.Offsetof(epv9.Log2TileRows))
	assert.EqualValues(t, 94, unsafe.Offsetof(epv9.SkipFrameFlag))
	assert.EqualValues(t, 96, unsafe.Offsetof(epv9.SkipFramesSize))

	assert.EqualValues(t, 20, unsafe.Sizeof(vaEncSegParamVP9{}), "VAEncSegParamVP9 size")
	assert.EqualValues(t, 176, unsafe.Sizeof(vaEncMiscParameterTypeVP9PerSegmantParam{}), "VAEncMiscParameterTypeVP9PerSegmantParam size")

	// ---- AV1 encode ----
	assert.EqualValues(t, 88, unsafe.Sizeof(vaEncSequenceParameterBufferAV1{}), "VAEncSequenceParameterBufferAV1 size")
	var esA vaEncSequenceParameterBufferAV1
	assert.EqualValues(t, 16, unsafe.Offsetof(esA.SeqFields))
	assert.EqualValues(t, 20, unsafe.Offsetof(esA.OrderHintBitsMinus1))

	assert.EqualValues(t, 156, unsafe.Sizeof(vaEncSegParamAV1{}), "VAEncSegParamAV1 size")
	assert.EqualValues(t, 56, unsafe.Sizeof(vaEncWarpedMotionParamsAV1{}), "VAEncWarpedMotionParamsAV1 size")

	assert.EqualValues(t, 1032, unsafe.Sizeof(vaEncPictureParameterBufferAV1{}), "VAEncPictureParameterBufferAV1 size")
	var epA vaEncPictureParameterBufferAV1
	assert.EqualValues(t, 4, unsafe.Offsetof(epA.ReconstructedFrame))
	assert.EqualValues(t, 8, unsafe.Offsetof(epA.CodedBuf))
	assert.EqualValues(t, 12, unsafe.Offsetof(epA.ReferenceFrames))
	assert.EqualValues(t, 44, unsafe.Offsetof(epA.RefFrameIdx))
	assert.EqualValues(t, 56, unsafe.Offsetof(epA.RefFrameCtrlL0))
	assert.EqualValues(t, 64, unsafe.Offsetof(epA.PictureFlags))
	assert.EqualValues(t, 68, unsafe.Offsetof(epA.SegIDBlockSize))
	assert.EqualValues(t, 71, unsafe.Offsetof(epA.FilterLevel))
	assert.EqualValues(t, 75, unsafe.Offsetof(epA.LoopFilterFlags))
	assert.EqualValues(t, 78, unsafe.Offsetof(epA.RefDeltas))
	assert.EqualValues(t, 88, unsafe.Offsetof(epA.BaseQindex))
	assert.EqualValues(t, 94, unsafe.Offsetof(epA.MinBaseQindex))
	assert.EqualValues(t, 96, unsafe.Offsetof(epA.QMatrixFlags))
	assert.EqualValues(t, 100, unsafe.Offsetof(epA.ModeControlFlags))
	assert.EqualValues(t, 104, unsafe.Offsetof(epA.Segments))
	assert.EqualValues(t, 260, unsafe.Offsetof(epA.TileCols))
	assert.EqualValues(t, 264, unsafe.Offsetof(epA.WidthInSbsMinus1))
	assert.EqualValues(t, 516, unsafe.Offsetof(epA.ContextUpdateTileID))
	assert.EqualValues(t, 518, unsafe.Offsetof(epA.CdefDampingMinus3))
	assert.EqualValues(t, 520, unsafe.Offsetof(epA.CdefYStrengths))
	assert.EqualValues(t, 536, unsafe.Offsetof(epA.LoopRestorationFlags))
	assert.EqualValues(t, 540, unsafe.Offsetof(epA.WM))
	assert.EqualValues(t, 932, unsafe.Offsetof(epA.BitOffsetQindex))
	assert.EqualValues(t, 960, unsafe.Offsetof(epA.TileGroupObuHdrInfo))
	assert.EqualValues(t, 961, unsafe.Offsetof(epA.NumberSkipFrames))
	assert.EqualValues(t, 964, unsafe.Offsetof(epA.SkipFramesReducedSize))

	assert.EqualValues(t, 20, unsafe.Sizeof(vaEncTileGroupBufferAV1{}), "VAEncTileGroupBufferAV1 size")
}
