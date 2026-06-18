//go:build linux

// HEVC slice_segment_header() parsing for the stateless V4L2 decoder,
// including the inter (P/B) path the VAAPI intra-only parser omits:
// slice_pic_order_cnt_lsb, the in-slice short_term_ref_pic_set, long-term
// reference signalling, temporal-MVP / collocated, ref-idx active counts,
// ref_pic_list_modification, the pred_weight_table, and five_minus_max_
// num_merge_cand. The result feeds both the per-slice control payload and
// the picture-level POC / DPB derivation in v4l2_stateless_hevc_linux.go.
//
// Spec references are ITU-T H.265 clause 7.3.6 / 7.4.7.

package hwaccel

// parsedSlice is the fully-parsed slice header plus the derived state the
// stateless controls need: the slice-params payload, the in-effect RPS,
// and the byte offset where slice_data() begins.
type parsedSlice struct {
	params          v4l2CtrlHEVCSliceParams
	firstSlice      bool
	nalType         int
	picOrderLsb     uint32 // slice_pic_order_cnt_lsb (0 for IDR)
	rps             stRPS  // the short-term RPS in effect for this picture
	hasLTPocLsb     []uint32
	ltUsedCurr      []bool
	dataOffsetBytes int
	rpsSizeBits     int // short_term_ref_pic_set_size in bits
}

