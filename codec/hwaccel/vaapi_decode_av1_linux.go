//go:build linux

// AV1 VLD decode for VA-API. A video.Packet carries one AV1 temporal unit (an
// OBU stream). This file walks the OBUs, parses the sequence-header and
// frame(-header) OBUs to fill VADecPictureParameterBufferAV1 (sequence tools,
// frame size, quantization, segmentation, loop-filter, cdef, loop-restoration,
// tile layout), locates the tile-group OBU payload as the slice data, and reads
// back the decoded NV12 surface. Mirrors the H.264/H.265/VP9 VLD structure.
//
// The decoder targets the intra (KEY_FRAME) single-tile form an IVF/WebM
// keyframe stream produces; reference management is minimal (all ref slots
// invalid for a keyframe).

package hwaccel

import (
	"unsafe"

	"github.com/daniel-sullivan/go-mediatoolkit/video"
)

// av1Decoder state carried across OBUs within a temporal unit.
type av1DecodeState struct {
	seq     *av1SeqHeader
	haveSeq bool
}

// decodeAV1 walks the temporal unit's OBUs, decodes the frame, and returns the
// shown frame.
func (d *vaDecoder) decodeAV1(tu []byte) ([]video.Frame, error) {
	obus := splitAV1OBUs(tu)
	if len(obus) == 0 {
		return nil, ErrBitstreamParse
	}

	var seq *av1SeqHeader
	if d.av1Seq != nil {
		seq = d.av1Seq
	}
	var fh *av1FrameHeader
	var tileData []byte // the tile-group OBU payload (slice data)
	var tileDataOffset int

	for _, obu := range obus {
		switch obu.typ {
		case av1OBUSequenceHeader:
			s, err := parseAV1SeqHeader(obu.payload)
			if err != nil {
				return nil, err
			}
			seq = s
			d.av1Seq = s
		case av1OBUFrameHeader, av1OBURedundantFH:
			if seq == nil {
				return nil, ErrParameterSetsMissing
			}
			h := &av1FrameHeader{}
			r := &av1Reader{data: obu.payload}
			if err := parseAV1FrameHeader(r, seq, h, obu.temporalID, obu.spatialID); err != nil {
				return nil, err
			}
			fh = h
		case av1OBUFrame:
			if seq == nil {
				return nil, ErrParameterSetsMissing
			}
			h := &av1FrameHeader{}
			r := &av1Reader{data: obu.payload}
			if err := parseAV1FrameHeader(r, seq, h, obu.temporalID, obu.spatialID); err != nil {
				return nil, err
			}
			fh = h
			// The tile group follows the frame header in the same OBU, byte
			// aligned. The remaining payload (from the byte after the header) is
			// the tile-group data.
			r.byteAlignAV1()
			hdrBytes := r.bit >> 3
			tileData = obu.payload[hdrBytes:]
			tileDataOffset = 0
		case av1OBUTileGroup:
			tileData = obu.payload
			tileDataOffset = 0
		}
	}

	if seq == nil || fh == nil {
		return nil, ErrParameterSetsMissing
	}
	if fh.showExistingFrame {
		return nil, nil
	}
	if tileData == nil {
		return nil, ErrBitstreamParse
	}

	f, shown, err := d.decodeAV1Frame(seq, fh, tileData, tileDataOffset)
	if err != nil {
		return nil, err
	}
	if shown {
		return []video.Frame{f}, nil
	}
	return nil, nil
}

// decodeAV1Frame configures the pipeline, submits the picture + tile data, and
// reads back the surface.
func (d *vaDecoder) decodeAV1Frame(seq *av1SeqHeader, fh *av1FrameHeader, tileData []byte, tileOff int) (video.Frame, bool, error) {
	if fh.frameWidth <= 0 || fh.frameHeight <= 0 {
		return video.Frame{}, false, ErrBitstreamParse
	}
	profile := vaProfileAV1Profile0
	if seq.seqProfile == 1 {
		profile = vaProfileAV1Profile1
	}
	if err := d.ensureContext(profile, alignUp(fh.frameWidth, 8), alignUp(fh.frameHeight, 8)); err != nil {
		return video.Frame{}, false, err
	}

	var bufs []uint32
	defer func() { d.freeDecodeBufs(bufs) }()

	pic := buildAV1PicParam(d.surface, seq, fh)
	if err := d.addDecodeBuf(&bufs, vaPictureParameterBufferType, int(unsafe.Sizeof(pic)), unsafe.Pointer(&pic)); err != nil {
		return video.Frame{}, false, err
	}
	slice := buildAV1SliceParam(tileData, tileOff)
	if err := d.addDecodeBuf(&bufs, vaSliceParameterBufferType, int(unsafe.Sizeof(slice)), unsafe.Pointer(&slice)); err != nil {
		return video.Frame{}, false, err
	}
	if err := d.addDecodeBuf(&bufs, vaSliceDataBufferType, len(tileData), unsafe.Pointer(&tileData[0])); err != nil {
		return video.Frame{}, false, err
	}
	if err := d.submitPicture(bufs); err != nil {
		return video.Frame{}, false, err
	}
	if !fh.showFrame {
		return video.Frame{}, false, nil
	}
	f, err := d.readSurface(fh.frameWidth, fh.frameHeight)
	if err != nil {
		return video.Frame{}, false, err
	}
	d.frameIdx++
	return f, true, nil
}

// buildAV1SliceParam fills VASliceParameterBufferAV1 for a single tile.
func buildAV1SliceParam(tileData []byte, off int) vaSliceParameterBufferAV1 {
	var sp vaSliceParameterBufferAV1
	sp.SliceDataSize = uint32(len(tileData))
	sp.SliceDataOffset = uint32(off)
	sp.SliceDataFlag = vaSliceDataFlagAll
	sp.TileRow = 0
	sp.TileColumn = 0
	sp.TileIdxInTileList = 0
	return sp
}
