package mp3

import (
	"bytes"
	"io"

	"go-mediatoolkit/containers"
	mp3lib "go-mediatoolkit/libraries/mp3"
	"go-mediatoolkit/mutations"
)

// Reader parses leading ID3v2 metadata (and a trailing ID3v1 tag when the
// source is seekable), peeks the first MPEG audio frame header to recover the
// stream's sample rate and channel count, and exposes a continuous [io.Reader]
// over the original byte stream so the bytes can be fed to
// [go-mediatoolkit/libraries/mp3.NewDecoder].
type Reader struct {
	header Header
	data   io.Reader
}

// NewReader parses the metadata bracketing an MP3 stream from r and returns a
// Reader. After NewReader returns, [Reader.Data] yields the same bytes the
// caller would have read from r — ID3v2 prefix, MPEG audio frames, and any
// ID3v1 trailer — buffering only the ID3v2 prefix (and a small first-frame
// lookahead) internally. r itself is advanced past the ID3v2 tag and the
// lookahead; the returned io.Reader replays the buffered prefix and then
// continues from r, so the byte stream is delivered intact.
//
// After skipping any ID3v2 prefix, NewReader peeks the first MPEG audio frame
// header and decodes its sample rate and channel mode into Header.SampleRate,
// Header.Channels, and Extra.StreamInfo (MPEG version, samples-per-frame,
// nominal bit rate). The audio is never decoded — only the 4-byte frame header
// is read. If a valid frame header cannot be found in the lookahead, those
// fields are left zero and NewReader does not error (graceful degradation).
//
// If r additionally satisfies [io.ReadSeeker], NewReader inspects the final
// 128 bytes for an ID3v1 trailer and folds its fields into the header (ID3v2
// values take precedence). The seek offset is restored before NewReader
// returns so the continuous reader is unaffected.
func NewReader(r io.Reader) (*Reader, error) {
	if r == nil {
		return nil, ErrBadArg
	}

	header := Header{
		Format:       "mp3",
		SampleFormat: mutations.FormatFloat64,
		Channels:     0,
		SampleRate:   0,
		Extra: Extras{
			RawFrames: map[string][]byte{},
		},
	}
	rawTags := containers.NewTags()

	// Read the trailing ID3v1 first (if seekable) so its tags form the base
	// layer that the leading ID3v2 may override.
	if seeker, ok := r.(io.ReadSeeker); ok {
		if v1, ok := readID3v1Trailer(seeker); ok {
			header.Extra.HasID3v1 = true
			for _, k := range v1.Keys() {
				for _, v := range v1.GetAll(k) {
					rawTags.Add(k, v)
				}
			}
		}
	}

	var prefix bytes.Buffer
	tee := io.TeeReader(r, &prefix)

	// Peek the first three bytes to distinguish an ID3v2 tag from raw audio.
	var magic [3]byte
	if _, err := io.ReadFull(tee, magic[:]); err != nil {
		return nil, err
	}

	if magic == id3v2Magic {
		// 10-byte ID3v2 header: "ID3", 2 version bytes, 1 flag byte,
		// 4 synchsafe size bytes. We've already consumed the 3 magic bytes.
		var rest [7]byte
		if _, err := io.ReadFull(tee, rest[:]); err != nil {
			return nil, err
		}
		major := int(rest[0])
		flags := rest[2]
		size := synchsafe(rest[3:7])
		if size < 0 {
			return nil, ErrInvalidID3
		}

		body := make([]byte, size)
		if _, err := io.ReadFull(tee, body); err != nil {
			return nil, err
		}

		extendedHeader := flags&0x40 != 0
		v2, err := parseID3v2(major, extendedHeader, body)
		if err != nil {
			return nil, err
		}
		header.Extra.ID3v2Version = v2.Version
		header.Extra.RawFrames = v2.RawFrames
		header.Extra.Pictures = v2.Pictures
		// ID3v2 overrides any ID3v1 value for the same key.
		for _, k := range v2.Tags.Keys() {
			rawTags.Delete(k)
			for _, val := range v2.Tags.GetAll(k) {
				rawTags.Add(k, val)
			}
		}
	} else if !isFrameSync(magic[0], magic[1]) {
		// Not an ID3v2 tag and not an MPEG frame sync.
		return nil, ErrNotMP3
	}

	// Peek the first MPEG audio frame header to recover the sample rate and
	// channel count. Bytes read here go through tee, so they land in prefix and
	// are replayed by Data() — the continuous reader is undisturbed. In the
	// raw-sync path the 3 sniffed magic bytes are already the start of the first
	// frame, so seed the scan with them; in the ID3v2 path tee is positioned at
	// the first audio byte and the seed is empty. The scan reads a short
	// lookahead and parses the 4-byte header; it never decodes audio.
	var head []byte
	if magic != id3v2Magic {
		head = magic[:]
	}
	if info, ok := peekFirstFrameInfo(head, tee); ok {
		header.SampleRate = info.SampleRate
		header.Channels = info.Channels
		header.Extra.StreamInfo = info
	}

	header.Tags = containers.StandardTagsFromMap(rawTags)

	return &Reader{
		header: header,
		// prefix already holds every byte we tee'd (magic + any ID3v2 tag +
		// the first-frame lookahead). Subsequent reads from r append the
		// remaining audio frames live.
		data: io.MultiReader(&prefix, r),
	}, nil
}

