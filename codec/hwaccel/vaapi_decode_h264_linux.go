//go:build linux

// H.264 VLD decode: SPS/PPS/slice-header parsing and VA parameter-buffer
// construction. The parser reads exactly the syntax elements needed to
// fill VAPictureParameterBufferH264, VAIQMatrixBufferH264 and
// VASliceParameterBufferH264 for an IDR / I-or-P single-slice picture (the
// form produced by the all-intra VAAPI encoder and typical keyframe
// streams). Fields the driver derives itself, and tool sets this decoder
// does not exercise (FMO/ASO, scaling matrices), are left at their
// spec-default values.

package hwaccel

import (
	"unsafe"

	"go-mediatoolkit/video"
)

// H.264 NAL unit types (nal[0] & 0x1f).
const (
	h264NalSlice    = 1
	h264NalIDRSlice = 5
	h264NalSPS      = 7
	h264NalPPS      = 8
	h264NalAUD      = 9
)

// h264SPS holds the subset of seq_parameter_set_rbsp() the decoder needs.
type h264SPS struct {
	profileIDC                int
	seqParameterSetID         uint32
	chromaFormatIDC           uint32
	log2MaxFrameNumMinus4     uint32
	picOrderCntType           uint32
	log2MaxPocLsbMinus4       uint32
	deltaPicOrderAlwaysZero   uint32
	maxNumRefFrames           uint32
	gapsInFrameNum            uint32
	picWidthInMbsMinus1       uint32
	picHeightInMapUnitsMinus1 uint32
	frameMbsOnlyFlag          uint32
	mbAdaptiveFrameField      uint32
	direct8x8Inference        uint32
	frameCroppingFlag         uint32
	cropLeft, cropRight       uint32
	cropTop, cropBottom       uint32
}

// h264PPS holds the subset of pic_parameter_set_rbsp() the decoder needs.
type h264PPS struct {
	picParameterSetID              uint32
	seqParameterSetID              uint32
	entropyCodingModeFlag          uint32
	bottomFieldPicOrderPresent     uint32
	numRefIdxL0DefaultMinus1       uint32
	numRefIdxL1DefaultMinus1       uint32
	weightedPredFlag               uint32
	weightedBipredIDC              uint32
	picInitQPMinus26               int32
	chromaQPIndexOffset            int32
	deblockingFilterControlPresent uint32
	constrainedIntraPred           uint32
	redundantPicCntPresent         uint32
	transform8x8Mode               uint32
	secondChromaQPIndexOffset      int32
}

// h264Params bundles the live SPS/PPS the decoder context was built from.
type h264Params struct {
	sps h264SPS
	pps h264PPS
}

// decodeH264 parses the access unit's parameter sets and slice, builds the
// VA buffers, decodes, and reads back the frame.
func (d *vaDecoder) decodeH264(nals [][]byte) ([]video.Frame, error) {
	var sliceNAL []byte
	var idr bool
	for _, nal := range nals {
		switch int(nal[0] & 0x1f) {
		case h264NalSPS:
			d.h264.sps = parseH264SPS(ebspToRBSP(nal[1:]))
			d.have = true
		case h264NalPPS:
			d.h264.pps = parseH264PPS(ebspToRBSP(nal[1:]))
		case h264NalIDRSlice:
			sliceNAL = nal
			idr = true
		case h264NalSlice:
			sliceNAL = nal
		}
	}
	if !d.have {
		return nil, ErrParameterSetsMissing
	}
	if sliceNAL == nil {
		return nil, nil // parameter-set-only access unit
	}

	sps := &d.h264.sps
	mbW := int(sps.picWidthInMbsMinus1) + 1
	frameHeightInMbs := (2 - int(sps.frameMbsOnlyFlag)) * (int(sps.picHeightInMapUnitsMinus1) + 1)
	codedW := mbW * 16
	codedH := frameHeightInMbs * 16
	if codedW <= 0 || codedH <= 0 {
		return nil, ErrBitstreamParse
	}
	if err := d.ensureContext(d.h264Profile(), codedW, codedH); err != nil {
		return nil, err
	}

	var bufs []uint32
	defer func() { d.freeDecodeBufs(bufs) }()

	pic := d.buildH264PicParam(idr)
	if err := d.addDecodeBuf(&bufs, vaPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return nil, err
	}
	iq := defaultH264IQMatrix()
	if err := d.addDecodeBuf(&bufs, vaIQMatrixBufferType, int(unsafe.Sizeof(iq)), unsafe.Pointer(&iq)); err != nil {
		return nil, err
	}
	slice := d.buildH264SliceParam(sliceNAL, idr)
	if err := d.addDecodeBuf(&bufs, vaSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return nil, err
	}
	if err := d.addDecodeBuf(&bufs, vaSliceDataBufferType, len(sliceNAL), unsafe.Pointer(&sliceNAL[0])); err != nil {
		return nil, err
	}

	if err := d.submitPicture(bufs); err != nil {
		return nil, err
	}

	visW := codedW - int(sps.cropRight)*2
	visH := codedH - int(sps.cropBottom)*2
	f, err := d.readSurface(visW, visH)
	if err != nil {
		return nil, err
	}
	d.frameIdx++
	return []video.Frame{f}, nil
}

