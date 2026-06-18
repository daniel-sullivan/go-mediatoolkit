// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package bitstream_encode

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// bufBytes is the power-of-two ring-buffer size both the C oracle and the Go
// port hand FDKinitBitStream / newWriteBitStream. It must be a power of two
// (FDK_InitBitBuffer asserts that) and large enough that no fabricated pattern
// wraps the ring — 4096 bytes (32768 bits) comfortably holds the longest case
// (a 1024-line ESC section emitting up to ~30 bits/pair).
const bufBytes = 4096

// codebookLav is the largest-absolute spectral value each Huffman codebook
// codes (bit_cnt.h:164, enum codeBookLav). Used to fabricate in-range
// quantized-spectrum sections. The escape codebook (11) is handled separately
// because it additionally codes magnitudes above 16 via an escape sequence.
var codebookLav = map[int]int{
	1: 1, 2: 1, 3: 2, 4: 2, 5: 4, 6: 4, 7: 7, 8: 7, 9: 12, 10: 12,
}

// codebookStep is the spectral-tuple stride each codebook consumes: 4 for the
// 4-tuple books (1-6), 2 for the 2-tuple books (7-11). width must be a multiple
// of the stride.
var codebookStep = map[int]int{
	1: 4, 2: 4, 3: 4, 4: 4, 5: 4, 6: 4, 7: 2, 8: 2, 9: 2, 10: 2, 11: 2,
}

// TestWriteBitsParity asserts the pure-Go FDK bit WRITER port
// (nativeaac.writeBits + fdkPut + syncCacheWrite + byteAlignWrite, driven via
// WriteBitsParity) produces byte-identical output and the same valid-bit count
// as the vendored inline FDKwriteBits + FDK_put ring store, over fabricated
// (value, width) sequences that span the 32-bit cache and exercise the full
// 1..32-bit write widths.
//
// The bit writer is a pure INTEGER kernel (shifts/masks/ring store), bit-exact
// in any build; the strict gate is the area convention (the aac_strict parity
// discipline), not a numerical necessity. The comparison runs only under
// aac_strict so a bare `go test` of the suite stays clean.
func TestWriteBitsParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("bitstream-encode parity asserts under -tags=aac_strict (the integer-parity gate convention); skipping in the default build")
	}

	rng := rand.New(rand.NewSource(0x5EED_0001))
	for iter := 0; iter < 512; iter++ {
		iter := iter
		t.Run("", func(t *testing.T) {
			// A random run of 1..400 writes, each 1..32 bits wide with a value
			// whose low `width` bits are random (FDKwriteBits masks to the low
			// width bits via BitMask[width], so the upper bits are don't-care —
			// feed them random to prove the port masks identically).
			n := 1 + rng.Intn(400)
			values := make([]uint32, n)
			widths := make([]uint32, n)
			for i := 0; i < n; i++ {
				w := uint32(1 + rng.Intn(32)) // 1..32
				widths[i] = w
				values[i] = rng.Uint32()
			}

			wantBuf, wantVB := cWriteBits(values, widths, bufBytes)
			gotBuf, gotVB := nativeaac.WriteBitsParity(values, widths, bufBytes)

			assert.Equal(t, wantVB, gotVB, "valid-bit count mismatch iter %d", iter)
			require.Equal(t, wantBuf, gotBuf, "bitstream bytes mismatch iter %d", iter)
		})
	}
}

// TestCodeValuesParity asserts the pure-Go spectral Huffman emitter port
// (nativeaac.CodeValues, driven via CodeValuesParity) matches the vendored
// FDKaacEnc_codeValues bit-for-bit (produced bytes + valid-bit count) over
// fabricated in-range quantized-spectrum sections for every codebook 1..11.
func TestCodeValuesParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("bitstream-encode parity asserts under -tags=aac_strict (the integer-parity gate convention); skipping in the default build")
	}

	rng := rand.New(rand.NewSource(0x5EED_0002))

	// Section widths the encoder actually emits: short-block (subset of) a
	// scalefactor band up to a full long block, all multiples of 4 so every
	// codebook stride divides them.
	widths := []int{4, 8, 12, 16, 32, 64, 128, 256, 1024}

	for cb := 1; cb <= 11; cb++ {
		cb := cb
		step := codebookStep[cb]
		t.Run("", func(t *testing.T) {
			for _, width := range widths {
				if width%step != 0 {
					continue
				}
				for iter := 0; iter < 32; iter++ {
					values := make([]int16, width)
					for i := range values {
						values[i] = fabricateCoef(rng, cb)
					}
					wantBuf, wantVB := cCodeValues(values, width, cb, bufBytes)
					gotBuf, gotVB := nativeaac.CodeValuesParity(values, width, cb, bufBytes)

					assert.Equal(t, wantVB, gotVB, "valid-bit mismatch cb %d width %d iter %d", cb, width, iter)
					require.Equal(t, wantBuf, gotBuf, "bytes mismatch cb %d width %d iter %d", cb, width, iter)
				}
			}
		})
	}
}

