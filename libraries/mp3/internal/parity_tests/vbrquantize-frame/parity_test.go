// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package vbrquantizeframe

import (
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 VBR quantizer bit-search
// orchestration tier (vbrquantize_frame.go: tryGlobalStepsize ..
// outOfBitsStrategy, reduce_bit_usage and VBR_encode_frame, a 1:1 port of the
// top tier of libmp3lame/vbrquantize.c) against the vendored LAME C reference
// (oracle.c, which compiles the genuine vbrquantize.c + takehiro.c + tables.c).
//
// VBR_encode_frame is the whole-frame vbr_mtrh entry: for each granule/channel
// it surveys scalefactors (block_sf), allocates side info, encodes 'as is',
// reduces bit usage (best_scalefac_store + best_huffman_divide), and — when the
// frame overflows the per-channel / per-granule / per-frame budget —
// redistributes the bits and re-runs the out-of-bits flatten search. Both sides
// receive byte-identical inputs (cfg, scalefac_band, huffman_init, the 2x2
// gr_info geometry, and the per-granule xr34orig / l3_xmin / max_bits), and the
// resolved per-granule side info (global_gain / scalefac[] / subblock_gain[] /
// scalefac_scale / preflag / scalefac_compress / table_select / region counts),
// the quantized spectrum (l3_enc[]), the bit lengths (part2_3_length /
// part2_length) and the function's bit-usage return value must match bit-for-bit
// — the byte-identical -V2 bitstream depends on every one of these.
//
// The slice is floating-point-bearing (the 'as is' quantize's float32 product +
// the TAKEHIRO_IEEE754_HACK magic-float add, and the out-of-budget redistribution
// sqrt weights), so the bit-exact assertions are gated behind nativemp3.StrictMode:
// a bare `go test` is clean and the strict run (mp3lame + mp3_strict + the FP CGO
// env, the //libraries/mp3:parity task) is the authoritative gate.

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

// longGranule builds the per-band geometry of a 44.1 kHz long block.
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

// shortGranule builds the per-band geometry of a 44.1 kHz short block.
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
	maxNZ = 3*shortBands44[sbmaxS] - 1
	return width, window, psymax, maxNZ
}

// fabricateLines fills xr / xr34 over n lines with plausible MDCT magnitudes and
// returns the granule's max xr34 (the xrpow_max VBR_encode_frame saves/restores).
// Energy concentrates in the low lines and decays toward the top (as a real MDCT
// spectrum does) so the bit demand stays inside a realistic per-channel budget —
// otherwise a flat high-magnitude spectrum can demand more bits than any budget
// the out-of-bits strategy can reach, tripping VBR_encode_frame's internal
// "should never happen" assert/exit on the C oracle.
func fabricateLines(rng *rand.Rand, n int, mag float64) (xr, xr34 []float32, xrpowMax float32) {
	xr = make([]float32, 576)
	xr34 = make([]float32, 576)
	for i := 0; i < n; i++ {
		// decay envelope: full magnitude near DC, ~1/100 by the top line.
		decay := math.Pow(0.01, float64(i)/float64(n))
		v := mag * decay * rng.Float64()
		if rng.IntN(3) == 0 {
			v = 0 // sparse spectrum: many exactly-zero lines
		}
		xr[i] = float32(v)
		xr34[i] = float32(math.Pow(float64(xr[i]), 0.75))
		if xr34[i] > xrpowMax {
			xrpowMax = xr34[i]
		}
	}
	return xr, xr34, xrpowMax
}

// fabricateL3xmin builds an SFBMAX per-band allowed-distortion budget. The expo
// range is parameterised so a test can make the budget tight (small l3_xmin ->
// big bit demand -> triggers the out-of-bits redistribution) or loose.
func fabricateL3xmin(rng *rand.Rand, loExp, hiExp float64) []float32 {
	x := make([]float32, sfbmax)
	for i := range x {
		x[i] = float32(math.Pow(10, loExp+rng.Float64()*(hiExp-loExp)))
	}
	return x
}

// granuleInput bundles one granule/channel's fabricated inputs + geometry.
type granuleInput struct {
	xr, xr34, l3xmin []float32
	eac              []byte
	width, window    []int
	psymax, maxNZ    int
	blockType        int
	xrpowMax         float32
}

