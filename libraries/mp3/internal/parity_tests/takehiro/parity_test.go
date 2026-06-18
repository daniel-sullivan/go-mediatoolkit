// SPDX-License-Identifier: LGPL-2.0-or-later

//go:build cgo && mp3lame

package takehiro

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// These parity tests pin the pure-Go nativemp3 takehiro bit-counting routines
// (takehiro.go: huffman_init / choose_table / noquant_count_bits /
// scale_bitcount / best_huffman_divide / best_scalefac_store, a 1:1 port of
// libmp3lame/takehiro.c's integer core) against the vendored LAME C reference
// (oracle.c). Each routine is driven on both sides over identical fabricated
// gr_info + scalefac_band input, and the full filled side information must be
// bit-for-bit equal.
//
// The slice is integer-only — pure table lookups and shifting, no floating
// point — so its results are independent of FMA/vectorization. The bit-exact
// assertions are nonetheless gated behind nativemp3.StrictMode per the
// FP-parity convention, so a bare `go test` is clean and the strict run
// (mp3lame + mp3_strict + the FP CGO env) is the authoritative gate.

// sfbLong44 is LAME's 44.1 kHz MPEG-1 long scalefactor-band boundary table
// (Table B.8.b, scalefac_band.l), the realistic input huffman_init and the
// region-split logic walk.
var sfbLong44 = []int{
	0, 4, 8, 12, 16, 20, 24, 30, 36, 44, 52, 62, 74, 90, 110, 134, 162, 196, 238, 288, 342, 418, 576,
}

var sfbShort44 = []int{
	0, 4, 8, 12, 16, 22, 30, 40, 52, 66, 84, 106, 136, 192,
}

func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("bit-exact parity asserted only under -tags=mp3_strict (FP env via mise //libraries/mp3:parity)")
	}
}

// fabricateL3Enc builds a 576-entry quantized-coefficient array whose nonzero
// tail and magnitude distribution exercise the count1 region, the no-ESC
// tables and (when maxMag is large) the ESC tables.
func fabricateL3Enc(rng *rand.Rand, nonzeroLen, maxMag int) []int {
	ix := make([]int, 576)
	for i := 0; i < nonzeroLen && i < 576; i++ {
		// bias toward small magnitudes so count1 (0/1) and the small no-ESC
		// tables are reached, with occasional large values for ESC.
		switch rng.IntN(4) {
		case 0:
			ix[i] = 0
		case 1:
			ix[i] = rng.IntN(2)
		case 2:
			ix[i] = rng.IntN(16)
		default:
			ix[i] = rng.IntN(maxMag + 1)
		}
	}
	return ix
}

func TestHuffmanInitBvScf(t *testing.T) {
	requireStrict(t)

	c := newCgoTk()
	defer c.free()
	n := newNativeTk()

	c.setSfbLong(sfbLong44)
	n.setSfbLong(sfbLong44)

	c.huffmanInit()
	n.huffmanInit()

	for i := 0; i < 576; i++ {
		require.Equalf(t, c.bvScf(i), n.bvScf(i), "bv_scf[%d]", i)
	}
}

func TestChooseTable(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0x7a, 0x4e))
	for iter := 0; iter < 400; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		// even, non-empty region length
		half := 1 + rng.IntN(120)
		end := half * 2
		maxMag := []int{1, 3, 15, 100, 8000}[rng.IntN(5)]
		ix := fabricateL3Enc(rng, end, maxMag)

		c.setL3Enc(0, 0, ix)
		n.setL3Enc(0, 0, ix)

		cbits, nbits := rng.IntN(50), 0
		nbits = cbits
		ct := c.chooseTable(0, 0, 0, end, &cbits)
		nt := n.chooseTable(0, 0, 0, end, &nbits)

		assert.Equalf(t, ct, nt, "iter %d: table index (end=%d maxMag=%d)", iter, end, maxMag)
		assert.Equalf(t, cbits, nbits, "iter %d: accumulated bits (end=%d maxMag=%d)", iter, end, maxMag)
		c.free()
	}
}

