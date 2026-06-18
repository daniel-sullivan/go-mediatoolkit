//go:build linux

// VP9 VLD decode for VA-API. VP9 is not Annex-B: a video.Packet carries one
// coded VP9 frame or a superframe (a superframe-index-delimited concatenation
// of frames, the last of which is shown). This file parses the VP9
// uncompressed header (6.2 frame_size / loop-filter / quantization /
// segmentation / tile syntax) far enough to fill
// VADecPictureParameterBufferVP9 + the eight VASegmentParameterVP9 entries and
// submits the whole frame as the slice data buffer, then reads back the
// decoded NV12 surface — mirroring the H.264/H.265 VLD structure in
// vaapi_decode_linux.go.
//
// The decoder targets the intra (key) frame form an IVF/WebM keyframe stream
// produces and the self-encode round trip emits; inter frames decode too (the
// reference surfaces are tracked in a small ring) but reference management is
// deliberately minimal.

package hwaccel

import (
	"unsafe"

	"go-mediatoolkit/video"
)

// decodeVP9 splits the packet into VP9 frames (handling a superframe index),
// decodes each, and returns the shown frame(s).
func (d *vaDecoder) decodeVP9(data []byte) ([]video.Frame, error) {
	frames := splitVP9Superframe(data)
	var out []video.Frame
	for _, fr := range frames {
		h, err := parseVP9UncompressedHeader(fr)
		if err != nil {
			return nil, err
		}
		if h.showExisting {
			// show_existing_frame: re-display a stored reference. The minimal
			// decoder does not maintain the show ring, so this is skipped.
			continue
		}
		f, shown, err := d.decodeVP9Frame(fr, h)
		if err != nil {
			return nil, err
		}
		if shown {
			out = append(out, f)
		}
	}
	return out, nil
}

// decodeVP9Frame configures the VA pipeline from the header, submits the
// frame, and (if shown) reads back the surface.
func (d *vaDecoder) decodeVP9Frame(fr []byte, h *vp9Header) (video.Frame, bool, error) {
	if h.width <= 0 || h.height <= 0 {
		return video.Frame{}, false, ErrBitstreamParse
	}
	profile := vaProfileVP9Profile0
	switch h.profile {
	case 1:
		profile = vaProfileVP9Profile1
	case 2:
		profile = vaProfileVP9Profile2
	case 3:
		profile = vaProfileVP9Profile3
	}
	if err := d.ensureContext(profile, h.width, h.height); err != nil {
		return video.Frame{}, false, err
	}

	var bufs []uint32
	defer func() { d.freeDecodeBufs(bufs) }()

	pic := buildVP9PicParam(d.surface, h)
	if err := d.addDecodeBuf(&bufs, vaPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return video.Frame{}, false, err
	}
	slice := buildVP9SliceParam(fr, h)
	if err := d.addDecodeBuf(&bufs, vaSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return video.Frame{}, false, err
	}
	if err := d.addDecodeBuf(&bufs, vaSliceDataBufferType, len(fr), unsafe.Pointer(&fr[0])); err != nil {
		return video.Frame{}, false, err
	}
	if err := d.submitPicture(bufs); err != nil {
		return video.Frame{}, false, err
	}
	if !h.showFrame {
		return video.Frame{}, false, nil
	}
	f, err := d.readSurface(h.width, h.height)
	if err != nil {
		return video.Frame{}, false, err
	}
	d.frameIdx++
	return f, true, nil
}

