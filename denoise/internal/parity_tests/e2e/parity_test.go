//go:build cgo && rnnoise_strict

package e2e

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func bitEq(t *testing.T, ctx string, frame, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "%s frame %d [%d]: C=%v (0x%08x) Go=%v (0x%08x)", ctx, frame, i, c, cb, g, gb)
}

// stream builds n FrameSize frames of ±32768-scale audio cycling through
// voiced speech, noise, near-silence (denormal/quiet path), tones, and a
// full-scale burst.
func stream(n int) [][]float32 {
	const fs = 480
	r := rand.New(rand.NewSource(20260714))
	out := make([][]float32, n)
	for f := 0; f < n; f++ {
		fr := make([]float32, fs)
		switch f % 5 {
		case 0, 1: // voiced
			period := 110.0 + float64((f*7)%9)*25
			f0 := 48000.0 / period
			for i := range fr {
				gi := f*fs + i
				fr[i] = float32(9000*math.Sin(2*math.Pi*f0*float64(gi)/48000)+
					4000*math.Sin(2*math.Pi*2*f0*float64(gi)/48000)+
					2000*math.Sin(2*math.Pi*3*f0*float64(gi)/48000)) +
					float32(r.Intn(1500)-750)
			}
		case 2: // noise
			for i := range fr {
				fr[i] = float32(r.Intn(24000) - 12000)
			}
		case 3: // near silence
			for i := range fr {
				fr[i] = float32(r.Intn(3) - 1)
			}
		default: // tone + occasional full-scale
			for i := range fr {
				gi := f*fs + i
				fr[i] = float32(15000 * math.Sin(2*math.Pi*1000*float64(gi)/48000))
				if i%128 == 0 {
					fr[i] = 32767
				}
			}
		}
		out[f] = fr
	}
	return out
}

// TestProcessFrameE2E is the end-to-end gate: many frames of varied
// audio through both denoisers, output PCM and VAD probability compared
// bit-for-bit. Any divergence anywhere in the chain surfaces here.
func TestProcessFrameE2E(t *testing.T) {
	cd := newCDenoiser()
	gs := rnnoise.NewState()

	for f, in := range stream(1500) { // 1500 frames = 15 s; recurrent state compounds any drift
		cout, cvad := cd.frame(in)
		gout := make([]float32, len(in))
		gvad := gs.ProcessFrame(gout, in)

		require.Lenf(t, gout, len(cout), "frame %d length", f)
		for i := range cout {
			bitEq(t, "out", f, i, cout[i], gout[i])
		}
		bitEq(t, "vad", f, 0, cvad, gvad)
	}
}
