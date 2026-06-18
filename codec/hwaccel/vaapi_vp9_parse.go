//go:build linux || darwin

// VP9 uncompressed-header bit parser and superframe splitter (VP9 spec §6.2,
// Annex B superframe index). Produces a vp9Header with exactly the fields the
// VADecPictureParameterBufferVP9 / per-segment buffers need. Only the
// uncompressed header is parsed; the compressed header and tile data are
// handed to the hardware verbatim as the slice-data buffer.

package hwaccel

// vp9SyncCode is the 24-bit frame sync code following the keyframe header
// fields (VP9 spec 6.2: 0x49 0x83 0x42).
const vp9SyncCode = 0x498342

// vp9Header holds the parsed VP9 uncompressed header fields the VA picture
// parameter buffer needs.
type vp9Header struct {
	profile             int
	showExisting        bool
	frameType           int // 0 = KEY_FRAME, 1 = NON_KEY_FRAME
	showFrame           bool
	errorResilient      bool
	bitDepth            int
	subsamplingX        int
	subsamplingY        int
	width               int
	height              int
	intraOnly           bool
	resetFrameContext   int
	refreshFrameFlags   int
	refFrameIdx         [3]int
	refFrameSignBias    [3]int
	allowHighPrecision  bool
	interpFilter        int // mcomp_filter_type (literal value)
	refreshFrameContext bool
	frameParallel       bool
	frameContextIdx     int
	// loop filter
	filterLevel    int
	sharpnessLevel int
	// quantization
	baseQIdx  int
	deltaYDC  int
	deltaUVDC int
	deltaUVAC int
	lossless  bool
	// segmentation
	segEnabled        bool
	segUpdateMap      bool
	segTemporalUpdate bool
	treeProbs         [7]uint8
	predProbs         [3]uint8
	// tiles
	log2TileCols int
	log2TileRows int
	// header sizing
	firstPartitionSize int
	headerSizeInBytes  int // uncompressed header length in bytes
}

// vp9Reader is a big-endian MSB-first bit reader over the uncompressed header.
type vp9Reader struct {
	data []byte
	bit  int // absolute bit position
	err  bool
}

func (r *vp9Reader) f(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		byteIdx := r.bit >> 3
		if byteIdx >= len(r.data) {
			r.err = true
			return v << (n - 1 - i)
		}
		shift := 7 - (r.bit & 7)
		b := int(r.data[byteIdx]>>uint(shift)) & 1
		v = (v << 1) | b
		r.bit++
	}
	return v
}

// s reads an n-bit value followed by a sign bit (su(n)).
func (r *vp9Reader) s(n int) int {
	v := r.f(n)
	if r.f(1) == 1 {
		return -v
	}
	return v
}

// byteAlign advances to the next byte boundary.
func (r *vp9Reader) byteAlign() {
	if r.bit&7 != 0 {
		r.bit = (r.bit + 7) &^ 7
	}
}

// splitVP9Superframe splits a packet into its constituent VP9 frames using the
// Annex-B superframe index (the optional trailing index byte set). A packet
// with no superframe index is returned as a single frame.
func splitVP9Superframe(data []byte) [][]byte {
	if len(data) == 0 {
		return nil
	}
	last := data[len(data)-1]
	// Superframe marker: top 3 bits == 0b110.
	if last&0xe0 != 0xc0 {
		return [][]byte{data}
	}
	bytesPerFrameSize := int((last>>3)&0x3) + 1
	framesInSuperframe := int(last&0x7) + 1
	indexSize := 2 + bytesPerFrameSize*framesInSuperframe
	if len(data) < indexSize {
		return [][]byte{data}
	}
	first := data[len(data)-indexSize]
	if first != last { // marker must bookend the index
		return [][]byte{data}
	}
	idx := len(data) - indexSize + 1
	var frames [][]byte
	off := 0
	for i := 0; i < framesInSuperframe; i++ {
		sz := 0
		for j := 0; j < bytesPerFrameSize; j++ {
			sz |= int(data[idx]) << (8 * j)
			idx++
		}
		if off+sz > len(data)-indexSize {
			return [][]byte{data}
		}
		frames = append(frames, data[off:off+sz])
		off += sz
	}
	return frames
}

