// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package fft

import (
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// TestParitySineTable512Q15 verifies the Go STC-narrowed twiddle ROM
// (FX_DBL2FXCONST_SGL applied to the raw Q1.31 SineTable512 constants) matches
// the genuine in-RAM SineTable512 the SINETABLE_16BIT C build links, entry for
// entry, across all 257 (N/2+1) entries.
func TestParitySineTable512Q15(t *testing.T) {
	const count = 257
	gotC := cSineTable512Q15(count)
	gotN := nativeaac.SineTable512Q15()
	require.Len(t, gotN, count)
	require.Equal(t, gotC, gotN)
}

// ditFFTLengths covers the FFT sizes dit_fft supports: ldn 3..9 == lengths
// 8..512. The AAC-LC filterbank only routes 64/128/256/512 (ldn 6..9) through
// dit_fft, but the kernel is size-generic and the smaller sizes exercise the
// stage loop boundary (block 1 / inner / block 2) just as hard.
var ditFFTLengths = []int{3, 4, 5, 6, 7, 8, 9}

// TestParityDitFFTRandom drives dit_fft over uniformly random Q1.31 spectra for
// each supported ldn and compares the in-place int32 output bit-for-bit against
// the vendored C kernel + ROM.
func TestParityDitFFTRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(0xF7, 0x2A))

	for _, ldn := range ditFFTLengths {
		n := 1 << ldn
		for trial := 0; trial < 400; trial++ {
			x := make([]int32, 2*n)
			for i := range x {
				x[i] = int32(r.Uint32())
			}

			gotC := cDitFFT(x, ldn)

			gotN := append([]int32(nil), x...)
			nativeaac.DitFFT(gotN, ldn)

			require.Equal(t, gotC, gotN, "ldn=%d trial=%d", ldn, trial)
		}
	}
}

// TestParityDitFFTReduced drives reduced-magnitude spectra (the post-scaling
// headroom range the MDCT actually hands the FFT) so the >>1 prescales and the
// twiddle-multiply rounding are exercised in their working range, not just at
// full int32 magnitude.
func TestParityDitFFTReduced(t *testing.T) {
	r := rand.New(rand.NewPCG(0x55, 0x99))

	for _, ldn := range ditFFTLengths {
		n := 1 << ldn
		for trial := 0; trial < 400; trial++ {
			x := make([]int32, 2*n)
			for i := range x {
				// Q1.31 values within [-2^24, 2^24): a few bits of headroom.
				x[i] = int32(r.Uint32()) >> 7
			}

			gotC := cDitFFT(x, ldn)

			gotN := append([]int32(nil), x...)
			nativeaac.DitFFT(gotN, ldn)

			require.Equal(t, gotC, gotN, "ldn=%d trial=%d", ldn, trial)
		}
	}
}

// TestParityDitFFTExtremes drives saturation-prone inputs (near-MAXVAL /
// near-MINVAL / zero lines) so the butterfly add/sub overflow behaviour and the
// int64-product>>32 twiddle kernels are pinned at the int32 boundary.
func TestParityDitFFTExtremes(t *testing.T) {
	r := rand.New(rand.NewPCG(0xABC, 0xDEF))

	const (
		maxv = int32(0x7FFFFFFF)
		minv = int32(-0x80000000)
	)
	extremes := []int32{maxv, minv, maxv - 1, minv + 1, maxv / 2, minv / 2, 0, 1, -1}

	for _, ldn := range ditFFTLengths {
		n := 1 << ldn
		for trial := 0; trial < 300; trial++ {
			x := make([]int32, 2*n)
			for i := range x {
				x[i] = extremes[r.IntN(len(extremes))]
			}

			gotC := cDitFFT(x, ldn)

			gotN := append([]int32(nil), x...)
			nativeaac.DitFFT(gotN, ldn)

			require.Equal(t, gotC, gotN, "ldn=%d trial=%d", ldn, trial)
		}
	}
}

// TestParityFFTDispatcher drives the fft() dispatcher slice (the 64/128/256/512
// AAC-LC filterbank lengths) and checks both the in-place spectrum and the
// SCALEFACTOR<n> added to the block exponent — the (mantissa, exponent) pair the
// MDCT carries — against running dit_fft directly with the matching scalefactor.
func TestParityFFTDispatcher(t *testing.T) {
	r := rand.New(rand.NewPCG(0x1234, 0x5678))

	cases := []struct {
		length int
		sf     int
	}{
		{64, 5}, {128, 6}, {256, 7}, {512, 8},
	}

	for _, tc := range cases {
		for trial := 0; trial < 200; trial++ {
			x := make([]int32, 2*tc.length)
			for i := range x {
				x[i] = int32(r.Uint32()) >> 4
			}

			// C reference: dit_fft with the same ROM the dispatcher uses.
			ldn := 0
			for (1 << ldn) < tc.length {
				ldn++
			}
			gotC := cDitFFT(x, ldn)

			gotN := append([]int32(nil), x...)
			gotSF := nativeaac.FFT(tc.length, gotN)

			require.Equal(t, gotC, gotN, "length=%d trial=%d", tc.length, trial)
			require.Equal(t, tc.sf, gotSF, "length=%d scalefactor", tc.length)
		}
	}
}
