package mp3

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	mp3lib "github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3"
)

// requireEncoder skips the calling test when the MP3 encoder is not compiled
// into this build. The LAME-derived [libraries/mp3.Encoder] is only available
// under -tags=mp3lame; otherwise it returns [mp3lib.ErrEncoderRequiresLAME].
// NewWriter constructs the encoder lazily, so the sentinel surfaces on the first
// Encode (or Encoder()) — that is what we probe here. Reader, ID3 tag, and
// metadata-only Writer tests do not call this and keep running in every build.
func requireEncoder(t *testing.T) {
	t.Helper()
	w, err := NewWriter(&bytes.Buffer{}, Header{SampleRate: 44100, Channels: 2})
	require.NoError(t, err)
	// The encoder is built lazily; ask for it to surface the backend sentinel.
	if _, err := w.Encoder(); errors.Is(err, mp3lib.ErrEncoderRequiresLAME) || errors.Is(err, mp3lib.ErrNotImplemented) {
		t.Skip("requires -tags mp3lame (LGPL encoder)")
	} else {
		require.NoError(t, err)
	}
	_ = w.Close()
}

// syntheticFrame is a minimal byte sequence that begins with a valid MPEG
// audio frame sync (0xFF 0xFB = MPEG-1 Layer III). The container layer only
// inspects the leading sync to confirm the stream is MP3; it never decodes the
// audio, so arbitrary trailing bytes stand in for frame payload.
var syntheticFrame = append([]byte{0xFF, 0xFB, 0x90, 0x00}, bytes.Repeat([]byte{0x55}, 60)...)

func TestID3v2EncodeParseRoundTrip(t *testing.T) {
	tags := containers.NewTags()
	tags.Set("TITLE", "Round Trip")
	tags.Set("ARTIST", "Daniel")
	tags.Add("ARTIST", "Test Artist") // multi-value
	tags.Set("ALBUM", "Layer III")
	tags.Set("DATE", "2026")

	full := encodeID3v2(tags)
	require.Greater(t, len(full), 10)
	assert.Equal(t, []byte("ID3"), full[0:3])
	assert.Equal(t, byte(3), full[3]) // ID3v2.3

	// Strip the 10-byte tag header and re-parse the frame body.
	body := full[10:]
	v2, err := parseID3v2(int(full[3]), false, body)
	require.NoError(t, err)

	assert.Equal(t, "Round Trip", v2.Tags.Get("TITLE"))
	assert.Equal(t, []string{"Daniel", "Test Artist"}, v2.Tags.GetAll("ARTIST"))
	assert.Equal(t, "Layer III", v2.Tags.Get("ALBUM"))
	assert.Equal(t, "2026", v2.Tags.Get("DATE"))
}

