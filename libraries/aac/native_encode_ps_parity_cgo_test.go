// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package aac

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"
)

// genPSStereoPCMFloat builds the same deterministic interleaved STEREO test signal
// as the ps-enc-e2e parity slice (a 440 Hz core tone + a high SBR tone, with a
// phase/level offset on the right channel so the PS tool has real inter-channel
// intensity/coherence), but quantised to [-1,1] float64 — the format the public
// [Encoder.Encode] consumes. It mirrors the int16 quantisation the engine applies,
// so the float input round-trips to the exact same int16 the heaac.NewPSEncoder
// oracle is driven with directly.
func genPSStereoPCMFloat(sampleRate, frames, frameLen int) []float64 {
	n := frames * frameLen
	pcm := make([]float64, n*2)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		lo := math.Sin(2 * math.Pi * 440.0 * t)
		hi := 0.4 * math.Sin(2*math.Pi*9000.0*t)
		l := 0.5 * (lo + hi)
		loR := math.Sin(2*math.Pi*440.0*t + 0.6)
		hiR := 0.3 * math.Sin(2*math.Pi*7000.0*t+0.4)
		r := 0.4 * (loR + hiR)
		// Build the int16 sample the oracle uses (l*30000 clamped), then express it
		// as the float the public encoder quantises back to the SAME int16
		// (int16(f*32768)). float(s)/32768 round-trips exactly for any int16 s.
		vl := clampI16(l * 30000.0)
		vr := clampI16(r * 30000.0)
		pcm[i*2+0] = float64(vl) / 32768.0
		pcm[i*2+1] = float64(vr) / 32768.0
	}
	return pcm
}

func clampI16(v float64) int16 {
	if v > 32767 {
		v = 32767
	} else if v < -32768 {
		v = -32768
	}
	return int16(v)
}

// TestNativeEncodePSPublicRouteByteIdentical proves the PUBLIC encoder route wires
// AOT-29 (parametric stereo) through to the byte-exact heaac PS encoder. It drives
// the public NewNativeEncoder(AOTPS) and the internal heaac.NewPSEncoder over the
// SAME stereo signal and asserts the AOT-29 AudioSpecificConfig and EVERY produced
// access unit are byte-identical (no tolerance). Because the ps-enc-e2e parity
// slice already pins heaac.NewPSEncoder byte-identical to the genuine fdk encoder,
// this transitively proves the public route is byte-identical to fdk — i.e. the
// reachability seam (native_stub.go AOT-29 branch + nativePsEncoder wrapper +
// newNativePsEncodeEngine) carries no divergence of its own.
func TestNativeEncodePSPublicRouteByteIdentical(t *testing.T) {
	const (
		frameLen = 2048 // HE-AAC v2 output frame (2*1024 core)
		frames   = 16
	)

	cases := []struct {
		name       string
		sampleRate int
		bitrate    int
	}{
		{"out44100-32k", 44100, 32000},
		{"out48000-32k", 48000, 32000},
		{"out32000-24k", 32000, 24000},
		{"out44100-24k", 44100, 24000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pcm := genPSStereoPCMFloat(tc.sampleRate, frames, frameLen)

			// Oracle: the internal heaac PS encoder driven directly with int16.
			oracle, err := heaac.NewPSEncoder(tc.sampleRate, tc.bitrate)
			require.NoError(t, err)
			require.Equal(t, frameLen, oracle.FrameSamples())

			// Public route: NewNativeEncoder(AOTPS) over the float PCM.
			enc, err := NewNativeEncoder(tc.sampleRate, 2,
				WithObjectType(AOTPS), WithBitrate(tc.bitrate))
			require.NoError(t, err)
			require.Equal(t, tc.sampleRate, enc.SampleRate())
			require.Equal(t, 2, enc.Channels())

			pubASC := enc.Config()
			require.Equal(t, AOTPS, pubASC.ObjectType)
			require.Equal(t, frameLen, pubASC.FrameSamples)
			// The public ASC Raw must be exactly the engine's AOT-29 ASC.
			require.Equal(t, oracle.ASC(), pubASC.Raw, "AOT-29 ASC mismatch")

			per := frameLen * 2 // stereo
			i16 := make([]int16, per)
			for f := 0; f < frames; f++ {
				frame := pcm[f*per : (f+1)*per]

				// Oracle consumes the exact int16 the public path quantises to.
				for j := 0; j < per; j++ {
					i16[j] = clampI16(frame[j] * 32768.0)
				}
				want, oerr := oracle.EncodeAccessUnit(i16)
				require.NoError(t, oerr, "oracle frame %d", f)

				got, perr := enc.Encode(frame)
				require.NoError(t, perr, "public frame %d", f)

				require.Equalf(t, want, got, "access unit %d byte mismatch", f)
			}
		})
	}
}

// TestNativeEncodePSRequiresStereo asserts the public AOT-29 route rejects a
// non-stereo channel count with the clear ErrPSRequiresStereo sentinel (parametric
// stereo encodes a stereo input down to a mono core).
func TestNativeEncodePSRequiresStereo(t *testing.T) {
	for _, ch := range []int{1, 3} {
		_, err := NewNativeEncoder(44100, ch, WithObjectType(AOTPS), WithBitrate(32000))
		require.ErrorIs(t, err, ErrPSRequiresStereo)
	}
}
