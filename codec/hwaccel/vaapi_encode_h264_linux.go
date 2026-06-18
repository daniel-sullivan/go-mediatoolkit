//go:build linux

// H.264 encode parameter construction and SPS/PPS authoring for the VAAPI
// low-power encoder. Every frame is an IDR I-slice; the SPS/PPS RBSPs are
// authored to match the seq/pic parameter buffers (profile High, CAVLC,
// 4:2:0 8-bit) and submitted as packed headers so the driver embeds them
// into the coded buffer.

package hwaccel

import (
	"unsafe"

	"go-mediatoolkit/video"
)

// h264 constants for the authored SPS/PPS.
const (
	h264ProfileHigh     = 100
	h264ProfileMain     = 77
	h264ProfileBaseline = 66
	h264LevelIDC        = 40 // level 4.0; ample for <= 1080p30
)

// h264NALRefIDR is the NAL unit header byte for an IDR slice (nal_ref_idc=3,
// type=5).
const h264NALRefIDR = 0x65

// addFunc creates a VA buffer and tracks it for teardown.
type addFunc func(typ uint32, size int, data unsafe.Pointer) (uint32, error)

// buildH264Params builds the sequence, picture, slice and rate-control
// buffers for an IDR frame and submits the packed SPS/PPS headers.
func (e *vaEncoder) buildH264Params(add addFunc) error {
	mbW := alignUp(e.width, 16) / 16
	mbH := alignUp(e.height, 16) / 16
	qp := e.pickQP()

	seq := vaEncSequenceParameterBufferH264{
		SeqParameterSetID:  0,
		LevelIdc:           h264LevelIDC,
		IntraPeriod:        1, // all-intra
		IntraIdrPeriod:     1,
		IpPeriod:           1,
		BitsPerSecond:      0, // CQP mode: quantiser carried in pic_init_qp
		MaxNumRefFrames:    0,
		PictureWidthInMbs:  uint16(mbW),
		PictureHeightInMbs: uint16(mbH),
	}
	// seq_fields: chroma_format_idc=1 (bit0..1), frame_mbs_only_flag=1 (bit2),
	// log2_max_frame_num_minus4=0, pic_order_cnt_type=0,
	// log2_max_pic_order_cnt_lsb_minus4=0.
	seq.SeqFields = 1 /*chroma_format_idc*/ | (1 << 2) /*frame_mbs_only_flag*/
	// If cropping needed (height not a multiple of 16), set crop.
	if e.height%16 != 0 {
		seq.FrameCroppingFlag = 1
		seq.FrameCropBottomOffset = uint32((mbH*16 - e.height) / 2)
	}
	if e.width%16 != 0 {
		seq.FrameCroppingFlag = 1
		seq.FrameCropRightOffset = uint32((mbW*16 - e.width) / 2)
	}
	if _, err := add(vaEncSequenceParameterBufferType, int(unsafe.Sizeof(seq)), unsafe.Pointer(&seq)); err != nil {
		return err
	}

	pic := vaEncPictureParameterBufferH264{
		CodedBuf:                e.codedBuf,
		PicParameterSetID:       0,
		SeqParameterSetID:       0,
		LastPicture:             0,
		FrameNum:                0,
		PicInitQP:               uint8(qp),
		NumRefIdxL0ActiveMinus1: 0,
		NumRefIdxL1ActiveMinus1: 0,
	}
	pic.CurrPic = vaPictureH264{PictureID: e.surface, FrameIdx: 0, Flags: 0}
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
	}
	// pic_fields: idr_pic_flag(bit0)=1, reference_pic_flag(bit1..2)=1,
	// entropy_coding_mode_flag(bit3)=0 (CAVLC), deblocking_filter_control
	// _present_flag(bit10)=1 so disable_deblocking_filter_idc is signalled.
	pic.PicFields = 1 /*idr*/ | (1 << 1) /*ref*/ | (1 << 10) /*deblock ctrl present*/
	if _, err := add(vaEncPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return err
	}

	// Rate control (CBR) + frame rate.
	if err := e.addRateAndFrameRate(add); err != nil {
		return err
	}

	// Packed SPS / PPS headers (must precede the slice param buffer).
	if err := e.addPackedHeader(add, vaEncPackedHeaderTypeSequence, e.sps); err != nil {
		return err
	}
	if err := e.addPackedHeader(add, vaEncPackedHeaderTypePicture, e.pps); err != nil {
		return err
	}

	slice := vaEncSliceParameterBufferH264{
		MacroblockAddress:           0,
		NumMacroblocks:              uint32(mbW * mbH),
		MacroblockInfo:              vaInvalidID,
		SliceType:                   2, // I slice
		PicParameterSetID:           0,
		IdrPicID:                    0,
		PicOrderCntLsb:              0,
		NumRefIdxActiveOverrideFlag: 0,
		NumRefIdxL0ActiveMinus1:     0,
		NumRefIdxL1ActiveMinus1:     0,
		SliceQPDelta:                0,
		DisableDeblockingFilterIdc:  0,
	}
	for i := range slice.RefPicList0 {
		slice.RefPicList0[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
		slice.RefPicList1[i] = vaPictureH264{PictureID: vaInvalidSurface, Flags: vaPictureH264Invalid}
	}
	if _, err := add(vaEncSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return err
	}
	return nil
}

