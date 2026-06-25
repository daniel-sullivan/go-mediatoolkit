// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encqcmain

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// Window types (psy_const.h:121): LONG/START/SHORT/STOP. dynBitCount only uses
// blockType to pick the side-info table (SHORT vs the rest).
const (
	longWindow  = 0
	shortWindow = 2
)

const noNoisePns = -0x80000000 // NO_NOISE_PNS == FDK_INT_MIN

// Per-block scalefactor-band ceilings (psy_const.h): a long block has at most
// MAX_SFB_LONG sfbs; a short block's per-group sfb count is at most
// MAX_SFB_SHORT. The bitLookUp scratch is sized MAX_SFB_LONG, so maxSfbPerGroup
// must respect these — exactly as a real AAC band layout does.
const (
	maxSfbLong  = 51
	maxSfbShort = 15
)

// toIntSlice converts an int32 buffer to the []int the Go port expects.
func toIntSlice(s []int32) []int {
	out := make([]int, len(s))
	for i, v := range s {
		out[i] = int(v)
	}
	return out
}

// toUintSlice converts a uint32 buffer to the []uint the Go port expects.
func toUintSlice(s []uint32) []uint {
	out := make([]uint, len(s))
	for i, v := range s {
		out[i] = uint(v)
	}
	return out
}

// quantRand returns a pseudo-random quantized spectral coefficient. The Huffman
// codebooks code |value| up to MAX_QUANT (8191) with ESC; sample a magnitude
// distribution that exercises every codebook (small values -> books 1..10,
// occasional large -> ESC book 11).
func quantRand(r *rand.Rand) int16 {
	switch r.Intn(4) {
	case 0:
		return 0
	case 1:
		return int16(r.Intn(3) - 1) // -1..1 (books 1,2,5,6)
	case 2:
		return int16(r.Intn(25) - 12) // -12..12 (books up to 9,10)
	default:
		v := r.Intn(8192) // 0..8191, ESC range
		if r.Intn(2) == 0 {
			v = -v
		}
		return int16(v)
	}
}

// makeChannel builds a randomized quantized channel for one block: an ascending
// sfbOffsets whose band widths are multiples of 4 (the count functions unroll by
// 4 / countEsc by 2 — real AAC bands are always multiples of 4), the quantized
// SHORT spectrum, the per-band maxValueInSfb (max |coef|, exactly the value
// FDKaacEnc_calcMaxValueInSfb would produce so the codebook selection matches a
// real frame), the scalefactor gain per band, and the noiseNrg/isBook/isScale
// inputs set for plain AAC-LC (no PNS, no intensity). sfbPerGroup == sfbCnt,
// maxSfbPerGroup == sfbCnt (one long-block group) unless grouped is requested.
func makeChannel(r *rand.Rand, sfbCnt, sfbPerGroup int) (
	quant []int16, maxVal []uint32, scalefac, offset, noiseNrg, isBook, isScale []int32, maxSfbPerGroup int) {
	quant = make([]int16, 1024)
	maxVal = make([]uint32, nativeaac.MaxGroupedSFB)
	scalefac = make([]int32, nativeaac.MaxGroupedSFB)
	offset = make([]int32, nativeaac.MaxGroupedSFB+1)
	noiseNrg = make([]int32, nativeaac.MaxGroupedSFB)
	isBook = make([]int32, nativeaac.MaxGroupedSFB)
	isScale = make([]int32, nativeaac.MaxGroupedSFB)

	// Scalefactor gains as a bounded random walk: the scfCount DPCM codes the
	// delta between consecutive coded bands, and the AAC scalefactor codebook
	// (CODE_BOOK_SCF_LAV == 60) can only represent |delta| <= 60. A real encoder
	// guarantees that; mirror it so bitCountScalefactorDelta stays in range.
	scf := int32(100)
	pos := int32(0)
	for i := 0; i < sfbCnt; i++ {
		offset[i] = pos
		w := int32(1+r.Intn(8)) * 4 // width 4..32, multiple of 4
		if pos+w > 1024 {
			w = 1024 - pos
			w -= w % 4
		}
		var m uint32
		for j := pos; j < pos+w; j++ {
			quant[j] = quantRand(r)
			a := quant[j]
			if a < 0 {
				a = -a
			}
			if uint32(a) > m {
				m = uint32(a)
			}
		}
		maxVal[i] = m
		pos += w

		// Keep every scalefactor within a 60-wide window so that ANY transmitted
		// DPCM delta (between any two coded bands, including across skipped empty
		// bands) stays within CODE_BOOK_SCF_LAV == 60 — the codebook limit a real
		// encoder's quantizer guarantees.
		scf += int32(r.Intn(13) - 6)
		if scf < 70 {
			scf = 70
		}
		if scf > 130 {
			scf = 130
		}
		scalefac[i] = scf
		noiseNrg[i] = noNoisePns // no PNS
		isBook[i] = 0            // no intensity stereo
		isScale[i] = 0
	}
	offset[sfbCnt] = pos

	maxSfbPerGroup = sfbCnt / (sfbCnt / sfbPerGroup)
	return quant, maxVal, scalefac, offset, noiseNrg, isBook, isScale, maxSfbPerGroup
}

