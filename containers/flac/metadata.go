package flac

import (
	"encoding/binary"
	"strings"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
)

// FLAC magic and metadata block-type identifiers (RFC 9639).
var flacMagic = [4]byte{'f', 'L', 'a', 'C'}

const (
	blockStreamInfo    = 0
	blockPadding       = 1
	blockApplication   = 2
	blockSeekTable     = 3
	blockVorbisComment = 4
	blockCuesheet      = 5
	blockPicture       = 6
)

// metaBlockHeader is the 4-byte header preceding every metadata block.
type metaBlockHeader struct {
	Last   bool
	Type   uint8
	Length uint32 // 24-bit body length
}

func parseBlockHeader(b [4]byte) metaBlockHeader {
	return metaBlockHeader{
		Last:   b[0]&0x80 != 0,
		Type:   b[0] & 0x7F,
		Length: uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]),
	}
}

// parseStreamInfo decodes the 34-byte STREAMINFO body. It returns
// ErrInvalidMetadata if body is not exactly 34 bytes.
func parseStreamInfo(body []byte) (StreamInfo, error) {
	if len(body) != 34 {
		return StreamInfo{}, ErrInvalidMetadata
	}
	si := StreamInfo{
		MinBlockSize: int(binary.BigEndian.Uint16(body[0:2])),
		MaxBlockSize: int(binary.BigEndian.Uint16(body[2:4])),
		MinFrameSize: int(uint32(body[4])<<16 | uint32(body[5])<<8 | uint32(body[6])),
		MaxFrameSize: int(uint32(body[7])<<16 | uint32(body[8])<<8 | uint32(body[9])),
	}
	// Bytes 10..17 hold the packed: 20 bits sample rate, 3 bits
	// (channels-1), 5 bits (bits/sample - 1), 36 bits total samples.
	packed := binary.BigEndian.Uint64(body[10:18])
	si.SampleRate = int(packed >> 44)                  // top 20 bits
	si.Channels = int((packed>>41)&0x7) + 1            // next 3 bits
	si.BitsPerSample = int((packed>>36)&0x1F) + 1      // next 5 bits
	si.TotalSamples = packed & ((uint64(1) << 36) - 1) // bottom 36 bits
	copy(si.MD5Signature[:], body[18:34])
	return si, nil
}

// encodeStreamInfo serialises a StreamInfo into the 34-byte body the
// metadata block carries. Used by the writer (when rewriting) and by
// tests; the libraries/flac encoder writes its own STREAMINFO in
// production, so this is mostly for round-trip / inspection helpers.
func encodeStreamInfo(si StreamInfo) []byte {
	out := make([]byte, 34)
	binary.BigEndian.PutUint16(out[0:2], uint16(si.MinBlockSize))
	binary.BigEndian.PutUint16(out[2:4], uint16(si.MaxBlockSize))
	out[4] = byte(si.MinFrameSize >> 16)
	out[5] = byte(si.MinFrameSize >> 8)
	out[6] = byte(si.MinFrameSize)
	out[7] = byte(si.MaxFrameSize >> 16)
	out[8] = byte(si.MaxFrameSize >> 8)
	out[9] = byte(si.MaxFrameSize)
	packed := uint64(si.SampleRate&0xFFFFF) << 44
	packed |= uint64((si.Channels-1)&0x7) << 41
	packed |= uint64((si.BitsPerSample-1)&0x1F) << 36
	packed |= si.TotalSamples & ((uint64(1) << 36) - 1)
	binary.BigEndian.PutUint64(out[10:18], packed)
	copy(out[18:34], si.MD5Signature[:])
	return out
}

// parseVorbisComment decodes a VORBIS_COMMENT body into a vendor string
// and a Tags map. VORBIS_COMMENT is little-endian — unique among FLAC
// metadata blocks.
func parseVorbisComment(body []byte) (vendor string, tags containers.Tags, err error) {
	tags = containers.NewTags()
	if len(body) < 4 {
		return "", nil, ErrInvalidMetadata
	}
	off := 0
	vendorLen := binary.LittleEndian.Uint32(body[off : off+4])
	off += 4
	if uint64(off)+uint64(vendorLen) > uint64(len(body)) {
		return "", nil, ErrInvalidMetadata
	}
	vendor = string(body[off : off+int(vendorLen)])
	off += int(vendorLen)

	if off+4 > len(body) {
		return "", nil, ErrInvalidMetadata
	}
	count := binary.LittleEndian.Uint32(body[off : off+4])
	off += 4

	for i := uint32(0); i < count; i++ {
		if off+4 > len(body) {
			return "", nil, ErrInvalidMetadata
		}
		entryLen := binary.LittleEndian.Uint32(body[off : off+4])
		off += 4
		if uint64(off)+uint64(entryLen) > uint64(len(body)) {
			return "", nil, ErrInvalidMetadata
		}
		entry := string(body[off : off+int(entryLen)])
		off += int(entryLen)

		eq := strings.IndexByte(entry, '=')
		if eq <= 0 {
			// Skip malformed entries without aborting — VORBIS_COMMENT
			// readers are explicitly required to be tolerant.
			continue
		}
		tags.Add(strings.ToUpper(entry[:eq]), entry[eq+1:])
	}
	return vendor, tags, nil
}

// encodeVorbisComment serialises a vendor string and tag map into a
// VORBIS_COMMENT body. Tag keys are emitted upper-case (matching the
// containers.Tags convention); value order within a multi-value key is
// preserved.
func encodeVorbisComment(vendor string, tags containers.Tags) []byte {
	// Pre-count entries.
	var entries []string
	for _, key := range tags.Keys() {
		for _, v := range tags.GetAll(key) {
			entries = append(entries, key+"="+v)
		}
	}

	size := 4 + len(vendor) + 4
	for _, e := range entries {
		size += 4 + len(e)
	}
	out := make([]byte, size)

	off := 0
	binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(vendor)))
	off += 4
	copy(out[off:], vendor)
	off += len(vendor)

	binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(entries)))
	off += 4
	for _, e := range entries {
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(len(e)))
		off += 4
		copy(out[off:], e)
		off += len(e)
	}
	return out
}

// parseSeekTable decodes a SEEKTABLE body (sequence of 18-byte points).
// Placeholder points (sample number == 2^64-1) are dropped per FLAC spec.
func parseSeekTable(body []byte) ([]SeekPoint, error) {
	if len(body)%18 != 0 {
		return nil, ErrInvalidMetadata
	}
	const placeholder = ^uint64(0)
	points := make([]SeekPoint, 0, len(body)/18)
	for off := 0; off < len(body); off += 18 {
		sn := binary.BigEndian.Uint64(body[off : off+8])
		if sn == placeholder {
			continue
		}
		points = append(points, SeekPoint{
			SampleNumber: sn,
			StreamOffset: binary.BigEndian.Uint64(body[off+8 : off+16]),
			FrameSamples: binary.BigEndian.Uint16(body[off+16 : off+18]),
		})
	}
	return points, nil
}

// encodeSeekTable serialises seek points into the SEEKTABLE body.
func encodeSeekTable(points []SeekPoint) []byte {
	out := make([]byte, 18*len(points))
	for i, p := range points {
		off := i * 18
		binary.BigEndian.PutUint64(out[off:off+8], p.SampleNumber)
		binary.BigEndian.PutUint64(out[off+8:off+16], p.StreamOffset)
		binary.BigEndian.PutUint16(out[off+16:off+18], p.FrameSamples)
	}
	return out
}
