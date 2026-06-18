// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrence2e

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"
)

// genPCM builds a deterministic interleaved int16 test signal: a sum of two
// sinusoids per channel (a low tone the AAC core carries plus a high tone the
// SBR band reconstructs), with a per-channel phase offset for stereo so the L/R
// stereo-mode decision is exercised.
func genPCM(sampleRate, channels, frames, frameLen int) []int16 {
	n := frames * frameLen
	pcm := make([]int16, n*channels)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		for ch := 0; ch < channels; ch++ {
			phase := float64(ch) * 0.3
			lo := math.Sin(2*math.Pi*440.0*t + phase)
			hi := 0.4 * math.Sin(2*math.Pi*9000.0*t+phase)
			v := 0.5 * (lo + hi) * 30000.0
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			pcm[i*channels+ch] = int16(v)
		}
	}
	return pcm
}

// nativeEncodeHEAAC drives the pure-Go heaac.Encoder over the same per-frame PCM
// the fdk oracle consumes and returns every produced access unit plus the ASC.
func nativeEncodeHEAAC(t *testing.T, sampleRate, channels, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte) {
	t.Helper()
	enc, err := heaac.NewEncoder(sampleRate, channels, bitrate)
	require.NoError(t, err)
	require.Equal(t, frameLen, enc.FrameSamples())

	per := frameLen * channels
	frames := len(pcm) / per
	for f := 0; f < frames; f++ {
		au, err := enc.EncodeAccessUnit(pcm[f*per : (f+1)*per])
		require.NoError(t, err)
		if len(au) > 0 {
			aus = append(aus, au)
		}
	}
	return aus, enc.ASC()
}

func TestSbrEncodeE2EByteIdentical(t *testing.T) {
	const (
		frameLen = 2048 // HE-AAC v1 output frame (2*1024 core)
		frames   = 16
	)

	cases := []struct {
		name       string
		sampleRate int
		channels   int
		bitrate    int
	}{
		{"mono-44100-48k", 44100, 1, 48000},
		{"stereo-44100-64k", 44100, 2, 64000},
		{"mono-32000-32k", 32000, 1, 32000},
		{"stereo-48000-64k", 48000, 2, 64000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := genPCM(tc.sampleRate, tc.channels, frames, frameLen)

			cAUs, cASC, _, ok := cEncodeHEAAC(tc.sampleRate, tc.channels, tc.bitrate, frameLen, pcm)
			require.True(t, ok, "fdk HE-AAC encode failed")
			require.NotEmpty(t, cAUs)

			nAUs, nASC := nativeEncodeHEAAC(t, tc.sampleRate, tc.channels, tc.bitrate, frameLen, pcm)

			require.Equal(t, cASC, nASC, "AOT-5 AudioSpecificConfig mismatch")
			require.Equal(t, len(cAUs), len(nAUs), "access-unit count mismatch")
			for i := range cAUs {
				require.Equalf(t, cAUs[i], nAUs[i], "access unit %d byte mismatch", i)
			}
		})
	}
}
