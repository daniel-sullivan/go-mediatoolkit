//go:build linux

// H.265 (HEVC) VLD decode: VPS/SPS/PPS/slice-header parsing and VA
// parameter-buffer construction. The parser reads the subset of the HEVC
// parameter sets and slice_segment_header() needed to fill
// VAPictureParameterBufferHEVC, VAIQMatrixBufferHEVC and
// VASliceParameterBufferHEVC for an IDR I-slice single-segment picture (the
// form produced by the all-intra encoder and typical keyframe streams).

package hwaccel

import (
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// HEVC NAL unit types ((nal[0] >> 1) & 0x3f).
const (
	hevcNalIDRWRADL = 19
	hevcNalIDRNLP   = 20
	hevcNalVPS      = 32
	hevcNalSPS      = 33
	hevcNalPPS      = 34
	hevcNalAUD      = 35
)

// hevcParams bundles the live SPS/PPS the HEVC decoder context was built
// from.
type hevcParams struct {
	sps hevcSPS
	pps hevcPPS
}

// hevcSPS holds the subset of seq_parameter_set_rbsp() the decoder needs.
type hevcSPS struct {
	chromaFormatIDC                 uint32
	separateColourPlane             uint32
	picWidthInLumaSamples           uint32
	picHeightInLumaSamples          uint32
	confWinLeft, confWinRight       uint32
	confWinTop, confWinBottom       uint32
	bitDepthLumaMinus8              uint32
	bitDepthChromaMinus8            uint32
	log2MaxPocLsbMinus4             uint32
	spsMaxDecPicBufferingMinus1     uint32
	log2MinLumaCBMinus3             uint32
	log2DiffMaxMinLumaCB            uint32
	log2MinTransformBlockMinus2     uint32
	log2DiffMaxMinTransformBlock    uint32
	maxTransformHierarchyDepthInter uint32
	maxTransformHierarchyDepthIntra uint32
	scalingListEnabled              uint32
	ampEnabled                      uint32
	saoEnabled                      uint32
	pcmEnabled                      uint32
	pcmSampleBitDepthLumaMinus1     uint32
	pcmSampleBitDepthChromaMinus1   uint32
	log2MinPcmCBMinus3              uint32
	log2DiffMaxMinPcmCB             uint32
	pcmLoopFilterDisabled           uint32
	numShortTermRPS                 uint32
	longTermRefPicsPresent          uint32
	spsTemporalMvpEnabled           uint32
	strongIntraSmoothing            uint32
}

// hevcPPS holds the subset of pic_parameter_set_rbsp() the decoder needs.
type hevcPPS struct {
	dependentSliceSegmentsEnabled   uint32
	outputFlagPresent               uint32
	numExtraSliceHeaderBits         uint32
	signDataHiding                  uint32
	cabacInitPresent                uint32
	numRefIdxL0DefaultMinus1        uint32
	numRefIdxL1DefaultMinus1        uint32
	initQPMinus26                   int32
	constrainedIntraPred            uint32
	transformSkipEnabled            uint32
	cuQPDeltaEnabled                uint32
	diffCuQPDeltaDepth              uint32
	ppsCbQPOffset                   int32
	ppsCrQPOffset                   int32
	ppsSliceChromaQPOffsetsPresent  uint32
	weightedPred                    uint32
	weightedBipred                  uint32
	transquantBypassEnabled         uint32
	tilesEnabled                    uint32
	entropyCodingSyncEnabled        uint32
	ppsLoopFilterAcrossSlices       uint32
	deblockingFilterControlPresent  uint32
	deblockingFilterOverrideEnabled uint32
	ppsDeblockingFilterDisabled     uint32
	ppsBetaOffsetDiv2               int32
	ppsTcOffsetDiv2                 int32
	listsModificationPresent        uint32
	log2ParallelMergeLevelMinus2    uint32
	sliceSegmentHeaderExtPresent    uint32
	scalingListDataPresent          uint32
}

// decodeHEVC parses the access unit's parameter sets and slice, builds the
// VA buffers, decodes, and reads back the frame.
func (d *vaDecoder) decodeHEVC(nals [][]byte) ([]video.Frame, error) {
	var sliceNAL []byte
	var nalType int
	for _, nal := range nals {
		t := int((nal[0] >> 1) & 0x3f)
		switch t {
		case hevcNalVPS:
			// VPS carries no field the param buffers need beyond what SPS
			// repeats; parsing is skipped.
		case hevcNalSPS:
			d.hevc.sps = parseHEVCSPS(ebspToRBSP(nal[2:]))
			d.have = true
		case hevcNalPPS:
			d.hevc.pps = parseHEVCPPS(ebspToRBSP(nal[2:]))
		default:
			if t <= 31 { // VCL NAL (slice)
				sliceNAL = nal
				nalType = t
			}
		}
	}
	if !d.have {
		return nil, ErrParameterSetsMissing
	}
	if sliceNAL == nil {
		return nil, nil
	}

	sps := &d.hevc.sps
	codedW := int(sps.picWidthInLumaSamples)
	codedH := int(sps.picHeightInLumaSamples)
	if codedW <= 0 || codedH <= 0 {
		return nil, ErrBitstreamParse
	}
	if err := d.ensureContext(vaProfileHEVCMain, codedW, codedH); err != nil {
		return nil, err
	}

	var bufs []uint32
	defer func() { d.freeDecodeBufs(bufs) }()

	pic := d.buildHEVCPicParam(nalType)
	if err := d.addDecodeBuf(&bufs, vaPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return nil, err
	}
	if sps.scalingListEnabled == 1 {
		iq := defaultHEVCIQMatrix()
		if err := d.addDecodeBuf(&bufs, vaIQMatrixBufferType, int(unsafe.Sizeof(iq)), unsafe.Pointer(&iq)); err != nil {
			return nil, err
		}
	}
	slice := d.buildHEVCSliceParam(sliceNAL, nalType)
	if err := d.addDecodeBuf(&bufs, vaSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return nil, err
	}
	if err := d.addDecodeBuf(&bufs, vaSliceDataBufferType, len(sliceNAL), unsafe.Pointer(&sliceNAL[0])); err != nil {
		return nil, err
	}

	if err := d.submitPicture(bufs); err != nil {
		return nil, err
	}

	subW, subH := 2, 2 // 4:2:0 conformance-window unit
	visW := codedW - int(sps.confWinLeft+sps.confWinRight)*subW
	visH := codedH - int(sps.confWinTop+sps.confWinBottom)*subH
	f, err := d.readSurface(visW, visH)
	if err != nil {
		return nil, err
	}
	d.frameIdx++
	return []video.Frame{f}, nil
}

// buildHEVCPicParam fills VAPictureParameterBufferHEVC from the live SPS/PPS.
func (d *vaDecoder) buildHEVCPicParam(nalType int) vaPictureParameterBufferHEVC {
	sps := &d.hevc.sps
	pps := &d.hevc.pps

	var pic vaPictureParameterBufferHEVC
	pic.CurrPic = vaPictureHEVC{PictureID: d.surface, PicOrderCnt: 0, Flags: 0}
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaPictureHEVC{PictureID: vaInvalidSurface, Flags: vaPictureHEVCInvalid}
	}
	pic.PicWidthInLumaSamples = uint16(sps.picWidthInLumaSamples)
	pic.PicHeightInLumaSamples = uint16(sps.picHeightInLumaSamples)

	// pic_fields (va_dec_hevc.h): chroma_format_idc(0..1),
	// separate_colour_plane(2), pcm_enabled(3), scaling_list_enabled(4),
	// transform_skip_enabled(5), amp_enabled(6), strong_intra_smoothing(7),
	// sign_data_hiding(8), constrained_intra_pred(9), cu_qp_delta(10),
	// weighted_pred(11), weighted_bipred(12), transquant_bypass(13),
	// tiles_enabled(14), entropy_coding_sync(15),
	// pps_loop_filter_across_slices(16), loop_filter_across_tiles(17),
	// pcm_loop_filter_disabled(18), NoPicReordering(19), NoBiPred(20).
	chroma := sps.chromaFormatIDC
	pic.PicFields = chroma |
		(sps.separateColourPlane << 2) |
		(sps.pcmEnabled << 3) |
		(sps.scalingListEnabled << 4) |
		(pps.transformSkipEnabled << 5) |
		(sps.ampEnabled << 6) |
		(sps.strongIntraSmoothing << 7) |
		(pps.signDataHiding << 8) |
		(pps.constrainedIntraPred << 9) |
		(pps.cuQPDeltaEnabled << 10) |
		(pps.weightedPred << 11) |
		(pps.weightedBipred << 12) |
		(pps.transquantBypassEnabled << 13) |
		(pps.tilesEnabled << 14) |
		(pps.entropyCodingSyncEnabled << 15) |
		(pps.ppsLoopFilterAcrossSlices << 16) |
		(sps.pcmLoopFilterDisabled << 18) |
		(1 << 19) | (1 << 20) // intra-only: no reordering, no bipred

	pic.SpsMaxDecPicBufferingMinus1 = uint8(sps.spsMaxDecPicBufferingMinus1)
	pic.BitDepthLumaMinus8 = uint8(sps.bitDepthLumaMinus8)
	pic.BitDepthChromaMinus8 = uint8(sps.bitDepthChromaMinus8)
	pic.PcmSampleBitDepthLumaMinus1 = uint8(sps.pcmSampleBitDepthLumaMinus1)
	pic.PcmSampleBitDepthChromaMinus1 = uint8(sps.pcmSampleBitDepthChromaMinus1)
	pic.Log2MinLumaCodingBlockSizeMinus3 = uint8(sps.log2MinLumaCBMinus3)
	pic.Log2DiffMaxMinLumaCodingBlockSize = uint8(sps.log2DiffMaxMinLumaCB)
	pic.Log2MinTransformBlockSizeMinus2 = uint8(sps.log2MinTransformBlockMinus2)
	pic.Log2DiffMaxMinTransformBlockSize = uint8(sps.log2DiffMaxMinTransformBlock)
	pic.Log2MinPcmLumaCodingBlockSizeMinus3 = uint8(sps.log2MinPcmCBMinus3)
	pic.Log2DiffMaxMinPcmLumaCodingBlockSize = uint8(sps.log2DiffMaxMinPcmCB)
	pic.MaxTransformHierarchyDepthIntra = uint8(sps.maxTransformHierarchyDepthIntra)
	pic.MaxTransformHierarchyDepthInter = uint8(sps.maxTransformHierarchyDepthInter)
	pic.InitQPMinus26 = int8(pps.initQPMinus26)
	pic.DiffCuQPDeltaDepth = uint8(pps.diffCuQPDeltaDepth)
	pic.PpsCbQPOffset = int8(pps.ppsCbQPOffset)
	pic.PpsCrQPOffset = int8(pps.ppsCrQPOffset)
	pic.Log2ParallelMergeLevelMinus2 = uint8(pps.log2ParallelMergeLevelMinus2)

	// slice_parsing_fields (va_dec_hevc.h): lists_modification_present(0),
	// long_term_ref_pics_present(1), sps_temporal_mvp_enabled(2),
	// cabac_init_present(3), output_flag_present(4),
	// dependent_slice_segments_enabled(5),
	// pps_slice_chroma_qp_offsets_present(6),
	// sample_adaptive_offset_enabled(7),
	// deblocking_filter_override_enabled(8),
	// pps_disable_deblocking_filter(9),
	// slice_segment_header_extension_present(10), RapPicFlag(11),
	// IdrPicFlag(12), IntraPicFlag(13).
	idr := nalType == hevcNalIDRWRADL || nalType == hevcNalIDRNLP
	rap := nalType >= 16 && nalType <= 23
	pic.SliceParsingFields = pps.listsModificationPresent |
		(sps.longTermRefPicsPresent << 1) |
		(sps.spsTemporalMvpEnabled << 2) |
		(pps.cabacInitPresent << 3) |
		(pps.outputFlagPresent << 4) |
		(pps.dependentSliceSegmentsEnabled << 5) |
		(pps.ppsSliceChromaQPOffsetsPresent << 6) |
		(sps.saoEnabled << 7) |
		(pps.deblockingFilterOverrideEnabled << 8) |
		(pps.ppsDeblockingFilterDisabled << 9) |
		(pps.sliceSegmentHeaderExtPresent << 10) |
		(boolU32(rap) << 11) |
		(boolU32(idr) << 12) |
		(1 << 13) // intra-only stream

	pic.Log2MaxPicOrderCntLsbMinus4 = uint8(sps.log2MaxPocLsbMinus4)
	pic.NumShortTermRefPicSets = uint8(sps.numShortTermRPS)
	pic.NumRefIdxL0DefaultActiveMinus1 = uint8(pps.numRefIdxL0DefaultMinus1)
	pic.NumRefIdxL1DefaultActiveMinus1 = uint8(pps.numRefIdxL1DefaultMinus1)
	pic.PpsBetaOffsetDiv2 = int8(pps.ppsBetaOffsetDiv2)
	pic.PpsTcOffsetDiv2 = int8(pps.ppsTcOffsetDiv2)
	pic.NumExtraSliceHeaderBits = uint8(pps.numExtraSliceHeaderBits)
	return pic
}

