// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package enctnsgauss

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"
)

// fl2 mirrors FL2_TIMERES_FIX(a) == FL2FXCONST_DBL(a / 2) (TNS_TIMERES_SCALE==1),
// the time-resolution mantissa the tnsInfoTab carries.
func fl2(a float64) int32 {
	v := a / 2.0
	const scale = 2147483648.0
	t := v*scale + 0.5
	if t >= 2147483647.0 {
		return 0x7FFFFFFF
	}
	return int32(t)
}

// TestCalcGaussWindowParity asserts calcGaussWindow == genuine
// FDKaacEnc_CalcGaussWindow over the realistic AAC-LC TNS configurations
// (granuleLength 1024/128, all sample rates, maxOrder+1 window sizes, the
// tnsInfoTab time-resolution values) plus randomized inputs across the legal
// window-size range. timeResolutionE is TNS_TIMERES_SCALE == 1.
func TestCalcGaussWindowParity(t *testing.T) {
	if !nativeaac.StrictMode {
		t.Skip("integer-parity assertion; run via mise //libraries/aac:parity (aac_strict)")
	}

	sampleRates := []int{8000, 11025, 12000, 16000, 22050, 24000, 32000, 44100, 48000, 64000, 88200, 96000}
	granuleLengths := []int{1024, 960, 128, 120}               // transformResolution
	timeRes := []int32{fl2(0.4), fl2(1.0), fl2(1.2), fl2(0.6)} // tnsInfoTab values
	winSizes := []int{13, 12, 9, 4, 1}                         // maxOrder+1 ; FDK_ASSERT(winSize < 16)

	for _, sr := range sampleRates {
		for _, gl := range granuleLengths {
			for _, tr := range timeRes {
				for _, ws := range winSizes {
					g := cCalcGaussWindow(ws, sr, gl, tr, 1)
					n := nativeaac.ParityCalcGaussWindow(ws, sr, gl, tr, 1)
					assert.Equalf(t, g, n, "sr=%d gl=%d tr=%#x ws=%d", sr, gl, tr, ws)
				}
			}
		}
	}

	// Randomized sweep across the legal parameter ranges.
	r := rand.New(rand.NewSource(0x6A05))
	for i := 0; i < 20000; i++ {
		ws := 1 + r.Intn(15) // 1..15 (< 16)
		sr := 8000 + r.Intn(88000)
		gl := []int{1024, 960, 512, 256, 128, 120}[r.Intn(6)]
		tr := int32(r.Int63n(0x7FFFFFFF))
		tre := r.Intn(3) // 0..2
		g := cCalcGaussWindow(ws, sr, gl, tr, tre)
		n := nativeaac.ParityCalcGaussWindow(ws, sr, gl, tr, tre)
		assert.Equalf(t, g, n, "rand sr=%d gl=%d tr=%#x ws=%d tre=%d", sr, gl, tr, ws, tre)
	}
}
