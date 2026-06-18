package timeline

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"
	"go-mediatoolkit/mutations"
)

func audio(samples []float64, rate, chans int) mutations.Audio {
	return mutations.Audio{Data: samples, SampleRate: rate, Channels: chans}
}

func TestTimelineAppendPlacesAtEnd(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	a := mustClip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)
	b := mustClip(t, []float64{4, 5}, consts.SampleRate48000, 1)

	_, err := tl.Append(Cue{Source: a.Playhead()})
	require.NoError(t, err)
	_, err = tl.Append(Cue{Source: b.Playhead()})
	require.NoError(t, err)

	dst := make([]float64, 6)
	_, _ = tl.Pull(dst)
	assert.Equal(t, []float64{1, 2, 3, 4, 5, 0}, dst)
}

func TestTimelineAppendRejectsInfiniteSource(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	// Repeat is indefinite — Append must refuse.
	clip := mustClip(t, []float64{1}, consts.SampleRate48000, 1)
	loop := Repeat(consts.SampleRate48000, 1, 0, clip.Playhead)
	_, err := tl.Append(Cue{Source: loop})
	assert.ErrorIs(t, err, ErrUnboundedSource)
}

func TestTimelineAppendAudioSequential(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	_, err := tl.AppendAudio(audio([]float64{1, 2, 3}, consts.SampleRate48000, 1))
	require.NoError(t, err)
	_, err = tl.AppendAudio(audio([]float64{4, 5}, consts.SampleRate48000, 1))
	require.NoError(t, err)

	dst := make([]float64, 5)
	_, _ = tl.Pull(dst)
	assert.Equal(t, []float64{1, 2, 3, 4, 5}, dst)
}

func TestTimelineAppendAudioFormatMismatch(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	_, err := tl.AppendAudio(audio([]float64{1, 2}, consts.SampleRate44100, 1))
	assert.ErrorIs(t, err, ErrFormatMismatch)
}

func TestTimelineMixedScheduleAndAppend(t *testing.T) {
	// Append fills sequentially; Schedule can add parallel cues.
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	_, _ = tl.AppendAudio(audio([]float64{1, 1, 1, 1}, consts.SampleRate48000, 1))
	// Parallel cue that overlaps frames 1..3.
	overlap := mustClip(t, []float64{2, 2, 2}, consts.SampleRate48000, 1)
	_, _ = tl.Schedule(Cue{
		Source: overlap.Playhead(),
		Start:  mutations.FramesToDuration(1, consts.SampleRate48000),
	})

	dst := make([]float64, 4)
	_, _ = tl.Pull(dst)
	assert.Equal(t, []float64{1, 3, 3, 3}, dst)
}

func TestTimelineScheduledDuration(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	assert.Equal(t, time.Duration(0), tl.ScheduledDuration())
	_, _ = tl.AppendAudio(audio(make([]float64, 480), consts.SampleRate48000, 1))
	assert.Equal(t, 10*time.Millisecond, tl.ScheduledDuration())
}

func TestTimelineKeepHistorySeekBack(t *testing.T) {
	tl, _ := NewTimelineWith(Config{
		SampleRate:  consts.SampleRate48000,
		Channels:    1,
		KeepHistory: true,
	})
	_, _ = tl.AppendAudio(audio([]float64{1, 2, 3, 4}, consts.SampleRate48000, 1))

	first := make([]float64, 4)
	_, _ = tl.Pull(first)
	require.Equal(t, []float64{1, 2, 3, 4}, first)

	// Rewind 3 frames.
	require.NoError(t, tl.Seek(-mutations.FramesToDuration(3, consts.SampleRate48000)))

	replay := make([]float64, 3)
	_, _ = tl.Pull(replay)
	assert.Equal(t, []float64{2, 3, 4}, replay)
}

func TestTimelineSeekWithoutHistoryFails(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	_, _ = tl.AppendAudio(audio([]float64{1, 2}, consts.SampleRate48000, 1))
	drain := make([]float64, 2)
	_, _ = tl.Pull(drain)
	err := tl.Seek(-time.Millisecond)
	assert.ErrorIs(t, err, ErrSeekOutOfRange)
}