// buildHEVCSliceParam fills VASliceParameterBufferHEVC. slice_data_byte_offset
// is the byte offset (NAL header included) to slice_data() — VA-API HEVC
// uses a *byte* offset, not a bit offset.
func (d *vaDecoder) buildHEVCSliceParam(sliceNAL []byte, nalType int) vaSliceParameterBufferHEVC {
	sh := d.parseHEVCSliceHeader(sliceNAL, nalType)

	var sp vaSliceParameterBufferHEVC
	sp.SliceDataSize = uint32(len(sliceNAL))
	sp.SliceDataOffset = 0
	sp.SliceDataFlag = vaSliceDataFlagAll
	sp.SliceDataByteOffset = uint32(sh.headerBytes)
	sp.SliceSegmentAddress = 0
	for i := range sp.RefPicList[0] {
		sp.RefPicList[0][i] = 0xFF
		sp.RefPicList[1][i] = 0xFF
	}
	// LongSliceFlags (va_dec_hevc.h): LastSliceOfPic(0)=1,
	// dependent_slice_segment(1), slice_type(2..3), color_plane_id(4..5),
	// slice_sao_luma(6), slice_sao_chroma(7), mvd_l1_zero(8),
	// cabac_init(9), slice_temporal_mvp_enabled(10),
	// slice_deblocking_filter_disabled(11), collocated_from_l0(12),
	// slice_loop_filter_across_slices(13).
	sp.LongSliceFlags = 1 |
		(sh.sliceType << 2) |
		(sh.saoLuma << 6) |
		(sh.saoChroma << 7)
	sp.CollocatedRefIdx = 0xFF
	sp.NumRefIdxL0ActiveMinus1 = uint8(d.hevc.pps.numRefIdxL0DefaultMinus1)
	sp.NumRefIdxL1ActiveMinus1 = uint8(d.hevc.pps.numRefIdxL1DefaultMinus1)
	sp.SliceQPDelta = int8(sh.sliceQPDelta)
	sp.SliceCbQPOffset = int8(sh.sliceCbQPOffset)
	sp.SliceCrQPOffset = int8(sh.sliceCrQPOffset)
	sp.SliceBetaOffsetDiv2 = int8(sh.betaOffsetDiv2)
	sp.SliceTcOffsetDiv2 = int8(sh.tcOffsetDiv2)
	sp.FiveMinusMaxNumMergeCand = uint8(sh.fiveMinusMaxMerge)
	return sp
}

