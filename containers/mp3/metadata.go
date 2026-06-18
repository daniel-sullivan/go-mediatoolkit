package mp3

import (
	"encoding/binary"
	"strings"

	"go-mediatoolkit/containers"
)

// id3v2Magic is the three-byte identifier ("ID3") that opens an ID3v2 tag.
var id3v2Magic = [3]byte{'I', 'D', '3'}

// id3v1Magic is the three-byte identifier ("TAG") that opens a 128-byte
// ID3v1 trailer.
var id3v1Magic = [3]byte{'T', 'A', 'G'}

// id3v1Size is the fixed size of an ID3v1 tag, in bytes.
const id3v1Size = 128

// frameIDToTag maps four-character ID3v2.3/2.4 frame IDs onto the
// Vorbis-comment-style standard tag keys used across the toolkit. Frame IDs
// that have no standard-tag analogue are preserved raw in
// [Extras.RawFrames] instead.
var frameIDToTag = map[string]string{
	"TIT2": "TITLE",
	"TPE1": "ARTIST",
	"TALB": "ALBUM",
	"TPE2": "ALBUMARTIST",
	"TYER": "DATE", // ID3v2.3 year
	"TDRC": "DATE", // ID3v2.4 recording date
	"TCON": "GENRE",
	"TRCK": "TRACKNUMBER",
	"COMM": "COMMENT",
	"TCOM": "COMPOSER",
	"TPE3": "PERFORMER",
	"TCOP": "COPYRIGHT",
	"TSSE": "ENCODER",
	"TENC": "ENCODER",
	"TPUB": "ORGANIZATION",
}

// tagToFrameID maps a standard tag key onto the ID3v2.3 frame ID used when
// writing. It is the canonical inverse of [frameIDToTag] — the first frame ID
// chosen per tag — so a write-then-read round-trip recovers the same tag.
var tagToFrameID = map[string]string{
	"TITLE":        "TIT2",
	"ARTIST":       "TPE1",
	"ALBUM":        "TALB",
	"ALBUMARTIST":  "TPE2",
	"DATE":         "TYER",
	"GENRE":        "TCON",
	"TRACKNUMBER":  "TRCK",
	"COMMENT":      "COMM",
	"COMPOSER":     "TCOM",
	"PERFORMER":    "TPE3",
	"COPYRIGHT":    "TCOP",
	"ENCODER":      "TSSE",
	"ORGANIZATION": "TPUB",
}

// synchsafe decodes a 28-bit synchsafe integer (the ID3v2 tag-size encoding:
// four 7-bit groups, high bit of each byte cleared).
func synchsafe(b []byte) int {
	return int(b[0]&0x7F)<<21 | int(b[1]&0x7F)<<14 | int(b[2]&0x7F)<<7 | int(b[3]&0x7F)
}

// putSynchsafe encodes n as a 28-bit synchsafe integer into a 4-byte slice.
func putSynchsafe(out []byte, n int) {
	out[0] = byte((n >> 21) & 0x7F)
	out[1] = byte((n >> 14) & 0x7F)
	out[2] = byte((n >> 7) & 0x7F)
	out[3] = byte(n & 0x7F)
}

// id3v2 holds the parsed contents of an ID3v2 tag header plus its projected
// tags and any frames that did not map to a standard tag.
type id3v2 struct {
	Version   int               // major version (3 or 4)
	Tags      containers.Tags   // standard-mappable frames
	RawFrames map[string][]byte // unmapped frame ID -> raw body (post-header)
	Pictures  [][]byte          // APIC frame bodies
}

