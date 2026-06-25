// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbriterationloop

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 VBR iteration DRIVERS
// (quantize_encode_vbr.go: vbrNewIterationLoop / vbrOldIterationLoop and their
// static prepares / bitpressure_strategy / vbrEncodeGranule, a 1:1 port of
// libmp3lame/quantize.c's VBR_*_iteration_loop) against the vendored LAME C
// reference (oracle.c + oracle_vbrquantize.c + oracle_takehiro.c, which compile
// the genuine quantize.c + quantize_pvt.c + reservoir.c + vbrquantize.c +
// takehiro.c + tables.c).
//
// VBR_new_iteration_loop is the vbr_mtrh (-V) per-frame driver: it prepares the
// budgets (VBR_new_prepare -> calc_xmin / on_pe), fills xrpow per granule, runs
// the whole-frame VBR_encode_frame quantization, picks the lowest bitrate able to
// hold the used bits (with reservoir padding) and finalises the reservoir. Both
// sides receive byte-identical state (cfg, sv_qnt, ATH, reservoir, scalefac_band,
// huffman_init, the per-(gr,ch) MDCT lines + psy ratio + block type), and the
// frame output — per-(gr,ch) resolved side info (global_gain / scalefac[] /
// subblock_gain[] / scalefac_scale / preflag / scalefac_compress / table_select /
// region counts), the quantized spectrum (l3_enc[]), the bit lengths
// (part2_3_length / part2_length), the chosen eov->bitrate_index and the
// post-frame reservoir size — must match bit-for-bit. The byte-identical -V2
// bitstream depends on every one of these.
//
// The slice is floating-point-bearing (the prepares' masking adjust + pow(10,.),
// bitpressure's xmin inflation, and the whole quantization the loop drives), so
// the bit-exact assertions are gated behind nativemp3.StrictMode: a bare
// `go test` is clean and the strict run (mp3lame + mp3_strict + the FP CGO env,
// the //libraries/mp3:parity task) is the authoritative gate.

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

// shortBands44 is the 44.1 kHz MPEG-1 short band boundary table (.S).
var shortBands44 = [sbmaxS + 1]int{
	0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192,
}

func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:parity)")
	}
}

func init() {
	cgoFillTables()
	goFillTables()
}

// fabricateLines fills xr over n low-concentrated, decaying MDCT magnitudes so
// the bit demand stays inside a realistic frame budget (a flat high-magnitude
// spectrum can demand more bits than any feasible budget, tripping the C
// "should never happen" exit).
func fabricateLines(rng *rand.Rand, n int, mag float64) []float32 {
	xr := make([]float32, 576)
	for i := 0; i < n; i++ {
		decay := math.Pow(0.01, float64(i)/float64(n))
		v := mag * decay * rng.Float64()
		if rng.IntN(3) == 0 {
			v = 0
		}
		xr[i] = float32(v)
	}
	return xr
}

// fabricateRatio builds a plausible psy ratio for a long block: en/thm per band
// derived from the band energy with a masking headroom, so calc_xmin produces a
// realistic l3_xmin (a few bands "athOver"). en.l is the band energy, thm.l a
// fraction of it (the masking threshold).
func fabricateRatio(xr []float32) (enL, thmL []float32) {
	enL = make([]float32, sbmaxL)
	thmL = make([]float32, sbmaxL)
	for sfb := 0; sfb < sbmaxL; sfb++ {
		var e float64
		for j := longBands44[sfb]; j < longBands44[sfb+1]; j++ {
			e += float64(xr[j]) * float64(xr[j])
		}
		enL[sfb] = float32(e)
		thmL[sfb] = float32(e * 0.25) // 6 dB masking headroom
	}
	return enL, thmL
}

