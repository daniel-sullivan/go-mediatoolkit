package duplex

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

// newTestEngine constructs an engine at 16 kHz mono with a fake
// detector and test-friendly knobs, applying overrides via mutate.
// Tests drive e.tick() directly (single-goroutine, deterministic)
// unless they specifically exercise Start's real pacing.
func newTestEngine(t *testing.T, mutate func(*Config)) (*Engine, *fakeDetector) {
	t.Helper()
	det := newFakeDetector(16000, 1)
	cfg := Config{
		SampleRate:  16000,
		Channels:    1,
		Detector:    det,
		PreRoll:     30 * time.Millisecond,
		LeadIn:      30 * time.Millisecond,
		EventBuffer: 4096,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	e, err := New(cfg)
	require.NoError(t, err)
	return e, det
}

func TestNew_Validation(t *testing.T) {
	det := newFakeDetector(16000, 1)
	base := func() Config {
		return Config{SampleRate: 16000, Channels: 1, Detector: det}
	}
	cases := []struct {
		name   string
		mutate func(*Config)
		err    error
	}{
		{"zero sample rate", func(c *Config) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"non-multiple-of-100 rate", func(c *Config) { c.SampleRate = 44150 }, ErrBadSampleRate},
		{"aec-unsupported rate", func(c *Config) { c.SampleRate = 44100; c.Detector = newFakeDetector(44100, 1); c.AEC = new(AECConfig) }, ErrBadSampleRate},
		{"zero channels", func(c *Config) { c.Channels = 0 }, ErrBadChannels},
		{"too many channels", func(c *Config) { c.Channels = 65 }, ErrBadChannels},
		{"nil detector", func(c *Config) { c.Detector = nil }, ErrNilDetector},
		{"detector rate mismatch", func(c *Config) { c.Detector = newFakeDetector(48000, 1) }, ErrDetectorMismatch},
		{"detector channel mismatch", func(c *Config) { c.Detector = newFakeDetector(16000, 2) }, ErrDetectorMismatch},
		{"nil render processor", func(c *Config) { c.RenderChain = []mutations.Processor{nil} }, ErrNilProcessor},
		{"nil capture processor", func(c *Config) { c.CaptureChain = []mutations.Processor{nil} }, ErrNilProcessor},
		{"negative preroll", func(c *Config) { c.PreRoll = -time.Second }, ErrBadConfig},
		{"negative leadin", func(c *Config) { c.LeadIn = -time.Second }, ErrBadConfig},
		{"negative tag unit", func(c *Config) { c.TagUnit = -time.Second }, ErrBadConfig},
		{"negative crossfade", func(c *Config) { c.Crossfade = -time.Second }, ErrBadConfig},
		{"negative capture buffer", func(c *Config) { c.CaptureBuffer = -time.Second }, ErrBadConfig},
		{"negative event buffer", func(c *Config) { c.EventBuffer = -1 }, ErrBadConfig},
		{"empty ambient", func(c *Config) { c.Ambient = new(AmbientConfig) }, ErrBadConfig},
		{"short ambient", func(c *Config) { c.Ambient = &AmbientConfig{PCM: make([]float64, 80)} }, ErrBadConfig},
		{"negative ambient gain", func(c *Config) { c.Ambient = &AmbientConfig{PCM: make([]float64, 320), Gain: -1} }, ErrBadConfig},
		{"misaligned ambient", func(c *Config) {
			c.Channels = 2
			c.Detector = newFakeDetector(16000, 2)
			c.Ambient = &AmbientConfig{PCM: make([]float64, 641)}
		}, ErrBadConfig},
		{"valid", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			if tc.mutate != nil {
				tc.mutate(&cfg)
			}
			e, err := New(cfg)
			if tc.err != nil {
				assert.ErrorIs(t, err, tc.err)
				assert.Nil(t, e)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, e)
		})
	}
}

func TestEngine_IdleTicksEmitSilenceWithContiguousSeq(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	frames, seqs := collectOutput(e)

	for i := 0; i < 5; i++ {
		e.tick()
	}

	require.Len(t, *frames, 5, "one output frame per tick")
	for i, f := range *frames {
		require.Len(t, f, 160)
		assert.Equal(t, constFrame(160, 0), f, "underrun frame %d must be silence", i)
		assert.Equal(t, int64(i+1), (*seqs)[i], "seq increments by exactly 1 from 1")
	}
	assert.Equal(t, int64(5), e.Metrics().RenderedFrames)
}

