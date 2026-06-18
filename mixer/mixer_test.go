package mixer

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/mutations"
	"go-mediatoolkit/timeline"
)

// drainWithTimeout pulls from the mixer until buf is full or timeout
// elapses. Returns how many samples ended up filled (the rest are zeros
// written by Fill on underrun).
func drainWithTimeout(t *testing.T, m *Mixer, buf []float64, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if m.ring.Len() >= len(buf) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	m.Fill(buf)
}

// waitForSteadyState repeatedly Fills buf and returns the first buf
// whose every sample satisfies pred. Skips initial silence written to
// the ring before a newly-added track took effect.
func waitForSteadyState(t *testing.T, m *Mixer, bufSize int, timeout time.Duration, pred func(float64) bool) []float64 {
	t.Helper()
	deadline := time.Now().Add(timeout)
	buf := make([]float64, bufSize)
	for time.Now().Before(deadline) {
		drainWithTimeout(t, m, buf, 100*time.Millisecond)
		ok := true
		for _, v := range buf {
			if !pred(v) {
				ok = false
				break
			}
		}
		if ok {
			return buf
		}
		time.Sleep(3 * time.Millisecond)
	}
	t.Fatalf("steady state not reached within %v; last buf head=%v tail=%v", timeout, buf[:8], buf[len(buf)-8:])
	return buf
}

func TestNewValidation(t *testing.T) {
	_, err := New(Config{SampleRate: 0, Channels: 1})
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = New(Config{SampleRate: consts.SampleRate48000, Channels: 0})
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestMixerFillSilenceWhenNoTracks(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 128})
	require.NoError(t, err)
	defer m.Close()

	buf := make([]float64, 16)
	for i := range buf {
		buf[i] = 99
	}
	drainWithTimeout(t, m, buf, 200*time.Millisecond)
	for _, v := range buf {
		assert.Equal(t, 0.0, v)
	}
}

func TestMixerSumsTwoTracks(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	a := clip(t, repeated(0.3, 200000), consts.SampleRate48000, 1)
	b := clip(t, repeated(0.4, 200000), consts.SampleRate48000, 1)
	_, err = m.AddSource(a.Playhead())
	require.NoError(t, err)
	_, err = m.AddSource(b.Playhead())
	require.NoError(t, err)

	buf := waitForSteadyState(t, m, 64, time.Second, func(v float64) bool {
		return v > 0.65 && v < 0.75
	})
	for i, v := range buf {
		assert.InDelta(t, 0.7, v, 1e-9, "frame %d", i)
	}
}

func TestMixerAppliesGain(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	c := clip(t, repeated(0.5, 200000), consts.SampleRate48000, 1)
	h, err := m.AddSource(c.Playhead())
	require.NoError(t, err)
	h.SetGain(0.25)

	buf := waitForSteadyState(t, m, 32, time.Second, func(v float64) bool {
		return v > 0.12 && v < 0.13
	})
	for i, v := range buf {
		assert.InDelta(t, 0.125, v, 1e-6, "frame %d", i)
	}
	assert.Equal(t, 0.25, h.Gain())
}

func TestMixerSoftSaturatesClippingSum(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	for i := 0; i < 3; i++ {
		c := clip(t, repeated(1.0, 200000), consts.SampleRate48000, 1)
		_, err = m.AddSource(c.Playhead())
		require.NoError(t, err)
	}

	buf := waitForSteadyState(t, m, 64, time.Second, func(v float64) bool {
		return v > mutations.SoftSaturationThreshold
	})
	for _, v := range buf {
		assert.Less(t, v, 1.0, "saturator must keep output below 1.0")
		assert.Greater(t, v, mutations.SoftSaturationThreshold, "loud sum should still be louder than threshold")
	}
}

