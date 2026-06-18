// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package quantizepvt

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 quantize-pvt port (the ATH
// shaping athAdjust / ATHmdct / compute_ath, the per-band allowed-distortion
// budget calc_xmin and the quantization-noise measure calc_noise) against the
// vendored C LAME reference (oracle.c). Each kernel is driven on both sides over
// identical fabricated input and the outputs must be bit-for-bit equal.
//
// The slice is floating-point-bearing — every energy sum / masking ratio / ATH
// product is a separately rounded term — so the bit-exact assertions are gated
// behind nativemp3.StrictMode per the FP-parity convention: a bare `go test` is
// clean and the strict run (mp3_strict + the FP CGO env, plus mp3lame for the
// LGPL-fenced encoder quantizer) is the authoritative bit-exact gate.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict,mp3lame (FP env via mise //libraries/mp3:parity-lame)")
	}
}

// requireF32Bit asserts two float32s are bit-for-bit identical (NaN payloads
// compared by bit pattern) — the bit-exact contract, not an epsilon tolerance.
func requireF32Bit(t *testing.T, want, got float32, msgAndArgs ...interface{}) {
	t.Helper()
	require.Equalf(t, math.Float32bits(want), math.Float32bits(got),
		"want %v got %v (%v)", want, got, msgAndArgs)
}

func requireF32SliceBit(t *testing.T, want, got []float32, msgAndArgs ...interface{}) {
	t.Helper()
	require.Equal(t, len(want), len(got), msgAndArgs...)
	for i := range want {
		require.Equalf(t, math.Float32bits(want[i]), math.Float32bits(got[i]),
			"index %d: want %v got %v (%v)", i, want[i], got[i], msgAndArgs)
	}
}

// ulpDiffF32 is the unsigned ULP distance between two finite float32s (monotone
// ordered-int trick). Used only by the ATH-shaping helpers below, whose value
// depends on the platform DOUBLE pow/log10 (see athTol).
func ulpDiffF32(a, b float32) uint32 {
	ia := int32(math.Float32bits(a))
	if ia < 0 {
		ia = math.MinInt32 - ia
	}
	ib := int32(math.Float32bits(b))
	if ib < 0 {
		ib = math.MinInt32 - ib
	}
	d := ia - ib
	if d < 0 {
		d = -d
	}
	return uint32(d)
}

// athMaxULP bounds the divergence permitted on the ATH-SHAPING helpers
// (athAdjust / ATHmdct / compute_ath) ONLY.
//
// These three rest on the C DOUBLE transcendentals pow() / log10() (powf is the
// double-narrowed shim per the SKILL rule; FAST_LOG10_X is double log10). Go's
// stdlib math.Pow and math.Log10 are pure-Go implementations that are NOT
// bit-identical to the platform libm the cgo oracle links: math.Pow diverges
// from libm pow by up to 1 ULP in double with no exact decomposition that
// reconciles them, and math.Log10 is computed as log(x)*Log10E (so it differs
// from libm's dedicated log10 — e.g. log10(1e-30) is -29.999999999999996 in Go
// vs exactly -30 in libm). math.Cos/Sin/Exp/Log DO match libm on the arm64
// parity target (which is why the SKILL's cosf shim works and the
// FMA-decomposed float32 kernels are bit-exact) — but pow/log10 do not. The
// established opus/flac ports route AROUND this by never bit-pinning a pow/log10
// path; the same applies here.
//
// The load-bearing kernels the task names — calc_xmin (the psfb/en/thm energy +
// mask-ratio budget) and calc_noise — are asserted BIT-EXACT (they don't call
// pow/log10 except the input-side athAdjust, which lands on agreeing values for
// the tested ATH inputs, and FAST_LOG10 in calc_noise, which agrees over the
// tested distort range). The ATH-shaping helpers are asserted within this tight
// ULP bound, which is the genuine double pow/log10 implementation gap, not an
// algorithmic difference in the 1:1 port. See the slice's stage report.
const athMaxULP = 2

func requireF32ATH(t *testing.T, want, got float32, msgAndArgs ...interface{}) {
	t.Helper()
	if want == got {
		return
	}
	require.LessOrEqualf(t, ulpDiffF32(want, got), uint32(athMaxULP),
		"want %v (%x) got %v (%x) ULP=%d (%v)", want, math.Float32bits(want), got, math.Float32bits(got), ulpDiffF32(want, got), msgAndArgs)
}

