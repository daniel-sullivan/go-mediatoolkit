package wav

import (
	"bytes"
	"encoding/binary"
	"strings"
)

// Fixed offsets inside the bext chunk body (EBU Tech 3285 v2).
const (
	bextDescriptionLen     = 256
	bextOriginatorLen      = 32
	bextOriginatorRefLen   = 32
	bextOriginationDateLen = 10
	bextOriginationTimeLen = 8
	bextFixedPrefixLen     = bextDescriptionLen + bextOriginatorLen +
		bextOriginatorRefLen + bextOriginationDateLen + bextOriginationTimeLen +
		8 /*TimeReference*/ + 2 /*Version*/ + 64 /*UMID*/ + 190 /*Reserved*/
)

func parseBext(body []byte) *BroadcastExt {
	if len(body) < bextFixedPrefixLen {
		return nil
	}
	b := &BroadcastExt{}
	off := 0

	b.Description = trimNUL(body[off : off+bextDescriptionLen])
	off += bextDescriptionLen
	b.Originator = trimNUL(body[off : off+bextOriginatorLen])
	off += bextOriginatorLen
	b.OriginatorRef = trimNUL(body[off : off+bextOriginatorRefLen])
	off += bextOriginatorRefLen
	b.OriginationDate = trimNUL(body[off : off+bextOriginationDateLen])
	off += bextOriginationDateLen
	b.OriginationTime = trimNUL(body[off : off+bextOriginationTimeLen])
	off += bextOriginationTimeLen

	b.TimeReference = binary.LittleEndian.Uint64(body[off : off+8])
	off += 8
	b.Version = binary.LittleEndian.Uint16(body[off : off+2])
	off += 2
	copy(b.UMID[:], body[off:off+64])
	off += 64
	off += 190 // reserved

	if off < len(body) {
		b.CodingHistory = trimNUL(body[off:])
	}
	return b
}

func buildBext(b *BroadcastExt) []byte {
	var buf bytes.Buffer

	fixed := func(s string, n int) {
		data := make([]byte, n)
		copy(data, s)
		buf.Write(data)
	}

	fixed(b.Description, bextDescriptionLen)
	fixed(b.Originator, bextOriginatorLen)
	fixed(b.OriginatorRef, bextOriginatorRefLen)
	fixed(b.OriginationDate, bextOriginationDateLen)
	fixed(b.OriginationTime, bextOriginationTimeLen)

	var tr [8]byte
	binary.LittleEndian.PutUint64(tr[:], b.TimeReference)
	buf.Write(tr[:])

	var ver [2]byte
	binary.LittleEndian.PutUint16(ver[:], b.Version)
	buf.Write(ver[:])

	buf.Write(b.UMID[:])
	buf.Write(make([]byte, 190)) // reserved

	if b.CodingHistory != "" {
		buf.WriteString(b.CodingHistory)
	}
	return buf.Bytes()
}

func trimNUL(b []byte) string {
	return strings.TrimRight(string(b), "\x00")
}
