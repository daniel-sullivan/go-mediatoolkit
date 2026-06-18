package wav

import (
	"bytes"
	"encoding/binary"
	"io"

	"go-mediatoolkit/containers"
	"go-mediatoolkit/mutations"
)

// Reader parses a RIFF/WAVE file and exposes the PCM data chunk as an
// [io.Reader] together with a fully-populated [Header].
type Reader struct {
	header Header
	data   io.Reader // bounded reader over the data chunk payload
}

// NewReader parses the RIFF/WAVE header chunks from r and returns a Reader.
// The reader must be positioned at the start of the RIFF header. After
// NewReader returns, r is advanced to the start of the data chunk payload
// and the Reader owns reads from that point forward.
//
// All header-bearing chunks (fmt, LIST/INFO, bext, cue) that appear before
// the data chunk are parsed and merged into Header. Unknown chunks before
// data are preserved in Header.Extra.Unknown. Anything after the data
// chunk is ignored.
func NewReader(r io.Reader) (*Reader, error) {
	id, size, err := readChunkHeader(r)
	if err != nil {
		return nil, err
	}
	if id != idRIFF {
		return nil, ErrNotRIFF
	}
	_ = size // we trust the underlying reader for EOF rather than the RIFF size

	var waveID [4]byte
	if _, err := io.ReadFull(r, waveID[:]); err != nil {
		return nil, err
	}
	if waveID != idWAVE {
		return nil, ErrNotWAVE
	}

	header := Header{
		Format: "wav",
		Extra: Extras{
			Unknown: map[string][]byte{},
		},
	}
	rawTags := containers.NewTags()

	var (
		fmtParsed bool
		fmtInfo   fmtChunk
	)

	// Walk chunks until we find "data".
	for {
		id, size, err = readChunkHeader(r)
		if err != nil {
			if err == io.EOF && fmtParsed {
				return nil, ErrMissingData
			}
			return nil, err
		}

		switch id {
		case idFMT:
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			fmtInfo, err = parseFmt(body)
			if err != nil {
				return nil, err
			}
			fmtParsed = true

			header.SampleRate = int(fmtInfo.SampleRate)
			header.Channels = int(fmtInfo.Channels)
			header.Extra.FormatTag = fmtInfo.FormatTag
			header.Extra.BitsPerSample = fmtInfo.BitsPerSample

			sf, ok := sampleFormatFor(fmtInfo)
			if !ok {
				return nil, ErrUnsupportedFormat
			}
			header.SampleFormat = sf
			header.BitRate = int(fmtInfo.ByteRate) * 8

			if err := skip(r, int64(padByte(size))); err != nil {
				return nil, err
			}

		case idDATA:
			if !fmtParsed {
				return nil, ErrMissingFmt
			}
			if fmtInfo.BlockAlign > 0 {
				frames := int(size / uint32(fmtInfo.BlockAlign))
				header.Duration = DurationFromSamples(frames, int(fmtInfo.SampleRate))
			}
			header.Tags = containers.StandardTagsFromMap(rawTags)
			return &Reader{
				header: header,
				data:   io.LimitReader(r, int64(size)),
			}, nil

		case idLIST:
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			if len(body) >= 4 && bytes.Equal(body[:4], idINFO[:]) {
				if err := parseLISTInfo(body[4:], rawTags); err != nil {
					return nil, err
				}
			} else {
				header.Extra.Unknown["LIST"] = body
			}
			if err := skip(r, int64(padByte(size))); err != nil {
				return nil, err
			}

		case idBEXT:
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			header.Extra.Bext = parseBext(body)
			if err := skip(r, int64(padByte(size))); err != nil {
				return nil, err
			}

		case idCUE:
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			header.Extra.Cues = parseCue(body)
			if err := skip(r, int64(padByte(size))); err != nil {
				return nil, err
			}

		default:
			body := make([]byte, size)
			if _, err := io.ReadFull(r, body); err != nil {
				return nil, err
			}
			header.Extra.Unknown[string(id[:])] = body
			if err := skip(r, int64(padByte(size))); err != nil {
				return nil, err
			}
		}
	}
}

// Header returns the parsed WAV header.
func (r *Reader) Header() Header { return r.header }

// Data returns an io.Reader over the raw PCM bytes in the data chunk.
// Feed this to [go-mediatoolkit/codec/pcm.NewDecoder] to obtain float64
// samples. Reading past the end returns io.EOF.
func (r *Reader) Data() io.Reader { return r.data }

// sampleFormatFor maps a fmt chunk to a mutations.SampleFormat.
func sampleFormatFor(f fmtChunk) (mutations.SampleFormat, bool) {
	// Resolve extensible format by inspecting the SubFormat GUID.
	tag := f.FormatTag
	if tag == formatExtensible && f.HasSubFormat {
		switch {
		case bytes.Equal(f.SubFormat[:2], []byte{0x01, 0x00}):
			tag = formatPCM
		case bytes.Equal(f.SubFormat[:2], []byte{0x03, 0x00}):
			tag = formatIEEEFloat
		}
	}

	bits := f.BitsPerSample
	switch tag {
	case formatPCM:
		switch bits {
		case 8:
			return mutations.FormatUint8, true
		case 16:
			return mutations.FormatInt16, true
		case 24:
			return mutations.FormatInt24, true
		case 32:
			return mutations.FormatInt32, true
		}
	case formatIEEEFloat:
		switch bits {
		case 32:
			return mutations.FormatFloat32, true
		case 64:
			return mutations.FormatFloat64, true
		}
	}
	return 0, false
}

// parseCue decodes a "cue " chunk body. Returns nil on malformed input.
func parseCue(body []byte) []CuePoint {
	if len(body) < 4 {
		return nil
	}
	count := binary.LittleEndian.Uint32(body[:4])
	cues := make([]CuePoint, 0, count)
	off := 4
	for i := uint32(0); i < count && off+24 <= len(body); i++ {
		cp := CuePoint{
			ID:           binary.LittleEndian.Uint32(body[off : off+4]),
			Position:     binary.LittleEndian.Uint32(body[off+4 : off+8]),
			ChunkStart:   binary.LittleEndian.Uint32(body[off+12 : off+16]),
			BlockStart:   binary.LittleEndian.Uint32(body[off+16 : off+20]),
			SampleOffset: binary.LittleEndian.Uint32(body[off+20 : off+24]),
		}
		copy(cp.DataChunkID[:], body[off+8:off+12])
		cues = append(cues, cp)
		off += 24
	}
	return cues
}
