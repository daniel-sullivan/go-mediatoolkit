// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encodee2e

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// buildPCM builds a deterministic multi-tone interleaved int16 signal so the
// encoder exercises real M/S, sectioning and TNS rather than a trivial tone.
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

// nativeAUs runs the pure-Go nativeaac encoder over the same per-frame PCM and
// returns every emitted access unit. EncodeOneFrame consumes one interleaved
// frame (channels*frameLen int16) and returns one raw AU.
func nativeAUs(t *testing.T, sampleRate, channels, bitrate, frameLen int, pcm []int16) [][]byte {
	t.Helper()
	enc, encErr := nativeaac.NewEncoder(sampleRate, channels, bitrate)
	require.Equal(t, nativeaac.AacEncOK, encErr, "nativeaac.NewEncoder failed")
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

// TestEncodeE2EParity is the end-to-end AAC-LC CBR encode parity gate. It feeds
// identical int16 PCM frames and an identical AAC-LC CBR config (sample rate,
// channels, bitrate, AOT=2) to BOTH the genuine vendored fdk encoder
// (aacEncEncode, TRANSMUX 0) and the pure-Go nativeaac encoder, and asserts the
// emitted access units are BYTE-IDENTICAL access-unit-for-access-unit.
//
// There is NO frame offset between the two: the FDK lib (with this CBR/raw
// config) emits exactly one AU per fed input frame with no priming delay, and
// the native EncodeOneFrame likewise emits one AU per frame, so AU i of one
// stream is asserted directly against AU i of the other. fdk-aac encode is
// fixed-point, so a fixed CBR config reproduces the bitstream bit-for-bit — the
// comparison is EXACT (require.Equal on the raw bytes), never a tolerance. Any
// divergence is a real encode bug and must be root-caused, not loosened.
func TestEncodeE2EParity(t *testing.T) {
	cases := []struct {
		name     string
		channels int
		rate     int
		bitrate  int
	}{
		{"mono-44100-128k", 1, 44100, 128000},
		{"stereo-44100-128k", 2, 44100, 128000},
		{"stereo-48000-128k", 2, 48000, 128000},
		{"mono-48000-96k", 1, 48000, 96000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				frameLen = 1024
				nFrames  = 12
			)
			pcm := buildPCM(nFrames, frameLen, tc.channels, tc.rate)

			ref, asc, ok := cEncode(tc.rate, tc.channels, tc.bitrate, frameLen, pcm)
			require.True(t, ok, "genuine fdk encode failed")
			require.NotEmpty(t, ref, "fdk encoder produced no access units")
			require.NotEmpty(t, asc, "fdk encoder produced no ASC")

			got := nativeAUs(t, tc.rate, tc.channels, tc.bitrate, frameLen, pcm)
			require.NotEmpty(t, got, "native encoder produced no access units")

			// Both streams emit one AU per input frame with no priming delay
			// (empirically verified: AU 0 of each is the same cold-state frame).
			require.Equalf(t, len(ref), len(got),
				"AU count mismatch: fdk emitted %d, native emitted %d", len(ref), len(got))

			// Assert byte-identity AU-for-AU. EXACT raw-byte equality; fdk-aac
			// CBR encode is fixed-point and hence bit-for-bit reproducible.
			for i := range ref {
				require.Equalf(t, ref[i], got[i],
					"access unit %d not byte-identical (fdk len=%d native len=%d): "+
						"the fixed-point CBR bitstream must match exactly",
					i, len(ref[i]), len(got[i]))
			}
		})
	}
}
