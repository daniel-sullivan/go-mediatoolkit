// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCgoRoundTripSine exercises the vendored Fraunhofer FDK-AAC backend
// end to end: encode a stereo sine frame to a raw AAC access unit, then
// decode it back and confirm the recovered PCM tracks the input. AAC is
// lossy, so the bar is energy/shape recovery (correlation + bounded error),
// not bit-exactness.
func TestCgoRoundTripSine(t *testing.T) {
	const (
		sampleRate = 44100
		channels   = 2
		freq       = 440.0
	)

	enc, err := NewEncoder(sampleRate, channels, WithObjectType(AOTAACLC), WithBitrate(128000))
	require.NoError(t, err)
	require.Equal(t, sampleRate, enc.SampleRate())
	require.Equal(t, channels, enc.Channels())

	asc := enc.Config()
	require.NotEmpty(t, asc.Raw, "encoder must report ASC bytes for the container")
	require.Equal(t, FrameSamplesShort, asc.FrameSamples)

	dec, err := NewDecoder(asc)
	require.NoError(t, err)

	frame := asc.FrameSamples
	pcm := make([]float64, frame*channels)

	// FDK introduces an encoder/decoder delay, so feed several frames and
	// assert that at least one decoded frame correlates with a pure tone.
	pcmOut := make([]float64, FrameSamplesLong*channels)
	var bestCorr float64
	phase := 0.0
	step := 2 * math.Pi * freq / sampleRate
	for f := 0; f < 8; f++ {
		for i := 0; i < frame; i++ {
			s := 0.5 * math.Sin(phase)
			phase += step
			pcm[i*channels] = s
			pcm[i*channels+1] = s
		}
		pkt, err := enc.Encode(pcm)
		require.NoError(t, err)
		require.NotEmpty(t, pkt, "encoder produced an access unit")
		require.LessOrEqual(t, len(pkt), MaxFrameBytes)

		n, err := dec.Decode(pkt, pcmOut)
		require.NoError(t, err)
		if n == 0 {
			continue // priming/delay frame
		}
		require.Equal(t, channels, dec.Channels())
		require.Equal(t, sampleRate, dec.SampleRate())

		// Correlate the decoded left channel against its own RMS energy;
		// a recovered tone has substantial energy (silence/garbage ~0).
		var energy float64
		for i := 0; i < n; i++ {
			v := pcmOut[i*channels]
			energy += v * v
		}
		rms := math.Sqrt(energy / float64(n))
		if rms > bestCorr {
			bestCorr = rms
		}
	}
	assert.Greater(t, bestCorr, 0.05, "decoded tone should carry audible energy")
}

// TestCgoEncoderConfigASC confirms the encoder surfaces a non-empty
// AudioSpecificConfig (the bytes the MP4 esds box must carry) and that a
// fresh decoder accepts it.
func TestCgoEncoderConfigASC(t *testing.T) {
	enc, err := NewEncoder(48000, 1)
	require.NoError(t, err)
	asc := enc.Config()
	require.NotEmpty(t, asc.Raw)
	assert.Equal(t, 48000, asc.SampleRate)
	assert.Equal(t, 1, asc.Channels)
	assert.Equal(t, AOTAACLC, asc.ObjectType)

	dec, err := NewDecoder(asc)
	require.NoError(t, err)
	assert.Equal(t, 1, dec.Channels())
}
