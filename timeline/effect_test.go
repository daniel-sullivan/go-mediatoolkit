package timeline

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/mutations"
)

// countingProcessor asserts EffectSource runs its chain in order and
// observes the Pull output length.
type countingProcessor struct {
	calls     int
	lastN     int
	operation func(samples []float64)
}

func (p *countingProcessor) Process(samples []float64) {
	p.calls++
	p.lastN = len(samples)
	if p.operation != nil {
		p.operation(samples)
	}
}

func (p *countingProcessor) Reset() {
	p.calls = 0
	p.lastN = 0
}

func TestEffectSourceEmptyChainPassThrough(t *testing.T) {
	clip := mustClip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	es := NewEffectSource(clip.Playhead())
	dst := make([]float64, 4)
	n, err := es.Pull(dst)
	assert.Equal(t, 4, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{1, 2, 3, 4}, dst)
}

func TestEffectSourceAppliesChainInOrder(t *testing.T) {
	clip := mustClip(t, []float64{1, 1, 1, 1}, consts.SampleRate48000, 1)
	first := &countingProcessor{operation: func(s []float64) {
		for i := range s {
			s[i] += 1
		}
	}}
	second := &countingProcessor{operation: func(s []float64) {
		for i := range s {
			s[i] *= 2
		}
	}}
	es := NewEffectSource(clip.Playhead(), first, second)

	dst := make([]float64, 4)
	_, _ = es.Pull(dst)

	// Chain result: (1+1) * 2 = 4 for every sample.
	assert.Equal(t, []float64{4, 4, 4, 4}, dst)
	assert.Equal(t, 1, first.calls)
	assert.Equal(t, 1, second.calls)
	assert.Equal(t, 4, first.lastN)
	assert.Equal(t, 4, second.lastN)
}

func TestEffectSourceRunsOnFilledPortionOnly(t *testing.T) {
	clip := mustClip(t, []float64{1, 2}, consts.SampleRate48000, 1)
	seen := &countingProcessor{}
	es := NewEffectSource(clip.Playhead(), seen)

	dst := make([]float64, 5) // larger than clip
	_, _ = es.Pull(dst)
	assert.Equal(t, 2, seen.lastN, "processor should only see the filled portion")
}

func TestEffectSourceForwardsMetadata(t *testing.T) {
	clip := mustClip(t, make([]float64, consts.SampleRate48000), consts.SampleRate48000, 2)
	es := NewEffectSource(clip.Playhead())
	assert.Equal(t, consts.SampleRate48000, es.SampleRate())
	assert.Equal(t, 2, es.Channels())
	assert.Equal(t, clip.Duration(), es.Duration())
	assert.False(t, es.Live())
}

func TestEffectSourceResetClearsAllProcessors(t *testing.T) {
	clip := mustClip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	a := &countingProcessor{}
	b := &countingProcessor{}
	es := NewEffectSource(clip.Playhead(), a, b)

	dst := make([]float64, 4)
	_, _ = es.Pull(dst)
	require.Equal(t, 1, a.calls)
	require.Equal(t, 1, b.calls)

	es.Reset()
	assert.Equal(t, 0, a.calls)
	assert.Equal(t, 0, b.calls)
}

func TestEffectSourceEndToEndWithEcho(t *testing.T) {
	// Impulse in a cached clip, single-tap echo on the wrapper.
	// The echoed sample should appear in a later Pull.
	samples := make([]float64, 20)
	samples[0] = 1.0
	clip := mustClip(t, samples, consts.SampleRate48000, 1)
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	es := NewEffectSource(clip.Playhead(), mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0))

	dst := make([]float64, 20)
	n, _ := es.Pull(dst)
	require.Equal(t, 20, n)
	assert.InDelta(t, 1.0, dst[0], 1e-9, "dry impulse")
	assert.InDelta(t, 0.5, dst[4], 1e-9, "first echo")
	assert.InDelta(t, 0.25, dst[8], 1e-9, "second echo")
	_ = time.Second // keep import usage compatible
}

