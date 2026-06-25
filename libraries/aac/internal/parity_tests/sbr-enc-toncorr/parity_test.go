// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrenctoncorr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// lcg is a tiny deterministic generator so the Go and C sides see identical
// (and reproducible) pseudo-random inputs.
type lcg struct{ s uint64 }

func (g *lcg) next() uint64 { g.s = g.s*6364136223846793005 + 1442695040888963407; return g.s }

// q31 returns a deterministic FIXP_DBL-range int32 (signed Q31) for QMF inputs.
func (g *lcg) q31() int32 { return int32(g.next() >> 33) } // 31-bit magnitude, signed

// TestCalculateTonalityQuotas drives FDKsbrEnc_CalculateTonalityQuotas (C) and
// the Go port over identical seeded state + complex QMF source, across a handful
// of configs, and asserts the full quota/sign/nrg output state is bit-identical.
func TestCalculateTonalityQuotas(t *testing.T) {
	type cfg struct {
		name                                                          string
		lpcLen0, lpcLen1, noQmfChannels, buffLen, usb, qmfScale       int
		numberOfEstimates, numberOfEstimatesPerFrame, move, startIdxM int
	}
	// NUMBER_TIME_SLOTS_2048 AAC tuning: lpcLength = 16-LPC_ORDER = 14, stepSize
	// = lpcLength[0]+LPC_ORDER = 16, nextSample = LPC_ORDER = 2, numberOfEstimates
	// = NO_OF_ESTIMATES_LC = 4, numberOfEstimatesPerFrame = noQmfSlots/16 = 2,
	// move = startIndexMatrix = 4-2 = 2, bufferLength = noQmfSlots = 32.
	cases := []cfg{
		{"slots2048_usb40", 14, 14, 64, 32, 40, 3, 4, 2, 2, 2},
		{"slots2048_usb24", 14, 14, 64, 32, 24, 5, 4, 2, 2, 2},
		{"slots1920_usb32", 13, 13, 64, 30, 32, 4, 4, 2, 2, 2},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			const stride = 64 // srcStride: usb+NUM_V_COMBINE worst case fits in 64
			stepSize := c.lpcLen0 + 2
			nextSample := 2

			g := &lcg{s: 0x1234_5678_9abc_def0 ^ uint64(c.usb)}
			quotaIn := make([]int32, 4*64)
			signIn := make([]int32, 4*64)
			nrgIn := make([]int32, 4)
			for i := range quotaIn {
				quotaIn[i] = g.q31() >> 4
				signIn[i] = int32(int8(g.next()))%2*2 - 1
			}
			for i := range nrgIn {
				nrgIn[i] = g.q31() >> 6
			}
			srcReal := make([]int32, c.buffLen*stride)
			srcImag := make([]int32, c.buffLen*stride)
			for i := range srcReal {
				srcReal[i] = g.q31() >> 3
				srcImag[i] = g.q31() >> 3
			}

			cQ, cS, cN, cNF := cQuotas(c.lpcLen0, c.lpcLen1, stepSize, nextSample,
				c.move, c.startIdxM, c.numberOfEstimates, c.numberOfEstimatesPerFrame,
				c.noQmfChannels, c.buffLen, c.usb, c.qmfScale, stride,
				quotaIn, signIn, nrgIn, srcReal, srcImag)

			gQ, gS, gN, gNF := sbr.CalculateTonalityQuotasForParity(c.lpcLen0, c.lpcLen1,
				stepSize, nextSample, c.move, c.startIdxM, c.numberOfEstimates,
				c.numberOfEstimatesPerFrame, c.noQmfChannels, c.buffLen, c.usb,
				c.qmfScale, stride, quotaIn, signIn, nrgIn, srcReal, srcImag)

			require.Equal(t, cQ, gQ, "quotaMatrix")
			require.Equal(t, cS, gS, "signMatrix")
			require.Equal(t, cN, gN, "nrgVector")
			require.Equal(t, cNF, gNF, "nrgVectorFreq")
		})
	}
}

// TestResetPatch drives the file-static resetPatch (C tap) and the Go port over
// a range of v_k_master tables / crossover configs and asserts the patch table +
// index vector + noOfPatches are bit-identical.
func TestResetPatch(t *testing.T) {
	type cfg struct {
		name       string
		vk         []uint8
		highStart  int
		fs         int
		noChannels int
		xposctrl   int
		guard      int
		shiftStart int
	}
	cases := []cfg{
		{
			name:      "44100_xover6",
			vk:        []uint8{6, 7, 8, 9, 10, 11, 13, 15, 17, 19, 22, 25, 29, 33, 38, 44, 51},
			highStart: 6, fs: 44100, noChannels: 64, xposctrl: 0, guard: 0, shiftStart: 1,
		},
		{
			name:      "32000_xover5",
			vk:        []uint8{5, 6, 7, 8, 9, 11, 13, 15, 18, 21, 25, 30, 36, 43, 51},
			highStart: 5, fs: 32000, noChannels: 64, xposctrl: 0, guard: 0, shiftStart: 1,
		},
		{
			name:      "48000_xover7_xposctrl1",
			vk:        []uint8{7, 8, 9, 10, 12, 14, 16, 19, 22, 26, 31, 37, 44, 52},
			highStart: 7, fs: 48000, noChannels: 64, xposctrl: 1, guard: 0, shiftStart: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			numMaster := len(c.vk) - 1
			cP, cIdx, cN := cPatch(c.xposctrl, c.highStart, c.vk, numMaster, c.fs,
				c.noChannels, c.guard, c.shiftStart)
			gP, gIdx, gN := sbr.ResetPatchForParity(c.xposctrl, c.highStart, c.vk,
				numMaster, c.fs, c.noChannels, c.guard, c.shiftStart)

			assert.Equal(t, cN, gN, "noOfPatches")
			require.Equal(t, cP, gP, "patchParam")
			require.Equal(t, cIdx, gIdx, "indexVector")
		})
	}
}
