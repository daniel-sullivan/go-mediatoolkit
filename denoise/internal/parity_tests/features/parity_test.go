//go:build cgo && rnnoise_strict

package features

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

// frames yields a stream of FrameSize (480) input frames: voiced speech-
// like segments, silence (exercises the E<0.04 clear+return path), and
// noise, so log10, the pitch chain, and the silence branch are all hit.
func frames(n int) [][]float32 {
	const fs = 480
	r := rand.New(rand.NewSource(2026))
	out := make([][]float32, n)
	for f := 0; f < n; f++ {
		fr := make([]float32, fs)
		switch f % 4 {
		case 0, 1: // voiced
			period := 120.0 + float64(f%5)*40
			f0 := 48000.0 / period
			for i := range fr {
				gi := f*fs + i
				fr[i] = float32(9000*math.Sin(2*math.Pi*f0*float64(gi)/48000)+
					3000*math.Sin(2*math.Pi*2*f0*float64(gi)/48000)) +
					float32(r.Intn(400)-200)
			}
		case 2: // near-silence -> silence branch
			for i := range fr {
				fr[i] = float32(r.Intn(3) - 1)
			}
		default: // noise
			for i := range fr {
				fr[i] = float32(r.Intn(30000) - 15000)
			}
		}
		out[f] = fr
	}
	return out
}

func TestComputeFrameFeaturesParity(t *testing.T) {
	cst := newCState()
	gst := rnnoise.NewState()

	for f, in := range frames(40) {
		cf, cex, cep, cexp, csil := cst.computeFeatures(in)
		gf, gsil, gex, gep, gexp := gst.ComputeFrameFeatures(in)

		require.Equalf(t, csil, gsil, "silence flag mismatch at frame %d", f)
		require.Len(t, gf, len(cf))
		for i := range cf {
			bitEq(t, "features", f, i, cf[i], gf[i])
		}
		for i := range cex {
			bitEq(t, "Ex", f, i, cex[i], gex[i])
			bitEq(t, "Ep", f, i, cep[i], gep[i])
			bitEq(t, "Exp", f, i, cexp[i], gexp[i])
		}
	}
}
