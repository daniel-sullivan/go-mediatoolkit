package vad

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/goldensignals"
	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/silero"
)

func TestSileroConfigValidation(t *testing.T) {
	valid := SileroConfig{SampleRate: 16000, Channels: 1}

	cases := []struct {
		name string
		mut  func(*SileroConfig)
		want error
	}{
		{"rate below 8k", func(c *SileroConfig) { c.SampleRate = 7999 }, ErrBadSampleRate},
		{"zero rate", func(c *SileroConfig) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"zero channels", func(c *SileroConfig) { c.Channels = 0 }, ErrBadChannels},
		{"too many channels", func(c *SileroConfig) { c.Channels = 65 }, ErrBadChannels},
		{"NaN threshold", func(c *SileroConfig) { c.Threshold = math.NaN() }, ErrBadThreshold},
		{"threshold above 1", func(c *SileroConfig) { c.Threshold = 1.5 }, ErrBadThreshold},
		{"negative threshold", func(c *SileroConfig) { c.Threshold = -0.5 }, ErrBadThreshold},
		{"neg at threshold", func(c *SileroConfig) { c.Threshold = 0.5; c.NegThreshold = 0.5 }, ErrBadThreshold},
		{"neg above threshold", func(c *SileroConfig) { c.Threshold = 0.4; c.NegThreshold = 0.6 }, ErrBadThreshold},
		{"neg at 1", func(c *SileroConfig) { c.NegThreshold = 1 }, ErrBadThreshold},
		{"NaN neg", func(c *SileroConfig) { c.NegThreshold = math.NaN() }, ErrBadThreshold},
		{"negative MinSpeech", func(c *SileroConfig) { c.MinSpeech = -time.Millisecond }, ErrBadConfig},
		{"negative MinSilence", func(c *SileroConfig) { c.MinSilence = -time.Millisecond }, ErrBadConfig},
		{"negative SpeechPad", func(c *SileroConfig) { c.SpeechPad = -time.Millisecond }, ErrBadConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := valid
			tc.mut(&cfg)
			_, err := NewSileroDetector(cfg)
			assert.ErrorIs(t, err, tc.want)
		})
	}

	det, err := NewSileroDetector(valid)
	require.NoError(t, err)
	assert.Equal(t, 16000, det.SampleRate())
	assert.Equal(t, 1, det.Channels())
	assert.False(t, det.Active())
	assert.Zero(t, det.Probability())

	// Threshold below the derived-neg floor still validates (neg is
	// floored at 0.01, so the band collapses only at ≤ 0.01).
	_, err = NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.02})
	assert.NoError(t, err)
	_, err = NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.01})
	assert.ErrorIs(t, err, ErrBadThreshold)
}

func TestSileroDefaultsAndLatency(t *testing.T) {
	det, err := NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1})
	require.NoError(t, err)

	threshold, neg := unpackThresholds(det.thresholds.Load())
	assert.InDelta(t, 0.5, threshold, 1e-6)
	assert.InDelta(t, 0.35, neg, 1e-6, "derived NegThreshold = Threshold − 0.15")
	assert.False(t, det.negExplicit.Load())

	// 16 kHz input: no resampler. Window fill 32 ms + MinSpeech
	// (250 ms → 8 windows → 256 ms) = 288 ms.
	assert.Equal(t, 288*time.Millisecond, det.DecisionLatency())

	// The derived floor: a tiny threshold derives neg = 0.01.
	det2, err := NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.05})
	require.NoError(t, err)
	_, neg = unpackThresholds(det2.thresholds.Load())
	assert.InDelta(t, 0.01, neg, 1e-6)
}

