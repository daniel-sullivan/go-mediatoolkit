//go:build linux

// Full HEVC bitstream parsing for the stateless V4L2 decoder. Unlike the
// VAAPI decoder's intra-only subset (vaapi_decode_hevc_parse_linux.go),
// the Pi-5 rpi-hevc-dec hardware parses nothing itself: userspace must
// supply every field of the SPS, PPS, slice header (including the inter
// path — reference-picture-set derivation, POC, reference-index counts,
// weighted-prediction tables) and the per-frame DPB so the kernel
// Request-API controls are fully populated. This file parses the whole
// syntax the H.265 spec needs to drive a frame-based stateless decode of
// I, P, and B pictures.
//
// The reader reuses the Annex-B bitReader / Exp-Golomb helpers in
// vaapi_nal_linux.go (ebspToRBSP, newBitReader, u/ue/se).
//
// Spec references are ITU-T H.265 clause numbers.

package hwaccel

// HEVC NAL unit types not already declared by the VAAPI decoder
// (vaapi_decode_hevc_linux.go declares IDRWRADL=19, IDRNLP=20, VPS=32,
// SPS=33, PPS=34, AUD=35). This set extends those with the leading /
// trailing / IRAP types the full inter-capable parse classifies.
const (
	hevcNalTrailN   = 0
	hevcNalTrailR   = 1
	hevcNalTSAN     = 2
	hevcNalTSAR     = 3
	hevcNalSTSAN    = 4
	hevcNalSTSAR    = 5
	hevcNalRADLN    = 6
	hevcNalRADLR    = 7
	hevcNalRASLN    = 8
	hevcNalRASLR    = 9
	hevcNalBLAWLP   = 16
	hevcNalBLAWRADL = 17
	hevcNalBLANLP   = 18
	hevcNalCRANUT   = 21
)

// nalUnitType extracts the 6-bit NAL unit type from the 2-byte HEVC NAL
// header (forbidden_zero(1) type(6) layer_id(6) temporal_id_plus1(3)).
func nalUnitType(nal []byte) int {
	if len(nal) < 2 {
		return -1
	}
	return int(nal[0]>>1) & 0x3f
}

// nalTemporalIDPlus1 extracts nuh_temporal_id_plus1 from the NAL header.
func nalTemporalIDPlus1(nal []byte) uint8 {
	if len(nal) < 2 {
		return 1
	}
	return nal[1] & 0x07
}

// isIRAP reports whether nalType is an intra random-access point
// (BLA/IDR/CRA), per H.265 Table 7-1.
func isIRAP(nalType int) bool {
	return nalType >= hevcNalBLAWLP && nalType <= hevcNalCRANUT
}

// isIDR reports whether nalType is an IDR picture.
func isIDR(nalType int) bool {
	return nalType == hevcNalIDRWRADL || nalType == hevcNalIDRNLP
}

// isBLA reports whether nalType is a BLA picture.
func isBLA(nalType int) bool {
	return nalType >= hevcNalBLAWLP && nalType <= hevcNalBLANLP
}

// stRPS is one parsed short_term_ref_pic_set: the delta POCs and used
// flags split into the "before" (negative) and "after" (positive) sets,
// per H.265 7.4.8.
type stRPS struct {
	numNegative  int
	numPositive  int
	deltaPocS0   [16]int32 // negative (before) deltas, ascending magnitude
	usedS0       [16]bool
	deltaPocS1   [16]int32 // positive (after) deltas
	usedS1       [16]bool
	numDeltaPocs int
}

// hevcVUIPresent is unused structurally; VUI is skipped.

// hevcFullSPS holds the SPS fields the stateless controls and the RPS /
// POC derivation need.
type hevcFullSPS struct {
	v4l2CtrlHEVCSPS
	maxSubLayersMinus1 uint32
	confWinLeft        uint32
	confWinRight       uint32
	confWinTop         uint32
	confWinBottom      uint32
	stRPSList          []stRPS
	ltRefPicPOCLsb     []uint32
	ltUsedByCurr       []bool
	parsed             bool
}

// hevcFullPPS holds the PPS fields the stateless controls and slice-header
// parse need.
type hevcFullPPS struct {
	v4l2CtrlHEVCPPS
	listsModificationPresent bool
	cabacInitPresent         bool
	weightedPred             bool
	weightedBipred           bool
	parsed                   bool
}

