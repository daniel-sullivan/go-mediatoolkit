package mutations_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/mutations"
)

func TestEchoImpulseRepeats(t *testing.T) {
	// 4-sample delay at 48 kHz mono, feedback 0.5, fully wet.
	// Delay = 4 samples exactly: FramesToDuration(4, consts.SampleRate48000) =
	// 83.33μs; feed that duration so the ring is 4 samples long.
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	e := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0)

	buf := make([]float64, 20)
	buf[0] = 1.0 // impulse
	e.Process(buf)

	// Expect echoes at indices 4, 8, 12, 16 with amplitudes
	// 0.5, 0.25, 0.125, 0.0625 (geometric decay by feedback).
	assert.InDelta(t, 1.0, buf[0], 1e-9, "original sample")
	assert.InDelta(t, 0.5, buf[4], 1e-9, "first echo")
	assert.InDelta(t, 0.25, buf[8], 1e-9, "second echo")
	assert.InDelta(t, 0.125, buf[12], 1e-9, "third echo")
	assert.InDelta(t, 0.0625, buf[16], 1e-9, "fourth echo")
	// Interstitial samples should be silent.
	for _, i := range []int{1, 2, 3, 5, 6, 7} {
		assert.Equal(t, 0.0, buf[i], "interstitial %d", i)
	}
}

func TestEchoDryWetMix(t *testing.T) {
	delay := mutations.FramesToDuration(2, consts.SampleRate48000)
	e := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 0.5) // 50% wet

	buf := []float64{1, 0, 0, 0, 0, 0}
	e.Process(buf)

	// buf[0]: x=1, delayed=0, y=1. out = 0.5*1 + 0.5*1 = 1.0
	// buf[2]: x=0, delayed=1 (from buf[0] write), y=0.5. out=0.5*0 + 0.5*0.5 = 0.25
	assert.InDelta(t, 1.0, buf[0], 1e-9)
	assert.InDelta(t, 0.25, buf[2], 1e-9)
}

func TestEchoClampsFeedback(t *testing.T) {
	// feedback of 1.5 should clamp to 0.99, still stable (not 1.0
	// which would be self-sustaining).
	delay := mutations.FramesToDuration(2, consts.SampleRate48000)
	e := mutations.NewEcho(delay, consts.SampleRate48000, 1, 1.5, 1.0)

	buf := make([]float64, 200)
	buf[0] = 1.0
	e.Process(buf)

	// Bounded — no blow-up.
	for i, v := range buf {
		assert.False(t, math.IsNaN(v), "NaN at %d", i)
		assert.Less(t, math.Abs(v), 2.0, "magnitude bound at %d", i)
	}
}

func TestEchoZeroDelayNoOp(t *testing.T) {
	e := mutations.NewEcho(0, consts.SampleRate48000, 1, 0.5, 1.0)
	buf := []float64{0.1, 0.2, 0.3}
	orig := append([]float64(nil), buf...)
	e.Process(buf)
	assert.Equal(t, orig, buf, "zero delay should pass through")
}

func TestEchoReset(t *testing.T) {
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	e := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0)

	buf := make([]float64, 20)
	buf[0] = 1.0
	e.Process(buf)
	require.NotZero(t, buf[4])

	e.Reset()

	buf2 := make([]float64, 20)
	e.Process(buf2)
	for i, v := range buf2 {
		assert.Equal(t, 0.0, v, "post-reset sample %d should be zero", i)
	}
}

func TestEchoMultiChannelDelayIsPerChannel(t *testing.T) {
	// Stereo 4-frame delay: an impulse on L at frame 0 should
	// echo to L at frame 4, not to R.
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	e := mutations.NewEcho(delay, consts.SampleRate48000, 2, 0.5, 1.0)

	buf := make([]float64, 20) // 10 stereo frames
	buf[0] = 1.0               // L impulse
	e.Process(buf)

	// L at frame 4 (sample index 8) should be 0.5; R at frame 4 (9) should be 0.
	assert.InDelta(t, 0.5, buf[8], 1e-9, "L echo")
	assert.InDelta(t, 0.0, buf[9], 1e-9, "R should not pick up L's echo")
}
