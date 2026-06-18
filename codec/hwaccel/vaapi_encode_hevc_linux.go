//go:build linux

// H.265 (HEVC) encode parameter construction and VPS/SPS/PPS/slice-header
// authoring for the VAAPI low-power encoder. Every frame is an IDR I-slice
// (Main profile, 4:2:0 8-bit, CTB 64, AMP+SAO+temporal-MVP on). The
// VPS/SPS/PPS RBSPs are authored to match the seq/pic parameter buffers and
// submitted as packed headers so the driver embeds them into the coded
// buffer.
//
// # iHD HEVC low-power packing requirements
//
// The Intel iHD low-power HEVC encoder differs from its H.264 path in how it
// expects packed headers, and getting this wrong stalls the GPU for several
// seconds and returns VA_STATUS_ERROR_ENCODING_ERROR from vaMapBuffer on the
// coded buffer (the failure mode that previously gated this path off). The
// requirements, recovered by stracing and LIBVA_TRACE-diffing ffmpeg's own
// hevc_vaapi -low_power submission, are:
//
//   - The config's VAConfigAttribEncPackedHeaders advertises
//     SEQUENCE | SLICE (not Sequence | Picture): the LP encoder does NOT
//     author the slice_segment_header(), so the application must pack it.
//   - VPS+SPS+PPS are concatenated into a single SEQUENCE packed header
//     (not three separate headers).
//   - A SLICE packed header carrying the authored slice_segment_header()
//     is submitted after the slice parameter buffer.
//   - The input surface is allocated with explicit MemoryType(=VA) +
//     PixelFormat(=NV12) attributes (see vaapi_encode_linux.go); a
//     mis-tagged pixel-format attribute let the driver pick a tiling the
//     HEVC LP path mishandles.
//
// With these in place the path encodes and round-trips near-losslessly,
// matching H.264. ErrEncodeUnsupportedOnDriver is retained as a sentinel for
// genuinely-unsupported drivers but is no longer returned for HEVC on iHD.

package hwaccel

import "unsafe"

// HEVC coding constants for the all-intra Main encoder.
const (
	hevcProfileMain   = 1
	hevcProfileMain10 = 2
	hevcLevelIDC      = 90 // level 3.0 (general_level_idc = 30*level); ample for <= 720p

	// log2 sizes: min CB = 8 (minus3=0), max CB = 64 (diff=3) -> CTB 64x64,
	// which the iHD low-power HEVC encoder requires.
	hevcLog2MinCbMinus3  = 0
	hevcLog2DiffMaxMinCb = 3
	hevcCtbSize          = 64
	// min TB = 4 (minus2=0), max TB = 32 (diff=3).
	hevcLog2MinTbMinus2  = 0
	hevcLog2DiffMaxMinTb = 3
)

// hevcNALVPS/SPS/PPS NAL header bytes (forbidden_zero=0, layer_id=0,
// temporal_id_plus1=1 -> low byte 0x01; type in bits 1..6 of the high byte).
func hevcNALHeader(nalType int) []byte {
	return []byte{byte(nalType << 1), 0x01}
}