// parseFullSPS parses a seq_parameter_set_rbsp() (NAL header already
// stripped from rbsp) into the v4l2 SPS control plus the auxiliary RPS /
// conformance-window state.
func parseFullSPS(rbsp []byte) hevcFullSPS {
	r := newBitReader(rbsp)
	var s hevcFullSPS

	r.u(16) // 2-byte HEVC NAL unit header
	s.VideoParameterSetID = uint8(r.u(4))
	s.maxSubLayersMinus1 = r.u(3)
	s.SPSMaxSubLayersMinus1 = uint8(s.maxSubLayersMinus1)
	r.u1() // sps_temporal_id_nesting_flag
	skipProfileTierLevel(r, s.maxSubLayersMinus1)

	s.SeqParameterSetID = uint8(r.ue())
	s.ChromaFormatIDC = uint8(r.ue())
	var flags uint64
	if s.ChromaFormatIDC == 3 {
		if r.u1() == 1 {
			flags |= hevcSPSFlagSeparateColourPlane
		}
	}
	s.PicWidthInLumaSamples = uint16(r.ue())
	s.PicHeightInLumaSamples = uint16(r.ue())
	if r.u1() == 1 { // conformance_window_flag
		s.confWinLeft = r.ue()
		s.confWinRight = r.ue()
		s.confWinTop = r.ue()
		s.confWinBottom = r.ue()
	}
	s.BitDepthLumaMinus8 = uint8(r.ue())
	s.BitDepthChromaMinus8 = uint8(r.ue())
	s.Log2MaxPicOrderCntLsbMinus4 = uint8(r.ue())

	subLayerOrdering := r.u1()
	start := s.maxSubLayersMinus1
	if subLayerOrdering == 1 {
		start = 0
	}
	for i := start; i <= s.maxSubLayersMinus1; i++ {
		s.SPSMaxDecPicBufferingMinus1 = uint8(r.ue())
		s.SPSMaxNumReorderPics = uint8(r.ue())
		s.SPSMaxLatencyIncreasePlus1 = uint8(r.ue())
	}

	s.Log2MinLumaCodingBlockSizeMinus3 = uint8(r.ue())
	s.Log2DiffMaxMinLumaCodingBlockSize = uint8(r.ue())
	s.Log2MinLumaTransformBlockSizeMinus2 = uint8(r.ue())
	s.Log2DiffMaxMinLumaTransformBlockSize = uint8(r.ue())
	s.MaxTransformHierarchyDepthInter = uint8(r.ue())
	s.MaxTransformHierarchyDepthIntra = uint8(r.ue())
	if r.u1() == 1 { // scaling_list_enabled_flag
		flags |= hevcSPSFlagScalingListEnabled
		if r.u1() == 1 { // sps_scaling_list_data_present_flag
			skipScalingListData(r)
		}
	}
	if r.u1() == 1 { // amp_enabled_flag
		flags |= hevcSPSFlagAmpEnabled
	}
	if r.u1() == 1 { // sample_adaptive_offset_enabled_flag
		flags |= hevcSPSFlagSampleAdaptiveOffset
	}
	if r.u1() == 1 { // pcm_enabled_flag
		flags |= hevcSPSFlagPCMEnabled
		s.PCMSampleBitDepthLumaMinus1 = uint8(r.u(4))
		s.PCMSampleBitDepthChromaMinus1 = uint8(r.u(4))
		s.Log2MinPCMLumaCodingBlockSizeMinus3 = uint8(r.ue())
		s.Log2DiffMaxMinPCMLumaCodingBlockSize = uint8(r.ue())
		if r.u1() == 1 { // pcm_loop_filter_disabled_flag
			flags |= hevcSPSFlagPCMLoopFilterDisabled
		}
	}

	numRPS := r.ue()
	s.NumShortTermRefPicSets = uint8(numRPS)
	s.stRPSList = make([]stRPS, numRPS)
	for i := uint32(0); i < numRPS; i++ {
		s.stRPSList[i] = parseShortTermRPS(r, i, numRPS, s.stRPSList)
	}

	if r.u1() == 1 { // long_term_ref_pics_present_flag
		flags |= hevcSPSFlagLongTermRefPicsPresent
		n := r.ue()
		s.NumLongTermRefPicsSPS = uint8(n)
		for i := uint32(0); i < n; i++ {
			s.ltRefPicPOCLsb = append(s.ltRefPicPOCLsb, r.u(int(s.Log2MaxPicOrderCntLsbMinus4)+4))
			s.ltUsedByCurr = append(s.ltUsedByCurr, r.u1() == 1)
		}
	}
	if r.u1() == 1 { // sps_temporal_mvp_enabled_flag
		flags |= hevcSPSFlagSPSTemporalMvpEnabled
	}
	if r.u1() == 1 { // strong_intra_smoothing_enabled_flag
		flags |= hevcSPSFlagStrongIntraSmoothing
	}
	// VUI and extensions are not needed by the hardware.
	s.Flags = flags
	s.parsed = true
	return s
}

