// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package huffman_spectral_decode

import (
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// bufBytes is the fabricated bit-buffer length. It must be a power of two
// (the FDK FDK_BITBUF invariant, mirrored by nativeaac.initBitStream).
const bufBytes = 512

// makeRandomBytes returns n random bytes seeded deterministically so the
// parity sweep is reproducible across runs.
func makeRandomBytes(seed uint64, n int) []byte {
	r := rand.New(rand.NewPCG(seed, seed+1))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

// strictGate skips FP-bit-exact-only assertions on a bare (non-strict) go
// test, per the aac_strict parity discipline. The plain Huffman path is an
// integer kernel and matches in any build, but the gate is kept for
// convention so the strict run is the one that asserts.
func strictGate(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("requires -tags=aac_strict (integer-parity gate); see libraries/aac/mise.toml")
	}
}

// TestParityDecodeHuffmanWordCB sweeps the optimised CB tree walker for every
// spectral codebook over many random starting bit positions, comparing the
// decoded index AND the post-read bit position bit-for-bit.
func TestParityDecodeHuffmanWordCB(t *testing.T) {
	strictGate(t)
	stream := makeRandomBytes(11, bufBytes)
	r := rand.New(rand.NewPCG(12, 13))
	for cb := 1; cb <= 11; cb++ {
		for i := 0; i < 2000; i++ {
			// keep a wide margin so no codeword runs off the buffer
			start := uint32(r.IntN(bufBytes*8 - 64))
			gotC, ndxC := cDecodeHuffmanWordCB(stream, cb, start)
			nr := nativeaac.NewHuffBitReader(stream, bufBytes, bufBytes*8)
			advance(nr, start)
			gotN := nr.DecodeHuffmanWordCB(cb)
			ndxN := nr.BitNdx()
			require.Equal(t, gotC, gotN, "cb=%d start=%d value", cb, start)
			require.Equal(t, ndxC, ndxN, "cb=%d start=%d bitpos", cb, start)
		}
	}
}

// TestParityDecodeHuffmanWord sweeps the non-CB tree walker (CBlock_DecodeHuffmanWord).
func TestParityDecodeHuffmanWord(t *testing.T) {
	strictGate(t)
	stream := makeRandomBytes(21, bufBytes)
	r := rand.New(rand.NewPCG(22, 23))
	for cb := 1; cb <= 11; cb++ {
		for i := 0; i < 2000; i++ {
			start := uint32(r.IntN(bufBytes*8 - 64))
			gotC, ndxC := cDecodeHuffmanWord(stream, cb, start)
			nr := nativeaac.NewHuffBitReader(stream, bufBytes, bufBytes*8)
			advance(nr, start)
			gotN := nr.DecodeHuffmanWord(cb)
			ndxN := nr.BitNdx()
			require.Equal(t, gotC, gotN, "cb=%d start=%d value", cb, start)
			require.Equal(t, ndxC, ndxN, "cb=%d start=%d bitpos", cb, start)
		}
	}
}

// TestParityGetEscape sweeps CBlock_GetEscape across the q values that
// exercise every branch: |q|!=16 (passthrough), +16/-16 with short and long
// (overlong-prefix) escape sequences read from the random stream.
func TestParityGetEscape(t *testing.T) {
	strictGate(t)
	stream := makeRandomBytes(31, bufBytes)
	r := rand.New(rand.NewPCG(32, 33))
	qs := []int{0, 1, -1, 15, -15, 16, -16, 17, -17, 8191, -8191}
	for _, q := range qs {
		for i := 0; i < 1000; i++ {
			start := uint32(r.IntN(bufBytes*8 - 64))
			gotC, ndxC := cGetEscape(stream, q, start)
			nr := nativeaac.NewHuffBitReader(stream, bufBytes, bufBytes*8)
			advance(nr, start)
			gotN := nr.GetEscape(q)
			ndxN := nr.BitNdx()
			require.Equal(t, gotC, gotN, "q=%d start=%d value", q, start)
			require.Equal(t, ndxC, ndxN, "q=%d start=%d bitpos", q, start)
		}
	}
}

// TestParityReadSpectralData drives the full non-HCR plain-Huffman branch over
// a range of fabricated ICS-info shapes (codebook assignments, band offsets,
// window-group structures) and compares the unpacked int32 spectrum
// bit-for-bit.
func TestParityReadSpectralData(t *testing.T) {
	strictGate(t)
	r := rand.New(rand.NewPCG(42, 43))

	for trial := 0; trial < 400; trial++ {
		stream := makeRandomBytes(uint64(1000+trial), bufBytes)

		// Fabricate an ICS-info-like shape. Keep the per-window spectrum
		// small so codewords stay well inside the buffer. The granule stride
		// and transmitted-band count bound how many codewords are read.
		granuleLength := 64 + r.IntN(64)            // per-window stride
		windowGroups := 1 + r.IntN(4)               // 1..4 groups
		transmittedBands := 1 + r.IntN(8)           // 1..8 sfb
		windowGroupLen := make([]int, windowGroups) // windows per group
		totalWindows := 0
		for g := range windowGroupLen {
			windowGroupLen[g] = 1 + r.IntN(3)
			totalWindows += windowGroupLen[g]
		}

		// Monotone band offsets in [0, granuleLength], step <= a small bound
		// so an offset window never exceeds the granule.
		bandOffsets := make([]int16, transmittedBands+1)
		off := 0
		for b := 0; b <= transmittedBands; b++ {
			bandOffsets[b] = int16(off)
			// even step keeps Dimension-2 books aligned; cap so we stay in
			// the granule
			step := 2 + 2*r.IntN(3)
			if off+step > granuleLength {
				step = granuleLength - off
				if step < 0 {
					step = 0
				}
			}
			off += step
		}

		// Per-(group*16+band) codebook array. Use the spectral books 1..11
		// plus the special codes (0/13/14/15 skip the band) so both the
		// decode and the skip paths are exercised. Books 16..31 (VCB11) map
		// to 11 on both sides.
		codeBook := make([]byte, windowGroups*16)
		bookChoices := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15, 20}
		for g := 0; g < windowGroups; g++ {
			for b := 0; b < transmittedBands; b++ {
				codeBook[g*16+b] = bookChoices[r.IntN(len(bookChoices))]
			}
		}

		spectrumLen := totalWindows * granuleLength

		// C must see its own mutable codebook copy (the VCB11 patch mutates it
		// in place on both sides); pass independent copies.
		cbC := append([]byte(nil), codeBook...)
		cbN := append([]byte(nil), codeBook...)

		gotC := cReadSpectralData(stream, cbC, bandOffsets, windowGroupLen,
			granuleLength, transmittedBands, spectrumLen)

		nr := nativeaac.NewHuffBitReader(stream, bufBytes, bufBytes*8)
		gotN := make([]int32, spectrumLen)
		nr.ReadSpectralData(cbN, bandOffsets, windowGroupLen,
			granuleLength, transmittedBands, gotN)

		require.Equal(t, gotC, gotN, "trial=%d spectrum mismatch", trial)
		require.Equal(t, cbC, cbN, "trial=%d codebook VCB11-patch mismatch", trial)
	}
}

// advance walks a fresh native reader forward by `start` bits so it lands at
// the same bit position the C side seeks to with FDKpushFor.
func advance(r *nativeaac.HuffBitReader, start uint32) {
	r.SkipBits(start)
}