// buildHEVCParams builds the sequence, picture, slice and frame-rate
// buffers for an IDR frame and submits the packed VPS/SPS/PPS headers.
func (e *vaEncoder) buildHEVCParams(add addFunc) error {
	qp := e.pickQP()
	// Picture size must be a multiple of min CB (8); align to CTB for the
	// surface but report luma sample count (aligned to min-CB) to the driver.
	picW := alignUp(e.width, 8)
	picH := alignUp(e.height, 8)
	ctbW := alignUp(e.width, hevcCtbSize) / hevcCtbSize
	ctbH := alignUp(e.height, hevcCtbSize) / hevcCtbSize

	seq := vaEncSequenceParameterBufferHEVC{
		GeneralProfileIdc:                 hevcProfileMain,
		GeneralLevelIdc:                   hevcLevelIDC,
		GeneralTierFlag:                   0,
		IntraPeriod:                       1,
		IntraIdrPeriod:                    1,
		IpPeriod:                          1,
		BitsPerSecond:                     0, // CQP mode: quantiser carried in pic_init_qp
		PicWidthInLumaSamples:             uint16(picW),
		PicHeightInLumaSamples:            uint16(picH),
		Log2MinLumaCodingBlockSizeMinus3:  hevcLog2MinCbMinus3,
		Log2DiffMaxMinLumaCodingBlockSize: hevcLog2DiffMaxMinCb,
		Log2MinTransformBlockSizeMinus2:   hevcLog2MinTbMinus2,
		Log2DiffMaxMinTransformBlockSize:  hevcLog2DiffMaxMinTb,
		MaxTransformHierarchyDepthInter:   2,
		MaxTransformHierarchyDepthIntra:   2,
	}
	// seq_fields bit layout (va_enc_hevc.h): chroma_format_idc(0..1)=1,
	// separate_colour_plane(2), bit_depth_luma_minus8(3..5),
	// bit_depth_chroma_minus8(6..8), scaling_list_enabled(9),
	// strong_intra_smoothing(10), amp_enabled(11), sao_enabled(12),
	// pcm_enabled(13), pcm_loop_filter_disabled(14),
	// sps_temporal_mvp_enabled(15), low_delay_seq(16), hierachical(17).
	// Match the iHD encoder's expected tool set (AMP + SAO + temporal MVP),
	// mirroring what its own ffmpeg path submits; it rejects the picture
	// otherwise.
	seq.SeqFields = 1 | (1 << 11) /*amp*/ | (1 << 12) /*sao*/ | (1 << 15) /*sps_temporal_mvp*/
	if _, err := add(vaEncSequenceParameterBufferType, int(unsafe.Sizeof(seq)), unsafe.Pointer(&seq)); err != nil {
		return err
	}

	// Sequence-level misc buffers (frame rate + rate control) before the
	// picture parameter buffer, matching the iHD encoder's submission order.
	if err := e.addRateAndFrameRate(add); err != nil {
		return err
	}

	pic := vaEncPictureParameterBufferHEVC{
		CodedBuf:                       e.codedBuf,
		CollocatedRefPicIndex:          0,
		LastPicture:                    0,
		PicInitQP:                      uint8(qp),
		NumRefIdxL0DefaultActiveMinus1: 0,
		NumRefIdxL1DefaultActiveMinus1: 0,
		SlicePicParameterSetID:         0,
		NalUnitType:                    hevcNALIDRWRADL, // 19
	}
	pic.DecodedCurrPic = vaPictureHEVC{PictureID: e.surface, PicOrderCnt: 0, Flags: 0}
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaPictureHEVC{PictureID: vaInvalidSurface, Flags: vaPictureHEVCInvalid}
	}
	// pic_fields bit layout (va_enc_hevc.h): idr_pic_flag(0)=1,
	// coding_type(1..3)=1 (I), reference_pic_flag(4)=1,
	// transform_skip_enabled(8)=1, pps_loop_filter_across_slices(16)=1 —
	// matching the iHD encoder's expected PPS tool set.
	pic.PicFields = 1 | (1 << 1) | (1 << 4) | (1 << 8) | (1 << 16)
	if _, err := add(vaEncPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return err
	}

	// The iHD low-power HEVC encoder expects VPS+SPS+PPS concatenated into a
	// single Sequence packed header (not three separate headers), exactly as
	// ffmpeg's hevc_vaapi -low_power path submits them.
	seqHdr := make([]byte, 0, len(e.vps)+len(e.sps)+len(e.pps))
	seqHdr = append(seqHdr, e.vps...)
	seqHdr = append(seqHdr, e.sps...)
	seqHdr = append(seqHdr, e.pps...)
	if err := e.addPackedHeader(add, vaEncPackedHeaderTypeSequence, seqHdr); err != nil {
		return err
	}

	slice := vaEncSliceParameterBufferHEVC{
		SliceSegmentAddress:     0,
		NumCtuInSlice:           uint32(ctbW * ctbH),
		SliceType:               2, // I slice
		SlicePicParameterSetID:  0,
		NumRefIdxL0ActiveMinus1: 0,
		NumRefIdxL1ActiveMinus1: 0,
		MaxNumMergeCand:         5,
		SliceQPDelta:            0,
	}
	for i := range slice.RefPicList0 {
		slice.RefPicList0[i] = vaPictureHEVC{PictureID: vaInvalidSurface, Flags: vaPictureHEVCInvalid}
		slice.RefPicList1[i] = vaPictureHEVC{PictureID: vaInvalidSurface, Flags: vaPictureHEVCInvalid}
	}
	// slice_fields bit layout (va_enc_hevc.h): last_slice_of_pic_flag(0)=1,
	// dependent_slice_segment(1), colour_plane_id(2..3),
	// slice_temporal_mvp_enabled(4), slice_sao_luma_flag(5)=1,
	// slice_sao_chroma_flag(6)=1, ... — SAO on (matches seq sao_enabled).
	slice.SliceFields = 1 | (1 << 5) /*sao luma*/ | (1 << 6) /*sao chroma*/
	if _, err := add(vaEncSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return err
	}

	// Unlike H.264 LP, the iHD HEVC LP encoder does not author the slice
	// segment header — the application packs it as a Slice packed header.
	if err := e.addPackedHeader(add, vaEncPackedHeaderTypeSlice, e.hevcSliceHeader(qp)); err != nil {
		return err
	}
	return nil
}

