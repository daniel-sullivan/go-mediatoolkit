package mutations_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/mutations"
)

func TestRenderBufferLengthIncludesTail(t *testing.T) {
	input := make([]float64, 100)
	out := mutations.RenderBuffer(input, nil, 50, 1)
	assert.Len(t, out, 150)
}

func TestRenderBufferStereoTail(t *testing.T) {
	input := make([]float64, 10) // 5 stereo frames
	out := mutations.RenderBuffer(input, nil, 8, 2)
	assert.Len(t, out, 10+8*2, "tail frames * channels")
}

func TestRenderBufferCapturesEchoTail(t *testing.T) {
	// Impulse + echo: the rendered buffer's tail region should hold
	// the decayed copies that RT EffectSource without WithTail would
	// have dropped.
	input := make([]float64, 4)
	input[0] = 1.0
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	echo := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0)

	out := mutations.RenderBuffer(input, []mutations.Processor{echo}, 16, 1)
	require.Len(t, out, 20)
	assert.InDelta(t, 1.0, out[0], 1e-9)
	assert.InDelta(t, 0.5, out[4], 1e-9)
	assert.InDelta(t, 0.25, out[8], 1e-9)
	assert.InDelta(t, 0.125, out[12], 1e-9)
	assert.InDelta(t, 0.0625, out[16], 1e-9)
}

func TestRenderBufferEqualsChunkedProcessing(t *testing.T) {
	// Ensure the offline render matches chunked processing — the
	// processors are stateful so this is a real invariant worth
	// pinning.
	input := make([]float64, 200)
	for i := range input {
		input[i] = float64(i%7) * 0.1
	}
	delay := mutations.FramesToDuration(5, consts.SampleRate48000)

	a := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.3, 0.5)
	offline := mutations.RenderBuffer(input, []mutations.Processor{a}, 50, 1)

	b := mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.3, 0.5)
	chunked := make([]float64, 250)
	copy(chunked, input)
	const chunk = 11
	for i := 0; i < len(chunked); i += chunk {
		end := i + chunk
		if end > len(chunked) {
			end = len(chunked)
		}
		b.Process(chunked[i:end])
	}

	for i := range offline {
		assert.InDelta(t, chunked[i], offline[i], 1e-9, "sample %d", i)
	}
}

func TestRenderBufferNilChainIsCopyPlusSilence(t *testing.T) {
	input := []float64{0.1, 0.2, 0.3}
	out := mutations.RenderBuffer(input, nil, 5, 1)
	assert.Equal(t, []float64{0.1, 0.2, 0.3, 0, 0, 0, 0, 0}, out)
}

func TestRenderBufferNegativeTailClamps(t *testing.T) {
	input := []float64{1, 2, 3}
	out := mutations.RenderBuffer(input, nil, -10, 1)
	assert.Equal(t, input, out)
}