// parseID3v2 parses an ID3v2 tag body (everything after the 10-byte tag
// header) given the major version and an optional extended-header flag. It
// extracts text frames into a tag map, mapping known frame IDs onto standard
// tag keys and preserving the rest. It is tolerant of trailing padding (a
// frame ID beginning with a NUL byte ends the frame walk).
func parseID3v2(version int, extendedHeader bool, body []byte) (*id3v2, error) {
	out := &id3v2{
		Version:   version,
		Tags:      containers.NewTags(),
		RawFrames: map[string][]byte{},
	}

	off := 0
	// ID3v2.3 and 2.4 use a 4-byte frame ID, 4-byte size, 2-byte flags.
	// 2.2 (3-byte IDs) is not supported.
	if version < 3 {
		return nil, ErrInvalidID3
	}

	if extendedHeader {
		// The extended header opens with its own size; skip it. v2.4 uses a
		// synchsafe size; v2.3 a plain big-endian size that does NOT include
		// the size field itself.
		if off+4 > len(body) {
			return nil, ErrInvalidID3
		}
		var extLen int
		if version >= 4 {
			extLen = synchsafe(body[off : off+4])
			off += extLen // v2.4 extended-header size covers the whole ext header
		} else {
			extLen = int(binary.BigEndian.Uint32(body[off : off+4]))
			off += 4 + extLen
		}
		if off > len(body) {
			return nil, ErrInvalidID3
		}
	}

	for off+10 <= len(body) {
		id := string(body[off : off+4])
		if body[off] == 0 {
			break // padding
		}
		var size int
		if version >= 4 {
			size = synchsafe(body[off+4 : off+8])
		} else {
			size = int(binary.BigEndian.Uint32(body[off+4 : off+8]))
		}
		off += 10
		if size < 0 || off+size > len(body) {
			return nil, ErrInvalidID3
		}
		frame := body[off : off+size]
		off += size

		if id == "APIC" {
			cp := make([]byte, len(frame))
			copy(cp, frame)
			out.Pictures = append(out.Pictures, cp)
			continue
		}

		if std, ok := frameIDToTag[id]; ok {
			val := decodeTextFrame(id, frame)
			if val != "" {
				out.Tags.Add(std, val)
			}
			continue
		}

		cp := make([]byte, len(frame))
		copy(cp, frame)
		out.RawFrames[id] = cp
	}

	return out, nil
}

// decodeTextFrame extracts the textual value of an ID3v2 text/comment frame
// body. The first byte is a text-encoding marker (0 = ISO-8859-1, 1 = UTF-16
// w/ BOM, 2 = UTF-16BE, 3 = UTF-8). COMM frames additionally carry a 3-byte
// language code and a NUL-terminated short description before the text.
func decodeTextFrame(id string, frame []byte) string {
	if len(frame) == 0 {
		return ""
	}
	enc := frame[0]
	rest := frame[1:]

	if id == "COMM" {
		// language (3 bytes) + short description (NUL-terminated in enc).
		if len(rest) < 3 {
			return ""
		}
		rest = rest[3:]
		rest = skipDescription(enc, rest)
	}

	return decodeString(enc, rest)
}

// skipDescription advances past a NUL-terminated short description encoded
// with enc, returning the remaining bytes (the comment text).
func skipDescription(enc byte, b []byte) []byte {
	switch enc {
	case 1, 2: // UTF-16: terminator is a 2-byte 0x0000
		for i := 0; i+1 < len(b); i += 2 {
			if b[i] == 0 && b[i+1] == 0 {
				return b[i+2:]
			}
		}
		return nil
	default: // single-byte terminator
		for i := 0; i < len(b); i++ {
			if b[i] == 0 {
				return b[i+1:]
			}
		}
		return nil
	}
}

// decodeString decodes a text payload according to an ID3v2 text-encoding
// byte. UTF-16 is decoded honouring an optional BOM; trailing NUL terminators
// are trimmed.
func decodeString(enc byte, b []byte) string {
	switch enc {
	case 0, 3: // ISO-8859-1 (treated as Latin-1) or UTF-8
		s := string(b)
		return strings.Trim(s, "\x00")
	case 1, 2: // UTF-16 (with BOM) / UTF-16BE
		return decodeUTF16(enc, b)
	default:
		return strings.Trim(string(b), "\x00")
	}
}