func fabricateGranule(rng *rand.Rand, short bool, tightXmin bool) granuleInput {
	var gi granuleInput
	if short {
		gi.width, gi.window, gi.psymax, gi.maxNZ = shortGranule()
		gi.blockType = 2
	} else {
		gi.width, gi.window, gi.psymax, gi.maxNZ = longGranule()
		gi.blockType = 0
	}
	mag := math.Pow(10, rng.Float64()*2)
	gi.xr, gi.xr34, gi.xrpowMax = fabricateLines(rng, gi.maxNZ+1, mag)
	if tightXmin {
		gi.l3xmin = fabricateL3xmin(rng, 0, 3)
	} else {
		gi.l3xmin = fabricateL3xmin(rng, 3, 7)
	}
	gi.eac = make([]byte, sfbmax)
	for i := 0; i < gi.psymax; i++ {
		if rng.IntN(10) == 0 {
			gi.eac[i] = 0
		} else {
			gi.eac[i] = 1
		}
	}
	return gi
}

// frameSetup populates a handle (cgo or go) with cfg + scalefac_band +
// huffman_init + the per-granule geometry/inputs, and returns the flat input
// arrays VBR_encode_frame takes.
type handle interface {
	setCfg(modeGr, channelsOut, noiseShaping, fullOuterLoop, useBestHuffman int)
	setSfbLong(l []int)
	setSfbShort(s []int)
	huffmanInit()
	setXr(gr, ch int, xr []float32)
	setWidth(gr, ch int, w []int)
	setWindow(gr, ch int, win []int)
	setEac(gr, ch int, eac []byte)
	setGeom(gr, ch, blockType, mixedBlockFlag, sfbmax, sfbdivide, psymax, maxNonzeroCoeff int, xrpowMax float32)
	encode(xr34orig, l3Xmin []float32, maxBits []int) int
}

func setupFrame(h handle, ngr, nch, noiseShaping, fullOuterLoop, useBestHuffman int,
	grans [2][2]granuleInput, maxBits []int) (xr34flat, xminflat []float32) {
	h.setCfg(ngr, nch, noiseShaping, fullOuterLoop, useBestHuffman)
	h.setSfbLong(longBands44[:])
	h.setSfbShort(shortBands44[:])
	h.huffmanInit()
	xr34flat = make([]float32, 2*2*576)
	xminflat = make([]float32, 2*2*sfbmax)
	for gr := 0; gr < 2; gr++ {
		for ch := 0; ch < 2; ch++ {
			gi := grans[gr][ch]
			if gr >= ngr || ch >= nch {
				// zero geometry for unused slots; max_bits is 0 so VBR_encode_frame
				// skips them, but the arrays must still be sized.
				continue
			}
			h.setXr(gr, ch, gi.xr)
			h.setWidth(gr, ch, gi.width)
			h.setWindow(gr, ch, gi.window)
			h.setEac(gr, ch, gi.eac)
			// sfbdivide (quantize.c:264/296): 11 for long blocks, sfbmax-18 for
			// short. It splits scalefac[] into the slen1 / slen2 partitions
			// mpeg1_scale_bitcount counts; a wrong value over-amplifies a
			// partition and trips bitcount's "should never happen" exit.
			sfbdivide := 11
			if gi.blockType == 2 {
				sfbdivide = gi.psymax - 18
			}
			h.setGeom(gr, ch, gi.blockType, 0, gi.psymax, sfbdivide, gi.psymax, gi.maxNZ, gi.xrpowMax)
			copy(xr34flat[(gr*2+ch)*576:], gi.xr34)
			copy(xminflat[(gr*2+ch)*sfbmax:], gi.l3xmin)
		}
	}
	return xr34flat, xminflat
}

// frameResult captures every side-info field the byte-identical bitstream depends
// on, for one frame.
type frameResult struct {
	usedBits int
	gg       [2][2]int
	sfScale  [2][2]int
	preflag  [2][2]int
	sfComp   [2][2]int
	bigVals  [2][2]int
	r0       [2][2]int
	r1       [2][2]int
	p23      [2][2]int
	p2       [2][2]int
	sf       [2][2][sfbmax]int
	sbg      [2][2][3]int
	tsel     [2][2][3]int
	l3enc    [2][2][576]int
	scfsi    [2][4]int
}

// reader is the getter surface both handles share.
type reader interface {
	globalGain(gr, ch int) int
	scalefacScale(gr, ch int) int
	preflag(gr, ch int) int
	scalefac(gr, ch, sfb int) int
	subblockGain(gr, ch, i int) int
	l3enc(gr, ch, i int) int
	part23Length(gr, ch int) int
	part2Length(gr, ch int) int
	scalefacCompress(gr, ch int) int
	bigValues(gr, ch int) int
	tableSelect(gr, ch, i int) int
	region0Count(gr, ch int) int
	region1Count(gr, ch int) int
	scfsi(ch, band int) int
}

