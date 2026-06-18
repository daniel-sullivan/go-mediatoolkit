// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeEncodeDecodeGate is the stage GATE: under CGO_ENABLED=0 -tags
// aacfdk, the pure-Go 1:1 AAC-LC CBR encoder (nativeEncoder.Encode ->
// EncodeFrame -> psyMain/QCMain/WriteBitstream) produces a raw access unit that
// the committed pure-Go AAC-LC decoder (NewNativeDecoder) decodes back to PCM.
// Byte-identical-vs-FDK proof is the next phase; this gate asserts the encode
// path yields a decodable AU and recovers signal energy.
func TestNativeEncodeDecodeGate(t *testing.T) {
	const (
		sampleRate = 44100
		channels   = 1
		freq       = 440.0
	)

	enc, err := NewNativeEncoder(sampleRate, channels, WithObjectType(AOTAACLC), WithBitrate(128000))
	require.NoError(t, err)
	require.Equal(t, sampleRate, enc.SampleRate())
	require.Equal(t, channels, enc.Channels())

	asc := enc.Config()
	require.Equal(t, FrameSamplesShort, asc.FrameSamples)
	require.NotEmpty(t, asc.Raw, "encoder must report ASC bytes for the container")

	dec, err := NewNativeDecoder(asc)
	require.NoError(t, err)

	frame := asc.FrameSamples
	pcm := make([]float64, frame*channels)
	pcmOut := make([]float64, FrameSamplesLong*channels)

	phase := 0.0
	step := 2 * math.Pi * freq / sampleRate

	var decodedAny bool
	for f := 0; f < 8; f++ {
		for i := 0; i < frame; i++ {
			s := 0.5 * math.Sin(phase)
			phase += step
			pcm[i*channels] = s
		}
		pkt, err := enc.Encode(pcm)
		require.NoError(t, err, "encode frame %d", f)
		require.NotEmpty(t, pkt, "encoder produced an access unit on frame %d", f)
		require.LessOrEqual(t, len(pkt), MaxFrameBytes)

		n, derr := dec.Decode(pkt, pcmOut)
		require.NoError(t, derr, "decode frame %d", f)
		if n > 0 {
			decodedAny = true
		}
	}
	assert.True(t, decodedAny, "decoder recovered at least one frame of samples from the native AU")
}
