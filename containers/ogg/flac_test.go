package ogg

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

func generateFLACTone(t *testing.T, sampleRate, channels, bits, samplesPerChannel int, freq float64) []int32 {
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

func TestOggFLACRoundTrip(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 4096
	)
	input := generateFLACTone(t, sampleRate, channels, bits, samplesPerCh, 1000.0)

	var ogg bytes.Buffer
	w, err := NewFLACWriter(&ogg, sampleRate, channels,
		WithFLACBitsPerSample(bits),
		WithFLACTotalSamples(uint64(samplesPerCh)),
	)
	require.NoError(t, err)
	require.NoError(t, w.Encode(input))
	require.NoError(t, w.Close())
	require.NotZero(t, ogg.Len())

	r, err := NewFLACReader(bytes.NewReader(ogg.Bytes()))
	require.NoError(t, err)

	hdr := r.Header()
	assert.Equal(t, "ogg/flac", hdr.Format)
	assert.Equal(t, sampleRate, hdr.SampleRate)
	assert.Equal(t, channels, hdr.Channels)
	assert.Equal(t, uint8(bits), hdr.Extra.Head.BitsPerSample)
	assert.Equal(t, uint8(1), hdr.Extra.Head.MajorVersion)

	// Decode the synthesised native FLAC stream and compare samples.
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

func TestOggFLACRoundTripWithTags(t *testing.T) {
	const (
		sampleRate   = 44100
		channels     = 2
		bits         = 24
		samplesPerCh = 2048
	)
	input := generateFLACTone(t, sampleRate, channels, bits, samplesPerCh, 880.0)

	var ogg bytes.Buffer
	w, err := NewFLACWriter(&ogg, sampleRate, channels,
		WithFLACBitsPerSample(bits),
		WithFLACTag("TITLE", "OggFLAC"),
		WithFLACTag("ARTIST", "Daniel"),
		WithFLACTag("ARTIST", "Test Artist"),
	)
	require.NoError(t, err)
	require.NoError(t, w.Encode(input))
	require.NoError(t, w.Close())

	r, err := NewFLACReader(bytes.NewReader(ogg.Bytes()))
	require.NoError(t, err)

	tags := r.Header().Tags
	require.NotNil(t, tags.Title)
	assert.Equal(t, "OggFLAC", *tags.Title)
	require.NotNil(t, tags.Artist)
	assert.Equal(t, "Daniel", *tags.Artist)
	require.Contains(t, tags.AdditionalTags, "ARTIST")
	assert.Equal(t, []string{"Test Artist"}, tags.AdditionalTags["ARTIST"])

	// And samples still decode bit-exact.
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

func TestOggFLACRoundTripMultichannel(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 6 // 5.1
		bits         = 16
		samplesPerCh = 1024
	)
	input := generateFLACTone(t, sampleRate, channels, bits, samplesPerCh, 220.0)

	var ogg bytes.Buffer
	w, err := NewFLACWriter(&ogg, sampleRate, channels, WithFLACBitsPerSample(bits))
	require.NoError(t, err)
	require.NoError(t, w.Encode(input))
	require.NoError(t, w.Close())

	r, err := NewFLACReader(bytes.NewReader(ogg.Bytes()))
	require.NoError(t, err)

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
	require.Equal(t, input, decoded)
}

func TestOggFLACReaderRejectsNonFLACOgg(t *testing.T) {
	// Build an Ogg stream whose only logical stream is an Opus one.
	var buf bytes.Buffer
	w, err := NewOpusWriter(&buf, 1)
	require.NoError(t, err)
	require.NoError(t, w.WritePacket([]byte{0, 0, 0, 0, 0}))
	require.NoError(t, w.Close())

	_, err = NewFLACReader(bytes.NewReader(buf.Bytes()))
	require.ErrorIs(t, err, ErrNoFLACStream)
}

func TestStreamingMultipleEncodes(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 8192
		chunkSpc     = 1024
	)
	input := generateFLACTone(t, sampleRate, channels, bits, samplesPerCh, 1500.0)

	var ogg bytes.Buffer
	w, err := NewFLACWriter(&ogg, sampleRate, channels, WithFLACBitsPerSample(bits))
	require.NoError(t, err)
	for off := 0; off < len(input); off += chunkSpc * channels {
		end := off + chunkSpc*channels
		if end > len(input) {
			end = len(input)
		}
		require.NoError(t, w.Encode(input[off:end]))
	}
	require.NoError(t, w.Close())

	r, err := NewFLACReader(bytes.NewReader(ogg.Bytes()))
	require.NoError(t, err)
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
	require.Equal(t, input, decoded)
}

// Sanity: the CRC-16 implementation matches a known vector for
// "123456789" — common ITU/CRC-16 test string. With FLAC's polynomial
// 0x8005 and init 0, the expected CRC-16 over those nine bytes is
// 0xFEE8 ("CRC-16/BUYPASS" / IBM-3740 family).
func TestFLACCRC16(t *testing.T) {
	got := flacCRC16([]byte("123456789"))
	assert.Equal(t, uint16(0xFEE8), got)
}

func TestFrameSampleCountFixed(t *testing.T) {
	// Construct a frame header that encodes blocksize index 5
	// (= 4608 samples per RFC 9639 §11.1 row "576 << (n-2)").
	// Layout: byte 0 = 0xFF (sync hi)
	//         byte 1 = 0xF8 (sync lo + reserved + fixed blocking)
	//         byte 2 = (blocksize<<4) | sample_rate
	//         byte 3 = (chan<<4) | (bps<<1) | reserved
	//         byte 4 = sample number (single-byte UTF-8 varint, 0)
	//         byte 5 = CRC-8 — value not validated by frameSampleCount
	frame := []byte{0xFF, 0xF8, byte(5 << 4), 0x00, 0x00, 0x00}
	got, err := frameSampleCount(frame)
	require.NoError(t, err)
	assert.Equal(t, 4608, got)
}

// ensure the wrapped containers.PacketReader interface compiles.
var _ containers.PacketReader = (*Stream)(nil)
