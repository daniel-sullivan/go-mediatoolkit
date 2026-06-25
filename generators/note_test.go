package generators_test

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func TestNoteLength(t *testing.T) {
	audio := generators.Note(consts.FreqNoteA4, 250*time.Millisecond, consts.SampleRate48000)
	assert.Equal(t, 12000, len(audio.Data))
	assert.Equal(t, consts.SampleRate48000, audio.SampleRate)
	assert.Equal(t, 1, audio.Channels)
}

func TestNoteEndpointsAreSilent(t *testing.T) {
	// ADSR should ramp from 0 and to 0 — first and last samples must
	// be exactly zero so concatenated notes don't click at the seam.
	audio := generators.Note(consts.FreqNoteA4, 200*time.Millisecond, consts.SampleRate48000)
	require.Greater(t, len(audio.Data), 0)
	assert.Equal(t, 0.0, audio.Data[0], "attack must start from silence")
	assert.Equal(t, 0.0, audio.Data[len(audio.Data)-1], "release must end at silence")
}

func TestNoteAmplitudeBelowPeak(t *testing.T) {
	// Peak must stay under the documented 0.4 ceiling so multi-note
	// summing leaves headroom for the mixer's saturator.
	audio := generators.Note(consts.FreqNoteA4, 500*time.Millisecond, consts.SampleRate48000)
	var peak float64
	for _, v := range audio.Data {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	assert.LessOrEqual(t, peak, 0.4001, "Note peak should not exceed notePeak")
	assert.Greater(t, peak, 0.3, "envelope should reach near peak in the sustain region")
}

func TestNoteRestProducesSilence(t *testing.T) {
	audio := generators.Note(0, 100*time.Millisecond, consts.SampleRate48000)
	require.Equal(t, 4800, len(audio.Data))
	for _, v := range audio.Data {
		assert.Equal(t, 0.0, v)
	}
}

func TestNoteShortDurationDoesNotPanic(t *testing.T) {
	// 1ms note at 48k = 48 samples — envelope must collapse cleanly.
	audio := generators.Note(consts.FreqNoteA4, time.Millisecond, consts.SampleRate48000)
	assert.Equal(t, 48, len(audio.Data))
	assert.Equal(t, 0.0, audio.Data[0])
	assert.Equal(t, 0.0, audio.Data[len(audio.Data)-1])
}