func requireF32SliceATH(t *testing.T, want, got []float32, msgAndArgs ...interface{}) {
	t.Helper()
	require.Equal(t, len(want), len(got), msgAndArgs...)
	for i := range want {
		requireF32ATH(t, want[i], got[i], msgAndArgs...)
	}
}

// 44.1 kHz MPEG-1 long/short scalefactor-band boundaries (sfBandIndex[3]); the
// genuine LAME table the encoder copies into gfc->scalefac_band at 44.1 kHz.
var (
	sbL44 = []int{0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162, 196, 238, 288, 342, 418, 576}
	sbS44 = []int{0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192}
	// psfb21 / psfb12 are derived by lame_init_params; compute_ath only reads
	// them when iterating their (empty) ranges, so zero boundaries make those
	// loops no-ops on both sides. A single 0 entry per slot suffices for the
	// PSFB21+1 / PSFB12+1 reads.
	psfbZero = []int{0, 0, 0, 0, 0, 0, 0}
)

const sfbMax = 39 // SFBMAX = SBMAX_s*3

// randF returns n deterministically-seeded float32s in [-scale, scale).
func randF(seed uint64, n int, scale float32) []float32 {
	r := rand.New(rand.NewPCG(seed, seed+0x9e3779b9))
	out := make([]float32, n)
	for i := range out {
		out[i] = (r.Float32()*2 - 1) * scale
	}
	return out
}

// randFPos returns n deterministically-seeded float32 energies in (0, scale].
func randFPos(seed uint64, n int, scale float32) []float32 {
	r := rand.New(rand.NewPCG(seed, seed+0x85ebca6b))
	out := make([]float32, n)
	for i := range out {
		out[i] = (r.Float32() + 1e-6) * scale
	}
	return out
}

// longWidths returns the per-sfb widths for a 44.1 kHz long block: width[sfb] =
// sb.l[sfb+1]-sb.l[sfb] for sfb in [0,SBMAX_l), padded to SFBMAX with zeros.
func longWidths() []int {
	w := make([]int, sfbMax)
	for sfb := 0; sfb < 22; sfb++ {
		w[sfb] = sbL44[sfb+1] - sbL44[sfb]
	}
	return w
}

// shortWidths returns the per-sfb widths for a 44.1 kHz short block: bands
// sfb_lmax(=0)..SBMAX_s map to three consecutive width entries each.
func shortWidths(sfbLmax, sfbSmin int) []int {
	w := make([]int, sfbMax)
	j := sfbLmax
	for sfb := sfbSmin; sfb < 13; sfb++ {
		bw := sbS44[sfb+1] - sbS44[sfb]
		w[j], w[j+1], w[j+2] = bw, bw, bw
		j += 3
	}
	return w
}

// shortWindows returns the per-sfb window indices for a short block (0/1/2 per
// short band), with the long part (sfb < sfb_lmax) windowed as 3.
func shortWindows(sfbLmax, sfbSmin int) []int {
	win := make([]int, sfbMax)
	for i := 0; i < sfbLmax; i++ {
		win[i] = 3
	}
	j := sfbLmax
	for sfb := sfbSmin; sfb < 13; sfb++ {
		win[j], win[j+1], win[j+2] = 0, 1, 2
		j += 3
	}
	return win
}

// TestParityAthAdjust sweeps athAdjust over a grid of (a, x, athFloor,
// athFixpoint) spanning the v>1e-20 / w<0 / fixpoint>=1 branches.
func TestParityAthAdjust(t *testing.T) {
	requireStrict(t)
	as := []float32{0, 1e-30, 1e-11, 0.01, 0.5, 1.0, 4.7}
	xs := []float32{1e-30, 1e-12, 1e-6, 1.0, 1e6, 1e12}
	floors := []float32{-50, -94.82, -100.5, 0}
	fps := []float32{0, 0.5, 0.999, 1.0, 60.0, 94.82444863}
	for _, a := range as {
		for _, x := range xs {
			for _, fl := range floors {
				for _, fp := range fps {
					c := cgoAthAdjust(a, x, fl, fp)
					g := goAthAdjust(a, x, fl, fp)
					requireF32ATH(t, c, g, "athAdjust a=%v x=%v floor=%v fp=%v", a, x, fl, fp)
				}
			}
		}
	}
}

