package generators_test

import (
	"testing"
	"time"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/generators"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSineLength(t *testing.T) {
	tests := []struct {
		duration   time.Duration
		sampleRate int
		wantFrames int
	}{
		{time.Second, consts.SampleRate44100, consts.SampleRate44100},
		{time.Second, consts.SampleRate48000, consts.SampleRate48000},
		{500 * time.Millisecond, consts.SampleRate44100, consts.SampleRate22050},
		{10 * time.Millisecond, consts.SampleRate48000, 480},
		{100 * time.Microsecond, consts.SampleRate48000, 4},
	}
	for _, tt := range tests {
		audio := generators.Sine(consts.FreqNoteA4, tt.duration, tt.sampleRate)
		assert.Len(t, audio.Data, tt.wantFrames, "Sine(consts.FreqNoteA4, %v, %d)", tt.duration, tt.sampleRate)
		assert.Equal(t, tt.sampleRate, audio.SampleRate)
		assert.Equal(t, 1, audio.Channels)
	}
}

func TestSineAmplitude(t *testing.T) {
	out := generators.Sine(1, time.Second, 1000).Data
	for i, v := range out {
		require.InDelta(t, 0, v, 1.0, "sample %d out of [-1, 1]: %f", i, v)
	}
}

func TestSineFrequency(t *testing.T) {
	sampleRate := 1000
	freq := 10.0
	out := generators.Sine(freq, time.Second, sampleRate).Data

	crossings := 0
	for i := 1; i < len(out); i++ {
		if (out[i-1] < 0 && out[i] >= 0) || (out[i-1] >= 0 && out[i] < 0) {
			crossings++
		}
	}

	expected := int(freq) * 2
	assert.InDelta(t, expected, crossings, 1, "zero crossings for %.0f Hz", freq)
}

func TestSineStartsAtZero(t *testing.T) {
	out := generators.Sine(consts.FreqNoteA4, 10*time.Millisecond, consts.SampleRate48000).Data
	assert.Equal(t, 0.0, out[0])
}

func TestChordLengthAndFormat(t *testing.T) {
	audio := generators.Chord(
		[]float64{consts.FreqNoteA3, consts.FreqNoteCS4, consts.FreqNoteE4},
		10*time.Millisecond,
		consts.SampleRate48000,
	)
	assert.Equal(t, 480, len(audio.Data))
	assert.Equal(t, consts.SampleRate48000, audio.SampleRate)
	assert.Equal(t, 1, audio.Channels)
}

func TestChordNormalisedPeak(t *testing.T) {
	// Single sine → peak amplitude is 0.5 (normalised).
	audio := generators.Chord([]float64{consts.FreqNoteA4}, 50*time.Millisecond, consts.SampleRate48000)
	var peak float64
	for _, v := range audio.Data {
		if v > peak {
			peak = v
		} else if -v > peak {
			peak = -v
		}
	}
	assert.InDelta(t, 0.5, peak, 0.01, "single-freq chord should peak near 0.5")
}

func TestChordEmptyFreqs(t *testing.T) {
	audio := generators.Chord(nil, 10*time.Millisecond, consts.SampleRate48000)
	require.Equal(t, 480, len(audio.Data))
	for _, v := range audio.Data {
		assert.Equal(t, 0.0, v)
	}
}

func TestSineInto(t *testing.T) {
	buf := make([]float64, 100)
	n := generators.SineInto(buf, 440, consts.SampleRate48000)
	assert.Equal(t, 100, n)

	durNs := float64(time.Second) * 100.0 / 48000.0
	ref := generators.Sine(consts.FreqNoteA4, time.Duration(int64(durNs)), consts.SampleRate48000).Data

	cmp := min(len(buf), len(ref))
	for i := 0; i < cmp; i++ {
		if !assert.InDelta(t, ref[i], buf[i], 1e-15, "sample %d", i) {
			break
		}
	}
}