func TestNoquantCountBits(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0x123, 0x456))
	for iter := 0; iter < 400; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setSfbLong(sfbLong44)
		n.setSfbLong(sfbLong44)
		c.setSfbShort(sfbShort44)
		n.setSfbShort(sfbShort44)
		c.huffmanInit()
		n.huffmanInit()

		blockType := []int{0, 2, 1}[rng.IntN(3)] // NORM / SHORT / START
		nonzero := 4 + rng.IntN(572)
		maxMag := []int{1, 3, 15, 100, 8000}[rng.IntN(5)]
		ix := fabricateL3Enc(rng, nonzero, maxMag)

		c.setL3Enc(0, 0, ix)
		n.setL3Enc(0, 0, ix)
		// max_nonzero_coeff is the index past the last nonzero; use 575 to let
		// noquant_count_bits scan the whole granule.
		c.setGeom(0, 0, blockType, 0, 210, 0, 0, 21, 11, 575, 0)
		n.setGeom(0, 0, blockType, 0, 210, 0, 0, 21, 11, 575, 0)

		cb := c.noquantCountBits(0, 0)
		nb := n.noquantCountBits(0, 0)

		assert.Equalf(t, cb, nb, "iter %d: bits (blockType=%d maxMag=%d)", iter, blockType, maxMag)
		assert.Equalf(t, c.bigValues(0, 0), n.bigValues(0, 0), "iter %d: big_values", iter)
		assert.Equalf(t, c.count1(0, 0), n.count1(0, 0), "iter %d: count1", iter)
		assert.Equalf(t, c.count1bits(0, 0), n.count1bits(0, 0), "iter %d: count1bits", iter)
		assert.Equalf(t, c.count1tableSelect(0, 0), n.count1tableSelect(0, 0), "iter %d: count1table_select", iter)
		assert.Equalf(t, c.region0Count(0, 0), n.region0Count(0, 0), "iter %d: region0_count", iter)
		assert.Equalf(t, c.region1Count(0, 0), n.region1Count(0, 0), "iter %d: region1_count", iter)
		for i := 0; i < 3; i++ {
			assert.Equalf(t, c.tableSelect(0, 0, i), n.tableSelect(0, 0, i), "iter %d: table_select[%d]", iter, i)
		}
		c.free()
	}
}

func TestNoquantCountBitsBestHuffman(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0xfeed, 0xbeef))
	for iter := 0; iter < 300; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setCfg(2, 2) // mode_gr=2, use_best_huffman=2 -> noquant calls best_huffman_divide
		n.setCfg(2, 2)
		c.setSfbLong(sfbLong44)
		n.setSfbLong(sfbLong44)
		c.huffmanInit()
		n.huffmanInit()

		nonzero := 8 + rng.IntN(560)
		maxMag := []int{1, 3, 15, 60}[rng.IntN(4)]
		ix := fabricateL3Enc(rng, nonzero, maxMag)
		c.setL3Enc(0, 0, ix)
		n.setL3Enc(0, 0, ix)
		c.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, 0) // NORM_TYPE
		n.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, 0)

		cb := c.noquantCountBits(0, 0)
		nb := n.noquantCountBits(0, 0)
		assert.Equalf(t, cb, nb, "iter %d: bits", iter)
		assert.Equalf(t, c.region0Count(0, 0), n.region0Count(0, 0), "iter %d: region0_count", iter)
		assert.Equalf(t, c.region1Count(0, 0), n.region1Count(0, 0), "iter %d: region1_count", iter)
		for i := 0; i < 3; i++ {
			assert.Equalf(t, c.tableSelect(0, 0, i), n.tableSelect(0, 0, i), "iter %d: table_select[%d]", iter, i)
		}
		c.free()
	}
}