func TestEffectSourceWithoutTailTruncatesAtSourceEOF(t *testing.T) {
	// Impulse in a 4-sample clip with echo. Without tail, the
	// echoed copy at frame 4 falls outside the source length and
	// should be lost.
	samples := make([]float64, 4)
	samples[0] = 1.0
	clip := mustClip(t, samples, consts.SampleRate48000, 1)
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	es := NewEffectSource(clip.Playhead(), mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0))

	dst := make([]float64, 20)
	n, err := es.Pull(dst)
	assert.Equal(t, 4, n, "no tail → pull stops at source length")
	assert.ErrorIs(t, err, io.EOF)
	// Buffer beyond the source should still be zero (not the echo).
	for i := 4; i < 20; i++ {
		assert.Equal(t, 0.0, dst[i], "frame %d", i)
	}
}

func TestEffectSourceWithTailExtendsPastSourceEOF(t *testing.T) {
	// Same setup but with a 16-frame tail — the echoed samples
	// should now appear.
	samples := make([]float64, 4)
	samples[0] = 1.0
	clip := mustClip(t, samples, consts.SampleRate48000, 1)
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	es := NewEffectSource(clip.Playhead(),
		mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0),
	).WithTail(mutations.FramesToDuration(16, consts.SampleRate48000))

	dst := make([]float64, 20)
	n, err := es.Pull(dst)
	assert.Equal(t, 20, n, "source (4) + tail (16) = 20 samples")
	assert.ErrorIs(t, err, io.EOF)
	assert.InDelta(t, 1.0, dst[0], 1e-9, "dry impulse")
	assert.InDelta(t, 0.5, dst[4], 1e-9, "first echo (in tail region)")
	assert.InDelta(t, 0.25, dst[8], 1e-9, "second echo (in tail region)")
	assert.InDelta(t, 0.125, dst[12], 1e-9, "third echo")
	assert.InDelta(t, 0.0625, dst[16], 1e-9, "fourth echo")
}

func TestEffectSourceTailAcrossMultiplePulls(t *testing.T) {
	// Ensure tail splits cleanly across multiple small Pull calls.
	samples := make([]float64, 4)
	samples[0] = 1.0
	clip := mustClip(t, samples, consts.SampleRate48000, 1)
	delay := mutations.FramesToDuration(4, consts.SampleRate48000)
	es := NewEffectSource(clip.Playhead(),
		mutations.NewEcho(delay, consts.SampleRate48000, 1, 0.5, 1.0),
	).WithTail(mutations.FramesToDuration(12, consts.SampleRate48000))

	var full []float64
	chunk := make([]float64, 5)
	for {
		n, err := es.Pull(chunk)
		full = append(full, chunk[:n]...)
		if err == io.EOF {
			break
		}
	}
	require.Len(t, full, 16, "source (4) + tail (12)")
	assert.InDelta(t, 1.0, full[0], 1e-9)
	assert.InDelta(t, 0.5, full[4], 1e-9)
	assert.InDelta(t, 0.25, full[8], 1e-9)
	assert.InDelta(t, 0.125, full[12], 1e-9)
}

func TestEffectSourceDurationIncludesTail(t *testing.T) {
	clip := mustClip(t, make([]float64, 480), consts.SampleRate48000, 1) // 10ms
	es := NewEffectSource(clip.Playhead(),
		mutations.NewEcho(mutations.FramesToDuration(4, consts.SampleRate48000), consts.SampleRate48000, 1, 0.5, 1.0),
	).WithTail(20 * time.Millisecond)

	// Source 10ms + tail 20ms = 30ms.
	assert.InDelta(t, float64(30*time.Millisecond), float64(es.Duration()), float64(time.Microsecond))
}

func TestEffectSourceTailOnIndefiniteSourceHasNoEffect(t *testing.T) {
	// Timeline has Duration == -1; tail should not appear on
	// the effective Duration (live/indefinite sources don't EOF
	// predictably).
	tl, err := NewTimeline(consts.SampleRate48000, 1)
	require.NoError(t, err)
	defer tl.Close()
	es := NewEffectSource(tl).WithTail(100 * time.Millisecond)
	assert.Equal(t, time.Duration(-1), es.Duration())
}