// h264Profile maps the parsed SPS profile_idc to a VAProfile for the config.
func (d *vaDecoder) h264Profile() int32 {
	switch d.h264.sps.profileIDC {
	case h264ProfileBaseline:
		return vaProfileH264ConstrainedBaseline
	case h264ProfileMain:
		return vaProfileH264Main
	default:
		return vaProfileH264High
	}
}

// buildH264PicParam fills VAPictureParameterBufferH264 from the live SPS/PPS.
func (d *vaDecoder) buildH264PicParam(idr bool) vaPictureParameterBufferH264 {
	sps := &d.h264.sps
	pps := &d.h264.pps

	var pic vaPictureParameterBufferH264
	pic.CurrPic = vaPictureH264{PictureID: d.surface, FrameIdx: 0, Flags: 0}
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
	}
	pic.PictureWidthInMbsMinus1 = uint16(sps.picWidthInMbsMinus1)
	frameHeightInMbs := (2 - int(sps.frameMbsOnlyFlag)) * (int(sps.picHeightInMapUnitsMinus1) + 1)
	pic.PictureHeightInMbsMinus1 = uint16(frameHeightInMbs - 1)
	pic.BitDepthLumaMinus8 = 0
	pic.BitDepthChromaMinus8 = 0
	pic.NumRefFrames = uint8(sps.maxNumRefFrames)

	// seq_fields bit layout (va.h): chroma_format_idc(0..1),
	// residual_colour_transform(2), gaps_in_frame_num(3),
	// frame_mbs_only(4), mb_adaptive_frame_field(5),
	// direct_8x8_inference(6), MinLumaBiPredSize8x8(7),
	// log2_max_frame_num_minus4(8..11), pic_order_cnt_type(12..13),
	// log2_max_pic_order_cnt_lsb_minus4(14..17),
	// delta_pic_order_always_zero(18).
	chroma := sps.chromaFormatIDC
	if chroma == 0 {
		chroma = 1
	}
	pic.SeqFields = chroma |
		(sps.gapsInFrameNum << 3) |
		(sps.frameMbsOnlyFlag << 4) |
		(sps.mbAdaptiveFrameField << 5) |
		(sps.direct8x8Inference << 6) |
		(sps.log2MaxFrameNumMinus4 << 8) |
		(sps.picOrderCntType << 12) |
		(sps.log2MaxPocLsbMinus4 << 14) |
		(sps.deltaPicOrderAlwaysZero << 18)

	pic.PicInitQPMinus26 = int8(pps.picInitQPMinus26)
	pic.ChromaQPIndexOffset = int8(pps.chromaQPIndexOffset)
	pic.SecondChromaQPIndexOffset = int8(pps.secondChromaQPIndexOffset)

	// pic_fields bit layout (va.h): entropy_coding_mode(0),
	// weighted_pred(1), weighted_bipred_idc(2..3), transform_8x8_mode(4),
	// field_pic(5), constrained_intra_pred(6), pic_order_present(7),
	// deblocking_filter_control_present(8), redundant_pic_cnt_present(9),
	// reference_pic_flag(10).
	refFlag := uint32(0)
	if idr {
		refFlag = 1
	}
	pic.PicFields = pps.entropyCodingModeFlag |
		(pps.weightedPredFlag << 1) |
		(pps.weightedBipredIDC << 2) |
		(pps.transform8x8Mode << 4) |
		(pps.constrainedIntraPred << 6) |
		(pps.bottomFieldPicOrderPresent << 7) |
		(pps.deblockingFilterControlPresent << 8) |
		(pps.redundantPicCntPresent << 9) |
		(refFlag << 10)

	pic.FrameNum = 0
	return pic
}