// parseShortTermRPS parses short_term_ref_pic_set(idx). For idx>0 it may
// be inter-predicted from a previous set; full derivation per H.265
// 7.4.8 so the resulting (deltaPoc, used) lists are exact — the stateless
// decoder needs these to build the per-frame DPB and POC lists.
func parseShortTermRPS(r *bitReader, idx, numRPS uint32, list []stRPS) stRPS {
	var rps stRPS
	interPred := uint32(0)
	if idx != 0 {
		interPred = r.u1()
	}
	if interPred == 1 {
		deltaIdxMinus1 := uint32(0)
		if idx == numRPS {
			deltaIdxMinus1 = r.ue()
		}
		refIdx := int(idx) - 1 - int(deltaIdxMinus1)
		deltaRPSSign := r.u1()
		absDeltaRPSMinus1 := r.ue()
		deltaRPS := int32(1 + absDeltaRPSMinus1)
		if deltaRPSSign == 1 {
			deltaRPS = -deltaRPS
		}
		ref := list[refIdx]
		refNumDelta := ref.numDeltaPocs

		if refIdx < 0 || refIdx >= len(list) {
			return rps // desync guard
		}
		usedFlags := make([]bool, refNumDelta+1)
		useDelta := make([]bool, refNumDelta+1)
		for j := 0; j <= refNumDelta; j++ {
			usedFlags[j] = r.u1() == 1
			useDelta[j] = true
			if !usedFlags[j] {
				useDelta[j] = r.u1() == 1
			}
		}
		// Derive S1 (positive / after) then S0 (negative / before) exactly
		// per H.265 (7-59)/(7-61).
		i := 0
		for j := ref.numPositive - 1; j >= 0; j-- {
			dPoc := ref.deltaPocS1[j] + deltaRPS
			k := ref.numNegative + j
			if i < v4l2HEVCDPBEntriesMax && dPoc < 0 && useDelta[k] {
				rps.deltaPocS0[i] = dPoc
				rps.usedS0[i] = usedFlags[k]
				i++
			}
		}
		if i < v4l2HEVCDPBEntriesMax && deltaRPS < 0 && useDelta[refNumDelta] {
			rps.deltaPocS0[i] = deltaRPS
			rps.usedS0[i] = usedFlags[refNumDelta]
			i++
		}
		for j := 0; j < ref.numNegative; j++ {
			dPoc := ref.deltaPocS0[j] + deltaRPS
			if i < v4l2HEVCDPBEntriesMax && dPoc < 0 && useDelta[j] {
				rps.deltaPocS0[i] = dPoc
				rps.usedS0[i] = usedFlags[j]
				i++
			}
		}
		rps.numNegative = i

		i = 0
		for j := ref.numNegative - 1; j >= 0; j-- {
			dPoc := ref.deltaPocS0[j] + deltaRPS
			if i < v4l2HEVCDPBEntriesMax && dPoc > 0 && useDelta[j] {
				rps.deltaPocS1[i] = dPoc
				rps.usedS1[i] = usedFlags[j]
				i++
			}
		}
		if i < v4l2HEVCDPBEntriesMax && deltaRPS > 0 && useDelta[refNumDelta] {
			rps.deltaPocS1[i] = deltaRPS
			rps.usedS1[i] = usedFlags[refNumDelta]
			i++
		}
		for j := 0; j < ref.numPositive; j++ {
			dPoc := ref.deltaPocS1[j] + deltaRPS
			k := ref.numNegative + j
			if i < v4l2HEVCDPBEntriesMax && dPoc > 0 && useDelta[k] {
				rps.deltaPocS1[i] = dPoc
				rps.usedS1[i] = usedFlags[k]
				i++
			}
		}
		rps.numPositive = i
		rps.numDeltaPocs = rps.numNegative + rps.numPositive
		return rps
	}

	numNeg := int(r.ue())
	numPos := int(r.ue())
	// NumNegativePics / NumPositivePics are bounded by
	// sps_max_dec_pic_buffering_minus1 (<= 15), so > 16 means the reader
	// desynced; clamp to the array bound to avoid a panic (the caller's
	// decode will fail downstream rather than corrupt memory).
	if numNeg > v4l2HEVCDPBEntriesMax {
		numNeg = v4l2HEVCDPBEntriesMax
	}
	if numPos > v4l2HEVCDPBEntriesMax {
		numPos = v4l2HEVCDPBEntriesMax
	}
	rps.numNegative = numNeg
	rps.numPositive = numPos
	var prev int32
	for i := 0; i < numNeg; i++ {
		d := int32(r.ue() + 1)
		prev -= d
		rps.deltaPocS0[i] = prev
		rps.usedS0[i] = r.u1() == 1
	}
	prev = 0
	for i := 0; i < numPos; i++ {
		d := int32(r.ue() + 1)
		prev += d
		rps.deltaPocS1[i] = prev
		rps.usedS1[i] = r.u1() == 1
	}
	rps.numDeltaPocs = rps.numNegative + rps.numPositive
	return rps
}