func TestSileroLiveSetters(t *testing.T) {
	det, err := NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1})
	require.NoError(t, err)

	// SetThreshold re-derives neg while it is not explicit.
	require.NoError(t, det.SetThreshold(0.8))
	threshold, neg := unpackThresholds(det.thresholds.Load())
	assert.InDelta(t, 0.8, threshold, 1e-6)
	assert.InDelta(t, 0.65, neg, 1e-6)

	// Explicit SetNegThreshold pins it against further SetThreshold.
	require.NoError(t, det.SetNegThreshold(0.3))
	require.NoError(t, det.SetThreshold(0.6))
	threshold, neg = unpackThresholds(det.thresholds.Load())
	assert.InDelta(t, 0.6, threshold, 1e-6)
	assert.InDelta(t, 0.3, neg, 1e-6, "explicit neg must survive SetThreshold")

	// SetNegThreshold(0) restores derivation.
	require.NoError(t, det.SetNegThreshold(0))
	threshold, neg = unpackThresholds(det.thresholds.Load())
	assert.InDelta(t, 0.45, neg, 1e-6)
	require.NoError(t, det.SetThreshold(0.5))
	_, neg = unpackThresholds(det.thresholds.Load())
	assert.InDelta(t, 0.35, neg, 1e-6)

	// Validation.
	assert.ErrorIs(t, det.SetThreshold(1.5), ErrBadThreshold)
	assert.ErrorIs(t, det.SetThreshold(math.NaN()), ErrBadThreshold)
	require.NoError(t, det.SetNegThreshold(0.3))
	assert.ErrorIs(t, det.SetThreshold(0.25), ErrBadThreshold,
		"threshold below the explicit neg must be rejected")
	assert.ErrorIs(t, det.SetNegThreshold(0.9), ErrBadThreshold,
		"neg above the current threshold must be rejected")
	assert.ErrorIs(t, det.SetMinSpeech(-time.Second), ErrBadConfig)
	assert.ErrorIs(t, det.SetMinSilence(-time.Second), ErrBadConfig)
	assert.ErrorIs(t, det.SetSpeechPad(-time.Second), ErrBadConfig)

	// Zero restores defaults; MinSpeech feeds DecisionLatency.
	require.NoError(t, det.SetMinSpeech(time.Nanosecond))
	assert.Equal(t, 32*time.Millisecond+32*time.Millisecond, det.DecisionLatency(),
		"nanosecond MinSpeech rounds up to one window")
	require.NoError(t, det.SetMinSpeech(0))
	assert.Equal(t, 288*time.Millisecond, det.DecisionLatency())
}

// simulateSilero is an independent expression of the documented
// streaming VADIterator adaptation, driven directly by per-window
// probabilities (the golden file's oracle values). The behavioural
// tests feed the real detector audio and require its event stream to
// match this simulation exactly — pinning the state machine,
// position mapping, padding, and clamping against the documented
// semantics without depending on the model's numerics near
// thresholds (the chosen thresholds sit ≥ 1e-3 from every golden
// probability, far beyond the 1e-4 parity bound).
func simulateSilero(probs []float64, threshold, neg float64, minSpeech, minSilence, pad time.Duration) []SpeechEvent {
	const rate = silero.SampleRate
	// Mirror the engine's float32 pair quantisation.
	threshold = float64(float32(threshold))
	neg = float64(float32(neg))
	minSpeechS := mutations.DurationToFrames(minSpeech, rate)
	minSilenceS := mutations.DurationToFrames(minSilence, rate)
	padF := mutations.DurationToFrames(pad, rate)

	var events []SpeechEvent
	emit := func(kind SpeechEventKind, frame int64, p float64) {
		events = append(events, SpeechEvent{
			Kind: kind, Frame: frame,
			Pos:         mutations.FramesToDuration(frame, rate),
			Probability: p,
		})
	}

	var inRun, announced, tempEndSet, hasEnded bool
	var runStart, tempEnd, lastEndPos int64
	for w, p := range probs {
		start := int64(w) * silero.WindowSize
		if p >= threshold {
			tempEndSet = false
			if !inRun {
				inRun, announced, runStart = true, false, start
			}
		}
		if inRun && p < neg {
			if !tempEndSet {
				tempEnd, tempEndSet = start, true
			}
			if start-tempEnd >= minSilenceS {
				if announced {
					pos := tempEnd + padF
					if pos > start {
						pos = start
					}
					lastEndPos, hasEnded = pos, true
					emit(SpeechEnd, pos, p)
				}
				inRun, announced, tempEndSet = false, false, false
				continue
			}
		}
		if inRun && !announced {
			earliestEnd := start + silero.WindowSize
			if tempEndSet {
				earliestEnd = tempEnd
			}
			if earliestEnd-runStart > minSpeechS {
				announced = true
				pos := runStart - padF
				if pos < 0 {
					pos = 0
				}
				if hasEnded && pos < lastEndPos {
					pos = lastEndPos
				}
				emit(SpeechStart, pos, p)
			}
		}
	}
	return events
}

