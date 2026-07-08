//go:build cgo

package fvad_filterbank

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/vad/internal/fvad"
)

// makeFrame builds one deterministic frame in the requested signal
// class. Classes cover what LogOfEnergy/SplitFilter branch on: digital
// silence (energy == 0 path), near-silence (negative tot_rshifts),
// full-scale extremes (saturation/wrap paths), plain PRNG, and
// speech-band-weighted tonal content.
func makeFrame(rng *rand.Rand, n int, class int) []int16 {
	out := make([]int16, n)
	switch class {
	case 0: // digital silence
	case 1: // near-silence: tiny values
		for i := range out {
			out[i] = int16(rng.IntN(7) - 3)
		}
	case 2: // extremes: max-magnitude runs
		for i := range out {
			if (i/8)%2 == 0 {
				out[i] = -32768
			} else {
				out[i] = 32767
			}
		}
	case 3: // full-range PRNG
		for i := range out {
			out[i] = int16(rng.IntN(65536) - 32768)
		}
	default: // band-limited-ish tonal mix at moderate level
		f1 := 100 + rng.Float64()*400
		f2 := 800 + rng.Float64()*3000
		for i := range out {
			v := 8000*math.Sin(2*math.Pi*f1*float64(i)/8000) +
				4000*math.Sin(2*math.Pi*f2*float64(i)/8000)
			out[i] = int16(v)
		}
	}
	return out
}

// TestCalculateFeaturesParity streams frames of every valid 8 kHz
// length through both implementations, comparing the six band
// energies, the total-energy indicator, and every carried filter state
// after each frame.
func TestCalculateFeaturesParity(t *testing.T) {
	for _, frameLen := range []int{80, 160, 240} {
		frameLen := frameLen
		t.Run(map[int]string{80: "10ms", 160: "20ms", 240: "30ms"}[frameLen], func(t *testing.T) {
			rng := rand.New(rand.NewPCG(21, uint64(frameLen)))

			cInst := newCFilterbankInst()
			gInst := new(fvad.Inst)
			gInst.InitCore()

			for frame := 0; frame < 600; frame++ {
				in := makeFrame(rng, frameLen, frame%5)

				var gFeatures [6]int16
				cFeatures, cTotal := cInst.calculateFeatures(in)
				gTotal := fvad.CalculateFeatures(gInst, in, gFeatures[:])

				require.Equal(t, cFeatures, gFeatures, "features frame=%d class=%d", frame, frame%5)
				require.Equal(t, cTotal, gTotal, "total energy frame=%d class=%d", frame, frame%5)

				upper, lower, hp := cInst.snapshot()
				require.Equal(t, upper, gInst.UpperState, "upper_state frame=%d", frame)
				require.Equal(t, lower, gInst.LowerState, "lower_state frame=%d", frame)
				require.Equal(t, hp, gInst.HpFilterState, "hp_filter_state frame=%d", frame)
			}
		})
	}
}
