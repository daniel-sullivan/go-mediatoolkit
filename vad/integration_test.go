package vad

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/mixer"
	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
	"github.com/daniel-sullivan/go-mediatoolkit/timeline"
)

// TestGateEffectSourceTailFlush: the Processor-contract integration —
// a Gate in a timeline.EffectSource chain, with the lookahead tail
// flushed via WithTail exactly like a loudness.Limiter.
func TestGateEffectSourceTailFlush(t *testing.T) {
	const rate = 16000
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: 1})
	require.NoError(t, err)
	g, err := NewGate(GateConfig{
		SampleRate: rate, Channels: 1, Detector: det,
		Lookahead: det.DecisionLatency(),
	})
	require.NoError(t, err)

	var sig []float64
	sig = appendSilence(sig, rate/2, 1)
	sig = appendTone(sig, 1000, 0.5, rate/2, rate, 1)
	clip, err := timeline.LoadClipFromPCM(sig, rate, 1)
	require.NoError(t, err)

	src := timeline.NewEffectSource(clip.Playhead(), g).WithTail(g.Latency())

	var out []float64
	buf := make([]float64, 160)
	for {
		n, err := src.Pull(buf)
		out = append(out, buf[:n]...)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}

	wantFrames := len(sig) + int(mutations.DurationToFrames(g.Latency(), rate))
	assert.Equal(t, wantFrames, len(out), "WithTail must flush exactly the gate's latency")

	// The delayed burst survived the gate: energy near the end of the
	// stream (the burst region shifted by LatencyFrames).
	sum := 0.0
	for i := rate/2 + g.LatencyFrames(); i < len(out); i++ {
		sum += out[i] * out[i]
	}
	assert.Greater(t, sum, 100.0, "the gated burst must come through the EffectSource")
}

// TestMixerDuckingSmoke: the full cross-track topology on a real
// mixer, headless — voice track's EffectSource feeds the detector,
// bed track's EffectSource carries the Ducker reading it. Pinned by
// the ducking example too; here it just must run clean (and duck).
func TestMixerDuckingSmoke(t *testing.T) {
	const (
		rate = 16000
		ch   = 1
	)
	det, err := NewEnergyDetector(EnergyConfig{SampleRate: rate, Channels: ch})
	require.NoError(t, err)
	duck, err := NewDucker(DuckerConfig{SampleRate: rate, Channels: ch, Detector: det})
	require.NoError(t, err)

	var voice []float64
	voice = appendSilence(voice, rate/4, ch)
	voice = appendTone(voice, 1000, 0.5, rate, rate, ch)
	voiceClip, err := timeline.LoadClipFromPCM(voice, rate, ch)
	require.NoError(t, err)

	var bed []float64
	bed = appendTone(bed, 220, 0.3, rate*2, rate, ch)
	bedClip, err := timeline.LoadClipFromPCM(bed, rate, ch)
	require.NoError(t, err)

	mx, err := mixer.New(mixer.Config{SampleRate: rate, Channels: ch})
	require.NoError(t, err)
	defer mx.Close()

	_, err = mx.AddSource(timeline.NewEffectSource(voiceClip.Playhead(), det))
	require.NoError(t, err)
	_, err = mx.AddSource(timeline.NewEffectSource(bedClip.Playhead(), duck))
	require.NoError(t, err)

	// Drain headlessly, paced against the mixer's real-time model,
	// polling the ducker's goroutine-safe gain reader from outside the
	// mix goroutine (the whole point of the atomic readers).
	minGain := 0.0
	buf := make([]float64, rate/10*ch) // 100 ms
	for i := 0; i < 15; i++ {
		mx.Fill(buf)
		if g := duck.GainDB(); g < minGain {
			minGain = g
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.Less(t, minGain, -6.0, "the bed must have ducked while the voice track played")
}
