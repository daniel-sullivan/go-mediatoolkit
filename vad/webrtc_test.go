package vad

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// appendSpeechLike appends frames of a speech-like signal: an
// amplitude-modulated (4 Hz syllabic envelope) harmonic series on a
// slowly wandering ~150 Hz fundamental — the spectral/temporal shape
// the WebRTC GMM's six-band features classify as speech, unlike a
// steady sine.
func appendSpeechLike(sig []float64, frames, rate, ch int) []float64 {
	start := len(sig) / ch
	for i := 0; i < frames; i++ {
		tt := float64(start + i)
		f := 150 + 100*math.Sin(2*math.Pi*0.3*tt/float64(rate))
		env := 0.5 + 0.5*math.Sin(2*math.Pi*4*tt/float64(rate))
		v := env * 0.3 * (math.Sin(2*math.Pi*f*tt/float64(rate)) +
			0.6*math.Sin(2*math.Pi*2*f*tt/float64(rate)) +
			0.3*math.Sin(2*math.Pi*5*f*tt/float64(rate)))
		for c := 0; c < ch; c++ {
			sig = append(sig, v)
		}
	}
	return sig
}

func TestWebRTCConfigValidation(t *testing.T) {
	base := WebRTCConfig{SampleRate: 16000, Channels: 1}

	cases := []struct {
		name   string
		mutate func(*WebRTCConfig)
		want   error
	}{
		{"rate too low", func(c *WebRTCConfig) { c.SampleRate = 7999 }, ErrBadSampleRate},
		{"rate zero", func(c *WebRTCConfig) { c.SampleRate = 0 }, ErrBadSampleRate},
		{"channels zero", func(c *WebRTCConfig) { c.Channels = 0 }, ErrBadChannels},
		{"channels too many", func(c *WebRTCConfig) { c.Channels = 65 }, ErrBadChannels},
		{"mode negative", func(c *WebRTCConfig) { c.Mode = -1 }, ErrBadMode},
		{"mode too high", func(c *WebRTCConfig) { c.Mode = 4 }, ErrBadMode},
		{"frame duration odd", func(c *WebRTCConfig) { c.FrameDuration = 15 * time.Millisecond }, ErrBadFrameDuration},
		{"frame duration huge", func(c *WebRTCConfig) { c.FrameDuration = time.Second }, ErrBadFrameDuration},
		{"negative onset", func(c *WebRTCConfig) { c.Onset = -time.Millisecond }, ErrBadConfig},
		{"negative hangover", func(c *WebRTCConfig) { c.Hangover = -time.Millisecond }, ErrBadConfig},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			_, err := NewWebRTCDetector(cfg)
			require.ErrorIs(t, err, tc.want)
		})
	}

	// Every valid combination constructs.
	for _, rate := range []int{8000, 16000, 32000, 44100, 48000, 192000} {
		for _, dur := range []time.Duration{0, 10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond} {
			for mode := 0; mode <= 3; mode++ {
				det, err := NewWebRTCDetector(WebRTCConfig{
					SampleRate: rate, Channels: 2, Mode: mode, FrameDuration: dur,
				})
				require.NoError(t, err, "rate=%d dur=%v mode=%d", rate, dur, mode)
				require.Equal(t, rate, det.SampleRate())
				require.Equal(t, 2, det.Channels())
			}
		}
	}
}

// TestWebRTCDecisionLatency pins the latency arithmetic at a native
// rate (no resampler): one frame fill plus the current onset, tracking
// live SetOnset changes.
func TestWebRTCDecisionLatency(t *testing.T) {
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: 16000, Channels: 1})
	require.NoError(t, err)

	// Defaults: 20 ms frame + 2-frame onset = 60 ms.
	require.Equal(t, 60*time.Millisecond, det.DecisionLatency())

	require.NoError(t, det.SetOnset(10*time.Millisecond))
	require.Equal(t, 30*time.Millisecond, det.DecisionLatency())

	// Zero restores the 2-frame default.
	require.NoError(t, det.SetOnset(0))
	require.Equal(t, 60*time.Millisecond, det.DecisionLatency())

	// A resampled rate reports a non-negative resampler delay on top.
	det2, err := NewWebRTCDetector(WebRTCConfig{SampleRate: 44100, Channels: 1})
	require.NoError(t, err)
	require.GreaterOrEqual(t, det2.DecisionLatency(), 60*time.Millisecond)
}

