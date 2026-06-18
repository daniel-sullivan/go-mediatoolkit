package wav

import (
	"bytes"
	"encoding/binary"
	"strings"

	"go-mediatoolkit/containers"
)

// infoToTag maps RIFF INFO four-CCs to Vorbis-style tag keys.
//
// See the RIFF INFO list specification (e.g. INAM = "name/title").
var infoToTag = map[string]string{
	"INAM": "TITLE",
	"IART": "ARTIST",
	"IPRD": "ALBUM",
	"ICRD": "DATE",
	"IGNR": "GENRE",
	"ICMT": "COMMENT",
	"ITRK": "TRACKNUMBER",
	"ICOP": "COPYRIGHT",
	"ISFT": "ENCODER",
	"IENG": "ENGINEER",
	"ITCH": "TECHNICIAN",
	"ISRC": "ISRC",
	"ISBJ": "SUBJECT",
	"IMUS": "COMPOSER",
	"IPRF": "PERFORMER",
	"IPUB": "ORGANIZATION",
	"IKEY": "KEYWORDS",
	"IMED": "MEDIUM",
	"ISRF": "SOURCE",
	"ILNG": "LANGUAGE",
	"ICNT": "COUNTRY",
}

// tagToInfo is the reverse mapping. Keys not in infoToTag are not
// round-tripped into the LIST/INFO chunk — they are written via a
// dedicated vendor extension chunk instead (see writeUnknownTags).
var tagToInfo = func() map[string]string {
	m := make(map[string]string, len(infoToTag))
	for k, v := range infoToTag {
		m[v] = k
	}
	return m
}()

// parseLISTInfo parses the body of a LIST chunk with list-type INFO
// (the leading four bytes "INFO" have already been consumed by the caller)
// and merges the sub-chunks into the given flat tag map.
func parseLISTInfo(body []byte, raw containers.Tags) error {
	// body is a sequence of INFO sub-chunks: 4-byte CC + 4-byte LE size + payload + pad.
	for len(body) >= 8 {
		cc := string(body[0:4])
		size := binary.LittleEndian.Uint32(body[4:8])
		end := 8 + int(size)
		if end > len(body) {
			return ErrBadChunkSize
		}
		payload := body[8:end]
		// INFO strings are null-terminated ASCII/ANSI; some tools store UTF-8.
		value := strings.TrimRight(string(payload), "\x00")
		value = strings.TrimSpace(value)
		if value != "" {
			if key, ok := infoToTag[cc]; ok {
				raw.Add(key, value)
			} else {
				// Preserve the raw four-CC as "WAV:XXXX" so nothing is lost.
				raw.Add("WAV:"+cc, value)
			}
		}
		// Skip past the padding byte if size was odd.
		adv := end + (end & 1)
		if adv > len(body) {
			break
		}
		body = body[adv:]
	}
	return nil
}

// buildLISTInfo serialises tags into a LIST/INFO chunk body (including the
// leading "INFO" list-type). Returns nil if there is nothing to write.
func buildLISTInfo(st containers.StandardTags) []byte {
	tags := st.Map()
	if len(tags) == 0 {
		return nil
	}

	var buf bytes.Buffer
	buf.Write(idINFO[:])

	appended := false
	for key, values := range tags {
		cc, ok := tagToInfo[key]
		if !ok {
			// Allow explicit WAV:XXXX pass-through.
			if strings.HasPrefix(key, "WAV:") && len(key) == 8 {
				cc = key[4:]
			} else {
				continue
			}
		}
		for _, v := range values {
			payload := append([]byte(v), 0) // null-terminate
			size := uint32(len(payload))

			var hdr [8]byte
			copy(hdr[:4], cc)
			binary.LittleEndian.PutUint32(hdr[4:], size)
			buf.Write(hdr[:])
			buf.Write(payload)
			if size&1 == 1 {
				buf.WriteByte(0)
			}
			appended = true
		}
	}
	if !appended {
		return nil
	}
	return buf.Bytes()
}
