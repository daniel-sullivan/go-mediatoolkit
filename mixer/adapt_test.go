package mixer

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/timeline"
)

func clip(t *testing.T, samples []float64, rate, chans int) *timeline.CachedClip {
	t.Helper()
	c, err := timeline.LoadClipFromPCM(samples, rate, chans)
	require.NoError(t, err)
	return c
}

func TestAdaptSourceNoOpIfMatching(t *testing.T) {
	c := clip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate48000, 1)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, adapted.SampleRate())
	assert.Equal(t, 1, adapted.Channels())
}

func TestMonoToStereoDuplicatesChannels(t *testing.T) {
	c := clip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate48000, 2)
	require.NoError(t, err)
	assert.Equal(t, 2, adapted.Channels())

	dst := make([]float64, 8)
	n, err := adapted.Pull(dst)
	assert.Equal(t, 8, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{1, 1, 2, 2, 3, 3, 4, 4}, dst)
}

func TestStereoToMonoAverages(t *testing.T) {
	// Frames: (1, 3), (2, 4), (0.5, -0.5).
	c := clip(t, []float64{1, 3, 2, 4, 0.5, -0.5}, consts.SampleRate48000, 2)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate48000, 1)
	require.NoError(t, err)
	assert.Equal(t, 1, adapted.Channels())

	dst := make([]float64, 3)
	n, err := adapted.Pull(dst)
	assert.Equal(t, 3, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{2, 3, 0}, dst)
}

func TestUnsupportedChannelConversion(t *testing.T) {
	c := clip(t, make([]float64, 15), consts.SampleRate48000, 3) // 5 frames of 3ch
	_, err := adaptSource(c.Playhead(), consts.SampleRate48000, 2)
	assert.ErrorIs(t, err, ErrUnsupportedChannels)
}

func TestResampleMatchingRateReturnsAsIs(t *testing.T) {
	c := clip(t, []float64{1, 2, 3}, consts.SampleRate48000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate48000, 1)
	require.NoError(t, err)
	_, isResampled := adapted.(*resampledSource)
	assert.False(t, isResampled)
}

func TestResampleUpsamplesProducesMoreFrames(t *testing.T) {
	// 100 frames at 24kHz → adapt to 48kHz → expect roughly 200 frames out.
	samples := make([]float64, 100)
	for i := range samples {
		samples[i] = float64(i) / 100.0
	}
	c := clip(t, samples, consts.SampleRate24000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate48000, 1)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, adapted.SampleRate())

	var total int
	dst := make([]float64, 64)
	for {
		n, err := adapted.Pull(dst)
		total += n
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	// Sinc resamplers trim filter transients; expect within 2% of 2x.
	assert.InDelta(t, 200, total, 10, "total frames produced")
}

func TestResampleConsumesAllInputAcrossManyPulls(t *testing.T) {
	// Regression: when dst is smaller than what the converter would
	// produce from one resampleInputChunk's worth of source samples,
	// the resampler must keep the unconsumed input for the next call.
	// If it discards the leftover and re-Pulls a fresh chunk every
	// time, the source playhead advances faster than the resampler
	// actually consumed and audio plays back at the wrong speed —
	// dramatically so for downsampling (e.g. 48k→44.1k inside the
	// mixer when a Bluetooth output negotiates 44100Hz).
	const inFrames = 16384
	samples := make([]float64, inFrames)
	for i := range samples {
		samples[i] = float64(i) / float64(inFrames)
	}
	c := clip(t, samples, consts.SampleRate48000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate24000, 1)
	require.NoError(t, err)

	dst := make([]float64, 64) // small chunks force many Pulls
	var total int
	for {
		n, err := adapted.Pull(dst)
		total += n
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	// 2:1 downsample → expect ~8192 frames out (sinc filter trims a
	// few transient samples at start/end).
	assert.InDelta(t, inFrames/2, total, 16, "total frames produced after downsample")
}

func TestResampleNonIntegerRatioConsumesAllInput(t *testing.T) {
	// Same regression as above but at the awkward 48k→44.1k ratio
	// that triggered it in the wild — non-integer ratios make the
	// per-Pull truncation more pronounced.
	const inFrames = 16384
	samples := make([]float64, inFrames)
	for i := range samples {
		samples[i] = float64(i) / float64(inFrames)
	}
	c := clip(t, samples, consts.SampleRate48000, 1)
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate44100, 1)
	require.NoError(t, err)

	dst := make([]float64, 480)
	var total int
	for {
		n, err := adapted.Pull(dst)
		total += n
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
	}
	expected := inFrames * 44100 / 48000 // 15052
	assert.InDelta(t, expected, total, 32, "total frames produced after 48k→44.1k")
}

func TestResampleDurationScales(t *testing.T) {
	c := clip(t, make([]float64, consts.SampleRate48000), consts.SampleRate48000, 1) // 1s
	adapted, err := adaptSource(c.Playhead(), consts.SampleRate24000, 1)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate24000, adapted.SampleRate())
	assert.Equal(t, 500*time.Millisecond, adapted.Duration())
}
