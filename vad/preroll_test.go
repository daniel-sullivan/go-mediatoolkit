package vad

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// replayAll drains p via Replay, returning copied frames and tags in
// replay order.
func replayAll(p *PreRoll) (frames [][]float64, tags []int64) {
	p.Replay(func(frame []float64, tag int64) {
		frames = append(frames, append([]float64(nil), frame...))
		tags = append(tags, tag)
	})
	return frames, tags
}

// constFrame returns a frame of n samples all equal to v.
func constFrame(n int, v float64) []float64 {
	f := make([]float64, n)
	for i := range f {
		f[i] = v
	}
	return f
}

func TestNewPreRoll_Validation(t *testing.T) {
	cases := []struct {
		name string
		cfg  PreRollConfig
		err  error
	}{
		{"zero sample rate", PreRollConfig{Channels: 1, Duration: time.Second}, ErrBadSampleRate},
		{"negative sample rate", PreRollConfig{SampleRate: -16000, Channels: 1}, ErrBadSampleRate},
		{"zero channels", PreRollConfig{SampleRate: 16000, Duration: time.Second}, ErrBadChannels},
		{"too many channels", PreRollConfig{SampleRate: 16000, Channels: 65}, ErrBadChannels},
		{"negative duration", PreRollConfig{SampleRate: 16000, Channels: 1, Duration: -time.Second}, ErrBadConfig},
		{"valid", PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 300 * time.Millisecond}, nil},
		{"valid disabled", PreRollConfig{SampleRate: 16000, Channels: 2}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := NewPreRoll(tc.cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				assert.Nil(t, p)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, p)
		})
	}
}

func TestPreRoll_DisabledIsInert(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1})
	require.NoError(t, err)

	p.Push(constFrame(160, 0.5), 10)
	p.Push(constFrame(160, 0.5), 20)

	_, ok := p.OldestTag()
	assert.False(t, ok, "disabled buffer must report no oldest tag")
	frames, _ := replayAll(p)
	assert.Empty(t, frames, "disabled buffer must replay nothing")
}

func TestPreRoll_ReplayOrderTagsAndClear(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: time.Second})
	require.NoError(t, err)

	p.Push(constFrame(160, 0.1), 100)
	p.Push(constFrame(80, 0.2), 110)
	p.Push(constFrame(160, 0.3), 115)

	tag, ok := p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(100), tag)

	frames, tags := replayAll(p)
	require.Len(t, frames, 3, "all pushed frames replayed")
	assert.Equal(t, []int64{100, 110, 115}, tags, "oldest-first order with original tags")
	assert.Equal(t, constFrame(160, 0.1), frames[0])
	assert.Equal(t, constFrame(80, 0.2), frames[1], "frame boundaries preserved verbatim")
	assert.Equal(t, constFrame(160, 0.3), frames[2])

	// Replay clears: a second replay sees nothing.
	frames, _ = replayAll(p)
	assert.Empty(t, frames, "Replay must clear the buffer")
	_, ok = p.OldestTag()
	assert.False(t, ok)
}

func TestPreRoll_PushCopies(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: time.Second})
	require.NoError(t, err)

	frame := constFrame(160, 0.25)
	p.Push(frame, 1)
	for i := range frame {
		frame[i] = -1 // caller reuses its buffer
	}

	frames, _ := replayAll(p)
	require.Len(t, frames, 1)
	assert.Equal(t, constFrame(160, 0.25), frames[0], "Push must copy — later caller mutation must not leak in")
}

func TestPreRoll_EvictsOldestBeyondDuration(t *testing.T) {
	// 30ms at 16kHz mono retains 480 samples = three 10ms frames.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 30 * time.Millisecond})
	require.NoError(t, err)

	for i := int64(0); i < 5; i++ {
		p.Push(constFrame(160, float64(i)), i)
	}

	tag, ok := p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(2), tag, "two oldest frames evicted")

	frames, tags := replayAll(p)
	assert.Equal(t, []int64{2, 3, 4}, tags, "newest three frames retained")
	for i, f := range frames {
		assert.Equal(t, float64(tags[i]), f[0], "frame content follows its tag")
	}
}

func TestPreRoll_OversizedFrameKept(t *testing.T) {
	// Retention 10ms = 160 samples, but a single 320-sample frame is
	// pushed: it must be kept (evicting it would leave nothing to
	// replay), then evicted by the next push.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 10 * time.Millisecond})
	require.NoError(t, err)

	p.Push(constFrame(320, 0.5), 1)
	tag, ok := p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(1), tag, "oversized lone frame retained")

	p.Push(constFrame(160, 0.7), 2)
	frames, tags := replayAll(p)
	require.Len(t, frames, 1)
	assert.Equal(t, []int64{2}, tags, "oversized frame evicted once a newer frame arrives")
}

