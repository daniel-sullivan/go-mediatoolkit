// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrencresampler

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// goResample streams `in` through the Go downsampler in `blocks` chunks (state
// persists across calls, as in the C path).
func goResample(wc, ratio int, in []int16, blocks int) (out []int16, delay int) {
	var d sbr.Downsampler
	sbr.InitDownsampler(&d, wc, ratio)
	per := len(in) / blocks
	outBuf := make([]int16, len(in)/ratio+blocks)
	total := 0
	for b := 0; b < blocks; b++ {
		n := sbr.Downsample(&d, in[b*per:(b+1)*per], per, outBuf[total:])
		total += n
	}
	return outBuf[:total], d.Delay()
}

// TestDownsample drives the C resampler and the Go port over several cutoff
// settings (selecting each of the five coefficient sets) and signal shapes, in
// multiple block sizes, and asserts the int16 outputs + reported delay match
// bit-for-bit.
func TestDownsample(t *testing.T) {
	// Cutoffs chosen to land on each filter set: Wc selection picks the first
	// set whose Wc > the requested wc. >=480 -> set48; 450..479 -> set45;
	// 410..449 -> set41; 350..409 -> set35; <=349 -> set25.
	wcs := []int{500, 460, 420, 360, 300}
	const ratio = 2
	const nIn = 2048 // multiple of ratio and of every block count

	for _, wc := range wcs {
		// Build a deterministic test signal: a sum of tones + a chirp + noise.
		in := make([]int16, nIn)
		for i := range in {
			v := 0.5*math.Sin(2*math.Pi*float64(i)*0.013) +
				0.3*math.Sin(2*math.Pi*float64(i)*0.21) +
				0.15*math.Sin(2*math.Pi*float64(i)*float64(i)*1e-5)
			// add a tiny deterministic dither
			v += float64((i*2654435761)&0xff-128) / 4096.0
			s := int(math.Round(v * 30000))
			if s > 32767 {
				s = 32767
			} else if s < -32768 {
				s = -32768
			}
			in[i] = int16(s)
		}

		for _, blocks := range []int{1, 2, 4, 8} {
			cOut, cDelay := cResample(wc, ratio, in, blocks)
			gOut, gDelay := goResample(wc, ratio, in, blocks)
			require.Equal(t, cDelay, gDelay, "delay wc=%d", wc)
			require.Equal(t, cOut, gOut, "output wc=%d blocks=%d", wc, blocks)
		}
	}
}