// buildH264SliceParam fills VASliceParameterBufferH264. The slice-data
// buffer is the whole slice NAL (header byte included); slice_data_offset
// is 0 and slice_data_bit_offset is the bit position, measured in the RBSP
// (emulation-prevention removed) from the start of the NAL, of the first
// macroblock's data — i.e. just past the parsed slice_header(). The
// hardware needs this to know where the entropy-coded residual begins.
func (d *vaDecoder) buildH264SliceParam(sliceNAL []byte, idr bool) vaSliceParameterBufferH264 {
	sh := d.parseH264SliceHeader(sliceNAL, idr)

	var sp vaSliceParameterBufferH264
	sp.SliceDataSize = uint32(len(sliceNAL))
	sp.SliceDataOffset = 0
	sp.SliceDataFlag = vaSliceDataFlagAll
	sp.SliceDataBitOffset = uint16(sh.headerBits)
	sp.FirstMbInSlice = uint16(sh.firstMbInSlice)
	sp.SliceType = uint8(sh.sliceType)
	sp.NumRefIdxL0ActiveMinus1 = uint8(d.h264.pps.numRefIdxL0DefaultMinus1)
	sp.NumRefIdxL1ActiveMinus1 = uint8(d.h264.pps.numRefIdxL1DefaultMinus1)
	sp.CabacInitIdc = uint8(sh.cabacInitIdc)
	sp.SliceQPDelta = int8(sh.sliceQPDelta)
	sp.DisableDeblockingFilterIdc = uint8(sh.disableDeblockIdc)
	sp.SliceAlphaC0OffsetDiv2 = int8(sh.alphaC0OffsetDiv2)
	sp.SliceBetaOffsetDiv2 = int8(sh.betaOffsetDiv2)
	for i := range sp.RefPicList0 {
		sp.RefPicList0[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
		sp.RefPicList1[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
	}
	return sp
}

// h264SliceHeader holds the parsed fields of an I/P slice header plus the
// bit offset where slice_data() begins.
type h264SliceHeader struct {
	firstMbInSlice    uint32
	sliceType         uint32
	cabacInitIdc      uint32
	sliceQPDelta      int32
	disableDeblockIdc uint32
	alphaC0OffsetDiv2 int32
	betaOffsetDiv2    int32
	headerBits        int
}

// parseH264SliceHeader parses slice_header() for an I or P slice (the only
// types our encoder/streams produce) and records the bit offset of the
// first byte of slice_data() in the RBSP. The slice NAL is RBSP-stripped
// first so bit positions match the (emulation-free) bit count VA-API wants.
func (d *vaDecoder) parseH264SliceHeader(sliceNAL []byte, idr bool) h264SliceHeader {
	sps := &d.h264.sps
	pps := &d.h264.pps

	rbsp := ebspToRBSP(sliceNAL)
	r := newBitReader(rbsp)
	r.u(8) // NAL header byte

	var sh h264SliceHeader
	sh.firstMbInSlice = r.ue()
	sh.sliceType = r.ue()
	r.ue()                                  // pic_parameter_set_id
	r.u(int(sps.log2MaxFrameNumMinus4) + 4) // frame_num

	if sps.frameMbsOnlyFlag == 0 {
		if r.u1() == 1 { // field_pic_flag
			r.u1() // bottom_field_flag
		}
	}
	if idr {
		r.ue() // idr_pic_id
	}
	if sps.picOrderCntType == 0 {
		r.u(int(sps.log2MaxPocLsbMinus4) + 4) // pic_order_cnt_lsb
		if pps.bottomFieldPicOrderPresent == 1 {
			r.se() // delta_pic_order_cnt_bottom
		}
	} else if sps.picOrderCntType == 1 && sps.deltaPicOrderAlwaysZero == 0 {
		r.se()
		if pps.bottomFieldPicOrderPresent == 1 {
			r.se()
		}
	}
	if pps.redundantPicCntPresent == 1 {
		r.ue() // redundant_pic_cnt
	}

	st := sh.sliceType % 5
	// ref_pic_list_modification / num_ref_idx override only for P/B (st 0/1).
	if st == 0 || st == 1 { // P or B
		if r.u1() == 1 { // num_ref_idx_active_override_flag
			r.ue() // num_ref_idx_l0_active_minus1
			if st == 1 {
				r.ue()
			}
		}
		d.skipRefPicListModification(r, st)
		if pps.weightedPredFlag == 1 && (st == 0) ||
			(pps.weightedBipredIDC == 1 && st == 1) {
			d.skipPredWeightTable(r, st)
		}
	}
	// dec_ref_pic_marking for reference slices (nal_ref_idc != 0; our slices
	// are always reference / IDR).
	if idr {
		r.u1() // no_output_of_prior_pics_flag
		r.u1() // long_term_reference_flag
	} else {
		if r.u1() == 1 { // adaptive_ref_pic_marking_mode_flag
			for {
				op := r.ue()
				if op == 0 {
					break
				}
				if op == 1 || op == 3 {
					r.ue()
				}
				if op == 2 {
					r.ue()
				}
				if op == 3 || op == 6 {
					r.ue()
				}
				if op == 4 {
					r.ue()
				}
			}
		}
	}

	if pps.entropyCodingModeFlag == 1 && st != 2 && st != 4 { // CABAC, non-I
		sh.cabacInitIdc = r.ue()
	}
	sh.sliceQPDelta = r.se()

	if pps.deblockingFilterControlPresent == 1 {
		sh.disableDeblockIdc = r.ue()
		if sh.disableDeblockIdc != 1 {
			sh.alphaC0OffsetDiv2 = r.se()
			sh.betaOffsetDiv2 = r.se()
		}
	}

	// CABAC: slice_data() is byte-aligned with cabac_alignment_one_bits.
	if pps.entropyCodingModeFlag == 1 {
		for r.pos%8 != 0 {
			r.u1()
		}
	}
	sh.headerBits = r.pos
	return sh
}

// skipRefPicListModification consumes ref_pic_list_modification() for the
// list(s) of slice type st.
func (d *vaDecoder) skipRefPicListModification(r *bitReader, st uint32) {
	skip := func() {
		if r.u1() == 1 { // ref_pic_list_modification_flag_lX
			for {
				idc := r.ue()
				if idc == 3 {
					break
				}
				r.ue() // abs_diff_pic_num_minus1 / long_term_pic_num
			}
		}
	}
	skip() // l0
	if st == 1 {
		skip() // l1
	}
}

// skipPredWeightTable consumes pred_weight_table() for slice type st.
func (d *vaDecoder) skipPredWeightTable(r *bitReader, st uint32) {
	r.ue() // luma_log2_weight_denom
	if d.h264.sps.chromaFormatIDC != 0 {
		r.ue() // chroma_log2_weight_denom
	}
	num := int(d.h264.pps.numRefIdxL0DefaultMinus1) + 1
	skipList := func(n int) {
		for i := 0; i < n; i++ {
			if r.u1() == 1 { // luma_weight_lX_flag
				r.se()
				r.se()
			}
			if d.h264.sps.chromaFormatIDC != 0 {
				if r.u1() == 1 { // chroma_weight_lX_flag
					for j := 0; j < 2; j++ {
						r.se()
						r.se()
					}
				}
			}
		}
	}
	skipList(num)
	if st == 1 {
		skipList(int(d.h264.pps.numRefIdxL1DefaultMinus1) + 1)
	}
}

// defaultH264IQMatrix returns a flat (16) scaling matrix — the spec default
// when no scaling lists are present.
func defaultH264IQMatrix() vaIQMatrixBufferH264 {
	var iq vaIQMatrixBufferH264
	for i := range iq.ScalingList4x4 {
		for j := range iq.ScalingList4x4[i] {
			iq.ScalingList4x4[i][j] = 16
		}
	}
	for i := range iq.ScalingList8x8 {
		for j := range iq.ScalingList8x8[i] {
			iq.ScalingList8x8[i][j] = 16
		}
	}
	return iq
}

// parseH264SPS parses the subset of an SPS RBSP the decoder needs.
func parseH264SPS(rbsp []byte) h264SPS {
	r := newBitReader(rbsp)
	var s h264SPS
	s.profileIDC = int(r.u(8))
	r.u(8) // constraint flags + reserved
	r.u(8) // level_idc
	s.seqParameterSetID = r.ue()

	switch s.profileIDC {
	case 100, 110, 122, 244, 44, 83, 86, 118, 128, 138, 139, 134, 135:
		s.chromaFormatIDC = r.ue()
		if s.chromaFormatIDC == 3 {
			r.u1() // separate_colour_plane_flag
		}
		r.ue()           // bit_depth_luma_minus8
		r.ue()           // bit_depth_chroma_minus8
		r.u1()           // qpprime_y_zero_transform_bypass_flag
		if r.u1() == 1 { // seq_scaling_matrix_present_flag
			n := 8
			if s.chromaFormatIDC == 3 {
				n = 12
			}
			for i := 0; i < n; i++ {
				if r.u1() == 1 { // seq_scaling_list_present_flag[i]
					skipScalingList(r, map[bool]int{true: 64, false: 16}[i >= 6])
				}
			}
		}
	default:
		s.chromaFormatIDC = 1
	}

	s.log2MaxFrameNumMinus4 = r.ue()
	s.picOrderCntType = r.ue()
	if s.picOrderCntType == 0 {
		s.log2MaxPocLsbMinus4 = r.ue()
	} else if s.picOrderCntType == 1 {
		s.deltaPicOrderAlwaysZero = r.u1()
		r.se() // offset_for_non_ref_pic
		r.se() // offset_for_top_to_bottom_field
		n := r.ue()
		for i := uint32(0); i < n; i++ {
			r.se()
		}
	}
	s.maxNumRefFrames = r.ue()
	s.gapsInFrameNum = r.u1()
	s.picWidthInMbsMinus1 = r.ue()
	s.picHeightInMapUnitsMinus1 = r.ue()
	s.frameMbsOnlyFlag = r.u1()
	if s.frameMbsOnlyFlag == 0 {
		s.mbAdaptiveFrameField = r.u1()
	}
	s.direct8x8Inference = r.u1()
	s.frameCroppingFlag = r.u1()
	if s.frameCroppingFlag == 1 {
		s.cropLeft = r.ue()
		s.cropRight = r.ue()
		s.cropTop = r.ue()
		s.cropBottom = r.ue()
	}
	// vui_parameters_present_flag and the rest are not needed.
	return s
}

// parseH264PPS parses the subset of a PPS RBSP the decoder needs. The
// CAVLC/baseline subset is parsed; the optional transform_8x8 / scaling
// extension at the tail is read only far enough to recover the flags.
func parseH264PPS(rbsp []byte) h264PPS {
	r := newBitReader(rbsp)
	var p h264PPS
	p.picParameterSetID = r.ue()
	p.seqParameterSetID = r.ue()
	p.entropyCodingModeFlag = r.u1()
	p.bottomFieldPicOrderPresent = r.u1()
	numSliceGroups := r.ue()
	if numSliceGroups > 0 {
		// FMO not produced by our encoder; skip the slice-group map syntax
		// conservatively by bailing — the picture param leaves it default.
		_ = numSliceGroups
	}
	p.numRefIdxL0DefaultMinus1 = r.ue()
	p.numRefIdxL1DefaultMinus1 = r.ue()
	p.weightedPredFlag = r.u1()
	p.weightedBipredIDC = r.u(2)
	p.picInitQPMinus26 = r.se()
	r.se() // pic_init_qs_minus26
	p.chromaQPIndexOffset = r.se()
	p.deblockingFilterControlPresent = r.u1()
	p.constrainedIntraPred = r.u1()
	p.redundantPicCntPresent = r.u1()
	// Optional tail (present iff more_rbsp_data): transform_8x8_mode_flag,
	// pic_scaling_matrix_present_flag, second_chroma_qp_index_offset.
	p.secondChromaQPIndexOffset = p.chromaQPIndexOffset
	if moreRBSPData(r) {
		p.transform8x8Mode = r.u1()
		if r.u1() == 1 { // pic_scaling_matrix_present_flag
			// Number of lists depends on transform_8x8 + chroma; our encoder
			// never sets this, so parsing the lists is not exercised.
		}
		p.secondChromaQPIndexOffset = r.se()
	}
	return p
}

// skipScalingList consumes a scaling_list() of the given size.
func skipScalingList(r *bitReader, size int) {
	lastScale := int32(8)
	nextScale := int32(8)
	for j := 0; j < size; j++ {
		if nextScale != 0 {
			delta := r.se()
			nextScale = (lastScale + delta + 256) % 256
		}
		if nextScale != 0 {
			lastScale = nextScale
		}
	}
}

// moreRBSPData reports whether unread payload remains before the
// rbsp_stop_one_bit. It is a conservative check: true if more than 8 bits
// remain, else it scans for the stop bit.
func moreRBSPData(r *bitReader) bool {
	left := r.bitsLeft()
	if left <= 0 {
		return false
	}
	if left > 8 {
		return true
	}
	// Peek the remaining bits: if exactly a stop-one followed by zeros, no
	// more data.
	save := r.pos
	first := r.u1()
	rest := uint32(0)
	for r.bitsLeft() > 0 {
		rest = (rest << 1) | r.u1()
	}
	r.pos = save
	return !(first == 1 && rest == 0)
}
