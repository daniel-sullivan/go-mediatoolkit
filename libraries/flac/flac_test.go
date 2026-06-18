package flac

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// generateTone produces interleaved int32 samples of a sine wave at the
// given frequency, sign-extended for the requested bit depth. Per-channel
// phase is offset so multichannel is not a trivial duplicate, while every
// sample stays within the [-2^(bits-1), 2^(bits-1)-1] range FLAC requires.
func generateTone(t *testing.T, sampleRate, channels, bitsPerSample, samplesPerChannel int, freqHz float64) []int32 {
	t.Helper()
	// Use 0.95 of full-scale so per-channel phase offsets cannot push
	// the resulting integer out of range.
	amp := float64(int32(1)<<(bitsPerSample-1)-1) * 0.95
	out := make([]int32, samplesPerChannel*channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			phase := 2*math.Pi*freqHz*float64(i)/float64(sampleRate) + float64(ch)*0.1
			out[i*channels+ch] = int32(math.Sin(phase) * amp)
		}
	}
	return out
}

func TestRoundTripBitDepths(t *testing.T) {
	cases := []struct {
		name string
		bits int
	}{
		{"16-bit", 16},
		{"24-bit", 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				sampleRate   = 48000
				channels     = 2
				samplesPerCh = 4096
				freqHz       = 1000.0
			)
			input := generateTone(t, sampleRate, channels, tc.bits, samplesPerCh, freqHz)

			var encoded bytes.Buffer
			info := StreamInfo{
				SampleRate:    sampleRate,
				Channels:      channels,
				BitsPerSample: tc.bits,
			}
			enc, err := NewEncoder(&encoded, info, WithCompressionLevel(5), WithTotalSamples(uint64(samplesPerCh)))
			require.NoError(t, err)
			require.NoError(t, enc.Encode(input))
			require.NoError(t, enc.Close())
			require.NotZero(t, encoded.Len(), "encoder produced no output")

			dec, err := NewDecoder(bytes.NewReader(encoded.Bytes()))
			require.NoError(t, err)
			defer dec.Close()

			decoded := make([]int32, 0, len(input))
			buf := make([]int32, MaxBlockSize*channels)
			for {
				n, err := dec.Decode(buf)
				if n > 0 {
					decoded = append(decoded, buf[:n*channels]...)
				}
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
			}

			info = dec.StreamInfo()
			assert.Equal(t, sampleRate, info.SampleRate)
			assert.Equal(t, channels, info.Channels)
			assert.Equal(t, tc.bits, info.BitsPerSample)
			require.Equal(t, len(input), len(decoded), "sample count mismatch")
			assert.Equal(t, input, decoded, "lossless round-trip must be bit-exact")
		})
	}
}

