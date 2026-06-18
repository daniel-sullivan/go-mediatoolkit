// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package psymainmultiframe

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac"

	"github.com/stretchr/testify/require"
)

// buildPCM builds a deterministic multi-tone interleaved int16 signal so the
// encoder exercises real thresholds / pre-echo / rate-control carry (matches
// encode-e2e's signal so a divergence here lines up with the e2e failure).
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

// nativeStates drives the pure-Go nativeaac encoder over the same per-frame PCM
// and returns the carried EncoderStateDump snapshot after each frame.
func nativeStates(t *testing.T, sampleRate, channels, bitrate, frameLen int, pcm []int16) []nativeaac.EncoderStateDump {
	t.Helper()
	enc, encErr := nativeaac.NewEncoder(sampleRate, channels, bitrate)
	require.Equal(t, nativeaac.AacEncOK, encErr, "nativeaac.NewEncoder failed")

	per := frameLen * channels
	framesIn := len(pcm) / per
	out := make([]nativeaac.EncoderStateDump, 0, framesIn)
	for f := 0; f < framesIn; f++ {
		_, e := enc.EncodeOneFrame(pcm[f*per : (f+1)*per])
		require.Equalf(t, nativeaac.AacEncOK, e, "native EncodeOneFrame frame %d failed", f)
		out = append(out, enc.DumpState())
	}
	return out
}

// compareChan reports the FIRST field that diverges between the genuine fdk
// snapshot and the native snapshot for one channel of one frame. Returns ""
// when they match.
func compareChan(c chanState, n nativeaac.ChannelStateDump) string {
	for i := 0; i < 51; i++ {
		if c.SfbThresholdNm1[i] != n.SfbThresholdNm1[i] {
			return fmtField("sfbThresholdnm1["+itoa(i)+"]", c.SfbThresholdNm1[i], n.SfbThresholdNm1[i])
		}
	}
	if c.MdctScaleNm1 != n.MdctScaleNm1 {
		return fmtField("mdctScalenm1", int32(c.MdctScaleNm1), int32(n.MdctScaleNm1))
	}
	if c.CalcPreEcho != n.CalcPreEcho {
		return fmtField("calcPreEcho", int32(c.CalcPreEcho), int32(n.CalcPreEcho))
	}
	if c.LastWindowSequence != n.LastWindowSequence {
		return fmtField("lastWindowSequence", int32(c.LastWindowSequence), int32(n.LastWindowSequence))
	}
	if c.WindowShape != n.WindowShape {
		return fmtField("windowShape", int32(c.WindowShape), int32(n.WindowShape))
	}
	if c.LastWindowShape != n.LastWindowShape {
		return fmtField("lastWindowShape", int32(c.LastWindowShape), int32(n.LastWindowShape))
	}
	if c.NoOfGroups != n.NoOfGroups {
		return fmtField("noOfGroups", int32(c.NoOfGroups), int32(n.NoOfGroups))
	}
	if c.PeLast != n.PeLast {
		return fmtField("peLast", int32(c.PeLast), int32(n.PeLast))
	}
	if c.DynBitsLast != n.DynBitsLast {
		return fmtField("dynBitsLast", int32(c.DynBitsLast), int32(n.DynBitsLast))
	}
	if c.PeCorrectionFactorM != n.PeCorrectionFactorM {
		return fmtField("peCorrectionFactor_m", c.PeCorrectionFactorM, n.PeCorrectionFactorM)
	}
	if c.PeCorrectionFactorE != n.PeCorrectionFactorE {
		return fmtField("peCorrectionFactor_e", int32(c.PeCorrectionFactorE), int32(n.PeCorrectionFactorE))
	}
	if c.ChaosMeasureOld != n.ChaosMeasureOld {
		return fmtField("chaosMeasureOld", c.ChaosMeasureOld, n.ChaosMeasureOld)
	}
	if c.MdctScale != n.MdctScale {
		return fmtField("mdctScale (psyData)", int32(c.MdctScale), int32(n.MdctScale))
	}
	return ""
}

