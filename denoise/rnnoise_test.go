package denoise

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRNNoiseDefaults(t *testing.T) {
	r, err := NewRNNoise(RNNoiseConfig{})
	require.NoError(t, err)
	assert.Equal(t, 48000, r.SampleRate())
	assert.Equal(t, 1, r.Channels())
	assert.Equal(t, 20*time.Millisecond, r.Latency()) // 2 frames @ 48 kHz
	assert.Equal(t, 0.0, r.Probability())
}

func TestNewRNNoiseValidation(t *testing.T) {
	// Non-48 kHz is accepted (resampled internally).
	r, err := NewRNNoise(RNNoiseConfig{SampleRate: 16000})
	require.NoError(t, err)
	assert.Equal(t, 16000, r.SampleRate())
	// Multichannel and out-of-range rates are rejected.
	_, err = NewRNNoise(RNNoiseConfig{Channels: 2})
	assert.Error(t, err)
	_, err = NewRNNoise(RNNoiseConfig{SampleRate: 500})
	assert.Error(t, err)
}

// TestProcessDenoises feeds white noise and checks the engine runs,
// produces finite output of the right length, and attenuates the noise
// (RNNoise suppresses non-speech), across the native rate and a resampled
// rate.
func TestProcessDenoises(t *testing.T) {
	for _, rate := range []int{48000, 16000, 44100} {
		r, err := NewRNNoise(RNNoiseConfig{SampleRate: rate})
		require.NoError(t, err)

		// ~0.5 s of white noise.
		n := rate / 2
		buf := make([]float64, n)
		seed := uint64(1)
		var inRMS float64
		for i := range buf {
			seed = seed*6364136223846793005 + 1
			v := (float64(seed>>40)/float64(1<<24))*2 - 1
			v *= 0.3
			buf[i] = v
			inRMS += v * v
		}
		inRMS = math.Sqrt(inRMS / float64(n))

		r.Process(buf)

		var outRMS float64
		for _, v := range buf {
			require.False(t, math.IsNaN(v) || math.IsInf(v, 0), "non-finite output at rate %d", rate)
			outRMS += v * v
		}
		outRMS = math.Sqrt(outRMS / float64(n))
		// Noise should be attenuated (allow generous margin; also covers
		// the start-up-silence latency region lowering RMS).
		assert.Lessf(t, outRMS, inRMS, "rate %d: expected noise attenuation (in RMS %.4f, out RMS %.4f)", rate, inRMS, outRMS)

		p := r.Probability()
		assert.GreaterOrEqual(t, p, 0.0)
		assert.LessOrEqual(t, p, 1.0)
	}
}

func TestEngineInterfaceSatisfied(t *testing.T) {
	var _ Engine = must(NewRNNoise(RNNoiseConfig{}))
}

func must(r *RNNoise, err error) *RNNoise {
	if err != nil {
		panic(err)
	}
	return r
}
