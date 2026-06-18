//go:build linux

// HEVC bitstream parsing for the VAAPI decoder: profile_tier_level(),
// short_term_ref_pic_set(), seq/pic_parameter_set_rbsp() and
// slice_segment_header(). Only the subset needed to fill the VA parameter
// buffers for an I-slice single-segment intra picture is parsed; tool sets
// the all-intra streams do not use (scaling-list data, PCM, tiles, weighted
// prediction) are parsed structurally where needed to keep bit alignment.

package hwaccel

// skipProfileTierLevel consumes profile_tier_level(profilePresentFlag=1,
// maxNumSubLayersMinus1). It reads the general profile/level block and any
// sub-layer profile/level present flags + blocks.
func skipProfileTierLevel(r *bitReader, maxSubLayersMinus1 uint32) {
	// general: profile_space(2) tier(1) profile_idc(5), 32 compat flags,
	// 4 constraint flags + 44 reserved bits, level_idc(8).
	r.u(2)
	r.u1()
	r.u(5)
	r.u(32)
	r.u(4)
	r.u(22)
	r.u(22)
	r.u(8) // general_level_idc

	if maxSubLayersMinus1 == 0 {
		return
	}
	profPresent := make([]uint32, maxSubLayersMinus1)
	levelPresent := make([]uint32, maxSubLayersMinus1)
	for i := uint32(0); i < maxSubLayersMinus1; i++ {
		profPresent[i] = r.u1()
		levelPresent[i] = r.u1()
	}
	if maxSubLayersMinus1 > 0 {
		for i := maxSubLayersMinus1; i < 8; i++ {
			r.u(2) // reserved_zero_2bits
		}
	}
	for i := uint32(0); i < maxSubLayersMinus1; i++ {
		if profPresent[i] == 1 {
			r.u(2)
			r.u1()
			r.u(5)
			r.u(32)
			r.u(4)
			r.u(22)
			r.u(22)
		}
		if levelPresent[i] == 1 {
			r.u(8)
		}
	}
}

// skipShortTermRPS consumes short_term_ref_pic_set(idx). numRPS is the total
// number of sets declared in the SPS; prevNumDelta is the NumDeltaPocs of the
// previous set (for inter-RPS prediction). Returns this set's NumDeltaPocs.
func skipShortTermRPS(r *bitReader, idx, numRPS, prevNumDelta uint32) uint32 {
	interPred := uint32(0)
	if idx != 0 {
		interPred = r.u1()
	}
	if interPred == 1 {
		// delta_idx only when idx == numRPS (the slice-header case); for SPS
		// sets idx < numRPS so it is absent.
		if idx == numRPS {
			r.ue()
		}
		r.u1() // delta_rps_sign
		r.ue() // abs_delta_rps_minus1
		num := uint32(0)
		for j := uint32(0); j <= prevNumDelta; j++ {
			used := r.u1()
			if used == 0 {
				if r.u1() == 1 { // use_delta_flag
					num++
				}
			} else {
				num++
			}
		}
		return num
	}
	numNeg := r.ue()
	numPos := r.ue()
	for i := uint32(0); i < numNeg; i++ {
		r.ue() // delta_poc_s0_minus1
		r.u1() // used_by_curr_pic_s0
	}
	for i := uint32(0); i < numPos; i++ {
		r.ue() // delta_poc_s1_minus1
		r.u1() // used_by_curr_pic_s1
	}
	return numNeg + numPos
}