// athLong/athShort are flat low ATH floors so calc_xmin's athAdjust contributes
// a small constant masking floor (the realistic ATH curve is computed by
// compute_ath which the loop never runs; the test supplies a stable floor).
func athTables() (l, s []float32) {
	l = make([]float32, sbmaxL)
	s = make([]float32, sbmaxS)
	for i := range l {
		l[i] = 1e-3
	}
	for i := range s {
		s[i] = 1e-3
	}
	return l, s
}

// setupFrame populates both handles identically with a 44.1 kHz MPEG-1 stereo
// vbr_mtrh frame (two long-block granules) and returns the per-frame pe / mer
// inputs.
func setupFrame(t *testing.T, seed uint64) (cg *cgoHandle, gh *goHandle, pe, mer []float32) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))

	cg = cgoNewHandle()
	gh = goNewHandle()

	const (
		modeGr      = 2
		channels    = 2
		version     = 1 // MPEG-1
		srate       = 44100
		avgBitrate  = 128
		sideinfoLen = 32 // MPEG-1 stereo side info bytes
		bufCon      = 8 * 2880
		vbrMin      = 1
		vbrMax      = 14
		modeExtLR   = 0 // MPG_MD_LR_LR (no M/S, keeps the test deterministic)
	)
	for _, set := range []func(){
		func() {
			cg.setCfg(modeGr, channels, version, srate, avgBitrate, sideinfoLen, bufCon,
				vbrMin, vbrMax, 0, 0, 0, modeExtLR)
			gh.setCfg(modeGr, channels, version, srate, avgBitrate, sideinfoLen, bufCon,
				vbrMin, vbrMax, 0, 0, 0, modeExtLR)
		},
		func() {
			cg.setCfgQuant(2, 0, 1, 0.0, 4.0, 4) // noise_shaping=2, full_outer_loop=0
			gh.setCfgQuant(2, 0, 1, 0.0, 4.0, 4)
		},
		func() {
			cg.setResv(0, 0)
			gh.setResv(0, 0)
		},
		func() {
			cg.setBinsearch(180, 4) // lame_init_params primes bin_search carry
			gh.setBinsearch(180, 4)
		},
		func() {
			cg.setSvQnt(0.0, 0.0, 0, 1) // mask_adjust 0, sfb21_extra=1 (44.1k)
			gh.setSvQnt(0.0, 0.0, 0, 1)
		},
	} {
		set()
	}

	// nspsytune masking factors: all 1.0 (no per-band tweak).
	lf := make([]float32, sbmaxL)
	sf := make([]float32, sbmaxS)
	for i := range lf {
		lf[i] = 1.0
	}
	for i := range sf {
		sf[i] = 1.0
	}
	cg.setLongfact(lf)
	gh.setLongfact(lf)
	cg.setShortfact(sf)
	gh.setShortfact(sf)

	athL, athS := athTables()
	cg.setATH(1.0, 1e-6, athL, athS)
	gh.setATH(1.0, 1e-6, athL, athS)

	cg.setSfbLong(longBands44[:])
	gh.setSfbLong(longBands44[:])
	cg.setSfbShort(shortBands44[:])
	gh.setSfbShort(shortBands44[:])
	cg.huffmanInit()
	gh.huffmanInit()

	for gr := 0; gr < modeGr; gr++ {
		for ch := 0; ch < channels; ch++ {
			xr := fabricateLines(rng, 400, 800.0)
			enL, thmL := fabricateRatio(xr)
			cg.setGeom(gr, ch, 0, 0) // NORM_TYPE long block
			gh.setGeom(gr, ch, 0, 0)
			cg.setXr(gr, ch, xr)
			gh.setXr(gr, ch, xr)
			cg.setRatioL(gr, ch, enL, thmL)
			gh.setRatioL(gr, ch, enL, thmL)
			// short ratio left zero (long blocks ignore it).
		}
	}

	pe = []float32{1200, 1100, 1300, 1000}
	mer = []float32{0.5, 0.5}
	return cg, gh, pe, mer
}

