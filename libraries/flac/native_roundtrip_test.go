package flac

import (
	"bytes"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeRoundTrip is a CGO_ENABLED=0-safe smoke test for the public
// native adapter wiring (native_encoder.go + native_stub.go). It drives a
// full NewNativeEncoder -> NewNativeDecoder round trip on a short known
// signal entirely through the pure-Go port — no other test hits these
// constructors directly — and asserts the decode is losslessly identical
// to the input plus a matching StreamInfo.
//
// Unlike TestRoundTripBitDepths (which goes through NewEncoder/NewDecoder
// and is therefore cgo-backed on the default build), this forces the
// native backend on both ends regardless of whether cgo is available.
func TestNativeRoundTrip(t *testing.T) {
	cases := []struct {
		name     string
		bits     int
		channels int
	}{
		{"16-bit mono", 16, 1},
		{"16-bit stereo", 16, 2},
		{"24-bit stereo", 24, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				sampleRate   = 48000
				samplesPerCh = 4096
				freqHz       = 1000.0
			)
			input := generateTone(t, sampleRate, tc.channels, tc.bits, samplesPerCh, freqHz)

			var encoded bytes.Buffer
			info := StreamInfo{
				SampleRate:    sampleRate,
				Channels:      tc.channels,
				BitsPerSample: tc.bits,
			}
			enc, err := NewNativeEncoder(&encoded, info,
				WithCompressionLevel(5), WithTotalSamples(uint64(samplesPerCh)))
			require.NoError(t, err)
			require.NoError(t, enc.Encode(input))
			require.NoError(t, enc.Close())
			require.NotZero(t, encoded.Len(), "native encoder produced no output")

			dec, err := NewNativeDecoder(bytes.NewReader(encoded.Bytes()))
			require.NoError(t, err)
			defer dec.Close()

			decoded := make([]int32, 0, len(input))
			buf := make([]int32, MaxBlockSize*tc.channels)
			for {
				n, err := dec.Decode(buf)
				if n > 0 {
					decoded = append(decoded, buf[:n*tc.channels]...)
				}
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(t, err)
			}

			gotInfo := dec.StreamInfo()
			assert.Equal(t, sampleRate, gotInfo.SampleRate, "StreamInfo sample rate")
			assert.Equal(t, tc.channels, gotInfo.Channels, "StreamInfo channels")
			assert.Equal(t, tc.bits, gotInfo.BitsPerSample, "StreamInfo bits per sample")
			assert.Equal(t, uint64(samplesPerCh), gotInfo.TotalSamples, "StreamInfo total samples")

			require.Equal(t, len(input), len(decoded), "sample count mismatch")
			assert.Equal(t, input, decoded, "native lossless round-trip must be bit-exact")
		})
	}
}