func TestReaderParsesID3v2AndPreservesBytes(t *testing.T) {
	tags := containers.NewTags()
	tags.Set("TITLE", "Hello")
	tags.Set("ARTIST", "World")

	id3 := encodeID3v2(tags)
	original := append(append([]byte{}, id3...), syntheticFrame...)

	r, err := NewReader(bytes.NewReader(original))
	require.NoError(t, err)

	hdr := r.Header()
	assert.Equal(t, "mp3", hdr.Format)
	require.NotNil(t, hdr.Tags.Title)
	assert.Equal(t, "Hello", *hdr.Tags.Title)
	require.NotNil(t, hdr.Tags.Artist)
	assert.Equal(t, "World", *hdr.Tags.Artist)
	assert.Equal(t, 3, hdr.Extra.ID3v2Version)

	// The first frame header (0xFF 0xFB 0x90 0x00) is peeked past the ID3v2
	// prefix: MPEG-1 Layer III, sample-rate index 0 (44100 Hz), stereo.
	assert.Equal(t, 44100, hdr.SampleRate)
	assert.Equal(t, 2, hdr.Channels)
	assert.Equal(t, mp3lib.MPEGVersion1, hdr.Extra.StreamInfo.Version)
	assert.Equal(t, 44100, hdr.Extra.StreamInfo.SampleRate)
	assert.Equal(t, 2, hdr.Extra.StreamInfo.Channels)
	assert.Equal(t, 1152, hdr.Extra.StreamInfo.SamplesPerFrame)

	// Data() must replay the ORIGINAL bytes (ID3 prefix + frames) verbatim.
	got, err := io.ReadAll(r.Data())
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestReaderAcceptsRawFrameSync(t *testing.T) {
	// No ID3 tag — stream begins directly with an MPEG frame sync.
	r, err := NewReader(bytes.NewReader(syntheticFrame))
	require.NoError(t, err)

	hdr := r.Header()
	assert.Equal(t, "mp3", hdr.Format)
	assert.Equal(t, 0, hdr.Extra.ID3v2Version)
	assert.False(t, hdr.Extra.HasID3v1)

	// With no ID3v2 tag the frame header is the leading bytes themselves; the
	// peek still recovers the parameters (44100 Hz, stereo) from the sniffed
	// magic bytes seeded into the scan.
	assert.Equal(t, 44100, hdr.SampleRate)
	assert.Equal(t, 2, hdr.Channels)

	got, err := io.ReadAll(r.Data())
	require.NoError(t, err)
	assert.Equal(t, syntheticFrame, got)
}

func TestParseFrameHeader(t *testing.T) {
	cases := []struct {
		name    string
		hdr     []byte
		ok      bool
		version mp3lib.MPEGVersion
		rate    int
		ch      int
		spf     int
		bitrate int
	}{
		{
			// FF FB 90 00: MPEG-1, Layer III, bit-rate idx 9 (128 kbps),
			// sample-rate idx 0 (44100), channel mode 00 (stereo).
			name: "mpeg1 44100 stereo 128k", hdr: []byte{0xFF, 0xFB, 0x90, 0x00},
			ok: true, version: mp3lib.MPEGVersion1, rate: 44100, ch: 2, spf: 1152, bitrate: 128000,
		},
		{
			// FF FB 92 C0: bit-rate idx 9 (128 kbps), sample-rate idx 0
			// (44100), channel mode 11 (mono).
			name: "mpeg1 44100 mono", hdr: []byte{0xFF, 0xFB, 0x92, 0xC0},
			ok: true, version: mp3lib.MPEGVersion1, rate: 44100, ch: 1, spf: 1152, bitrate: 128000,
		},
		{
			// FF F3 50 00: version 10 (MPEG-2), Layer III, bit-rate idx 5
			// (40 kbps on V2), sample-rate idx 0 (22050), stereo.
			name: "mpeg2 22050 stereo", hdr: []byte{0xFF, 0xF3, 0x50, 0x00},
			ok: true, version: mp3lib.MPEGVersion2, rate: 22050, ch: 2, spf: 576, bitrate: 40000,
		},
		{
			// FF E3 50 00: version 00 (MPEG-2.5), Layer III, sample-rate idx 0
			// (11025), stereo.
			name: "mpeg2.5 11025 stereo", hdr: []byte{0xFF, 0xE3, 0x50, 0x00},
			ok: true, version: mp3lib.MPEGVersion25, rate: 11025, ch: 2, spf: 576, bitrate: 40000,
		},
		{
			// FF FB 80 00: bit-rate idx 8 (112k V1), sample-rate idx 0,
			// stereo — covers a different rate index.
			name: "mpeg1 48000 stereo", hdr: []byte{0xFF, 0xFB, 0x94, 0x00},
			ok: true, version: mp3lib.MPEGVersion1, rate: 48000, ch: 2, spf: 1152, bitrate: 128000,
		},
		{
			// FF FB 0C 00: bit-rate idx 0 (free) -> BitRate 0, still accepted.
			name: "free bitrate accepted", hdr: []byte{0xFF, 0xFB, 0x00, 0x00},
			ok: true, version: mp3lib.MPEGVersion1, rate: 44100, ch: 2, spf: 1152, bitrate: 0,
		},
		{
			// Version bits 01 are reserved.
			name: "reserved version", hdr: []byte{0xFF, 0xEB, 0x90, 0x00}, ok: false,
		},
		{
			// Layer bits 00 are reserved (here: Layer II = 10).
			name: "not layer III", hdr: []byte{0xFF, 0xFD, 0x90, 0x00}, ok: false,
		},
		{
			// Sample-rate idx 11 is reserved.
			name: "reserved sample rate", hdr: []byte{0xFF, 0xFB, 0x9C, 0x00}, ok: false,
		},
		{
			name: "not a sync", hdr: []byte{0x00, 0x00, 0x90, 0x00}, ok: false,
		},
		{
			name: "too short", hdr: []byte{0xFF, 0xFB, 0x90}, ok: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info, ok := parseFrameHeader(tc.hdr)
			assert.Equal(t, tc.ok, ok)
			if !tc.ok {
				return
			}
			assert.Equal(t, tc.version, info.Version)
			assert.Equal(t, tc.rate, info.SampleRate)
			assert.Equal(t, tc.ch, info.Channels)
			assert.Equal(t, tc.spf, info.SamplesPerFrame)
			assert.Equal(t, tc.bitrate, info.BitRate)
		})
	}
}

