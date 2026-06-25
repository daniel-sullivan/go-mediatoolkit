package mutations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestFramesToDurationExact(t *testing.T) {
	assert.Equal(t, time.Second, mutations.FramesToDuration(consts.SampleRate48000, consts.SampleRate48000))
	assert.Equal(t, time.Duration(0), mutations.FramesToDuration(0, consts.SampleRate48000))
}

func TestDurationToFramesRoundsToNearest(t *testing.T) {
	assert.Equal(t, int64(consts.SampleRate48000), mutations.DurationToFrames(time.Second, consts.SampleRate48000))
	assert.Equal(t, int64(0), mutations.DurationToFrames(0, consts.SampleRate48000))
	assert.Equal(t, int64(9600), mutations.DurationToFrames(100*time.Millisecond, consts.SampleRate96000))
}

func TestFramesDurationRoundTripAt48kHz(t *testing.T) {
	// The raw multiplier at 48 kHz is 20833.333... ns per frame;
	// truncation would collapse framesToDuration(2) back to 1 frame.
	// With rounding, the round-trip is stable.
	for i := int64(1); i < 100; i++ {
		got := mutations.DurationToFrames(mutations.FramesToDuration(i, consts.SampleRate48000), consts.SampleRate48000)
		assert.Equal(t, i, got, "frame %d", i)
	}
}

func TestDurationToFramesNegativeRounds(t *testing.T) {
	assert.Equal(t, int64(-consts.SampleRate48000), mutations.DurationToFrames(-time.Second, consts.SampleRate48000))
	assert.Equal(t, int64(-1), mutations.DurationToFrames(mutations.FramesToDuration(-1, consts.SampleRate48000), consts.SampleRate48000))
}
