//go:build cgo

package huffmandecode

import (
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/mp3/internal/nativemp3"
)

// requireStrict skips a parity test unless the mp3_strict build tag is set.
// The Huffman slice's dequantization (g_pow43 lookups + L3_pow_43) is
// floating point, so its output only matches the cgo oracle bit-for-bit when
// the strict, FMA-free Go build is paired with the scalar (-ffp-contract=off,
// -fno-vectorize, …) cgo oracle the mise `parity` task configures. A bare
// `go test` builds the default (FMA-fusing) helpers and would diverge in the
// last ULP, so the assertions are gated to the canonical
// `mise run //libraries/mp3:parity` gate. See the FP-parity convention in the
// add-audio-format skill.
func requireStrict(t *testing.T) {
	t.Helper()
	if !nativemp3.StrictMode {
		t.Skip("huffman-decode parity asserts bit-exactness; run under -tags=mp3_strict (mise run //libraries/mp3:parity)")
	}
}

// makeRandomBytes returns n deterministically seeded random bytes so the
// parity sweep is reproducible. The payload feeds the bit reader; any byte run
// decodes deterministically because the minimp3 Huffman tables are total over
// every 4-/5-bit peek (each entry is a valid leaf or internal node).
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// TestGPow43Parity pins the ported gPow43 table entry-for-entry against the
// vendored minimp3 g_pow43. This is integer-exact (a float table copy) and so
// matches in both build modes, but gates with the suite for uniformity.
func TestGPow43Parity(t *testing.T) {
	requireStrict(t)
	c := cgoGPow43()
	require.Equal(t, len(c), nativemp3.GPow43Len())
	for i := 0; i < len(c); i++ {
		assert.Equalf(t, c[i], nativemp3.GPow43At(i), "g_pow43[%d]", i)
	}
}

// TestL3Pow43Parity sweeps L3_pow_43 across the small-x table-read branch
// (x < 129), the two interpolated branches (x < 1024 with mult=16, and
// x >= 1024 with mult=256), and the sign-folding boundary, asserting the Go
// port reproduces the C bit-for-bit. minimp3 only ever feeds L3_pow_43 a
// `lsb` that is at most 15 + (2^linbits - 1) with linbits <= 13, i.e. 8206,
// for which the lookup index 16 + ((x+sign)>>6) stays inside g_pow43[145];
// the sweep stops there because beyond it both implementations read past the
// table (the C reference relies on the reachable domain bound, not a clamp).
func TestL3Pow43Parity(t *testing.T) {
	requireStrict(t)
	// The largest lsb the Huffman escape path can produce: 15 + (2^13 - 1).
	const maxReachableX = 15 + (1 << 13) - 1 // 8206
	// Exhaustive over the table-read branch and the first interpolation
	// branch, then a sparse sweep through the large-x branch up to the
	// 13-linbit escape ceiling.
	for x := 0; x < 2048; x++ {
		assert.Equalf(t, cgoL3Pow43(x), nativemp3.L3Pow43(x), "L3_pow_43(%d)", x)
	}
	for x := 2048; x <= maxReachableX; x += 7 {
		assert.Equalf(t, cgoL3Pow43(x), nativemp3.L3Pow43(x), "L3_pow_43(%d)", x)
	}
	assert.Equalf(t, cgoL3Pow43(maxReachableX), nativemp3.L3Pow43(maxReachableX), "L3_pow_43(%d)", maxReachableX)
}

// grSpec describes a synthetic-but-valid L3_huffman input. The fields mirror
// the L3_gr_info_t members L3_huffman consumes.
//
// The band walk is shaped like a real granule: `nbands` bands of width 4 (so
// np = 2 pairs each) followed by a 0 sentinel (buildSfb). The big-value region
// consumes ceil(bigValues/2) bands; the count1 region consumes the rest and
// terminates on the 0 sentinel (the C RELOAD_SCALEFACTOR's `if (!np) break`),
// not on the bit limit, so the limit is set high and uniform across specs.
// With nbands kept small the total dst written stays well under 576, matching
// how a real granule fills the low coefficients and leaves the tail zero.
type grSpec struct {
	name        string
	bigValues   uint16
	tableSelect [3]uint8
	regionCount [3]uint8
	count1Table uint8
	nbands      int
	payloadSeed uint64
	bsPos       int
}

// buildSfb fills a scalefactor-band width table the way minimp3's g_scf_long /
// g_scf_short tables are shaped: `nbands` band widths of 4 followed by a 0
// sentinel. L3_huffman walks it with *sfb++ and the count1 region's
// RELOAD_SCALEFACTOR breaks on the first 0-width band (`if (!np) break`), so
// the terminator must be in reach; without it both the C oracle and the Go
// port walk off the end of the array. The remaining entries stay 0 so a
// misbehaving walk faults deterministically rather than reading live data.
func buildSfb(nbands int) []byte {
	sfb := make([]byte, 64)
	for i := 0; i < nbands && i < len(sfb); i++ {
		sfb[i] = 4
	}
	return sfb
}