// parseSliceHeader parses a first-or-dependent slice_segment_header() for
// any slice type. sps/pps are the active parameter sets. The full inter
// path is parsed so the slice-params control and POC/DPB derivation are
// exact for P and B pictures.
func parseSliceHeader(sliceNAL []byte, sps *hevcFullSPS, pps *hevcFullPPS) parsedSlice {
	nalType := nalUnitType(sliceNAL)
	rbsp := ebspToRBSP(sliceNAL)
	r := newBitReader(rbsp)

	var ps parsedSlice
	ps.nalType = nalType
	sp := &ps.params
	sp.NalUnitType = uint8(nalType)
	sp.NuhTemporalIDPlus1 = nalTemporalIDPlus1(sliceNAL)

	r.u(16) // 2-byte NAL header

	firstSlice := r.u1() == 1
	ps.firstSlice = firstSlice
	if isIRAP(nalType) {
		if r.u1() == 1 { // no_output_of_prior_pics_flag
			// recorded at picture level via decode-params flag
		}
	}
	r.ue() // slice_pic_parameter_set_id

	var flags uint64
	dependent := false
	ctbLog2 := ctbLog2SizeY(sps)
	picSizeInCtbs := picSizeInCtbsY(sps, ctbLog2)
	if !firstSlice {
		if pps.Flags&hevcPPSFlagDependentSliceSegment != 0 {
			dependent = r.u1() == 1
		}
		addrBits := ceilLog2(picSizeInCtbs)
		sp.SliceSegmentAddr = r.u(addrBits)
	}
	if dependent {
		flags |= hevcSliceFlagDependentSliceSegment
		sp.Flags = flags
		// A dependent slice inherits the independent slice's header; only
		// the address differs. The caller carries forward the prior
		// header's derived fields.
		ps.dataOffsetBytes = byteAlign(r)
		sp.DataByteOffset = uint32(ps.dataOffsetBytes)
		return ps
	}

	for i := uint32(0); i < uint32(pps.NumExtraSliceHeaderBits); i++ {
		r.u1()
	}
	sliceType := r.ue()
	sp.SliceType = uint8(sliceType)

	if pps.Flags&hevcPPSFlagOutputFlagPresent != 0 {
		r.u1() // pic_output_flag
	}
	if sps.Flags&hevcSPSFlagSeparateColourPlane != 0 {
		sp.ColourPlaneID = uint8(r.u(2))
	}

	numL0 := uint32(pps.NumRefIdxL0DefaultActiveMinus1) + 1
	numL1 := uint32(pps.NumRefIdxL1DefaultActiveMinus1) + 1

	if !isIDR(nalType) {
		ps.picOrderLsb = r.u(int(sps.Log2MaxPicOrderCntLsbMinus4) + 4)

		// short_term_ref_pic_set_sps_flag
		startBits := r.pos
		if r.u1() == 0 { // not from SPS: parse an in-slice RPS
			idx := uint32(len(sps.stRPSList))
			ps.rps = parseShortTermRPS(r, idx, idx, sps.stRPSList)
		} else {
			// st_rps_idx via ceil(log2(num_short_term_ref_pic_sets)) bits.
			n := len(sps.stRPSList)
			if n > 1 {
				idx := r.u(ceilLog2(uint32(n)))
				if int(idx) < n {
					ps.rps = sps.stRPSList[idx]
				}
			} else if n == 1 {
				ps.rps = sps.stRPSList[0]
			}
		}
		ps.rpsSizeBits = r.pos - startBits
		sp.ShortTermRefPicSetSize = uint16(ps.rpsSizeBits)

		// Long-term reference picture handling.
		if sps.Flags&hevcSPSFlagLongTermRefPicsPresent != 0 {
			ltStart := r.pos
			numLTSps := uint32(0)
			if sps.NumLongTermRefPicsSPS > 0 {
				numLTSps = r.ue()
			}
			numLTPics := r.ue()
			for i := uint32(0); i < numLTSps+numLTPics; i++ {
				if i < numLTSps {
					bits := ceilLog2(uint32(sps.NumLongTermRefPicsSPS))
					if bits > 0 {
						r.u(bits) // lt_idx_sps
					}
				} else {
					r.u(int(sps.Log2MaxPicOrderCntLsbMinus4) + 4) // poc_lsb_lt
					r.u1()                                        // used_by_curr_pic_lt_flag
				}
				if r.u1() == 1 { // delta_poc_msb_present_flag
					r.ue() // delta_poc_msb_cycle_lt
				}
			}
			sp.LongTermRefPicSetSize = uint16(r.pos - ltStart)
		}

		if sps.Flags&hevcSPSFlagSPSTemporalMvpEnabled != 0 {
			if r.u1() == 1 { // slice_temporal_mvp_enabled_flag
				flags |= hevcSliceFlagTemporalMvpEnabled
			}
		}
	}

	// SAO.
	if sps.Flags&hevcSPSFlagSampleAdaptiveOffset != 0 {
		if r.u1() == 1 {
			flags |= hevcSliceFlagSAOLuma
		}
		if sps.ChromaFormatIDC != 0 {
			if r.u1() == 1 {
				flags |= hevcSliceFlagSAOChroma
			}
		}
	}

	if sliceType == hevcSliceTypeP || sliceType == hevcSliceTypeB {
		if r.u1() == 1 { // num_ref_idx_active_override_flag
			numL0 = r.ue() + 1
			if sliceType == hevcSliceTypeB {
				numL1 = r.ue() + 1
			}
		}
		sp.NumRefIdxL0ActiveMinus1 = uint8(numL0 - 1)
		if sliceType == hevcSliceTypeB {
			sp.NumRefIdxL1ActiveMinus1 = uint8(numL1 - 1)
		}

		// ref_pic_lists_modification: present when lists_modification and
		// NumPicTotalCurr > 1.
		numPicTotalCurr := ps.rps.numNegative + ps.rps.numPositive
		if pps.listsModificationPresent && numPicTotalCurr > 1 {
			parseRefPicListModification(r, numL0, numPicTotalCurr)
			if sliceType == hevcSliceTypeB {
				parseRefPicListModification(r, numL1, numPicTotalCurr)
			}
		}

		if sliceType == hevcSliceTypeB {
			if r.u1() == 1 { // mvd_l1_zero_flag
				flags |= hevcSliceFlagMvdL1Zero
			}
		}
		if pps.cabacInitPresent {
			if r.u1() == 1 { // cabac_init_flag
				flags |= hevcSliceFlagCabacInit
			}
		}
		if flags&hevcSliceFlagTemporalMvpEnabled != 0 {
			collocatedFromL0 := true
			if sliceType == hevcSliceTypeB {
				collocatedFromL0 = r.u1() == 1
			}
			if collocatedFromL0 {
				flags |= hevcSliceFlagCollocatedFromL0
			}
			needCollocatedRefIdx := (collocatedFromL0 && numL0 > 1) ||
				(!collocatedFromL0 && numL1 > 1)
			if needCollocatedRefIdx {
				sp.CollocatedRefIdx = uint8(r.ue())
			}
		}
		if (pps.weightedPred && sliceType == hevcSliceTypeP) ||
			(pps.weightedBipred && sliceType == hevcSliceTypeB) {
			parsePredWeightTable(r, sp, int(numL0), int(numL1), int(sliceType), sps)
		}
		sp.FiveMinusMaxNumMergeCand = uint8(r.ue())
	}

	sp.SliceQPDelta = int8(r.se())
	if pps.Flags&hevcPPSFlagSliceChromaQPOffsetsPresent != 0 {
		sp.SliceCbQPOffset = int8(r.se())
		sp.SliceCrQPOffset = int8(r.se())
	}

	deblockOverride := false
	if pps.Flags&hevcPPSFlagDeblockingFilterOverride != 0 {
		deblockOverride = r.u1() == 1
	}
	if deblockOverride {
		if r.u1() == 1 { // slice_deblocking_filter_disabled_flag
			flags |= hevcSliceFlagDeblockingFilterDisabled
		} else {
			sp.SliceBetaOffsetDiv2 = int8(r.se())
			sp.SliceTcOffsetDiv2 = int8(r.se())
		}
	} else {
		sp.SliceBetaOffsetDiv2 = pps.PPSBetaOffsetDiv2
		sp.SliceTcOffsetDiv2 = pps.PPSTcOffsetDiv2
		if pps.Flags&hevcPPSFlagDisableDeblockingFilter != 0 {
			flags |= hevcSliceFlagDeblockingFilterDisabled
		}
	}

	if pps.Flags&hevcPPSFlagLoopFilterAcrossSlices != 0 &&
		(flags&hevcSliceFlagSAOLuma != 0 || flags&hevcSliceFlagSAOChroma != 0 ||
			flags&hevcSliceFlagDeblockingFilterDisabled == 0) {
		if r.u1() == 1 {
			flags |= hevcSliceFlagLoopFilterAcrossSlices
		}
	} else if pps.Flags&hevcPPSFlagLoopFilterAcrossSlices != 0 {
		flags |= hevcSliceFlagLoopFilterAcrossSlices
	}

	// num_entry_point_offsets (tiles / WPP). Parsed structurally to keep
	// alignment; the slice-params control carries the count.
	if pps.Flags&hevcPPSFlagTilesEnabled != 0 || pps.Flags&hevcPPSFlagEntropyCodingSyncEnabled != 0 {
		n := r.ue()
		sp.NumEntryPointOffsets = n
		if n > 0 {
			lenMinus1 := r.ue()
			for i := uint32(0); i < n; i++ {
				r.u(int(lenMinus1) + 1)
			}
		}
	}

	if pps.Flags&hevcPPSFlagSliceSegmentHeaderExtension != 0 {
		extLen := r.ue()
		for i := uint32(0); i < extLen; i++ {
			r.u(8)
		}
	}

	sp.Flags = flags
	ps.dataOffsetBytes = byteAlign(r)
	sp.DataByteOffset = uint32(ps.dataOffsetBytes)
	return ps
}

