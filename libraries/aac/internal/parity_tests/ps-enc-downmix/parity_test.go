// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psencdownmix

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"
	"go-mediatoolkit/libraries/aac/internal/nativeaac/sbr"
)

// genPSStereoPCM mirrors the ps-enc-e2e generator: a sum of a low tone (AAC core)
// and a high tone (SBR) per channel, with a phase + level offset on the right
// channel so the PS downmix has real inter-channel content to fold.
func genPSStereoPCM(sampleRate, frames, frameLen int) []int16 {
	n := frames * frameLen
	pcm := make([]int16, n*2)
	for i := 0; i < n; i++ {
		t := float64(i) / float64(sampleRate)
		lo := math.Sin(2 * math.Pi * 440.0 * t)
		hi := 0.4 * math.Sin(2*math.Pi*9000.0*t)
		l := 0.5 * (lo + hi)
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

// psTuning returns the per-bitrate PS tuning (psTuningTable, sbrenc_rom.cpp:899-908),
// matching heaac.psTuningFor so this oracle drives the exact downmix config the
// ps-enc-e2e content frames exercise.
func psTuning(bitrate int) (nStereoBands, maxEnvelopes int, iidThresh int32) {
	switch {
	case bitrate < 22000:
		return 10, 1, nativeaac.Fl2fxconstDBL(3.0 / 4.0)
	case bitrate < 28000:
		return 20, 1, nativeaac.Fl2fxconstDBL(2.0 / 4.0)
	case bitrate < 36000:
		return 20, 2, nativeaac.Fl2fxconstDBL(1.5 / 4.0)
	default:
		return 20, 4, nativeaac.Fl2fxconstDBL(1.1 / 4.0)
	}
}

// TestPSDownmixParity is the stateful HE-AAC v2 PS-downmix parity gate: the
// genuine fdk FDKsbrEnc_PSEnc_ParametricStereoProcessing -> DownmixPSQmfData and
// the pure-Go sbr.PSEncParametricStereoProcessing, fed the same planar stereo
// int16 input across multiple frames over persistent QMF + hybrid + PS state,
// must produce the EXACT same downmixed mono QMF (real/imag per slot) and the
// same per-frame downmix qmfScale. Fixed-point => no tolerance.
func TestPSDownmixParity(t *testing.T) {
	const (
		noQmfSlots = 32 // dual-rate HE-AAC v2: 2048-sample SBR frame >> 6
		noQmfBands = 64
		nFrames    = 16
	)

	cases := []struct {
		name       string
		sampleRate int
		bitrate    int
	}{
		{"out44100-32k", 44100, 32000},
		{"out48000-32k", 48000, 32000},
		{"out32000-24k", 32000, 24000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nb, me, iid := psTuning(tc.bitrate)

			// The PS downmix consumes the INPUT-rate stereo signal at the core SBR
			// QMF grid: noQmfSlots*noQmfBands input samples per channel per frame.
			frameLen := noQmfSlots * noQmfBands
			pcm := genPSStereoPCM(tc.sampleRate, nFrames, frameLen)

			cReal, cImag, cDown, cScale, ok := cPSDownmix(pcm, nFrames, noQmfSlots, noQmfBands, nb, me, iid)
			require.True(t, ok, "fdk PS downmix failed")

			nReal, nImag, nDown, nScale := sbr.RunPSDownmixDriver(pcm, nFrames, noQmfSlots, noQmfBands, nb, me, iid)
			require.Len(t, nReal, nFrames)

			for f := 0; f < nFrames; f++ {
				require.Equalf(t, cScale[f], nScale[f], "frame %d: downmix qmfScale mismatch", f)
				require.Equalf(t, cReal[f], nReal[f], "frame %d: downmix mono QMF real mismatch", f)
				require.Equalf(t, cImag[f], nImag[f], "frame %d: downmix mono QMF imag mismatch", f)
				require.Equalf(t, cDown[f], nDown[f], "frame %d: downsampled MONO core signal mismatch", f)
			}
		})
	}
}
