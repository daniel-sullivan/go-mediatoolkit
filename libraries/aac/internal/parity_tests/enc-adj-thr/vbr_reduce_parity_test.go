// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package encadjthr

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// buildVBRPCM builds a deterministic multi-tone interleaved int16 signal that
// exercises block switching (so the captured reduceThresholdsVBR invocations
// span long / start / short / stop windows).
func buildVBRPCM(nFrames, frameLen, channels, rate int) []int16 {
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

// TestReduceThresholdsVBRParity asserts reduceThresholdsVBR (the VBR
// threshold-reduction inside FDKaacEnc_AdjustThresholds' else-branch) is
// byte-exact vs the genuine static FDKaacEnc_reduceThresholdsVBR. Rather than
// synthesising abstract inputs, it drives the pure-Go VBR encoder over real PCM
// with per-frame capture enabled, then replays EVERY captured reduceThresholdsVBR
// invocation (one per AAC frame, across long / start / short / stop windows and
// the cross-frame chaosMeasureOld smoothing) through the real fdk static and
// requires the reduced thresholds, avoid-hole flags AND the updated
// chaosMeasureOld to match exactly. This exercises both the long-block and
// short-block reduction paths and the calcChaosMeasure dependency.
//
// Mono only: the flat-capture replay harness reproduces the single-channel
// grouped-SFB layout exactly. The end-to-end byte-identical VBR gate
// (parity_tests/enc-vbr-e2e) covers the multi-channel grouped short-block layout
// through the real encoder, where the per-group memory addressing is authentic;
// reproducing that grouping faithfully in this flat-array replay for stereo
// short blocks is unnecessary given that authoritative e2e coverage.
func TestReduceThresholdsVBRParity(t *testing.T) {
	cases := []struct {
		name     string
		channels int
		rate     int
		mode     int
	}{
		{"mono-44100-vbr1", 1, 44100, 1},
		{"mono-44100-vbr3", 1, 44100, 3},
		{"mono-48000-vbr5", 1, 48000, 5},
	}

	const stride = 64 // MAX_GROUPED_SFB
	toI32 := func(s []int) []int32 {
		o := make([]int32, len(s))
		for i, v := range s {
			o[i] = int32(v)
		}
		return o
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const frameLen, nFrames = 1024, 12
			pcm := buildVBRPCM(nFrames, frameLen, tc.channels, tc.rate)

			nativeaac.VbrCaptureEnabled = true
			nativeaac.VbrCaptures = nil
			defer func() { nativeaac.VbrCaptureEnabled = false; nativeaac.VbrCaptures = nil }()

			enc, encErr := nativeaac.NewEncoderVBR(tc.rate, tc.channels, nativeaac.AacencBitrateMode(tc.mode))
			require.Equal(t, nativeaac.AacEncOK, encErr)
			per := frameLen * tc.channels
			for f := 0; f < nFrames; f++ {
				_, e := enc.EncodeOneFrame(pcm[f*per : (f+1)*per])
				require.Equal(t, nativeaac.AacEncOK, e)
			}
			require.NotEmpty(t, nativeaac.VbrCaptures, "no reduceThresholdsVBR captures recorded")

			for i, cap := range nativeaac.VbrCaptures {
				nThr, nAh, nCmo := nativeaac.ParityReduceThresholdsVBR(cap.NChannels, stride,
					cap.SfbWeightedEnergyLdData, cap.SfbThresholdLdData, cap.SfbMinSnrLdData,
					cap.SfbFormFactorLdData, cap.SfbEnergy, cap.SfbEnergyLdData, cap.AhFlagIn, cap.ThrExp,
					cap.SfbOffset, cap.SfbCnt, cap.SfbPerGroup, cap.MaxSfbPerGroup, cap.LastWindowSequence,
					cap.GroupLen, cap.VbrQualFactor, cap.ChaosMeasureOldIn)

				cThr, cAh, cCmo := cReduceThresholdsVBR(cap.NChannels, stride,
					cap.SfbWeightedEnergyLdData, cap.SfbThresholdLdData, cap.SfbMinSnrLdData,
					cap.SfbFormFactorLdData, cap.SfbEnergy, cap.SfbEnergyLdData, cap.AhFlagIn, cap.ThrExp,
					toI32(cap.SfbOffset), cap.SfbCnt, cap.SfbPerGroup, cap.MaxSfbPerGroup,
					cap.LastWindowSequence, toI32(cap.GroupLen), cap.VbrQualFactor, cap.ChaosMeasureOldIn)

				// Compare only the active SFB range per channel ([0, sfbCnt)).
				// reduceThresholdsVBR only writes within the grouped active SFBs, so
				// entries beyond sfbCnt are uninitialised scratch (not part of the
				// kernel's output contract) and would compare stale-vs-stale garbage.
				for ch := 0; ch < cap.NChannels; ch++ {
					base := ch * stride
					rng := cap.SfbCnt
					require.Equalf(t, cThr[base:base+rng], nThr[base:base+rng],
						"frame %d ch %d (lastWS=%d): reduced thresholds differ", i, ch, cap.LastWindowSequence)
					require.Equalf(t, cAh[base:base+rng], nAh[base:base+rng],
						"frame %d ch %d (lastWS=%d): avoid-hole flags differ", i, ch, cap.LastWindowSequence)
				}
				require.Equalf(t, cCmo, nCmo, "frame %d (lastWS=%d): chaosMeasureOld differs", i, cap.LastWindowSequence)
			}
		})
	}
}
