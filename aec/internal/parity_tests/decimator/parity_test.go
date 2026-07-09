//go:build cgo && aec_oracle

package decimator

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the other slices use; both sides consume
// identical Go-generated inputs, so quality is irrelevant.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func requireBitExactF32(t *testing.T, what string, iter int, got, want []float32) {
	t.Helper()
	for i := range want {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			t.Fatalf("%s: iter %d: index %d: got %v (0x%08x), want %v (0x%08x)",
				what, iter, i, got[i], math.Float32bits(got[i]),
				want[i], math.Float32bits(want[i]))
		}
	}
}

// TestDecimatorParity runs each supported downsampling factor
// (2, 4, 8) over many PRNG kBlockSize(64) frames, with the cascaded
// biquad filter state carried across calls on both sides (as the
// real EchoPathDelayEstimator does), and requires bit-exact
// agreement of the decimated output.
func TestDecimatorParity(t *testing.T) {
	for _, factor := range []int{2, 4, 8} {
		factor := factor
		t.Run(factorName(factor), func(t *testing.T) {
			rng := lcg(uint32(factor)*7919 + 11)

			goDec := aec3.NewDecimator(factor)
			cDec := newDecimatorC(factor)
			defer cDec.close()

			outLen := aec3.BlockSize / factor
			for iter := 0; iter < 500; iter++ {
				var in [aec3.BlockSize]float32
				for i := range in {
					// Full-scale-ish magnitudes, matching how the render
					// mixer/decimator sees samples upstream.
					in[i] = rng.next() * 30000
				}
				if iter%25 == 0 {
					// Bursts of full-scale input to exercise saturation-adjacent
					// filter coefficients.
					for i := 0; i < 8; i++ {
						in[i] = 32000
						in[i+8] = -32000
					}
				}

				goOut := make([]float32, outLen)
				goDec.Decimate(in[:], goOut)
				cOut := cDec.decimate(in[:], outLen)

				requireBitExactF32(t, "Decimate", iter, goOut, cOut)
			}
		})
	}
}

func factorName(f int) string {
	switch f {
	case 2:
		return "DS2"
	case 4:
		return "DS4"
	case 8:
		return "DS8"
	default:
		return "unknown"
	}
}