// assertDynBitCountEqual runs FDKaacEnc_dynBitCount in both the genuine C and the
// Go port for one block and asserts the returned bit total AND every SECTION_DATA
// field is bit-identical.
func assertDynBitCountEqual(t *testing.T, tag string,
	quant []int16, maxVal []uint32, scalefac []int32, blockType, sfbCnt,
	maxSfbPerGroup, sfbPerGroup int, offset, noiseNrg, isBook, isScale []int32,
	syntaxFlags uint) {
	t.Helper()

	c := cDynBitCount(quant, maxVal, scalefac, blockType, sfbCnt, maxSfbPerGroup,
		sfbPerGroup, offset, noiseNrg, isBook, isScale, syntaxFlags)

	g := nativeaac.DynBitCountForParity(quant, toUintSlice(maxVal),
		toIntSlice(scalefac), blockType, sfbCnt, maxSfbPerGroup, sfbPerGroup,
		toIntSlice(offset), toIntSlice(noiseNrg), toIntSlice(isBook),
		toIntSlice(isScale), syntaxFlags)

	require.Equalf(t, c.totalBits, g.TotalBits, "%s totalBits", tag)
	require.Equalf(t, c.noOfSections, g.NoOfSections, "%s noOfSections", tag)
	require.Equalf(t, c.huffmanBits, g.HuffmanBits, "%s huffmanBits", tag)
	require.Equalf(t, c.sideInfoBits, g.SideInfoBits, "%s sideInfoBits", tag)
	require.Equalf(t, c.scalefacBits, g.ScalefacBits, "%s scalefacBits", tag)
	require.Equalf(t, c.noiseNrgBits, g.NoiseNrgBits, "%s noiseNrgBits", tag)
	require.Equalf(t, c.firstScf, g.FirstScf, "%s firstScf", tag)
	require.Equalf(t, c.sectCodeBook, g.SectCodeBook, "%s sectCodeBook", tag)
	require.Equalf(t, c.sectSfbStart, g.SectSfbStart, "%s sectSfbStart", tag)
	require.Equalf(t, c.sectSfbCnt, g.SectSfbCnt, "%s sectSfbCnt", tag)
	require.Equalf(t, c.sectSectionBits, g.SectSectionBits, "%s sectSectionBits", tag)
}

// TestBitCountParity asserts bitCount (the per-codebook cost row) == genuine
// FDKaacEnc_bitCount over randomized bands of every width / maxVal.
func TestBitCountParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xB1C0))
	for iter := 0; iter < 20000; iter++ {
		width := (1 + r.Intn(8)) * 4
		vals := make([]int16, width)
		var maxVal int
		for i := range vals {
			vals[i] = quantRand(r)
			a := int(vals[i])
			if a < 0 {
				a = -a
			}
			if a > maxVal {
				maxVal = a
			}
		}
		c := cBitCount(vals, width, maxVal)
		g := nativeaac.BitCountForParity(vals, width, maxVal)
		require.Equalf(t, c, g, "bitCount iter=%d width=%d maxVal=%d", iter, width, maxVal)
	}
}