// TestSileroEventsMatchSimulation drives the real detector over the
// speech-pulses golden signal at 16 kHz mono (no resampling — exact
// positions) under several configurations and requires the event
// stream to match simulateSilero on the golden probabilities, plus
// structural invariants (alternation, monotonic non-overlapping
// padded positions).
func TestSileroEventsMatchSimulation(t *testing.T) {
	golden, err := goldensignals.LoadGolden("testdata/silero_golden.json")
	require.NoError(t, err)
	probs := golden.Probabilities["speech-pulses"]
	require.NotEmpty(t, probs)

	var sig []float64
	for _, s := range goldensignals.Signals() {
		if s.Name == "speech-pulses" {
			sig = make([]float64, len(s.Samples))
			for i, v := range s.Samples {
				sig[i] = float64(v)
			}
		}
	}
	require.NotNil(t, sig)

	configs := []struct {
		name string
		cfg  SileroConfig
	}{
		{"defaults", SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2}},
		{"instant", SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2,
			MinSpeech: time.Nanosecond, MinSilence: time.Nanosecond, SpeechPad: time.Nanosecond}},
		{"long-min-speech", SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2,
			MinSpeech: 2 * time.Second}},
		{"big-pad", SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2,
			SpeechPad: 400 * time.Millisecond}},
		{"long-min-silence", SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2,
			MinSilence: 500 * time.Millisecond}},
	}

	// Guard the simulation's premise: decisions must not hinge on the
	// 1e-4 model-vs-golden drift.
	for _, p := range probs {
		for _, th := range []float64{0.35, 0.2} {
			require.Greater(t, math.Abs(p-th), 1e-3,
				"golden probability %v sits too close to test threshold %v", p, th)
		}
	}

	for _, tc := range configs {
		t.Run(tc.name, func(t *testing.T) {
			det, err := NewSileroDetector(tc.cfg)
			require.NoError(t, err)
			got := collectEvents(det)

			// Assert reader/publish ordering from inside the callback.
			det.Events().Subscribe(func(ev SpeechEvent) {
				assert.Equal(t, ev.Kind == SpeechStart, det.Active())
				last, ok := det.LastTransition()
				assert.True(t, ok)
				assert.Equal(t, ev, last)
			})

			processChunks(det, sig, 160, 1)

			cfg := tc.cfg
			if cfg.MinSpeech == 0 {
				cfg.MinSpeech = defaultSileroMinSpeech
			}
			if cfg.MinSilence == 0 {
				cfg.MinSilence = defaultSileroMinSilence
			}
			if cfg.SpeechPad == 0 {
				cfg.SpeechPad = defaultSileroSpeechPad
			}
			want := simulateSilero(probs, cfg.Threshold, cfg.NegThreshold,
				cfg.MinSpeech, cfg.MinSilence, cfg.SpeechPad)

			require.Len(t, *got, len(want), "event count")
			for i, ev := range *got {
				assert.Equal(t, want[i].Kind, ev.Kind, "event %d kind", i)
				assert.Equal(t, want[i].Frame, ev.Frame, "event %d frame", i)
				assert.Equal(t, want[i].Pos, ev.Pos, "event %d pos", i)
				assert.InDelta(t, want[i].Probability, ev.Probability, 1e-4, "event %d probability", i)
			}

			// Structural invariants.
			var prevEnd int64
			for i, ev := range *got {
				if i%2 == 0 {
					assert.Equal(t, SpeechStart, ev.Kind)
					assert.GreaterOrEqual(t, ev.Frame, prevEnd, "padded start must not overlap previous end")
				} else {
					assert.Equal(t, SpeechEnd, ev.Kind)
					assert.GreaterOrEqual(t, ev.Frame, (*got)[i-1].Frame)
					prevEnd = ev.Frame
				}
				assert.GreaterOrEqual(t, ev.Frame, int64(0))
			}
			if tc.name == "long-min-speech" {
				assert.Empty(t, *got, "2 s MinSpeech must discard the 1.5 s utterances")
			} else {
				assert.NotEmpty(t, *got, "the pulse train must produce utterances")
			}
		})
	}
}

