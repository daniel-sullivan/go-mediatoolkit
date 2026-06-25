package timeline

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/consts"
)

func TestNewInputSourceValidation(t *testing.T) {
	_, err := NewInputSource(0, 1, 256)
	assert.ErrorIs(t, err, ErrBadSampleRate)
	_, err = NewInputSource(consts.SampleRate48000, 0, 256)
	assert.ErrorIs(t, err, ErrBadChannels)
	_, err = NewInputSource(consts.SampleRate48000, 1, 0)
	assert.ErrorIs(t, err, ErrNegativeStart)
}

func TestInputSourceMetadata(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 2, 512)
	require.NoError(t, err)
	assert.Equal(t, consts.SampleRate48000, s.SampleRate())
	assert.Equal(t, 2, s.Channels())
	assert.Equal(t, time.Duration(-1), s.Duration())
	assert.True(t, s.Live())
}

func TestInputSourceCallbackDeliversToPull(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 1, 256)
	require.NoError(t, err)
	cb := s.Callback()
	cb([]float64{1, 2, 3, 4})

	dst := make([]float64, 6)
	n, err := s.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Equal(t, []float64{1, 2, 3, 4, 0, 0}, dst)
}

func TestInputSourcePartialPullWhenEmpty(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 1, 256)
	require.NoError(t, err)

	dst := make([]float64, 4)
	n, err := s.Pull(dst)
	require.NoError(t, err, "empty live source returns no error, just 0 samples")
	assert.Equal(t, 0, n)
}

func TestInputSourceDropsOnOverflow(t *testing.T) {
	// Ring is 4 frames mono; pushing 10 forces 6 to be dropped.
	s, err := NewInputSource(consts.SampleRate48000, 1, 4)
	require.NoError(t, err)
	cb := s.Callback()
	cb([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	assert.GreaterOrEqual(t, s.Dropped(), uint64(6))
}

func TestInputSourceCloseDrainsThenEOFs(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 1, 256)
	require.NoError(t, err)
	cb := s.Callback()
	cb([]float64{1, 2, 3})

	require.NoError(t, s.Close())

	// Buffered samples still readable post-close.
	dst := make([]float64, 3)
	n, err := s.Pull(dst)
	assert.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, []float64{1, 2, 3}, dst)

	// Ring now empty → EOF.
	n, err = s.Pull(dst)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF)
}

func TestInputSourceCallbackIgnoredAfterClose(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 1, 256)
	require.NoError(t, err)
	cb := s.Callback()

	require.NoError(t, s.Close())
	cb([]float64{1, 2, 3}) // should be a no-op

	dst := make([]float64, 4)
	n, err := s.Pull(dst)
	assert.Equal(t, 0, n)
	assert.ErrorIs(t, err, io.EOF)
}

func TestInputSourceCloseIdempotent(t *testing.T) {
	s, err := NewInputSource(consts.SampleRate48000, 1, 256)
	require.NoError(t, err)
	assert.NoError(t, s.Close())
	assert.NoError(t, s.Close())
}
