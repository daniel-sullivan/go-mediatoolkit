// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNativeDecodePublicSurface drives the full public surface end-to-end: it
// encodes a real stereo signal with the vendored FDK encoder, then decodes the
// access-unit sequence through the pure-Go Decoder (NewNativeDecoder, the 1:1
// fixed-point port wired in native_stub.go) and checks the produced float64 PCM
// is finite, in range, and non-silent.
//
// The bit-for-bit exactness of the native decode against the FDK fixed-point
// reference is proven (no tolerance, no ULP) by the limiter-disabled
// internal/parity_tests/decode-e2e slice; this test only confirms the public
// [NewNativeDecoder] wiring and float64 normalisation path are live.
func TestNativeDecodePublicSurface(t *testing.T) {
	const (
		sampleRate = 44100
		channels   = 2
		frames     = 10
	)

	enc, err := NewEncoder(sampleRate, channels, WithObjectType(AOTAACLC), WithBitrate(128000))
	require.NoError(t, err)
	asc := enc.Config()
	frame := asc.FrameSamples

	var aus [][]byte
	for f := 0; f < frames; f++ {
		pcm := make([]float64, frame*channels)
		for n := 0; n < frame; n++ {
			t0 := float64(f*frame+n) / float64(sampleRate)
			s := 0.3 * math.Sin(2*math.Pi*440*t0)
			pcm[n*channels] = s
			pcm[n*channels+1] = 0.8 * s
		}
		au, err := enc.Encode(pcm)
		require.NoError(t, err)
		if len(au) > 0 {
			aus = append(aus, au)
		}
	}
	require.NotEmpty(t, aus)

	ndec, err := NewNativeDecoder(asc)
	require.NoError(t, err)
	require.Equal(t, sampleRate, ndec.SampleRate())
	require.Equal(t, channels, ndec.Channels())

	var energy float64
	for i, au := range aus {
		buf := make([]float64, frame*channels)
		n, err := ndec.Decode(au, buf)
		require.NoErrorf(t, err, "native decode AU %d", i)
		require.Equal(t, frame, n)
		for _, v := range buf {
			require.False(t, math.IsNaN(v) || math.IsInf(v, 0))
			require.LessOrEqual(t, math.Abs(v), 1.0)
			energy += v * v
		}
	}
	require.Greater(t, energy, 0.0, "native decode produced only silence")
}