// fabricateCoef draws one in-range spectral coefficient for codebook cb. Books
// 1..10 draw uniformly in [-LAV, +LAV]; the escape book (11) draws magnitudes
// up to 8191 (signed) so the escape-sequence path (magnitude >= 16) and the
// in-table path (< 16) are both exercised.
func fabricateCoef(rng *rand.Rand, cb int) int16 {
	if cb == 11 {
		// Bias toward small magnitudes (most lines) but regularly draw large
		// ones to cover the n-bit escape encoding for n in 4..13.
		var mag int
		if rng.Intn(3) == 0 {
			mag = rng.Intn(8192) // 0..8191, exercises escape
		} else {
			mag = rng.Intn(32) // 0..31, straddles the 16-boundary
		}
		if rng.Intn(2) == 0 {
			mag = -mag
		}
		return int16(mag)
	}
	lav := codebookLav[cb]
	return int16(rng.Intn(2*lav+1) - lav)
}

// TestCodeScalefactorDeltaParity asserts the pure-Go scalefactor-delta Huffman
// emitter port (nativeaac.CodeScalefactorDelta, driven via
// CodeScalefactorDeltaParity) matches the vendored
// FDKaacEnc_codeScalefactorDelta bit-for-bit, including the +-1 range-error
// return at the boundary |delta| == CODE_BOOK_SCF_LAV+1.
func TestCodeScalefactorDeltaParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("bitstream-encode parity asserts under -tags=aac_strict (the integer-parity gate convention); skipping in the default build")
	}

	// Sweep the full in-range delta domain [-60, 60] plus the just-out-of-range
	// values (+-61) that must return the range error and write nothing.
	for delta := -62; delta <= 62; delta++ {
		delta := delta
		t.Run("", func(t *testing.T) {
			wantBuf, wantVB, wantErr := cCodeScalefactorDelta(delta, bufBytes)
			gotBuf, gotVB, gotErr := nativeaac.CodeScalefactorDeltaParity(delta, bufBytes)

			assert.Equal(t, wantErr, gotErr, "range-error mismatch delta %d", delta)
			assert.Equal(t, wantVB, gotVB, "valid-bit mismatch delta %d", delta)
			require.Equal(t, wantBuf, gotBuf, "bytes mismatch delta %d", delta)
		})
	}
}

// TestBitstreamEncodeOracleDeterministic is a lightweight always-on check (no
// strict gate) that the C oracle itself runs and is stable across repeated
// calls — it guards the cgo build/link of the vendored bit_cnt.cpp +
// aacEnc_rom.cpp + FDK_bitbuffer.cpp + genericStds.cpp TUs even when the strict
// assertions above are skipped.
func TestBitstreamEncodeOracleDeterministic(t *testing.T) {
	values := []uint32{0xABCD, 0x12345, 0x7, 0xFFFF_FFFF, 0x1}
	widths := []uint32{16, 20, 3, 32, 1}
	a, avb := cWriteBits(values, widths, bufBytes)
	b, bvb := cWriteBits(values, widths, bufBytes)
	require.Equal(t, a, b, "C bit-writer oracle is non-deterministic")
	require.Equal(t, avb, bvb)

	spectrum := make([]int16, 16)
	for i := range spectrum {
		spectrum[i] = int16(i%3 - 1)
	}
	c, cvb := cCodeValues(spectrum, len(spectrum), 4, bufBytes)
	d, dvb := cCodeValues(spectrum, len(spectrum), 4, bufBytes)
	require.Equal(t, c, d, "C codeValues oracle is non-deterministic")
	require.Equal(t, cvb, dvb)
}
