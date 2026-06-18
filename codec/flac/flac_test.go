package flac

import (
	"bytes"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/mutations"
)

// generateSineFloat64 creates interleaved float64 samples in [-0.95,
// 0.95] across freq Hz; per-channel phase is offset so multichannel is
// not a duplicate.
func generateSineFloat64(t *testing.T, sampleRate, channels, samplesPerChannel int, freq float64) []float64 {
	t.Helper()
	out := make([]float64, samplesPerChannel*channels)
	for i := 0; i < samplesPerChannel; i++ {
		for ch := 0; ch < channels; ch++ {
			phase := 2*math.Pi*freq*float64(i)/float64(sampleRate) + float64(ch)*0.1
			out[i*channels+ch] = 0.95 * math.Sin(phase)
		}
	}
	return out
}

func encodeRoundTrip(t *testing.T, sampleRate, channels, bits int, input []float64) []float64 {
	t.Helper()
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, sampleRate, channels,
		WithBitsPerSample(bits),
		WithTotalSamples(uint64(len(input)/channels)),
	)
	require.NoError(t, err)
	_, err = enc.Write(mutations.Audio{Data: input, SampleRate: sampleRate, Channels: channels})
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	dec, err := NewDecoder(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	out := make([]float64, 0, len(input))
	tmp := make([]float64, 4096)
	for {
		audio, err := dec.Read(tmp)
		if len(audio.Data) > 0 {
			out = append(out, audio.Data...)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	return out
}

func TestRoundTrip16(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 4096
	)
	input := generateSineFloat64(t, sampleRate, channels, samplesPerCh, 1000.0)
	out := encodeRoundTrip(t, sampleRate, channels, bits, input)

	require.Equal(t, len(input), len(out))
	// Quantisation tolerance for 16-bit: ~1 LSB / max ≈ 1 / 32767 ≈ 3e-5.
	tol := 1.5 / float64(int32(1)<<15-1)
	for i := range input {
		assert.InDelta(t, input[i], out[i], tol, "sample %d", i)
	}
}

func TestRoundTrip24(t *testing.T) {
	const (
		sampleRate   = 44100
		channels     = 2
		bits         = 24
		samplesPerCh = 2048
	)
	input := generateSineFloat64(t, sampleRate, channels, samplesPerCh, 880.0)
	out := encodeRoundTrip(t, sampleRate, channels, bits, input)

	require.Equal(t, len(input), len(out))
	tol := 1.5 / float64(int32(1)<<23-1)
	for i := range input {
		assert.InDelta(t, input[i], out[i], tol, "sample %d", i)
	}
}

func TestMultichannel(t *testing.T) {
	const (
		sampleRate   = 48000
		channels     = 6
		bits         = 16
		samplesPerCh = 1024
	)
	input := generateSineFloat64(t, sampleRate, channels, samplesPerCh, 220.0)
	out := encodeRoundTrip(t, sampleRate, channels, bits, input)
	require.Equal(t, len(input), len(out))
}

func TestEncoderRejectsFormatMismatch(t *testing.T) {
	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, 48000, 2, WithBitsPerSample(16))
	require.NoError(t, err)
	defer enc.Close()
	_, err = enc.Write(mutations.Audio{Data: []float64{0, 0}, SampleRate: 44100, Channels: 2})
	assert.ErrorIs(t, err, ErrFormatMismatch)
	_, err = enc.Write(mutations.Audio{Data: []float64{0}, SampleRate: 48000, Channels: 1})
	assert.ErrorIs(t, err, ErrFormatMismatch)
}

func TestEncoderSaturatesAtFullScale(t *testing.T) {
	const (
		sampleRate = 48000
		channels   = 1
		bits       = 16
	)
	input := []float64{1.5, -1.5, 0.99999999, 0.0}
	out := encodeRoundTrip(t, sampleRate, channels, bits, input)
	require.Equal(t, len(input), len(out))
	// 1.5 should saturate to maxVal/maxAbs = (32767)/(32767) = 1.0
	assert.InDelta(t, 1.0, out[0], 1e-3)
	// -1.5 should saturate to minVal/maxAbs = -32768/32767 ≈ -1.00003
	assert.InDelta(t, -1.0, out[1], 1.1/32767.0)
}

func TestNewDecoderRejectsNilReader(t *testing.T) {
	_, err := NewDecoder(nil)
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestNewEncoderRejectsNilWriter(t *testing.T) {
	_, err := NewEncoder(nil, 48000, 2)
	assert.ErrorIs(t, err, ErrBadArg)
}

func TestSmallReadBuffer(t *testing.T) {
	// Caller's read buffer is much smaller than one block — Read must
	// drain the leftover before decoding the next block.
	const (
		sampleRate   = 48000
		channels     = 2
		bits         = 16
		samplesPerCh = 4096
	)
	input := generateSineFloat64(t, sampleRate, channels, samplesPerCh, 1000.0)

	var buf bytes.Buffer
	enc, err := NewEncoder(&buf, sampleRate, channels, WithBitsPerSample(bits))
	require.NoError(t, err)
	_, err = enc.Write(mutations.Audio{Data: input, SampleRate: sampleRate, Channels: channels})
	require.NoError(t, err)
	require.NoError(t, enc.Close())

	dec, err := NewDecoder(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)

	out := make([]float64, 0, len(input))
	// Buffer holds ~32 stereo frames at a time — far smaller than a
	// FLAC block (default 4096 samples-per-channel).
	tmp := make([]float64, 64)
	for {
		audio, err := dec.Read(tmp)
		out = append(out, audio.Data...)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
	}
	require.Equal(t, len(input), len(out))
	tol := 1.5 / float64(int32(1)<<15-1)
	for i := range input {
		assert.InDelta(t, input[i], out[i], tol, "sample %d", i)
	}
}