// readID3v1Trailer reads the final 128 bytes of a seekable source and, if they
// form a valid ID3v1 tag, returns the parsed tags. The seek offset is restored
// to its original position before returning.
func readID3v1Trailer(s io.ReadSeeker) (containers.Tags, bool) {
	start, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return nil, false
	}
	end, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, false
	}
	defer s.Seek(start, io.SeekStart) //nolint:errcheck // best-effort restore

	if end-start < id3v1Size {
		return nil, false
	}
	if _, err := s.Seek(-id3v1Size, io.SeekEnd); err != nil {
		return nil, false
	}
	buf := make([]byte, id3v1Size)
	if _, err := io.ReadFull(s, buf); err != nil {
		return nil, false
	}
	tags, err := parseID3v1(buf)
	if err != nil {
		return nil, false
	}
	return tags, true
}

// isFrameSync reports whether the first two bytes are an MPEG audio frame
// sync: 11 set bits (0xFF followed by the top three bits of the next byte).
func isFrameSync(b0, b1 byte) bool {
	return b0 == 0xFF && (b1&0xE0) == 0xE0
}

// frameLookahead is the number of bytes peeked past the ID3v2 prefix when
// scanning for the first MPEG audio frame header. A frame begins immediately
// after the ID3v2 tag in a well-formed file; the slack absorbs any stray
// padding (e.g. a trailing zero pad inside an over-sized ID3v2 size field)
// before the sync without buffering a meaningful chunk of audio.
const frameLookahead = 1024

// peekFirstFrameInfo scans head followed by up to frameLookahead bytes read
// from r for an MPEG audio frame sync and parses the 4-byte header that follows.
// head carries any frame bytes already consumed by the magic sniff (empty after
// an ID3v2 tag, where r is positioned at the first audio byte). It returns the
// decoded StreamInfo and true on success, or a zero StreamInfo and false if no
// parseable frame header is found — the caller treats that as "unknown" and
// leaves the header fields zero rather than failing. Bytes read here are tee'd
// into the replay buffer by the caller, so the continuous Data() stream is
// unaffected.
func peekFirstFrameInfo(head []byte, r io.Reader) (mp3lib.StreamInfo, bool) {
	buf := make([]byte, frameLookahead)
	n, err := io.ReadFull(r, buf)
	if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
		return mp3lib.StreamInfo{}, false
	}
	buf = append(append([]byte{}, head...), buf[:n]...)
	for i := 0; i+4 <= len(buf); i++ {
		if !isFrameSync(buf[i], buf[i+1]) {
			continue
		}
		if info, ok := parseFrameHeader(buf[i : i+4]); ok {
			return info, true
		}
	}
	return mp3lib.StreamInfo{}, false
}

// mpegSampleRates indexes the sample-rate table by [version][sampleRateIndex].
// MPEG-1 uses the highest rates, MPEG-2 half of them, MPEG-2.5 a quarter — the
// standard fixed tables from the MPEG audio spec.
var mpegSampleRates = [4][3]int{
	mp3lib.MPEGVersion25: {11025, 12000, 8000},
	mp3lib.MPEGVersion2:  {22050, 24000, 16000},
	mp3lib.MPEGVersion1:  {44100, 48000, 32000},
}

