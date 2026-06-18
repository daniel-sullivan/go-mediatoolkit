//go:build linux || darwin

// AV1 OBU / sequence-header / frame-header bit parser for VA-API decode. A
// video.Packet for AV1 carries one temporal unit: a concatenation of
// length-delimited OBUs (each with obu_has_size_field set). This parser walks
// the OBU stream, parses the sequence-header OBU and the frame (or
// frame-header) OBU far enough to fill VADecPictureParameterBufferAV1, and
// locates the tile-group OBU payload (the slice data the hardware decodes).
//
// Only the fields the VA picture/slice buffers need are extracted; entropy
// decoding is left to the hardware. The parser targets the intra (KEY_FRAME)
// form an IVF/WebM keyframe stream and libaom/svt-av1 keyframes produce, with a
// single tile. The AV1 default loop-filter ref/mode deltas are applied when the
// frame does not update them (matching the reference iHD submission).

package hwaccel

// av1Reader is an MSB-first bit reader over an OBU payload.
type av1Reader struct {
	data []byte
	bit  int
	err  bool
}

func (r *av1Reader) f(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		bi := r.bit >> 3
		if bi >= len(r.data) {
			r.err = true
			return v << (n - 1 - i)
		}
		b := int(r.data[bi]>>uint(7-(r.bit&7))) & 1
		v = (v << 1) | b
		r.bit++
	}
	return v
}

func (r *av1Reader) bool() bool { return r.f(1) == 1 }

// su reads a signed value with n magnitude bits + sign (su(1+n) style used by
// AV1 delta_q / loop-filter deltas: f(n) then sign).
func (r *av1Reader) su(n int) int {
	v := r.f(n)
	if r.bool() {
		return v - (1 << n)
	}
	return v
}

// uvlc reads an unsigned variable-length (Exp-Golomb-like) code.
func (r *av1Reader) uvlc() int {
	lead := 0
	for !r.bool() {
		lead++
		if lead > 32 {
			r.err = true
			return 0
		}
	}
	if lead >= 32 {
		return (1 << 32) - 1
	}
	return r.f(lead) + (1 << lead) - 1
}

// ns reads ns(n): a non-symmetric unsigned encoded value in [0, n).
func (r *av1Reader) ns(n int) int {
	w := av1FloorLog2(n) + 1
	m := (1 << w) - n
	v := r.f(w - 1)
	if v < m {
		return v
	}
	extra := r.f(1)
	return (v << 1) - m + extra
}

func av1FloorLog2(x int) int {
	s := 0
	for x != 0 {
		x >>= 1
		s++
	}
	return s - 1
}

// le reads n bytes little-endian (byte-aligned).
func (r *av1Reader) le(n int) int {
	t := 0
	for i := 0; i < n; i++ {
		t |= r.f(8) << (i * 8)
	}
	return t
}

// AV1 OBU types.
const (
	av1OBUSequenceHeader = 1
	av1OBUTemporalDelim  = 2
	av1OBUFrameHeader    = 3
	av1OBUTileGroup      = 4
	av1OBUMetadata       = 5
	av1OBUFrame          = 6
	av1OBURedundantFH    = 7
	av1OBUTileList       = 8
	av1OBUPadding        = 15
)

// AV1 frame types.
const (
	av1KeyFrame       = 0
	av1InterFrame     = 1
	av1IntraOnlyFrame = 2
	av1SwitchFrame    = 3
)

// av1PrimaryRefNone is PRIMARY_REF_NONE.
const av1PrimaryRefNone = 7

// av1OBU is one parsed OBU with its payload slice and absolute byte range in
// the temporal unit.
type av1OBU struct {
	typ          int
	temporalID   int
	spatialID    int
	payload      []byte
	payloadStart int // absolute byte offset of payload in the TU
}

