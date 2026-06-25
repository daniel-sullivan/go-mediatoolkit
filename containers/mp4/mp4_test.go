package mp4

import (
	"bytes"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aaccodec "github.com/daniel-sullivan/go-mediatoolkit/codec/aac"
	"github.com/daniel-sullivan/go-mediatoolkit/containers"
	aaclib "github.com/daniel-sullivan/go-mediatoolkit/libraries/aac"
)

// The Reader's PacketReader must satisfy both the codec/aac and the generic
// containers packet-reader interfaces so callers can pipe straight into
// aac.NewDecoder.
var (
	_ aaccodec.PacketReader   = (*PacketReader)(nil)
	_ containers.PacketReader = (*PacketReader)(nil)
)

// fakeAccessUnits returns a handful of distinctly-sized byte slices standing
// in for AAC access units. The container layer treats them as opaque, so
// their contents only need to be recoverable byte-for-byte.
func fakeAccessUnits() [][]byte {
	return [][]byte{
		{0x21, 0x00, 0x49, 0x90, 0x02},
		{0x21, 0x1a, 0xff, 0x01, 0x02, 0x03},
		{0x21, 0x10, 0x05},
		{0x21, 0x00, 0x49, 0x90, 0x02, 0x19, 0x33, 0x33},
	}
}

func writeMP4(t *testing.T, h Header, units [][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, h)
	require.NoError(t, err)
	for _, u := range units {
		require.NoError(t, w.WritePacket(u))
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}

// boxVersion returns the version byte (the first FullBox header byte) of the
// first descendant box of the given type found by depth-first walk, or -1 if
// the box is absent.
func boxVersion(t *testing.T, file []byte, typ BoxType) int {
	t.Helper()
	var walk func(boxes []box) int
	walk = func(boxes []box) int {
		for i := range boxes {
			if boxes[i].Type == typ {
				if len(boxes[i].Payload) < 1 {
					return -1
				}
				return int(boxes[i].Payload[0])
			}
			// Recurse into container boxes (those whose payload parses as
			// further boxes). Leaf boxes simply fail to parse and are skipped.
			if children, err := readBoxes(boxes[i].Payload, boxes[i].PayloadAt); err == nil && len(children) > 0 {
				if v := walk(children); v >= 0 {
					return v
				}
			}
		}
		return -1
	}
	top, err := readBoxes(file, 0)
	require.NoError(t, err)
	return walk(top)
}

// hasBox reports whether a box of the given type appears anywhere in the file.
func hasBox(t *testing.T, file []byte, typ BoxType) bool {
	t.Helper()
	var walk func(boxes []box) bool
	walk = func(boxes []box) bool {
		for i := range boxes {
			if boxes[i].Type == typ {
				return true
			}
			if children, err := readBoxes(boxes[i].Payload, boxes[i].PayloadAt); err == nil && len(children) > 0 {
				if walk(children) {
					return true
				}
			}
		}
		return false
	}
	top, err := readBoxes(file, 0)
	require.NoError(t, err)
	return walk(top)
}

var (
	boxMvhd  = BoxType{'m', 'v', 'h', 'd'}
	boxTkhd  = BoxType{'t', 'k', 'h', 'd'}
	boxMdhdT = BoxType{'m', 'd', 'h', 'd'}
)

// TestWriterVersion0WhenFits verifies the writer keeps emitting the
// compatible version-0 mvhd/tkhd/mdhd and 32-bit stco boxes for short streams
// that fit a uint32 (no regression: 64-bit boxes are only used when needed).
func TestWriterVersion0WhenFits(t *testing.T) {
	units := fakeAccessUnits()
	data := writeMP4(t, Header{SampleRate: 44100, Channels: 2}, units)

	assert.Equal(t, 0, boxVersion(t, data, boxMvhd), "mvhd should be v0 when duration fits uint32")
	assert.Equal(t, 0, boxVersion(t, data, boxTkhd), "tkhd should be v0 when duration fits uint32")
	assert.Equal(t, 0, boxVersion(t, data, boxMdhdT), "mdhd should be v0 when duration fits uint32")
	assert.True(t, hasBox(t, data, BoxStco), "stco (32-bit) should be used for a small file")
	assert.False(t, hasBox(t, data, BoxCo64), "co64 should not appear for a small file")
}

// TestWriterVersion1OnOverlongDuration verifies that a track whose duration in
// timescale ticks overflows a uint32 is emitted with version-1 (64-bit)
// mvhd/tkhd/mdhd boxes (ISO/IEC 14496-12 §8.2.2/§8.3.2/§8.4.2) — the case that
// previously failed loud — and that it round-trips through the reader with the
// 64-bit duration recovered.
func TestWriterVersion1OnOverlongDuration(t *testing.T) {
	var buf bytes.Buffer
	w, err := NewWriter(&buf, Header{SampleRate: 44100, Channels: 2})
	require.NoError(t, err)
	for i := 0; i < 8; i++ {
		require.NoError(t, w.WritePacket([]byte{0x21, 0x00, 0x49, 0x90, 0x02}))
	}
	// 8 frames × 2^30 ticks/frame = 2^33 ticks > MaxUint32, forcing v1 boxes.
	const frameTicks = 1 << 30
	w.asc.FrameSamples = frameTicks
	require.NoError(t, w.Close())

	data := buf.Bytes()
	require.NotZero(t, len(data), "a valid v1 file should be written")

	assert.Equal(t, 1, boxVersion(t, data, boxMvhd), "mvhd must be v1 for an overlong duration")
	assert.Equal(t, 1, boxVersion(t, data, boxTkhd), "tkhd must be v1 for an overlong duration")
	assert.Equal(t, 1, boxVersion(t, data, boxMdhdT), "mdhd must be v1 for an overlong duration")
	// Short streams stay at stco; the duration overflow alone must not promote
	// the chunk-offset box.
	assert.True(t, hasBox(t, data, BoxStco), "stco stays 32-bit when only the duration overflows")

	// Parse back: the 64-bit duration must survive the round trip.
	r, err := NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	got := drainPackets(t, r.Packets())
	require.Len(t, got, 8)

	wantTicks := uint64(8) * frameTicks
	require.Greater(t, wantTicks, uint64(0xFFFFFFFF), "test fixture must exceed uint32")
	wantDur := time.Duration(float64(wantTicks) / 44100.0 * float64(time.Second))
	// Duration derived from stts (count×delta, summed as uint64).
	assert.InEpsilon(t, wantDur.Seconds(), r.Header().Duration.Seconds(), 1e-6)
}

// TestWriterCo64OnLargeOffset verifies the writer emits a co64 (64-bit
// chunk-offset) box (ISO/IEC 14496-12 §8.7.5) in place of stco — the case that
// previously failed loud — and that the file round-trips with the access units
// recovered byte-for-byte. forceCo64 exercises the co64 write/read path with a
// real (small) offset so no multi-gigabyte payload is buffered.
func TestWriterCo64OnLargeOffset(t *testing.T) {
	units := fakeAccessUnits()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, Header{SampleRate: 44100, Channels: 2})
	require.NoError(t, err)
	w.forceCo64 = true
	for _, u := range units {
		require.NoError(t, w.WritePacket(u))
	}
	require.NoError(t, w.Close())

	data := buf.Bytes()
	assert.True(t, hasBox(t, data, BoxCo64), "co64 must be emitted")
	assert.False(t, hasBox(t, data, BoxStco), "stco must not appear when co64 is used")

	r, err := NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	got := drainPackets(t, r.Packets())
	require.Len(t, got, len(units))
	for i := range units {
		assert.Equal(t, units[i], got[i], "access unit %d round-trips through co64", i)
	}
}