// TestReaderDegradesOnUnparseableFrame asserts that a stream whose first sync
// is followed by an unparseable header (reserved fields) is still accepted —
// SampleRate / Channels just stay zero rather than erroring.
func TestReaderDegradesOnUnparseableFrame(t *testing.T) {
	// Valid sync, but reserved version bits (01) -> parseFrameHeader fails.
	bad := append([]byte{0xFF, 0xEB, 0x90, 0x00}, bytes.Repeat([]byte{0x55}, 60)...)
	r, err := NewReader(bytes.NewReader(bad))
	require.NoError(t, err)
	hdr := r.Header()
	assert.Equal(t, 0, hdr.SampleRate)
	assert.Equal(t, 0, hdr.Channels)

	got, err := io.ReadAll(r.Data())
	require.NoError(t, err)
	assert.Equal(t, bad, got)
}

func TestReaderRejectsNonMP3(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"ogg magic", []byte("OggS........")},
		{"flac magic", []byte("fLaC....")},
		{"random text", []byte("not audio at all")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewReader(bytes.NewReader(tc.input))
			require.ErrorIs(t, err, ErrNotMP3)
		})
	}
}

func TestReaderParsesID3v1Trailer(t *testing.T) {
	v1 := make([]byte, id3v1Size)
	copy(v1[0:3], "TAG")
	copy(v1[3:33], "Trailer Title")
	copy(v1[33:63], "Trailer Artist")
	copy(v1[63:93], "Trailer Album")
	copy(v1[93:97], "1999")
	copy(v1[97:125], "a comment")
	v1[125] = 0 // ID3v1.1 marker
	v1[126] = 7 // track number

	original := append(append([]byte{}, syntheticFrame...), v1...)

	r, err := NewReader(bytes.NewReader(original))
	require.NoError(t, err)

	hdr := r.Header()
	assert.True(t, hdr.Extra.HasID3v1)
	require.NotNil(t, hdr.Tags.Title)
	assert.Equal(t, "Trailer Title", *hdr.Tags.Title)
	require.NotNil(t, hdr.Tags.Artist)
	assert.Equal(t, "Trailer Artist", *hdr.Tags.Artist)
	require.NotNil(t, hdr.Tags.TrackNumber)
	assert.Equal(t, "7", *hdr.Tags.TrackNumber)

	// Bytes are still preserved end to end.
	got, err := io.ReadAll(r.Data())
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestReaderID3v2OverridesID3v1(t *testing.T) {
	v2tags := containers.NewTags()
	v2tags.Set("TITLE", "V2 Title")
	id3v2 := encodeID3v2(v2tags)

	v1 := make([]byte, id3v1Size)
	copy(v1[0:3], "TAG")
	copy(v1[3:33], "V1 Title")
	copy(v1[33:63], "V1 Artist")

	original := append(append(append([]byte{}, id3v2...), syntheticFrame...), v1...)

	r, err := NewReader(bytes.NewReader(original))
	require.NoError(t, err)
	hdr := r.Header()

	// ID3v2 wins for TITLE; ID3v1 supplies ARTIST that v2 lacks.
	require.NotNil(t, hdr.Tags.Title)
	assert.Equal(t, "V2 Title", *hdr.Tags.Title)
	require.NotNil(t, hdr.Tags.Artist)
	assert.Equal(t, "V1 Artist", *hdr.Tags.Artist)
	assert.True(t, hdr.Extra.HasID3v1)
	assert.Equal(t, 3, hdr.Extra.ID3v2Version)
}

func TestReaderRejectsNil(t *testing.T) {
	_, err := NewReader(nil)
	require.ErrorIs(t, err, ErrBadArg)
}

func TestParseID3v1Invalid(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
	}{
		{"too short", make([]byte, 10)},
		{"bad magic", append([]byte("XXX"), make([]byte, id3v1Size-3)...)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseID3v1(tc.in)
			require.ErrorIs(t, err, ErrInvalidID3)
		})
	}
}

