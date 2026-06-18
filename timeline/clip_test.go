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

func TestMustCacheReturnsClip(t *testing.T) {
	audio := mutations.Audio{Data: []float64{1, 2, 3}, SampleRate: consts.SampleRate48000, Channels: 1}
	c := MustCacheClip(audio)
	require.NotNil(t, c)
	assert.Equal(t, int64(3), c.Frames())
}

func TestMustCachePanicsOnBadInput(t *testing.T) {
	assert.Panics(t, func() {
		MustCacheClip(mutations.Audio{Data: []float64{1}, SampleRate: 0, Channels: 1})
	})
}

func TestLoadClipFromPCMValidation(t *testing.T) {
	_, err := LoadClipFromPCM([]float64{0, 0}, 0, 1)
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = LoadClipFromPCM([]float64{0, 0}, consts.SampleRate48000, 0)
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestLoadClipFromPCMCopies(t *testing.T) {
	src := []float64{1, 2, 3, 4}
	clip, err := LoadClipFromPCM(src, consts.SampleRate48000, 1)
	require.NoError(t, err)
	src[0] = 99
	ph := clip.Playhead()
	dst := make([]float64, 4)
	n, _ := ph.Pull(dst)
	require.Equal(t, 4, n)
	assert.Equal(t, []float64{1, 2, 3, 4}, dst, "clip should not alias caller buffer")
}

func TestCachedClipMetadata(t *testing.T) {
	clip, err := LoadClipFromPCM(make([]float64, 96), consts.SampleRate48000, 2)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, clip.SampleRate())
	assert.Equal(t, 2, clip.Channels())
	assert.Equal(t, int64(48), clip.Frames())
	assert.Equal(t, time.Millisecond, clip.Duration())
}

func TestPlayheadPullAndEOF(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3, 4, 5}, consts.SampleRate48000, 1)
	require.NoError(t, err)
	ph := clip.Playhead()

	dst := make([]float64, 3)
	n, err := ph.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []float64{1, 2, 3}, dst)

	dst = make([]float64, 3)
	n, err = ph.Pull(dst)
	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, io.EOF, "partial read with EOF is valid")
	assert.Equal(t, []float64{4, 5, 0}, dst)

	n, err = ph.Pull(dst)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF)
}

func TestMultiplePlayheadsAreIndependent(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	require.NoError(t, err)
	a := clip.Playhead()
	b := clip.Playhead()

	ba := make([]float64, 2)
	n, _ := a.Pull(ba)
	require.Equal(t, 2, n)

	bb := make([]float64, 4)
	n, err = b.Pull(bb)
	assert.Equal(t, 4, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{1, 2, 3, 4}, bb)

	// a is at position 2 — pulling now returns the rest.
	ba = make([]float64, 4)
	n, err = a.Pull(ba)
	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{3, 4, 0, 0}, ba)
}

func TestPlayheadSeek(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3, 4, 5, 6, 7, 8}, consts.SampleRate48000, 2)
	require.NoError(t, err)
	ph := clip.Playhead()
	sk := ph.(Seekable)

	require.NoError(t, sk.Seek(2)) // skip 2 stereo frames = 4 samples

	dst := make([]float64, 4)
	n, err := ph.Pull(dst)
	assert.Equal(t, 4, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{5, 6, 7, 8}, dst)
}

func TestPlayheadSeekPastEnd(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	require.NoError(t, err)
	err = clip.Playhead().(Seekable).Seek(100)
	assert.ErrorIs(t, err, io.EOF)
}

func TestPlayheadSeekRewind(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3, 4, 5, 6}, consts.SampleRate48000, 1)
	require.NoError(t, err)
	ph := clip.Playhead()
	sk := ph.(Seekable)

	require.NoError(t, sk.Seek(4)) // cursor at frame 4 ({5, 6} remain)

	dst := make([]float64, 2)
	n, _ := ph.Pull(dst)
	require.Equal(t, 2, n)
	assert.Equal(t, []float64{5, 6}, dst)

	// Rewind by 3 frames from the end → cursor at frame 3 ({4, 5, 6} remain).
	require.NoError(t, sk.Seek(-3))
	dst = make([]float64, 3)
	n, err = ph.Pull(dst)
	assert.Equal(t, 3, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{4, 5, 6}, dst)
}

func TestPlayheadSeekRewindPastStartClamps(t *testing.T) {
	clip, err := LoadClipFromPCM([]float64{1, 2, 3}, consts.SampleRate48000, 1)
	require.NoError(t, err)
	ph := clip.Playhead()
	sk := ph.(Seekable)

	require.NoError(t, sk.Seek(2))
	require.NoError(t, sk.Seek(-100)) // rewind past start → clamp to 0

	dst := make([]float64, 3)
	n, err := ph.Pull(dst)
	assert.Equal(t, 3, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{1, 2, 3}, dst)
}

// fakeDecoder feeds fixed samples to OpenClip without needing a real codec.
type fakeDecoder struct {
	samples []float64
	rate    int
	chans   int
	cursor  int
}

func (d *fakeDecoder) Read(buf []float64) (mutations.Audio, error) {
	remaining := len(d.samples) - d.cursor
	if remaining <= 0 {
		return mutations.Audio{Data: buf[:0], SampleRate: d.rate, Channels: d.chans}, io.EOF
	}
	n := len(buf)
	if n > remaining {
		n = remaining
	}
	copy(buf, d.samples[d.cursor:d.cursor+n])
	d.cursor += n
	audio := mutations.Audio{Data: buf[:n], SampleRate: d.rate, Channels: d.chans}
	if d.cursor >= len(d.samples) {
		return audio, io.EOF
	}
	return audio, nil
}

func (d *fakeDecoder) Channels() int   { return d.chans }
func (d *fakeDecoder) SampleRate() int { return d.rate }

func TestOpenClipStreams(t *testing.T) {
	dec := &fakeDecoder{samples: []float64{1, 2, 3, 4, 5}, rate: consts.SampleRate48000, chans: 1}
	sc, err := OpenClip(dec, 100*time.Millisecond)
	require.NoError(t, err)

	assert.Equal(t, consts.SampleRate48000, sc.SampleRate())
	assert.Equal(t, 1, sc.Channels())
	assert.Equal(t, 100*time.Millisecond, sc.Duration())
	assert.False(t, sc.Live())

	dst := make([]float64, 3)
	n, err := sc.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []float64{1, 2, 3}, dst)

	n, err = sc.Pull(dst)
	assert.Equal(t, 2, n)
	assert.ErrorIs(t, err, io.EOF)
}

func TestOpenClipValidation(t *testing.T) {
	_, err := OpenClip(nil, -1)
	assert.ErrorIs(t, err, ErrNilSource)
	_, err = OpenClip(&fakeDecoder{rate: 0, chans: 1}, -1)
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = OpenClip(&fakeDecoder{rate: consts.SampleRate48000, chans: 0}, -1)
	assert.ErrorIs(t, err, ErrBadChannels)
}

func TestLoadClipDrainsDecoder(t *testing.T) {
	dec := &fakeDecoder{samples: []float64{1, 2, 3, 4, 5, 6, 7}, rate: consts.SampleRate48000, chans: 1}
	clip, err := LoadClip(dec)
	require.NoError(t, err)
	assert.Equal(t, int64(7), clip.Frames())

	ph := clip.Playhead()
	dst := make([]float64, 7)
	n, err := ph.Pull(dst)
	assert.Equal(t, 7, n)
	assert.ErrorIs(t, err, io.EOF)
	assert.Equal(t, []float64{1, 2, 3, 4, 5, 6, 7}, dst)
}
