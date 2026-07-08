//go:build cgo

package gatingblock

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/parity_tests/strictparity"
	"github.com/daniel-sullivan/go-mediatoolkit/loudness/internal/r128"
)

// Raw libebur128 mode / channel values (ebur128.h). Hardcoded rather than
// imported from package loudness because a parity slice must not link that
// package's own copy of the C oracle.
const (
	modeM = 1 << 0 // EBUR128_MODE_M

	chUnused   = 0  // EBUR128_UNUSED
	chLeft     = 1  // EBUR128_LEFT
	chRight    = 2  // EBUR128_RIGHT
	chCenter   = 3  // EBUR128_CENTER
	chLs       = 4  // EBUR128_LEFT_SURROUND / Mp110
	chRs       = 5  // EBUR128_RIGHT_SURROUND / Mm110
	chDualMono = 6  // EBUR128_DUAL_MONO
	chMp060    = 9  // EBUR128_Mp060 (also weighted 1.41)
	chMp090    = 11 // EBUR128_Mp090 (also weighted 1.41)
)

// samplesIn100ms mirrors ebur128_init's (rate+5)/10.
func samplesIn100ms(rate int) int { return (rate + 5) / 10 }

// energyNonStrictRelTolerance / energyNonStrictAbsFloor bound the block
// energy comparison under strictparity.Enabled == false: without a guaranteed
// -ffp-contract=off oracle, the per-sample weighted-sum-of-squares
// accumulation may be FMA-fused on the C side, drifting from the FMA-free
// Go port by a handful of ULP (empirically ~1e-13 relative here). 1e-9
// leaves several orders of margin while still catching a real port defect.
// The absolute floor anchors near-zero energies (e.g. silence), where a
// purely relative tolerance would divide by ~0.
const energyNonStrictRelTolerance = 1e-9
const energyNonStrictAbsFloor = 1e-9

// assertEnergyParity asserts gating-block energy parity: bit-exact under
// strictparity.Enabled (the C oracle built with -ffp-contract=off, via the
// //loudness:parity / //loudness:test mise tasks), else a combined
// relative+absolute tolerance loose enough to absorb FMA-fused oracle
// drift but tight enough to still catch a real divergence.
func assertEnergyParity(t *testing.T, want, got float64) {
	t.Helper()
	if strictparity.Enabled {
		if math.Float64bits(want) != math.Float64bits(got) {
			t.Fatalf("block energy mismatch: C=%.17g (%016x) Go=%.17g (%016x)",
				want, math.Float64bits(want), got, math.Float64bits(got))
		}
		return
	}
	diff := math.Abs(want - got)
	bound := energyNonStrictAbsFloor
	if rel := energyNonStrictRelTolerance * math.Max(math.Abs(want), math.Abs(got)); rel > bound {
		bound = rel
	}
	if diff > bound {
		t.Fatalf("block energy off by %.3g (non-strict bound %.3g): C=%.17g Go=%.17g", diff, bound, want, got)
	}
}

// goBlockEnergy drives the pure-Go r128.State the same way the C shim
// drives libebur128: MODE_M meter, channel-map override, feed the PCM, read
// the trailing-400ms block energy.
func goBlockEnergy(channels, rate int, chmap []int, src []float64) float64 {
	st := r128.NewState(channels, rate, modeM)
	for c, v := range chmap {
		st.SetChannel(c, v)
	}
	if len(src) > 0 {
		st.AddFrames(src)
	}
	return st.CalcGatingBlockEnergy(samplesIn100ms(rate) * 4)
}

// TestGatingBlockEnergyParity compares the 400 ms block energy bit-for-bit
// across channel counts, channel maps, and frame counts that exercise both
// the pre-fill branch (fewer than 400 ms buffered) and the wrap-around
// branch (ring buffer full and overwritten).
func TestGatingBlockEnergyParity(t *testing.T) {
	const rate = 48000
	si4 := samplesIn100ms(rate) * 4 // 400 ms in frames

	type mapCase struct {
		name     string
		channels int
		chmap    []int
	}
	cases := []mapCase{
		{"mono/left", 1, []int{chLeft}},
		{"mono/dualmono", 1, []int{chDualMono}},
		{"mono/unused", 1, []int{chUnused}},
		{"stereo/LR", 2, []int{chLeft, chRight}},
		{"stereo/L+unused", 2, []int{chLeft, chUnused}},
		{"stereo/surround", 2, []int{chLs, chRs}}, // both 1.41-weighted
		{"5ch/default", 5, []int{chLeft, chRight, chCenter, chLs, chRs}},
		{"5ch/unused-center", 5, []int{chLeft, chRight, chUnused, chLs, chRs}},
		{"5ch/side+diagonal", 5, []int{chLeft, chRight, chCenter, chMp060, chMp090}},
	}

	// Frame counts: partial (<400ms), exactly 400ms, and several multiples
	// past the ring size so the wrap-around summation branch fires.
	frameCounts := []int{si4 / 3, si4 - 1, si4, si4 + 7, si4*2 + 123, si4 * 5}

	seed := uint64(0x9105_10AD_0000_0001)
	for _, mc := range cases {
		for _, nf := range frameCounts {
			mc, nf := mc, nf
			t.Run(fmt.Sprintf("%s/frames=%d", mc.name, nf), func(t *testing.T) {
				seed++
				rng := rand.New(rand.NewPCG(seed, 0x1770))
				src := make([]float64, nf*mc.channels)
				for i := range src {
					src[i] = (rng.Float64()*2 - 1) * 0.9
				}

				want := cgoBlockEnergy(mc.channels, rate, mc.chmap, src)
				got := goBlockEnergy(mc.channels, rate, mc.chmap, src)

				assertEnergyParity(t, want, got)
			})
		}
	}
}
