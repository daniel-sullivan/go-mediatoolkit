//go:build cgo && rnnoise_strict

package biquad

import (
	"math"
	"math/rand"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/denoise/internal/rnnoise"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// coefficients from rnnoise_process_frame (denoise.c):
//
//	static const float b_hp[2] = {-2, 1};
//	static const float a_hp[2] = {-1.99599, 0.99600};
var (
	bHP = [2]float32{-2, 1}
	aHP = [2]float32{-1.99599, 0.99600}
)

func bitEq(t *testing.T, i int, c, g float32) {
	t.Helper()
	cb, gb := math.Float32bits(c), math.Float32bits(g)
	assert.Equalf(t, cb, gb, "sample %d: C=%v (0x%08x) Go=%v (0x%08x)", i, c, cb, g, gb)
}

// runStreamed pushes signal through both implementations in FrameSize
// chunks (as the real pipeline does), threading the 2-word state across
// frames, and asserts bit-exact output and state at every step.
func runStreamed(t *testing.T, signal []float32) {
	t.Helper()
	var cMem, gMem [2]float32
	n := rnnoise.FrameSize
	for off := 0; off < len(signal); off += n {
		end := off + n
		if end > len(signal) {
			end = len(signal)
		}
		frame := signal[off:end]

		cy := cBiquad(frame, bHP, aHP, &cMem)

		gy := make([]float32, len(frame))
		rnnoise.BiquadHP(gy, &gMem, frame)

		require.Len(t, gy, len(cy))
		for i := range cy {
			bitEq(t, off+i, cy[i], gy[i])
		}
		bitEq(t, -1, cMem[0], gMem[0])
		bitEq(t, -2, cMem[1], gMem[1])
	}
}

func TestBiquadImpulse(t *testing.T) {
	s := make([]float32, 4*rnnoise.FrameSize)
	s[0] = 32768
	s[rnnoise.FrameSize+5] = -16000
	runStreamed(t, s)
}

func TestBiquadRampAndDC(t *testing.T) {
	s := make([]float32, 3*rnnoise.FrameSize)
	for i := range s {
		s[i] = float32(i%1000) - 500 // sawtooth around 0
	}
	runStreamed(t, s)
}

func TestBiquadTones(t *testing.T) {
	for _, freq := range []float64{50, 440, 3000, 12000} {
		s := make([]float32, 5*rnnoise.FrameSize)
		for i := range s {
			s[i] = float32(20000 * math.Sin(2*math.Pi*freq*float64(i)/48000))
		}
		runStreamed(t, s)
	}
}

func TestBiquadFullScaleAndNoise(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	s := make([]float32, 6*rnnoise.FrameSize)
	for i := range s {
		switch {
		case i%97 == 0:
			s[i] = 32767
		case i%89 == 0:
			s[i] = -32768
		default:
			s[i] = float32(r.Intn(65536) - 32768)
		}
	}
	runStreamed(t, s)
}