// parseVP9UncompressedHeader parses the VP9 uncompressed header (spec §6.2).
func parseVP9UncompressedHeader(fr []byte) (*vp9Header, error) {
	if len(fr) < 1 {
		return nil, ErrBitstreamParse
	}
	r := &vp9Reader{data: fr}
	h := &vp9Header{}

	if r.f(2) != 0x2 { // frame_marker
		return nil, ErrBitstreamParse
	}
	profileLow := r.f(1)
	profileHigh := r.f(1)
	h.profile = (profileHigh << 1) | profileLow
	if h.profile == 3 {
		r.f(1) // reserved_zero
	}

	if r.f(1) == 1 { // show_existing_frame
		h.showExisting = true
		r.f(3) // frame_to_show_map_idx
		return h, nil
	}

	h.frameType = r.f(1)
	h.showFrame = r.f(1) == 1
	h.errorResilient = r.f(1) == 1

	if h.frameType == 0 { // KEY_FRAME
		if r.f(24) != vp9SyncCode {
			return nil, ErrBitstreamParse
		}
		h.parseColorConfig(r)
		h.intraOnly = false
		// frame_size
		h.width = r.f(16) + 1
		h.height = r.f(16) + 1
		h.parseRenderSize(r)
		h.refreshFrameFlags = 0xFF
	} else {
		if !h.showFrame {
			h.intraOnly = r.f(1) == 1
		}
		resetCtx := 0
		if !h.errorResilient {
			resetCtx = r.f(2)
		}
		h.resetFrameContext = resetCtx
		if h.intraOnly {
			if r.f(24) != vp9SyncCode {
				return nil, ErrBitstreamParse
			}
			if h.profile > 0 {
				h.parseColorConfig(r)
			} else {
				h.bitDepth = 8
				h.subsamplingX, h.subsamplingY = 1, 1
			}
			h.refreshFrameFlags = r.f(8)
			h.width = r.f(16) + 1
			h.height = r.f(16) + 1
			h.parseRenderSize(r)
		} else {
			h.bitDepth = 8
			h.subsamplingX, h.subsamplingY = 1, 1
			h.refreshFrameFlags = r.f(8)
			for i := 0; i < 3; i++ {
				h.refFrameIdx[i] = r.f(3)
				h.refFrameSignBias[i] = r.f(1)
			}
			// frame_size_with_refs: found_ref over 3 refs
			found := false
			for i := 0; i < 3; i++ {
				if r.f(1) == 1 {
					found = true
					break
				}
			}
			if !found {
				h.width = r.f(16) + 1
				h.height = r.f(16) + 1
			}
			h.parseRenderSize(r)
			h.allowHighPrecision = r.f(1) == 1
			h.readInterpolationFilter(r)
		}
	}

	if !h.errorResilient {
		h.refreshFrameContext = r.f(1) == 1
		h.frameParallel = r.f(1) == 1
	}
	h.frameContextIdx = r.f(2)

	h.parseLoopFilter(r)
	h.parseQuantization(r)
	h.parseSegmentation(r)
	h.parseTileInfo(r)

	h.firstPartitionSize = r.f(16)
	r.byteAlign()
	h.headerSizeInBytes = r.bit >> 3

	if r.err {
		return nil, ErrBitstreamParse
	}
	if h.bitDepth == 0 {
		h.bitDepth = 8
	}
	return h, nil
}

// parseColorConfig reads color_config (spec §6.2.2).
func (h *vp9Header) parseColorConfig(r *vp9Reader) {
	if h.profile >= 2 {
		if r.f(1) == 1 {
			h.bitDepth = 12
		} else {
			h.bitDepth = 10
		}
	} else {
		h.bitDepth = 8
	}
	colorSpace := r.f(3)
	if colorSpace != 7 { // CS_RGB
		r.f(1) // color_range
		if h.profile == 1 || h.profile == 3 {
			h.subsamplingX = r.f(1)
			h.subsamplingY = r.f(1)
			r.f(1) // reserved_zero
		} else {
			h.subsamplingX, h.subsamplingY = 1, 1
		}
	} else {
		// CS_RGB: 4:4:4
		h.subsamplingX, h.subsamplingY = 0, 0
		if h.profile == 1 || h.profile == 3 {
			r.f(1) // reserved_zero
		}
	}
}

// parseRenderSize consumes render_and_frame_size_different + render size.
func (h *vp9Header) parseRenderSize(r *vp9Reader) {
	if r.f(1) == 1 { // render_and_frame_size_different
		r.f(16) // render_width_minus_1
		r.f(16) // render_height_minus_1
	}
}

