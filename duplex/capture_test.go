package duplex

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/vad"
)

func TestEngine_PushValidation(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.Channels = 2; c.Detector = newFakeDetector(16000, 2) })
	assert.ErrorIs(t, e.Push(make([]float64, 321), 0), ErrBadFrame)
	assert.NoError(t, e.Push(nil, 0), "empty push is a no-op")
	assert.NoError(t, e.Push(make([]float64, 320), 0))
}

func TestEngine_CaptureOverflowDropsNewest(t *testing.T) {
	// 100ms queue bound holds exactly ten 10ms frames.
	e, _ := newTestEngine(t, func(c *Config) { c.CaptureBuffer = 100 * time.Millisecond })

	for i := int64(0); i < 10; i++ {
		require.NoError(t, e.Push(constFrame(160, 0.1), i))
	}
	for i := int64(10); i < 15; i++ {
		assert.ErrorIs(t, e.Push(constFrame(160, 0.1), i), ErrCaptureOverflow)
	}

	m := e.Metrics()
	assert.Equal(t, uint64(5), m.CaptureFramesDropped)
	assert.Equal(t, 100*time.Millisecond, m.CaptureQueued)

	// Draining frees the bound again.
	e.tick()
	assert.NoError(t, e.Push(constFrame(160, 0.1), 20), "drained queue accepts pushes again")
}

func TestEngine_CaptureBurstFullyProcessedAcrossTicks(t *testing.T) {
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = time.Duration(0)
		c.LeadIn = time.Nanosecond
	})

	// Enter speech on the first frame, then burst 24 frames at once.
	det.trigger(vad.SpeechStart)
	for i := int64(0); i < 24; i++ {
		require.NoError(t, e.Push(constFrame(160, 0.2), i*10))
	}

	countAudio := func(evs []Event) (n int, tags []int64) {
		for _, ev := range evs {
			if ev.Kind == EventAudioFrame {
				n++
				tags = append(tags, ev.Tag)
			}
		}
		return n, tags
	}

	// One tick processes at most captureBatchFrames of the burst.
	e.tick()
	n, _ := countAudio(drainEvents(e))
	assert.Equal(t, captureBatchFrames, n, "per-tick capture work must be bounded")

	// Subsequent ticks absorb the rest, in order, losing nothing.
	var allTags []int64
	for i := 0; i < 5; i++ {
		e.tick()
		_, tags := countAudio(drainEvents(e))
		allTags = append(allTags, tags...)
	}
	require.Len(t, allTags, 16, "the whole burst must eventually be processed")
	for i, tag := range allTags {
		assert.Equal(t, int64((i+captureBatchFrames)*10), tag, "burst frames must stay FIFO")
	}
}

func TestEngine_CaptureReframingAssignsFirstSampleTags(t *testing.T) {
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = time.Duration(0)
		c.LeadIn = time.Nanosecond
	})
	det.trigger(vad.SpeechStart)

	// Three 15ms pushes (240 samples each) re-frame into 4.5 10ms
	// frames; each internal frame carries the tag of the push its
	// first sample came from.
	require.NoError(t, e.Push(constFrame(240, 0.1), 100))
	require.NoError(t, e.Push(constFrame(240, 0.2), 200))
	require.NoError(t, e.Push(constFrame(240, 0.3), 300))

	e.tick()
	var tags []int64
	for _, ev := range drainEvents(e) {
		if ev.Kind == EventAudioFrame {
			tags = append(tags, ev.Tag)
		}
	}
	assert.Equal(t, []int64{100, 100, 200, 300}, tags,
		"re-framed tags follow each frame's first sample; the trailing half frame waits")
}