// hevcSliceHeader authors the Annex-B packed slice_segment_header() for the
// single all-intra IDR I-slice, consistent with the authored SPS/PPS tool
// set (SAO on, temporal-MVP off at slice level, deblocking on). The iHD
// low-power HEVC encoder appends the CTU coded data after this header.
func (e *vaEncoder) hevcSliceHeader(qp int) []byte {
	w := newBitWriter()
	w.putBit(1) // first_slice_segment_in_pic_flag
	// no_output_of_prior_pics_flag (present because nal_unit_type is an IDR,
	// 16..23): a fresh IDR keeps no prior output constraint.
	w.putBit(0)
	w.ue(0) // slice_pic_parameter_set_id
	// dependent_slice_segment_flag / slice_segment_address are absent for the
	// first (and only) segment.
	// num_extra_slice_header_bits == 0 in our PPS, so no extra bits here.
	w.ue(2)     // slice_type (I)
	w.putBit(1) // slice_sao_luma_flag   (sample_adaptive_offset_enabled_flag=1)
	w.putBit(1) // slice_sao_chroma_flag
	// No ref-pic / temporal-MVP / weighted-pred syntax for an I-slice.
	// slice_qp_delta == 0: the slice QP equals the PPS init_qp (== qp).
	_ = qp
	w.se(0)
	// pps_slice_chroma_qp_offsets_present, deblocking_filter_override_enabled
	// and deblocking_filter_control_present are all 0 in our PPS, so no
	// chroma-QP or deblocking-override syntax follows. But
	// pps_loop_filter_across_slices_enabled_flag is 1 and SAO is on, so
	// slice_loop_filter_across_slices_enabled_flag is present.
	w.putBit(1) // slice_loop_filter_across_slices_enabled_flag
	// byte_alignment() terminates the header.
	rbsp := w.byteAlign()
	return annexB(append(hevcNALHeader(hevcNALIDRWRADL), rbsp...))
}

// hevcNALIDRWRADL is the NAL unit type for an IDR_W_RADL slice.
const hevcNALIDRWRADL = 19

// authorHEVCParameterSets authors minimal Main-profile VPS/SPS/PPS RBSPs in
// Annex-B form.
func (e *vaEncoder) authorHEVCParameterSets() {
	e.vps = annexB(e.hevcVPS())
	e.sps = annexB(e.hevcSPS())
	e.pps = annexB(e.hevcPPS())
}

