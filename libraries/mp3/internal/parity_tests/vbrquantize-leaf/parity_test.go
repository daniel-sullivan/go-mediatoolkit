// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizeleaf

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 VBR quantizer leaf kernels
// (vbrquantize.go: vec_max_c / find_lowest_scalefac / k_34_4 /
// calc_sfb_noise_x34 / tri_calc_sfb_noise_x34 / calc_scalefac /
// guess_scalefac_x34 / find_scalefac_x34, a 1:1 port of the float leaf
// functions of libmp3lame/vbrquantize.c) against the vendored LAME C reference
// (oracle.c). Each kernel is driven on both sides over identical fabricated
// band inputs and the result must be bit-for-bit equal.
//
// The slice is floating-point-bearing (the band-noise sums and the
// TAKEHIRO_IEEE754_HACK magic-float add), so the bit-exact assertions are gated
// behind nativemp3.StrictMode: a bare `go test` is clean and the strict run
// (mp3lame + mp3_strict + the FP CGO env, the //libraries/mp3:encode-parity
// task) is the authoritative gate.

func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:encode-parity)")
	}
}

func init() {
	// Fill the precompute tables on both sides once. (Each side's fill is the
	// verbatim iteration_init table-fill loop; oracle_fill_tables and
	// FillVbrQuantizeTables are idempotent.)
	cgoFillTables()
	goFillTables()
}

// fabricateBand builds an xr / xr34 pair for one scalefactor band of bw lines.
// xr holds plausible MDCT magnitudes (the C reads fabsf(xr)); xr34 = |xr|^(3/4)
// is the coloured magnitude the search operates on, exactly as LAME's
// init_xrpow fills it. mag scales the band energy so different sf ranges are
// exercised. Both sides receive byte-identical float32 arrays.
func fabricateBand(rng *rand.Rand, bw int, mag float64) (xr, xr34 []float32) {
	xr = make([]float32, bw)
	xr34 = make([]float32, bw)
	for i := 0; i < bw; i++ {
		v := mag * rng.Float64()
		// occasional near-zero and occasional larger coefficient
		switch rng.IntN(4) {
		case 0:
			v = mag * 0.001 * rng.Float64()
		case 1:
			v *= 4
		}
		xr[i] = float32(v)
		xr34[i] = float32(math.Pow(float64(xr[i]), 0.75))
	}
	return xr, xr34
}

func TestVecMaxC(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(1, 2))
	for _, bw := range []int{0, 1, 2, 3, 4, 5, 7, 8, 11, 18, 24, 192, 576} {
		for trial := 0; trial < 20; trial++ {
			_, xr34 := fabricateBand(rng, max(bw, 1), 1000*rng.Float64())
			got := goVecMaxC(xr34, bw)
			want := cgoVecMaxC(xr34, bw)
			assert.Equal(t, math.Float32bits(want), math.Float32bits(got),
				"vec_max_c bw=%d trial=%d: want=%v got=%v", bw, trial, want, got)
		}
	}
}

func TestFindLowestScalefac(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(3, 4))
	for trial := 0; trial < 2000; trial++ {
		// xr34 across many orders of magnitude so every binary-search branch runs.
		xr34 := float32(math.Pow(10, -3+9*rng.Float64()))
		assert.Equal(t, cgoFindLowestScalefac(xr34), goFindLowestScalefac(xr34),
			"find_lowest_scalefac xr34=%v", xr34)
	}
}

func TestK344(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(5, 6))
	for trial := 0; trial < 5000; trial++ {
		var x [4]float64
		for i := range x {
			// the C asserts x[k] <= IXMAX_VAL (8206); stay in [0, 8206].
			x[i] = 8206 * rng.Float64()
		}
		assert.Equal(t, cgoK344(x), goK344(x), "k_34_4 x=%v", x)
	}
}

