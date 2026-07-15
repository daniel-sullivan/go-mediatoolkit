//go:build cgo && rnnoise_strict

package pitch

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pitchBufSize = 1728 // PITCH_MAX_PERIOD + PITCH_FRAME_SIZE
	maxPeriod    = 768
	minPeriod    = 60
	frameSize    = 960
)

func bitEq(t *testing.T, ctx string, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "%s[%d]: C=%v (0x%08x) Go=%v (0x%08x)", ctx, i, c, cb, g, gb)
}

// voiced builds a PITCH_BUF_SIZE buffer with a clear fundamental period
// (plus harmonics and a little noise) so the pitch search and doubling
// logic take their voiced-path branches.
func voiced(period float64, seed int64) []float32 {
	buf := make([]float32, pitchBufSize)
	r := rand.New(rand.NewSource(seed))
	f0 := 48000.0 / period
	for i := range buf {
		v := 8000*math.Sin(2*math.Pi*f0*float64(i)/48000) +
			3000*math.Sin(2*math.Pi*2*f0*float64(i)/48000) +
			1500*math.Sin(2*math.Pi*3*f0*float64(i)/48000)
		v += float64(r.Intn(600) - 300)
		buf[i] = float32(v)
	}
	return buf
}

func TestPitchDownsampleParity(t *testing.T) {
	for _, p := range []float64{80, 150, 220, 400, 600} {
		buf := voiced(p, int64(p))
		c := cPitchDownsample(buf, pitchBufSize)
		g := rnnoise.PitchDownsample(buf, pitchBufSize)
		require.Len(t, g, len(c))
		for i := range c {
			bitEq(t, "xlp", i, c[i], g[i])
		}
	}
}

// TestPitchChainParity runs the full downsample->search->remove_doubling
// sequence exactly as rnn_compute_frame_features does, comparing the
// downsampled buffer, the chosen pitch index, and the gain bit-for-bit.
func TestPitchChainParity(t *testing.T) {
	cases := []struct {
		period     float64
		prevPeriod int
		prevGain   float32
	}{
		{80, 0, 0},
		{150, 150, 0.6},
		{150, 151, 0.9}, // abs(T1-prev)<=1 branch
		{220, 218, 0.5}, // abs(T1-prev)<=2 branch
		{400, 100, 0.3},
		{600, 600, 0.95},
		{95, 300, 0.1}, // short period (bias branches)
	}
	const maxPitch = maxPeriod - 3*minPeriod // 588
	for _, tc := range cases {
		buf := voiced(tc.period, int64(tc.period)*7+int64(tc.prevPeriod))

		cxlp := cPitchDownsample(buf, pitchBufSize)
		gxlp := rnnoise.PitchDownsample(buf, pitchBufSize)
		require.Len(t, gxlp, len(cxlp))
		for i := range cxlp {
			bitEq(t, "xlp", i, cxlp[i], gxlp[i])
		}

		cidx := cPitchSearch(cxlp, maxPeriod>>1, frameSize, maxPitch)
		gidx := rnnoise.PitchSearch(gxlp[maxPeriod>>1:], gxlp, frameSize, maxPitch)
		require.Equalf(t, cidx, gidx, "pitch_search index (period %v)", tc.period)

		cidx = maxPeriod - cidx
		gidx = maxPeriod - gidx

		cgain, cIdxOut := cRemoveDoubling(cxlp, maxPeriod, minPeriod, frameSize, cidx, tc.prevPeriod, tc.prevGain)
		ggain, gIdxOut := rnnoise.RemoveDoubling(gxlp, maxPeriod, minPeriod, frameSize, gidx, tc.prevPeriod, tc.prevGain)

		require.Equalf(t, cIdxOut, gIdxOut, "remove_doubling T0 (period %v)", tc.period)
		bitEq(t, "gain", int(tc.period), cgain, ggain)
	}
}
