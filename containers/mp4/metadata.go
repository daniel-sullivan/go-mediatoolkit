package mp4

import (
	"encoding/binary"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
)

// itunesTagKeys maps iTunes ilst four-character atom names to the
// Vorbis-comment-style keys used by [containers.StandardTags]. The leading
// byte of the ©-prefixed atoms is 0xA9. Atoms not in this table are
// preserved verbatim in [Extras.FreeformTags].
var itunesTagKeys = map[string]string{
	"\xa9nam": "TITLE",
	"\xa9ART": "ARTIST",
	"aART":    "ALBUMARTIST",
	"\xa9alb": "ALBUM",
	"\xa9day": "DATE",
	"\xa9gen": "GENRE",
	"gnre":    "GENRE",
	"\xa9cmt": "COMMENT",
	"\xa9wrt": "COMPOSER",
	"cprt":    "COPYRIGHT",
	"\xa9too": "ENCODER",
	"desc":    "DESCRIPTION",
}

// reverseTagKeys maps Vorbis-comment keys back to the iTunes atom name used
// on write. Only the keys [StandardTags] projects onto are emitted; ALBUMARTIST
// and GENRE pick their canonical atom.
var reverseTagKeys = map[string]string{
	"TITLE":       "\xa9nam",
	"ARTIST":      "\xa9ART",
	"ALBUMARTIST": "aART",
	"ALBUM":       "\xa9alb",
	"DATE":        "\xa9day",
	"GENRE":       "\xa9gen",
	"COMMENT":     "\xa9cmt",
	"COMPOSER":    "\xa9wrt",
	"COPYRIGHT":   "cprt",
	"ENCODER":     "\xa9too",
	"DESCRIPTION": "desc",
	"TRACKNUMBER": "trkn",
}

// iTunes "data" atom well-known type codes.
const (
	dataTypeUTF8   = 1
	dataTypeBinary = 0
	dataTypeJPEG   = 13
	dataTypePNG    = 14
	dataTypeUint   = 21
)

// parseIlst walks the ilst box's child item atoms and collects them into a
// flat tag map plus the freeform / cover-art remainder. Each item atom's name
// is its box type; its value is the first nested "data" atom.
func parseIlst(body []byte, base int64) (containers.Tags, map[string][]string, [][]byte, error) {
	items, err := readBoxes(body, base)
	if err != nil {
		return nil, nil, nil, err
	}

	tags := containers.NewTags()
	freeform := map[string][]string{}
	var covers [][]byte

	for _, item := range items {
		name := item.Type.String()
		dataType, value, ok := parseDataAtom(item.Payload)
		if !ok {
			continue
		}

		if name == "covr" {
			cp := make([]byte, len(value))
			copy(cp, value)
			covers = append(covers, cp)
			continue
		}

		key, mapped := itunesTagKeys[name]
		switch {
		case name == "trkn":
			// trkn is a binary atom: reserved(2) | track(2) | total(2).
			if len(value) >= 4 {
				track := binary.BigEndian.Uint16(value[2:4])
				tags.Add("TRACKNUMBER", itoa(int(track)))
			}
		case name == "gnre" && dataType == dataTypeBinary:
			// Legacy numeric genre: a 1-based index into the ID3 genre
			// list. Preserve the raw index as the tag value rather than
			// guessing the table.
			if len(value) >= 2 {
				tags.Add("GENRE", itoa(int(binary.BigEndian.Uint16(value))))
			}
		case mapped:
			tags.Add(key, string(value))
		default:
			freeform[name] = append(freeform[name], string(value))
		}
	}

	if len(freeform) == 0 {
		freeform = nil
	}
	return tags, freeform, covers, nil
}

// parseDataAtom reads the first "data" child atom of an ilst item: a FullBox
// whose body is type(4) | locale(4) | value. Returns the well-known type code
// and the value bytes.
func parseDataAtom(itemBody []byte) (dataType uint32, value []byte, ok bool) {
	children, err := readBoxes(itemBody, 0)
	if err != nil {
		return 0, nil, false
	}
	for _, c := range children {
		if c.Type != (BoxType{'d', 'a', 't', 'a'}) {
			continue
		}
		if len(c.Payload) < 8 {
			return 0, nil, false
		}
		dt := binary.BigEndian.Uint32(c.Payload[0:4]) & 0x00ffffff
		return dt, c.Payload[8:], true
	}
	return 0, nil, false
}

// buildIlst serialises a StandardTags (plus freeform and cover art) into an
// ilst box body. Standard tags emit a UTF-8 "data" atom; trkn emits the
// binary 8-byte form; cover art emits a "covr" atom per image.
func buildIlst(tags containers.StandardTags, freeform map[string][]string, covers [][]byte) []byte {
	var out []byte

	emit := func(atom string, dataType uint32, value []byte) {
		data := buildDataAtom(dataType, value)
		out = append(out, buildBox(atom, data)...)
	}

	flat := tags.Map()
	for _, key := range []string{
		"TITLE", "ARTIST", "ALBUMARTIST", "ALBUM", "DATE", "GENRE",
		"COMMENT", "COMPOSER", "COPYRIGHT", "ENCODER", "DESCRIPTION",
	} {
		atom, ok := reverseTagKeys[key]
		if !ok {
			continue
		}
		for _, v := range flat.GetAll(key) {
			emit(atom, dataTypeUTF8, []byte(v))
		}
	}

	if v := flat.Get("TRACKNUMBER"); v != "" {
		track := atoi(v)
		buf := make([]byte, 8)
		binary.BigEndian.PutUint16(buf[2:4], uint16(track))
		emit("trkn", dataTypeBinary, buf)
	}

	for name, vals := range freeform {
		for _, v := range vals {
			emit(name, dataTypeUTF8, []byte(v))
		}
	}

	for _, art := range covers {
		dt := uint32(dataTypeJPEG)
		if len(art) >= 8 && art[0] == 0x89 && art[1] == 'P' {
			dt = dataTypePNG
		}
		emit("covr", dt, art)
	}

	return out
}

// buildDataAtom builds an iTunes "data" atom: a box of type "data" whose body
// is type(4) | locale(4) | value.
func buildDataAtom(dataType uint32, value []byte) []byte {
	body := make([]byte, 8+len(value))
	binary.BigEndian.PutUint32(body[0:4], dataType&0x00ffffff)
	// locale stays zero.
	copy(body[8:], value)
	return buildBox("data", body)
}

// itoa / atoi are tiny base-10 conversions kept local to avoid pulling
// strconv into the tag-projection path; track and genre values are small
// non-negative integers.
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func atoi(s string) int {
	v := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + int(c-'0')
	}
	return v
}
