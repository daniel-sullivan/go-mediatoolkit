package flac

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/containers"
	flaclib "go-mediatoolkit/libraries/flac"
)

func generateTone(t *testing.T, sampleRate, channels, bits, samplesPerChannel int, freq float64) []int32 {
	t.Helper()
	amp := float64(int32(1)<<(bits-1)-1) * 0.95
	out := make([]int32, samplesPerChannel*channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			phase := 2*math.Pi*freq*float64(i)/float64(sampleRate) + float64(ch)*0.1
			out[i*channels+ch] = int32(math.Sin(phase) * amp)
		}
	}
	return out
}

func TestContainerRoundTripPlain(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 4096
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 1000.0)

	var buf bytes.Buffer
	h := Header{
		SampleRate: sampleRate,
		Channels:   channels,
		Extra: Extras{
			StreamInfo: StreamInfo{
				BitsPerSample: bits,
				TotalSamples:  uint64(samplesPerCh),
			},
		},
	}
	w, err := NewWriter(&buf, h)
	require.NoError(t, err)
	require.NoError(t, w.Encode(input))
	require.NoError(t, w.Close())

	r, err := NewReader(&buf)
	require.NoError(t, err)

	hdr := r.Header()
	assert.Equal(t, "flac", hdr.Format)
	assert.Equal(t, sampleRate, hdr.SampleRate)
	assert.Equal(t, channels, hdr.Channels)
	assert.Equal(t, bits, hdr.Extra.StreamInfo.BitsPerSample)
	assert.Equal(t, uint64(samplesPerCh), hdr.Extra.StreamInfo.TotalSamples)

	dec, err := flaclib.NewDecoder(r.Data())
	require.NoError(t, err)
	defer dec.Close()

	decoded := make([]int32, 0, len(input))
	tmp := make([]int32, flaclib.MaxBlockSize*channels)
	for {
		n, err := dec.Decode(tmp)
		if n > 0 {
			decoded = append(decoded, tmp[:n*channels]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, len(input), len(decoded))
	assert.Equal(t, input, decoded)
}

func TestContainerRoundTripWithTags(t *testing.T) {
	const (
		sampleRate   = 44100
		channels     = 2
		bits         = 24
		samplesPerCh = 2048
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 880.0)

	tags := containers.NewTags()
	tags.Set("TITLE", "Round Trip")
	tags.Set("ARTIST", "Daniel")
	tags.Add("ARTIST", "Test Artist") // multi-value
	tags.Set("ALBUM", "Phase 2")

	stdTags := containers.StandardTagsFromMap(tags)

	var buf bytes.Buffer
	h := Header{
		SampleRate: sampleRate,
		Channels:   channels,
		Tags:       stdTags,
		Extra: Extras{
			StreamInfo: StreamInfo{BitsPerSample: bits},
			Vendor:     "go-mediatoolkit/test",
		},
	}
	w, err := NewWriter(&buf, h)
	require.NoError(t, err)
	require.NoError(t, w.Encode(input))
	require.NoError(t, w.Close())

	r, err := NewReader(&buf)
	require.NoError(t, err)
	hdr := r.Header()

	// libFLAC overrides the vendor string at encode time
	// (see libraries/flac.WithVendor doc); just assert non-empty.
	assert.NotEmpty(t, hdr.Extra.Vendor)
	assert.Equal(t, "Round Trip", *hdr.Tags.Title)
	assert.Equal(t, "Daniel", *hdr.Tags.Artist)
	assert.Equal(t, "Phase 2", *hdr.Tags.Album)
	// Multi-value ARTIST: first goes to typed field, extras to AdditionalTags.
	require.Contains(t, hdr.Tags.AdditionalTags, "ARTIST")
	assert.Equal(t, []string{"Test Artist"}, hdr.Tags.AdditionalTags["ARTIST"])

	// And samples still decode.
	dec, err := flaclib.NewDecoder(r.Data())
	require.NoError(t, err)
	defer dec.Close()

	tmp := make([]int32, flaclib.MaxBlockSize*channels)
	decoded := make([]int32, 0, len(input))
	for {
		n, err := dec.Decode(tmp)
		if n > 0 {
			decoded = append(decoded, tmp[:n*channels]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	assert.Equal(t, input, decoded)
}

func TestReaderRejectsNonFLAC(t *testing.T) {
	_, err := NewReader(bytes.NewReader([]byte("OggS........")))
	require.ErrorIs(t, err, ErrNotFLAC)
}

func TestStreamInfoEncodeDecodeRoundTrip(t *testing.T) {
	si := StreamInfo{
		MinBlockSize:  4096,
		MaxBlockSize:  4096,
		MinFrameSize:  10,
		MaxFrameSize:  20000,
		SampleRate:    192000,
		Channels:      6,
		BitsPerSample: 24,
		TotalSamples:  1<<35 + 17,
		MD5Signature:  [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
	}
	body := encodeStreamInfo(si)
	require.Len(t, body, 34)
	got, err := parseStreamInfo(body)
	require.NoError(t, err)
	assert.Equal(t, si, got)
}

func TestVorbisCommentEncodeDecodeRoundTrip(t *testing.T) {
	tags := containers.NewTags()
	tags.Set("TITLE", "Hello")
	tags.Add("ARTIST", "A")
	tags.Add("ARTIST", "B")
	body := encodeVorbisComment("vendor-x", tags)
	vendor, got, err := parseVorbisComment(body)
	require.NoError(t, err)
	assert.Equal(t, "vendor-x", vendor)
	assert.Equal(t, "Hello", got.Get("TITLE"))
	assert.Equal(t, []string{"A", "B"}, got.GetAll("ARTIST"))
}

func TestSeekTableEncodeDecodeRoundTrip(t *testing.T) {
	pts := []SeekPoint{
		{SampleNumber: 0, StreamOffset: 0, FrameSamples: 4096},
		{SampleNumber: 4096, StreamOffset: 1024, FrameSamples: 4096},
		{SampleNumber: 8192, StreamOffset: 2048, FrameSamples: 4096},
	}
	body := encodeSeekTable(pts)
	got, err := parseSeekTable(body)
	require.NoError(t, err)
	assert.Equal(t, pts, got)
}
