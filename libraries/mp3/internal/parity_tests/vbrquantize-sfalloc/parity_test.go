// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizesfalloc

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 VBR quantizer scalefactor-
// ALLOCATION tier (vbrquantize_sfalloc.go: block_sf / quantize_x34 /
// set_subblock_gain / set_scalefacs / checkScalefactor / short_block_constrain /
// long_block_constrain, a 1:1 port of the static allocation functions of
// libmp3lame/vbrquantize.c) against the vendored LAME C reference (oracle.c).
//
// The slice is exercised as the genuine VBR pipeline: block_sf surveys a
// fabricated granule on BOTH sides (its vbrsf / vbrsfmin / vbrmax / mingain
// outputs must match bit-for-bit), then those outputs feed the long / short
// constrain allocators (whose resolved global_gain / scalefac[] / subblock_gain[]
// / scalefac_scale / preflag must match), and quantize_x34 quantizes by the
// resolved scalefactors (l3_enc must match). The set_subblock_gain /
// set_scalefacs / checkScalefactor leaves are also pinned in isolation.
//
// The slice is floating-point-bearing (block_sf's find dispatch and
// quantize_x34's float32 product + the TAKEHIRO_IEEE754_HACK magic-float add),
// so the bit-exact assertions are gated behind nativemp3.StrictMode: a bare `go
// test` is clean and the strict run (mp3lame + mp3_strict + the FP CGO env, the
// //libraries/mp3:encode-parity task) is the authoritative gate.

const (
	sbmaxL = 22 // SBMAX_l
	sbmaxS = 13 // SBMAX_s
	sfbmax = 39 // SFBMAX
)

// longBands44 is the 44.1 kHz MPEG-1 long scalefactor-band boundary table
// (sfBandIndex 44100 .L); the per-band width is its successive difference.
var longBands44 = [sbmaxL + 1]int{
	0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162, 196, 238, 288, 342, 418, 576,
}

// shortBands44 is the 44.1 kHz MPEG-1 short band boundary table (.S); each band
// repeats across the three short windows.
var shortBands44 = [sbmaxS + 1]int{
	0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192,
}

func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:encode-parity)")
	}
}

func init() {
	cgoFillTables()
	goFillTables()
}

// longGranule builds the per-band geometry of a 44.1 kHz long block: width[sfb],
// window[sfb]=0, and the max_nonzero_coeff just past the last populated band.
func longGranule() (width, window []int, psymax, maxNZ int) {
	width = make([]int, sfbmax)
	window = make([]int, sfbmax)
	for sfb := 0; sfb < sbmaxL; sfb++ {
		width[sfb] = longBands44[sfb+1] - longBands44[sfb]
	}
	psymax = sbmaxL
	maxNZ = longBands44[sbmaxL] - 1 // 575
	return width, window, psymax, maxNZ
}

// shortGranule builds the per-band geometry of a 44.1 kHz short block: the 13
// short bands interleaved across 3 windows -> 39 sfb, each window's gain tracked
// via window[sfb] = sfb%3.
func shortGranule() (width, window []int, psymax, maxNZ int) {
	width = make([]int, sfbmax)
	window = make([]int, sfbmax)
	sfb := 0
	for b := 0; b < sbmaxS; b++ {
		w := shortBands44[b+1] - shortBands44[b]
		for win := 0; win < 3; win++ {
			width[sfb] = w
			window[sfb] = win
			sfb++
		}
	}
	psymax = sfbmax
	// last nonzero coefficient = 3 * shortBands[sbmaxS] - 1
	maxNZ = 3*shortBands44[sbmaxS] - 1
	return width, window, psymax, maxNZ
}

