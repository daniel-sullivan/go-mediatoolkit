// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package sbrdece2e

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"

	"github.com/stretchr/testify/require"
)

// TestSbrDecodeInt32Parity is the rigorous HE-AAC v1 SBR decode parity gate. It
// drives BOTH the genuine vendored fdk SBR decoder (sbrDecoder_Open / InitElement
// / Parse / Apply, via sbr_direct_bridge.cpp) and the pure-Go heaac SBR pipeline
// over the IDENTICAL per-frame AAC-LC core int32 input + sbr_extension_data bit
// location, and asserts the int32 SBR output is EXACTLY equal frame-for-frame
// (pre-narrowing). fdk-aac SBR is fixed-point, so the decode is reproducible
// bit-for-bit.
//
// Feeding both decoders the same core input + payload removes the AAC-LC core
// output delay and the int16 narrowing from the comparison, isolating the SBR
// decode math itself at int32 resolution.
func TestSbrDecodeInt32Parity(t *testing.T) {
	cases := []struct {
		name     string
		channels int
		outRate  int // encoder AACENC_SAMPLERATE == SBR output rate; core == half
	}{
		{"mono-out44100", 1, 44100},
		{"mono-out48000", 1, 48000},
		{"mono-out32000", 1, 32000},
		{"stereo-out44100", 2, 44100},
		{"stereo-out48000", 2, 48000},
		{"stereo-out32000", 2, 32000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const coreFrameLen = 1024
			coreRate := tc.outRate / 2
			nf := 16

			pcm := make([]int16, nf*coreFrameLen*tc.channels)
			for n := 0; n < nf*coreFrameLen; n++ {
				t0 := float64(n) / float64(tc.outRate)
				s := 0.45*math.Sin(2*math.Pi*440*t0) +
					0.25*math.Sin(2*math.Pi*3000*t0) +
					0.18*math.Sin(2*math.Pi*8000*t0) +
					0.12*math.Sin(2*math.Pi*10500*t0)
				l := int16(s * 24000)
				for c := 0; c < tc.channels; c++ {
					v := l
					if c == 1 {
						v = int16((s*0.8 + 0.12*math.Sin(2*math.Pi*900*t0)) * 24000)
					}
					pcm[n*tc.channels+c] = v
				}
			}

			aus, asc, _, ok := cEncodeHEAAC(tc.outRate, tc.channels, 48000, coreFrameLen, pcm)
			require.True(t, ok, "fdk HE-AAC encode failed")
			require.NotEmpty(t, asc, "encoder produced no ASC")
			if len(aus) < nf {
				nf = len(aus)
			}
			require.Greater(t, nf, 3, "too few access units")

			dec, err := heaac.NewDecoder(coreFrameLen, coreRate, tc.channels, 0)
			require.NoError(t, err)
			require.Equal(t, 2*coreFrameLen, dec.FrameSamples())
			require.Equal(t, tc.outRate, dec.SampleRate())

			// Run the pure-Go SBR pipeline, capturing per-frame int32 output + the
			// core int32 input + payload bit location for the fdk oracle.
			var coreFlat []int32
			var auFlat []byte
			auLens := make([]int32, nf)
			startBits := make([]int32, nf)
			countBits := make([]int32, nf)
			crcFlags := make([]int32, nf)
			prevEls := make([]int32, nf)
			goOut := make([][]int32, nf)

			for f := 0; f < nf; f++ {
				so, ci, sb, cb, crc, pe, derr := dec.DecodeAccessUnitInt32(aus[f])
				require.NoErrorf(t, derr, "go SBR decode of AU %d failed", f)
				coreFlat = append(coreFlat, ci...)
				auFlat = append(auFlat, aus[f]...)
				auLens[f] = int32(len(aus[f]))
				startBits[f] = int32(sb)
				countBits[f] = int32(cb)
				crcFlags[f] = int32(crc)
				prevEls[f] = int32(pe)
				goOut[f] = so
			}

			cOut, ok := cSbrDirect(coreRate, tc.outRate, tc.channels, nf, coreFlat,
				auFlat, auLens, startBits, countBits, crcFlags, prevEls)
			require.True(t, ok, "fdk sbr_direct failed")

			fs := tc.channels * 2 * coreFrameLen
			for f := 0; f < nf; f++ {
				require.Equalf(t, cOut[f*fs:(f+1)*fs], goOut[f],
					"frame %d int32 SBR output mismatch (Go vs fdk sbrDecoder_Apply)", f)
			}
		})
	}
}