func TestWriterEmitsID3v2Prefix(t *testing.T) {
	// Metadata-only: NewWriter writes the ID3v2 tag eagerly and Close flushes
	// nothing because no audio frame was ever written, so this runs in the
	// default (no-mp3lame) build without the encoder.
	var buf bytes.Buffer
	h := Header{
		SampleRate: 44100,
		Channels:   2,
		Tags: containers.StandardTagsFromMap(func() containers.Tags {
			tg := containers.NewTags()
			tg.Set("TITLE", "Written")
			tg.Set("ARTIST", "Writer")
			return tg
		}()),
	}

	w, err := NewWriter(&buf, h)
	require.NoError(t, err)
	require.NotNil(t, w)

	// The ID3v2 tag is written eagerly at construction, before any frames.
	out := buf.Bytes()
	require.GreaterOrEqual(t, len(out), 10)
	assert.Equal(t, []byte("ID3"), out[0:3])

	// The prefix must parse back to the tags we projected.
	size := synchsafe(out[6:10])
	require.GreaterOrEqual(t, len(out), 10+size)
	v2, err := parseID3v2(int(out[3]), false, out[10:10+size])
	require.NoError(t, err)
	assert.Equal(t, "Written", v2.Tags.Get("TITLE"))
	assert.Equal(t, "Writer", v2.Tags.Get("ARTIST"))

	// Closing the writer flushes the encoder and must not panic.
	require.NoError(t, w.Close())
	require.ErrorIs(t, w.Close(), ErrAlreadyClosed)
}

func TestWriterRejectsNil(t *testing.T) {
	_, err := NewWriter(nil, Header{SampleRate: 44100, Channels: 2})
	require.ErrorIs(t, err, ErrBadArg)
}

func TestWriterEncodeWritesAudioFrames(t *testing.T) {
	requireEncoder(t)
	var buf bytes.Buffer
	w, err := NewWriter(&buf, Header{SampleRate: 44100, Channels: 2})
	require.NoError(t, err)

	// One full frame of interleaved stereo int16 silence.
	frame := make([]int16, mp3lib.MaxSamplesPerFrame*2)
	require.NoError(t, w.Encode(frame))
	require.NoError(t, w.Close())

	// With the encoder available, Encode produces compressed bytes.
	assert.NotZero(t, buf.Len())
}