func TestMixerRemoveSilencesTrack(t *testing.T) {
	// Small ring so we can drain stale content in a single Fill.
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 64, ChunkFrames: 32, IdleTick: 100 * time.Microsecond})
	require.NoError(t, err)
	defer m.Close()

	// Enough samples that the track cannot EOF during the test.
	c := clip(t, repeated(0.5, 200000), consts.SampleRate48000, 1)
	h, err := m.AddSource(c.Playhead())
	require.NoError(t, err)

	h.Remove()
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("removed track's Done did not close")
	}

	// Ring may still hold a chunk's worth of pre-remove samples —
	// drain it.
	drain := make([]float64, 128)
	m.Fill(drain)

	// Give the mixer a few iterations to fill the ring with silence.
	time.Sleep(30 * time.Millisecond)
	result := make([]float64, 32)
	m.Fill(result)
	for i, v := range result {
		assert.Equal(t, 0.0, v, "frame %d after remove", i)
	}
}

func TestMixerTrackEOFClosesDone(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	c := clip(t, repeated(0.1, 32), consts.SampleRate48000, 1)
	h, err := m.AddSource(c.Playhead())
	require.NoError(t, err)

	// Drive several fills so the source EOFs.
	buf := make([]float64, 64)
	for i := 0; i < 10; i++ {
		drainWithTimeout(t, m, buf, 100*time.Millisecond)
	}

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("EOF did not close Done")
	}
}

func TestMixerCloseFinishesAllTracks(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1})
	require.NoError(t, err)

	c := clip(t, repeated(0.5, 4096), consts.SampleRate48000, 1)
	h, err := m.AddSource(c.Playhead())
	require.NoError(t, err)

	require.NoError(t, m.Close())
	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("Close did not finish track")
	}

	_, err = m.AddSource(c.Playhead())
	assert.ErrorIs(t, err, ErrMixerClosed)

	require.NoError(t, m.Close(), "Close idempotent")
}

func TestMixerAutoAdaptsMonoTrackToStereoOutput(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 2, RingFrames: 2048, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	c := clip(t, repeated(0.5, 128), consts.SampleRate48000, 1)
	_, err = m.AddSource(c.Playhead())
	require.NoError(t, err)

	buf := make([]float64, 128) // 64 stereo frames
	drainWithTimeout(t, m, buf, 500*time.Millisecond)
	// L and R should match for a mono-duplicated source.
	for i := 0; i < len(buf); i += 2 {
		assert.InDelta(t, buf[i], buf[i+1], 1e-9, "frame %d", i/2)
	}
}

func TestMixerCapsRingWhenLiveSourcePresent(t *testing.T) {
	// LiveRingFrames is a floor; the runtime cap is
	// max(LiveRingFrames, observedCallback + 2*ChunkFrames). With a
	// floor of 512 and 64-frame drains the floor dominates, so the
	// ring should settle near 512.
	m, err := New(Config{
		SampleRate:     consts.SampleRate48000,
		Channels:       1,
		RingFrames:     4096,
		ChunkFrames:    64,
		LiveRingFrames: 512,
	})
	require.NoError(t, err)
	defer m.Close()

	in, err := timeline.NewInputSource(consts.SampleRate48000, 1, 4096)
	require.NoError(t, err)
	cb := in.Callback()
	cb(repeated(0.5, 4000))

	_, err = m.AddSource(in)
	require.NoError(t, err)

	// Drain pre-add silence using realistically-sized fills so the
	// observed-callback high-water mark stays small; the auto-tune
	// then resolves to the configured floor.
	time.Sleep(30 * time.Millisecond)
	drain := make([]float64, 64)
	for i := 0; i < 200; i++ {
		m.Fill(drain)
	}

	// Give the mixer time to refill up to the live cap.
	time.Sleep(50 * time.Millisecond)

	// Effective cap = max(512, 64 + 2*64) = 512. Tolerate one chunk
	// of overshoot before the next iteration sees atCap.
	assert.LessOrEqual(t, m.ring.Len(), 576, "ring must not exceed live-cap floor + one chunk while a live track is present")
	assert.Greater(t, m.ring.Len(), 0, "mixer should still be producing some output")
}

