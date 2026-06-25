package mutations_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

func TestAudioDurationAndFrames(t *testing.T) {
	a := mutations.Audio{Data: make([]float64, 96), SampleRate: consts.SampleRate48000, Channels: 2}
	assert.Equal(t, 48, a.Frames())
	assert.Equal(t, time.Millisecond, a.Duration())
}

func TestAudioDurationZeroForEmpty(t *testing.T) {
	assert.Equal(t, time.Duration(0), mutations.Audio{}.Duration())
	assert.Equal(t, 0, mutations.Audio{}.Frames())
}

func TestAudioClone(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 2, 3}, SampleRate: consts.SampleRate48000, Channels: 1}
	b := a.Clone()
	b.Data[0] = 99
	assert.Equal(t, 1.0, a.Data[0], "clone must not alias original")
	assert.Equal(t, 99.0, b.Data[0])
	assert.Equal(t, a.SampleRate, b.SampleRate)
	assert.Equal(t, a.Channels, b.Channels)
}

func TestAudioApplyGainChainable(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 1, 1, 1}, SampleRate: consts.SampleRate48000, Channels: 1}
	result := a.ApplyGain(0.5)
	assert.Equal(t, []float64{0.5, 0.5, 0.5, 0.5}, a.Data, "in place")
	assert.Equal(t, a.Data, result.Data, "chain returns same backing data")
}

func TestAudioApplyGainEnvelopeFadesWithoutFormatArgs(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 1, 1, 1}, SampleRate: consts.SampleRate48000, Channels: 1}
	env := []mutations.GainPoint{{At: 0, Value: 0}, {At: mutations.FramesToDuration(3, consts.SampleRate48000), Value: 1}}
	a.ApplyGainEnvelope(env, 0)
	// Slope 0→1 across 3 frames: ~0.0, ~0.33, ~0.67, ~1.0
	assert.InDelta(t, 0.0, a.Data[0], 1e-9)
	assert.InDelta(t, 1.0, a.Data[3], 1e-4)
}

func TestAudioApplyFadeInAndOut(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 1, 1, 1, 1, 1, 1, 1}, SampleRate: consts.SampleRate48000, Channels: 1}
	a.ApplyFadeIn(mutations.FramesToDuration(4, consts.SampleRate48000))
	assert.InDelta(t, 0.0, a.Data[0], 1e-9)
	// ns quantisation at 48 kHz drifts by ~20 μs across 4 frames; 1e-4
	// is loose enough to accommodate it while still proving the fade ramped.
	assert.InDelta(t, 1.0, a.Data[4], 1e-4, "unity after fade-in window")

	b := mutations.Audio{Data: []float64{1, 1, 1, 1}, SampleRate: consts.SampleRate48000, Channels: 1}
	b.ApplyFadeOut(0, mutations.FramesToDuration(3, consts.SampleRate48000))
	assert.InDelta(t, 1.0, b.Data[0], 1e-9)
	assert.InDelta(t, 0.0, b.Data[3], 1e-4)
}

func TestAudioApplyCustomGain(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 1, 1}, SampleRate: 1000, Channels: 1}
	a.ApplyCustomGain(func(t time.Duration) float64 {
		return float64(t.Milliseconds())
	}, 0)
	assert.Equal(t, []float64{0, 1, 2}, a.Data)
}

func TestAudioApplySaturator(t *testing.T) {
	a := mutations.Audio{Data: []float64{2.0, -2.0, 0.5}, SampleRate: consts.SampleRate48000, Channels: 1}
	a.ApplySaturator(mutations.HardClip)
	assert.Equal(t, []float64{1.0, -1.0, 0.5}, a.Data)
}

func TestAudioApplyEffectRunsProcessor(t *testing.T) {
	// Impulse + echo: verify the effect runs through the method.
	a := mutations.Audio{Data: make([]float64, 20), SampleRate: consts.SampleRate48000, Channels: 1}
	a.Data[0] = 1.0
	echo := mutations.NewEcho(mutations.FramesToDuration(4, consts.SampleRate48000), consts.SampleRate48000, 1, 0.5, 1.0)
	a.ApplyEffect(echo)
	assert.InDelta(t, 0.5, a.Data[4], 1e-9, "first echo")
}

func TestAudioApplyEffectsChain(t *testing.T) {
	a := mutations.Audio{Data: []float64{1, 1, 1, 1}, SampleRate: consts.SampleRate48000, Channels: 1}
	// Two nil-safe processors: ApplyGain isn't a Processor, so use real ones.
	echo := mutations.NewEcho(mutations.FramesToDuration(2, consts.SampleRate48000), consts.SampleRate48000, 1, 0.5, 0)
	a.ApplyEffects(echo, nil) // nil should be skipped
	// Echo 100% dry at wet=0 leaves samples unchanged.
	assert.Equal(t, []float64{1, 1, 1, 1}, a.Data)
}

func TestAudioCrossfadeLoopReturnsNewBuffer(t *testing.T) {
	a := mutations.Audio{Data: make([]float64, 20), SampleRate: consts.SampleRate48000, Channels: 1}
	for i := range a.Data {
		a.Data[i] = float64(i)
	}
	out := a.CrossfadeLoop(mutations.FramesToDuration(4, consts.SampleRate48000))

	assert.Len(t, out.Data, 16, "output shorter by fade frames")
	assert.Equal(t, consts.SampleRate48000, out.SampleRate)
	assert.Equal(t, 1, out.Channels)
	// Receiver untouched.
	assert.Equal(t, 20, len(a.Data))
	assert.Equal(t, 19.0, a.Data[19])
}

func TestAudioRenderWithEffectsReturnsNewBuffer(t *testing.T) {
	a := mutations.Audio{Data: make([]float64, 4), SampleRate: consts.SampleRate48000, Channels: 1}
	a.Data[0] = 1.0
	echo := mutations.NewEcho(mutations.FramesToDuration(4, consts.SampleRate48000), consts.SampleRate48000, 1, 0.5, 1.0)

	tail := mutations.FramesToDuration(16, consts.SampleRate48000)
	out := a.RenderWithEffects([]mutations.Processor{echo}, tail)

	require.Len(t, out.Data, 20, "original + tail")
	assert.InDelta(t, 1.0, out.Data[0], 1e-9)
	assert.InDelta(t, 0.5, out.Data[4], 1e-9)
	assert.InDelta(t, 0.25, out.Data[8], 1e-9)
	// Receiver untouched.
	assert.Len(t, a.Data, 4)
}
