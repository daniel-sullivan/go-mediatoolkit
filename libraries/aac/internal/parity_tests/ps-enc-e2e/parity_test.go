// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psence2e

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"
)

// genPSStereoPCM builds a deterministic interleaved STEREO int16 test signal: a
// sum of a low tone (the AAC core carries) and a high tone (SBR reconstructs) per
// channel, with a phase + level offset on the right channel so the PS tool has
// real inter-channel intensity/coherence to encode.
func genPSStereoPCM(sampleRate, frames, frameLen int) []int16 {
	n := frames * frameLen
	pcm := make([]int16, n*2)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		lo := math.Sin(2 * math.Pi * 440.0 * t)
		hi := 0.4 * math.Sin(2*math.Pi*9000.0*t)
		l := 0.5 * (lo + hi)
		// right channel: phase-shifted, level-scaled.
		loR := math.Sin(2*math.Pi*440.0*t + 0.6)
		hiR := 0.3 * math.Sin(2*math.Pi*7000.0*t+0.4)
		r := 0.4 * (loR + hiR)
		vl := l * 30000.0
		vr := r * 30000.0
		if vl > 32767 {
			vl = 32767
		} else if vl < -32768 {
			vl = -32768
		}
		if vr > 32767 {
			vr = 32767
		} else if vr < -32768 {
			vr = -32768
		}
		pcm[i*2+0] = int16(vl)
		pcm[i*2+1] = int16(vr)
	}
	return pcm
}

// nativeEncodePSHEAAC drives the pure-Go heaac PS encoder over the same per-frame
// STEREO PCM the fdk oracle consumes and returns every produced access unit + ASC.
func nativeEncodePSHEAAC(t *testing.T, sampleRate, bitrate, frameLen int, pcm []int16) (aus [][]byte, asc []byte) {
	t.Helper()
	enc, err := heaac.NewPSEncoder(sampleRate, bitrate)
	require.NoError(t, err)
	require.Equal(t, frameLen, enc.FrameSamples())

	per := frameLen * 2 // stereo input
	frames := len(pcm) / per
	for f := 0; f < frames; f++ {
		au, err := enc.EncodeAccessUnit(pcm[f*per : (f+1)*per])
		require.NoError(t, err)
		if len(au) > 0 {
			aus = append(aus, au)
		}
	}
	return aus, enc.ASC()
}

// TestPSEncodeE2EByteIdentical is the HE-AAC v2 (AOT_PS) ENCODE byte-identical gate:
// a real STEREO signal encoded by BOTH the genuine fdk encoder (AOT_PS, mono core +
// SBR + ps_data) and the pure-Go heaac PS encoder must produce the same AOT-29 ASC
// and the same access-unit byte stream across ALL frames.
//
// FULL byte-identical: the AOT-29 ASC and EVERY access unit (warmup delay-line
// frames AND content frames) are asserted EQUAL with no tolerance. This proves the
// complete HE-AAC v2 encode chain: the framing + AOT-29 hierarchical signaling, the
// bitstream-delay (nBitstrDelay == 1) + metadata audio-delay (nAudioDataDelay ==
// 1057, the >1024 two-iteration CompensateAudioDelay) choreography, the PS parameter
// extractor + ps_data writer, AND the DownmixPSQmfData fixed-point downmix — the
// per-band stereo scale factor, the hybrid synthesis, and the half-rate QMF
// synthesis that produces the downsampled MONO AAC-LC core. The final downmix
// divergence (the 32-band PS synthesis QMF used L==64 DCT-IV twiddles/window instead
// of the L==32 ones — dct.cpp:138-142, fixed in sbr.inverseModulationHQ) is closed;
// the stateful downmix is pinned independently by the ps-enc-downmix parity slice.
func TestPSEncodeE2EByteIdentical(t *testing.T) {
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
			pcm := genPSStereoPCM(tc.sampleRate, frames, frameLen)

			cAUs, cASC, ok := cEncodePSHEAAC(tc.sampleRate, tc.bitrate, frameLen, pcm)
			require.True(t, ok, "fdk HE-AAC v2 (AOT_PS) encode failed")
			require.NotEmpty(t, cAUs)

			nAUs, nASC := nativeEncodePSHEAAC(t, tc.sampleRate, tc.bitrate, frameLen, pcm)

			require.Equal(t, cASC, nASC, "AOT-29 AudioSpecificConfig mismatch")
			require.Equal(t, len(cAUs), len(nAUs), "access-unit count mismatch")
			// FULL byte-identical: every access unit (warmup + content) must match.
			for i := range cAUs {
				require.Equalf(t, cAUs[i], nAUs[i], "access unit %d byte mismatch", i)
			}
		})
	}
}