// av1ReadLeb128 reads a leb128 value from b at off, returning the value and the
// number of bytes consumed.
func av1ReadLeb128(b []byte, off int) (int, int) {
	v := 0
	for i := 0; i < 8; i++ {
		if off+i >= len(b) {
			return v, i
		}
		c := b[off+i]
		v |= int(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return v, 8
}

// splitAV1OBUs splits a temporal unit into its OBUs (all carrying
// obu_has_size_field, the form IVF/WebM/our encoder produce).
func splitAV1OBUs(tu []byte) []av1OBU {
	var obus []av1OBU
	off := 0
	for off < len(tu) {
		hdr := tu[off]
		// obu_forbidden_bit(1), obu_type(4), obu_extension_flag(1),
		// obu_has_size_field(1), obu_reserved_1bit(1).
		typ := int(hdr>>3) & 0xf
		extFlag := (hdr >> 2) & 1
		hasSize := (hdr >> 1) & 1
		p := off + 1
		temporalID, spatialID := 0, 0
		if extFlag == 1 {
			if p >= len(tu) {
				break
			}
			temporalID = int(tu[p]>>5) & 0x7
			spatialID = int(tu[p]>>3) & 0x3
			p++
		}
		size := 0
		if hasSize == 1 {
			n := 0
			size, n = av1ReadLeb128(tu, p)
			p += n
		} else {
			size = len(tu) - p
		}
		if p+size > len(tu) {
			break
		}
		obus = append(obus, av1OBU{
			typ:          typ,
			temporalID:   temporalID,
			spatialID:    spatialID,
			payload:      tu[p : p+size],
			payloadStart: p,
		})
		off = p + size
	}
	return obus
}

// av1SeqHeader holds the parsed sequence-header fields the picture buffer needs.
type av1SeqHeader struct {
	seqProfile              int
	stillPicture            bool
	use128x128Superblock    bool
	enableFilterIntra       bool
	enableIntraEdgeFilter   bool
	enableInterintra        bool
	enableMaskedCompound    bool
	enableWarpedMotion      bool
	enableDualFilter        bool
	enableOrderHint         bool
	enableJntComp           bool
	enableRefFrameMvs       bool
	enableSuperres          bool
	enableCdef              bool
	enableRestoration       bool
	frameWidthBitsMinus1    int
	frameHeightBitsMinus1   int
	maxFrameWidthMinus1     int
	maxFrameHeightMinus1    int
	frameIDNumbersPresent   bool
	deltaFrameIDLenMinus2   int
	additionalFrameIDLen    int
	orderHintBits           int
	bitDepth                int
	monoChrome              bool
	colorRange              int
	subsamplingX            int
	subsamplingY            int
	chromaSamplePosition    int
	matrixCoefficients      int
	separateUVDeltaQ        bool
	filmGrainPresent        bool
	reducedStillPicture     bool
	decoderModelInfoPresent bool
	equalPictureInterval    bool
	seqForceScreenContent   int
	seqForceIntegerMv       int
	seqChooseScreenContent  bool
	seqChooseIntegerMv      bool
}

// av1ColorPrimariesUnspecified / TransferUnspecified are the CICP "unspecified"
// values that trigger the explicit color_config sub-parse below.
const av1CPUnspecified = 2

// parseAV1SeqHeader parses sequence_header_obu (AV1 spec §5.5).
func parseAV1SeqHeader(payload []byte) (*av1SeqHeader, error) {
	r := &av1Reader{data: payload}
	s := &av1SeqHeader{bitDepth: 8, subsamplingX: 1, subsamplingY: 1, orderHintBits: 0}

	s.seqProfile = r.f(3)
	s.stillPicture = r.bool()
	s.reducedStillPicture = r.bool()
	if s.reducedStillPicture {
		r.f(5) // seq_level_idx[0]
	} else {
		timingInfoPresent := r.bool()
		if timingInfoPresent {
			r.f(32) // num_units_in_display_tick
			r.f(32) // time_scale
			s.equalPictureInterval = r.bool()
			if s.equalPictureInterval {
				r.uvlc() // num_ticks_per_picture_minus_1
			}
			s.decoderModelInfoPresent = r.bool()
			if s.decoderModelInfoPresent {
				r.f(5)  // buffer_delay_length_minus_1
				r.f(32) // num_units_in_decoding_tick
				r.f(5)  // buffer_removal_time_length_minus_1
				r.f(5)  // frame_presentation_time_length_minus_1
			}
		}
		initialDisplayDelayPresent := r.bool()
		operatingPointsCntMinus1 := r.f(5)
		for i := 0; i <= operatingPointsCntMinus1; i++ {
			r.f(12) // operating_point_idc[i]
			seqLevelIdx := r.f(5)
			if seqLevelIdx > 7 {
				r.f(1) // seq_tier[i]
			}
			if s.decoderModelInfoPresent {
				if r.bool() { // decoder_model_present_for_this_op
					// operating_parameters_info: skipped (buffer_delay_length unknown here;
					// the reduced/keyframe streams we target do not set this).
				}
			}
			if initialDisplayDelayPresent {
				if r.bool() { // initial_display_delay_present_for_this_op
					r.f(4)
				}
			}
		}
	}

	s.frameWidthBitsMinus1 = r.f(4)
	s.frameHeightBitsMinus1 = r.f(4)
	s.maxFrameWidthMinus1 = r.f(s.frameWidthBitsMinus1 + 1)
	s.maxFrameHeightMinus1 = r.f(s.frameHeightBitsMinus1 + 1)

	if !s.reducedStillPicture {
		s.frameIDNumbersPresent = r.bool()
	}
	if s.frameIDNumbersPresent {
		s.deltaFrameIDLenMinus2 = r.f(4)
		s.additionalFrameIDLen = r.f(3)
	}

	s.use128x128Superblock = r.bool()
	s.enableFilterIntra = r.bool()
	s.enableIntraEdgeFilter = r.bool()

	if s.reducedStillPicture {
		s.seqForceScreenContent = 2 // SELECT_SCREEN_CONTENT_TOOLS
		s.seqForceIntegerMv = 2     // SELECT_INTEGER_MV
		s.orderHintBits = 0
	} else {
		s.enableInterintra = r.bool()
		s.enableMaskedCompound = r.bool()
		s.enableWarpedMotion = r.bool()
		s.enableDualFilter = r.bool()
		s.enableOrderHint = r.bool()
		if s.enableOrderHint {
			s.enableJntComp = r.bool()
			s.enableRefFrameMvs = r.bool()
		}
		s.seqChooseScreenContent = r.bool()
		if s.seqChooseScreenContent {
			s.seqForceScreenContent = 2
		} else {
			s.seqForceScreenContent = r.f(1)
		}
		if s.seqForceScreenContent > 0 {
			s.seqChooseIntegerMv = r.bool()
			if s.seqChooseIntegerMv {
				s.seqForceIntegerMv = 2
			} else {
				s.seqForceIntegerMv = r.f(1)
			}
		} else {
			s.seqForceIntegerMv = 2
		}
		if s.enableOrderHint {
			s.orderHintBits = r.f(3) + 1
		}
	}

	s.enableSuperres = r.bool()
	s.enableCdef = r.bool()
	s.enableRestoration = r.bool()

	parseAV1ColorConfig(r, s)
	s.filmGrainPresent = r.bool()

	if r.err {
		return nil, ErrBitstreamParse
	}
	return s, nil
}

// parseAV1ColorConfig parses color_config (AV1 spec §5.5.2).
func parseAV1ColorConfig(r *av1Reader, s *av1SeqHeader) {
	highBitdepth := r.bool()
	if s.seqProfile == 2 && highBitdepth {
		twelveBit := r.bool()
		if twelveBit {
			s.bitDepth = 12
		} else {
			s.bitDepth = 10
		}
	} else if s.seqProfile <= 2 {
		if highBitdepth {
			s.bitDepth = 10
		} else {
			s.bitDepth = 8
		}
	}
	if s.seqProfile == 1 {
		s.monoChrome = false
	} else {
		s.monoChrome = r.bool()
	}
	colorDescriptionPresent := r.bool()
	colorPrimaries, transferChar, matrixCoeff := av1CPUnspecified, av1CPUnspecified, av1CPUnspecified
	if colorDescriptionPresent {
		colorPrimaries = r.f(8)
		transferChar = r.f(8)
		matrixCoeff = r.f(8)
	}
	s.matrixCoefficients = matrixCoeff
	if s.monoChrome {
		s.colorRange = r.f(1)
		s.subsamplingX, s.subsamplingY = 1, 1
		s.chromaSamplePosition = 0
		s.separateUVDeltaQ = false
		return
	}
	if colorPrimaries == 1 && transferChar == 13 && matrixCoeff == 0 {
		// sRGB
		s.colorRange = 1
		s.subsamplingX, s.subsamplingY = 0, 0
	} else {
		s.colorRange = r.f(1)
		switch s.seqProfile {
		case 0:
			s.subsamplingX, s.subsamplingY = 1, 1
		case 1:
			s.subsamplingX, s.subsamplingY = 0, 0
		default: // profile 2
			if s.bitDepth == 12 {
				s.subsamplingX = r.f(1)
				if s.subsamplingX == 1 {
					s.subsamplingY = r.f(1)
				} else {
					s.subsamplingY = 0
				}
			} else {
				s.subsamplingX, s.subsamplingY = 1, 0
			}
		}
		if s.subsamplingX == 1 && s.subsamplingY == 1 {
			s.chromaSamplePosition = r.f(2)
		}
	}
	s.separateUVDeltaQ = r.bool()
}

// av1FrameHeader holds the parsed frame-header fields for the picture buffer.
type av1FrameHeader struct {
	showExistingFrame        bool
	frameType                int
	showFrame                bool
	showableFrame            bool
	errorResilient           bool
	disableCdfUpdate         bool
	allowScreenContent       int
	forceIntegerMv           int
	frameWidth               int
	frameHeight              int
	useSuperres              bool
	superresDenom            int
	allowIntrabc             bool
	allowHighPrecision       bool
	isMotionModeSwitchable   bool
	useRefFrameMvs           bool
	disableFrameEndUpdateCdf bool
	primaryRefFrame          int
	orderHint                int

	// tile info
	uniformTileSpacing  bool
	tileColsLog2        int
	tileRowsLog2        int
	tileCols            int
	tileRows            int
	widthInSbs          []int
	heightInSbs         []int
	contextUpdateTileID int
	tileSizeBytesMinus1 int

	// quantization
	baseQIdx      int
	deltaQYDc     int
	deltaQUDc     int
	deltaQUAc     int
	deltaQVDc     int
	deltaQVAc     int
	usingQMatrix  bool
	qmY, qmU, qmV int

	// segmentation
	segEnabled        bool
	segUpdateMap      bool
	segTemporalUpdate bool
	segUpdateData     bool
	featureData       [8][8]int16
	featureMask       [8]uint8

	// delta q / lf
	deltaQPresent  bool
	deltaQRes      int
	deltaLfPresent bool
	deltaLfRes     int
	deltaLfMulti   bool

	// loop filter
	loopFilterLevel     [4]int
	sharpness           int
	modeRefDeltaEnabled bool
	modeRefDeltaUpdate  bool
	refDeltas           [8]int8
	modeDeltas          [2]int8

	// cdef
	cdefDampingMinus3 int
	cdefBits          int
	cdefYStrengths    [8]uint8
	cdefUVStrengths   [8]uint8

	// loop restoration
	lrType      [3]int
	lrUnitShift int
	lrUVShift   int

	// transform / reference mode
	txMode            int
	referenceSelect   bool
	reducedTxSet      bool
	allowWarpedMotion bool

	codedLossless bool
}

// av1DefaultRefDeltas / av1DefaultModeDeltas are the AV1 loop-filter delta
// defaults (spec §7.14 setup_past_independence / loop_filter_delta defaults).
var av1DefaultRefDeltas = [8]int8{1, 0, 0, 0, -1, 0, -1, -1}
var av1DefaultModeDeltas = [2]int8{0, 0}

// parseAV1FrameHeader parses uncompressed_header for an intra (KEY/INTRA_ONLY)
// frame (AV1 spec §5.9). Inter-frame-only syntax (ref frame selection, global
// motion, skip-mode) is not parsed; the decoder targets keyframe streams.
func parseAV1FrameHeader(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader, temporalID, spatialID int) error {
	idLen := 0
	if s.frameIDNumbersPresent {
		idLen = s.additionalFrameIDLen + s.deltaFrameIDLenMinus2 + 3
	}
	allFrames := (1 << 8) - 1

	if s.reducedStillPicture {
		fh.frameType = av1KeyFrame
		fh.showFrame = true
	} else {
		fh.showExistingFrame = r.bool()
		if fh.showExistingFrame {
			r.f(3) // frame_to_show_map_idx
			return nil
		}
		fh.frameType = r.f(2)
		fh.showFrame = r.bool()
		if fh.showFrame {
			// (decoder model / presentation time skipped — not present in target streams)
		} else {
			fh.showableFrame = r.bool()
		}
		if fh.frameType == av1SwitchFrame || (fh.frameType == av1KeyFrame && fh.showFrame) {
			fh.errorResilient = true
		} else {
			fh.errorResilient = r.bool()
		}
	}
	if fh.frameType == av1KeyFrame && fh.showFrame {
		fh.showableFrame = false
	} else if !fh.showFrame {
		// showableFrame already read
	} else {
		fh.showableFrame = true
	}

	fh.disableCdfUpdate = r.bool()
	if s.seqForceScreenContent == 2 { // SELECT_SCREEN_CONTENT_TOOLS
		fh.allowScreenContent = r.f(1)
	} else {
		fh.allowScreenContent = s.seqForceScreenContent
	}
	if fh.allowScreenContent > 0 {
		if s.seqForceIntegerMv == 2 {
			fh.forceIntegerMv = r.f(1)
		} else {
			fh.forceIntegerMv = s.seqForceIntegerMv
		}
	} else {
		fh.forceIntegerMv = 0
	}
	isIntra := fh.frameType == av1KeyFrame || fh.frameType == av1IntraOnlyFrame
	if isIntra {
		fh.forceIntegerMv = 1
	}

	if s.frameIDNumbersPresent {
		r.f(idLen) // current_frame_id
	}

	frameSizeOverride := false
	if fh.frameType == av1SwitchFrame {
		frameSizeOverride = true
	} else if !s.reducedStillPicture {
		frameSizeOverride = r.bool()
	}
	fh.orderHint = r.f(s.orderHintBits)

	if isIntra || fh.errorResilient {
		fh.primaryRefFrame = av1PrimaryRefNone
	} else {
		fh.primaryRefFrame = r.f(3)
	}

	// (decoder_model: buffer_removal_time skipped — not present)

	allowHighPrecisionMv := false
	useRefFrameMvs := false
	_ = allowHighPrecisionMv
	_ = useRefFrameMvs

	refreshFrameFlags := 0
	if fh.frameType == av1SwitchFrame || (fh.frameType == av1KeyFrame && fh.showFrame) {
		refreshFrameFlags = allFrames
	} else {
		refreshFrameFlags = r.f(8)
	}
	_ = refreshFrameFlags

	if (!fh.errorResilient && s.enableOrderHint) && (fh.frameType != av1KeyFrame || !fh.showFrame) {
		// ref_order_hint reading for refresh — only on non-key. Skipped for key.
	}

	// frame_size + render_size (intra path)
	if frameSizeOverride {
		fh.frameWidth = r.f(s.frameWidthBitsMinus1+1) + 1
		fh.frameHeight = r.f(s.frameHeightBitsMinus1+1) + 1
	} else {
		fh.frameWidth = s.maxFrameWidthMinus1 + 1
		fh.frameHeight = s.maxFrameHeightMinus1 + 1
	}
	parseAV1Superres(r, s, fh)
	// render_size
	if r.bool() { // render_and_frame_size_different
		r.f(16) // render_width_minus_1
		r.f(16) // render_height_minus_1
	}

	if !fh.forceIntegerMvBool() && !isIntra {
		fh.allowHighPrecision = false // not parsed (inter only)
	}

	if isIntra {
		// allow_intrabc
		if fh.allowScreenContent > 0 /* && UpscaledWidth==FrameWidth */ {
			fh.allowIntrabc = r.bool()
		}
	}

	// (inter-frame reference setup skipped for keyframe)

	// disable_frame_end_update_cdf
	if s.reducedStillPicture || fh.disableCdfUpdate {
		fh.disableFrameEndUpdateCdf = true
	} else {
		fh.disableFrameEndUpdateCdf = r.bool()
	}

	// tile_info
	parseAV1TileInfo(r, s, fh)
	// quantization_params
	parseAV1QuantParams(r, s, fh)
	// segmentation_params
	parseAV1SegParams(r, fh)
	// delta_q_params
	parseAV1DeltaQParams(r, fh)
	// delta_lf_params
	parseAV1DeltaLfParams(r, fh)

	// coded_lossless / loop filter / cdef / lr
	fh.codedLossless = fh.baseQIdx == 0 && fh.deltaQYDc == 0 && fh.deltaQUAc == 0 &&
		fh.deltaQUDc == 0 && fh.deltaQVAc == 0 && fh.deltaQVDc == 0
	allowIntrabc := fh.allowIntrabc

	// loop_filter_params
	fh.refDeltas = av1DefaultRefDeltas
	fh.modeDeltas = av1DefaultModeDeltas
	if fh.codedLossless || allowIntrabc {
		fh.loopFilterLevel = [4]int{0, 0, 0, 0}
	} else {
		parseAV1LoopFilterParams(r, s, fh, isIntra)
	}

	// cdef_params
	if fh.codedLossless || allowIntrabc || !s.enableCdef {
		fh.cdefBits = 0
		fh.cdefYStrengths[0] = 0
		fh.cdefUVStrengths[0] = 0
		fh.cdefDampingMinus3 = 0
	} else {
		parseAV1CdefParams(r, s, fh)
	}

	// lr_params
	if fh.codedLossless || allowIntrabc || !s.enableRestoration {
		fh.lrType = [3]int{0, 0, 0}
	} else {
		parseAV1LrParams(r, s, fh)
	}

	// read_tx_mode
	if fh.codedLossless {
		fh.txMode = 0 // ONLY_4X4
	} else {
		if r.bool() { // tx_mode_select
			fh.txMode = 2 // TX_MODE_SELECT
		} else {
			fh.txMode = 1 // TX_MODE_LARGEST
		}
	}
	// frame_reference_mode (inter only) -> reference_select stays false for intra
	// skip_mode_params (inter only)
	if !isIntra && !fh.errorResilient && s.enableWarpedMotion {
		fh.allowWarpedMotion = r.bool()
	}
	fh.reducedTxSet = r.bool()

	if r.err {
		return ErrBitstreamParse
	}
	return nil
}

func (fh *av1FrameHeader) forceIntegerMvBool() bool { return fh.forceIntegerMv == 1 }

// parseAV1Superres parses superres_params (spec §5.9.8).
func parseAV1Superres(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader) {
	const superresDenomMin = 9
	const superresDenomBits = 3
	fh.superresDenom = 8 // SUPERRES_NUM
	if s.enableSuperres {
		fh.useSuperres = r.bool()
	}
	if fh.useSuperres {
		fh.superresDenom = r.f(superresDenomBits) + superresDenomMin
	}
}

// parseAV1TileInfo parses tile_info (spec §5.9.15).
func parseAV1TileInfo(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader) {
	sbSize := 64
	if s.use128x128Superblock {
		sbSize = 128
	}
	sbCols := (fh.frameWidth + sbSize - 1) / sbSize
	sbRows := (fh.frameHeight + sbSize - 1) / sbSize
	sbShift := 6
	if s.use128x128Superblock {
		sbShift = 7
	}
	sbSizeLog2 := sbShift
	maxTileWidthSb := 4096 >> sbSizeLog2
	maxTileAreaSb := (4096 * 2304) >> (2 * sbSizeLog2)
	minLog2TileCols := av1TileLog2(maxTileWidthSb, sbCols)
	maxLog2TileCols := av1TileLog2(1, min2(sbCols, 64 /*MAX_TILE_COLS*/))
	maxLog2TileRows := av1TileLog2(1, min2(sbRows, 64 /*MAX_TILE_ROWS*/))
	minLog2Tiles := max2(minLog2TileCols, av1TileLog2(maxTileAreaSb, sbRows*sbCols))

	fh.uniformTileSpacing = r.bool()
	if fh.uniformTileSpacing {
		fh.tileColsLog2 = minLog2TileCols
		for fh.tileColsLog2 < maxLog2TileCols {
			if r.bool() {
				fh.tileColsLog2++
			} else {
				break
			}
		}
		minLog2TileRows := max2(minLog2Tiles-fh.tileColsLog2, 0)
		fh.tileRowsLog2 = minLog2TileRows
		for fh.tileRowsLog2 < maxLog2TileRows {
			if r.bool() {
				fh.tileRowsLog2++
			} else {
				break
			}
		}
		fh.tileCols = 1 << fh.tileColsLog2
		fh.tileRows = 1 << fh.tileRowsLog2
		fh.widthInSbs = uniformWidths(sbCols, fh.tileCols)
		fh.heightInSbs = uniformWidths(sbRows, fh.tileRows)
	} else {
		widestTileSb := 0
		startSb := 0
		var ws []int
		for startSb < sbCols {
			maxW := min2(sbCols-startSb, maxTileWidthSb)
			w := r.ns(maxW) + 1
			ws = append(ws, w)
			widestTileSb = max2(w, widestTileSb)
			startSb += w
		}
		fh.widthInSbs = ws
		fh.tileCols = len(ws)
		fh.tileColsLog2 = av1TileLog2(1, fh.tileCols)
		maxTileAreaSb2 := maxTileAreaSb
		maxTileHeightSb := max2(maxTileAreaSb2/max2(widestTileSb, 1), 1)
		startSb = 0
		var hs []int
		for startSb < sbRows {
			maxH := min2(sbRows-startSb, maxTileHeightSb)
			h := r.ns(maxH) + 1
			hs = append(hs, h)
			startSb += h
		}
		fh.heightInSbs = hs
		fh.tileRows = len(hs)
		fh.tileRowsLog2 = av1TileLog2(1, fh.tileRows)
	}
	if fh.tileColsLog2 > 0 || fh.tileRowsLog2 > 0 {
		fh.contextUpdateTileID = r.f(fh.tileRowsLog2 + fh.tileColsLog2)
		fh.tileSizeBytesMinus1 = r.f(2)
	} else {
		fh.contextUpdateTileID = 0
	}
}

func uniformWidths(total, parts int) []int {
	if parts <= 0 {
		return []int{total}
	}
	out := make([]int, parts)
	base := total / parts
	rem := total % parts
	for i := 0; i < parts; i++ {
		out[i] = base
		if i < rem {
			out[i]++
		}
	}
	return out
}

func av1TileLog2(blkSize, target int) int {
	k := 0
	for (blkSize << k) < target {
		k++
	}
	return k
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// parseAV1QuantParams parses quantization_params (spec §5.9.12).
func parseAV1QuantParams(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader) {
	fh.baseQIdx = r.f(8)
	fh.deltaQYDc = av1ReadDeltaQ(r)
	if !s.monoChrome {
		diffUVDelta := false
		if s.separateUVDeltaQ {
			diffUVDelta = r.bool()
		}
		fh.deltaQUDc = av1ReadDeltaQ(r)
		fh.deltaQUAc = av1ReadDeltaQ(r)
		if diffUVDelta {
			fh.deltaQVDc = av1ReadDeltaQ(r)
			fh.deltaQVAc = av1ReadDeltaQ(r)
		} else {
			fh.deltaQVDc = fh.deltaQUDc
			fh.deltaQVAc = fh.deltaQUAc
		}
	}
	fh.usingQMatrix = r.bool()
	if fh.usingQMatrix {
		fh.qmY = r.f(4)
		fh.qmU = r.f(4)
		if !s.separateUVDeltaQ {
			fh.qmV = fh.qmU
		} else {
			fh.qmV = r.f(4)
		}
	}
}

func av1ReadDeltaQ(r *av1Reader) int {
	if r.bool() { // delta_coded
		return r.su(6)
	}
	return 0
}

// parseAV1SegParams parses segmentation_params (spec §5.9.14).
func parseAV1SegParams(r *av1Reader, fh *av1FrameHeader) {
	// Segmentation feature bits / signed flags (SEG_LVL_MAX = 8).
	segFeatureBits := []int{8, 6, 6, 6, 6, 3, 0, 0}
	segFeatureSigned := []bool{true, true, true, true, true, false, false, false}
	segFeatureMax := []int{255, 63, 63, 63, 63, 7, 0, 0}

	fh.segEnabled = r.bool()
	if !fh.segEnabled {
		return
	}
	if fh.primaryRefFrame == av1PrimaryRefNone {
		fh.segUpdateMap = true
		fh.segTemporalUpdate = false
		fh.segUpdateData = true
	} else {
		fh.segUpdateMap = r.bool()
		if fh.segUpdateMap {
			fh.segTemporalUpdate = r.bool()
		}
		fh.segUpdateData = r.bool()
	}
	if fh.segUpdateData {
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				featureValue := 0
				if r.bool() { // feature_enabled
					fh.featureMask[i] |= 1 << uint(j)
					bits := segFeatureBits[j]
					if bits == 0 {
						featureValue = 0
					} else if segFeatureSigned[j] {
						featureValue = r.su(bits)
					} else {
						featureValue = r.f(bits)
						if featureValue > segFeatureMax[j] {
							featureValue = segFeatureMax[j]
						}
					}
				}
				fh.featureData[i][j] = int16(featureValue)
			}
		}
	}
}

// parseAV1DeltaQParams parses delta_q_params (spec §5.9.17).
func parseAV1DeltaQParams(r *av1Reader, fh *av1FrameHeader) {
	if fh.baseQIdx > 0 {
		fh.deltaQPresent = r.bool()
	}
	if fh.deltaQPresent {
		fh.deltaQRes = r.f(2)
	}
}

// parseAV1DeltaLfParams parses delta_lf_params (spec §5.9.18).
func parseAV1DeltaLfParams(r *av1Reader, fh *av1FrameHeader) {
	if fh.deltaQPresent {
		if !fh.allowIntrabc {
			fh.deltaLfPresent = r.bool()
		}
		if fh.deltaLfPresent {
			fh.deltaLfRes = r.f(2)
			fh.deltaLfMulti = r.bool()
		}
	}
}

// parseAV1LoopFilterParams parses loop_filter_params (spec §5.9.11).
func parseAV1LoopFilterParams(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader, isIntra bool) {
	fh.refDeltas = av1DefaultRefDeltas
	fh.modeDeltas = av1DefaultModeDeltas
	fh.loopFilterLevel[0] = r.f(6)
	fh.loopFilterLevel[1] = r.f(6)
	if !s.monoChrome {
		if fh.loopFilterLevel[0] != 0 || fh.loopFilterLevel[1] != 0 {
			fh.loopFilterLevel[2] = r.f(6)
			fh.loopFilterLevel[3] = r.f(6)
		}
	}
	fh.sharpness = r.f(3)
	fh.modeRefDeltaEnabled = r.bool()
	if fh.modeRefDeltaEnabled {
		fh.modeRefDeltaUpdate = r.bool()
		if fh.modeRefDeltaUpdate {
			for i := 0; i < 8; i++ {
				if r.bool() {
					fh.refDeltas[i] = int8(r.su(6))
				}
			}
			for i := 0; i < 2; i++ {
				if r.bool() {
					fh.modeDeltas[i] = int8(r.su(6))
				}
			}
		}
	}
}

// parseAV1CdefParams parses cdef_params (spec §5.9.19).
func parseAV1CdefParams(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader) {
	fh.cdefDampingMinus3 = r.f(2)
	fh.cdefBits = r.f(2)
	n := 1 << fh.cdefBits
	for i := 0; i < n; i++ {
		yPri := r.f(4)
		ySec := r.f(2)
		if ySec == 3 {
			ySec++
		}
		fh.cdefYStrengths[i] = uint8(yPri<<2 | ySec)
		if !s.monoChrome {
			uvPri := r.f(4)
			uvSec := r.f(2)
			if uvSec == 3 {
				uvSec++
			}
			fh.cdefUVStrengths[i] = uint8(uvPri<<2 | uvSec)
		}
	}
}

// parseAV1LrParams parses lr_params (spec §5.9.20).
func parseAV1LrParams(r *av1Reader, s *av1SeqHeader, fh *av1FrameHeader) {
	usesLr := false
	for i := 0; i < 3; i++ {
		t := r.f(2) // FrameRestorationType remap: {NONE,SWITCHABLE,WIENER,SGRPROJ}
		// remap_lr_type: 0->NONE(0),1->SWITCHABLE(3),2->WIENER(1),3->SGRPROJ(2)
		remap := []int{0, 3, 1, 2}[t]
		fh.lrType[i] = remap
		if remap != 0 {
			usesLr = true
		}
		if s.monoChrome {
			break
		}
	}
	if usesLr {
		if s.use128x128Superblock {
			fh.lrUnitShift = r.f(1) + 1
		} else {
			fh.lrUnitShift = r.f(1)
			if fh.lrUnitShift == 1 {
				fh.lrUnitShift += r.f(1)
			}
		}
		if s.subsamplingX == 1 && s.subsamplingY == 1 && (fh.lrType[1] != 0 || fh.lrType[2] != 0) {
			fh.lrUVShift = r.f(1)
		}
	}
}

// byteAlignAV1 advances the reader to the next byte boundary.
func (r *av1Reader) byteAlignAV1() {
	if r.bit&7 != 0 {
		r.bit = (r.bit + 7) &^ 7
	}
}