// parseRefPicListModification consumes ref_pic_list_modification for one
// list: list_entry_lX[i] each ceil(log2(NumPicTotalCurr)) bits, gated by
// ref_pic_list_modification_flag_lX.
func parseRefPicListModification(r *bitReader, numRefIdx uint32, numPicTotalCurr int) {
	if r.u1() == 1 { // ref_pic_list_modification_flag_lX
		bits := ceilLog2(uint32(numPicTotalCurr))
		for i := uint32(0); i < numRefIdx; i++ {
			if bits > 0 {
				r.u(bits)
			}
		}
	}
}

// parsePredWeightTable parses pred_weight_table() into the slice-params
// embedded weight table (H.265 7.3.6.3).
func parsePredWeightTable(r *bitReader, sp *v4l2CtrlHEVCSliceParams, numL0, numL1, sliceType int, sps *hevcFullSPS) {
	w := &sp.PredWeightTable
	w.LumaLog2WeightDenom = uint8(r.ue())
	if sps.ChromaFormatIDC != 0 {
		w.DeltaChromaLog2WeightDenom = int8(r.se())
	}
	parseWeightList(r, numL0, sps, w.DeltaLumaWeightL0[:], w.LumaOffsetL0[:],
		w.DeltaChromaWeightL0[:], w.ChromaOffsetL0[:])
	if sliceType == hevcSliceTypeB {
		parseWeightList(r, numL1, sps, w.DeltaLumaWeightL1[:], w.LumaOffsetL1[:],
			w.DeltaChromaWeightL1[:], w.ChromaOffsetL1[:])
	}
}

