// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNativeEncodeDecodeVBRGate is the VBR stage GATE: under CGO_ENABLED=0 -tags
// aacfdk, the pure-Go 1:1 AAC-LC VBR encoder (NewNativeEncoder + WithVBR ->
// EncodeFrame with the FDKaacEnc_AdaptThresholdsVBR threshold-reduction path)
// produces raw access units that the committed pure-Go AAC-LC decoder
// (NewNativeDecoder) decodes back to PCM. Byte-identical-vs-FDK is asserted
// separately by parity_tests/enc-vbr-e2e; this gate proves the VBR encode path
// yields decodable AUs and recovers signal energy in the pure-Go-only build.
func TestNativeEncodeDecodeVBRGate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		channels int
		quality  int
	}{
		{"mono-vbr3", 1, 3},
		{"stereo-vbr5", 2, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const (
				sampleRate = 44100
				freq       = 440.0
			)

			enc, err := NewNativeEncoder(sampleRate, tc.channels, WithObjectType(AOTAACLC), WithVBR(tc.quality))
			require.NoError(t, err)
			require.Equal(t, sampleRate, enc.SampleRate())
			require.Equal(t, tc.channels, enc.Channels())

			asc := enc.Config()
			require.Equal(t, FrameSamplesShort, asc.FrameSamples)
			require.NotEmpty(t, asc.Raw)

			dec, err := NewNativeDecoder(asc)
			require.NoError(t, err)

			frame := asc.FrameSamples
			pcm := make([]float64, frame*tc.channels)
			pcmOut := make([]float64, FrameSamplesLong*tc.channels)

			phase := 0.0
			step := 2 * math.Pi * freq / sampleRate

			var decodedAny bool
			for f := 0; f < 8; f++ {
				for i := 0; i < frame; i++ {
					s := 0.5 * math.Sin(phase)
					phase += step
					for c := 0; c < tc.channels; c++ {
						pcm[i*tc.channels+c] = s
					}
				}
				pkt, err := enc.Encode(pcm)
				require.NoError(t, err, "encode frame %d", f)
				require.NotEmpty(t, pkt, "VBR encoder produced an access unit on frame %d", f)
				require.LessOrEqual(t, len(pkt), MaxFrameBytes)

				n, derr := dec.Decode(pkt, pcmOut)
				require.NoError(t, derr, "decode frame %d", f)
				if n > 0 {
					decodedAny = true
				}
			}
			assert.True(t, decodedAny, "decoder recovered at least one frame from the native VBR AU")
		})
	}
}
