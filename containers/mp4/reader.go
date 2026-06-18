package mp4

import (
	"io"
	"time"

	"go-mediatoolkit/containers"
	aaclib "go-mediatoolkit/libraries/aac"
	"go-mediatoolkit/mutations"
)

// Box types for the nested audio-track hierarchy
// moov → trak → mdia → minf → stbl → (stsd → mp4a → esds, stsz, stsc, stco/co64, stts)
// and the metadata hierarchy moov → udta → meta → ilst.
var (
	boxTrak = BoxType{'t', 'r', 'a', 'k'}
	boxMdia = BoxType{'m', 'd', 'i', 'a'}
	boxMdhd = BoxType{'m', 'd', 'h', 'd'}
	boxMinf = BoxType{'m', 'i', 'n', 'f'}
	boxStbl = BoxType{'s', 't', 'b', 'l'}
	boxStsd = BoxType{'s', 't', 's', 'd'}
	boxMp4a = BoxType{'m', 'p', '4', 'a'}
	boxUdta = BoxType{'u', 'd', 't', 'a'}
	boxMeta = BoxType{'m', 'e', 't', 'a'}
)

// Reader parses an MP4/ISOBMFF file into its [Header] and exposes the AAC
// access units it carries via [Reader.Packets]. The whole file is buffered
// in memory because chunk offsets are absolute file positions; the returned
// packet reader yields one AAC access unit per call, ready to feed into
// [go-mediatoolkit/codec/aac.NewDecoder] together with [Header].Extra.Config.
//
// A Reader is not safe for concurrent use.
type Reader struct {
	header  Header
	packets [][]byte
}

// NewReader reads and parses the entire MP4 stream from r. It locates the
// audio track, decodes its esds AudioSpecificConfig and stsz/stsc/stco(/co64)
// sample tables, slices the mdat payload into AAC access units, and projects
// the moov.udta.meta.ilst iTunes atoms onto [containers.StandardTags].
func NewReader(r io.Reader) (*Reader, error) {
	file, err := readFullBody(r)
	if err != nil {
		return nil, err
	}

	// The stream must open with an ftyp box. Validate the very first box
	// header before a full parse so non-MP4 input is rejected as ErrNotMP4
	// rather than tripping a generic box-overrun error.
	if len(file) < 8 {
		return nil, ErrNotMP4
	}
	var firstType BoxType
	copy(firstType[:], file[4:8])
	if firstType != BoxFtyp {
		return nil, ErrNotMP4
	}

	top, err := readBoxes(file, 0)
	if err != nil {
		return nil, err
	}

	moov := find(top, BoxMoov)
	if moov == nil {
		return nil, ErrMissingMoov
	}
	moovChildren, err := readBoxes(moov.Payload, moov.PayloadAt)
	if err != nil {
		return nil, err
	}

	header := Header{
		Format:       FormatM4A,
		SampleFormat: mutations.FormatFloat64,
	}

	// ── ftyp brands ──────────────────────────────────────────────────
	if ftyp := find(top, BoxFtyp); ftyp != nil && len(ftyp.Payload) >= 8 {
		header.Extra.MajorBrand = string(ftyp.Payload[0:4])
		for o := 8; o+4 <= len(ftyp.Payload); o += 4 {
			brand := string(ftyp.Payload[o : o+4])
			if brand != "\x00\x00\x00\x00" {
				header.Extra.CompatibleBrands = append(header.Extra.CompatibleBrands, brand)
			}
		}
	}

	// ── audio track: trak → mdia → minf → stbl ───────────────────────
	stbl, mediaTimescale, mediaDurationTicks, err := findAudioStbl(moovChildren)
	if err != nil {
		return nil, err
	}
	stblChildren, err := readBoxes(stbl.Payload, stbl.PayloadAt)
	if err != nil {
		return nil, err
	}

	// esds (AudioSpecificConfig) lives in stsd → mp4a.
	asc, err := findESDS(stblChildren)
	if err != nil {
		return nil, err
	}
	header.Extra.Config = asc
	header.SampleRate = asc.SampleRate
	header.Channels = asc.Channels

	// ── sample tables ────────────────────────────────────────────────
	var table SampleTable
	if b := find(stblChildren, BoxStsz); b != nil {
		if table.SampleSizes, err = parseStsz(b.Payload); err != nil {
			return nil, err
		}
	} else {
		return nil, ErrInvalidSampleTable
	}
	if b := find(stblChildren, BoxStsc); b != nil {
		if table.SampleToChunk, err = parseStsc(b.Payload); err != nil {
			return nil, err
		}
	}
	switch {
	case find(stblChildren, BoxStco) != nil:
		if table.ChunkOffsets, err = parseStco(find(stblChildren, BoxStco).Payload); err != nil {
			return nil, err
		}
	case find(stblChildren, BoxCo64) != nil:
		if table.ChunkOffsets, err = parseCo64(find(stblChildren, BoxCo64).Payload); err != nil {
			return nil, err
		}
	default:
		return nil, ErrInvalidSampleTable
	}
	header.Extra.SampleTable = table

	// Duration from stts (preferred) or the mdhd media header.
	if b := find(stblChildren, BoxStts); b != nil {
		entries, err := parseStts(b.Payload)
		if err != nil {
			return nil, err
		}
		if mediaTimescale > 0 {
			ticks := totalDurationTicks(entries)
			header.Duration = time.Duration(float64(ticks) / float64(mediaTimescale) * float64(time.Second))
		}
	} else if mediaTimescale > 0 && mediaDurationTicks > 0 {
		header.Duration = time.Duration(float64(mediaDurationTicks) / float64(mediaTimescale) * float64(time.Second))
	}

	// ── slice mdat into access units ─────────────────────────────────
	locs, err := resolveSampleOffsets(table)
	if err != nil {
		return nil, err
	}
	packets, err := sliceAccessUnits(file, locs)
	if err != nil {
		return nil, err
	}

	// ── iTunes metadata: moov → udta → meta → ilst ──────────────────
	if err := readMetadata(moovChildren, &header); err != nil {
		return nil, err
	}

	return &Reader{header: header, packets: packets}, nil
}

