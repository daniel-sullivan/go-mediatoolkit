package opus

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncoderCreate(t *testing.T) {
	enc, err := NewEncoder(48000, 1)
	require.NoError(t, err)
	assert.Equal(t, 48000, enc.SampleRate())
	assert.Equal(t, 1, enc.Channels())
}

func TestEncoderEncode(t *testing.T) {
	enc, err := NewEncoder(48000, 1)
	require.NoError(t, err)

	// Generate a 20ms frame of 440Hz sine.
	frameSamples := 960 // 20ms at 48kHz
	pcm := make([]float64, frameSamples)
	for i := range pcm {
		pcm[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/48000)
	}

	pkt, err := enc.Encode(pcm, 1275)
	require.NoError(t, err)
	assert.Greater(t, len(pkt), 1, "packet should have at least TOC + data")

	// Verify TOC byte is CELT mode.
	assert.True(t, pkt[0]&0x80 != 0, "should be CELT mode")
}

func TestEncoderDecodeRoundTrip(t *testing.T) {
	enc, err := NewEncoder(48000, 1)
	require.NoError(t, err)
	dec, err := NewDecoder(48000, 1)
	require.NoError(t, err)

	// Generate a 20ms frame.
	frameSamples := 960
	pcmIn := make([]float64, frameSamples)
	for i := range pcmIn {
		pcmIn[i] = 0.5 * math.Sin(2*math.Pi*440*float64(i)/48000)
	}

	// Encode.
	pkt, err := enc.Encode(pcmIn, 1275)
	require.NoError(t, err)

	// Decode.
	pcmOut := make([]float64, MaxFrameSize(48000))
	n, err := dec.Decode(pkt, pcmOut)
	require.NoError(t, err)
	assert.Equal(t, frameSamples, n)

	// The output won't match the input exactly (lossy codec),
	// but it should produce valid non-zero output.
	hasNonZero := false
	for i := 0; i < n; i++ {
		if pcmOut[i] != 0 {
			hasNonZero = true
			break
		}
	}
	assert.True(t, hasNonZero, "decoded output should have non-zero samples")
}