func TestCalcSfbNoiseX34(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(7, 8))
	for _, bw := range []int{1, 2, 3, 4, 5, 7, 8, 11, 18, 24} {
		for trial := 0; trial < 80; trial++ {
			mag := math.Pow(10, rng.Float64()*4) // 1 .. 1e4
			xr, xr34 := fabricateBand(rng, bw, mag)
			// only sf for which sfpow34*max(xr34) <= IXMAX_VAL are valid (the
			// k_34_4 assert): sf >= find_lowest_scalefac(max). Use that lower
			// bound plus a few steps up.
			maxXr34 := cgoVecMaxC(xr34, bw)
			lo := cgoFindLowestScalefac(maxXr34)
			for d := 0; d < 6; d++ {
				sf := int(lo) + d
				if sf > 255 {
					break
				}
				want := cgoCalcSfbNoiseX34(xr, xr34, bw, uint8(sf))
				got := goCalcSfbNoiseX34(xr, xr34, bw, uint8(sf))
				assert.Equal(t, math.Float32bits(want), math.Float32bits(got),
					"calc_sfb_noise_x34 bw=%d sf=%d trial=%d: want=%v got=%v", bw, sf, trial, want, got)
			}
		}
	}
}

func TestTriCalcSfbNoiseX34(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(9, 10))
	for _, bw := range []int{1, 4, 8, 18, 24} {
		for trial := 0; trial < 120; trial++ {
			mag := math.Pow(10, rng.Float64()*4)
			xr, xr34 := fabricateBand(rng, bw, mag)
			maxXr34 := cgoVecMaxC(xr34, bw)
			lo := cgoFindLowestScalefac(maxXr34)
			// l3_xmin around a plausible allowed-distortion budget.
			l3Xmin := float32(math.Pow(10, rng.Float64()*6))
			for d := 1; d < 5; d++ {
				sf := int(lo) + d
				if sf > 254 {
					break
				}
				want := cgoTriCalcSfbNoiseX34(xr, xr34, l3Xmin, bw, uint8(sf))
				got := goTriCalcSfbNoiseX34(xr, xr34, l3Xmin, bw, uint8(sf))
				assert.Equal(t, want, got,
					"tri_calc_sfb_noise_x34 bw=%d sf=%d l3xmin=%v: want=%d got=%d", bw, sf, l3Xmin, want, got)
			}
		}
	}
}

func TestCalcScalefac(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(11, 12))
	for trial := 0; trial < 5000; trial++ {
		l3Xmin := float32(math.Pow(10, -6+14*rng.Float64()))
		bw := 1 + rng.IntN(36)
		assert.Equal(t, cgoCalcScalefac(l3Xmin, bw), goCalcScalefac(l3Xmin, bw),
			"calc_scalefac l3xmin=%v bw=%d", l3Xmin, bw)
	}
}

func TestGuessScalefacX34(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(13, 14))
	for _, bw := range []int{1, 4, 8, 18, 24} {
		for trial := 0; trial < 200; trial++ {
			mag := math.Pow(10, rng.Float64()*4)
			xr, xr34 := fabricateBand(rng, bw, mag)
			l3Xmin := float32(math.Pow(10, -6+14*rng.Float64()))
			sfMin := uint8(rng.IntN(200))
			assert.Equal(t,
				cgoGuessScalefacX34(xr, xr34, l3Xmin, bw, sfMin),
				goGuessScalefacX34(xr, xr34, l3Xmin, bw, sfMin),
				"guess_scalefac_x34 bw=%d l3xmin=%v sfmin=%d", bw, l3Xmin, sfMin)
		}
	}
}

func TestFindScalefacX34(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(15, 16))
	for _, bw := range []int{1, 4, 8, 18, 24} {
		for trial := 0; trial < 200; trial++ {
			mag := math.Pow(10, rng.Float64()*4)
			xr, xr34 := fabricateBand(rng, bw, mag)
			maxXr34 := cgoVecMaxC(xr34, bw)
			// sf_min must keep the search inside the k_34_4 domain.
			sfMin := cgoFindLowestScalefac(maxXr34)
			l3Xmin := float32(math.Pow(10, rng.Float64()*8))
			assert.Equal(t,
				cgoFindScalefacX34(xr, xr34, l3Xmin, bw, sfMin),
				goFindScalefacX34(xr, xr34, l3Xmin, bw, sfMin),
				"find_scalefac_x34 bw=%d l3xmin=%v sfmin=%d", bw, l3Xmin, sfMin)
		}
	}
}