// fabricateLines fills xr / xr34 over n lines with plausible MDCT magnitudes,
// occasionally near-zero or boosted so a spread of scalefactors is exercised.
func fabricateLines(rng *rand.Rand, n int, mag float64) (xr, xr34 []float32) {
	xr = make([]float32, 576)
	xr34 = make([]float32, 576)
	for i := 0; i < n; i++ {
		v := mag * rng.Float64()
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

// fabricateL3xmin builds an SFBMAX per-band allowed-distortion budget.
func fabricateL3xmin(rng *rand.Rand) []float32 {
	x := make([]float32, sfbmax)
	for i := range x {
		x[i] = float32(math.Pow(10, rng.Float64()*8))
	}
	return x
}

// fabricateEac marks each band's energy-above-cutoff flag (mostly 1, a few 0 to
// exercise block_sf's 255-sentinel rewrite path).
func fabricateEac(rng *rand.Rand, psymax int) []byte {
	eac := make([]byte, sfbmax)
	for i := 0; i < psymax; i++ {
		if rng.IntN(8) == 0 {
			eac[i] = 0
		} else {
			eac[i] = 1
		}
	}
	return eac
}

// TestBlockSf pins block_sf for both block types and both find dispatches: the
// vbrsf / vbrsfmin surveys, vbrmax, and the mingain floors must match.
func TestBlockSf(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(20, 21))
	for _, short := range []bool{false, true} {
		for _, findSel := range []int{0, 1} {
			for trial := 0; trial < 60; trial++ {
				var width, window []int
				var psymax, maxNZ int
				if short {
					width, window, psymax, maxNZ = shortGranule()
				} else {
					width, window, psymax, maxNZ = longGranule()
				}
				mag := math.Pow(10, rng.Float64()*4)
				xr, xr34 := fabricateLines(rng, maxNZ+1, mag)
				l3xmin := fabricateL3xmin(rng)
				eac := fabricateEac(rng, psymax)

				cg, gg := cgoNewHandle(), goNewHandle()
				blockType := 0
				if short {
					blockType = 2
				}
				cg.setCfg(2, 1)
				cg.setXr(xr)
				cg.setWidth(width)
				cg.setWindow(window)
				cg.setEac(eac)
				cg.setGeom(blockType, 210, 0, 0, psymax, psymax, maxNZ)
				gg.setCfg(2, 1)
				gg.setXr(xr)
				gg.setWidth(width)
				gg.setWindow(window)
				gg.setEac(eac)
				gg.setGeom(blockType, 210, 0, 0, psymax, psymax, maxNZ)

				cvbrsf := make([]int, sfbmax)
				cvbrsfmin := make([]int, sfbmax)
				gvbrsf := make([]int, sfbmax)
				gvbrsfmin := make([]int, sfbmax)
				cmax, cmgL, cmgS := cg.blockSf(xr34, l3xmin, findSel, cvbrsf, cvbrsfmin)
				gmax, gmgL, gmgS := gg.blockSf(xr34, l3xmin, findSel, gvbrsf, gvbrsfmin)

				assert.Equal(t, cmax, gmax, "block_sf vbrmax short=%v find=%d trial=%d", short, findSel, trial)
				assert.Equal(t, cmgL, gmgL, "block_sf mingain_l short=%v find=%d trial=%d", short, findSel, trial)
				assert.Equal(t, cmgS, gmgS, "block_sf mingain_s short=%v find=%d trial=%d", short, findSel, trial)
				assert.Equal(t, cvbrsf, gvbrsf, "block_sf vbrsf short=%v find=%d trial=%d", short, findSel, trial)
				assert.Equal(t, cvbrsfmin, gvbrsfmin, "block_sf vbrsfmin short=%v find=%d trial=%d", short, findSel, trial)
				cg.free()
			}
		}
	}
}

// runPipeline drives block_sf then the matching constrain allocator then
// quantize_x34 on one handle, returning the resolved side info + l3_enc.
type sideInfo struct {
	globalGain, scalefacScale, preflag int
	scalefac                           [sfbmax]int
	subblockGain                       [3]int
	l3enc                              [576]int
}

func TestConstrainAndQuantize(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(22, 23))
	for _, short := range []bool{false, true} {
		for _, noiseShaping := range []int{1, 2} {
			for trial := 0; trial < 80; trial++ {
				var width, window []int
				var psymax, maxNZ int
				if short {
					width, window, psymax, maxNZ = shortGranule()
				} else {
					width, window, psymax, maxNZ = longGranule()
				}
				mag := math.Pow(10, rng.Float64()*4)
				xr, xr34 := fabricateLines(rng, maxNZ+1, mag)
				l3xmin := fabricateL3xmin(rng)
				eac := fabricateEac(rng, psymax)
				blockType := 0
				if short {
					blockType = 2
				}
				sfbmaxGeom := psymax // sfbmax == psymax for these block shapes here

				drive := func(useCgo bool) sideInfo {
					var cg *cgoHandle
					var gg *goHandle
					var vbrsf, vbrsfmin []int
					var vbrmax, mgL int
					var mgS [3]int
					vbrsf = make([]int, sfbmax)
					vbrsfmin = make([]int, sfbmax)
					if useCgo {
						cg = cgoNewHandle()
						cg.setCfg(2, noiseShaping)
						cg.setXr(xr)
						cg.setWidth(width)
						cg.setWindow(window)
						cg.setEac(eac)
						cg.setGeom(blockType, 210, 0, 0, sfbmaxGeom, psymax, maxNZ)
						vbrmax, mgL, mgS = cg.blockSf(xr34, l3xmin, VbrFindFull, vbrsf, vbrsfmin)
						if short {
							cg.shortConstrain(vbrsf, vbrsfmin, vbrmax, mgL, mgS)
						} else {
							cg.longConstrain(vbrsf, vbrsfmin, vbrmax, mgL, mgS)
						}
						cg.quantizeX34(xr34)
						return readSide(cg)
					}
					gg = goNewHandle()
					gg.setCfg(2, noiseShaping)
					gg.setXr(xr)
					gg.setWidth(width)
					gg.setWindow(window)
					gg.setEac(eac)
					gg.setGeom(blockType, 210, 0, 0, sfbmaxGeom, psymax, maxNZ)
					vbrmax, mgL, mgS = gg.blockSf(xr34, l3xmin, VbrFindFull, vbrsf, vbrsfmin)
					if short {
						gg.shortConstrain(vbrsf, vbrsfmin, vbrmax, mgL, mgS)
					} else {
						gg.longConstrain(vbrsf, vbrsfmin, vbrmax, mgL, mgS)
					}
					gg.quantizeX34(xr34)
					return readSide(gg)
				}

				cSide := drive(true)
				gSide := drive(false)
				require.Equal(t, cSide.globalGain, gSide.globalGain,
					"constrain global_gain short=%v ns=%d trial=%d", short, noiseShaping, trial)
				assert.Equal(t, cSide.scalefacScale, gSide.scalefacScale,
					"scalefac_scale short=%v ns=%d trial=%d", short, noiseShaping, trial)
				assert.Equal(t, cSide.preflag, gSide.preflag,
					"preflag short=%v ns=%d trial=%d", short, noiseShaping, trial)
				assert.Equal(t, cSide.scalefac, gSide.scalefac,
					"scalefac short=%v ns=%d trial=%d", short, noiseShaping, trial)
				assert.Equal(t, cSide.subblockGain, gSide.subblockGain,
					"subblock_gain short=%v ns=%d trial=%d", short, noiseShaping, trial)
				assert.Equal(t, cSide.l3enc, gSide.l3enc,
					"l3_enc short=%v ns=%d trial=%d", short, noiseShaping, trial)
			}
		}
	}
}