func TestEngine_FeedChunkSlicesAndCarriesPartial(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	frames, _ := collectOutput(e)

	// 2.5 frames: two whole frames play, the half frame waits.
	require.NoError(t, e.FeedChunk(constFrame(400, 0.5)))
	for i := 0; i < 3; i++ {
		e.tick()
	}
	assert.Equal(t, constFrame(160, 0.5), (*frames)[0])
	assert.Equal(t, constFrame(160, 0.5), (*frames)[1])
	assert.Equal(t, constFrame(160, 0), (*frames)[2], "held partial must not play")

	// Completing the partial releases one more whole frame.
	require.NoError(t, e.FeedChunk(constFrame(80, 0.5)))
	e.tick()
	assert.Equal(t, constFrame(160, 0.5), (*frames)[3], "carried partial completed by the next chunk")

	// Misaligned feed is rejected outright.
	e2, _ := newTestEngine(t, func(c *Config) { c.Channels = 2; c.Detector = newFakeDetector(16000, 2) })
	assert.ErrorIs(t, e2.FeedChunk(make([]float64, 321)), ErrBadFrame)
}

func TestEngine_MarkChunkBoundaryFlushesPartialZeroPadded(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) { c.Crossfade = time.Nanosecond }) // isolate the flush
	frames, _ := collectOutput(e)

	require.NoError(t, e.FeedChunk(constFrame(240, 0.5))) // 1.5 frames
	e.MarkChunkBoundary()
	require.NoError(t, e.FeedChunk(constFrame(160, -0.5)))

	for i := 0; i < 3; i++ {
		e.tick()
	}
	assert.Equal(t, constFrame(160, 0.5), (*frames)[0])
	want := append(constFrame(80, 0.5), constFrame(80, 0)...)
	assert.Equal(t, want, (*frames)[1], "boundary must flush the partial zero-padded, not splice chunks")
	assert.Equal(t, constFrame(160, -0.5), (*frames)[2])
}

func TestEngine_CrossfadeSmoothsMarkedSeam(t *testing.T) {
	const fade = 5 * time.Millisecond // 80 samples at 16kHz

	run := func(t *testing.T, crossfade time.Duration) []float64 {
		e, _ := newTestEngine(t, func(c *Config) { c.Crossfade = crossfade })
		frames, _ := collectOutput(e)
		require.NoError(t, e.FeedChunk(constFrame(320, 0.8)))
		e.MarkChunkBoundary()
		require.NoError(t, e.FeedChunk(constFrame(320, -0.8)))
		for i := 0; i < 4; i++ {
			e.tick()
		}
		var all []float64
		for _, f := range *frames {
			all = append(all, f...)
		}
		return all
	}

	maxStep := func(samples []float64) float64 {
		var m float64
		for i := 1; i < len(samples); i++ {
			if d := math.Abs(samples[i] - samples[i-1]); d > m {
				m = d
			}
		}
		return m
	}

	// Control: a hard cut has the full 1.6 discontinuity at the seam.
	hard := run(t, time.Nanosecond)
	assert.Greater(t, maxStep(hard), 1.5, "hard cut control must show the raw discontinuity")

	// Equal-power fade over 80 samples: worst adjacent-sample step is
	// ~1.6·(π/2)/80 ≈ 0.031; assert an order-of-magnitude bound.
	faded := run(t, fade)
	assert.Less(t, maxStep(faded), 0.1, "marked seam must be crossfaded smooth")

	// The blend must sit inside the seam frame only: audio before and
	// after is untouched.
	assert.Equal(t, constFrame(160, 0.8), faded[:160])
	assert.Equal(t, constFrame(160, -0.8), faded[480:])

	// Unmarked seams do not fade: same feeds, no MarkChunkBoundary.
	e, _ := newTestEngine(t, nil)
	frames, _ := collectOutput(e)
	require.NoError(t, e.FeedChunk(constFrame(320, 0.8)))
	require.NoError(t, e.FeedChunk(constFrame(320, -0.8)))
	for i := 0; i < 4; i++ {
		e.tick()
	}
	assert.Equal(t, constFrame(160, -0.8), (*frames)[2], "unmarked seam must not blend")
}

func TestEngine_ClearPendingDropsQueuedAudioImmediately(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	frames, _ := collectOutput(e)

	require.NoError(t, e.FeedChunk(constFrame(800, 0.5))) // 5 frames
	e.tick()
	e.ClearPending()
	e.tick()

	assert.Equal(t, constFrame(160, 0.5), (*frames)[0])
	assert.Equal(t, constFrame(160, 0), (*frames)[1], "queued audio must be gone the very next tick")
}