// TestSileroMinSilenceNearZeroStillAnnounces pins the fix for a bug
// where a qualifying run whose silence confirmed in the very same
// window it was first detected (MinSilence ≈ 0) was discarded with NO
// events at all: the end-confirmation used to be checked (and act,
// via an early return) BEFORE the MinSpeech announce check, so a run
// long enough to announce never got the chance. decide's state
// machine is exercised directly via stepFSM with a synthetic
// probability sequence — bypassing the model — so the exact
// same-window race is deterministic rather than depending on model
// numerics landing on a particular golden frame.
func TestSileroMinSilenceNearZeroStillAnnounces(t *testing.T) {
	det, err := NewSileroDetector(SileroConfig{
		SampleRate: 16000, Channels: 1,
		MinSpeech:  100 * time.Millisecond, // 1600 samples
		MinSilence: time.Nanosecond,        // rounds to 0 samples: confirms instantly
		SpeechPad:  0,
	})
	require.NoError(t, err)
	got := collectEvents(det)

	// threshold=0.5, neg=0.35 (defaults). Four windows above threshold
	// (2048 samples, past the 1600-sample MinSpeech gate) followed by
	// one window below neg: tempEnd is set AND confirmed (minSilenceS
	// == 0) in that same call.
	probs := []float64{0.9, 0.9, 0.9, 0.9, 0.1}
	for w, p := range probs {
		det.stepFSM(p, int64(w)*silero.WindowSize)
	}

	require.Len(t, *got, 2, "a qualifying run must announce Start and End, not be silently discarded")
	assert.Equal(t, SpeechStart, (*got)[0].Kind)
	assert.Equal(t, SpeechEnd, (*got)[1].Kind)
	assert.Equal(t, int64(4)*silero.WindowSize, (*got)[1].Frame, "end lands at tempEnd (window 4), unpadded")
}

func TestSileroResetReproducesEvents(t *testing.T) {
	var sig []float64
	for _, s := range goldensignals.Signals() {
		if s.Name == "speech-pulses" {
			sig = make([]float64, 10*goldensignals.SampleRate)
			for i := range sig {
				sig[i] = float64(s.Samples[i])
			}
		}
	}
	require.NotNil(t, sig)

	det, err := NewSileroDetector(SileroConfig{SampleRate: 16000, Channels: 1, Threshold: 0.35, NegThreshold: 0.2})
	require.NoError(t, err)
	got := collectEvents(det)

	processChunks(det, sig, 480, 1)
	first := append([]SpeechEvent(nil), (*got)...)
	require.NotEmpty(t, first, "the pulse train must trigger events")

	det.Reset()
	assert.False(t, det.Active())
	assert.Zero(t, det.Probability())
	_, ok := det.LastTransition()
	assert.False(t, ok)

	*got = nil
	processChunks(det, sig, 480, 1)
	assert.Equal(t, first, *got, "Reset must reproduce the identical event stream")
}

// TestSileroResampledSmoke exercises the 48 kHz stereo path end to
// end: the adapter downmixes and resamples into the fixed 16 kHz
// model, events fire for speech-like audio, and positions stay within
// the input stream. (Positional exactness under resampling is pinned
// by the adapter suite; the golden/parity gates pin the model.)
func TestSileroResampledSmoke(t *testing.T) {
	var mono16 []float32
	for _, s := range goldensignals.Signals() {
		if s.Name == "speech-pulses" {
			mono16 = s.Samples[:20*goldensignals.SampleRate]
		}
	}
	require.NotNil(t, mono16)

	// Naive 3× upsample (sample-and-hold) to 48 kHz stereo: crude,
	// but spectrally close enough for a smoke signal.
	sig := make([]float64, 0, len(mono16)*3*2)
	for _, v := range mono16 {
		for r := 0; r < 3; r++ {
			sig = append(sig, float64(v), float64(v))
		}
	}

	det, err := NewSileroDetector(SileroConfig{SampleRate: 48000, Channels: 2, Threshold: 0.3})
	require.NoError(t, err)
	got := collectEvents(det)

	processChunks(det, sig, 480, 2)

	require.NotEmpty(t, *got, "48 kHz stereo speech-like audio must trigger events")
	totalFrames := int64(len(sig) / 2)
	var prev int64 = -1
	for i, ev := range *got {
		assert.Equal(t, i%2 == 0, ev.Kind == SpeechStart, "alternation at %d", i)
		assert.GreaterOrEqual(t, ev.Frame, int64(0))
		assert.Less(t, ev.Frame, totalFrames)
		assert.GreaterOrEqual(t, ev.Frame, prev, "monotonic positions")
		prev = ev.Frame
	}
	assert.Positive(t, det.DecisionLatency())
}