// TestCountValuesParity asserts countValues == genuine FDKaacEnc_countValues for
// every codebook over randomized bands. Codebooks 1..10 step by 4 and books 5/6
// (signed, +4 offset) require |value| <= 4; books 1/2 require |value| <= 1; the
// per-book magnitude clamp mirrors what the section coder guarantees before it
// calls countValues with a chosen book.
func TestCountValuesParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	// largest absolute value each codebook can represent (CODE_BOOK_*_LAV).
	lav := map[int]int{1: 1, 2: 1, 3: 2, 4: 2, 5: 4, 6: 4, 7: 7, 8: 7, 9: 12, 10: 12, 11: 8191}
	r := rand.New(rand.NewSource(0xC0FF))
	for _, cb := range []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11} {
		for iter := 0; iter < 4000; iter++ {
			width := (1 + r.Intn(8)) * 4
			vals := make([]int16, width)
			m := lav[cb]
			for i := range vals {
				if cb == 0 {
					vals[i] = 0
					continue
				}
				v := r.Intn(2*m + 1)
				vals[i] = int16(v - m)
			}
			c := cCountValues(vals, width, cb)
			g := nativeaac.CountValuesForParity(vals, width, cb)
			require.Equalf(t, c, g, "countValues cb=%d iter=%d width=%d", cb, iter, width)
		}
	}
}

// TestDynBitCountParity_Long asserts the full FDKaacEnc_dynBitCount over
// randomized long-block channels (one group, maxSfbPerGroup == sfbCnt).
func TestDynBitCountParity_Long(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0xD17B))
	for iter := 0; iter < 4000; iter++ {
		sfbCnt := 1 + r.Intn(maxSfbLong)
		quant, maxVal, scf, offset, noiseNrg, isBook, isScale, maxSfb :=
			makeChannel(r, sfbCnt, sfbCnt)
		assertDynBitCountEqual(t, "long", quant, maxVal, scf, longWindow, sfbCnt,
			maxSfb, sfbCnt, offset, noiseNrg, isBook, isScale, 0)
	}
}

// TestDynBitCountParity_LongReducedMaxSfb asserts dynBitCount when the encoder has
// reduced maxSfbPerGroup below sfbCnt (the crash-recovery / global-gain path the
// QC loop drives), so the trailing bands are not coded.
func TestDynBitCountParity_LongReducedMaxSfb(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x4EDC))
	for iter := 0; iter < 4000; iter++ {
		sfbCnt := 2 + r.Intn(maxSfbLong-2)
		quant, maxVal, scf, offset, noiseNrg, isBook, isScale, _ :=
			makeChannel(r, sfbCnt, sfbCnt)
		maxSfb := r.Intn(sfbCnt + 1) // 0..sfbCnt, including the maxSfbPerGroup==0 early-out
		assertDynBitCountEqual(t, "reduced", quant, maxVal, scf, longWindow, sfbCnt,
			maxSfb, sfbCnt, offset, noiseNrg, isBook, isScale, 0)
	}
}

// TestDynBitCountParity_Short asserts dynBitCount over grouped short blocks
// (sfbCnt is a multiple of sfbPerGroup; the short side-info table is used).
func TestDynBitCountParity_Short(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}
	r := rand.New(rand.NewSource(0x5409))
	for iter := 0; iter < 4000; iter++ {
		sfbPerGroup := 1 + r.Intn(14) // short blocks have up to MAX_SFB_SHORT(15) sfbs
		nGroups := 1 + r.Intn(8)
		sfbCnt := sfbPerGroup * nGroups
		if sfbCnt > nativeaac.MaxGroupedSFB {
			continue
		}
		quant, maxVal, scf, offset, noiseNrg, isBook, isScale, _ :=
			makeChannel(r, sfbCnt, sfbPerGroup)
		maxSfb := 1 + r.Intn(sfbPerGroup)
		assertDynBitCountEqual(t, "short", quant, maxVal, scf, shortWindow, sfbCnt,
			maxSfb, sfbPerGroup, offset, noiseNrg, isBook, isScale, 0)
	}
}
