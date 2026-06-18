package nativemp3

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHuffmanTableShapes pins the sizes and a few spot values of the ported
// minimp3 Huffman tables so an accidental transcription error (a dropped or
// duplicated comma in the big packed tabs[] array) is caught immediately.
func TestHuffmanTableShapes(t *testing.T) {
	assert.Len(t, gPow43, 129+16)
	assert.Len(t, l3Tabs, 2164) // total packed size of minimp3's tabs[]
	assert.Len(t, tab32, 28)
	assert.Len(t, tab33, 16)
	assert.Len(t, tabindex, 32)
	assert.Len(t, gLinbits, 32)

	// Spot-check g_pow43: index 16 is the zero point; the first 16 entries
	// are the negated low magnitudes (index 0 = 0 ... index 15 = -36.993181)
	// that the inline sign-folding index `16 + lsb - 16*sign` reads.
	assert.Equal(t, float32(0), gPow43[16])             // x = 0
	assert.Equal(t, float32(1), gPow43[16+1])           // x = 1
	assert.Equal(t, float32(0), gPow43[0])              // negated-half zero
	assert.Equal(t, float32(-36.993181), gPow43[15])    // negated-half extreme
	assert.Equal(t, float32(645.079578), gPow43[145-1]) // positive-half extreme

	// First and last big-value table offsets.
	assert.Equal(t, int16(0), tabindex[0])
	assert.Equal(t, int16(1842), tabindex[24])

	// linbits for table 31 is 13.
	assert.Equal(t, uint8(13), gLinbits[31])
}

// TestL3Pow43 checks the dequantization power function against its table for
// the small-x fast path and a couple of interpolated large-x values computed
// directly from the closed form, mirroring minimp3's L3_pow_43.
func TestL3Pow43(t *testing.T) {
	// Small x (< 129) is an exact table read.
	for x := 0; x < 129; x++ {
		assert.Equal(t, gPow43[16+x], L3Pow43(x), "x=%d", x)
	}

	// Large x interpolates; compare against x^(4/3) within a loose tolerance
	// (the linbits escape path is only used for large coefficients and is not
	// required to be exactly x^(4/3), but should track it closely).
	for _, x := range []int{200, 500, 1023, 4096, 8191} {
		got := float64(L3Pow43(x))
		want := math.Pow(float64(x), 4.0/3.0)
		assert.InEpsilon(t, want, got, 1e-3, "x=%d", x)
	}
}

// TestL3HuffmanEmptyGranule exercises the count1 region's loop-exit path: a
// granule with no big values whose Huffman position has already reached the
// granule limit decodes nothing and parks bs.Pos at the limit.
func TestL3HuffmanEmptyGranule(t *testing.T) {
	// A byte run long enough for the 4-byte cache prime plus the count1
	// peek; contents are irrelevant because layer3grLimit stops the loop on
	// the first BSPOS check.
	payload := make([]byte, 32)
	var bs BitStream
	BsInit(&bs, payload, len(payload))

	gr := &L3GrInfo{BigValues: 0, Sfbtab: make([]byte, 40)}
	dst := make([]float32, 576)
	scf := make([]float32, 40)

	// layer3gr_limit = bs.pos (0) means BSPOS (which starts negative, at
	// -24 + (pos&7) after the prime) immediately exceeds the limit, so the
	// count1 loop breaks on its first iteration.
	L3Huffman(dst, &bs, gr, scf, 0)

	require.Equal(t, 0, bs.Pos)
	// Nothing decoded.
	for _, v := range dst {
		assert.Equal(t, float32(0), v)
	}
}

// TestL3HuffmanBigValuesSetsLimit decodes a single big-value pair from the
// smallest non-trivial codebook (table 1, linbits = 0) and confirms the
// granule position is parked at layer3gr_limit afterwards. Table 1's leaf at
// the all-ones 5-bit peek is a direct leaf (no internal nodes), so this walks
// the linbits==0 inner loop once and writes two dequantized lines.
func TestL3HuffmanBigValuesSetsLimit(t *testing.T) {
	payload := make([]byte, 32)
	// Bit pattern is unconstrained for this smoke test; table 1 entries are
	// all leaves so any 5-bit peek yields a valid leaf.
	for i := range payload {
		payload[i] = 0x80
	}
	var bs BitStream
	BsInit(&bs, payload, len(payload))

	sfb := make([]byte, 40)
	sfb[0] = 4 // np = 2 => one big-value pair in this band
	gr := &L3GrInfo{
		BigValues:   1,
		Sfbtab:      sfb,
		TableSelect: [3]uint8{1, 0, 0},
		RegionCount: [3]uint8{0, 0, 0},
	}
	dst := make([]float32, 576)
	scf := make([]float32, 40)
	scf[0] = 1.0

	const limit = 4096
	L3Huffman(dst, &bs, gr, scf, limit)

	assert.Equal(t, limit, bs.Pos)
}
