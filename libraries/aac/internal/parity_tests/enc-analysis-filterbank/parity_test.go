// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encanalysisfilterbank

import (
	"math/rand/v2"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// frameLen is the AAC-LC long-block frame length (nTimeSamples == 1024). The
// forward MDCT produces frameLen FIXP_DBL lines per block: one long spectrum, or
// eight short spectra of frameLen>>3 lines packed back to back.
const frameLen = 1024

// Window types, the WINDOW_TYPE enum (psy_const.h:120-125).
const (
	longWindow  = 0
	startWindow = 1
	shortWindow = 2
	stopWindow  = 3
)

// filterTypeFBLC is FB_LC, the AAC-LC non-ELD filterbank id passed as filterType
// (ignored by FDKaacEnc_Transform_Real on this path). Any non-FB_ELD value works.
const filterTypeFBLC = 0

// aacLCSequence is a realistic AAC-LC window-sequence run that exercises every
// slope transition the analysis MDCT must carry: steady long blocks, a
// long-start into 8 short blocks, then a stop back to long. transform.cpp derives
// fr from blockType (LONG/STOP: fr=frameLen; START/SHORT: fr=frameLen>>3) and the
// left slope/length come from the previous block's prev_wrs/prev_fr inside
// mdct_block. The first long block additionally hits the prev_fr==0 startup.
var aacLCSequence = []int{
	longWindow,
	longWindow,
	startWindow,
	shortWindow,
	stopWindow,
	longWindow,
	longWindow,
}

// blockName maps a WINDOW_TYPE to a label for failure messages.
func blockName(bt int) string {
	switch bt {
	case longWindow:
		return "long"
	case startWindow:
		return "start"
	case shortWindow:
		return "short"
	case stopWindow:
		return "stop"
	}
	return "?"
}

// inputBufLen is MAX_INPUT_BUFFER_SIZE == 2*1024 (psy_const.h:156). The encoder
// hands FDKaacEnc_Transform_Real a 2x-frame psyInputBuffer even though
// noInSamples == frameLen, because the forward mdct_block's 50%-overlap fold
// reads time samples up to index 2*tl-1 (mdct.cpp:248-252).
const inputBufLen = 2 * frameLen

// genPCM fills inputBufLen int16 INT_PCM samples scaled into ±(1<<shift) so the
// sweep covers small inputs through near-full-scale (shift==15 reaches the int16
// boundary). A uniform magnitude across the frame keeps the eight short-block
// exponents in agreement (transform.cpp:159-164), so the short path returns rc==0
// — both sides resolve the same rc regardless, which the test also asserts.
func genPCM(r *rand.Rand, shift int) []int16 {
	x := make([]int16, inputBufLen)
	mask := int32(1)<<uint(shift+1) - 1
	half := int32(1) << uint(shift)
	for i := range x {
		x[i] = int16((int32(r.Uint32()) & mask) - half)
	}
	return x
}

var shifts = []int{4, 9, 14, 15}

// frForBlock returns the right window slope length the C derives from blockType
// for the windowShape != LOL (sine/KBD) AAC-LC path (transform.cpp:140-153).
func frForBlock(bt int) int {
	switch bt {
	case longWindow, stopWindow:
		return frameLen
	default: // start, short
		return frameLen >> 3
	}
}

// nSpecForBlock returns the spectrum count (transform.cpp:132-138).
func nSpecForBlock(bt int) int {
	if bt == shortWindow {
		return 8
	}
	return 1
}

// TestParityTransformRealSequence drives the genuine FDKaacEnc_Transform_Real and
// the ported nativeaac TransformReal through the same stateful AAC-LC window
// sequence over random PCM, asserting the int32 MDCT spectrum, the return code,
// the published block exponent, AND the updated prevWindowShape match bit-for-bit
// at EVERY block. The analysis MDCT is stateful (each block's left slope is the
// previous block's right slope via prev_wrs/prev_fr), so a single divergence
// anywhere propagates to every later block.
func TestParityTransformRealSequence(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc analysis filterbank parity asserts under -tags aac_strict")
	}
	const shape = 0 // SINE_WINDOW — AAC-LC window_shape 0
	for _, shift := range shifts {
		for run := 0; run < 25; run++ {
			r := rand.New(rand.NewPCG(0xF11B, uint64(shift)<<8|uint64(run)))

			cSt := cNewEncState()
			nSt := nativeaac.NewEncMdctState(0)

			cPrevShape := shape
			nPrevShape := shape
			nonZero := false

			for bi, bt := range aacLCSequence {
				pcm := genPCM(r, shift)

				cOut, cRc, cE, cPS := cSt.cTransformReal(pcm, bt, shape, cPrevShape,
					frameLen, filterTypeFBLC)

				nOut := make([]int32, frameLen)
				nRc, nE, nPS := nSt.TransformReal(append([]int16(nil), pcm...), nOut,
					bt, shape, nPrevShape, frameLen)

				name := blockName(bt)
				require.Equal(t, cRc, nRc, "shift=%d run=%d blk=%d(%s) rc", shift, run, bi, name)
				require.Equal(t, cE, nE, "shift=%d run=%d blk=%d(%s) mdctData_e", shift, run, bi, name)
				require.Equal(t, cPS, nPS, "shift=%d run=%d blk=%d(%s) prevWindowShape", shift, run, bi, name)
				require.Equal(t, cOut, nOut, "shift=%d run=%d blk=%d(%s) mdctSpectrum", shift, run, bi, name)

				cPrevShape = cPS
				nPrevShape = nPS

				for _, v := range cOut {
					if v != 0 {
						nonZero = true
						break
					}
				}
			}
			require.True(t, nonZero, "shift=%d run=%d produced all-zero spectrum (degenerate)", shift, run)

			cSt.free()
		}
	}
}

// TestParityWindowSlopeROM verifies the genuine FDKgetWindowSlope ROM the encoder
// analysis MDCT selects (transform.cpp:155) matches the ported radix-2 selector
// the Go MdctBlockFwd uses, for both AAC-LC slope lengths (1024 long, 128 short)
// and both shapes (sine, KBD), so the fold is driven with identical window data.
func TestParityWindowSlopeROM(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("enc analysis filterbank parity asserts under -tags aac_strict")
	}
	for _, length := range []int{1024, 128} {
		for _, shape := range []int{0, 1} {
			cW := cWindowSlope(length, shape, length/2)
			nW := nativeaac.WindowSlopeRadix2Flat(length, shape)
			require.Equal(t, cW, nW, "FDKgetWindowSlope length=%d shape=%d", length, shape)
		}
	}
}
