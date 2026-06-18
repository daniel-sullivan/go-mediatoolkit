// SPDX-License-Identifier: FDK-AAC
//go:build cgo && aacfdk

package aac

import (
	"math"
	"testing"

	"go-mediatoolkit/libraries/aac/internal/nativeaac/heaac"

	"github.com/stretchr/testify/require"
)

// TestNativePSDecodePublicSurface drives the HE-AAC v2 (parametric stereo) DECODE
// path through the public package surface end-to-end and proves the Part B routing
// upmixes an AOT-29 stream to STEREO via the pure-Go (NATIVE) path.
//
// It encodes a real STEREO signal with the vendored FDK encoder configured for
// AOT_PS (29) — which downmixes to a MONO AAC-LC core and codes the spatial image
// as ps_data in the SBR extension — then decodes the access-unit sequence through
// the public pure-Go Decoder (NewNativeDecoder). Before the Part B wiring,
// NewNativeDecoder of an AOT-29 ASC fell back to a mono SBR engine and never
// produced the second channel; now it routes to the heaac PS engine
// (heaac.NewPSDecoder) and reports a 2-channel stereo output at the SBR-doubled
// rate.
//
// CORRECTNESS is asserted by an EXACT cross-check: the public Decoder output must
// equal — to the LSB, after the int16->float64 normalisation — the int16 STEREO
// PCM the underlying heaac.NewPSDecoder engine produces directly. That engine's
// PS upmix is itself proven EXACT-integer bit-exact vs the genuine fdk SBR+PS by
// the internal ps-dec-e2e slice (TestPSDecodeInt32BitExact, require.Equal, no
// tolerance). So this test closes the public-surface loop: routing + int16->float64
// normalisation faithfully deliver the bit-exact PS stereo, no tolerance.
func TestNativePSDecodePublicSurface(t *testing.T) {
	const (
		coreFrameLen = 1024
		outRate      = 44100
		bitrate      = 32000
		frames       = 32
	)

	channels := 2
	coreRate := outRate / 2

	enc, err := NewEncoder(outRate, channels, WithObjectType(AOTPS), WithBitrate(bitrate))
	require.NoError(t, err)
	asc := enc.Config()
	frame := asc.FrameSamples // == 2048 (SBR-doubled long frame)
	require.Equal(t, FrameSamplesLong, frame)

	// The genuine FDK ASC must signal AOT_PS (29) and resolve to a STEREO output.
	psASC := AudioSpecificConfig{
		ObjectType: AOTPS,
		Raw:        asc.Raw,
		SampleRate: outRate,
		Channels:   channels,
	}
	sr, ch := psASC.Output()
	require.Equal(t, 2, ch, "AOT_PS ASC must report a stereo output")
	require.Equal(t, outRate, sr, "AOT_PS ASC must report the SBR-doubled output rate")

	// Encode a real stereo chirp (phase-shifted, level-scaled right channel so the
	// PS tool has genuine inter-channel intensity/coherence to code).
	var aus [][]byte
	for f := 0; f < frames; f++ {
		pcm := make([]float64, frame*channels)
		for n := 0; n < frame; n++ {
			t0 := float64(f*frame+n) / float64(outRate)
			f0 := 200.0 + 30.0*t0
			l := 0.5*math.Sin(2*math.Pi*f0*t0*(1+0.3*t0)) + 0.15*math.Sin(2*math.Pi*2500*t0)
			r := 0.4*math.Sin(2*math.Pi*f0*t0*(1+0.3*t0)+0.6) + 0.2*math.Sin(2*math.Pi*1700*t0)
			pcm[n*channels] = l
			pcm[n*channels+1] = r
		}
		au, err := enc.Encode(pcm)
		require.NoError(t, err)
		if len(au) > 0 {
			aus = append(aus, au)
		}
	}
	require.Greater(t, len(aus), 8, "too few AOT_PS access units")

	// Public native PS decode: NewNativeDecoder must route AOT-29 to the heaac PS
	// engine and produce a 2-channel STEREO output at the doubled rate.
	ndec, err := NewNativeDecoder(psASC)
	require.NoError(t, err)
	require.Equal(t, 2, ndec.Channels(), "native PS decoder must output stereo")
	require.Equal(t, outRate, ndec.SampleRate())
	require.Equal(t, AOTPS, ndec.Config().ObjectType)

	// Reference: the underlying heaac PS engine, driven directly (int16 stereo).
	refEng, err := heaac.NewPSDecoder(coreFrameLen, coreRate, 0)
	require.NoError(t, err)
	require.Equal(t, 2, refEng.Channels())
	require.Equal(t, outRate, refEng.SampleRate())

	fs := frame * 2 // stereo interleaved
	var energy float64
	for f := range aus {
		// Public float64 path.
		got := make([]float64, fs)
		n, derr := ndec.Decode(aus[f], got)
		require.NoErrorf(t, derr, "native PS decode of AU %d", f)
		require.Equal(t, frame, n)

		// Reference int16 engine path.
		refI16 := make([]int16, fs)
		_, rerr := refEng.DecodeAccessUnit(aus[f], refI16)
		require.NoErrorf(t, rerr, "ref heaac PS decode of AU %d", f)

		// EXACT: the public float64 output is the reference int16 / 32768.
		for i := 0; i < fs; i++ {
			require.False(t, math.IsNaN(got[i]) || math.IsInf(got[i], 0))
			require.Equalf(t, float64(refI16[i])/32768.0, got[i],
				"AU %d sample %d (ch %d): public native PS output diverges from the bit-exact heaac engine", f, i, i&1)
			energy += got[i] * got[i]
		}
	}
	require.Greater(t, energy, 0.0, "native PS decode produced only silence")
	t.Logf("native PS public-surface decode OK — %d frames, bit-exact stereo vs heaac.NewPSDecoder", len(aus))
}
