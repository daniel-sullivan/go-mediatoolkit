// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package dct

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// aacLCLengths are the transform lengths the AAC-LC inverse filterbank feeds the
// DCT: tl == 1024 (long block) and tl == 128 (short block). Both are radix-2
// lengths routed to dct_getTables' case 0x4 (SineTable1024 + SineWindowXXX), and
// the inner fft(M) lands on the supported dit_fft sizes (512 / 64).
var aacLCLengths = []int{128, 1024}

// sineTable1024Len is N/2+1 == 513, the entry count of the genuine SineTable1024
// dct_getTables selects for every radix-2 length (both 128 and 1024). The kernels
// stride this single table by sin_step (16 for L=128, 2 for L=1024), so the whole
// table — not an M+1-entry slice — must be copied out; the windowSlopes twiddle
// table, indexed contiguously by i, holds exactly M entries.
const sineTable1024Len = 513

// genQ31 fills a length-n int32 buffer with Q1.31 values right-shifted by `shift`
// bits of headroom — the MDCT hands the DCT post-scaled, reduced-magnitude
// spectra, so `shift` lets the tests sweep from a few bits of headroom up to full
// int32 magnitude.
func genQ31(r *rand.Rand, n, shift int) []int32 {
	x := make([]int32, n)
	for i := range x {
		x[i] = int32(r.Uint32()) >> shift
	}
	return x
}

// shifts sweeps the working magnitude range: 8/4 bits of headroom (the MDCT's
// actual operating range) and 0 (full int32, exercising the >>1 prescales and
// twiddle-multiply rounding at the saturation boundary).
var shifts = []int{8, 4, 0}

// TestParityDctGetTablesSinStep verifies the ported sin_step selection
// (dctGetTablesSinStep) equals the genuine dct_getTables sin_step for every
// AAC-LC transform length.
func TestParityDctGetTablesSinStep(t *testing.T) {
	for _, L := range aacLCLengths {
		M := L >> 1
		_, _, cStep := cGetTables(L, M, sineTable1024Len)
		require.Equal(t, cStep, nativeaac.DctGetTablesSinStep(L), "L=%d", L)
	}
}

// TestParityDctIV drives dct_IV — the AAC-LC inverse-filterbank core
// (mdct.cpp:520) — over random spectra and compares the in-place int32 output
// AND the exponent delta (the e in the MDCT's (mantissa,exponent) pair)
// bit-for-bit against the vendored C, using the genuine dct_getTables ROM.
func TestParityDctIV(t *testing.T) {
	r := rand.New(rand.NewPCG(0xDC4, 0x1024))
	for _, L := range aacLCLengths {
		M := L >> 1
		tw, st, sinStep := cGetTables(L, M, sineTable1024Len)
		for _, shift := range shifts {
			for trial := 0; trial < 200; trial++ {
				x := genQ31(r, L, shift)

				gotC, eC := cDctIV(x, L)

				gotN := append([]int32(nil), x...)
				eN := nativeaac.DctIV(gotN, L, sinStep, tw, st)

				require.Equal(t, gotC, gotN, "L=%d shift=%d trial=%d", L, shift, trial)
				require.Equal(t, eC, eN, "L=%d exponent", L)
			}
		}
	}
}

// TestParityDstIV drives dst_IV (the alias-symmetry IV path) the same way.
func TestParityDstIV(t *testing.T) {
	r := rand.New(rand.NewPCG(0xD57, 0x1004))
	for _, L := range aacLCLengths {
		M := L >> 1
		tw, st, sinStep := cGetTables(L, M, sineTable1024Len)
		for _, shift := range shifts {
			for trial := 0; trial < 200; trial++ {
				x := genQ31(r, L, shift)

				gotC, eC := cDstIV(x, L)

				gotN := append([]int32(nil), x...)
				eN := nativeaac.DstIV(gotN, L, sinStep, tw, st)

				require.Equal(t, gotC, gotN, "L=%d shift=%d trial=%d", L, shift, trial)
				require.Equal(t, eC, eN, "L=%d exponent", L)
			}
		}
	}
}

// TestParityDctIII drives dct_III (the III alias path) — only the sin_twiddle ROM
// is consulted. tmp is the scratch buffer (length L).
func TestParityDctIII(t *testing.T) {
	r := rand.New(rand.NewPCG(0xD3, 0x111))
	for _, L := range aacLCLengths {
		M := L >> 1
		_, st, sinStep := cGetTables(L, M, sineTable1024Len)
		for _, shift := range shifts {
			for trial := 0; trial < 200; trial++ {
				x := genQ31(r, L, shift)

				gotC, eC := cDctIII(x, L)

				gotN := append([]int32(nil), x...)
				tmp := make([]int32, L)
				eN := nativeaac.DctIII(gotN, tmp, L, sinStep, st)

				require.Equal(t, gotC, gotN, "L=%d shift=%d trial=%d", L, shift, trial)
				require.Equal(t, eC, eN, "L=%d exponent", L)
			}
		}
	}
}

// TestParityDstIII drives dst_III (mirrored-input + odd-sign-flip reuse of
// dct_III).
func TestParityDstIII(t *testing.T) {
	r := rand.New(rand.NewPCG(0xD33, 0x113))
	for _, L := range aacLCLengths {
		M := L >> 1
		_, st, sinStep := cGetTables(L, M, sineTable1024Len)
		for _, shift := range shifts {
			for trial := 0; trial < 200; trial++ {
				x := genQ31(r, L, shift)

				gotC, eC := cDstIII(x, L)

				gotN := append([]int32(nil), x...)
				tmp := make([]int32, L)
				eN := nativeaac.DstIII(gotN, tmp, L, sinStep, st)

				require.Equal(t, gotC, gotN, "L=%d shift=%d trial=%d", L, shift, trial)
				require.Equal(t, eC, eN, "L=%d exponent", L)
			}
		}
	}
}

// TestParityDctII drives dct_II (the analysis-side companion) the same way.
func TestParityDctII(t *testing.T) {
	r := rand.New(rand.NewPCG(0xD2, 0x222))
	for _, L := range aacLCLengths {
		M := L >> 1
		_, st, sinStep := cGetTables(L, M, sineTable1024Len)
		for _, shift := range shifts {
			for trial := 0; trial < 200; trial++ {
				x := genQ31(r, L, shift)

				gotC, eC := cDctII(x, L)

				gotN := append([]int32(nil), x...)
				tmp := make([]int32, L)
				eN := nativeaac.DctII(gotN, tmp, L, sinStep, st)

				require.Equal(t, gotC, gotN, "L=%d shift=%d trial=%d", L, shift, trial)
				require.Equal(t, eC, eN, "L=%d exponent", L)
			}
		}
	}
}