// authorParameterSets builds the Annex-B SPS/PPS (and, for HEVC, VPS) the
// encoder submits as packed headers and prefixes onto keyframes.
func (e *vaEncoder) authorParameterSets() {
	switch e.codec {
	case video.H265:
		e.authorHEVCParameterSets()
		return
	case video.VP9:
		// VP9 low-power encode authors no parameter sets: the driver writes
		// the full frame including its uncompressed header.
		return
	case video.AV1:
		// AV1 OBU sequence/frame headers are authored per-frame in the AV1
		// encode path (they carry frame-specific bit offsets), not cached here.
		return
	}
	e.sps = annexB(e.h264SPS())
	e.pps = annexB(e.h264PPS())
}

// h264SPS authors a minimal High-profile SPS RBSP (with the NAL header byte
// prepended): nal_ref_idc=3, type=7.
func (e *vaEncoder) h264SPS() []byte {
	mbW := alignUp(e.width, 16) / 16
	mbH := alignUp(e.height, 16) / 16

	w := newBitWriter()
	w.putBits(uint32(e.h264ProfileIDC()), 8) // profile_idc
	w.putBits(0, 8)                          // constraint flags + reserved
	w.putBits(h264LevelIDC, 8)               // level_idc
	w.ue(0)                                  // seq_parameter_set_id

	if e.h264ProfileIDC() == h264ProfileHigh {
		w.ue(1)     // chroma_format_idc = 1 (4:2:0)
		w.ue(0)     // bit_depth_luma_minus8
		w.ue(0)     // bit_depth_chroma_minus8
		w.putBit(0) // qpprime_y_zero_transform_bypass_flag
		w.putBit(0) // seq_scaling_matrix_present_flag
	}

	w.ue(0)               // log2_max_frame_num_minus4
	w.ue(0)               // pic_order_cnt_type
	w.ue(0)               // log2_max_pic_order_cnt_lsb_minus4
	w.ue(0)               // max_num_ref_frames
	w.putBit(0)           // gaps_in_frame_num_value_allowed_flag
	w.ue(uint32(mbW - 1)) // pic_width_in_mbs_minus1
	w.ue(uint32(mbH - 1)) // pic_height_in_map_units_minus1
	w.putBit(1)           // frame_mbs_only_flag
	w.putBit(1)           // direct_8x8_inference_flag

	crop := e.width%16 != 0 || e.height%16 != 0
	if crop {
		w.putBit(1)                           // frame_cropping_flag
		w.ue(0)                               // crop_left
		w.ue(uint32((mbW*16 - e.width) / 2))  // crop_right
		w.ue(0)                               // crop_top
		w.ue(uint32((mbH*16 - e.height) / 2)) // crop_bottom
	} else {
		w.putBit(0) // frame_cropping_flag
	}
	w.putBit(0) // vui_parameters_present_flag

	rbsp := w.rbspTrailing()
	return append([]byte{0x67}, rbsp...) // NAL: nal_ref_idc=3, type=7 (SPS)
}

// h264PPS authors a minimal CAVLC PPS RBSP (NAL header type=8).
func (e *vaEncoder) h264PPS() []byte {
	w := newBitWriter()
	w.ue(0)                      // pic_parameter_set_id
	w.ue(0)                      // seq_parameter_set_id
	w.putBit(0)                  // entropy_coding_mode_flag (CAVLC)
	w.putBit(0)                  // bottom_field_pic_order_in_frame_present_flag
	w.ue(0)                      // num_slice_groups_minus1
	w.ue(0)                      // num_ref_idx_l0_default_active_minus1
	w.ue(0)                      // num_ref_idx_l1_default_active_minus1
	w.putBit(0)                  // weighted_pred_flag
	w.putBits(0, 2)              // weighted_bipred_idc
	w.se(int32(e.pickQP()) - 26) // pic_init_qp_minus26
	w.se(0)                      // pic_init_qs_minus26
	w.se(0)                      // chroma_qp_index_offset
	w.putBit(1)                  // deblocking_filter_control_present_flag
	w.putBit(0)                  // constrained_intra_pred_flag
	w.putBit(0)                  // redundant_pic_cnt_present_flag

	rbsp := w.rbspTrailing()
	return append([]byte{0x68}, rbsp...) // NAL: nal_ref_idc=3, type=8 (PPS)
}

// h264ProfileIDC maps the configured VA profile to an H.264 profile_idc.
func (e *vaEncoder) h264ProfileIDC() int {
	switch e.profile {
	case vaProfileH264ConstrainedBaseline:
		return h264ProfileBaseline
	case vaProfileH264Main:
		return h264ProfileMain
	default:
		return h264ProfileHigh
	}
}
