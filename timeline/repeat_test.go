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

func TestRepeatEOFBased(t *testing.T) {
	// Natural-length loop: factory returns a 4-sample clip, Repeat
	// restarts on EOF.
	clip := mustClip(t, []float64{1, 2, 3, 4}, consts.SampleRate48000, 1)
	r := Repeat(consts.SampleRate48000, 1, 0, clip.Playhead)

	dst := make([]float64, 10)
	n, err := r.Pull(dst)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, []float64{1, 2, 3, 4, 1, 2, 3, 4, 1, 2}, dst)
}

func TestRepeatExplicitDurationTruncates(t *testing.T) {
	// 6-sample clip, loopDuration = 3 frames → truncates mid-clip.
	clip := mustClip(t, []float64{1, 2, 3, 4, 5, 6}, consts.SampleRate48000, 1)
	r := Repeat(consts.SampleRate48000, 1, mutations.FramesToDuration(3, consts.SampleRate48000), clip.Playhead)

	dst := make([]float64, 9)
	_, _ = r.Pull(dst)
	assert.Equal(t, []float64{1, 2, 3, 1, 2, 3, 1, 2, 3}, dst)
}

func TestRepeatExplicitDurationPadsShort(t *testing.T) {
	// 2-sample clip, loopDuration = 5 frames → pads with 3 silence each iteration.
	clip := mustClip(t, []float64{1, 2}, consts.SampleRate48000, 1)
	r := Repeat(consts.SampleRate48000, 1, mutations.FramesToDuration(5, consts.SampleRate48000), clip.Playhead)

	dst := make([]float64, 10)
	_, _ = r.Pull(dst)
	assert.Equal(t, []float64{1, 2, 0, 0, 0, 1, 2, 0, 0, 0}, dst)
}

func TestRepeatMetadata(t *testing.T) {
	r := Repeat(consts.SampleRate48000, 2, 0, func() Source { return nil })
	assert.Equal(t, consts.SampleRate48000, r.SampleRate())
	assert.Equal(t, 2, r.Channels())
	assert.Equal(t, -1, int(r.Duration())) // indefinite
	assert.False(t, r.Live())
}

func TestRepeatArrangement(t *testing.T) {
	// Factory returns a fresh Timeline on each iteration — the
	// "loop an arrangement" use case.
	clipA := mustClip(t, []float64{1, 1}, consts.SampleRate48000, 1)
	clipB := mustClip(t, []float64{2, 2}, consts.SampleRate48000, 1)

	factory := func() Source {
		tl, err := NewTimeline(consts.SampleRate48000, 1)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = tl.Schedule(Cue{Source: clipA.Playhead()})
		_, _ = tl.Schedule(Cue{
			Source: clipB.Playhead(),
			Start:  mutations.FramesToDuration(3, consts.SampleRate48000),
		})
		return tl
	}

	r := Repeat(consts.SampleRate48000, 1, mutations.FramesToDuration(6, consts.SampleRate48000), factory)
	dst := make([]float64, 12)
	_, _ = r.Pull(dst)
	// One iteration: [1, 1, 0, 2, 2, 0]; repeated once → 12 frames.
	assert.Equal(t, []float64{1, 1, 0, 2, 2, 0, 1, 1, 0, 2, 2, 0}, dst)
}

func TestRepeatNilFactoryEmitsSilence(t *testing.T) {
	// Degenerate factory returning nil — Repeat fills silence.
	r := Repeat(consts.SampleRate48000, 1, 0, func() Source { return nil })
	dst := make([]float64, 5)
	n, err := r.Pull(dst)
	assert.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, []float64{0, 0, 0, 0, 0}, dst)
}

func TestRepeatPartialFrame(t *testing.T) {
	r := Repeat(consts.SampleRate48000, 2, 0, func() Source { return nil })
	dst := []float64{0, 0, 0} // not a multiple of 2
	_, err := r.Pull(dst)
	assert.ErrorIs(t, err, ErrPartialFrame)
}

func TestRepeatPropagatesInnerError(t *testing.T) {
	// Inner source returns a non-EOF error — Repeat surfaces it.
	target := io.ErrUnexpectedEOF
	r := Repeat(consts.SampleRate48000, 1, 0, func() Source {
		return &errSource{rate: consts.SampleRate48000, chans: 1, err: target}
	})
	dst := make([]float64, 4)
	_, err := r.Pull(dst)
	assert.ErrorIs(t, err, target)
}

type errSource struct {
	rate, chans int
	err         error
}

func (s *errSource) Pull([]float64) (int, error) { return 0, s.err }
func (s *errSource) SampleRate() int             { return s.rate }
func (s *errSource) Channels() int               { return s.chans }
func (s *errSource) Duration() time.Duration     { return -1 }
func (s *errSource) Live() bool                  { return false }