// findAudioStbl walks the first trak whose mdia/minf/stbl contains an mp4a
// sample entry, returning the stbl box along with that track's media
// timescale and duration (from mdhd) for duration computation.
func findAudioStbl(moovChildren []box) (stbl *box, timescale uint32, durationTicks uint64, err error) {
	for ti := range moovChildren {
		if moovChildren[ti].Type != boxTrak {
			continue
		}
		trakChildren, e := readBoxes(moovChildren[ti].Payload, moovChildren[ti].PayloadAt)
		if e != nil {
			return nil, 0, 0, e
		}
		mdia := find(trakChildren, boxMdia)
		if mdia == nil {
			continue
		}
		mdiaChildren, e := readBoxes(mdia.Payload, mdia.PayloadAt)
		if e != nil {
			return nil, 0, 0, e
		}

		var ts uint32
		var dur uint64
		if mdhd := find(mdiaChildren, boxMdhd); mdhd != nil {
			ts, dur = parseMdhd(mdhd.Payload)
		}

		minf := find(mdiaChildren, boxMinf)
		if minf == nil {
			continue
		}
		minfChildren, e := readBoxes(minf.Payload, minf.PayloadAt)
		if e != nil {
			return nil, 0, 0, e
		}
		sb := find(minfChildren, boxStbl)
		if sb == nil {
			continue
		}
		// Confirm this stbl carries an mp4a entry before accepting it.
		sbChildren, e := readBoxes(sb.Payload, sb.PayloadAt)
		if e != nil {
			return nil, 0, 0, e
		}
		if !hasMp4a(sbChildren) {
			continue
		}
		return sb, ts, dur, nil
	}
	return nil, 0, 0, ErrUnsupportedCodec
}

// parseMdhd extracts the media timescale and duration from an mdhd box,
// handling both the version-0 (32-bit) and version-1 (64-bit) layouts.
func parseMdhd(body []byte) (timescale uint32, duration uint64) {
	if len(body) < 4 {
		return 0, 0
	}
	version := body[0]
	if version == 1 {
		if len(body) < 4+8+8+4+8 {
			return 0, 0
		}
		timescale = beU32(body[4+8+8:])
		duration = beU64(body[4+8+8+4:])
		return timescale, duration
	}
	if len(body) < 4+4+4+4+4 {
		return 0, 0
	}
	timescale = beU32(body[4+4+4:])
	duration = uint64(beU32(body[4+4+4+4:]))
	return timescale, duration
}