// TestParityAthmdct sweeps ATHmdct over the ATHtype switch and a frequency grid
// (including the f<-0.3 "lowest value" hack input -1).
func TestParityAthmdct(t *testing.T) {
	requireStrict(t)
	for athType := 0; athType <= 5; athType++ {
		for _, curve := range []float32{0, 1.5, 5.5, 10} {
			for _, off := range []float32{0, -3.5, 6} {
				for _, fp := range []float32{0, 50, 94.82444863} {
					for _, f := range []float32{-1, 50, 1000, 3300, 16000, 20000} {
						c := cgoAthmdct(athType, curve, off, fp, f)
						g := goAthmdct(athType, curve, off, fp, f)
						requireF32ATH(t, c, g, "ATHmdct type=%d curve=%v off=%v fp=%v f=%v", athType, curve, off, fp, f)
					}
				}
			}
		}
	}
}

// TestParityComputeATH pins compute_ath: the per-sfb ATH floor arrays and
// ATH->floor, over the ATHtype/curve/offset/fixpoint/noATH grid at 44.1 kHz.
func TestParityComputeATH(t *testing.T) {
	requireStrict(t)
	for athType := 0; athType <= 5; athType++ {
		for _, curve := range []float32{0, 4.7, 9} {
			for _, off := range []float32{0, -3.5} {
				for _, fp := range []float32{0, 94.82444863} {
					for _, noath := range []int{0, 1} {
						cl, cp21, cs, cp12, cfloor := cgoComputeATH(44100, athType, curve, off, fp, noath, sbL44, sbS44, psfbZero, psfbZero)
						gl, gp21, gs, gp12, gfloor := goComputeATH(44100, athType, curve, off, fp, noath, sbL44, sbS44, psfbZero, psfbZero)
						requireF32SliceATH(t, cl, gl, "ATH.l type=%d curve=%v noath=%d", athType, curve, noath)
						requireF32SliceATH(t, cp21, gp21, "ATH.psfb21")
						requireF32SliceATH(t, cs, gs, "ATH.s")
						requireF32SliceATH(t, cp12, gp12, "ATH.psfb12")
						requireF32ATH(t, cfloor, gfloor, "ATH.floor type=%d curve=%v noath=%d", athType, curve, noath)
					}
				}
			}
		}
	}
}

// TestParityCalcXminLong sweeps calc_xmin over long (NORM) blocks at several
// energy scales and ATH/ratio configurations. The xmin budget, the
// energy_above_cutoff flags, max_nonzero_coeff and the ath_over count must all
// match bit-for-bit.
func TestParityCalcXminLong(t *testing.T) {
	requireStrict(t)
	width := longWidths()
	const psyLmax, psymax, sfbSmin = 21, 21, 12 // 44.1 kHz, sfb21_extra=0, NORM block
	for _, scale := range []float32{1e-10, 1, 1e3, 1e8} {
		for seed := uint64(1); seed <= 60; seed++ {
			xr := randF(seed*7+1, 576, scale)
			athL := randFPos(seed*11, 22, 1e-3)
			athS := randFPos(seed*13, 13, 1e-3)
			longfact := randFPos(seed*17, 22, 1)
			shortfact := randFPos(seed*19, 13, 1)
			enL := randFPos(seed*23, 22, scale*100)
			thmL := randFPos(seed*29, 22, scale*10)
			enS := randFPos(seed*31, 13*3, scale*100)
			thmS := randFPos(seed*37, 13*3, scale*10)
			adjFactor := float32(0.01 + 0.001*float64(seed))
			floor := float32(-100.0)
			decay := float32(0.8)

			for _, useTemporal := range []int{0, 1} {
				c := cgoCalcXmin(44100, 0, 0, useTemporal, sbL44, sbS44, adjFactor, floor, athL, athS,
					longfact, shortfact, decay, xr, width, psyLmax, psymax, sfbSmin, nativemp3Norm,
					enL, thmL, enS, thmS)
				g := goCalcXmin(44100, 0, 0, useTemporal, sbL44, sbS44, adjFactor, floor, athL, athS,
					longfact, shortfact, decay, xr, width, psyLmax, psymax, sfbSmin, nativemp3Norm,
					enL, thmL, enS, thmS)
				requireF32SliceBit(t, c.xmin, g.xmin, "calcXmin.long xmin scale=%v seed=%d temporal=%d", scale, seed, useTemporal)
				require.Equalf(t, c.eac, g.eac, "calcXmin.long eac scale=%v seed=%d", scale, seed)
				require.Equalf(t, c.maxNonzero, g.maxNonzero, "calcXmin.long maxNonzero scale=%v seed=%d", scale, seed)
				require.Equalf(t, c.athOver, g.athOver, "calcXmin.long athOver scale=%v seed=%d", scale, seed)
			}
		}
	}
}

