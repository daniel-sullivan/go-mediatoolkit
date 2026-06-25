// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encvbre2e

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// buildPCM builds a deterministic multi-tone interleaved int16 signal so the
// encoder exercises real M/S, sectioning, TNS and the VBR threshold reduction
// rather than a trivial tone.
func buildPCM(nFrames, frameLen, channels, rate int) []int16 {
	pcm := make([]int16, nFrames*frameLen*channels)
	for n := 0; n < nFrames*frameLen; n++ {
		t0 := float64(n) / float64(rate)
		s := 0.5*math.Sin(2*math.Pi*440*t0) +
			0.25*math.Sin(2*math.Pi*1500*t0) +
			0.15*math.Sin(2*math.Pi*5000*t0)
		l := int16(s * 26000)
		for c := 0; c < channels; c++ {
			v := l
			if c == 1 {
				v = int16((s*0.8 + 0.1*math.Sin(2*math.Pi*880*t0)) * 26000)
			}
			pcm[n*channels+c] = v
		}
	}
	return pcm
}

// nativeVBRAUs runs the pure-Go nativeaac VBR encoder over the same per-frame
// PCM and returns every emitted access unit. EncodeOneFrame consumes one
// interleaved frame (channels*frameLen int16) and returns one raw AU.
func nativeVBRAUs(t *testing.T, sampleRate, channels, bitrateMode, frameLen int, pcm []int16) [][]byte {
	t.Helper()
	enc, encErr := nativeaac.NewEncoderVBR(sampleRate, channels, nativeaac.AacencBitrateMode(bitrateMode))
	require.Equal(t, nativeaac.AacEncOK, encErr, "nativeaac.NewEncoderVBR failed")
	require.Equal(t, frameLen, enc.FrameLength(), "unexpected native frame length")

	per := frameLen * channels
	framesIn := len(pcm) / per
	aus := make([][]byte, 0, framesIn)
	for f := 0; f < framesIn; f++ {
		au, e := enc.EncodeOneFrame(pcm[f*per : (f+1)*per])
		require.Equalf(t, nativeaac.AacEncOK, e, "native EncodeOneFrame frame %d failed", f)
		aus = append(aus, au)
	}
	return aus
}

// TestEncodeVBRE2EParity is the end-to-end AAC-LC VBR encode parity gate. It
// feeds identical int16 PCM frames and an identical AAC-LC VBR config (sample
// rate, channels, AOT=2, a fixed AACENC_BITRATEMODE) to BOTH the genuine
// vendored fdk encoder (aacEncEncode, TRANSMUX 0) and the pure-Go nativeaac VBR
// encoder, and asserts the emitted access units are BYTE-IDENTICAL
// access-unit-for-access-unit.
//
// As with the CBR e2e slice there is no frame offset: the FDK lib (VBR/raw
// config) emits exactly one AU per fed input frame with no priming delay, and
// the native EncodeOneFrame likewise emits one AU per frame, so AU i is asserted
// directly against AU i. fdk-aac encode is fixed-point, so a fixed VBR mode
// reproduces the bitstream bit-for-bit — the comparison is EXACT
// (require.Equal on the raw bytes), never a tolerance. Any divergence is a real
// VBR encode bug and must be root-caused, not loosened.
//
// This drives the VBR else-branch of FDKaacEnc_AdjustThresholds
// (FDKaacEnc_AdaptThresholdsVBR -> FDKaacEnc_reduceThresholdsVBR) on both sides.
func TestEncodeVBRE2EParity(t *testing.T) {
	cases := []struct {
		name        string
		channels    int
		rate        int
		bitrateMode int // AACENC_BITRATEMODE 1..5
	}{
		{"mono-44100-vbr1", 1, 44100, 1},
		{"mono-44100-vbr3", 1, 44100, 3},
		{"mono-44100-vbr5", 1, 44100, 5},
		{"stereo-44100-vbr2", 2, 44100, 2},
		{"stereo-44100-vbr3", 2, 44100, 3},
		{"stereo-48000-vbr4", 2, 48000, 4},
		{"stereo-48000-vbr5", 2, 48000, 5},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				frameLen = 1024
				nFrames  = 12
			)
			pcm := buildPCM(nFrames, frameLen, tc.channels, tc.rate)

			ref, asc, ok := cEncodeVBR(tc.rate, tc.channels, tc.bitrateMode, frameLen, pcm)
			require.True(t, ok, "genuine fdk VBR encode failed")
			require.NotEmpty(t, ref, "fdk encoder produced no access units")
			require.NotEmpty(t, asc, "fdk encoder produced no ASC")

			got := nativeVBRAUs(t, tc.rate, tc.channels, tc.bitrateMode, frameLen, pcm)
			require.NotEmpty(t, got, "native encoder produced no access units")

			require.Equalf(t, len(ref), len(got),
				"AU count mismatch: fdk emitted %d, native emitted %d", len(ref), len(got))

			// Assert byte-identity AU-for-AU. EXACT raw-byte equality; fdk-aac
			// VBR encode is fixed-point and hence bit-for-bit reproducible.
			for i := range ref {
				require.Equalf(t, ref[i], got[i],
					"VBR access unit %d not byte-identical (fdk len=%d native len=%d): "+
						"the fixed-point VBR bitstream must match exactly",
					i, len(ref[i]), len(got[i]))
			}
		})
	}
}