func assertFrameMatch(t *testing.T, cg *cgoHandle, gh *goHandle) {
	t.Helper()
	assert.Equal(t, cg.bitrateIndex(), gh.bitrateIndex(), "bitrate_index")
	assert.Equal(t, cg.resvSize(), gh.resvSize(), "ResvSize")
	for gr := 0; gr < 2; gr++ {
		for ch := 0; ch < 2; ch++ {
			assert.Equalf(t, cg.globalGain(gr, ch), gh.globalGain(gr, ch), "global_gain[%d][%d]", gr, ch)
			assert.Equalf(t, cg.scalefacScale(gr, ch), gh.scalefacScale(gr, ch), "scalefac_scale[%d][%d]", gr, ch)
			assert.Equalf(t, cg.preflag(gr, ch), gh.preflag(gr, ch), "preflag[%d][%d]", gr, ch)
			assert.Equalf(t, cg.blockType(gr, ch), gh.blockType(gr, ch), "block_type[%d][%d]", gr, ch)
			assert.Equalf(t, cg.part23Length(gr, ch), gh.part23Length(gr, ch), "part2_3_length[%d][%d]", gr, ch)
			assert.Equalf(t, cg.part2Length(gr, ch), gh.part2Length(gr, ch), "part2_length[%d][%d]", gr, ch)
			assert.Equalf(t, cg.scalefacCompress(gr, ch), gh.scalefacCompress(gr, ch), "scalefac_compress[%d][%d]", gr, ch)
			assert.Equalf(t, cg.bigValues(gr, ch), gh.bigValues(gr, ch), "big_values[%d][%d]", gr, ch)
			assert.Equalf(t, cg.region0Count(gr, ch), gh.region0Count(gr, ch), "region0_count[%d][%d]", gr, ch)
			assert.Equalf(t, cg.region1Count(gr, ch), gh.region1Count(gr, ch), "region1_count[%d][%d]", gr, ch)
			for i := 0; i < 3; i++ {
				assert.Equalf(t, cg.subblockGain(gr, ch, i), gh.subblockGain(gr, ch, i), "subblock_gain[%d][%d][%d]", gr, ch, i)
				assert.Equalf(t, cg.tableSelect(gr, ch, i), gh.tableSelect(gr, ch, i), "table_select[%d][%d][%d]", gr, ch, i)
			}
			for sfb := 0; sfb < sfbmax; sfb++ {
				assert.Equalf(t, cg.scalefac(gr, ch, sfb), gh.scalefac(gr, ch, sfb), "scalefac[%d][%d][%d]", gr, ch, sfb)
			}
			for i := 0; i < 576; i++ {
				if cg.l3enc(gr, ch, i) != gh.l3enc(gr, ch, i) {
					assert.Equalf(t, cg.l3enc(gr, ch, i), gh.l3enc(gr, ch, i), "l3_enc[%d][%d][%d]", gr, ch, i)
					break
				}
			}
		}
	}
}

// TestVBRNewIterationLoop pins the vbr_mtrh (-V) per-frame iteration loop EXACT
// vs LAME across several pseudo-random 44.1 kHz stereo frames.
func TestVBRNewIterationLoop(t *testing.T) {
	requireStrict(t)
	for _, seed := range []uint64{1, 2, 3, 7, 42, 99} {
		cg, gh, pe, mer := setupFrame(t, seed)
		cg.runNew(pe, mer)
		gh.runNew(pe, mer)
		assertFrameMatch(t, cg, gh)
		cg.free()
	}
}

// TestVBROldIterationLoop pins the vbr_rh per-frame iteration loop EXACT vs LAME.
func TestVBROldIterationLoop(t *testing.T) {
	requireStrict(t)
	for _, seed := range []uint64{1, 5, 11, 23} {
		cg, gh, pe, mer := setupFrame(t, seed)
		cg.runOld(pe, mer)
		gh.runOld(pe, mer)
		assertFrameMatch(t, cg, gh)
		cg.free()
	}
}