// parseWeightList parses one reference list's luma/chroma weight flags and
// deltas.
func parseWeightList(r *bitReader, num int, sps *hevcFullSPS,
	deltaLuma []int8, lumaOff []int8, deltaChroma [][2]int8, chromaOff [][2]int8) {
	lumaFlag := make([]bool, num)
	chromaFlag := make([]bool, num)
	for i := 0; i < num && i < v4l2HEVCDPBEntriesMax; i++ {
		lumaFlag[i] = r.u1() == 1
	}
	if sps.ChromaFormatIDC != 0 {
		for i := 0; i < num && i < v4l2HEVCDPBEntriesMax; i++ {
			chromaFlag[i] = r.u1() == 1
		}
	}
	for i := 0; i < num && i < v4l2HEVCDPBEntriesMax; i++ {
		if lumaFlag[i] {
			deltaLuma[i] = int8(r.se())
			lumaOff[i] = int8(r.se())
		}
		if chromaFlag[i] {
			for j := 0; j < 2; j++ {
				deltaChroma[i][j] = int8(r.se())
				chromaOff[i][j] = int8(r.se())
			}
		}
	}
}

// byteAlign consumes byte_alignment() and returns the byte offset where
// slice_data() begins.
func byteAlign(r *bitReader) int {
	r.u1() // alignment_bit_equal_to_one
	for r.pos%8 != 0 {
		r.u1()
	}
	return r.pos / 8
}

// ---- geometry helpers -------------------------------------------------

// ctbLog2SizeY returns CtbLog2SizeY = log2MinCB + log2DiffMaxMinCB.
func ctbLog2SizeY(sps *hevcFullSPS) int {
	return int(sps.Log2MinLumaCodingBlockSizeMinus3) + 3 +
		int(sps.Log2DiffMaxMinLumaCodingBlockSize)
}

// picSizeInCtbsY returns PicSizeInCtbsY = PicWidthInCtbsY *
// PicHeightInCtbsY.
func picSizeInCtbsY(sps *hevcFullSPS, ctbLog2 int) uint32 {
	ctb := uint32(1) << ctbLog2
	wCtbs := (uint32(sps.PicWidthInLumaSamples) + ctb - 1) / ctb
	hCtbs := (uint32(sps.PicHeightInLumaSamples) + ctb - 1) / ctb
	return wCtbs * hCtbs
}

// ceilLog2 returns ceil(log2(n)), the bit width to address n values.
func ceilLog2(n uint32) int {
	if n <= 1 {
		return 0
	}
	bits := 0
	v := n - 1
	for v > 0 {
		bits++
		v >>= 1
	}
	return bits
}
