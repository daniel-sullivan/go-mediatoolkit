package inspection

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// naiveDFT computes the DFT naively for reference.
func naiveDFT(x []complex128) []complex128 {
	n := len(x)
	result := make([]complex128, n)
	for k := 0; k < n; k++ {
		var sum complex128
		for j := 0; j < n; j++ {
			angle := -2 * math.Pi * float64(k) * float64(j) / float64(n)
			sum += x[j] * complex(math.Cos(angle), math.Sin(angle))
		}
		result[k] = sum
	}
	return result
}

func assertComplexSliceClose(t *testing.T, expected, got []complex128, tol float64, msg string) {
	t.Helper()
	require.Equal(t, len(expected), len(got), "%s: length mismatch", msg)
	for i := range expected {
		d := cmplx.Abs(expected[i] - got[i])
		if d > tol {
			t.Errorf("%s: index %d: expected %v, got %v (diff=%v)", msg, i, expected[i], got[i], d)
		}
	}
}

func TestMixedRadixFFTSizes(t *testing.T) {
	sizes := []int{2, 3, 4, 5, 6, 8, 10, 12, 15, 16, 20, 30, 60, 120, 240}

	for _, n := range sizes {
		t.Run("", func(t *testing.T) {
			// Generate test signal.
			x := make([]complex128, n)
			xRef := make([]complex128, n)
			for i := range x {
				v := complex(math.Sin(float64(i)*0.3)+math.Cos(float64(i)*0.7), 0)
				x[i] = v
				xRef[i] = v
			}

			expected := naiveDFT(xRef)
			MixedRadixFFT(x)

			assertComplexSliceClose(t, expected, x, 1e-8, "")
		})
	}
}

func TestMixedRadixFFTRoundTrip(t *testing.T) {
	sizes := []int{3, 5, 6, 10, 15, 30, 60, 120, 240}

	for _, n := range sizes {
		t.Run("", func(t *testing.T) {
			original := make([]complex128, n)
			for i := range original {
				original[i] = complex(math.Sin(float64(i)*0.5), math.Cos(float64(i)*0.3))
			}

			x := make([]complex128, n)
			copy(x, original)

			MixedRadixFFT(x)
			MixedRadixIFFT(x)

			assertComplexSliceClose(t, original, x, 1e-9, "round trip")
		})
	}
}

func TestMixedRadixFFTParseval(t *testing.T) {
	// Parseval's theorem: sum|x|^2 = (1/N) * sum|X|^2
	n := 60
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Sin(float64(i)*0.7), 0)
	}

	var timeEnergy float64
	for _, v := range x {
		timeEnergy += real(v)*real(v) + imag(v)*imag(v)
	}

	MixedRadixFFT(x)

	var freqEnergy float64
	for _, v := range x {
		freqEnergy += real(v)*real(v) + imag(v)*imag(v)
	}
	freqEnergy /= float64(n)

	assert.InDelta(t, timeEnergy, freqEnergy, 1e-8, "Parseval's theorem")
}

func TestMixedRadixMatchesRadix2(t *testing.T) {
	for _, n := range []int{4, 8, 16, 64, 256} {
		x1 := make([]complex128, n)
		x2 := make([]complex128, n)
		for i := range x1 {
			x1[i] = complex(math.Sin(float64(i)*0.4), math.Cos(float64(i)*0.6))
			x2[i] = x1[i]
		}

		FFT(x1)
		MixedRadixFFT(x2)

		assertComplexSliceClose(t, x1, x2, 1e-12, "power-of-2 match")
	}
}

func TestMixedRadixFFTDCSignal(t *testing.T) {
	n := 60
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(3.0, 0)
	}
	MixedRadixFFT(x)

	assert.InDelta(t, 3.0*float64(n), cmplx.Abs(x[0]), 1e-9, "DC bin")
	for i := 1; i < n; i++ {
		assert.InDelta(t, 0, cmplx.Abs(x[i]), 1e-9, "bin %d should be zero", i)
	}
}

func TestMixedRadixFFTPureTone(t *testing.T) {
	// A pure cosine at bin k should produce energy only in bins k and N-k.
	n := 60
	k := 7
	x := make([]complex128, n)
	for i := range x {
		x[i] = complex(math.Cos(2*math.Pi*float64(k)*float64(i)/float64(n)), 0)
	}
	MixedRadixFFT(x)

	for i := 0; i < n; i++ {
		mag := cmplx.Abs(x[i])
		if i == k || i == n-k {
			assert.InDelta(t, float64(n)/2, mag, 1e-8, "bin %d should have energy", i)
		} else {
			assert.InDelta(t, 0, mag, 1e-8, "bin %d should be zero", i)
		}
	}
}