func fmtField(name string, fdk, nat int32) string {
	return name + ": fdk=" + itoa(int(fdk)) + " native=" + itoa(int(nat))
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestPsyMainMultiFrameStateParity drives the genuine fdk encoder and the
// pure-Go nativeaac encoder over the SAME >=3-frame real signal and compares
// the FULL carried inter-frame state after each frame. Frame 0 matches by
// construction (cold state); the test localizes the first carried field that
// diverges at frame >= 1. EXACT integer equality, no tolerance.
func TestPsyMainMultiFrameStateParity(t *testing.T) {
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
				nFrames  = 6
			)
			pcm := buildPCM(nFrames, frameLen, tc.channels, tc.rate)

			ref, ok := cEncodeStates(tc.rate, tc.channels, tc.bitrate, frameLen, pcm)
			require.True(t, ok, "genuine fdk encode failed")
			require.Len(t, ref, nFrames)

			got := nativeStates(t, tc.rate, tc.channels, tc.bitrate, frameLen, pcm)
			require.Len(t, got, nFrames)

			for f := 0; f < nFrames; f++ {
				if ref[f].BitResTot != got[f].BitResTot {
					t.Errorf("frame %d: bitResTot diverges: fdk=%d native=%d",
						f, ref[f].BitResTot, got[f].BitResTot)
				}
				// Element-level rate-control carry (the qcMain bit distribution /
				// adj_thr pe machinery): the PE, granted bits and granted PE that
				// drive the next frame's threshold adaptation and bitstream.
				if ref[f].Pe != got[f].Pe {
					t.Errorf("frame %d: peData.pe diverges: fdk=%d native=%d", f, ref[f].Pe, got[f].Pe)
				}
				if ref[f].GrantedDynBits != got[f].GrantedDynBits {
					t.Errorf("frame %d: grantedDynBits diverges: fdk=%d native=%d",
						f, ref[f].GrantedDynBits, got[f].GrantedDynBits)
				}
				if ref[f].GrantedPe != got[f].GrantedPe {
					t.Errorf("frame %d: grantedPe diverges: fdk=%d native=%d",
						f, ref[f].GrantedPe, got[f].GrantedPe)
				}
				for ch := 0; ch < tc.channels; ch++ {
					if d := compareChan(ref[f].Ch[ch], got[f].Channels[ch]); d != "" {
						t.Errorf("frame %d ch %d: first diverging carried state: %s", f, ch, d)
					}
				}
			}
		})
	}
}

// TestPsyMainMultiFramePsyOutParity asserts the per-SFB POST-psyMain outputs
// that feed peData.pe match the genuine fdk FDKaacEnc_psyMain EXACTLY, per
// frame: the ld-domain psyOut threshold/energy (sfbThresholdLdData /
// sfbEnergyLdData), the per-band M-S mask + frame-level msDigest, and the
// post-IntensityStereoProcessing intensity book (isBook). Over >= 4 frames
// including the STOP->LONG transition at frame 3, this localizes the FIRST
// per-SFB value that diverges — the stereo joint-stereo (intensity / M-S)
// decision that the carried-state pe field only reflects downstream.
//
// The linear psyData sfbEnergy/sfbThreshold scratch unions are deliberately NOT
// compared: they are reused by the qc tier after psyMain, so their post-frame
// value in the genuine handle is not a stable parity target (the ld-domain
// copies the rate-control actually consumes are).
func TestPsyMainMultiFramePsyOutParity(t *testing.T) {
	cases := []struct {
		name     string
		channels int
		rate     int
		bitrate  int
	}{
		{"stereo-44100-128k", 2, 44100, 128000},
		{"stereo-48000-128k", 2, 48000, 128000},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const (
				frameLen = 1024
				nFrames  = 6
			)
			pcm := buildPCM(nFrames, frameLen, tc.channels, tc.rate)

			ref, ok := cEncodeStates(tc.rate, tc.channels, tc.bitrate, frameLen, pcm)
			require.True(t, ok, "genuine fdk encode failed")

			got := nativeStates(t, tc.rate, tc.channels, tc.bitrate, frameLen, pcm)

			for f := 0; f < nFrames; f++ {
				if ref[f].MsDigest != got[f].MsDigest {
					t.Errorf("frame %d: msDigest diverges: fdk=%d native=%d", f, ref[f].MsDigest, got[f].MsDigest)
				}
				for i := 0; i < 60; i++ {
					if ref[f].MsMask[i] != int32(got[f].MsMask[i]) {
						t.Errorf("frame %d: msMask[%d] diverges: fdk=%d native=%d", f, i, ref[f].MsMask[i], got[f].MsMask[i])
						break
					}
				}
				for ch := 0; ch < tc.channels; ch++ {
					rp := &ref[f].PsyOut[ch]
					gp := &got[f].PsyOut[ch]
					if rp.MaxSfbPerGroup != gp.MaxSfbPerGroup {
						t.Errorf("frame %d ch %d: maxSfbPerGroup fdk=%d native=%d", f, ch, rp.MaxSfbPerGroup, gp.MaxSfbPerGroup)
					}
					cmp := func(name string, r, g *[60]int32) {
						for i := 0; i < 60; i++ {
							if r[i] != g[i] {
								t.Errorf("frame %d ch %d: %s[%d] diverges: fdk=%d native=%d", f, ch, name, i, r[i], g[i])
								return
							}
						}
					}
					cmp("sfbEnergyLdData", &rp.SfbEnergyLdData, &gp.SfbEnergyLdData)
					cmp("sfbThresholdLdData", &rp.SfbThresholdLdData, &gp.SfbThresholdLdData)
					for i := 0; i < 60; i++ {
						if rp.IsBook[i] != int32(gp.IsBook[i]) {
							t.Errorf("frame %d ch %d: isBook[%d] diverges: fdk=%d native=%d", f, ch, i, rp.IsBook[i], gp.IsBook[i])
							break
						}
					}
				}
			}
		})
	}
}
