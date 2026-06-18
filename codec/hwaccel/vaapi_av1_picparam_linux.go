//go:build linux

// buildAV1PicParam fills VADecPictureParameterBufferAV1 from a parsed AV1
// sequence header + frame header. The bitfield-union value words
// (seq_info_fields, pic_info_fields, loop_filter_info_fields, qmatrix_fields,
// mode_control_fields, loop_restoration_fields) are assembled with the exact
// shifts from va_dec_av1.h, validated field-by-field against the iHD reference
// submission recovered by LIBVA_TRACE-diffing ffmpeg's av1 hwaccel.

package hwaccel

// av1BitDepthIdx maps a bit depth to VADecPictureParameterBufferAV1.bit_depth_idx
// (0 = 8-bit, 1 = 10-bit, 2 = 12-bit).
func av1BitDepthIdx(bd int) uint8 {
	switch bd {
	case 10:
		return 1
	case 12:
		return 2
	default:
		return 0
	}
}

func buildAV1PicParam(surface uint32, s *av1SeqHeader, fh *av1FrameHeader) vaDecPictureParameterBufferAV1 {
	var pic vaDecPictureParameterBufferAV1

	pic.Profile = uint8(s.seqProfile)
	pic.OrderHintBitsMinus1 = uint8(maxI(s.orderHintBits-1, 0))
	pic.BitDepthIdx = av1BitDepthIdx(s.bitDepth)
	pic.MatrixCoefficients = uint8(s.matrixCoefficients)

	// seq_info_fields (va_dec_av1.h): still_picture(0), use_128x128_superblock(1),
	// enable_filter_intra(2), enable_intra_edge_filter(3),
	// enable_interintra_compound(4), enable_masked_compound(5),
	// enable_dual_filter(6), enable_order_hint(7), enable_jnt_comp(8),
	// enable_cdef(9), mono_chrome(10), color_range(11), subsampling_x(12),
	// subsampling_y(13), chroma_sample_position(14, deprecated),
	// film_grain_params_present(15).
	pic.SeqInfoFields = boolU32(s.stillPicture) |
		boolU32(s.use128x128Superblock)<<1 |
		boolU32(s.enableFilterIntra)<<2 |
		boolU32(s.enableIntraEdgeFilter)<<3 |
		boolU32(s.enableInterintra)<<4 |
		boolU32(s.enableMaskedCompound)<<5 |
		boolU32(s.enableDualFilter)<<6 |
		boolU32(s.enableOrderHint)<<7 |
		boolU32(s.enableJntComp)<<8 |
		boolU32(s.enableCdef)<<9 |
		boolU32(s.monoChrome)<<10 |
		uint32(s.colorRange&1)<<11 |
		uint32(s.subsamplingX&1)<<12 |
		uint32(s.subsamplingY&1)<<13 |
		uint32(s.chromaSamplePosition&1)<<14 |
		boolU32(s.filmGrainPresent)<<15

	pic.CurrentFrame = surface
	pic.CurrentDisplayPicture = surface
	pic.AnchorFramesNum = 0
	pic.AnchorFramesList = 0

	pic.FrameWidthMinus1 = uint16(fh.frameWidth - 1)
	pic.FrameHeightMinus1 = uint16(fh.frameHeight - 1)
	pic.OutputFrameWidthInTilesMinus1 = 0
	pic.OutputFrameHeightInTilesMinus1 = 0

	for i := range pic.RefFrameMap {
		pic.RefFrameMap[i] = vaInvalidSurface
	}
	// ref_frame_idx all 0 for a keyframe.
	pic.PrimaryRefFrame = uint8(fh.primaryRefFrame)
	pic.OrderHint = uint8(fh.orderHint)

	// seg_info
	pic.SegInfo.SegmentInfoFields = boolU32(fh.segEnabled) |
		boolU32(fh.segUpdateMap)<<1 |
		boolU32(fh.segTemporalUpdate)<<2 |
		boolU32(fh.segUpdateData)<<3
	pic.SegInfo.FeatureData = fh.featureData
	pic.SegInfo.FeatureMask = fh.featureMask

	// film_grain_info: zero (film_grain_params_present=0 in target streams).

	pic.TileCols = uint8(fh.tileCols)
	pic.TileRows = uint8(fh.tileRows)
	for i, w := range fh.widthInSbs {
		if i >= len(pic.WidthInSbsMinus1) {
			break
		}
		pic.WidthInSbsMinus1[i] = uint16(w - 1)
	}
	for i, h := range fh.heightInSbs {
		if i >= len(pic.HeightInSbsMinus1) {
			break
		}
		pic.HeightInSbsMinus1[i] = uint16(h - 1)
	}
	pic.TileCountMinus1 = uint16(fh.tileCols*fh.tileRows - 1)
	pic.ContextUpdateTileID = uint16(fh.contextUpdateTileID)

	// pic_info_fields (va_dec_av1.h): frame_type(0..1), show_frame(2),
	// showable_frame(3), error_resilient_mode(4), disable_cdf_update(5),
	// allow_screen_content_tools(6), force_integer_mv(7), allow_intrabc(8),
	// use_superres(9), allow_high_precision_mv(10), is_motion_mode_switchable(11),
	// use_ref_frame_mvs(12), disable_frame_end_update_cdf(13),
	// uniform_tile_spacing_flag(14), allow_warped_motion(15),
	// large_scale_tile(16).
	pic.PicInfoFields = uint32(fh.frameType&0x3) |
		boolU32(fh.showFrame)<<2 |
		boolU32(fh.showableFrame)<<3 |
		boolU32(fh.errorResilient)<<4 |
		boolU32(fh.disableCdfUpdate)<<5 |
		uint32(boolI(fh.allowScreenContent > 0))<<6 |
		uint32(fh.forceIntegerMv&1)<<7 |
		boolU32(fh.allowIntrabc)<<8 |
		boolU32(fh.useSuperres)<<9 |
		boolU32(fh.allowHighPrecision)<<10 |
		boolU32(fh.isMotionModeSwitchable)<<11 |
		boolU32(fh.useRefFrameMvs)<<12 |
		boolU32(fh.disableFrameEndUpdateCdf)<<13 |
		boolU32(fh.uniformTileSpacing)<<14 |
		boolU32(fh.allowWarpedMotion)<<15

	pic.SuperresScaleDenominator = uint8(fh.superresDenom)
	pic.InterpFilter = 0 // intra: interp filter unused
	pic.FilterLevel[0] = uint8(fh.loopFilterLevel[0])
	pic.FilterLevel[1] = uint8(fh.loopFilterLevel[1])
	pic.FilterLevelU = uint8(fh.loopFilterLevel[2])
	pic.FilterLevelV = uint8(fh.loopFilterLevel[3])

	// loop_filter_info_fields (uint8 union): sharpness_level(0..2),
	// mode_ref_delta_enabled(3), mode_ref_delta_update(4).
	pic.LoopFilterInfoFields = uint8(fh.sharpness&0x7) |
		boolU8(fh.modeRefDeltaEnabled)<<3 |
		boolU8(fh.modeRefDeltaUpdate)<<4

	pic.RefDeltas = fh.refDeltas
	pic.ModeDeltas = fh.modeDeltas

	pic.BaseQindex = uint8(fh.baseQIdx)
	pic.YDCDeltaQ = int8(fh.deltaQYDc)
	pic.UDCDeltaQ = int8(fh.deltaQUDc)
	pic.UACDeltaQ = int8(fh.deltaQUAc)
	pic.VDCDeltaQ = int8(fh.deltaQVDc)
	pic.VACDeltaQ = int8(fh.deltaQVAc)

	// qmatrix_fields (uint16 union): using_qmatrix(0), qm_y(1..4), qm_u(5..8),
	// qm_v(9..12).
	pic.QMatrixFields = boolU16(fh.usingQMatrix) |
		uint16(fh.qmY&0xf)<<1 |
		uint16(fh.qmU&0xf)<<5 |
		uint16(fh.qmV&0xf)<<9

	// mode_control_fields (uint32 union): delta_q_present_flag(0),
	// log2_delta_q_res(1..2), delta_lf_present_flag(3), log2_delta_lf_res(4..5),
	// delta_lf_multi(6), tx_mode(7..8), reference_select(9),
	// reduced_tx_set_used(10), skip_mode_present(11).
	pic.ModeControlFields = boolU32(fh.deltaQPresent) |
		uint32(fh.deltaQRes&0x3)<<1 |
		boolU32(fh.deltaLfPresent)<<3 |
		uint32(fh.deltaLfRes&0x3)<<4 |
		boolU32(fh.deltaLfMulti)<<6 |
		uint32(fh.txMode&0x3)<<7 |
		boolU32(fh.referenceSelect)<<9 |
		boolU32(fh.reducedTxSet)<<10

	pic.CdefDampingMinus3 = uint8(fh.cdefDampingMinus3)
	pic.CdefBits = uint8(fh.cdefBits)
	pic.CdefYStrengths = fh.cdefYStrengths
	pic.CdefUVStrengths = fh.cdefUVStrengths

	// loop_restoration_fields (uint16 union): yframe_restoration_type(0..1),
	// cbframe_restoration_type(2..3), crframe_restoration_type(4..5),
	// lr_unit_shift(6..7), lr_uv_shift(8).
	pic.LoopRestorationFields = uint16(fh.lrType[0]&0x3) |
		uint16(fh.lrType[1]&0x3)<<2 |
		uint16(fh.lrType[2]&0x3)<<4 |
		uint16(fh.lrUnitShift&0x3)<<6 |
		uint16(fh.lrUVShift&0x1)<<8

	// wm[7]: identity (default) for an intra frame; left zero (wmtype=IDENTITY=0).
	return pic
}

func maxI(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func boolI(b bool) int {
	if b {
		return 1
	}
	return 0
}

func boolU8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

func boolU16(b bool) uint16 {
	if b {
		return 1
	}
	return 0
}
