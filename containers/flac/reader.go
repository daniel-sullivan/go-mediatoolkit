package flac

import (
	"bytes"
	"io"
	"time"

	"go-mediatoolkit/containers"
	"go-mediatoolkit/mutations"
)

// Reader parses the metadata chain at the start of a FLAC stream and
// exposes a continuous [io.Reader] over the original byte stream so the
// remainder can be fed to [go-mediatoolkit/libraries/flac.NewDecoder].
type Reader struct {
	header Header
	data   io.Reader
}

// NewReader parses the "fLaC" magic and the metadata block chain from
// r and returns a Reader. After NewReader returns, [Reader.Data] yields
// the same bytes the caller would have read from r — magic, metadata
// blocks, and audio frames — buffering only the metadata prefix
// internally. r itself is advanced past the last metadata block; the
// returned io.Reader replays the buffered prefix and then continues
// from r.
func NewReader(r io.Reader) (*Reader, error) {
	var prefix bytes.Buffer
	tee := io.TeeReader(r, &prefix)

	var magic [4]byte
	if _, err := io.ReadFull(tee, magic[:]); err != nil {
		return nil, err
	}
	if magic != flacMagic {
		return nil, ErrNotFLAC
	}

	header := Header{
		Format:       "flac",
		SampleFormat: mutations.FormatFloat64,
		Extra:        Extras{Application: map[[4]byte][]byte{}},
	}
	rawTags := containers.NewTags()
	gotStreamInfo := false

	for {
		var hb [4]byte
		if _, err := io.ReadFull(tee, hb[:]); err != nil {
			return nil, err
		}
		bh := parseBlockHeader(hb)

		body := make([]byte, bh.Length)
		if _, err := io.ReadFull(tee, body); err != nil {
			return nil, err
		}

		switch bh.Type {
		case blockStreamInfo:
			if gotStreamInfo {
				return nil, ErrInvalidMetadata
			}
			si, err := parseStreamInfo(body)
			if err != nil {
				return nil, err
			}
			header.Extra.StreamInfo = si
			header.SampleRate = si.SampleRate
			header.Channels = si.Channels
			if si.SampleRate > 0 && si.TotalSamples > 0 {
				secs := float64(si.TotalSamples) / float64(si.SampleRate)
				header.Duration = time.Duration(secs * float64(time.Second))
			}
			gotStreamInfo = true

		case blockVorbisComment:
			vendor, tags, err := parseVorbisComment(body)
			if err != nil {
				return nil, err
			}
			header.Extra.Vendor = vendor
			for _, k := range tags.Keys() {
				for _, v := range tags.GetAll(k) {
					rawTags.Add(k, v)
				}
			}

		case blockSeekTable:
			pts, err := parseSeekTable(body)
			if err != nil {
				return nil, err
			}
			header.Extra.SeekTable = pts

		case blockPadding:
			header.Extra.Padding += int(bh.Length)

		case blockApplication:
			if len(body) < 4 {
				return nil, ErrInvalidMetadata
			}
			var id [4]byte
			copy(id[:], body[:4])
			payload := make([]byte, len(body)-4)
			copy(payload, body[4:])
			header.Extra.Application[id] = payload

		case blockPicture:
			pic := make([]byte, len(body))
			copy(pic, body)
			header.Extra.Pictures = append(header.Extra.Pictures, pic)

		case blockCuesheet:
			cs := make([]byte, len(body))
			copy(cs, body)
			header.Extra.Cuesheet = cs
		}

		if bh.Last {
			break
		}
	}

	if !gotStreamInfo {
		return nil, ErrMissingStreamInfo
	}
	header.Tags = containers.StandardTagsFromMap(rawTags)

	return &Reader{
		header: header,
		// prefix already contains every byte we tee'd. Subsequent
		// reads from r are appended live.
		data: io.MultiReader(&prefix, r),
	}, nil
}

// Header returns the parsed FLAC header.
func (r *Reader) Header() Header { return r.header }

// Data returns an io.Reader over the entire FLAC stream — magic,
// metadata blocks, and audio frames. Pass it to
// [go-mediatoolkit/libraries/flac.NewDecoder] to decode samples.
func (r *Reader) Data() io.Reader { return r.data }