// writeProfileTierLevel writes a profile_tier_level() for a Main-profile,
// general-tier stream at hevcLevelIDC, with maxNumSubLayersMinus1 == 0.
func writeProfileTierLevel(w *bitWriter) {
	w.putBits(0, 2)               // general_profile_space
	w.putBit(0)                   // general_tier_flag
	w.putBits(hevcProfileMain, 5) // general_profile_idc
	// general_profile_compatibility_flag[32]: set bit for Main (idx 1).
	for i := 0; i < 32; i++ {
		if i == hevcProfileMain {
			w.putBit(1)
		} else {
			w.putBit(0)
		}
	}
	w.putBit(1) // general_progressive_source_flag
	w.putBit(0) // general_interlaced_source_flag
	w.putBit(0) // general_non_packed_constraint_flag
	w.putBit(0) // general_frame_only_constraint_flag
	// general_reserved_zero_43bits + general_inbld/reserved: 44 zero bits.
	w.putBits(0, 22)
	w.putBits(0, 22)
	w.putBits(hevcLevelIDC, 8) // general_level_idc
}

// hevcVPS authors a minimal VPS RBSP (NAL type 32).
func (e *vaEncoder) hevcVPS() []byte {
	w := newBitWriter()
	w.putBits(0, 4)       // vps_video_parameter_set_id
	w.putBits(3, 2)       // vps_reserved_three_2bits
	w.putBits(0, 6)       // vps_max_layers_minus1
	w.putBits(0, 3)       // vps_max_sub_layers_minus1
	w.putBit(1)           // vps_temporal_id_nesting_flag
	w.putBits(0xFFFF, 16) // vps_reserved_0xffff_16bits
	writeProfileTierLevel(w)
	w.putBit(0)     // vps_sub_layer_ordering_info_present_flag
	w.ue(1)         // vps_max_dec_pic_buffering_minus1[0]
	w.ue(0)         // vps_max_num_reorder_pics[0]
	w.ue(0)         // vps_max_latency_increase_plus1[0]
	w.putBits(0, 6) // vps_max_layer_id
	w.ue(0)         // vps_num_layer_sets_minus1
	w.putBit(0)     // vps_timing_info_present_flag
	w.putBit(0)     // vps_extension_flag

	return append(hevcNALHeader(32), w.rbspTrailing()...)
}

// hevcSPS authors a minimal Main-profile SPS RBSP (NAL type 33).
func (e *vaEncoder) hevcSPS() []byte {
	picW := alignUp(e.width, 8)
	picH := alignUp(e.height, 8)

	w := newBitWriter()
	w.putBits(0, 4) // sps_video_parameter_set_id
	w.putBits(0, 3) // sps_max_sub_layers_minus1
	w.putBit(1)     // sps_temporal_id_nesting_flag
	writeProfileTierLevel(w)
	w.ue(0)            // sps_seq_parameter_set_id
	w.ue(1)            // chroma_format_idc (4:2:0)
	w.ue(uint32(picW)) // pic_width_in_luma_samples
	w.ue(uint32(picH)) // pic_height_in_luma_samples

	// conformance window if luma dims differ from visible size.
	if picW != e.width || picH != e.height {
		w.putBit(1)                         // conformance_window_flag
		w.ue(0)                             // conf_win_left_offset
		w.ue(uint32((picW - e.width) / 2))  // conf_win_right_offset (chroma units)
		w.ue(0)                             // conf_win_top_offset
		w.ue(uint32((picH - e.height) / 2)) // conf_win_bottom_offset
	} else {
		w.putBit(0) // conformance_window_flag
	}

	w.ue(0)                    // bit_depth_luma_minus8
	w.ue(0)                    // bit_depth_chroma_minus8
	w.ue(0)                    // log2_max_pic_order_cnt_lsb_minus4
	w.putBit(0)                // sps_sub_layer_ordering_info_present_flag
	w.ue(1)                    // sps_max_dec_pic_buffering_minus1[0]
	w.ue(0)                    // sps_max_num_reorder_pics[0]
	w.ue(0)                    // sps_max_latency_increase_plus1[0]
	w.ue(hevcLog2MinCbMinus3)  // log2_min_luma_coding_block_size_minus3
	w.ue(hevcLog2DiffMaxMinCb) // log2_diff_max_min_luma_coding_block_size
	w.ue(hevcLog2MinTbMinus2)  // log2_min_luma_transform_block_size_minus2
	w.ue(hevcLog2DiffMaxMinTb) // log2_diff_max_min_luma_transform_block_size
	w.ue(2)                    // max_transform_hierarchy_depth_inter
	w.ue(2)                    // max_transform_hierarchy_depth_intra
	w.putBit(0)                // scaling_list_enabled_flag
	// Tool set must match the VAEncSequenceParameterBufferHEVC.seq_fields
	// submitted in buildHEVCParams (and what the iHD LP encoder expects):
	// AMP + SAO + temporal-MVP on. A mismatch between this RBSP and the
	// param buffer corrupts the driver's view of the stream.
	w.putBit(1) // amp_enabled_flag
	w.putBit(1) // sample_adaptive_offset_enabled_flag
	w.putBit(0) // pcm_enabled_flag
	w.ue(0)     // num_short_term_ref_pic_sets
	w.putBit(0) // long_term_ref_pics_present_flag
	w.putBit(1) // sps_temporal_mvp_enabled_flag
	w.putBit(0) // strong_intra_smoothing_enabled_flag
	w.putBit(0) // vui_parameters_present_flag
	w.putBit(0) // sps_extension_present_flag

	return append(hevcNALHeader(33), w.rbspTrailing()...)
}

