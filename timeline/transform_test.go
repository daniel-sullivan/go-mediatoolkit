package timeline

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
)

// These tests cover the timeline-side Transform wrapper. The envelope
// algorithm itself is tested in mutations/gain_test.go — this file
// only verifies that Transform.apply delegates correctly.

func TestTransformApplyZeroIsNoOp(t *testing.T) {
	samples := []float64{1, -1, 0.5, -0.5}
	orig := append([]float64(nil), samples...)
	var tr Transform
	tr.apply(samples, 0, 2, consts.SampleRate48000)
	assert.Equal(t, orig, samples)
}

func TestTransformApplyGain(t *testing.T) {
	tr := Transform{Gain: []EnvelopePoint{{At: 0, Value: 0.5}}}
	samples := []float64{1, -1, 0.4, -0.4}
	tr.apply(samples, 0, 2, consts.SampleRate48000)
	assert.Equal(t, []float64{0.5, -0.5, 0.2, -0.2}, samples)
}

func TestTransformApplyRespectsCueElapsed(t *testing.T) {
	// Envelope ramps 0→1 over 10ms. With cueElapsed = 10ms the
	// entire buffer should see unity gain regardless of frame index.
	tr := Transform{Gain: []EnvelopePoint{
		{At: 0, Value: 0},
		{At: 10 * time.Millisecond, Value: 1},
	}}
	samples := []float64{1, 1, 1, 1}
	tr.apply(samples, 10*time.Millisecond, 1, consts.SampleRate48000)
	for i, v := range samples {
		assert.InDelta(t, 1.0, v, 1e-9, "sample %d", i)
	}
}

func TestTransformGainFuncAlone(t *testing.T) {
	tr := Transform{GainFunc: func(time.Duration) float64 { return 0.25 }}
	samples := []float64{1, 1, 1, 1}
	tr.apply(samples, 0, 1, consts.SampleRate48000)
	assert.Equal(t, []float64{0.25, 0.25, 0.25, 0.25}, samples)
}

func TestTransformGainFuncStacksWithEnvelope(t *testing.T) {
	// Envelope = constant 0.5; GainFunc = constant 0.5. Output
	// should be 0.25 (they multiply).
	tr := Transform{
		Gain:     []EnvelopePoint{{At: 0, Value: 0.5}},
		GainFunc: func(time.Duration) float64 { return 0.5 },
	}
	samples := []float64{1, 1, 1, 1}
	tr.apply(samples, 0, 1, consts.SampleRate48000)
	for i, v := range samples {
		assert.InDelta(t, 0.25, v, 1e-9, "sample %d", i)
	}
}