func readFrame(r reader, used, ngr, nch int) frameResult {
	var fr frameResult
	fr.usedBits = used
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			fr.gg[gr][ch] = r.globalGain(gr, ch)
			fr.sfScale[gr][ch] = r.scalefacScale(gr, ch)
			fr.preflag[gr][ch] = r.preflag(gr, ch)
			fr.sfComp[gr][ch] = r.scalefacCompress(gr, ch)
			fr.bigVals[gr][ch] = r.bigValues(gr, ch)
			fr.r0[gr][ch] = r.region0Count(gr, ch)
			fr.r1[gr][ch] = r.region1Count(gr, ch)
			fr.p23[gr][ch] = r.part23Length(gr, ch)
			fr.p2[gr][ch] = r.part2Length(gr, ch)
			for i := 0; i < sfbmax; i++ {
				fr.sf[gr][ch][i] = r.scalefac(gr, ch, i)
			}
			for i := 0; i < 3; i++ {
				fr.sbg[gr][ch][i] = r.subblockGain(gr, ch, i)
				fr.tsel[gr][ch][i] = r.tableSelect(gr, ch, i)
			}
			for i := 0; i < 576; i++ {
				fr.l3enc[gr][ch][i] = r.l3enc(gr, ch, i)
			}
		}
	}
	for ch := 0; ch < nch; ch++ {
		for band := 0; band < 4; band++ {
			fr.scfsi[ch][band] = r.scfsi(ch, band)
		}
	}
	return fr
}

// runFrame drives both handles over identical inputs and returns the two
// frameResults.
func runFrame(t *testing.T, rng *rand.Rand, ngr, nch, noiseShaping, fullOuterLoop, useBestHuffman int,
	grans [2][2]granuleInput, maxBits []int) (frameResult, frameResult) {
	t.Helper()
	cg := cgoNewHandle()
	defer cg.free()
	gg := goNewHandle()

	cxr34, cxmin := setupFrame(cg, ngr, nch, noiseShaping, fullOuterLoop, useBestHuffman, grans, maxBits)
	gxr34, gxmin := setupFrame(gg, ngr, nch, noiseShaping, fullOuterLoop, useBestHuffman, grans, maxBits)

	cused := cg.encode(cxr34, cxmin, maxBits)
	gused := gg.encode(gxr34, gxmin, maxBits)

	return readFrame(cg, cused, ngr, nch), readFrame(gg, gused, ngr, nch)
}

func assertFrameEqual(t *testing.T, c, g frameResult, ngr, nch int, label string) {
	t.Helper()
	require.Equal(t, c.usedBits, g.usedBits, "%s: used bits", label)
	for gr := 0; gr < ngr; gr++ {
		for ch := 0; ch < nch; ch++ {
			assert.Equal(t, c.gg[gr][ch], g.gg[gr][ch], "%s: global_gain[%d][%d]", label, gr, ch)
			assert.Equal(t, c.sfScale[gr][ch], g.sfScale[gr][ch], "%s: scalefac_scale[%d][%d]", label, gr, ch)
			assert.Equal(t, c.preflag[gr][ch], g.preflag[gr][ch], "%s: preflag[%d][%d]", label, gr, ch)
			assert.Equal(t, c.sfComp[gr][ch], g.sfComp[gr][ch], "%s: scalefac_compress[%d][%d]", label, gr, ch)
			assert.Equal(t, c.bigVals[gr][ch], g.bigVals[gr][ch], "%s: big_values[%d][%d]", label, gr, ch)
			assert.Equal(t, c.r0[gr][ch], g.r0[gr][ch], "%s: region0_count[%d][%d]", label, gr, ch)
			assert.Equal(t, c.r1[gr][ch], g.r1[gr][ch], "%s: region1_count[%d][%d]", label, gr, ch)
			assert.Equal(t, c.p23[gr][ch], g.p23[gr][ch], "%s: part2_3_length[%d][%d]", label, gr, ch)
			assert.Equal(t, c.p2[gr][ch], g.p2[gr][ch], "%s: part2_length[%d][%d]", label, gr, ch)
			assert.Equal(t, c.sf[gr][ch], g.sf[gr][ch], "%s: scalefac[%d][%d]", label, gr, ch)
			assert.Equal(t, c.sbg[gr][ch], g.sbg[gr][ch], "%s: subblock_gain[%d][%d]", label, gr, ch)
			assert.Equal(t, c.tsel[gr][ch], g.tsel[gr][ch], "%s: table_select[%d][%d]", label, gr, ch)
			assert.Equal(t, c.l3enc[gr][ch], g.l3enc[gr][ch], "%s: l3_enc[%d][%d]", label, gr, ch)
		}
	}
	for ch := 0; ch < nch; ch++ {
		assert.Equal(t, c.scfsi[ch], g.scfsi[ch], "%s: scfsi[%d]", label, ch)
	}
}

