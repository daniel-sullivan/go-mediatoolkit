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

// TestTimelineContainsRepeat schedules a Repeat-wrapped loop as a
// Cue on a Timeline — the ambient-loop-under-one-shot layering
// pattern common in game audio.
func TestTimelineContainsRepeat(t *testing.T) {
	outer, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	loopClip := mustClip(t, []float64{0.1, 0.1, 0.1, 0.1}, consts.SampleRate48000, 1)
	loop := Repeat(consts.SampleRate48000, 1, 0, loopClip.Playhead)

	_, err = outer.Schedule(Cue{Source: loop})
	require.NoError(t, err)

	oneShot := mustClip(t, []float64{1, 1}, consts.SampleRate48000, 1)
	_, err = outer.Schedule(Cue{
		Source: oneShot.Playhead(),
		Start:  mutations.FramesToDuration(2, consts.SampleRate48000),
	})
	require.NoError(t, err)

	dst := make([]float64, 8)
	_, err = outer.Pull(dst)
	require.NoError(t, err)
	// Ambient loop contributes 0.1 per frame continuously; oneShot
	// contributes 1.0 at frames 2 and 3.
	want := []float64{0.1, 0.1, 1.1, 1.1, 0.1, 0.1, 0.1, 0.1}
	for i, v := range dst {
		assert.InDelta(t, want[i], v, 1e-9, "frame %d", i)
	}
}

// TestTimelineAppendSequential exercises the Append (back-to-back)
// path — formerly served by PlaylistTimeline. Cues land at the end
// of the last-scheduled cue.
func TestTimelineAppendSequential(t *testing.T) {
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)

	a := mustClip(t, []float64{1, 2}, consts.SampleRate48000, 1)
	b := mustClip(t, []float64{3, 4}, consts.SampleRate48000, 1)
	_, err = tl.Append(Cue{Source: a.Playhead()})
	require.NoError(t, err)
	_, err = tl.Append(Cue{Source: b.Playhead()})
	require.NoError(t, err)

	dst := make([]float64, 6)
	_, err = tl.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, []float64{1, 2, 3, 4, 0, 0}, dst)
}

// TestRepeatContainsTimeline loops an entire arrangement by making
// the factory return a freshly-built Timeline each iteration.
func TestRepeatContainsTimeline(t *testing.T) {
	clip := mustClip(t, []float64{5, 5, 5}, consts.SampleRate48000, 1)
	factory := func() Source {
		inner, err := NewTimeline(consts.SampleRate48000, 1)
		require.NoError(t, err)
		_, err = inner.Schedule(Cue{Source: clip.Playhead()})
		require.NoError(t, err)
		// Timeline never EOFs; bound its output at 3 frames so the
		// Repeat wrapper rotates to a fresh iteration.
		return &timelineBoundedSource{Timeline: inner, limit: 3}
	}
	outer := Repeat(consts.SampleRate48000, 1, mutations.FramesToDuration(6, consts.SampleRate48000), factory)

	dst := make([]float64, 12)
	_, err := outer.Pull(dst)
	require.NoError(t, err)
	// [5,5,5,0,0,0] repeated — the factory emits 3 real frames then
	// Repeat's duration-pad fills the rest.
	want := []float64{5, 5, 5, 0, 0, 0, 5, 5, 5, 0, 0, 0}
	assert.Equal(t, want, dst)
}

// timelineBoundedSource wraps a Timeline with a frame budget so it
// returns EOF after limit frames. Timelines themselves never EOF; the
// helper makes them usable inside Repeat tests.
type timelineBoundedSource struct {
	*Timeline
	limit  int64
	pulled int64
}

func (s *timelineBoundedSource) Pull(dst []float64) (int, error) {
	ch := int64(s.Channels())
	remFrames := s.limit - s.pulled
	if remFrames <= 0 {
		return 0, io.EOF
	}
	frames := int64(len(dst)) / ch
	if frames > remFrames {
		frames = remFrames
		dst = dst[:int(frames)*int(ch)]
	}
	n, err := s.Timeline.Pull(dst)
	s.pulled += int64(n) / ch
	if s.pulled >= s.limit {
		return n, io.EOF
	}
	return n, err
}

func (s *timelineBoundedSource) Duration() time.Duration {
	return mutations.FramesToDuration(s.limit, s.SampleRate())
}