// hevcPPS authors a minimal PPS RBSP (NAL type 34).
func (e *vaEncoder) hevcPPS() []byte {
	w := newBitWriter()
	w.ue(0)                      // pps_pic_parameter_set_id
	w.ue(0)                      // pps_seq_parameter_set_id
	w.putBit(0)                  // dependent_slice_segments_enabled_flag
	w.putBit(0)                  // output_flag_present_flag
	w.putBits(0, 3)              // num_extra_slice_header_bits
	w.putBit(0)                  // sign_data_hiding_enabled_flag
	w.putBit(0)                  // cabac_init_present_flag
	w.ue(0)                      // num_ref_idx_l0_default_active_minus1
	w.ue(0)                      // num_ref_idx_l1_default_active_minus1
	w.se(int32(e.pickQP()) - 26) // init_qp_minus26
	w.putBit(0)                  // constrained_intra_pred_flag
	// transform_skip_enabled and pps_loop_filter_across_slices must mirror the
	// VAEncPictureParameterBufferHEVC.pic_fields submitted in buildHEVCParams
	// (bits 8 and 16); a mismatch desynchronises the authored RBSP from the
	// driver's view and from the decoder's slice_segment_header() byte offset.
	w.putBit(1) // transform_skip_enabled_flag
	w.putBit(0) // cu_qp_delta_enabled_flag
	w.se(0)     // pps_cb_qp_offset
	w.se(0)     // pps_cr_qp_offset
	w.putBit(0) // pps_slice_chroma_qp_offsets_present_flag
	w.putBit(0) // weighted_pred_flag
	w.putBit(0) // weighted_bipred_flag
	w.putBit(0) // transquant_bypass_enabled_flag
	w.putBit(0) // tiles_enabled_flag
	w.putBit(0) // entropy_coding_sync_enabled_flag
	w.putBit(1) // pps_loop_filter_across_slices_enabled_flag
	w.putBit(0) // deblocking_filter_control_present_flag
	w.putBit(0) // pps_scaling_list_data_present_flag
	w.putBit(0) // lists_modification_present_flag
	w.ue(0)     // log2_parallel_merge_level_minus2
	w.putBit(0) // slice_segment_header_extension_present_flag
	w.putBit(0) // pps_extension_present_flag

	return append(hevcNALHeader(34), w.rbspTrailing()...)
}