// TestVBREncodeFrameLong drives a 2-granule 2-channel MPEG-1 long-block frame
// with a loose distortion budget (the 'as is' encode fits, exercising the
// fast-return path) and a tight budget (forces the out-of-bits redistribution).
func TestVBREncodeFrameLong(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(40, 41))
	for _, tight := range []bool{false, true} {
		for _, fullOuter := range []int{0, -1} {
			for trial := 0; trial < 40; trial++ {
				var grans [2][2]granuleInput
				for gr := 0; gr < 2; gr++ {
					for ch := 0; ch < 2; ch++ {
						grans[gr][ch] = fabricateGranule(rng, false, tight)
					}
				}
				maxBits := []int{1400, 1400, 1400, 1400}
				if tight {
					maxBits = []int{800, 800, 800, 800}
				}
				c, g := runFrame(t, rng, 2, 2, 1, fullOuter, 1, grans, maxBits)
				assertFrameEqual(t, c, g, 2, 2, "long tight="+boolStr(tight)+" fullOuter="+itoa(fullOuter)+" trial="+itoa(trial))
			}
		}
	}
}

// TestVBREncodeFrameShort drives a short-block frame (subblock_gain path) with
// noise_shaping 2 (allows scalefac_scale=1) under both budgets.
func TestVBREncodeFrameShort(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(42, 43))
	for _, tight := range []bool{false, true} {
		for _, noiseShaping := range []int{1, 2} {
			for trial := 0; trial < 40; trial++ {
				var grans [2][2]granuleInput
				for gr := 0; gr < 2; gr++ {
					for ch := 0; ch < 2; ch++ {
						grans[gr][ch] = fabricateGranule(rng, true, tight)
					}
				}
				maxBits := []int{1400, 1400, 1400, 1400}
				if tight {
					maxBits = []int{760, 760, 760, 760}
				}
				c, g := runFrame(t, rng, 2, 2, noiseShaping, 0, 1, grans, maxBits)
				assertFrameEqual(t, c, g, 2, 2, "short tight="+boolStr(tight)+" ns="+itoa(noiseShaping)+" trial="+itoa(trial))
			}
		}
	}
}

// TestVBREncodeFrameMixed drives mixed long/short granules across channels and a
// mono (nch=1) frame, plus asymmetric per-channel budgets that exercise the
// per-channel redistribution arms.
func TestVBREncodeFrameMixed(t *testing.T) {
	requireStrict(t)
	rng := rand.New(rand.NewPCG(44, 45))
	for trial := 0; trial < 60; trial++ {
		var grans [2][2]granuleInput
		for gr := 0; gr < 2; gr++ {
			for ch := 0; ch < 2; ch++ {
				short := (gr+ch)%2 == 0
				grans[gr][ch] = fabricateGranule(rng, short, trial%2 == 0)
			}
		}
		maxBits := []int{1100, 600, 600, 1100}
		c, g := runFrame(t, rng, 2, 2, 1, 0, 1, grans, maxBits)
		assertFrameEqual(t, c, g, 2, 2, "mixed trial="+itoa(trial))
	}
	// mono frame
	for trial := 0; trial < 40; trial++ {
		var grans [2][2]granuleInput
		for gr := 0; gr < 2; gr++ {
			grans[gr][0] = fabricateGranule(rng, trial%3 == 0, trial%2 == 0)
		}
		maxBits := []int{1000, 0, 1000, 0}
		if trial%2 == 0 {
			maxBits = []int{700, 0, 700, 0}
		}
		c, g := runFrame(t, rng, 2, 1, 1, 0, 1, grans, maxBits)
		assertFrameEqual(t, c, g, 2, 1, "mono trial="+itoa(trial))
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [12]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