// TestChunkOffsetBoxForm exercises buildChunkOffset directly across the
// stco/co64 boundary: offsets up to uint32 max stay stco, a >4 GiB offset
// promotes to co64, and parseCo64 reads the 64-bit value back exactly.
func TestChunkOffsetBoxForm(t *testing.T) {
	tests := []struct {
		name    string
		offset  uint64
		force   bool
		wantBox string
	}{
		{"small stco", 0x1000, false, "stco"},
		{"uint32 max stco", 0xFFFFFFFF, false, "stco"},
		{"over 4GiB co64", 0x1_0000_0000, false, "co64"},
		{"huge co64", 0x1234_5678_9ABC, false, "co64"},
		{"forced co64", 0x2000, true, "co64"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildChunkOffset(tc.offset, tc.force)
			boxes, err := readBoxes(raw, 0)
			require.NoError(t, err)
			require.Len(t, boxes, 1)
			require.Equal(t, tc.wantBox, boxes[0].Type.String())

			var offsets []uint64
			if tc.wantBox == "co64" {
				offsets, err = parseCo64(boxes[0].Payload)
			} else {
				offsets, err = parseStco(boxes[0].Payload)
			}
			require.NoError(t, err)
			require.Len(t, offsets, 1)
			assert.Equal(t, tc.offset, offsets[0])
		})
	}
}