func TestTimelineSeekWithoutFactoryFails(t *testing.T) {
	// Schedule without Factory → Seek can't rebuild → error.
	tl, _ := NewTimelineWith(Config{
		SampleRate:  consts.SampleRate48000,
		Channels:    1,
		KeepHistory: true,
	})
	clip := mustClip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	_, _ = tl.Schedule(Cue{Source: clip.Playhead()}) // no Factory

	dst := make([]float64, 2)
	_, _ = tl.Pull(dst)

	err := tl.Seek(-mutations.FramesToDuration(1, consts.SampleRate48000))
	assert.ErrorIs(t, err, ErrNotSeekable)
}

func TestTimelineSeekWithFactoryOK(t *testing.T) {
	tl, _ := NewTimelineWith(Config{
		SampleRate:  consts.SampleRate48000,
		Channels:    1,
		KeepHistory: true,
	})
	clip := mustClip(t, []float64{10, 20, 30, 40}, consts.SampleRate48000, 1)
	_, _ = tl.Schedule(Cue{
		Source:  clip.Playhead(),
		Factory: func() Source { return clip.Playhead() },
	})

	first := make([]float64, 4)
	_, _ = tl.Pull(first)
	require.Equal(t, []float64{10, 20, 30, 40}, first)

	require.NoError(t, tl.Seek(-mutations.FramesToDuration(3, consts.SampleRate48000)))
	replay := make([]float64, 3)
	_, _ = tl.Pull(replay)
	assert.Equal(t, []float64{20, 30, 40}, replay)
}

func TestTimelinePositionAdvancesWithPull(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	assert.Equal(t, time.Duration(0), tl.Position())
	dst := make([]float64, 480) // 10ms
	_, _ = tl.Pull(dst)
	assert.Equal(t, 10*time.Millisecond, tl.Position())
}

func TestTimelineSeekForwardPastScheduledIsOK(t *testing.T) {
	tl, _ := NewTimelineWith(Config{
		SampleRate:  consts.SampleRate48000,
		Channels:    1,
		KeepHistory: true,
	})
	_, _ = tl.AppendAudio(audio([]float64{1, 2}, consts.SampleRate48000, 1))
	require.NoError(t, tl.Seek(time.Second))
	dst := make([]float64, 4)
	n, err := tl.Pull(dst)
	assert.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{0, 0, 0, 0}, dst)
}

func TestTimelineLiveAppendPattern(t *testing.T) {
	// Simulate a producer Appending 5 chunks in a loop, consumer
	// Pulls in parallel. Realtime mode (no history).
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	const chunks = 5
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < chunks; i++ {
			data := make([]float64, 48) // 1ms
			for j := range data {
				data[j] = float64(i + 1)
			}
			_, _ = tl.AppendAudio(audio(data, consts.SampleRate48000, 1))
			time.Sleep(100 * time.Microsecond)
		}
	}()

	total := 0
	for total < chunks*48 {
		buf := make([]float64, 48)
		n, err := tl.Pull(buf)
		total += n
		require.NoError(t, err)
		if n < 48 {
			time.Sleep(200 * time.Microsecond)
		}
	}
	<-done
}

func TestTimelineAppendAfterPullCursorIsLinear(t *testing.T) {
	// Append after the consumer has drained: the new audio should
	// start at the current end frame, play contiguously even though
	// Pull advanced the cursor past older cues.
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	_, _ = tl.AppendAudio(audio([]float64{1, 2}, consts.SampleRate48000, 1))
	first := make([]float64, 2)
	_, _ = tl.Pull(first)

	_, _ = tl.AppendAudio(audio([]float64{3, 4}, consts.SampleRate48000, 1))
	next := make([]float64, 2)
	n, err := tl.Pull(next)
	assert.Equal(t, 2, n)
	_ = err
	assert.Equal(t, []float64{3, 4}, next)
}

func TestTimelineCloseDrainsHandles(t *testing.T) {
	tl, _ := NewTimeline(consts.SampleRate48000, 1)
	h, _ := tl.AppendAudio(audio([]float64{1, 2, 3}, consts.SampleRate48000, 1))
	require.NoError(t, tl.Close())
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("handle not closed after Timeline.Close")
	}
	dst := make([]float64, 4)
	_, err := tl.Pull(dst)
	assert.ErrorIs(t, err, io.EOF)
}