// TestL3HuffmanParity drives the Go L3Huffman and the C L3_huffman over
// identical inputs and asserts all 576 dequantized frequency lines and the
// final bit position match bit-for-bit. The corpus spans:
//   - linbits==0 tables (1,2,3,5,7,8,11,15) — direct + internal-node leaves;
//   - linbits!=0 tables (16,24,31) — the escape (lsb==15) path through
//     L3_pow_43, including the 13-linbit ceiling;
//   - both count1 quadruple tables (count1_table 0 -> tab32, 1 -> tab33);
//   - sign-folding via the random payload's high bit; and
//   - non-byte-aligned start positions to exercise the bs_cache prime shift.
//
// `nbands` is chosen per spec so the big-value walk (ceil(bigValues/2) bands)
// plus the count1 reloads consume strictly fewer than the 0-sentinel band and
// write strictly fewer than 576 dst lines (each band is 4 lines).
func TestL3HuffmanParity(t *testing.T) {
	requireStrict(t)

	// A uniform, high bit limit: the count1 loop terminates on the 0-sentinel
	// band, not the limit, so every spec drives the same termination path the
	// real decoder uses when a granule's coefficients run out before its bits.
	const limit = 1 << 20

	specs := []grSpec{
		{name: "tab1-linbits0", bigValues: 8, tableSelect: [3]uint8{1, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 1},
		{name: "tab2", bigValues: 8, tableSelect: [3]uint8{2, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 2},
		{name: "tab3", bigValues: 8, tableSelect: [3]uint8{3, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 3},
		{name: "tab5", bigValues: 8, tableSelect: [3]uint8{5, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 5},
		{name: "tab7", bigValues: 12, tableSelect: [3]uint8{7, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 18, payloadSeed: 7},
		{name: "tab8", bigValues: 12, tableSelect: [3]uint8{8, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 18, payloadSeed: 8},
		{name: "tab11", bigValues: 12, tableSelect: [3]uint8{11, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 18, payloadSeed: 11},
		{name: "tab15", bigValues: 16, tableSelect: [3]uint8{15, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 20, payloadSeed: 15},
		// linbits!=0 (escape / L3_pow_43) tables.
		{name: "tab16-linbits1", bigValues: 12, tableSelect: [3]uint8{16, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 18, payloadSeed: 16},
		{name: "tab24-linbits4", bigValues: 16, tableSelect: [3]uint8{24, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 20, payloadSeed: 24},
		{name: "tab31-linbits13", bigValues: 20, tableSelect: [3]uint8{31, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 24, payloadSeed: 31},
		// Multi-region: three different tables across the three big-value regions.
		{name: "multi-region", bigValues: 24, tableSelect: [3]uint8{1, 24, 31}, regionCount: [3]uint8{3, 3, 3}, nbands: 28, payloadSeed: 99},
		// count1 region with the alternate quadruple table.
		{name: "count1-tab33", bigValues: 4, tableSelect: [3]uint8{2, 0, 0}, regionCount: [3]uint8{15, 0, 0}, count1Table: 1, nbands: 20, payloadSeed: 33},
		{name: "count1-tab32-long", bigValues: 2, tableSelect: [3]uint8{1, 0, 0}, regionCount: [3]uint8{15, 0, 0}, count1Table: 0, nbands: 24, payloadSeed: 320},
		// Non-byte-aligned start positions to exercise the bs_cache prime shift.
		{name: "bspos-3", bigValues: 8, tableSelect: [3]uint8{5, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 41, bsPos: 3},
		{name: "bspos-7", bigValues: 8, tableSelect: [3]uint8{24, 0, 0}, regionCount: [3]uint8{15, 0, 0}, nbands: 16, payloadSeed: 42, bsPos: 7},
		// big_values 0: straight to the count1 region.
		{name: "no-bigvalues", bigValues: 0, tableSelect: [3]uint8{0, 0, 0}, regionCount: [3]uint8{0, 0, 0}, nbands: 20, payloadSeed: 50},
	}

	// The reassembled main-data buffer is sized as the real decoder sizes its
	// scratch.maindata, so the bs_cache prime and CHECK_BITS reads never index
	// past the slice even when bsPos is non-zero.
	const payloadBytes = nativemp3.MaxBitReservoirBytes + nativemp3.MaxL3FramePayloadBytes

	for _, s := range specs {
		t.Run(s.name, func(t *testing.T) {
			payload := makeRandomBytes(s.payloadSeed, payloadBytes)
			sfb := buildSfb(s.nbands)
			scf := make([]float32, len(sfb))
			// Distinct, exactly representable gains per band so a misaligned
			// scalefactor walk would surface as a wrong magnitude.
			for i := range scf {
				scf[i] = float32(i + 1)
			}

			// C oracle over its own copy of the payload (L3_huffman does not
			// mutate it, but copy to be safe against aliasing surprises).
			cPayload := append([]byte(nil), payload...)
			cDst, cPos := cgoL3Huffman(cPayload, s.bsPos, sfb, s.bigValues,
				s.tableSelect, s.regionCount, s.count1Table, scf, limit)

			// Go port over an independent copy.
			goPayload := append([]byte(nil), payload...)
			var bs nativemp3.BitStream
			nativemp3.BsInit(&bs, goPayload, len(goPayload))
			bs.Pos = s.bsPos
			gr := &nativemp3.L3GrInfo{
				BigValues:   s.bigValues,
				Sfbtab:      sfb,
				TableSelect: s.tableSelect,
				RegionCount: s.regionCount,
				Count1Table: s.count1Table,
			}
			goDst := make([]float32, 576)
			nativemp3.L3Huffman(goDst, &bs, gr, scf, limit)

			assert.Equalf(t, limit, bs.Pos, "go final bs.Pos")
			assert.Equalf(t, cPos, bs.Pos, "final bs.pos C vs Go")
			for i := 0; i < 576; i++ {
				assert.Equalf(t, cDst[i], goDst[i], "dst[%d] (table %v)", i, s.tableSelect)
			}
		})
	}
}
