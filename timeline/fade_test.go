package timeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestNewFadeInZeroDurationIsUnity(t *testing.T) {
	tr := NewFadeIn(0)
	assert.Nil(t, tr.Gain)
}

func TestNewFadeInRamp(t *testing.T) {
	tr := NewFadeIn(100 * time.Millisecond)
	require.Len(t, tr.Gain, 2)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(tr.Gain, 0), 1e-9)
	assert.InDelta(t, 0.5, mutations.EnvelopeValue(tr.Gain, 50*time.Millisecond), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, 100*time.Millisecond), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, time.Second), 1e-9)
}

func TestNewFadeOutHoldsThenFades(t *testing.T) {
	tr := NewFadeOut(200*time.Millisecond, 100*time.Millisecond)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, 0), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, 150*time.Millisecond), 1e-9)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, 200*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.5, mutations.EnvelopeValue(tr.Gain, 250*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(tr.Gain, 300*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(tr.Gain, time.Second), 1e-9)
}

func TestNewFadeOutAtZero(t *testing.T) {
	tr := NewFadeOut(0, 100*time.Millisecond)
	require.Len(t, tr.Gain, 2)
	assert.InDelta(t, 1.0, mutations.EnvelopeValue(tr.Gain, 0), 1e-9)
	assert.InDelta(t, 0.5, mutations.EnvelopeValue(tr.Gain, 50*time.Millisecond), 1e-9)
	assert.InDelta(t, 0.0, mutations.EnvelopeValue(tr.Gain, 100*time.Millisecond), 1e-9)
}

func TestCrossfadeMirroredEnvelopes(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	// 10-frame cues; crossfade over the last 4 frames of from (into
	// the first 4 frames of to).
	from := mustClip(t, ones(10), consts.SampleRate48000, 1)
	to := mustClip(t, twos(10), consts.SampleRate48000, 1)

	_, _, err = Crossfade(tl,
		Cue{Source: from.Playhead()},
		Cue{Source: to.Playhead()},
		mutations.FramesToDuration(4, consts.SampleRate48000),
	)
	require.NoError(t, err)

	// Overall window: from plays 0..9, to plays 6..15 (10 frames starting
	// at frame 6). Crossfade region is frames 6..9 (4 frames).
	dst := make([]float64, 16)
	_, err = tl.Pull(dst)
	require.NoError(t, err)

	// Frames 0..5: from @ 1.0, to silent.
	for i := 0; i < 6; i++ {
		assert.InDelta(t, 1.0, dst[i], 1e-3, "frame %d", i)
	}
	// Frames 6..9: equal-linear crossfade. At frame 6 from is still at
	// full gain (1.0) while to has just appeared at 0 gain. The sum
	// ramps 1.0 → 1.75 across the four fade frames.
	assert.InDelta(t, 1.00, dst[6], 0.01, "frame 6: from=1, to=0")
	assert.InDelta(t, 1.25, dst[7], 0.01, "frame 7")
	assert.InDelta(t, 1.50, dst[8], 0.01, "frame 8")
	assert.InDelta(t, 1.75, dst[9], 0.01, "frame 9")
	// Frames 10..15: from exhausted, to @ full gain 2.0.
	for i := 10; i < 16; i++ {
		assert.InDelta(t, 2.0, dst[i], 1e-3, "frame %d", i)
	}
}

func TestCrossfadeIndefiniteSourceErrors(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	loopClip := mustClip(t, []float64{1, 1}, consts.SampleRate48000, 1)
	loopSrc := Repeat(consts.SampleRate48000, 1, 0, loopClip.Playhead)

	clip := mustClip(t, []float64{1, 2}, consts.SampleRate48000, 1)
	_, _, err = Crossfade(tl,
		Cue{Source: loopSrc},
		Cue{Source: clip.Playhead()},
		10*time.Millisecond,
	)
	assert.ErrorIs(t, err, ErrCrossfadeIndefinite)
}

func TestCrossfadeNilSource(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	_, _, err = Crossfade(tl, Cue{}, Cue{}, time.Millisecond)
	assert.ErrorIs(t, err, ErrNilSource)
}

func ones(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 1
	}
	return out
}

func twos(n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 2
	}
	return out
}
