//go:build cgo && aec_oracle

package fft

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// lcg is the same PRNG the smoke slice uses; both sides consume the
// identical Go-generated inputs, so its quality is irrelevant —
// determinism and coverage of many magnitudes is all that matters.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	// Roughly [-1, 1), like scaled PCM.
	return float32(int32(*l)) / float32(math.MaxInt32)
}

func requireBitExact(t *testing.T, what string, got, want []float32) {
	t.Helper()
	for i := range want {
		if math.Float32bits(got[i]) != math.Float32bits(want[i]) {
			t.Fatalf("%s: index %d: got %v (0x%08x), want %v (0x%08x)",
				what, i, got[i], math.Float32bits(got[i]),
				want[i], math.Float32bits(want[i]))
		}
	}
}

// TestOouraForwardInverseParity feeds PRNG vectors through the raw
// Ooura forward and inverse transforms and requires bit-exact
// agreement, including chained round-trips (fft -> ifft -> fft) so
// intermediate packed layouts are exercised too.
func TestOouraForwardInverseParity(t *testing.T) {
	rng := lcg(1)
	for iter := 0; iter < 200; iter++ {
		var goBuf, cBuf [128]float32
		for i := range goBuf {
			v := rng.next() * 1000
			goBuf[i] = v
			cBuf[i] = v
		}

		var ooura aec3.OouraFFT
		ooura.Fft(goBuf[:])
		oouraFftC(&cBuf)
		requireBitExact(t, "forward", goBuf[:], cBuf[:])

		ooura.InverseFft(goBuf[:])
		oouraIfftC(&cBuf)
		requireBitExact(t, "inverse", goBuf[:], cBuf[:])

		ooura.Fft(goBuf[:])
		oouraFftC(&cBuf)
		requireBitExact(t, "forward-again", goBuf[:], cBuf[:])
	}
}

// TestAec3FftWrapperParity checks ZeroPaddedFft (rectangular +
// Hanning) and PaddedFft (rectangular + sqrt-Hanning) against the
// oracle for PRNG inputs across many magnitudes.
func TestAec3FftWrapperParity(t *testing.T) {
	rng := lcg(2)
	goFFT := aec3.NewAec3FFT()
	scales := []float32{1, 1e-3, 32768}

	for iter := 0; iter < 100; iter++ {
		scale := scales[iter%len(scales)]
		var x, xOld [64]float32
		for i := range x {
			x[i] = rng.next() * scale
			xOld[i] = rng.next() * scale
		}

		for _, w := range []aec3.Window{aec3.WindowRectangular, aec3.WindowHanning} {
			var X aec3.FFTData
			goFFT.ZeroPaddedFft(x[:], w, &X)
			re, im := zeroPaddedFftC(&x, int(w))
			requireBitExact(t, "ZeroPaddedFft re", X.Re[:], re[:])
			requireBitExact(t, "ZeroPaddedFft im", X.Im[:], im[:])
		}

		for _, w := range []aec3.Window{aec3.WindowRectangular, aec3.WindowSqrtHanning} {
			var X aec3.FFTData
			goFFT.PaddedFft(x[:], xOld[:], w, &X)
			re, im := paddedFftC(&x, &xOld, int(w))
			requireBitExact(t, "PaddedFft re", X.Re[:], re[:])
			requireBitExact(t, "PaddedFft im", X.Im[:], im[:])
		}
	}
}