func TestEngine_EventSequenceFullUtterance(t *testing.T) {
	// The full per-utterance contract, end to end: SpeechStart with a
	// back-dated timestamp, lead-in silence counting up to the oldest
	// pre-roll tag, post-DSP pre-roll replay, live frames, stop —
	// strictly FIFO.
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = 30 * time.Millisecond
		c.LeadIn = 30 * time.Millisecond
		c.CaptureChain = []mutations.Processor{mutations.NewGain(0.5)}
	})

	// Ten out-of-speech frames, tags 0..90: the 30ms pre-roll keeps
	// the newest three (70, 80, 90).
	for i := int64(0); i < 10; i++ {
		require.NoError(t, e.Push(constFrame(160, 0.4), i*10))
		e.tick()
	}
	assert.Empty(t, drainEvents(e), "no events before speech")

	// Speech starts on the frame tagged 100.
	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.6), 100))
	e.tick()

	evs := drainEvents(e)
	require.Len(t, evs, 8, "start + 3 lead-in + 3 pre-roll + 1 live")

	start := evs[0]
	require.Equal(t, EventSpeechStart, start.Kind, "EventSpeechStart must come first")
	assert.Equal(t, int64(40), start.Timestamp, "oldest pre-roll tag 70 backdated by 30ms lead-in")
	assert.Positive(t, start.OnsetFrame)

	for i, wantTag := range []int64{40, 50, 60} {
		af := evs[1+i]
		require.Equal(t, EventAudioFrame, af.Kind)
		assert.Equal(t, wantTag, af.Tag, "lead-in tags count up toward the real start")
		assert.Equal(t, constFrame(160, 0), af.Frame, "lead-in frames are silence")
	}

	for i, wantTag := range []int64{70, 80, 90} {
		af := evs[4+i]
		require.Equal(t, EventAudioFrame, af.Kind)
		assert.Equal(t, wantTag, af.Tag, "pre-roll replays oldest-first with original tags")
		assert.InDeltaSlice(t, constFrame(160, 0.2), af.Frame, 1e-12,
			"pre-roll frames are post-DSP (0.4 through the 0.5 gain chain)")
	}

	live := evs[7]
	require.Equal(t, EventAudioFrame, live.Kind)
	assert.Equal(t, int64(100), live.Tag)
	assert.InDeltaSlice(t, constFrame(160, 0.3), live.Frame, 1e-12, "live frames are post-DSP")

	// Two more in-speech frames stream straight through.
	for i := int64(11); i < 13; i++ {
		require.NoError(t, e.Push(constFrame(160, 0.6), i*10))
		e.tick()
	}
	evs = drainEvents(e)
	require.Len(t, evs, 2)
	assert.Equal(t, int64(110), evs[0].Tag)
	assert.Equal(t, int64(120), evs[1].Tag)

	// Speech stops on the frame tagged 130: a SpeechStop, and that
	// frame goes to the pre-roll for the NEXT utterance instead of
	// the event stream.
	det.trigger(vad.SpeechEnd)
	require.NoError(t, e.Push(constFrame(160, 0.4), 130))
	e.tick()
	evs = drainEvents(e)
	require.Len(t, evs, 1)
	require.Equal(t, EventSpeechStop, evs[0].Kind)
	assert.Equal(t, int64(130), evs[0].Tag)

	// The next utterance's replay proves frame 130 was banked.
	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.6), 140))
	e.tick()
	evs = drainEvents(e)
	var replayTags []int64
	for _, ev := range evs[1:] { // skip the EventSpeechStart
		if ev.Kind == EventAudioFrame {
			replayTags = append(replayTags, ev.Tag)
		}
	}
	assert.Contains(t, replayTags, int64(130), "post-utterance frame must resume pre-roll accumulation")
}

func TestEngine_SpeechStartTimestampClampsAtZero(t *testing.T) {
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = time.Duration(0)
		c.LeadIn = 30 * time.Millisecond
	})

	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.5), 0))
	e.tick()

	evs := drainEvents(e)
	require.Len(t, evs, 5, "start + 3 lead-in + live")
	require.Equal(t, EventSpeechStart, evs[0].Kind)
	assert.Equal(t, int64(0), evs[0].Timestamp, "back-dating clamps at 0")
	for _, ev := range evs[1:4] {
		assert.Equal(t, int64(0), ev.Tag, "lead-in tags clamp at 0 near session start")
	}
}