func TestScaleBitcountMPEG1(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0x11, 0x22))
	for iter := 0; iter < 400; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setCfg(2, 0) // mode_gr=2 -> mpeg1_scale_bitcount
		n.setCfg(2, 0)

		blockType := []int{0, 2}[rng.IntN(2)]
		mixed := rng.IntN(2)
		preflag := rng.IntN(2)
		sf := make([]int, 39)
		for i := range sf {
			sf[i] = rng.IntN(8) // non-negative (mpeg1 asserts >= 0)
		}
		c.setScalefac(0, 0, sf)
		n.setScalefac(0, 0, sf)
		c.setGeom(0, 0, blockType, mixed, 210, 0, preflag, 21, 11, 575, 0)
		n.setGeom(0, 0, blockType, mixed, 210, 0, preflag, 21, 11, 575, 0)

		cr := c.scaleBitcount(0, 0)
		nr := n.scaleBitcount(0, 0)
		assert.Equalf(t, cr, nr, "iter %d: over flag (bt=%d mixed=%d pre=%d)", iter, blockType, mixed, preflag)
		assert.Equalf(t, c.part2Length(0, 0), n.part2Length(0, 0), "iter %d: part2_length", iter)
		assert.Equalf(t, c.scalefacCompress(0, 0), n.scalefacCompress(0, 0), "iter %d: scalefac_compress", iter)
		assert.Equalf(t, c.preflag(0, 0), n.preflag(0, 0), "iter %d: preflag", iter)
		for sfb := 0; sfb < 39; sfb++ {
			assert.Equalf(t, c.scalefac(0, 0, sfb), n.scalefac(0, 0, sfb), "iter %d: scalefac[%d]", iter, sfb)
		}
		c.free()
	}
}

func TestScaleBitcountMPEG2LSF(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0x33, 0x44))
	for iter := 0; iter < 400; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setCfg(1, 0) // mode_gr=1 -> mpeg2_scale_bitcount (LSF)
		n.setCfg(1, 0)

		blockType := []int{0, 2}[rng.IntN(2)]
		preflag := rng.IntN(2)
		sf := make([]int, 39)
		for i := range sf {
			sf[i] = rng.IntN(16)
		}
		c.setScalefac(0, 0, sf)
		n.setScalefac(0, 0, sf)
		c.setGeom(0, 0, blockType, 0, 210, 0, preflag, 39, 11, 575, 0)
		n.setGeom(0, 0, blockType, 0, 210, 0, preflag, 39, 11, 575, 0)

		cr := c.scaleBitcount(0, 0)
		nr := n.scaleBitcount(0, 0)
		assert.Equalf(t, cr, nr, "iter %d: over (bt=%d pre=%d)", iter, blockType, preflag)
		if cr == 0 {
			assert.Equalf(t, c.part2Length(0, 0), n.part2Length(0, 0), "iter %d: part2_length", iter)
			assert.Equalf(t, c.scalefacCompress(0, 0), n.scalefacCompress(0, 0), "iter %d: scalefac_compress", iter)
			for i := 0; i < 4; i++ {
				assert.Equalf(t, c.slen(0, 0, i), n.slen(0, 0, i), "iter %d: slen[%d]", iter, i)
			}
		}
		c.free()
	}
}