// decodeUTF16 decodes a UTF-16 payload. enc==1 honours a leading BOM
// (defaulting to little-endian); enc==2 is big-endian without a BOM.
func decodeUTF16(enc byte, b []byte) string {
	bigEndian := enc == 2
	if enc == 1 && len(b) >= 2 {
		switch {
		case b[0] == 0xFF && b[1] == 0xFE:
			bigEndian = false
			b = b[2:]
		case b[0] == 0xFE && b[1] == 0xFF:
			bigEndian = true
			b = b[2:]
		}
	}
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		var u uint16
		if bigEndian {
			u = uint16(b[i])<<8 | uint16(b[i+1])
		} else {
			u = uint16(b[i]) | uint16(b[i+1])<<8
		}
		if u == 0 {
			break
		}
		sb.WriteRune(rune(u))
	}
	return sb.String()
}

// parseID3v1 parses a 128-byte ID3v1 trailer into a tag map. It returns
// ErrInvalidID3 if tag is not exactly 128 bytes or lacks the "TAG" magic.
// Trailing spaces and NULs are trimmed from each field.
func parseID3v1(tag []byte) (containers.Tags, error) {
	if len(tag) != id3v1Size || [3]byte{tag[0], tag[1], tag[2]} != id3v1Magic {
		return nil, ErrInvalidID3
	}
	tags := containers.NewTags()
	field := func(b []byte) string {
		return strings.TrimRight(string(b), " \x00")
	}
	if v := field(tag[3:33]); v != "" {
		tags.Set("TITLE", v)
	}
	if v := field(tag[33:63]); v != "" {
		tags.Set("ARTIST", v)
	}
	if v := field(tag[63:93]); v != "" {
		tags.Set("ALBUM", v)
	}
	if v := field(tag[93:97]); v != "" {
		tags.Set("DATE", v)
	}
	// Bytes 97..126 are the comment; if byte 125 is NUL and 126 is non-zero,
	// it's an ID3v1.1 track number (byte 126).
	if tag[125] == 0 && tag[126] != 0 {
		tags.Set("COMMENT", field(tag[97:125]))
		tags.Set("TRACKNUMBER", itoa(int(tag[126])))
	} else if v := field(tag[97:127]); v != "" {
		tags.Set("COMMENT", v)
	}
	return tags, nil
}

// itoa renders a small non-negative integer without importing strconv at the
// call site clutter; kept local to the metadata helpers.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// encodeID3v2 serialises a tag map into a complete ID3v2.3 tag, including the
// 10-byte header. Each standard tag becomes a UTF-8 text frame (encoding byte
// 3). The result is a freestanding byte block to prepend to the audio frames.
func encodeID3v2(tags containers.Tags) []byte {
	var frames []byte
	for _, key := range tags.Keys() {
		frameID, ok := tagToFrameID[strings.ToUpper(key)]
		if !ok {
			continue
		}
		for _, v := range tags.GetAll(key) {
			frames = append(frames, encodeTextFrame(frameID, v)...)
		}
	}

	out := make([]byte, 10+len(frames))
	out[0], out[1], out[2] = 'I', 'D', '3'
	out[3] = 3 // major version 2.3
	out[4] = 0 // revision
	out[5] = 0 // flags
	putSynchsafe(out[6:10], len(frames))
	copy(out[10:], frames)
	return out
}

// encodeTextFrame builds a single ID3v2.3 UTF-8 text frame: 4-byte ID, 4-byte
// big-endian size, 2-byte flags, then a 0x03 encoding byte and the UTF-8
// value.
func encodeTextFrame(id, value string) []byte {
	body := make([]byte, 0, 1+len(value))
	body = append(body, 3) // UTF-8
	body = append(body, value...)

	out := make([]byte, 10+len(body))
	copy(out[0:4], id)
	binary.BigEndian.PutUint32(out[4:8], uint32(len(body)))
	// flags out[8], out[9] left zero
	copy(out[10:], body)
	return out
}