// TestHeaderBoxVersionRoundTrip exercises buildMvhd/buildTkhd/buildMdhd at both
// versions and confirms parseMdhd recovers the 64-bit duration from the v1
// layout (the reader's media-header parse).
func TestHeaderBoxVersionRoundTrip(t *testing.T) {
	const timescale = 48000
	tests := []struct {
		name     string
		duration uint64
		wantVer  byte
	}{
		{"v0 small", 1_000_000, 0},
		{"v0 uint32 max", 0xFFFFFFFF, 0},
		{"v1 over uint32", 0x1_0000_0000, 1},
		{"v1 huge", 0x7FFF_FFFF_FFFF, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			for _, b := range []struct {
				typ string
				raw []byte
			}{
				{"mvhd", buildMvhd(timescale, tc.duration)},
				{"tkhd", buildTkhd(tc.duration)},
				{"mdhd", buildMdhd(timescale, tc.duration)},
			} {
				boxes, err := readBoxes(b.raw, 0)
				require.NoError(t, err)
				require.Len(t, boxes, 1)
				require.Equal(t, b.typ, boxes[0].Type.String())
				assert.Equal(t, tc.wantVer, boxes[0].Payload[0], "%s version", b.typ)
			}

			// parseMdhd must recover timescale + 64-bit duration at both versions.
			mdhd, err := readBoxes(buildMdhd(timescale, tc.duration), 0)
			require.NoError(t, err)
			ts, dur := parseMdhd(mdhd[0].Payload)
			assert.Equal(t, uint32(timescale), ts)
			assert.Equal(t, tc.duration, dur)
		})
	}
}

func TestWriterReaderRoundTrip(t *testing.T) {
	const (
		sampleRate = 44100
		channels   = 2
	)
	units := fakeAccessUnits()

	tests := []struct {
		name string
		tags containers.Tags
	}{
		{
			name: "no tags",
			tags: nil,
		},
		{
			name: "standard tags",
			tags: func() containers.Tags {
				tg := containers.NewTags()
				tg.Set("TITLE", "Test Track")
				tg.Set("ARTIST", "Daniel")
				tg.Set("ALBUM", "MP4 Phase")
				tg.Set("DATE", "2026")
				return tg
			}(),
		},
		{
			name: "freeform plus tracknumber",
			tags: func() containers.Tags {
				tg := containers.NewTags()
				tg.Set("TITLE", "Numbered")
				tg.Set("TRACKNUMBER", "7")
				return tg
			}(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := Header{SampleRate: sampleRate, Channels: channels}
			if tc.tags != nil {
				h.Tags = containers.StandardTagsFromMap(tc.tags)
			}

			data := writeMP4(t, h, units)

			r, err := NewReader(bytes.NewReader(data))
			require.NoError(t, err)

			hdr := r.Header()
			assert.Equal(t, FormatM4A, hdr.Format)
			assert.Equal(t, sampleRate, hdr.SampleRate)
			assert.Equal(t, channels, hdr.Channels)
			assert.Equal(t, aaclib.AOTAACLC, hdr.Extra.Config.ObjectType)
			assert.Equal(t, sampleRate, hdr.Extra.Config.SampleRate)
			assert.Equal(t, channels, hdr.Extra.Config.Channels)

			// Sample table recovers one size per access unit.
			require.Len(t, hdr.Extra.SampleTable.SampleSizes, len(units))
			for i, u := range units {
				assert.Equal(t, uint32(len(u)), hdr.Extra.SampleTable.SampleSizes[i])
			}

			// Access units come back byte-for-byte in order.
			got := drainPackets(t, r.Packets())
			require.Len(t, got, len(units))
			for i := range units {
				assert.Equal(t, units[i], got[i], "access unit %d", i)
			}

			// Tags project back through StandardTags.
			if tc.tags != nil {
				if want := tc.tags.Get("TITLE"); want != "" {
					require.NotNil(t, hdr.Tags.Title)
					assert.Equal(t, want, *hdr.Tags.Title)
				}
				if want := tc.tags.Get("ARTIST"); want != "" {
					require.NotNil(t, hdr.Tags.Artist)
					assert.Equal(t, want, *hdr.Tags.Artist)
				}
				if want := tc.tags.Get("ALBUM"); want != "" {
					require.NotNil(t, hdr.Tags.Album)
					assert.Equal(t, want, *hdr.Tags.Album)
				}
				if want := tc.tags.Get("DATE"); want != "" {
					require.NotNil(t, hdr.Tags.Date)
					assert.Equal(t, want, *hdr.Tags.Date)
				}
				if want := tc.tags.Get("TRACKNUMBER"); want != "" {
					require.NotNil(t, hdr.Tags.TrackNumber)
					assert.Equal(t, want, *hdr.Tags.TrackNumber)
				}
			}
		})
	}
}