// parseHEVCSPS parses the subset of an SPS RBSP (NAL header already stripped)
// the decoder needs.
func parseHEVCSPS(rbsp []byte) hevcSPS {
	r := newBitReader(rbsp)
	var s hevcSPS

	r.u(4) // sps_video_parameter_set_id
	maxSubLayersMinus1 := r.u(3)
	r.u1() // sps_temporal_id_nesting_flag
	skipProfileTierLevel(r, maxSubLayersMinus1)

	r.ue() // sps_seq_parameter_set_id
	s.chromaFormatIDC = r.ue()
	if s.chromaFormatIDC == 3 {
		s.separateColourPlane = r.u1()
	}
	s.picWidthInLumaSamples = r.ue()
	s.picHeightInLumaSamples = r.ue()
	if r.u1() == 1 { // conformance_window_flag
		s.confWinLeft = r.ue()
		s.confWinRight = r.ue()
		s.confWinTop = r.ue()
		s.confWinBottom = r.ue()
	}
	s.bitDepthLumaMinus8 = r.ue()
	s.bitDepthChromaMinus8 = r.ue()
	s.log2MaxPocLsbMinus4 = r.ue()

	subLayerOrderingPresent := r.u1()
	start := maxSubLayersMinus1
	if subLayerOrderingPresent == 1 {
		start = 0
	}
	for i := start; i <= maxSubLayersMinus1; i++ {
		s.spsMaxDecPicBufferingMinus1 = r.ue()
		r.ue() // sps_max_num_reorder_pics
		r.ue() // sps_max_latency_increase_plus1
	}

	s.log2MinLumaCBMinus3 = r.ue()
	s.log2DiffMaxMinLumaCB = r.ue()
	s.log2MinTransformBlockMinus2 = r.ue()
	s.log2DiffMaxMinTransformBlock = r.ue()
	s.maxTransformHierarchyDepthInter = r.ue()
	s.maxTransformHierarchyDepthIntra = r.ue()
	s.scalingListEnabled = r.u1()
	if s.scalingListEnabled == 1 {
		if r.u1() == 1 { // sps_scaling_list_data_present_flag
			skipScalingListData(r)
		}
	}
	s.ampEnabled = r.u1()
	s.saoEnabled = r.u1()
	s.pcmEnabled = r.u1()
	if s.pcmEnabled == 1 {
		s.pcmSampleBitDepthLumaMinus1 = r.u(4)
		s.pcmSampleBitDepthChromaMinus1 = r.u(4)
		s.log2MinPcmCBMinus3 = r.ue()
		s.log2DiffMaxMinPcmCB = r.ue()
		s.pcmLoopFilterDisabled = r.u1()
	}
	s.numShortTermRPS = r.ue()
	prevNumDelta := uint32(0)
	for i := uint32(0); i < s.numShortTermRPS; i++ {
		prevNumDelta = skipShortTermRPS(r, i, s.numShortTermRPS, prevNumDelta)
	}
	s.longTermRefPicsPresent = r.u1()
	if s.longTermRefPicsPresent == 1 {
		n := r.ue() // num_long_term_ref_pics_sps
		for i := uint32(0); i < n; i++ {
			r.u(int(s.log2MaxPocLsbMinus4) + 4) // lt_ref_pic_poc_lsb_sps
			r.u1()                              // used_by_curr_pic_lt_sps_flag
		}
	}
	s.spsTemporalMvpEnabled = r.u1()
	s.strongIntraSmoothing = r.u1()
	// vui and extensions are not needed.
	return s
}

// parseHEVCPPS parses the subset of a PPS RBSP (NAL header stripped).
func parseHEVCPPS(rbsp []byte) hevcPPS {
	r := newBitReader(rbsp)
	var p hevcPPS

	r.ue() // pps_pic_parameter_set_id
	r.ue() // pps_seq_parameter_set_id
	p.dependentSliceSegmentsEnabled = r.u1()
	p.outputFlagPresent = r.u1()
	p.numExtraSliceHeaderBits = r.u(3)
	p.signDataHiding = r.u1()
	p.cabacInitPresent = r.u1()
	p.numRefIdxL0DefaultMinus1 = r.ue()
	p.numRefIdxL1DefaultMinus1 = r.ue()
	p.initQPMinus26 = r.se()
	p.constrainedIntraPred = r.u1()
	p.transformSkipEnabled = r.u1()
	p.cuQPDeltaEnabled = r.u1()
	if p.cuQPDeltaEnabled == 1 {
		p.diffCuQPDeltaDepth = r.ue()
	}
	p.ppsCbQPOffset = r.se()
	p.ppsCrQPOffset = r.se()
	p.ppsSliceChromaQPOffsetsPresent = r.u1()
	p.weightedPred = r.u1()
	p.weightedBipred = r.u1()
	p.transquantBypassEnabled = r.u1()
	p.tilesEnabled = r.u1()
	p.entropyCodingSyncEnabled = r.u1()
	if p.tilesEnabled == 1 {
		numTileCols := r.ue() // num_tile_columns_minus1
		numTileRows := r.ue() // num_tile_rows_minus1
		if r.u1() == 0 {      // uniform_spacing_flag
			for i := uint32(0); i < numTileCols; i++ {
				r.ue()
			}
			for i := uint32(0); i < numTileRows; i++ {
				r.ue()
			}
		}
		r.u1() // loop_filter_across_tiles_enabled_flag
	}
	p.ppsLoopFilterAcrossSlices = r.u1()
	p.deblockingFilterControlPresent = r.u1()
	if p.deblockingFilterControlPresent == 1 {
		p.deblockingFilterOverrideEnabled = r.u1()
		p.ppsDeblockingFilterDisabled = r.u1()
		if p.ppsDeblockingFilterDisabled == 0 {
			p.ppsBetaOffsetDiv2 = r.se()
			p.ppsTcOffsetDiv2 = r.se()
		}
	}
	if r.u1() == 1 { // pps_scaling_list_data_present_flag
		p.scalingListDataPresent = 1
		skipScalingListData(r)
	}
	p.listsModificationPresent = r.u1()
	p.log2ParallelMergeLevelMinus2 = r.ue()
	p.sliceSegmentHeaderExtPresent = r.u1()
	return p
}

