//go:build cgo && mp3lame

package mp3

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// This end-to-end test is the decode-side wiring gate: it encodes a sine via
// the cgo libmp3lame path, then decodes the resulting MP3 with BOTH the cgo
// minimp3 decoder (NewDecoder under cgo) and the pure-Go port
// (NewNativeDecoder). The two decoders run the same minimp3 algorithm, so they
// must produce the same int16 PCM. The default build may fuse FP ops in the
// IMDCT / synthesis filterbank, so the comparison uses a small per-sample
// tolerance and a near-zero RMS bound; under -tags=mp3_strict the port is
// FMA-free and bit-exact, which this test also asserts. It is gated on
// cgo+mp3lame because it needs the cgo encoder to fabricate the input MP3.
//
// Build tag note: this file is compiled only with cgo (it relies on the cgo
// NewDecoder path being the C minimp3 oracle) and mp3lame (for the encoder).

func encodeSineMP3(t *testing.T, sampleRate, channels int, nSamples int, freq float64, opts ...EncoderOption) []byte {
	t.Helper()
	// Build interleaved int16 PCM.
	pcm := make([]int16, nSamples*channels)
	for i := 0; i < nSamples; i++ {
		v := int16(math.Round(0.5 * 32767 * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate))))
		for c := 0; c < channels; c++ {
			pcm[i*channels+c] = v
		}
	}

	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, StreamInfo{SampleRate: sampleRate, Channels: channels}, opts...)
	require.NoError(t, err)
	require.NoError(t, enc.EncodeFrame(pcm))
	require.NoError(t, enc.Close())
	require.NotEmpty(t, buf.Bytes(), "encoder produced no output")
	return buf.Bytes()
}

// decodeAllInt16 drains every audio frame from dec into one interleaved int16
// slice, returning it plus the decoded sample rate and channel count.
func decodeAllInt16(t *testing.T, dec Decoder) (pcm []int16, sampleRate, channels int) {
	t.Helper()
	buf := make([]int16, MaxSamplesPerFrame*MaxChannels)
	for {
		n, err := dec.DecodeFrame(buf)
		if n > 0 {
			pcm = append(pcm, buf[:n*dec.Channels()]...)
		}
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	require.NoError(t, dec.Close())
	return pcm, dec.SampleRate(), dec.Channels()
}

func TestNativeDecodeMatchesCgo(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		channels   int
		freq       float64
		nSamples   int
		opts       []EncoderOption
	}{
		{name: "stereo 44100 cbr", sampleRate: 44100, channels: 2, freq: 1000, nSamples: 32768, opts: []EncoderOption{WithBitRate(192000)}},
		{name: "stereo 48000 cbr", sampleRate: 48000, channels: 2, freq: 800, nSamples: 32768, opts: []EncoderOption{WithBitRate(256000)}},
		{name: "mono 44100 cbr", sampleRate: 44100, channels: 1, freq: 440, nSamples: 24000, opts: []EncoderOption{WithBitRate(128000)}},
		{name: "stereo 32000 cbr", sampleRate: 32000, channels: 2, freq: 660, nSamples: 24000, opts: []EncoderOption{WithBitRate(128000)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := encodeSineMP3(t, tc.sampleRate, tc.channels, tc.nSamples, tc.freq, tc.opts...)

			cgoDec, err := NewDecoder(bytes.NewReader(stream))
			require.NoError(t, err)
			cgoPCM, cgoSR, cgoCH := decodeAllInt16(t, cgoDec)

			natDec, err := NewNativeDecoder(bytes.NewReader(stream))
			require.NoError(t, err)
			natPCM, natSR, natCH := decodeAllInt16(t, natDec)

			require.Equal(t, cgoSR, natSR, "sample rate")
			require.Equal(t, cgoCH, natCH, "channels")
			require.Equal(t, len(cgoPCM), len(natPCM), "decoded sample count")
			require.NotEmpty(t, natPCM, "native decoder produced no samples")

			// Compare. Under mp3_strict the port is bit-exact with minimp3; in
			// the default build FP fusion in the IMDCT/synthesis may shift a
			// handful of samples by one quantization step.
			var maxDiff int
			var sumSq float64
			for i := range cgoPCM {
				d := int(cgoPCM[i]) - int(natPCM[i])
				if d < 0 {
					d = -d
				}
				if d > maxDiff {
					maxDiff = d
				}
				sumSq += float64(d) * float64(d)
			}
			rms := math.Sqrt(sumSq / float64(len(cgoPCM)))

			if nativemp3.StrictMode {
				assert.Zero(t, maxDiff, "strict build must be bit-exact with cgo minimp3 (maxDiff=%d)", maxDiff)
			} else {
				assert.LessOrEqual(t, maxDiff, 2, "default build per-sample diff should be tiny")
				assert.Less(t, rms, 0.05, "default build RMS diff should be near zero")
			}
			t.Logf("%s: samples=%d maxDiff=%d rms=%.4g", tc.name, len(natPCM), maxDiff, rms)
		})
	}
}