func TestBestHuffmanDivide(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0xa1, 0xb2))
	for iter := 0; iter < 300; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setCfg(2, 0)
		n.setCfg(2, 0)
		c.setSfbLong(sfbLong44)
		n.setSfbLong(sfbLong44)
		c.huffmanInit()
		n.huffmanInit()

		nonzero := 8 + rng.IntN(560)
		maxMag := []int{1, 3, 15, 60}[rng.IntN(4)]
		ix := fabricateL3Enc(rng, nonzero, maxMag)
		c.setL3Enc(0, 0, ix)
		n.setL3Enc(0, 0, ix)
		c.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, 0)
		n.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, 0)

		// prime big_values / count1 / part2_3_length via noquant_count_bits first
		cb := c.noquantCountBits(0, 0)
		nb := n.noquantCountBits(0, 0)
		require.Equal(t, cb, nb, "iter %d: priming bits diverged", iter)
		// noquant set part2_3 only when use_best_huffman==2; set it explicitly
		// here so best_huffman_divide has a baseline to beat.
		c.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, cb+10)
		n.setGeom(0, 0, 0, 0, 210, 0, 0, 21, 11, 575, nb+10)

		c.bestHuffmanDivide(0, 0)
		n.bestHuffmanDivide(0, 0)

		assert.Equalf(t, c.part23Length(0, 0), n.part23Length(0, 0), "iter %d: part2_3_length", iter)
		assert.Equalf(t, c.bigValues(0, 0), n.bigValues(0, 0), "iter %d: big_values", iter)
		assert.Equalf(t, c.count1tableSelect(0, 0), n.count1tableSelect(0, 0), "iter %d: count1table_select", iter)
		assert.Equalf(t, c.region0Count(0, 0), n.region0Count(0, 0), "iter %d: region0_count", iter)
		assert.Equalf(t, c.region1Count(0, 0), n.region1Count(0, 0), "iter %d: region1_count", iter)
		for i := 0; i < 3; i++ {
			assert.Equalf(t, c.tableSelect(0, 0, i), n.tableSelect(0, 0, i), "iter %d: table_select[%d]", iter, i)
		}
		c.free()
	}
}

func TestBestScalefacStore(t *testing.T) {
	requireStrict(t)

	rng := rand.New(rand.NewPCG(0xc3, 0xd4))
	for iter := 0; iter < 300; iter++ {
		c := newCgoTk()
		n := newNativeTk()

		c.setCfg(2, 0)
		n.setCfg(2, 0)
		c.setSfbLong(sfbLong44)
		n.setSfbLong(sfbLong44)

		// width[] over 21 long bands (derived from sfbLong44 deltas), plus a
		// guard band, summing to 576.
		width := make([]int, 39)
		for sfb := 0; sfb < 21; sfb++ {
			width[sfb] = sfbLong44[sfb+1] - sfbLong44[sfb]
		}
		nonzero := 8 + rng.IntN(560)
		maxMag := []int{1, 3, 15}[rng.IntN(3)]
		ix := fabricateL3Enc(rng, nonzero, maxMag)
		sf := make([]int, 39)
		for i := range sf {
			sf[i] = rng.IntN(8)
		}

		for ch := 0; ch < 2; ch++ {
			for gr := 0; gr < 2; gr++ {
				c.setWidth(gr, ch, width)
				n.setWidth(gr, ch, width)
				c.setL3Enc(gr, ch, ix)
				n.setL3Enc(gr, ch, ix)
				c.setScalefac(gr, ch, sf)
				n.setScalefac(gr, ch, sf)
				c.setGeom(gr, ch, 0, 0, 210, 0, 0, 21, 11, 575, 0)
				n.setGeom(gr, ch, 0, 0, 210, 0, 0, 21, 11, 575, 0)
			}
		}

		// store granule 1 channel 0 (exercises the scfsi path against gr 0)
		c.bestScalefacStore(1, 0)
		n.bestScalefacStore(1, 0)

		assert.Equalf(t, c.scalefacScale(1, 0), n.scalefacScale(1, 0), "iter %d: scalefac_scale", iter)
		assert.Equalf(t, c.preflag(1, 0), n.preflag(1, 0), "iter %d: preflag", iter)
		assert.Equalf(t, c.part2Length(1, 0), n.part2Length(1, 0), "iter %d: part2_length", iter)
		assert.Equalf(t, c.scalefacCompress(1, 0), n.scalefacCompress(1, 0), "iter %d: scalefac_compress", iter)
		for sfb := 0; sfb < 21; sfb++ {
			assert.Equalf(t, c.scalefac(1, 0, sfb), n.scalefac(1, 0, sfb), "iter %d: scalefac[%d]", iter, sfb)
		}
		for i := 0; i < 4; i++ {
			assert.Equalf(t, c.scfsi(0, i), n.scfsi(0, i), "iter %d: scfsi[0][%d]", iter, i)
		}
		c.free()
	}
}