// hevcSliceHeader holds the parsed slice_segment_header() fields plus the
// byte offset of slice_data().
type hevcSliceHeader struct {
	sliceType         uint32
	saoLuma           uint32
	saoChroma         uint32
	sliceQPDelta      int32
	sliceCbQPOffset   int32
	sliceCrQPOffset   int32
	betaOffsetDiv2    int32
	tcOffsetDiv2      int32
	fiveMinusMaxMerge uint32
	headerBytes       int
}

// parseHEVCSliceHeader parses an I-slice (intra) first-segment
// slice_segment_header() and records the byte offset where slice_data()
// begins. Only the intra path is exercised by the all-intra streams.
func (d *vaDecoder) parseHEVCSliceHeader(sliceNAL []byte, nalType int) hevcSliceHeader {
	sps := &d.hevc.sps
	pps := &d.hevc.pps

	rbsp := ebspToRBSP(sliceNAL)
	r := newBitReader(rbsp)
	r.u(16) // 2-byte NAL header

	var sh hevcSliceHeader
	firstSlice := r.u1() // first_slice_segment_in_pic_flag
	if nalType >= 16 && nalType <= 23 {
		r.u1() // no_output_of_prior_pics_flag
	}
	r.ue() // slice_pic_parameter_set_id

	dependent := uint32(0)
	if firstSlice == 0 {
		if pps.dependentSliceSegmentsEnabled == 1 {
			dependent = r.u1()
		}
		// slice_segment_address: ceil(log2(PicSizeInCtbsY)) bits. For a
		// single-segment intra picture firstSlice==1, so this branch is not
		// taken; left unparsed.
		_ = dependent
	}

	if dependent == 0 {
		for i := uint32(0); i < pps.numExtraSliceHeaderBits; i++ {
			r.u1()
		}
		sh.sliceType = r.ue()
		if pps.outputFlagPresent == 1 {
			r.u1() // pic_output_flag
		}
		if sps.separateColourPlane == 1 {
			r.u(2) // colour_plane_id
		}
		idr := nalType == hevcNalIDRWRADL || nalType == hevcNalIDRNLP
		if !idr {
			r.u(int(sps.log2MaxPocLsbMinus4) + 4) // slice_pic_order_cnt_lsb
			// short_term_ref_pic_set handling and reference lists belong to
			// inter slices; the all-intra streams never reach here.
		}
		if sps.saoEnabled == 1 {
			sh.saoLuma = r.u1()
			if sps.chromaFormatIDC != 0 {
				sh.saoChroma = r.u1()
			}
		}
		// Inter-prediction syntax (ref lists, weights, merge cand) is absent
		// for I slices (slice_type == 2).
		sh.sliceQPDelta = r.se()
		if pps.ppsSliceChromaQPOffsetsPresent == 1 {
			sh.sliceCbQPOffset = r.se()
			sh.sliceCrQPOffset = r.se()
		}
		deblockOverride := uint32(0)
		if pps.deblockingFilterOverrideEnabled == 1 {
			deblockOverride = r.u1()
		}
		if deblockOverride == 1 {
			if r.u1() == 0 { // slice_deblocking_filter_disabled_flag
				sh.betaOffsetDiv2 = r.se()
				sh.tcOffsetDiv2 = r.se()
			}
		} else {
			sh.betaOffsetDiv2 = pps.ppsBetaOffsetDiv2
			sh.tcOffsetDiv2 = pps.ppsTcOffsetDiv2
		}
		if pps.ppsLoopFilterAcrossSlices == 1 &&
			(sh.saoLuma == 1 || sh.saoChroma == 1 || pps.ppsDeblockingFilterDisabled == 0) {
			r.u1() // slice_loop_filter_across_slices_enabled_flag
		}
	}

	// slice_segment_header() ends with byte_alignment(): alignment_bit_equal
	// _to_one then zero bits to a byte boundary. slice_data() is byte-aligned.
	r.u1() // alignment_bit_equal_to_one
	for r.pos%8 != 0 {
		r.u1()
	}
	sh.headerBytes = r.pos / 8
	return sh
}

// skipScalingListData consumes scaling_list_data() structurally.
func skipScalingListData(r *bitReader) {
	for sizeID := 0; sizeID < 4; sizeID++ {
		step := 1
		if sizeID == 3 {
			step = 3
		}
		for matrixID := 0; matrixID < 6; matrixID += step {
			if r.u1() == 0 { // scaling_list_pred_mode_flag == 0
				r.ue() // scaling_list_pred_matrix_id_delta
			} else {
				coefNum := 64
				if sizeID == 0 {
					coefNum = 16
				}
				if sizeID > 1 {
					r.se() // scaling_list_dc_coef_minus8
				}
				for i := 0; i < coefNum; i++ {
					r.se() // scaling_list_delta_coef
				}
			}
		}
	}
}