func TestMultichannel(t *testing.T) {
	const (
		sampleRate   = 44100
		channels     = 6 // 5.1
		bits         = 16
		samplesPerCh = 2048
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 440.0)

	var encoded bytes.Buffer
	info := StreamInfo{SampleRate: sampleRate, Channels: channels, BitsPerSample: bits}
	enc, err := NewEncoder(&encoded, info)
	require.NoError(t, err)
	require.NoError(t, enc.Encode(input))
	require.NoError(t, enc.Close())

	dec, err := NewDecoder(bytes.NewReader(encoded.Bytes()))
	require.NoError(t, err)
	defer dec.Close()

	decoded := make([]int32, 0, len(input))
	buf := make([]int32, MaxBlockSize*channels)
	for {
		n, err := dec.Decode(buf)
		if n > 0 {
			decoded = append(decoded, buf[:n*channels]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	require.Equal(t, len(input), len(decoded))
	assert.Equal(t, input, decoded)
}

func TestStreamingMultipleEncodeCalls(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 1
		bits         = 24
		samplesPerCh = 8192
		chunkSpc     = 1024
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 880.0)

	var encoded bytes.Buffer
	info := StreamInfo{SampleRate: sampleRate, Channels: channels, BitsPerSample: bits}
	enc, err := NewEncoder(&encoded, info)
	require.NoError(t, err)

	for off := 0; off < len(input); off += chunkSpc * channels {
		end := off + chunkSpc*channels
		if end > len(input) {
			end = len(input)
		}
		require.NoError(t, enc.Encode(input[off:end]))
	}
	require.NoError(t, enc.Close())

	dec, err := NewDecoder(bytes.NewReader(encoded.Bytes()))
	require.NoError(t, err)
	defer dec.Close()

	decoded := make([]int32, 0, len(input))
	buf := make([]int32, MaxBlockSize*channels)
	for {
		n, err := dec.Decode(buf)
		if n > 0 {
			decoded = append(decoded, buf[:n*channels]...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, input, decoded)
}

func TestEncoderVerify(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 1024
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 1500.0)

	var encoded bytes.Buffer
	info := StreamInfo{SampleRate: sampleRate, Channels: channels, BitsPerSample: bits}
	enc, err := NewEncoder(&encoded, info, WithVerify(true))
	require.NoError(t, err)
	require.NoError(t, enc.Encode(input))
	require.NoError(t, enc.Close())
	assert.NotZero(t, encoded.Len())
}

func TestDecoderRejectsCorruptStream(t *testing.T) {
	dec, err := NewDecoder(bytes.NewReader([]byte("not a flac stream")))
	require.NoError(t, err)
	defer dec.Close()
	buf := make([]int32, MaxBlockSize*2)
	_, err = dec.Decode(buf)
	require.Error(t, err)
	assert.NotErrorIs(t, err, io.EOF)
}

func TestNewEncoderRejectsBadStreamInfo(t *testing.T) {
	cases := []struct {
		name string
		info StreamInfo
		want error
	}{
		{"zero rate", StreamInfo{Channels: 2, BitsPerSample: 16}, ErrBadSampleRate},
		{"too many chans", StreamInfo{SampleRate: 48000, Channels: 9, BitsPerSample: 16}, ErrBadChannels},
		{"low bit depth", StreamInfo{SampleRate: 48000, Channels: 2, BitsPerSample: 2}, ErrBadBitDepth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewEncoder(&bytes.Buffer{}, tc.info)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestNewDecoderRejectsNilReader(t *testing.T) {
	_, err := NewDecoder(nil)
	require.ErrorIs(t, err, ErrBadArg)
}

func TestEncoderTagsAndVendor(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 1024
	)
	input := generateTone(t, sampleRate, channels, bits, samplesPerCh, 1000.0)

	var buf bytes.Buffer
	info := StreamInfo{SampleRate: sampleRate, Channels: channels, BitsPerSample: bits}
	enc, err := NewEncoder(&buf, info,
		WithVendor("test-vendor"),
		WithTag("TITLE", "Hello"),
		WithTag("ARTIST", "A"),
		WithTag("ARTIST", "B"),
	)
	require.NoError(t, err)
	require.NoError(t, enc.Encode(input))
	require.NoError(t, enc.Close())

	dec, err := NewDecoder(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer dec.Close()

	// Drive decode so metadata callback fires.
	tmp := make([]int32, MaxBlockSize*channels)
	for {
		_, err := dec.Decode(tmp)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}

	if decoderParsesTags {
		// libFLAC overrides the vendor string with its own identification;
		// see WithVendor doc. Just assert it is non-empty.
		assert.NotEmpty(t, dec.Vendor())
		tags := dec.Tags()
		require.NotNil(t, tags)
		assert.Equal(t, []string{"Hello"}, tags["TITLE"])
		assert.Equal(t, []string{"A", "B"}, tags["ARTIST"])
	} else {
		// The native pure-Go decoder intentionally does not surface
		// VORBIS_COMMENT metadata (matching the Opus decoder): Vendor()
		// is empty and Tags() is nil.
		assert.Equal(t, "", dec.Vendor())
		assert.Nil(t, dec.Tags())
	}
}
