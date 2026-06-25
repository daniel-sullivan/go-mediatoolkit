package timeline

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func mustClip(t *testing.T, samples []float64, rate, chans int) *CachedClip {
	t.Helper()
	c, err := LoadClipFromPCM(samples, rate, chans)
	require.NoError(t, err)
	return c
}

func TestNewTimelineValidation(t *testing.T) {
	_, err := NewTimeline(0, 1)
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = NewTimeline(consts.SampleRate48000, 0)
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestTimelineMetadata(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 2)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, tl.SampleRate())
	assert.Equal(t, 2, tl.Channels())
	assert.Equal(t, time.Duration(-1), tl.Duration())
	assert.False(t, tl.Live())
}

func TestTimelineSilenceWhenEmpty(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	dst := []float64{9, 9, 9, 9}
	n, err := tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{0, 0, 0, 0}, dst)
}

func TestTimelineScheduleAtCursor(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)

	h, err := tl.Schedule(Cue{Source: clip.Playhead()})
	require.NoError(t, err)

	dst := make([]float64, 6)
	n, err := tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, []float64{1, 2, 3, 4, 0, 0}, dst)

	// Handle should be done after the clip finishes.
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("handle.Done never closed")
	}
}

func TestTimelineScheduleInFuture(t *testing.T) {
	// 4 frames at 48kHz = ~83μs. Schedule clip at 2 frames (~41μs).
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)

	startAt := mutations.FramesToDuration(2, consts.SampleRate48000)
	_, err = tl.Schedule(Cue{Source: clip.Playhead(), Start: startAt})
	require.NoError(t, err)

	dst := make([]float64, 6)
	n, err := tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, []float64{0, 0, 1, 2, 3, 0}, dst)
}

func TestTimelinePastScheduledMidClip(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	// Advance cursor by pulling 3 frames of silence.
	silence := make([]float64, 3)
	_, _ = tl.Pull(silence)

	// Schedule at time 0 — we're already at frame 3. Clip should pick
	// up from frame 3 of its own content.
	clip := mustClip(t, []float64{10, 20, 30, 40, 50, 60}, consts.SampleRate48000, 1)
	_, err = tl.Schedule(Cue{Source: clip.Playhead()})
	require.NoError(t, err)

	dst := make([]float64, 4)
	n, err := tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{40, 50, 60, 0}, dst)
}

func TestTimelinePastScheduledEntirelyBehind(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	silence := make([]float64, 100)
	_, _ = tl.Pull(silence)

	clip := mustClip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)
	h, err := tl.Schedule(Cue{Source: clip.Playhead()})
	require.NoError(t, err)

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("past-exhausted cue should finish immediately")
	}

	dst := make([]float64, 4)
	n, err := tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{0, 0, 0, 0}, dst)
}

func TestTimelineMixesOverlappingCues(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	a := mustClip(t, []float64{1, 1, 1, 1}, consts.SampleRate48000, 1)
	b := mustClip(t, []float64{0.5, 0.5, 0.5, 0.5}, consts.SampleRate48000, 1)

	_, err = tl.Schedule(Cue{Source: a.Playhead()})
	require.NoError(t, err)
	_, err = tl.Schedule(Cue{Source: b.Playhead(), Start: mutations.FramesToDuration(2, consts.SampleRate48000)})
	require.NoError(t, err)

	dst := make([]float64, 6)
	_, err = tl.Pull(dst)
	require.NoError(t, err)
	assert.InDeltaSlice(t, []float64{1, 1, 1.5, 1.5, 0.5, 0.5}, dst, 1e-9)
}

func TestTimelineCancelStopsPlayback(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1, 2, 3, 4, 5, 6}, consts.SampleRate48000, 1)

	h, err := tl.Schedule(Cue{Source: clip.Playhead()})
	require.NoError(t, err)

	dst := make([]float64, 2)
	_, _ = tl.Pull(dst)
	assert.Equal(t, []float64{1, 2}, dst)

	h.Cancel()

	dst = make([]float64, 4)
	_, _ = tl.Pull(dst)
	assert.Equal(t, []float64{0, 0, 0, 0}, dst)

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Cancel did not close Done")
	}
}

func TestTimelineClose(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)
	h, err := tl.Schedule(Cue{Source: clip.Playhead()})
	require.NoError(t, err)

	require.NoError(t, tl.Close())

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not finish handle")
	}

	_, err = tl.Schedule(Cue{Source: clip.Playhead()})
	assert.ErrorIs(t, err, ErrTimelineClosed)

	dst := make([]float64, 2)
	n, err := tl.Pull(dst)
	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, io.EOF)

	assert.NoError(t, tl.Close(), "Close is idempotent")
}

func TestTimelineScheduleValidation(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	_, err = tl.Schedule(Cue{})
	assert.ErrorIs(t, err, ErrNilSource)

	clip := mustClip(t, []float64{1}, consts.SampleRate48000, 1)
	_, err = tl.Schedule(Cue{Source: clip.Playhead(), Start: -time.Millisecond})
	assert.ErrorIs(t, err, ErrNegativeStart)

	mismatch := mustClip(t, []float64{1}, consts.SampleRate44100, 1)
	_, err = tl.Schedule(Cue{Source: mismatch.Playhead()})
	assert.ErrorIs(t, err, ErrFormatMismatch)

	stereoMismatch := mustClip(t, []float64{1, 1}, consts.SampleRate48000, 2)
	_, err = tl.Schedule(Cue{Source: stereoMismatch.Playhead()})
	assert.ErrorIs(t, err, ErrFormatMismatch)

	_, err = tl.Schedule(Cue{
		Source:    clip.Playhead(),
		Transform: Transform{Gain: []EnvelopePoint{{At: 100 * time.Millisecond}, {At: 0}}},
	})
	assert.ErrorIs(t, err, mutations.ErrUnsortedEnvelope)
}

func TestTimelinePullPartialFrame(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 2)
	require.NoError(t, err)
	dst := make([]float64, 3) // not a whole number of stereo frames
	_, err = tl.Pull(dst)
	assert.ErrorIs(t, err, ErrPartialFrame)
}

func TestTimelineTransformAppliedEndToEnd(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1, 1, 1, 1}, consts.SampleRate48000, 1)
	_, err = tl.Schedule(Cue{
		Source:    clip.Playhead(),
		Transform: Transform{Gain: []EnvelopePoint{{At: 0, Value: 0.25}}},
	})
	require.NoError(t, err)

	dst := make([]float64, 4)
	_, err = tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, []float64{0.25, 0.25, 0.25, 0.25}, dst)
}

func TestTimelineScheduleConcurrentWithPull(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	clip := mustClip(t, []float64{1}, consts.SampleRate48000, 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		dst := make([]float64, 64)
		for i := 0; i < 100; i++ {
			_, _ = tl.Pull(dst)
		}
	}()
	for i := 0; i < 100; i++ {
		_, err := tl.Schedule(Cue{Source: clip.Playhead(), Start: mutations.FramesToDuration(int64(i*10), consts.SampleRate48000)})
		require.NoError(t, err)
	}
	<-done
}