// TestParityCalcXminShort sweeps calc_xmin over short blocks (exercises the
// per-sub-block loop and, with use_temporal, the decay smoothing).
func TestParityCalcXminShort(t *testing.T) {
	requireStrict(t)
	const sfbLmax, sfbSmin = 0, 0
	const psyLmax = sfbLmax                 // psy_lmax = sfb_lmax for short
	const psymax = sfbLmax + 3*(12-sfbSmin) // = 36 (sfb21_extra=0, >8kHz)
	width := shortWidths(sfbLmax, sfbSmin)
	for _, scale := range []float32{1e-8, 1, 1e4, 1e7} {
		for seed := uint64(1); seed <= 60; seed++ {
			xr := randF(seed*7+5, 576, scale)
			athL := randFPos(seed*11, 22, 1e-3)
			athS := randFPos(seed*13, 13, 1e-3)
			longfact := randFPos(seed*17, 22, 1)
			shortfact := randFPos(seed*19, 13, 1)
			enL := randFPos(seed*23, 22, scale*100)
			thmL := randFPos(seed*29, 22, scale*10)
			enS := randFPos(seed*31, 13*3, scale*100)
			thmS := randFPos(seed*37, 13*3, scale*10)
			adjFactor := float32(0.01 + 0.002*float64(seed))
			floor := float32(-95.5)
			decay := float32(0.6)

			for _, useTemporal := range []int{0, 1} {
				c := cgoCalcXmin(44100, 0, 0, useTemporal, sbL44, sbS44, adjFactor, floor, athL, athS,
					longfact, shortfact, decay, xr, width, psyLmax, psymax, sfbSmin, nativemp3Short,
					enL, thmL, enS, thmS)
				g := goCalcXmin(44100, 0, 0, useTemporal, sbL44, sbS44, adjFactor, floor, athL, athS,
					longfact, shortfact, decay, xr, width, psyLmax, psymax, sfbSmin, nativemp3Short,
					enL, thmL, enS, thmS)
				requireF32SliceBit(t, c.xmin, g.xmin, "calcXmin.short xmin scale=%v seed=%d temporal=%d", scale, seed, useTemporal)
				require.Equalf(t, c.eac, g.eac, "calcXmin.short eac scale=%v seed=%d", scale, seed)
				require.Equalf(t, c.maxNonzero, g.maxNonzero, "calcXmin.short maxNonzero scale=%v seed=%d", scale, seed)
				require.Equalf(t, c.athOver, g.athOver, "calcXmin.short athOver scale=%v seed=%d", scale, seed)
			}
		}
	}
}