func TestEngine_PreRollDisabledFallsBackToLiveTag(t *testing.T) {
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = time.Duration(0)
		c.LeadIn = 30 * time.Millisecond
	})

	// Frames before speech are simply discarded (nothing banked).
	for i := int64(0); i < 5; i++ {
		require.NoError(t, e.Push(constFrame(160, 0.4), i*10))
		e.tick()
	}
	drainEvents(e)

	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.6), 500))
	e.tick()

	evs := drainEvents(e)
	require.Len(t, evs, 5, "start + 3 lead-in + live only — no replay")
	require.Equal(t, EventSpeechStart, evs[0].Kind)
	assert.Equal(t, int64(470), evs[0].Timestamp, "live tag 500 backdated by the 30ms lead-in")
	assert.Equal(t, []int64{470, 480, 490}, []int64{evs[1].Tag, evs[2].Tag, evs[3].Tag})
	assert.Equal(t, int64(500), evs[4].Tag)
}

func TestEngine_InSpeechGuardAbsorbsRedundantTransitions(t *testing.T) {
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = time.Duration(0)
		c.LeadIn = time.Nanosecond
	})

	// Two starts in a row: one SpeechStart.
	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.5), 0))
	e.tick()
	det.trigger(vad.SpeechStart)
	require.NoError(t, e.Push(constFrame(160, 0.5), 10))
	e.tick()

	var starts, stops int
	for _, ev := range drainEvents(e) {
		switch ev.Kind {
		case EventSpeechStart:
			starts++
		case EventSpeechStop:
			stops++
		}
	}
	assert.Equal(t, 1, starts, "redundant detector starts must be absorbed")

	// Two ends in a row: one SpeechStop.
	det.trigger(vad.SpeechEnd)
	require.NoError(t, e.Push(constFrame(160, 0.1), 20))
	e.tick()
	det.trigger(vad.SpeechEnd)
	require.NoError(t, e.Push(constFrame(160, 0.1), 30))
	e.tick()
	for _, ev := range drainEvents(e) {
		if ev.Kind == EventSpeechStop {
			stops++
		}
	}
	assert.Equal(t, 1, stops, "redundant detector ends must be absorbed")
}

// tunableDetector wraps fakeDetector with the tuning-setter methods
// the SetVAD* passthroughs assert for, recording what reaches it.
type tunableDetector struct {
	*fakeDetector
	threshold  float64
	minSilence time.Duration
}

func (d *tunableDetector) SetThreshold(v float64) error {
	d.threshold = v
	return nil
}

func (d *tunableDetector) SetMinSilence(v time.Duration) error {
	d.minSilence = v
	return nil
}

func TestEngine_VADTuningPassthrough(t *testing.T) {
	det := &tunableDetector{fakeDetector: newFakeDetector(16000, 1)}
	e, err := New(Config{SampleRate: 16000, Channels: 1, Detector: det})
	require.NoError(t, err)

	require.NoError(t, e.SetVADThreshold(0.7))
	assert.Equal(t, 0.7, det.threshold, "threshold must reach the detector")
	require.NoError(t, e.SetVADMinSilence(400*time.Millisecond))
	assert.Equal(t, 400*time.Millisecond, det.minSilence, "min-silence must reach the detector")

	// A detector without the setters reports ErrTuningUnsupported.
	plain, _ := newTestEngine(t, nil)
	assert.ErrorIs(t, plain.SetVADThreshold(0.5), ErrTuningUnsupported)
	assert.ErrorIs(t, plain.SetVADMinSilence(time.Second), ErrTuningUnsupported)
}

func TestEngine_StartAndStopWithinOneFrame(t *testing.T) {
	// A burst shorter than one frame: the detector reports start and
	// end from the same Process call. The engine must emit a
	// complete, ordered start→stop pair and bank the frame for the
	// next utterance (it was never forwarded live).
	e, det := newTestEngine(t, func(c *Config) {
		c.PreRoll = 30 * time.Millisecond
		c.LeadIn = time.Nanosecond
	})

	det.trigger(vad.SpeechStart)
	det.trigger(vad.SpeechEnd)
	require.NoError(t, e.Push(constFrame(160, 0.5), 100))
	e.tick()

	evs := drainEvents(e)
	require.Len(t, evs, 2)
	assert.Equal(t, EventSpeechStart, evs[0].Kind)
	assert.Equal(t, EventSpeechStop, evs[1].Kind)
	tag, ok := e.preroll.OldestTag()
	require.True(t, ok)
	assert.Equal(t, int64(100), tag, "the burst frame goes to the pre-roll, not the event stream")
}