// sideReader is satisfied by both handles' getters.
type sideReader interface {
	globalGain() int
	scalefacScale() int
	preflag() int
	scalefac(int) int
	subblockGain(int) int
	l3enc(int) int
}

func readSide(h sideReader) sideInfo {
	var s sideInfo
	s.globalGain = h.globalGain()
	s.scalefacScale = h.scalefacScale()
	s.preflag = h.preflag()
	for i := 0; i < sfbmax; i++ {
		s.scalefac[i] = h.scalefac(i)
	}
	for i := 0; i < 3; i++ {
		s.subblockGain[i] = h.subblockGain(i)
	}
	for i := 0; i < 576; i++ {
		s.l3enc[i] = h.l3enc(i)
	}
	return s
}

// TestSetSubblockGain pins set_subblock_gain in isolation: the sf[] deltas are
// updated in place and subblock_gain / global_gain mutated.
func TestSetSubblockGain(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(24, 25))
	_, window, psymax, _ := shortGranule()
	for trial := 0; trial < 400; trial++ {
		globalGain := 100 + rng.IntN(120)
		scalefacScale := rng.IntN(2)
		var mgS [3]int
		for i := range mgS {
			mgS[i] = rng.IntN(80)
		}
		sf := make([]int, sfbmax)
		for i := range sf {
			sf[i] = -rng.IntN(120) // sf deltas are typically <= 0
		}

		cg, gg := cgoNewHandle(), goNewHandle()
		csf := append([]int(nil), sf...)
		gsf := append([]int(nil), sf...)
		cg.setWindow(window)
		cg.setGeom(2, globalGain, scalefacScale, 0, psymax, psymax, 0)
		gg.setWindow(window)
		gg.setGeom(2, globalGain, scalefacScale, 0, psymax, psymax, 0)

		cg.setSubblockGainK(mgS, csf)
		gg.setSubblockGainK(mgS, gsf)

		assert.Equal(t, csf, gsf, "set_subblock_gain sf trial=%d", trial)
		for i := 0; i < 3; i++ {
			assert.Equal(t, cg.subblockGain(i), gg.subblockGain(i),
				"set_subblock_gain subblock_gain[%d] trial=%d", i, trial)
		}
		assert.Equal(t, cg.globalGain(), gg.globalGain(),
			"set_subblock_gain global_gain trial=%d", trial)
		cg.free()
	}
}