func TestPreRoll_SetDuration(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 50 * time.Millisecond})
	require.NoError(t, err)

	for i := int64(0); i < 5; i++ {
		p.Push(constFrame(160, float64(i)), i)
	}

	// Negative: rejected, nothing changes.
	assert.ErrorIs(t, p.SetDuration(-time.Millisecond), ErrBadConfig)
	tag, ok := p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(0), tag)

	// Shrink to 20ms: only the newest two frames survive.
	require.NoError(t, p.SetDuration(20*time.Millisecond))
	tag, ok = p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(3), tag, "shrink keeps the newest frames")

	// Grow back: nothing reappears, and new pushes fill the wider window.
	require.NoError(t, p.SetDuration(50*time.Millisecond))
	_, tags := replayAll(p)
	assert.Equal(t, []int64{3, 4}, tags)

	// Zero: disables and clears.
	p.Push(constFrame(160, 0.5), 9)
	require.NoError(t, p.SetDuration(0))
	_, ok = p.OldestTag()
	assert.False(t, ok, "SetDuration(0) clears the buffer")
	p.Push(constFrame(160, 0.5), 10)
	frames, _ := replayAll(p)
	assert.Empty(t, frames, "SetDuration(0) disables Push")

	// Re-enable live.
	require.NoError(t, p.SetDuration(20*time.Millisecond))
	p.Push(constFrame(160, 0.5), 11)
	_, tags = replayAll(p)
	assert.Equal(t, []int64{11}, tags, "a later SetDuration re-enables the buffer")
}

func TestPreRoll_VaryingFrameSizes(t *testing.T) {
	// Retention 480 samples (30ms at 16kHz mono); mixed frame sizes.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 30 * time.Millisecond})
	require.NoError(t, err)

	p.Push(constFrame(320, 0.1), 1) // 20ms
	p.Push(constFrame(80, 0.2), 2)  // 5ms  → total 25ms
	p.Push(constFrame(160, 0.3), 3) // 10ms → total 35ms: evicts the 20ms frame
	_, tags := replayAll(p)
	assert.Equal(t, []int64{2, 3}, tags)
}

func TestPreRoll_ClearAndReuse(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 2, Duration: 100 * time.Millisecond})
	require.NoError(t, err)

	p.Push(constFrame(320, 0.1), 1)
	p.Clear()
	_, ok := p.OldestTag()
	assert.False(t, ok)

	// Recycled storage must not leak stale samples or tags across
	// push/replay/push cycles.
	for round := int64(0); round < 3; round++ {
		p.Push(constFrame(320, float64(round)), round*10)
		p.Push(constFrame(64, float64(round)+0.5), round*10+1)
		frames, tags := replayAll(p)
		require.Len(t, frames, 2)
		assert.Equal(t, []int64{round * 10, round*10 + 1}, tags)
		assert.Equal(t, constFrame(320, float64(round)), frames[0])
		assert.Equal(t, constFrame(64, float64(round)+0.5), frames[1])
	}
}

func TestPreRoll_TinyDurationKeepsNewestFrame(t *testing.T) {
	// A nonzero Duration shorter than one sample period must not
	// silently disable the buffer: it retains the single most recent
	// frame.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: time.Nanosecond})
	require.NoError(t, err)

	p.Push(constFrame(160, 0.1), 1)
	p.Push(constFrame(160, 0.2), 2)
	_, tags := replayAll(p)
	assert.Equal(t, []int64{2}, tags, "tiny nonzero duration keeps exactly the newest frame")
}

func TestPreRoll_HugeDurationSaturates(t *testing.T) {
	// A retention window large enough to overflow the duration→samples
	// conversion must saturate to "keep everything", not wrap negative
	// and evict everything.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 48000, Channels: 2, Duration: 1 << 62})
	require.NoError(t, err)

	for i := int64(0); i < 10; i++ {
		p.Push(constFrame(960, 0.5), i)
	}
	tag, ok := p.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(0), tag, "nothing evicted under a saturated window")
}

func TestPreRoll_ReplayReentrantPush(t *testing.T) {
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: time.Second})
	require.NoError(t, err)

	p.Push(constFrame(160, 0.1), 1)
	p.Push(constFrame(160, 0.2), 2)

	var replayed []int64
	p.Replay(func(frame []float64, tag int64) {
		replayed = append(replayed, tag)
		p.Push(constFrame(160, 0.9), tag+100) // reentrant push mid-replay
	})
	assert.Equal(t, []int64{1, 2}, replayed, "reentrant pushes must not be replayed by the same call")

	_, tags := replayAll(p)
	assert.Equal(t, []int64{101, 102}, tags, "reentrant pushes accumulate for the next replay")
}

func TestPreRoll_LongRunningEvictionCycle(t *testing.T) {
	// A steady push stream far past capacity: the window must always
	// hold exactly the newest frames, with head compaction never
	// corrupting order.
	p, err := NewPreRoll(PreRollConfig{SampleRate: 16000, Channels: 1, Duration: 100 * time.Millisecond})
	require.NoError(t, err)

	const total = 500 // 5s of 10ms frames through a 100ms window
	for i := int64(0); i < total; i++ {
		p.Push(constFrame(160, float64(i)), i)
	}
	_, tags := replayAll(p)
	require.Len(t, tags, 10, "100ms window holds ten 10ms frames")
	for i, tag := range tags {
		assert.Equal(t, int64(total-10+i), tag)
	}
}
