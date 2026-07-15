//go:build cgo && rnnoise_strict

package network

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bitEq(t *testing.T, ctx string, frame, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "%s frame %d [%d]: C=%v (0x%08x) Go=%v (0x%08x)", ctx, frame, i, c, cb, g, gb)
}

// featureFrames yields NBFeatures (65) vectors in a plausible range
// (log-energy-ish features are roughly [-12, 12]; DCT coeffs smaller).
// The wide random range is more stringent than real features.
func featureFrames(n int) [][]float32 {
	r := rand.New(rand.NewSource(4242))
	out := make([][]float32, n)
	for f := 0; f < n; f++ {
		v := make([]float32, 65)
		for i := range v {
			switch {
			case i < 32:
				v[i] = float32(r.Float64()*16 - 8)
			case i < 64:
				v[i] = float32(r.Float64()*4 - 2)
			default:
				v[i] = float32(r.Float64()*2 - 1)
			}
		}
		out[f] = v
	}
	return out
}

// TestComputeRnnParity drives both networks recurrently over many frames
// (so the conv and GRU states accumulate) and compares the 32 gains and
// the VAD probability bit-for-bit each frame.
func TestComputeRnnParity(t *testing.T) {
	c := newCRnn()
	g := rnnoise.NewRnnState()

	for f, in := range featureFrames(60) {
		cg, cv := c.step(in)
		gg, gv := g.Step(in)
		require.Len(t, gg, len(cg))
		for i := range cg {
			bitEq(t, "gain", f, i, cg[i], gg[i])
		}
		bitEq(t, "vad", f, 0, cv, gv)
	}
}
