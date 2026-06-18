// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package filterbank

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// sineTable1024Len is N/2+1 == 513, the SineTable1024 entry count dct_getTables
// selects for every radix-2 length the inner dct_IV strides by sin_step (16 for
// tl=128, 2 for tl=1024) — the same constant the sibling dct oracle uses.
const sineTable1024Len = 513

// frameLen is the AAC-LC long-block transform length (1024). Short blocks are
// frameLen>>3 (128) with nSpec==8; start/stop blocks mix a 1024 and a 128 slope.
const frameLen = 1024

// block describes one imlt_block call: its window sequence resolved to the
// (tl, nSpec, fl, fr) the C CBlock_FrequencyToTime derives (block.cpp:1035-1063).
type block struct {
	name  string
	tl    int
	nSpec int
	fl    int
	fr    int
}

// aacLCSequence is a realistic AAC-LC window-sequence run that exercises every
// slope transition the IMDCT overlap-add must carry: steady long blocks, a
// long-start into 8 short blocks, then a stop back to long. The fl/fr come
// straight from the switch in CBlock_FrequencyToTime (BLOCK_LONG: fl=fr=1024;
// BLOCK_START: fl=1024,fr=128; BLOCK_SHORT: fl=fr=128,tl=128,nSpec=8;
// BLOCK_STOP: fl=128,fr=1024). The first long block additionally hits the
// prev_tl==0 startup (fl=fr) inside imlt_block.
var aacLCSequence = []block{
	{"long0", 1024, 1, 1024, 1024},
	{"long1", 1024, 1, 1024, 1024},
	{"start", 1024, 1, 1024, 128},
	{"short", 128, 8, 128, 128},
	{"stop", 1024, 1, 128, 1024},
	{"long2", 1024, 1, 1024, 1024},
	{"long3", 1024, 1, 1024, 1024},
}

// genSpectrum fills nSpec*tl int32 with Q1.31 values right-shifted by `shift`
// bits of headroom — the IMDCT is handed dequantized/TNS'd spectra well inside
// full magnitude, so the sweep covers the operating range plus the saturation
// boundary at shift==0.
func genSpectrum(r *rand.Rand, n, shift int) []int32 {
	x := make([]int32, n)
	for i := range x {
		x[i] = int32(r.Uint32()) >> shift
	}
	return x
}

// genScale fills nSpec SHORT scalefactors (the per-spectrum input exponents the
// MDCT carries) in a small range matching the decoder's specScale.
func genScale(r *rand.Rand, nSpec int) []int16 {
	s := make([]int16, nSpec)
	for i := range s {
		s[i] = int16(r.IntN(8)) // 0..7, the typical specScale band
	}
	return s
}

var shifts = []int{8, 4, 0}

// TestParityImltBlockSequence drives the genuine imlt_block and the ported
// nativeaac.ImltBlock through the same stateful AAC-LC window sequence over
// random spectra, asserting the int32 time output AND the returned sample count
// match bit-for-bit at EVERY block (the overlap-add carry makes each block
// depend on all prior ones, so a single-ULP divergence anywhere propagates).
func TestParityImltBlockSequence(t *testing.T) {
	const shape = 0 // SHAPE_SINE — AAC-LC window_shape 0
	for _, shift := range shifts {
		for run := 0; run < 25; run++ {
			r := rand.New(rand.NewPCG(0x1117B, uint64(shift)<<8|uint64(run)))

			cSt := cNewState()
			nSt := nativeaac.NewMdctState(768)

			nonZero := false
			for _, b := range aacLCSequence {
				tw, st, sinStep := cDctTables(b.tl, b.tl/2, sineTable1024Len)
				wls := cWindowSlope(b.fl, shape, b.fl/2)
				wrs := cWindowSlope(b.fr, shape, b.fr/2)
				scale := genScale(r, b.nSpec)

				spec := genSpectrum(r, b.nSpec*b.tl, shift)

				// imlt_block modifies spectrum in place; each side gets a copy.
				cOut, cN := cSt.cImltBlock(append([]int32(nil), spec...), scale,
					b.nSpec, frameLen, b.tl, wls, b.fl, wrs, b.fr, 0, 0)

				nOut := make([]int32, frameLen)
				nN := nSt.ImltBlock(nOut, append([]int32(nil), spec...), scale,
					b.nSpec, frameLen, b.tl, wls, b.fl, wrs, b.fr, 0, 0, sinStep, tw, st)

				require.Equal(t, cN, nN, "shift=%d run=%d block=%s sampleCount", shift, run, b.name)
				require.Equal(t, cOut, nOut, "shift=%d run=%d block=%s timeOutput", shift, run, b.name)

				for _, v := range cOut {
					if v != 0 {
						nonZero = true
						break
					}
				}
			}
			// Guard against a degenerate all-zero false pass: a random spectrum
			// run must yield non-trivial time output somewhere in the sequence.
			require.True(t, nonZero, "shift=%d run=%d produced all-zero output (degenerate)", shift, run)

			cSt.free()
		}
	}
}

// TestParityScaleOut verifies the AAC-LC FrequencyToTime output tail
// (scaleValuesSaturate(dst,src,len,scale), block.cpp:1240) — the saturating
// shift that lands the imlt_block output in the PCM buffer — matches bit-for-bit
// across the full scale range, including the saturating boundaries.
func TestParityScaleOut(t *testing.T) {
	r := rand.New(rand.NewPCG(0x5CA1E, 0x0))
	scales := []int{-31, -8, -2, -1, 0, 1, 2, 8, 31}
	for _, shift := range shifts {
		for _, sc := range scales {
			for trial := 0; trial < 50; trial++ {
				src := genSpectrum(r, frameLen, shift)
				gotC := cScaleOut(src, frameLen, sc)
				gotN := make([]int32, frameLen)
				nativeaac.ScaleValuesSaturateDst(gotN, append([]int32(nil), src...), frameLen, int32(sc))
				require.Equal(t, gotC, gotN, "shift=%d scale=%d trial=%d", shift, sc, trial)
			}
		}
	}
}