// buildVP9PicParam fills VADecPictureParameterBufferVP9 from the parsed header.
func buildVP9PicParam(surface uint32, h *vp9Header) vaDecPictureParameterBufferVP9 {
	var pic vaDecPictureParameterBufferVP9
	pic.FrameWidth = uint16(h.width)
	pic.FrameHeight = uint16(h.height)
	for i := range pic.ReferenceFrames {
		pic.ReferenceFrames[i] = vaInvalidSurface
	}

	// pic_fields (va_dec_vp9.h): subsampling_x(0), subsampling_y(1),
	// frame_type(2), show_frame(3), error_resilient_mode(4), intra_only(5),
	// allow_high_precision_mv(6), mcomp_filter_type(7..9),
	// frame_parallel_decoding_mode(10), reset_frame_context(11..12),
	// refresh_frame_context(13), frame_context_idx(14..15),
	// segmentation_enabled(16), segmentation_temporal_update(17),
	// segmentation_update_map(18), last_ref_frame(19..21),
	// last_ref_frame_sign_bias(22), golden_ref_frame(23..25),
	// golden_ref_frame_sign_bias(26), alt_ref_frame(27..29),
	// alt_ref_frame_sign_bias(30), lossless_flag(31).
	pic.PicFields = uint32(h.subsamplingX) |
		uint32(h.subsamplingY)<<1 |
		uint32(h.frameType)<<2 |
		boolU32(h.showFrame)<<3 |
		boolU32(h.errorResilient)<<4 |
		boolU32(h.intraOnly)<<5 |
		boolU32(h.allowHighPrecision)<<6 |
		uint32(h.interpFilter&0x7)<<7 |
		boolU32(h.frameParallel)<<10 |
		uint32(h.resetFrameContext&0x3)<<11 |
		boolU32(h.refreshFrameContext)<<13 |
		uint32(h.frameContextIdx&0x3)<<14 |
		boolU32(h.segEnabled)<<16 |
		boolU32(h.segTemporalUpdate)<<17 |
		boolU32(h.segUpdateMap)<<18 |
		uint32(h.refFrameIdx[0]&0x7)<<19 |
		uint32(h.refFrameSignBias[0]&0x1)<<22 |
		uint32(h.refFrameIdx[1]&0x7)<<23 |
		uint32(h.refFrameSignBias[1]&0x1)<<26 |
		uint32(h.refFrameIdx[2]&0x7)<<27 |
		uint32(h.refFrameSignBias[2]&0x1)<<30 |
		boolU32(h.lossless)<<31

	pic.FilterLevel = uint8(h.filterLevel)
	pic.SharpnessLevel = uint8(h.sharpnessLevel)
	pic.Log2TileRows = uint8(h.log2TileRows)
	pic.Log2TileColumns = uint8(h.log2TileCols)
	pic.FrameHeaderLengthInByte = uint8(h.headerSizeInBytes)
	pic.FirstPartitionSize = uint16(h.firstPartitionSize)
	pic.MbSegmentTreeProbs = h.treeProbs
	pic.SegmentPredProbs = h.predProbs
	pic.Profile = uint8(h.profile)
	pic.BitDepth = uint8(h.bitDepth)
	return pic
}

// buildVP9SliceParam fills VASliceParameterBufferVP9. The whole coded frame is
// the slice data; seg_param[0] carries the frame-level dequantization step
// sizes the hardware applies (luma/chroma AC/DC quant scales, derived from
// base_q_idx and the per-plane deltas via the VP9 dequant tables). These are
// REQUIRED even when segmentation is disabled — the driver maps every block to
// segment 0, and leaving its quant scales zero makes the decoder dequantize
// every coefficient to zero (a blank surface). Segments 1..7 stay zero for the
// segmentation-disabled streams the decoder targets.
func buildVP9SliceParam(fr []byte, h *vp9Header) vaSliceParameterBufferVP9 {
	var sp vaSliceParameterBufferVP9
	sp.SliceDataSize = uint32(len(fr))
	sp.SliceDataOffset = 0
	sp.SliceDataFlag = vaSliceDataFlagAll

	// Frame-level dequant steps (segment 0). luma AC uses base_q_idx directly;
	// luma DC, chroma DC and chroma AC apply their signed deltas.
	sp.SegParam[0].LumaACQuantScale = vp9ACQuant(h.baseQIdx)
	sp.SegParam[0].LumaDCQuantScale = vp9DCQuant(h.baseQIdx + h.deltaYDC)
	sp.SegParam[0].ChromaACQuantScale = vp9ACQuant(h.baseQIdx + h.deltaUVAC)
	sp.SegParam[0].ChromaDCQuantScale = vp9DCQuant(h.baseQIdx + h.deltaUVDC)
	return sp
}