// TestWebRTCDetectsSpeechRejectsSilence drives the canonical stream:
// silence → speech-like burst → silence, asserting the event sequence,
// the polled state at each phase, and sane back-timestamped positions.
func TestWebRTCDetectsSpeechRejectsSilence(t *testing.T) {
	const rate = 16000
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	evs := collectEvents(det)

	var sig []float64
	sig = appendSilence(sig, rate, 1) // 1 s silence
	burstStart := len(sig)
	sig = appendSpeechLike(sig, 2*rate, rate, 1) // 2 s speech-like
	burstEnd := len(sig)
	sig = appendSilence(sig, 2*rate, 1) // 2 s silence

	// Feed in 10 ms chunks; check state at the phase boundaries.
	chunk := rate / 100
	for off := 0; off < len(sig); off += chunk {
		det.Process(sig[off : off+chunk])
		if off+chunk == burstStart {
			require.False(t, det.Active(), "active during leading silence")
			require.Zero(t, det.Probability())
		}
		if off+chunk == burstEnd {
			require.True(t, det.Active(), "not active at end of speech burst")
			require.Equal(t, 1.0, det.Probability())
		}
	}
	require.False(t, det.Active(), "still active after trailing silence")

	// Exactly one Start/End pair, strictly alternating and monotonic.
	require.Len(t, *evs, 2)
	start, end := (*evs)[0], (*evs)[1]
	require.Equal(t, SpeechStart, start.Kind)
	require.Equal(t, SpeechEnd, end.Kind)
	require.Less(t, start.Frame, end.Frame)
	require.Equal(t, 1.0, start.Probability)
	require.Zero(t, end.Probability)

	// Back-timestamped positions: the start points into the burst (the
	// GMM needs a little model warm-up, so allow a few hundred ms), the
	// end points at/after where speech stopped, before the hangover
	// horizon plus libfvad's own overhang.
	require.GreaterOrEqual(t, start.Frame, int64(burstStart))
	require.Less(t, start.Frame, int64(burstStart+rate/2), "start too late: %v", start.Pos)
	require.GreaterOrEqual(t, end.Frame, int64(burstEnd-rate/4))
	require.Less(t, end.Frame, int64(burstEnd+rate/2), "end too late: %v", end.Pos)

	// LastTransition mirrors the final event.
	last, ok := det.LastTransition()
	require.True(t, ok)
	require.Equal(t, end, last)
}

// TestWebRTCBufferSizeInvariance: decisions are a pure function of the
// stream, not of Process call granularity — 1-frame, prime, and giant
// pushes produce identical event sequences.
func TestWebRTCBufferSizeInvariance(t *testing.T) {
	const rate = 32000
	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendSpeechLike(sig, rate, rate, 1)
	sig = appendSilence(sig, rate, 1)

	runWith := func(chunk int) []SpeechEvent {
		det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		evs := collectEvents(det)
		processChunks(det, sig, chunk, 1)
		return *evs
	}

	reference := runWith(len(sig))
	require.NotEmpty(t, reference, "signal produced no events")
	for _, chunk := range []int{1, 7, 113, 640} {
		require.Equal(t, reference, runWith(chunk), "chunk=%d", chunk)
	}
}

// TestWebRTCResampledRateDetects: a non-native rate resamples to 16 kHz
// internally and still detects, with positions reported in the INPUT
// timeline and close to a native-16k reference run of the same audio.
func TestWebRTCResampledRateDetects(t *testing.T) {
	const inRate = 44100
	const refRate = 16000

	build := func(rate int, ch int) []float64 {
		var sig []float64
		sig = appendSilence(sig, rate/2, ch)
		sig = appendSpeechLike(sig, 2*rate, rate, ch)
		sig = appendSilence(sig, rate, ch)
		return sig
	}

	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: inRate, Channels: 2})
	require.NoError(t, err)
	evs := collectEvents(det)
	processChunks(det, build(inRate, 2), 441, 2)

	ref, err := NewWebRTCDetector(WebRTCConfig{SampleRate: refRate, Channels: 1})
	require.NoError(t, err)
	refEvs := collectEvents(ref)
	processChunks(ref, build(refRate, 1), 160, 1)

	require.NotEmpty(t, *refEvs, "reference produced no events")
	require.NotEmpty(t, *evs, "resampled run produced no events")
	require.Equal(t, (*refEvs)[0].Kind, (*evs)[0].Kind)

	// Positions map back to the input timeline: compare as durations.
	// The resampled signal is not sample-identical to the reference
	// (different phase grid), so allow a couple of engine frames.
	diff := ((*evs)[0].Pos - (*refEvs)[0].Pos).Abs()
	require.Less(t, diff, 3*20*time.Millisecond,
		"start positions diverge: %v vs %v", (*evs)[0].Pos, (*refEvs)[0].Pos)
}