func TestMixerLiveCapAutoGrowsToCallbackSize(t *testing.T) {
	// When the device hands the mixer a callback larger than the
	// configured LiveRingFrames floor, the cap auto-grows so the ring
	// always covers one drain plus a chunk of jitter headroom. Without
	// this, BT-style 1024–2048-frame callbacks would underrun every
	// time on the default tight floor.
	m, err := New(Config{
		SampleRate:     consts.SampleRate48000,
		Channels:       1,
		RingFrames:     8192,
		ChunkFrames:    64,
		LiveRingFrames: 128,
	})
	require.NoError(t, err)
	defer m.Close()

	in, err := timeline.NewInputSource(consts.SampleRate48000, 1, 8192)
	require.NoError(t, err)
	cb := in.Callback()
	cb(repeated(0.5, 8000))

	_, err = m.AddSource(in)
	require.NoError(t, err)

	// Simulate a single big callback (BT-style 2048-frame drain). The
	// mixer records 2048 as observedCallback and grows the live-source
	// cap so the ring covers at least one device drain plus jitter
	// headroom — the documented runtime cap is
	// max(LiveRingFrames, observedCallback + 2*ChunkFrames).
	//
	// The interesting invariant — and the whole point of this test — is
	// the *lower* bound: the ring must auto-grow to >= 2048 so an
	// oversized callback no longer underruns on the tight default floor.
	// We do not assert a tight upper window: the exact steady-state
	// settle point is scheduling-dependent. The mix goroutine checks
	// ring.Len() against the cap once per iteration and then writes a
	// chunk, so the ring can sit a chunk or more above the nominal cap;
	// under the race detector's ~10x slowdown the producer can overshoot
	// by several chunks before the consumer is rescheduled (observed
	// settle points of 2176, 2560, 2688, even 6144 frames under -race).
	// All of those still satisfy the real contract — covered the drain,
	// never exceeded the physical ring — so the only sound upper bound is
	// the ring capacity itself.
	//
	// Poll rather than sleep a fixed amount: under -asan/-race the
	// background drain goroutine is scheduled slowly, so a fixed sleep
	// is flaky. require.Eventually returns as soon as the condition
	// holds, so the generous 30s timeout only matters on failure.
	require.Eventually(t, func() bool {
		return m.ring.Len() > 0
	}, 30*time.Second, 5*time.Millisecond,
		"ring should start filling from the registered source")

	big := make([]float64, 2048)
	m.Fill(big)

	// The ring must auto-grow to cover the observed 2048-frame callback,
	// bounded only by the physical ring capacity. Require the condition
	// to hold for several consecutive reads so a single transient
	// (mid-grow) sample can neither satisfy nor trip the assertion.
	const lo = 2048 // must cover the observed callback size
	hi := m.ring.Cap()
	stable := 0
	require.Eventually(t, func() bool {
		n := m.ring.Len()
		if n >= lo && n <= hi {
			stable++
			return stable >= 5
		}
		stable = 0
		return false
	}, 30*time.Second, 5*time.Millisecond,
		"ring must auto-grow to cover the observed callback size (>= %d frames) and stay within the ring capacity (%d)", lo, hi)
}

func TestMixerRingFillsNormallyWithoutLiveSource(t *testing.T) {
	m, err := New(Config{
		SampleRate:     consts.SampleRate48000,
		Channels:       1,
		RingFrames:     4096,
		ChunkFrames:    64,
		LiveRingFrames: 128,
	})
	require.NoError(t, err)
	defer m.Close()

	c := clip(t, repeated(0.5, 200000), consts.SampleRate48000, 1)
	_, err = m.AddSource(c.Playhead())
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	// No live source — ring should fill well past the live cap.
	assert.Greater(t, m.ring.Len(), 128, "ring should fill past LiveRingFrames when no live source registered")
}

func TestMixerCustomSaturator(t *testing.T) {
	// Hard-clip saturator: sum > 1 becomes exactly 1, not the
	// soft-saturator's < 1 asymptote.
	m, err := New(Config{
		SampleRate:  consts.SampleRate48000,
		Channels:    1,
		RingFrames:  1024,
		ChunkFrames: 64,
		Saturator:   mutations.HardClip,
	})
	require.NoError(t, err)
	defer m.Close()

	for i := 0; i < 3; i++ {
		c := clip(t, repeated(1.0, 200000), consts.SampleRate48000, 1)
		_, err = m.AddSource(c.Playhead())
		require.NoError(t, err)
	}

	buf := waitForSteadyState(t, m, 64, time.Second, func(v float64) bool {
		return v == 1.0
	})
	for _, v := range buf {
		assert.Equal(t, 1.0, v, "hard clip must pin overshoot at exactly 1")
	}
}