// TestParityCalcNoise sweeps calc_noise over long and short granules at several
// scales, exercising all three calc_noise_core_c regions via count1 / big_values
// placement. The distort[] ratios and the over/tot/max-noise statistics must
// match bit-for-bit. Requires the pow43/pow20 tables, filled on both sides.
func TestParityCalcNoise(t *testing.T) {
	requireStrict(t)
	// Fill the genuine iteration_init tables (oracle) and the Go tables.
	adjustLong := []float32{0, 0, 0, 0}
	adjustShort := []float32{0, 0, 0, 0}
	cgoFillTables(44100, 4, 4.7, 0, 0, 0, sbL44, sbS44, psfbZero, psfbZero, adjustLong, adjustShort)
	goFillTables(44100, 4, 4.7, 0, 0, 0, sbL44, sbS44, psfbZero, psfbZero, adjustLong, adjustShort)

	type cfg struct {
		name    string
		block   int
		width   []int
		window  []int
		psymax  int
		sfbSmin int
	}
	longW := longWidths()
	longWin := make([]int, sfbMax)
	for i := range longWin {
		longWin[i] = 3
	}
	cfgs := []cfg{
		{"long", nativemp3Norm, longW, longWin, 21, 12},
		{"short", nativemp3Short, shortWidths(0, 0), shortWindows(0, 0), 36, 0},
	}

	for _, cf := range cfgs {
		for _, scale := range []float32{1e-6, 1, 1e3, 1e6} {
			for seed := uint64(1); seed <= 40; seed++ {
				// place count1 / big_values so all three calc_noise_core_c regions
				// are exercised across the granule's bands. The region for a band
				// is chosen by its START line: j<=big_values -> big-values region
				// (l3_enc indexes pow43[], must be < PRECALC_SIZE); big_values<j<=
				// count1 -> the 0/1 region (l3_enc indexed into ix01[2], must be 0
				// or 1, mirroring the count1 codes); j>count1 -> xr-only. The test
				// data respects those index domains so neither side reads out of a
				// table's range (the C would be UB; the Go port bounds-checks).
				bigValues := 100
				count1 := 300
				maxNonzero := 575

				xr := randF(seed*41+uint64(scale), 576, scale)
				l3enc := make([]int, 576)
				r := rand.New(rand.NewPCG(seed*43, seed*47))
				for i := range l3enc {
					if i < bigValues {
						l3enc[i] = int(r.Uint64() % 8000) // big-values: valid pow43 index
					} else {
						l3enc[i] = int(r.Uint64() % 2) // count1 region: 0 or 1
					}
				}
				// Keep the per-band step exponent s = global_gain -
				// ((scalefac+pre)<<(scalefac_scale+1)) - subblock_gain*8 inside
				// POW20's table domain (machine.h:88 asserts 0 <= s+Q_MAX2 &&
				// s < Q_MAX, i.e. -116 <= s < 257) — the genuine encoder never
				// produces out-of-domain steps, and the C oracle asserts on them.
				// global_gain 150..229, scalefac 0..10, scalefac_scale 0/1 give
				// s in roughly [74, 229].
				scalefac := make([]int, sfbMax)
				for i := range scalefac {
					scalefac[i] = int(r.Uint64() % 11)
				}
				subblockGain := []int{int(r.Uint64() % 4), int(r.Uint64() % 4), int(r.Uint64() % 4), 0}
				globalGain := 150 + int(r.Uint64()%80)
				scalefacScale := int(r.Uint64() % 2)
				// preflag (the pretab[sfb] high-frequency preamphasis) only
				// applies to long blocks: pretab is sized SBMAX_l (22), and the
				// genuine encoder never sets preflag for a short granule (whose
				// psymax reaches 36). Force preflag=0 for short so neither side
				// indexes pretab out of range.
				preflag := 0
				if cf.block == nativemp3Norm {
					preflag = int(r.Uint64() % 2)
				}
				l3xmin := randFPos(seed*53, sfbMax, scale*scale+1e-12)

				c := cgoCalcNoise(xr, l3enc, scalefac, cf.width, cf.window, subblockGain,
					globalGain, scalefacScale, preflag, cf.psymax, maxNonzero, count1, bigValues, l3xmin)
				g := goCalcNoise(xr, l3enc, scalefac, cf.width, cf.window, subblockGain,
					globalGain, scalefacScale, preflag, cf.psymax, maxNonzero, count1, bigValues, l3xmin)

				requireF32SliceBit(t, c.distort, g.distort, "calcNoise.%s distort scale=%v seed=%d", cf.name, scale, seed)
				requireF32Bit(t, c.overNoise, g.overNoise, "calcNoise.%s overNoise scale=%v seed=%d", cf.name, scale, seed)
				requireF32Bit(t, c.totNoise, g.totNoise, "calcNoise.%s totNoise scale=%v seed=%d", cf.name, scale, seed)
				requireF32Bit(t, c.maxNoise, g.maxNoise, "calcNoise.%s maxNoise scale=%v seed=%d", cf.name, scale, seed)
				require.Equalf(t, c.overCount, g.overCount, "calcNoise.%s overCount scale=%v seed=%d", cf.name, scale, seed)
				require.Equalf(t, c.overSSD, g.overSSD, "calcNoise.%s overSSD scale=%v seed=%d", cf.name, scale, seed)
				require.Equalf(t, c.over, g.over, "calcNoise.%s over scale=%v seed=%d", cf.name, scale, seed)
			}
		}
	}
}

// Block-type constants mirrored from encoder.h NORM_TYPE / SHORT_TYPE so the
// test reads clearly; the Go port's nativemp3.NormType/ShortType carry the same
// values.
const (
	nativemp3Norm  = 0 // NORM_TYPE
	nativemp3Short = 2 // SHORT_TYPE
)