// readInterpolationFilter reads the interpolation filter (spec §6.2.10).
func (h *vp9Header) readInterpolationFilter(r *vp9Reader) {
	// is_filter_switchable
	if r.f(1) == 1 {
		h.interpFilter = 4 // SWITCHABLE
		return
	}
	// literal_to_filter map: {EIGHTTAP_SMOOTH, EIGHTTAP, EIGHTTAP_SHARP, BILINEAR}
	lit := r.f(2)
	h.interpFilter = []int{1, 0, 2, 3}[lit]
}

// parseLoopFilter reads loop_filter_params (spec §6.2.8).
func (h *vp9Header) parseLoopFilter(r *vp9Reader) {
	h.filterLevel = r.f(6)
	h.sharpnessLevel = r.f(3)
	if r.f(1) == 1 { // loop_filter_delta_enabled
		if r.f(1) == 1 { // loop_filter_delta_update
			for i := 0; i < 4; i++ { // ref deltas
				if r.f(1) == 1 {
					r.s(6)
				}
			}
			for i := 0; i < 2; i++ { // mode deltas
				if r.f(1) == 1 {
					r.s(6)
				}
			}
		}
	}
}

// parseQuantization reads quantization_params (spec §6.2.9).
func (h *vp9Header) parseQuantization(r *vp9Reader) {
	h.baseQIdx = r.f(8)
	dq := func() int {
		if r.f(1) == 1 {
			return r.s(4)
		}
		return 0
	}
	h.deltaYDC = dq()
	h.deltaUVDC = dq()
	h.deltaUVAC = dq()
	h.lossless = h.baseQIdx == 0 && h.deltaYDC == 0 && h.deltaUVDC == 0 && h.deltaUVAC == 0
}

// parseSegmentation reads segmentation_params (spec §6.2.10) capturing the
// tree/pred probs the VA picture buffer needs.
func (h *vp9Header) parseSegmentation(r *vp9Reader) {
	for i := range h.treeProbs {
		h.treeProbs[i] = 255
	}
	for i := range h.predProbs {
		h.predProbs[i] = 255
	}
	h.segEnabled = r.f(1) == 1
	if !h.segEnabled {
		return
	}
	h.segUpdateMap = r.f(1) == 1
	if h.segUpdateMap {
		for i := 0; i < 7; i++ {
			if r.f(1) == 1 {
				h.treeProbs[i] = uint8(r.f(8))
			} else {
				h.treeProbs[i] = 255
			}
		}
		h.segTemporalUpdate = r.f(1) == 1
		for i := 0; i < 3; i++ {
			if h.segTemporalUpdate && r.f(1) == 1 {
				h.predProbs[i] = uint8(r.f(8))
			} else {
				h.predProbs[i] = 255
			}
		}
	}
	if r.f(1) == 1 { // segmentation_update_data
		r.f(1) // segmentation_abs_or_delta_update
		// Segmentation feature data: SEG_LVL_MAX=4 features over 8 segments.
		bits := []int{8, 6, 2, 0}
		for i := 0; i < 8; i++ {
			for j := 0; j < 4; j++ {
				if r.f(1) == 1 { // feature_enabled
					if bits[j] > 0 {
						r.f(bits[j])
						if j < 2 { // signed features (qindex, lf level)
							r.f(1)
						}
					}
				}
			}
		}
	}
}

// parseTileInfo reads tile_info (spec §6.2.14).
func (h *vp9Header) parseTileInfo(r *vp9Reader) {
	sb64Cols := (alignUp(h.width, 64) / 64)
	minLog2 := 0
	for (64 << minLog2) < sb64Cols {
		minLog2++
	}
	maxLog2 := 1
	for (sb64Cols >> (maxLog2 + 1)) >= 1 {
		maxLog2++
	}
	// minLog2TileCols / maxLog2TileCols per spec calc_min/max_log2_tile_cols
	minLog2TileCols := 0
	for (64 << minLog2TileCols) < sb64Cols {
		minLog2TileCols++
	}
	maxLog2TileCols := 0
	for (sb64Cols >> (maxLog2TileCols + 1)) >= 1 {
		maxLog2TileCols++
	}
	log2TileCols := minLog2TileCols
	for log2TileCols < maxLog2TileCols {
		if r.f(1) == 1 { // increment_tile_cols_log2
			log2TileCols++
		} else {
			break
		}
	}
	h.log2TileCols = log2TileCols
	if r.f(1) == 1 { // tile_rows_log2
		if r.f(1) == 1 {
			h.log2TileRows = 2
		} else {
			h.log2TileRows = 1
		}
	}
	_ = minLog2
	_ = maxLog2
}