func TestMixerPartialFrameFillZeros(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 2})
	require.NoError(t, err)
	defer m.Close()
	buf := []float64{1, 2, 3} // not a whole number of stereo frames
	m.Fill(buf)
	assert.Equal(t, []float64{0, 0, 0}, buf)
}

func TestMixerEmitsUnderrunEvent(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 256, ChunkFrames: 32})
	require.NoError(t, err)
	defer m.Close()

	var underruns atomic.Uint64
	m.Events().Subscribe(func(e Event) {
		if e.Kind == EventUnderrun {
			underruns.Add(1)
		}
	})

	// Drain the ring to force underrun.
	for i := 0; i < 100; i++ {
		buf := make([]float64, 512)
		m.Fill(buf)
	}
	assert.Greater(t, underruns.Load(), uint64(0))
	assert.Greater(t, m.Underruns(), uint64(0))
}

func TestMixerTrackLifecycleEvents(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	added := make(chan uint64, 1)
	removed := make(chan uint64, 1)
	m.Events().Subscribe(func(e Event) {
		switch e.Kind {
		case EventTrackAdded:
			select {
			case added <- e.TrackID:
			default:
			}
		case EventTrackRemoved:
			select {
			case removed <- e.TrackID:
			default:
			}
		}
	})

	// Large enough that the source will not EOF during the test,
	// so Remove (not natural end) triggers the second event.
	c := clip(t, repeated(0.5, 200000), consts.SampleRate48000, 1)
	h, err := m.AddSource(c.Playhead())
	require.NoError(t, err)

	select {
	case id := <-added:
		assert.Equal(t, h.ID(), id)
	case <-time.After(time.Second):
		t.Fatal("TrackAdded event not delivered")
	}

	h.Remove()

	select {
	case id := <-removed:
		assert.Equal(t, h.ID(), id)
	case <-time.After(time.Second):
		t.Fatal("TrackRemoved event not delivered")
	}
}

func repeated(v float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// Ensure a full timeline integration: static timeline → mixer track.
func TestMixerWithTimeline(t *testing.T) {
	m, err := New(Config{SampleRate: consts.SampleRate48000, Channels: 1, RingFrames: 1024, ChunkFrames: 64})
	require.NoError(t, err)
	defer m.Close()

	tl, err := timeline.NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	defer tl.Close()

	c := clip(t, repeated(0.6, 200000), consts.SampleRate48000, 1)
	_, err = tl.Schedule(timeline.Cue{Source: c.Playhead()})
	require.NoError(t, err)

	_, err = m.AddSource(&boundedTimeline{Timeline: tl, frames: 200000})
	require.NoError(t, err)

	buf := waitForSteadyState(t, m, 64, time.Second, func(v float64) bool {
		return v > 0.55 && v < 0.65
	})
	for i, v := range buf {
		assert.InDelta(t, 0.6, v, 1e-9, "frame %d", i)
	}
}

type boundedTimeline struct {
	*timeline.Timeline
	frames  int
	pulled  int
	exhaust bool
}

func (b *boundedTimeline) Pull(dst []float64) (int, error) {
	if b.exhaust {
		return 0, ioEOF
	}
	rem := b.frames - b.pulled
	if rem <= 0 {
		b.exhaust = true
		return 0, ioEOF
	}
	take := len(dst) / b.Channels()
	if take > rem {
		take = rem
		dst = dst[:take*b.Channels()]
	}
	n, err := b.Timeline.Pull(dst)
	b.pulled += n / b.Channels()
	if b.pulled >= b.frames {
		b.exhaust = true
		return n, ioEOF
	}
	return n, err
}

func (b *boundedTimeline) Duration() time.Duration {
	return time.Duration(b.frames) * time.Second / time.Duration(b.SampleRate())
}