// parseFrameHeader decodes the four bytes of an MPEG audio frame header into a
// StreamInfo. The 32-bit header layout (MSB first) is the standard one:
//
//	AAAAAAAA AAABBCCD EEEEFFGH IIJJKLMM
//
//	A (11) frame sync (all 1s)              F (2)  sample-rate index
//	B (2)  MPEG audio version ID            G (1)  padding bit
//	C (2)  layer description                H (1)  private bit
//	D (1)  protection bit                   I (2)  channel mode
//	E (4)  bit-rate index                   J..M   mode ext / copyright / etc.
//
// parseFrameHeader only needs the version, layer, sample-rate index, and
// channel mode to fill SampleRate / Channels / Version / SamplesPerFrame; the
// bit-rate index yields the nominal BitRate when it maps to a defined value. It
// reports false for reserved version/sample-rate fields, a non-Layer-III
// stream, or a free/reserved bit rate the table cannot resolve (BitRate stays
// zero in the last case but the frame is still accepted).
func parseFrameHeader(h []byte) (mp3lib.StreamInfo, bool) {
	if len(h) < 4 || !isFrameSync(h[0], h[1]) {
		return mp3lib.StreamInfo{}, false
	}

	var version mp3lib.MPEGVersion
	switch (h[1] >> 3) & 0x03 {
	case 0x00:
		version = mp3lib.MPEGVersion25
	case 0x02:
		version = mp3lib.MPEGVersion2
	case 0x03:
		version = mp3lib.MPEGVersion1
	default: // 0x01 is reserved.
		return mp3lib.StreamInfo{}, false
	}

	// Layer description: 0x01 is Layer III (the only layer this toolkit
	// handles). 0x00 is reserved; 0x02/0x03 are Layer II/I.
	if (h[1]>>1)&0x03 != 0x01 {
		return mp3lib.StreamInfo{}, false
	}

	srIndex := (h[2] >> 2) & 0x03
	if srIndex == 0x03 { // reserved
		return mp3lib.StreamInfo{}, false
	}
	sampleRate := mpegSampleRates[version][srIndex]

	// Channel mode: 0b11 is single-channel (mono); everything else is stereo,
	// joint-stereo, or dual-channel — all two channels.
	channels := 2
	if (h[3]>>6)&0x03 == 0x03 {
		channels = 1
	}

	// MPEG-1 Layer III carries 1152 samples/frame; MPEG-2 and 2.5 carry 576.
	samplesPerFrame := 1152
	if version != mp3lib.MPEGVersion1 {
		samplesPerFrame = 576
	}

	return mp3lib.StreamInfo{
		Version:         version,
		SampleRate:      sampleRate,
		Channels:        channels,
		BitRate:         frameBitRate(version, (h[2]>>4)&0x0F),
		SamplesPerFrame: samplesPerFrame,
	}, true
}

// mp3BitRatesV1 / mp3BitRatesV2 are the Layer III nominal bit-rate tables (kbps)
// indexed by the 4-bit bit-rate field. Index 0 is "free" and 0x0F is reserved;
// both map to 0 (unknown). MPEG-2 and MPEG-2.5 share the V2 table.
var (
	mp3BitRatesV1 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mp3BitRatesV2 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
)

// frameBitRate maps a 4-bit bit-rate index to bits per second for the given
// MPEG version's Layer III table, returning 0 for the free/reserved entries.
func frameBitRate(version mp3lib.MPEGVersion, index byte) int {
	if index > 0x0F {
		return 0
	}
	if version == mp3lib.MPEGVersion1 {
		return mp3BitRatesV1[index] * 1000
	}
	return mp3BitRatesV2[index] * 1000
}

// Header returns the parsed MP3 header.
func (r *Reader) Header() Header { return r.header }

// Data returns an io.Reader over the entire MP3 stream — ID3v2 prefix, audio
// frames, and any ID3v1 trailer. Pass it to
// [go-mediatoolkit/libraries/mp3.NewDecoder] to decode samples.
func (r *Reader) Data() io.Reader { return r.data }
