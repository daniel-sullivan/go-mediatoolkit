package mp4

import (
	"encoding/binary"
	"io"
)

// box is one parsed ISOBMFF atom: its type, the absolute file offset of its
// header start, the offset of its payload, and the payload bytes. Container
// boxes (moov, trak, mdia, minf, stbl, udta, meta, ilst) carry child boxes
// in Payload; leaf boxes carry their raw body.
type box struct {
	Type        BoxType
	HeaderStart int64 // absolute offset of the size field
	PayloadAt   int64 // absolute offset of the first payload byte
	Payload     []byte
}

// readBoxes parses the sequence of boxes occupying body, which begins at
// absolute file offset base. base lets leaf parsers (chunk offsets in
// particular) reason in absolute file coordinates. The returned boxes share
// backing storage with body; callers that retain bytes past the parse must
// copy them.
func readBoxes(body []byte, base int64) ([]box, error) {
	var boxes []box
	off := 0
	for off < len(body) {
		if off+8 > len(body) {
			return nil, ErrInvalidBox
		}
		size := int64(binary.BigEndian.Uint32(body[off : off+4]))
		var typ BoxType
		copy(typ[:], body[off+4:off+8])

		headerLen := int64(8)
		switch size {
		case 0:
			// Box extends to the end of the enclosing body.
			size = int64(len(body) - off)
		case 1:
			// 64-bit largesize follows the type.
			if off+16 > len(body) {
				return nil, ErrInvalidBox
			}
			size = int64(binary.BigEndian.Uint64(body[off+8 : off+16]))
			headerLen = 16
		}

		if size < headerLen || off+int(size) > len(body) {
			return nil, ErrInvalidBox
		}

		payloadStart := off + int(headerLen)
		payloadEnd := off + int(size)
		boxes = append(boxes, box{
			Type:        typ,
			HeaderStart: base + int64(off),
			PayloadAt:   base + int64(payloadStart),
			Payload:     body[payloadStart:payloadEnd],
		})
		off = payloadEnd
	}
	return boxes, nil
}

// find returns the first child box of the given type, or nil.
func find(boxes []box, t BoxType) *box {
	for i := range boxes {
		if boxes[i].Type == t {
			return &boxes[i]
		}
	}
	return nil
}

// readFullBody reads the entire stream into memory. MP4 parsing is
// random-access (chunk offsets reference absolute file positions), so the
// reader buffers the whole file rather than streaming it.
func readFullBody(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrBadArg
	}
	return io.ReadAll(r)
}
