// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package decodee2e

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// TestDecodeE2EParity is the end-to-end AAC-LC decode parity gate. It encodes a
// real signal to AAC-LC raw access units with the vendored FDK ENCODER, decodes
// the AU sequence with BOTH the FDK cgo DECODER (PCM limiter disabled) and the
// pure-Go internal/nativeaac decoder from fresh state, and asserts the two
// int16 PCM streams are EXACTLY equal frame-for-frame. fdk-aac is fixed-point,
// so decode is reproducible bit-for-bit (no tolerance, no ULP).
func TestDecodeE2EParity(t *testing.T) {
	cases := []struct {
		name     string
		channels int
		rate     int
	}{
		{"mono-44100", 1, 44100},
		{"stereo-44100", 2, 44100},
		{"stereo-48000", 2, 48000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				frameLen = 1024
				bitrate  = 128000
				nFrames  = 8
			)

			// Build a deterministic multi-tone signal so the encoder exercises
			// real M/S, sections, and TNS — not a trivial pure tone.
			pcm := make([]int16, nFrames*frameLen*tc.channels)
			for n := 0; n < nFrames*frameLen; n++ {
				t0 := float64(n) / float64(tc.rate)
				s := 0.5*math.Sin(2*math.Pi*440*t0) +
					0.25*math.Sin(2*math.Pi*1500*t0) +
					0.15*math.Sin(2*math.Pi*5000*t0)
				l := int16(s * 26000)
				for c := 0; c < tc.channels; c++ {
					v := l
					if c == 1 {
						// Decorrelate the right channel a little for stereo tools.
						v = int16((s*0.8 + 0.1*math.Sin(2*math.Pi*880*t0)) * 26000)
					}
					pcm[n*tc.channels+c] = v
				}
			}

			aus, asc, ok := cEncode(tc.rate, tc.channels, bitrate, frameLen, pcm)
			require.True(t, ok, "fdk encode failed")
			require.NotEmpty(t, aus, "encoder produced no access units")
			require.NotEmpty(t, asc, "encoder produced no ASC")

			// Reference: FDK cgo decoder (limiter disabled), all AUs from fresh.
			ref, sp, ch, ok := cDecode(asc, aus, tc.channels, frameLen)
			require.True(t, ok, "fdk decode failed")
			require.Equal(t, frameLen, sp, "unexpected frame size")
			require.Equal(t, tc.channels, ch, "unexpected channel count")

			// Under test: pure-Go nativeaac decoder, same AUs from fresh state.
			dec, err := nativeaac.NewDecoder(frameLen, uint32(tc.rate), tc.channels)
			require.NoError(t, err)

			// The FDK reference decoder emits one extra leading frame (its
			// configured one-frame output delay): aacDecoder_DecodeFrame
			// returns a priming frame first, so ref[k+1] is the decode of AU[k].
			// The pure-Go decoder has no such buffering — DecodeAccessUnit(AU[k])
			// returns decode(AU[k]) directly — so native frame i is asserted
			// against ref frame i+1. (The whole spectral->time chain, including
			// the IMDCT 50% overlap-add carry, is identical between the two; the
			// only difference is the reference's leading priming frame.)
			const refDelay = 1
			compared := 0
			for i, au := range aus {
				out := make([]int16, frameLen*tc.channels)
				n, err := dec.DecodeAccessUnit(au, out)
				require.NoErrorf(t, err, "native decode of AU %d failed", i)
				require.Equal(t, frameLen, n)

				// FDK emits one frame per fed AU, so ref holds len(aus) real
				// frames (indices 0..len(aus)-1); ref[len(aus)..] is unwritten
				// scratch. With the 1-frame delay, decode(AU[i]) == ref[i+1] is
				// only available for i+1 <= len(aus)-1.
				j := i + refDelay
				if j >= len(aus) {
					continue // decode(AU[i]) is still inside FDK's delay buffer
				}
				base := j * frameLen * tc.channels
				want := ref[base : base+frameLen*tc.channels]

				// EXACT integer equality — no tolerance, no ULP. fdk-aac decode
				// is fixed-point, hence bit-for-bit reproducible.
				require.Equalf(t, want, out, "frame %d (vs ref frame %d) int16 PCM mismatch", i, j)
				compared++
			}
			require.Greater(t, compared, 0, "no frames compared")
		})
	}
}
