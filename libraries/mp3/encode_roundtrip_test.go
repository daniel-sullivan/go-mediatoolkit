// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build mp3lame

package mp3

import (
	"bytes"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This is the pure-Go encoder wiring gate. It encodes a sine through the
// pure-Go LAME port (NewNativeEncoder runs the real Phase-3 pipeline:
// lame_encode_buffer -> mdct -> psymodel -> quantize -> format_bitstream) and
// decodes the resulting MP3 back with the pure-Go minimp3 port
// (NewNativeDecoder), then asserts the recovered signal matches the original
// within MP3 tolerance. It needs no cgo — both the encoder and decoder are
// pure Go — so it runs under CGO_ENABLED=0 with -tags mp3lame, which is exactly
// the LGPL fence the encoder lives behind. (The cgo decode_roundtrip_test.go
// gates the decoder against the C minimp3 oracle; this one gates the encoder
// end-to-end against the decoder.)
//
// A lossy MP3 round-trip is not bit-exact: it introduces a codec delay (the
// analysis/synthesis filterbank latency, ~1100 samples at 44.1 kHz) and
// quantization noise. The test recovers the delay by cross-correlation, then
// measures the segmental SNR over the steady-state interior; a clean tone
// round-trips at far better than the 20 dB floor asserted here.

// encodePureGoMP3 encodes an interleaved-int16 sine through the pure-Go MP3
// encoder and returns the MP3 byte stream.
func encodePureGoMP3(t *testing.T, sampleRate, channels, nSamples int, freq float64, opts ...EncoderOption) []byte {
	t.Helper()
	pcm := make([]int16, nSamples*channels)
	for i := 0; i < nSamples; i++ {
		v := int16(math.Round(0.5 * 32767 * math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate))))
		for c := 0; c < channels; c++ {
			pcm[i*channels+c] = v
		}
	}

	var buf bytes.Buffer
	enc, err := NewNativeEncoder(&buf, StreamInfo{SampleRate: sampleRate, Channels: channels}, opts...)
	require.NoError(t, err)
	require.NoError(t, enc.EncodeFrame(pcm))
	require.NoError(t, enc.Close())
	out := buf.Bytes()
	require.NotEmpty(t, out, "pure-Go encoder produced no output")
	// MPEG-1 Layer III frame sync: 0xFF 0xFB / 0xFA (11-bit sync + version 1 +
	// layer III). Confirms the bytes are a real MP3 frame, not garbage.
	require.GreaterOrEqual(t, len(out), 4)
	require.Equal(t, byte(0xFF), out[0], "missing MP3 frame sync")
	require.Equal(t, byte(0xF0), out[1]&0xF0, "missing MP3 frame sync")
	return out
}

// decodePureGoInt16 decodes the whole MP3 stream with the pure-Go decoder into
// one interleaved-int16 slice.
func decodePureGoInt16(t *testing.T, stream []byte) (pcm []int16, channels int) {
	t.Helper()
	dec, err := NewNativeDecoder(bytes.NewReader(stream))
	require.NoError(t, err)
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
	return pcm, dec.Channels()
}

func TestPureGoEncodeRoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		sampleRate int
		channels   int
		freq       float64
		nSamples   int
		opts       []EncoderOption
	}{
		{name: "mono 44100 440Hz 192k", sampleRate: 44100, channels: 1, freq: 440, nSamples: 44100, opts: []EncoderOption{WithBitRate(192000)}},
		{name: "stereo 44100 1kHz 192k", sampleRate: 44100, channels: 2, freq: 1000, nSamples: 32768, opts: []EncoderOption{WithBitRate(192000)}},
		{name: "stereo 48000 800Hz 256k", sampleRate: 48000, channels: 2, freq: 800, nSamples: 32768, opts: []EncoderOption{WithBitRate(256000)}},
		// VBR (vbr_mtrh, -V2): WithVBR + WithQuality(2). Exercises the ported
		// VBR_new iteration loop and the finalized Xing/LAME tag splice on Close.
		{name: "mono 44100 440Hz V2", sampleRate: 44100, channels: 1, freq: 440, nSamples: 44100, opts: []EncoderOption{WithVBR(true), WithQuality(2)}},
		{name: "stereo 44100 1kHz V2", sampleRate: 44100, channels: 2, freq: 1000, nSamples: 32768, opts: []EncoderOption{WithVBR(true), WithQuality(2)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := encodePureGoMP3(t, tc.sampleRate, tc.channels, tc.nSamples, tc.freq, tc.opts...)
			decoded, ch := decodePureGoInt16(t, stream)
			require.Equal(t, tc.channels, ch, "decoded channel count")
			require.NotEmpty(t, decoded, "decoder produced no samples")

			// Rebuild the original first-channel reference.
			ref := make([]int16, tc.nSamples)
			for i := range ref {
				ref[i] = int16(math.Round(0.5 * 32767 * math.Sin(2*math.Pi*tc.freq*float64(i)/float64(tc.sampleRate))))
			}
			// Extract the decoder's first channel.
			got := make([]int16, len(decoded)/ch)
			for i := range got {
				got[i] = decoded[i*ch]
			}

			// Recover the codec delay by cross-correlation over a coarse grid.
			bestShift, bestCorr := 0, math.Inf(-1)
			for s := 0; s < 2000 && s < len(got); s++ {
				var corr float64
				for i := 0; i+s < len(got) && i < len(ref); i += 7 {
					corr += float64(ref[i]) * float64(got[i+s])
				}
				if corr > bestCorr {
					bestCorr = corr
					bestShift = s
				}
			}

			// Segmental SNR over the steady-state interior (skip the codec's
			// startup transient and the tail beyond the original).
			var noise, signal float64
			var cnt int
			for i := 5000; i+bestShift < len(got) && i < len(ref)-2000; i++ {
				d := float64(ref[i]) - float64(got[i+bestShift])
				noise += d * d
				signal += float64(ref[i]) * float64(ref[i])
				cnt++
			}
			require.Positive(t, cnt, "no overlap to compare")
			snr := 20 * math.Log10(math.Sqrt(signal)/math.Sqrt(noise))
			t.Logf("%s: mp3Bytes=%d decoded=%d shift=%d SNR=%.1fdB", tc.name, len(stream), len(got), bestShift, snr)
			assert.Greater(t, snr, 20.0, "round-trip SNR below MP3 tolerance (%.1f dB)", snr)
		})
	}
}