func drainPackets(t *testing.T, pr *PacketReader) [][]byte {
	t.Helper()
	var out [][]byte
	for {
		pkt, err := pr.ReadPacket()
		if err == io.EOF {
			return out
		}
		require.NoError(t, err)
		out = append(out, pkt)
	}
}

func TestReaderRejectsNonMP4(t *testing.T) {
	_, err := NewReader(bytes.NewReader([]byte("not an mp4 file at all!!")))
	require.ErrorIs(t, err, ErrNotMP4)
}

func TestReaderRejectsMissingMoov(t *testing.T) {
	// A valid ftyp box, but nothing else.
	ftyp := buildFtyp(Header{})
	_, err := NewReader(bytes.NewReader(ftyp))
	require.ErrorIs(t, err, ErrMissingMoov)
}

func TestReaderDurationFromStts(t *testing.T) {
	const sampleRate = 48000
	units := fakeAccessUnits()
	h := Header{SampleRate: sampleRate, Channels: 1}
	data := writeMP4(t, h, units)

	r, err := NewReader(bytes.NewReader(data))
	require.NoError(t, err)

	// 4 access units × 1024 samples / 48000 Hz ≈ 85.3 ms.
	hdr := r.Header()
	assert.Greater(t, hdr.Duration.Milliseconds(), int64(80))
	assert.Less(t, hdr.Duration.Milliseconds(), int64(90))
}

func TestBoxRoundTrip(t *testing.T) {
	body := []byte{1, 2, 3, 4, 5}
	raw := buildBox("free", body)
	boxes, err := readBoxes(raw, 0)
	require.NoError(t, err)
	require.Len(t, boxes, 1)
	assert.Equal(t, "free", boxes[0].Type.String())
	assert.Equal(t, body, boxes[0].Payload)
	assert.Equal(t, int64(0), boxes[0].HeaderStart)
	assert.Equal(t, int64(8), boxes[0].PayloadAt)
}

func TestAudioSpecificConfigRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		objectType aaclib.AudioObjectType
		sampleRate int
		channels   int
	}{
		{"aac-lc 44100 stereo", aaclib.AOTAACLC, 44100, 2},
		{"aac-lc 48000 mono", aaclib.AOTAACLC, 48000, 1},
		{"aac-lc 96000 stereo", aaclib.AOTAACLC, 96000, 2},
		{"aac-main 32000 stereo", aaclib.AOTAACMain, 32000, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			asc := aaclib.AudioSpecificConfig{
				ObjectType: tc.objectType,
				SampleRate: tc.sampleRate,
				Channels:   tc.channels,
			}
			raw := encodeAudioSpecificConfig(asc)
			got, err := decodeAudioSpecificConfig(raw)
			require.NoError(t, err)
			assert.Equal(t, tc.objectType, got.ObjectType)
			assert.Equal(t, tc.sampleRate, got.SampleRate)
			assert.Equal(t, tc.channels, got.Channels)
		})
	}
}

func TestEsdsRoundTrip(t *testing.T) {
	asc := aaclib.AudioSpecificConfig{
		ObjectType: aaclib.AOTAACLC,
		SampleRate: 44100,
		Channels:   2,
	}
	esds := buildEsds(asc)
	// buildEsds wraps the body in a box; parseESDS wants the box body.
	boxes, err := readBoxes(esds, 0)
	require.NoError(t, err)
	require.Len(t, boxes, 1)
	require.Equal(t, "esds", boxes[0].Type.String())

	got, err := parseESDS(boxes[0].Payload)
	require.NoError(t, err)
	assert.Equal(t, aaclib.AOTAACLC, got.ObjectType)
	assert.Equal(t, 44100, got.SampleRate)
	assert.Equal(t, 2, got.Channels)
}

func TestSampleTableMultiChunk(t *testing.T) {
	// Two chunks: chunk 1 holds 2 samples, chunk 2 holds 1 sample.
	st := SampleTable{
		SampleSizes:  []uint32{10, 20, 30},
		ChunkOffsets: []uint64{100, 200},
		SampleToChunk: []SampleToChunkEntry{
			{FirstChunk: 1, SamplesPerChunk: 2, SampleDescriptionIndex: 1},
			{FirstChunk: 2, SamplesPerChunk: 1, SampleDescriptionIndex: 1},
		},
	}
	locs, err := resolveSampleOffsets(st)
	require.NoError(t, err)
	require.Len(t, locs, 3)
	assert.Equal(t, sampleLocation{offset: 100, size: 10}, locs[0])
	assert.Equal(t, sampleLocation{offset: 110, size: 20}, locs[1])
	assert.Equal(t, sampleLocation{offset: 200, size: 30}, locs[2])
}