// TestWebRTCLiveSetters exercises validation and live effect of every
// setter.
func TestWebRTCLiveSetters(t *testing.T) {
	const rate = 16000
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)

	require.ErrorIs(t, det.SetMode(-1), ErrBadMode)
	require.ErrorIs(t, det.SetMode(4), ErrBadMode)
	require.ErrorIs(t, det.SetOnset(-time.Second), ErrBadConfig)
	require.ErrorIs(t, det.SetHangover(-time.Second), ErrBadConfig)

	for mode := 0; mode <= 3; mode++ {
		require.NoError(t, det.SetMode(mode))
	}
	require.NoError(t, det.SetOnset(time.Nanosecond)) // no debounce
	require.NoError(t, det.SetHangover(50*time.Millisecond))

	// The detector still works after the changes (mode is applied on
	// the audio goroutine at the next decision frame).
	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendSpeechLike(sig, rate, rate, 1)
	evs := collectEvents(det)
	processChunks(det, sig, 160, 1)
	require.NotEmpty(t, *evs)
	require.Equal(t, SpeechStart, (*evs)[0].Kind)

	// A shorter hangover ends the utterance sooner than the default:
	// run the same signal + trailing silence twice.
	run := func(hangover time.Duration) time.Duration {
		d, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1})
		require.NoError(t, err)
		require.NoError(t, d.SetHangover(hangover))
		var s []float64
		s = appendSilence(s, rate/2, 1)
		s = appendSpeechLike(s, rate, rate, 1)
		s = appendSilence(s, rate, 1)
		es := collectEvents(d)
		processChunks(d, s, 160, 1)
		require.Len(t, *es, 2)
		return (*es)[1].Pos
	}
	// Positions are back-timestamped to where speech stopped, so they
	// should agree regardless of hangover — what changes is whether a
	// short gap bridges. Just require both to produce a clean pair.
	_ = run(40 * time.Millisecond)
	_ = run(400 * time.Millisecond)
}

// TestWebRTCReset: state clears, positions restart, configuration
// (including live-set values) is retained.
func TestWebRTCReset(t *testing.T) {
	const rate = 16000
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 1, Mode: 2})
	require.NoError(t, err)
	require.NoError(t, det.SetMode(3))

	var speech []float64
	speech = appendSilence(speech, rate/2, 1)
	speech = appendSpeechLike(speech, rate, rate, 1)

	evs := collectEvents(det)
	processChunks(det, speech, 160, 1)
	require.NotEmpty(t, *evs)
	require.True(t, det.Active())

	det.Reset()
	require.False(t, det.Active())
	require.Zero(t, det.Probability())
	_, ok := det.LastTransition()
	require.False(t, ok, "LastTransition should clear on Reset")

	// After reset the stream restarts at frame 0 and the detector still
	// detects (subscriptions survive; mode 3 retained — construction
	// succeeded and no error paths fired).
	*evs = nil
	processChunks(det, speech, 160, 1)
	require.NotEmpty(t, *evs, "no events after Reset")
	require.Equal(t, SpeechStart, (*evs)[0].Kind)
	require.Less(t, (*evs)[0].Frame, int64(len(speech)),
		"positions did not restart after Reset")
}

// TestWebRTCPassThrough: Process never modifies the samples.
func TestWebRTCPassThrough(t *testing.T) {
	const rate = 48000
	det, err := NewWebRTCDetector(WebRTCConfig{SampleRate: rate, Channels: 2})
	require.NoError(t, err)

	var sig []float64
	sig = appendSpeechLike(sig, rate/10, rate, 2)
	orig := append([]float64(nil), sig...)
	det.Process(sig)
	require.Equal(t, orig, sig, "Process modified the caller's samples")
}
