// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package dequant

import (
	"math/rand/v2"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// maxQuantizedValue mirrors MAX_QUANTIZED_VALUE (channelinfo.h:148): the live
// decode feeds the inverse quantizer band maxima in [0, 8191]. Bounding the
// fabricated spectrum here keeps both sides on the real inverse-quantize path
// (the > MAX_QUANTIZED_VALUE guard at block.cpp:543 returns a parse error and
// short-circuits the band otherwise — which both sides would still take in
// lockstep, but we exercise the rescale kernels by staying in range).
const maxQuantizedValue = 8191

// AAC-LC 1024-sample-frame parameters: samplesPerFrame 1024, a representative
// 44.1 kHz sampling-rate index (4) for the long-block scalefactor-band ROM.
const (
	samplesPerFrame   = 1024
	samplingRateIndex = 4
	samplingRate      = 44100
)

// regularBooks are the spectral Huffman codebooks (1..11). The scalefactor read
// decodes a BOOKSCL delta for every band whose section codebook is one of these
// (and is left at 0 for ZERO_HCB). We avoid INTENSITY_HCB(2)/NOISE_HCB in the
// section layout for the long-block sweep so every transmitted band drives the
// regular scalefactor-decode path; a dedicated test exercises NOISE_HCB (PNS).
var regularBooks = []uint8{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}

// fabricateLongBlock builds a fabricated AAC-LC long-block dequant context: a
// random section codebook layout over the transmitted bands (a mix of regular
// books and ZERO_HCB), a random scalefactor-data bitstream, and a random
// quantized spectrum bounded to [-8191, 8191]. The same context is fed to both
// the C oracle and the Go port.
func fabricateLongBlock(r *rand.Rand, includeNoise bool) (
	sfBuf []byte, codeBook [8 * 16]uint8, maxSfb uint8, spectrum []int32) {

	// Long block: 1 window group, 1 window, up to the long-block band count.
	// Use a healthy max_sfb so several bands are transmitted; the band offsets
	// for index 4 long blocks run out to 1024.
	maxSfb = uint8(20 + r.IntN(20)) // 20..39 transmitted bands

	for band := 0; band < int(maxSfb); band++ {
		switch r.IntN(4) {
		case 0:
			codeBook[band] = 0 // ZERO_HCB
		default:
			codeBook[band] = regularBooks[r.IntN(len(regularBooks))]
		}
	}
	if includeNoise {
		// Stamp a couple of NOISE_HCB (PNS) bands to drive the CPns_Read path.
		codeBook[1] = 13 // NOISE_HCB
		if maxSfb > 5 {
			codeBook[5] = 13
		}
	}

	// Scalefactor-data bitstream: random bytes. Both readers walk the genuine
	// BOOKSCL Huffman tree (and the 9-bit PNS start / further deltas)
	// identically, so any byte string parses in lockstep; the delta
	// accumulators are compared bit-for-bit. A power-of-two buffer satisfies the
	// FDK_BITBUF invariant.
	sfBuf = make([]byte, 256)
	for i := range sfBuf {
		sfBuf[i] = byte(r.IntN(256))
	}

	// Quantized spectrum: one long window of samplesPerFrame lines, bounded.
	spectrum = make([]int32, samplesPerFrame)
	for i := range spectrum {
		spectrum[i] = int32(r.IntN(2*maxQuantizedValue+1) - maxQuantizedValue)
	}
	return
}

// TestParityDequantLongBlock sweeps the full AAC-LC long-block dequant stage —
// readScaleFactorData → inverseQuantizeSpectralData → scaleSpectralData — over
// many fabricated channel contexts, asserting EXACT int32/int16 equality of the
// scaled spectrum, the per-band scalefactors, the per-(window,sfb) and
// per-window block exponents, and both driver return codes.
func TestParityDequantLongBlock(t *testing.T) {
	r := rand.New(rand.NewPCG(0xDEC0, 0x0DE5))

	for trial := 0; trial < 4000; trial++ {
		sfBuf, codeBook, maxSfb, spectrum := fabricateLongBlock(r, false)

		var wgl [8]uint8
		wgl[0] = 1 // 1 window per group (long block)

		in := nativeaac.DequantInput{
			SamplesPerFrame:   samplesPerFrame,
			SamplingRateIndex: samplingRateIndex,
			SamplingRate:      samplingRate,
			GlobalGain:        uint8(r.IntN(256)),
			Flags:             0,
			WindowSequence:    0, // BLOCK_LONG
			WindowGroups:      1,
			WindowGroupLength: wgl,
			MaxSfBands:        maxSfb,
			CodeBook:          codeBook,
		}

		cRes, cSpec := cDequant(sfBuf, uint32(len(sfBuf)*8), samplesPerFrame,
			samplingRateIndex, samplingRate, in.GlobalGain, in.Flags,
			in.WindowSequence, in.WindowGroups, wgl, 0, maxSfb, codeBook, spectrum)

		nRes := nativeaac.RunDequant(in, sfBuf, uint32(len(sfBuf)), uint32(len(sfBuf)*8), spectrum)

		require.Equal(t, cRes.readSfErr, nRes.ReadSfErr, "trial=%d readSfErr", trial)
		require.Equal(t, cRes.invQuantErr, nRes.InvQuantErr, "trial=%d invQuantErr", trial)
		require.Equal(t, cRes.scaleFactor, nRes.ScaleFactor, "trial=%d scaleFactor", trial)
		require.Equal(t, cRes.sfbScale, nRes.SfbScale, "trial=%d sfbScale", trial)
		require.Equal(t, cRes.specScale, nRes.SpecScale, "trial=%d specScale", trial)
		require.Equal(t, cSpec, nRes.Spectrum, "trial=%d spectrum", trial)
	}
}

// TestParityDequantPNS drives the PNS (NOISE_HCB) scalefactor path through
// CPns_Read: noise bands seed the first-band 9-bit energy and accumulate
// subsequent BOOKSCL deltas, and the inverse quantizer leaves them a
// ((scalefactor>>2)+1) headroom exponent. Both sides are compared bit-for-bit.
func TestParityDequantPNS(t *testing.T) {
	r := rand.New(rand.NewPCG(0x9015, 0xE0FF))

	for trial := 0; trial < 2000; trial++ {
		sfBuf, codeBook, maxSfb, spectrum := fabricateLongBlock(r, true)

		var wgl [8]uint8
		wgl[0] = 1

		in := nativeaac.DequantInput{
			SamplesPerFrame:   samplesPerFrame,
			SamplingRateIndex: samplingRateIndex,
			SamplingRate:      samplingRate,
			GlobalGain:        uint8(r.IntN(256)),
			Flags:             0,
			WindowSequence:    0,
			WindowGroups:      1,
			WindowGroupLength: wgl,
			MaxSfBands:        maxSfb,
			CodeBook:          codeBook,
		}

		cRes, cSpec := cDequant(sfBuf, uint32(len(sfBuf)*8), samplesPerFrame,
			samplingRateIndex, samplingRate, in.GlobalGain, in.Flags,
			in.WindowSequence, in.WindowGroups, wgl, 0, maxSfb, codeBook, spectrum)

		nRes := nativeaac.RunDequant(in, sfBuf, uint32(len(sfBuf)), uint32(len(sfBuf)*8), spectrum)

		require.Equal(t, cRes.readSfErr, nRes.ReadSfErr, "trial=%d readSfErr", trial)
		require.Equal(t, cRes.invQuantErr, nRes.InvQuantErr, "trial=%d invQuantErr", trial)
		require.Equal(t, cRes.scaleFactor, nRes.ScaleFactor, "trial=%d scaleFactor", trial)
		require.Equal(t, cRes.sfbScale, nRes.SfbScale, "trial=%d sfbScale", trial)
		require.Equal(t, cRes.specScale, nRes.SpecScale, "trial=%d specScale", trial)
		require.Equal(t, cSpec, nRes.Spectrum, "trial=%d spectrum", trial)
	}
}

// TestParityDequantShortBlock sweeps the eight-short-window dequant path: the
// window grouping derived from a random scale_factor_grouping drives the
// per-(group,window) loops in all three drivers, and the granule stride is
// samplesPerFrame/8. Outputs are compared bit-for-bit.
func TestParityDequantShortBlock(t *testing.T) {
	r := rand.New(rand.NewPCG(0x5409, 0xB10C))

	for trial := 0; trial < 4000; trial++ {
		// Short block: max_sfb is 4 bits; build a grouping + section layout per
		// group. Use the short-block band count for ROM index 4 (up to ~14).
		maxSfb := uint8(8 + r.IntN(6)) // 8..13 transmitted bands

		// scale_factor_grouping (7 bits) -> window groups + per-group lengths,
		// reproduced via the Go port's own ics_info derivation by feeding it to
		// the cIcsInfo the export rebuilds; here we just supply the raw byte and
		// the derived WindowGroupLength the C accessor reads.
		sfg := uint8(r.IntN(128))

		// Derive WindowGroups / WindowGroupLength exactly as IcsRead
		// (channelinfo.cpp) does for short blocks, so both sides agree.
		var wgl [8]uint8
		groups := uint8(0)
		for i := 0; i < 7; i++ {
			mask := uint32(1) << uint(6-i)
			wgl[i] = 1
			if uint32(sfg)&mask != 0 {
				wgl[groups]++
			} else {
				groups++
			}
		}
		wgl[7] = 1
		groups++

		var codeBook [8 * 16]uint8
		for g := 0; g < int(groups); g++ {
			for band := 0; band < int(maxSfb); band++ {
				if r.IntN(4) == 0 {
					codeBook[g*16+band] = 0
				} else {
					codeBook[g*16+band] = regularBooks[r.IntN(len(regularBooks))]
				}
			}
		}

		sfBuf := make([]byte, 256)
		for i := range sfBuf {
			sfBuf[i] = byte(r.IntN(256))
		}

		spectrum := make([]int32, samplesPerFrame)
		for i := range spectrum {
			spectrum[i] = int32(r.IntN(2*maxQuantizedValue+1) - maxQuantizedValue)
		}

		in := nativeaac.DequantInput{
			SamplesPerFrame:     samplesPerFrame,
			SamplingRateIndex:   samplingRateIndex,
			SamplingRate:        samplingRate,
			GlobalGain:          uint8(r.IntN(256)),
			Flags:               0,
			WindowSequence:      2, // BLOCK_SHORT
			WindowGroups:        groups,
			WindowGroupLength:   wgl,
			ScaleFactorGrouping: sfg,
			MaxSfBands:          maxSfb,
			CodeBook:            codeBook,
		}

		cRes, cSpec := cDequant(sfBuf, uint32(len(sfBuf)*8), samplesPerFrame,
			samplingRateIndex, samplingRate, in.GlobalGain, in.Flags,
			in.WindowSequence, in.WindowGroups, wgl, sfg, maxSfb, codeBook, spectrum)

		nRes := nativeaac.RunDequant(in, sfBuf, uint32(len(sfBuf)), uint32(len(sfBuf)*8), spectrum)

		require.Equal(t, cRes.readSfErr, nRes.ReadSfErr, "trial=%d readSfErr", trial)
		require.Equal(t, cRes.invQuantErr, nRes.InvQuantErr, "trial=%d invQuantErr", trial)
		require.Equal(t, cRes.scaleFactor, nRes.ScaleFactor, "trial=%d scaleFactor", trial)
		require.Equal(t, cRes.sfbScale, nRes.SfbScale, "trial=%d sfbScale", trial)
		require.Equal(t, cRes.specScale, nRes.SpecScale, "trial=%d specScale", trial)
		require.Equal(t, cSpec, nRes.Spectrum, "trial=%d spectrum", trial)
	}
}