// parseFullPPS parses a pic_parameter_set_rbsp() (NAL header stripped)
// into the v4l2 PPS control plus the slice-header-relevant flags.
func parseFullPPS(rbsp []byte) hevcFullPPS {
	r := newBitReader(rbsp)
	var p hevcFullPPS
	var flags uint64

	r.u(16) // 2-byte HEVC NAL unit header
	p.PicParameterSetID = uint8(r.ue())
	r.ue() // pps_seq_parameter_set_id
	if r.u1() == 1 {
		flags |= hevcPPSFlagDependentSliceSegment
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagOutputFlagPresent
	}
	p.NumExtraSliceHeaderBits = uint8(r.u(3))
	if r.u1() == 1 {
		flags |= hevcPPSFlagSignDataHiding
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagCabacInitPresent
		p.cabacInitPresent = true
	}
	p.NumRefIdxL0DefaultActiveMinus1 = uint8(r.ue())
	p.NumRefIdxL1DefaultActiveMinus1 = uint8(r.ue())
	p.InitQPMinus26 = int8(r.se())
	if r.u1() == 1 {
		flags |= hevcPPSFlagConstrainedIntraPred
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagTransformSkipEnabled
	}
	if r.u1() == 1 { // cu_qp_delta_enabled_flag
		flags |= hevcPPSFlagCuQPDeltaEnabled
		p.DiffCuQPDeltaDepth = uint8(r.ue())
	}
	p.PPSCbQPOffset = int8(r.se())
	p.PPSCrQPOffset = int8(r.se())
	if r.u1() == 1 {
		flags |= hevcPPSFlagSliceChromaQPOffsetsPresent
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagWeightedPred
		p.weightedPred = true
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagWeightedBipred
		p.weightedBipred = true
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagTransquantBypassEnabled
	}
	tilesEnabled := r.u1() == 1
	if tilesEnabled {
		flags |= hevcPPSFlagTilesEnabled
	}
	if r.u1() == 1 {
		flags |= hevcPPSFlagEntropyCodingSyncEnabled
	}
	if tilesEnabled {
		p.NumTileColumnsMinus1 = uint8(r.ue())
		p.NumTileRowsMinus1 = uint8(r.ue())
		if r.u1() == 1 { // uniform_spacing_flag
			flags |= hevcPPSFlagUniformSpacing
		} else {
			for i := 0; i < int(p.NumTileColumnsMinus1); i++ {
				v := r.ue()
				if i < len(p.ColumnWidthMinus1) {
					p.ColumnWidthMinus1[i] = uint8(v)
				}
			}
			for i := 0; i < int(p.NumTileRowsMinus1); i++ {
				v := r.ue()
				if i < len(p.RowHeightMinus1) {
					p.RowHeightMinus1[i] = uint8(v)
				}
			}
		}
		if r.u1() == 1 { // loop_filter_across_tiles_enabled_flag
			flags |= hevcPPSFlagLoopFilterAcrossTiles
		}
	} else {
		// Default uniform spacing for the single-tile case.
		flags |= hevcPPSFlagUniformSpacing
	}
	if r.u1() == 1 { // pps_loop_filter_across_slices_enabled_flag
		flags |= hevcPPSFlagLoopFilterAcrossSlices
	}
	if r.u1() == 1 { // deblocking_filter_control_present_flag
		flags |= hevcPPSFlagDeblockingFilterControl
		if r.u1() == 1 { // deblocking_filter_override_enabled_flag
			flags |= hevcPPSFlagDeblockingFilterOverride
		}
		if r.u1() == 1 { // pps_deblocking_filter_disabled_flag
			flags |= hevcPPSFlagDisableDeblockingFilter
		} else {
			p.PPSBetaOffsetDiv2 = int8(r.se())
			p.PPSTcOffsetDiv2 = int8(r.se())
		}
	}
	if r.u1() == 1 { // pps_scaling_list_data_present_flag
		skipScalingListData(r)
	}
	if r.u1() == 1 { // lists_modification_present_flag
		flags |= hevcPPSFlagListsModificationPresent
		p.listsModificationPresent = true
	}
	p.Log2ParallelMergeLevelMinus2 = uint8(r.ue())
	if r.u1() == 1 { // slice_segment_header_extension_present_flag
		flags |= hevcPPSFlagSliceSegmentHeaderExtension
	}
	p.Flags = flags
	p.parsed = true
	return p
}