func boolU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// defaultHEVCIQMatrix returns the HEVC default scaling lists (flat 16).
func defaultHEVCIQMatrix() vaIQMatrixBufferHEVC {
	var iq vaIQMatrixBufferHEVC
	fill := func(b []byte) {
		for i := range b {
			b[i] = 16
		}
	}
	for i := range iq.ScalingList4x4 {
		fill(iq.ScalingList4x4[i][:])
	}
	for i := range iq.ScalingList8x8 {
		fill(iq.ScalingList8x8[i][:])
	}
	for i := range iq.ScalingList16x16 {
		fill(iq.ScalingList16x16[i][:])
	}
	for i := range iq.ScalingList32x32 {
		fill(iq.ScalingList32x32[i][:])
	}
	for i := range iq.ScalingListDC16x16 {
		iq.ScalingListDC16x16[i] = 16
	}
	for i := range iq.ScalingListDC32x32 {
		iq.ScalingListDC32x32[i] = 16
	}
	return iq
}

// vaIQMatrixBufferHEVC mirrors VAIQMatrixBufferHEVC (va_dec_hevc.h).
type vaIQMatrixBufferHEVC struct {
	ScalingList4x4     [6][16]uint8
	ScalingList8x8     [6][64]uint8
	ScalingList16x16   [6][64]uint8
	ScalingList32x32   [2][64]uint8
	ScalingListDC16x16 [6]uint8
	ScalingListDC32x32 [2]uint8
	vaReserved         [4]uint32
}
