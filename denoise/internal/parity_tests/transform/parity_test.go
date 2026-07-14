//go:build cgo && rnnoise_strict

package transform

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bitEq(t *testing.T, ctx string, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "%s[%d]: C=%v (0x%08x) Go=%v (0x%08x)", ctx, i, c, cb, g, gb)
}

// signals returns a spread of WindowSize (960) real inputs.
func signals() map[string][]float32 {
	n := rnnoise.WindowSize
	out := map[string][]float32{}

	impulse := make([]float32, n)
	impulse[0] = 32768
	impulse[123] = -12000
	out["impulse"] = impulse

	tone := make([]float32, n)
	for i := range tone {
		tone[i] = float32(20000 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	out["tone440"] = tone

	multi := make([]float32, n)
	for i := range multi {
		multi[i] = float32(8000*math.Sin(2*math.Pi*300*float64(i)/48000) +
			5000*math.Sin(2*math.Pi*3100*float64(i)/48000))
	}
	out["multitone"] = multi

	r := rand.New(rand.NewSource(7))
	noise := make([]float32, n)
	for i := range noise {
		noise[i] = float32(r.Intn(65536) - 32768)
	}
	out["noise"] = noise

	return out
}

func TestForwardTransformParity(t *testing.T) {
	for name, in := range signals() {
		t.Run(name, func(t *testing.T) {
			cr, ci := cForwardTransform(in, rnnoise.FreqSize)
			gr, gi := rnnoise.ForwardTransform(in)
			require.Len(t, gr, len(cr))
			for i := range cr {
				bitEq(t, "fft.r", i, cr[i], gr[i])
				bitEq(t, "fft.i", i, ci[i], gi[i])
			}
		})
	}
}

func TestApplyWindowParity(t *testing.T) {
	for name, in := range signals() {
		t.Run(name, func(t *testing.T) {
			c := cApplyWindow(in)
			g := make([]float32, len(in))
			copy(g, in)
			rnnoise.ApplyWindow(g)
			for i := range c {
				bitEq(t, "window", i, c[i], g[i])
			}
		})
	}
}

func TestInverseTransformParity(t *testing.T) {
	// Feed the forward transform of each signal back through the inverse
	// (as the real pipeline does), exercising the conjugate-mirror path.
	for name, in := range signals() {
		t.Run(name, func(t *testing.T) {
			fr, fi := rnnoise.ForwardTransform(in)
			c := cInverseTransform(fr, fi, rnnoise.WindowSize)
			g := rnnoise.InverseTransform(fr, fi)
			require.Len(t, g, len(c))
			for i := range c {
				bitEq(t, "ifft", i, c[i], g[i])
			}
		})
	}
}