func TestEngine_AmbientLoopsAndMixesUnderVoice(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) {
		// 1.5 frames of constant 0.25 at gain 0.5 → the loop wraps
		// mid-frame; contribution is a constant 0.125 either way.
		c.Ambient = &AmbientConfig{PCM: constFrame(240, 0.25), Gain: 0.5}
	})
	frames, _ := collectOutput(e)

	for i := 0; i < 4; i++ {
		e.tick()
	}
	for i := 0; i < 4; i++ {
		assert.Equal(t, constFrame(160, 0.125), (*frames)[i], "ambient must loop seamlessly across its wrap (frame %d)", i)
	}

	require.NoError(t, e.FeedChunk(constFrame(160, 0.5)))
	e.tick()
	assert.Equal(t, constFrame(160, 0.625), (*frames)[4], "ambient mixes under the voice")
}

func TestEngine_MixClampsToFullScale(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) {
		c.Ambient = &AmbientConfig{PCM: constFrame(160, 0.25)} // gain zero → unity
	})
	frames, _ := collectOutput(e)

	require.NoError(t, e.FeedChunk(constFrame(160, 0.9)))
	e.tick()
	assert.Equal(t, constFrame(160, 1.0), (*frames)[0], "0.9 voice + 0.25 ambient must clamp at full scale")
}

func TestEngine_RenderChainRunsBeforeAmbient(t *testing.T) {
	e, _ := newTestEngine(t, func(c *Config) {
		c.RenderChain = []mutations.Processor{mutations.NewGain(0.5)}
		c.Ambient = &AmbientConfig{PCM: constFrame(160, 0.2)}
	})
	frames, _ := collectOutput(e)

	require.NoError(t, e.FeedChunk(constFrame(160, 0.8)))
	e.tick()
	// Voice through the chain (0.8·0.5 = 0.4) + un-gained ambient 0.2.
	assert.InDeltaSlice(t, constFrame(160, 0.6), (*frames)[0], 1e-12,
		"render chain applies to the voice, ambient mixes after it")
}

func TestEngine_StartPacesRealTime(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	var mu sync.Mutex
	var seqs []int64
	e.SetOutput(func(frame []float64, seq int64) {
		mu.Lock()
		seqs = append(seqs, seq)
		mu.Unlock()
	})

	require.NoError(t, e.Start(context.Background()))
	assert.ErrorIs(t, e.Start(context.Background()), ErrEngineStarted)
	time.Sleep(250 * time.Millisecond)
	e.Stop()

	mu.Lock()
	defer mu.Unlock()
	// 250ms at one frame per 10ms is 25 ticks; allow generous CI
	// scheduling slop in both directions.
	assert.GreaterOrEqual(t, len(seqs), 15, "ticker must pace roughly one frame per 10ms")
	assert.LessOrEqual(t, len(seqs), 40, "ticker must not free-run")
	for i, s := range seqs {
		assert.Equal(t, int64(i+1), s, "seq must be gapless")
	}
}

func TestEngine_StopLifecycle(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	require.NoError(t, e.Start(context.Background()))
	e.Stop()
	e.Stop() // idempotent

	_, open := <-e.Events()
	assert.False(t, open, "Stop must close the events channel")
	assert.ErrorIs(t, e.Start(context.Background()), ErrEngineStopped)

	// Stop without Start still closes the channel.
	e2, _ := newTestEngine(t, nil)
	e2.Stop()
	_, open = <-e2.Events()
	assert.False(t, open)

	// Context cancellation stops the loop and closes the channel too.
	e3, _ := newTestEngine(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, e3.Start(ctx))
	cancel()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-e3.Events():
			if !open {
				return
			}
		case <-deadline:
			t.Fatal("events channel not closed after context cancellation")
		}
	}
}

func TestEngine_ConcurrentAPIUnderRace(t *testing.T) {
	e, _ := newTestEngine(t, nil)
	require.NoError(t, e.Start(context.Background()))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	work := []func(){
		func() { _ = e.FeedChunk(constFrame(160, 0.3)) },
		func() { _ = e.Push(constFrame(160, 0.1), 1) },
		func() { e.Metrics() },
		func() { e.MarkChunkBoundary() },
		func() { e.ClearPending() },
		func() { e.SetOutput(func([]float64, int64) {}) },
		func() { e.SetAudioBufferDelay(20 * time.Millisecond) },
	}
	for _, fn := range work {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					fn()
				}
			}
		}()
	}
	// Keep the events channel drained so the audio goroutine never
	// parks on a full buffer.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range e.Events() {
		}
	}()

	time.Sleep(200 * time.Millisecond)
	close(stop)
	e.Stop()
	wg.Wait()
}
