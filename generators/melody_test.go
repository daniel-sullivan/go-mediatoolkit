package generators_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
	"github.com/daniel-sullivan/go-mediatoolkit/generators"
)

func TestMelodyTotalLength(t *testing.T) {
	notes := []generators.MelodyNote{
		{Freq: consts.FreqNoteA4, Duration: 100 * time.Millisecond},
		{Freq: consts.FreqNoteB4, Duration: 200 * time.Millisecond},
		{Freq: 0, Duration: 50 * time.Millisecond}, // rest
	}
	audio := generators.Melody(notes, consts.SampleRate48000)
	// 100ms + 200ms + 50ms = 350ms at 48k = 16800 frames
	assert.Equal(t, 16800, len(audio.Data))
	assert.Equal(t, consts.SampleRate48000, audio.SampleRate)
	assert.Equal(t, 1, audio.Channels)
}

func TestMelodyRestIsSilent(t *testing.T) {
	notes := []generators.MelodyNote{
		{Freq: consts.FreqNoteA4, Duration: 100 * time.Millisecond},
		{Freq: 0, Duration: 100 * time.Millisecond},
	}
	audio := generators.Melody(notes, consts.SampleRate48000)
	// Second half (4800 frames in) must be entirely silent.
	for i := 4800; i < len(audio.Data); i++ {
		assert.Equal(t, 0.0, audio.Data[i], "rest sample %d should be 0", i)
	}
}

func TestMelodyEmptyReturnsEmpty(t *testing.T) {
	audio := generators.Melody(nil, consts.SampleRate48000)
	assert.Equal(t, 0, len(audio.Data))
	assert.Equal(t, consts.SampleRate48000, audio.SampleRate)
	assert.Equal(t, 1, audio.Channels)
}

func TestMaryHadALittleLambDuration(t *testing.T) {
	// 32 quarters at 120 BPM = 32 * 0.5s = 16s.
	audio := generators.MaryHadALittleLamb(consts.SampleRate48000)
	require.Equal(t, 1, audio.Channels)
	expected := 16 * consts.SampleRate48000
	assert.Equal(t, expected, len(audio.Data))
}
