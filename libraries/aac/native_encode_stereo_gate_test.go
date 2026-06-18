// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeEncodeStereoGate(t *testing.T) {
	const sampleRate, channels, freq = 44100, 2, 440.0
	enc, err := NewNativeEncoder(sampleRate, channels, WithObjectType(AOTAACLC), WithBitrate(128000))
	require.NoError(t, err)
	asc := enc.Config()
	dec, err := NewNativeDecoder(asc)
	require.NoError(t, err)
	frame := asc.FrameSamples
	pcm := make([]float64, frame*channels)
	pcmOut := make([]float64, FrameSamplesLong*channels)
	phase := 0.0
	step := 2 * math.Pi * freq / sampleRate
	for f := 0; f < 6; f++ {
		for i := 0; i < frame; i++ {
			s := 0.5 * math.Sin(phase)
			phase += step
			pcm[i*channels] = s
			pcm[i*channels+1] = s * 0.8
		}
		pkt, err := enc.Encode(pcm)
		require.NoError(t, err, "frame %d", f)
		require.NotEmpty(t, pkt)
		_, derr := dec.Decode(pkt, pcmOut)
		require.NoError(t, derr, "decode frame %d", f)
	}
}
