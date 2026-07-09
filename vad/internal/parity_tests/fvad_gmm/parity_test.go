//go:build cgo

package fvad_gmm

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// TestGaussianProbabilityParity sweeps dense grids over the Q domains
// the GMM feeds the kernel — input in Q4 (the band log-energies land
// in roughly 0..2000, but the update loop can hand it any int16), mean
// in Q7 (model tables span ~640..12800 and drift within int16), std in
// Q7 (clamped to ≥384 live, but SetMode/adaptation edges are grid-swept
// anyway, including the degenerate std == 0 division-guard path) —
// plus a PRNG cross-product over full int16 ranges.
func TestGaussianProbabilityParity(t *testing.T) {
	inputs := []int16{-32768, -4096, -1, 0, 1, 100, 500, 1000, 2000, 4000, 8191, 32767}
	for v := int16(0); v <= 2400; v += 37 {
		inputs = append(inputs, v)
	}
	means := []int16{-32768, -640, 0, 640, 768, 1600, 3369, 6738, 9216, 11520, 12800, 32767}
	for v := int16(512); v <= 12800; v += 509 {
		means = append(means, v)
	}
	stds := []int16{0, 1, -1, -384, 128, 384, 400, 505, 828, 1064, 1540, 4096, 16384, 32767, -32768}
	for v := int16(384); v <= 2048; v += 61 {
		stds = append(stds, v)
	}

	for _, input := range inputs {
		for _, mean := range means {
			for _, std := range stds {
				cProb, cDelta := cGaussianProbability(input, mean, std)
				gProb, gDelta := fvad.GaussianProbability(input, mean, std)
				require.Equal(t, cProb, gProb, "probability input=%d mean=%d std=%d", input, mean, std)
				require.Equal(t, cDelta, gDelta, "delta input=%d mean=%d std=%d", input, mean, std)
			}
		}
	}
}

func TestGaussianProbabilityParityPRNG(t *testing.T) {
	rng := rand.New(rand.NewPCG(23, 24))
	for i := 0; i < 300000; i++ {
		input := int16(rng.IntN(65536) - 32768)
		mean := int16(rng.IntN(65536) - 32768)
		std := int16(rng.IntN(65536) - 32768)
		cProb, cDelta := cGaussianProbability(input, mean, std)
		gProb, gDelta := fvad.GaussianProbability(input, mean, std)
		require.Equal(t, cProb, gProb, "probability input=%d mean=%d std=%d", input, mean, std)
		require.Equal(t, cDelta, gDelta, "delta input=%d mean=%d std=%d", input, mean, std)
	}
}
