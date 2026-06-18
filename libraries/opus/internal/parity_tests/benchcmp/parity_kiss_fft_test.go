//go:build cgo && opus_strict

package benchcmp

import (
	"math"
	"math/rand"
	"testing"

	"go-mediatoolkit/libraries/opus/internal/nativeopus"
)

// buildGoFFTState mirrors C's kiss_fft_state by copying fields from
// the given cFFT handle — isolates butterfly parity from any Go-vs-C
// cos/sin differences in twiddle computation.
func buildGoFFTState(t *testing.T, cst cFFT) nativeopus.FftStateHandle {
	t.Helper()
	nfft := cst.Nfft()
	factors := make([]int16, 2*8)
	for i := range factors {
		factors[i] = cst.Factor(i)
	}
	bitrev := make([]int16, nfft)
	for i := range bitrev {
		bitrev[i] = cst.Bitrev(i)
	}
	// Twiddle table length = nfft for base states allocated via
	// opus_fft_alloc (non-shared) in our build.
	twR := make([]float32, nfft)
	twI := make([]float32, nfft)
	for i := 0; i < nfft; i++ {
		twR[i] = cst.Twr(i)
		twI[i] = cst.Twi(i)
	}
	return nativeopus.NewFftStateFromData(
		nfft, cst.Scale(), cst.Shift(), factors, bitrev, twR, twI)
}

// fftSizesToTest — every nfft value CELT actually uses plus a few
// small ones for factor-chain coverage.
var fftSizesToTest = []int{
	15, 60, 120, 240, 480, 960,
}

// TestParity_OpusFFT — forward FFT bit-exactness across Opus-used
// transform sizes.
func TestParity_OpusFFT(t *testing.T) {
	for _, n := range fftSizesToTest {
		t.Run(nameN(n), func(t *testing.T) {
			cst := cFFTAlloc(n)
			if cst.p == nil {
				t.Fatalf("cFFTAlloc(%d) returned NULL", n)
				return
			}
			defer cst.Free()
			gst := buildGoFFTState(t, cst)

			r := rand.New(rand.NewSource(int64(n) + 17))
			in := make([]float32, 2*n)
			for i := range in {
				in[i] = r.Float32()*2 - 1
			}

			cOut := make([]float32, 2*n)
			gOut := make([]float32, 2*n)
			cst.Fft(in, cOut)
			nativeopus.ExportTestOpusFFTC(gst, in, gOut)
			for i := range cOut {
				if math.Float32bits(cOut[i]) != math.Float32bits(gOut[i]) {
					t.Errorf("nfft=%d [%d]: C=%g (0x%08x) Go=%g (0x%08x)",
						n, i, cOut[i], math.Float32bits(cOut[i]),
						gOut[i], math.Float32bits(gOut[i]))
					break
				}
			}
		})
	}
}

// TestParity_OpusIFFT — inverse FFT parity.
func TestParity_OpusIFFT(t *testing.T) {
	for _, n := range fftSizesToTest {
		t.Run(nameN(n), func(t *testing.T) {
			cst := cFFTAlloc(n)
			if cst.p == nil {
				t.Fatalf("cFFTAlloc(%d) returned NULL", n)
				return
			}
			defer cst.Free()
			gst := buildGoFFTState(t, cst)

			r := rand.New(rand.NewSource(int64(n) + 29))
			in := make([]float32, 2*n)
			for i := range in {
				in[i] = r.Float32()*2 - 1
			}

			cOut := make([]float32, 2*n)
			gOut := make([]float32, 2*n)
			cst.IFft(in, cOut)
			nativeopus.ExportTestOpusIFFTC(gst, in, gOut)
			for i := range cOut {
				if math.Float32bits(cOut[i]) != math.Float32bits(gOut[i]) {
					t.Errorf("nfft=%d [%d]: C=%g (0x%08x) Go=%g (0x%08x)",
						n, i, cOut[i], math.Float32bits(cOut[i]),
						gOut[i], math.Float32bits(gOut[i]))
					break
				}
			}
		})
	}
}

func nameN(n int) string { return "nfft=" + sprintfDec32(int32(n)) }