// hasMp4a reports whether the stbl's stsd holds an mp4a sample entry.
func hasMp4a(stblChildren []box) bool {
	stsd := find(stblChildren, boxStsd)
	if stsd == nil || len(stsd.Payload) < 8 {
		return false
	}
	// stsd: version+flags(4) | entryCount(4) | sample entries.
	entries, err := readBoxes(stsd.Payload[8:], 0)
	if err != nil {
		return false
	}
	return find(entries, boxMp4a) != nil
}

// findESDS locates the esds box inside stsd → mp4a and parses its
// AudioSpecificConfig.
func findESDS(stblChildren []box) (aaclib.AudioSpecificConfig, error) {
	stsd := find(stblChildren, boxStsd)
	if stsd == nil || len(stsd.Payload) < 8 {
		return aaclib.AudioSpecificConfig{}, ErrUnsupportedCodec
	}
	entries, err := readBoxes(stsd.Payload[8:], 0)
	if err != nil {
		return aaclib.AudioSpecificConfig{}, err
	}
	mp4a := find(entries, boxMp4a)
	if mp4a == nil {
		return aaclib.AudioSpecificConfig{}, ErrUnsupportedCodec
	}
	// AudioSampleEntry has a fixed 28-byte body before any child boxes:
	//   reserved(6) | dataRefIndex(2) | reserved(8) | channelCount(2) |
	//   sampleSize(2) | predefined(2) | reserved(2) | sampleRate(4).
	if len(mp4a.Payload) < 28 {
		return aaclib.AudioSpecificConfig{}, ErrInvalidBox
	}
	children, err := readBoxes(mp4a.Payload[28:], 0)
	if err != nil {
		return aaclib.AudioSpecificConfig{}, err
	}
	esds := find(children, BoxEsds)
	if esds == nil {
		return aaclib.AudioSpecificConfig{}, ErrMissingEsds
	}
	return parseESDS(esds.Payload)
}

// readMetadata parses moov → udta → meta → ilst (when present) and projects
// the iTunes atoms onto the header's StandardTags and Extras.
func readMetadata(moovChildren []box, header *Header) error {
	udta := find(moovChildren, boxUdta)
	if udta == nil {
		return nil
	}
	udtaChildren, err := readBoxes(udta.Payload, udta.PayloadAt)
	if err != nil {
		return err
	}
	meta := find(udtaChildren, boxMeta)
	if meta == nil {
		return nil
	}
	// meta is a FullBox: a 4-byte version+flags header precedes its child
	// boxes (hdlr, ilst, …).
	if len(meta.Payload) < 4 {
		return nil
	}
	metaChildren, err := readBoxes(meta.Payload[4:], meta.PayloadAt+4)
	if err != nil {
		return err
	}
	ilst := find(metaChildren, BoxIlst)
	if ilst == nil {
		return nil
	}

	tags, freeform, covers, err := parseIlst(ilst.Payload, ilst.PayloadAt)
	if err != nil {
		return err
	}
	header.Tags = containers.StandardTagsFromMap(tags)
	header.Extra.FreeformTags = freeform
	header.Extra.CoverArt = covers
	return nil
}

// Header returns the parsed MP4 header.
func (r *Reader) Header() Header { return r.header }

// Packets returns a [PacketReader] over the AAC access units carried in the
// mdat box, in decode order. Feed it (with [Header].Extra.Config) into
// [go-mediatoolkit/codec/aac.NewDecoder].
func (r *Reader) Packets() *PacketReader {
	return &PacketReader{packets: r.packets}
}

// AccessUnits returns the raw AAC access units in decode order. Callers that
// want random access (rather than the streaming [PacketReader]) use this.
func (r *Reader) AccessUnits() [][]byte { return r.packets }

// PacketReader yields AAC access units one at a time. It satisfies
// [go-mediatoolkit/codec/aac.PacketReader].
type PacketReader struct {
	packets [][]byte
	i       int
}

// ReadPacket returns the next AAC access unit, or io.EOF when exhausted.
func (p *PacketReader) ReadPacket() ([]byte, error) {
	if p.i >= len(p.packets) {
		return nil, io.EOF
	}
	pkt := p.packets[p.i]
	p.i++
	return pkt, nil
}

func beU32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func beU64(b []byte) uint64 {
	return uint64(b[0])<<56 | uint64(b[1])<<48 | uint64(b[2])<<40 | uint64(b[3])<<32 |
		uint64(b[4])<<24 | uint64(b[5])<<16 | uint64(b[6])<<8 | uint64(b[7])
}