// TestSetScalefacs pins set_scalefacs across the three max_range tables and both
// scalefac_scale / preflag settings.
func TestSetScalefacs(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(26, 27))
	for _, sel := range []int{0, 1, 2} {
		_, window, psymax, _ := longGranule()
		sfbmaxGeom := sbmaxL
		if sel == 0 {
			_, window, psymax, _ = shortGranule()
			sfbmaxGeom = sfbmax
		}
		for trial := 0; trial < 300; trial++ {
			globalGain := 120 + rng.IntN(100)
			scalefacScale := rng.IntN(2)
			preflag := 0
			if sel != 0 {
				preflag = rng.IntN(2)
			}
			sf := make([]int, sfbmax)
			vbrsfmin := make([]int, sfbmax)
			for i := range sf {
				sf[i] = -rng.IntN(60)
				vbrsfmin[i] = rng.IntN(40)
			}
			cg, gg := cgoNewHandle(), goNewHandle()
			csf := append([]int(nil), sf...)
			gsf := append([]int(nil), sf...)
			cg.setWindow(window)
			cg.setGeom(0, globalGain, scalefacScale, preflag, sfbmaxGeom, psymax, 0)
			gg.setWindow(window)
			gg.setGeom(0, globalGain, scalefacScale, preflag, sfbmaxGeom, psymax, 0)

			cg.setScalefacsK(vbrsfmin, csf, sel)
			gg.setScalefacsK(vbrsfmin, gsf, sel)

			assert.Equal(t, csf, gsf, "set_scalefacs sf sel=%d trial=%d", sel, trial)
			for i := 0; i < sfbmax; i++ {
				assert.Equal(t, cg.scalefac(i), gg.scalefac(i),
					"set_scalefacs scalefac[%d] sel=%d trial=%d", i, sel, trial)
			}
			cg.free()
		}
	}
}

// TestCheckScalefactor pins the over-amplification predicate.
func TestCheckScalefactor(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(28, 29))
	_, window, psymax, _ := longGranule()
	for trial := 0; trial < 500; trial++ {
		cg, gg := cgoNewHandle(), goNewHandle()
		globalGain := 100 + rng.IntN(120)
		scalefacScale := rng.IntN(2)
		preflag := rng.IntN(2)
		scalefac := make([]int, sfbmax)
		vbrsfmin := make([]int, sfbmax)
		for i := range scalefac {
			scalefac[i] = rng.IntN(16)
			vbrsfmin[i] = rng.IntN(120)
		}
		cg.setWindow(window)
		cg.setGeom(0, globalGain, scalefacScale, preflag, sbmaxL, psymax, 0)
		cg.setScalefac(scalefac)
		gg.setWindow(window)
		gg.setGeom(0, globalGain, scalefacScale, preflag, sbmaxL, psymax, 0)
		gg.setScalefac(scalefac)

		assert.Equal(t, cg.checkScalefactor(vbrsfmin), gg.checkScalefactor(vbrsfmin),
			"checkScalefactor trial=%d", trial)
		cg.free()
	}
}

// VbrFindFull mirrors parityhooks' VbrSfFindFull selector (find_scalefac_x34).
const VbrFindFull = 1
