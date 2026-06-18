package inspection_test

import (
	"math"
	"math/cmplx"
	"testing"

	"go-mediatoolkit/inspection"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFFTImpulse(t *testing.T) {
	x := []complex128{1, 0, 0, 0}
	inspection.FFT(x)
	for i, v := range x {
		assert.InDelta(t, 0, cmplx.Abs(v-1), 1e-10, "bin %d", i)
	}
}

func TestFFTDC(t *testing.T) {
	x := []complex128{1, 1, 1, 1}
	inspection.FFT(x)
	assert.InDelta(t, 4.0, cmplx.Abs(x[0]), 1e-10, "DC bin")
	for i := 1; i < len(x); i++ {
		assert.InDelta(t, 0, cmplx.Abs(x[i]), 1e-10, "bin %d", i)
	}
}

func TestFFTPureSine(t *testing.T) {
	n := 8
	x := make([]complex128, n)
	for i := 0; i < n; i++ {
		x[i] = complex(math.Sin(2*math.Pi*float64(i)/float64(n)), 0)
	}
	inspection.FFT(x)

	for i := 0; i < n; i++ {
		mag := cmplx.Abs(x[i])
		if i == 1 || i == n-1 {
			assert.InDelta(t, 4.0, mag, 1e-10, "bin %d", i)
		} else {
			assert.InDelta(t, 0, mag, 1e-10, "bin %d", i)
		}
	}
}

func TestFFTInverse(t *testing.T) {
	original := []complex128{1, 2, 3, 4, 5, 6, 7, 8}
	x := make([]complex128, len(original))
	copy(x, original)

	inspection.FFT(x)
	inspection.IFFT(x)

	for i := range original {
		assert.InDelta(t, 0, cmplx.Abs(x[i]-original[i]), 1e-10, "sample %d", i)
	}
}

func TestRealFFT(t *testing.T) {
	mag := inspection.RealFFT([]float64{1, 1, 1, 1, 1, 1, 1, 1})
	require.Len(t, mag, 5)
	assert.InDelta(t, 8.0, mag[0], 1e-10, "DC magnitude")
	for i := 1; i < len(mag); i++ {
		assert.InDelta(t, 0, mag[i], 1e-10, "bin %d", i)
	}
}

func TestNextPow2(t *testing.T) {
	tests := []struct{ in, want int }{
		{0, 1}, {1, 1}, {2, 2}, {3, 4}, {4, 4},
		{5, 8}, {7, 8}, {8, 8}, {9, 16}, {1023, 1024}, {1024, 1024},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, inspection.NextPow2(tt.in), "NextPow2(%d)", tt.in)
	}
}

func TestPadPow2(t *testing.T) {
	out := inspection.PadPow2([]complex128{1, 2, 3})
	require.Len(t, out, 4)
	assert.Equal(t, complex128(0), out[3])
}

func BenchmarkFFT1024(b *testing.B)  { benchFFT(b, 1024) }
func BenchmarkFFT4096(b *testing.B)  { benchFFT(b, 4096) }
func BenchmarkFFT65536(b *testing.B) { benchFFT(b, 65536) }

func benchFFT(b *testing.B, n int) {
	b.Helper()
	src := make([]complex128, n)
	for i := range src {
		src[i] = complex(math.Sin(float64(i)*0.01), 0)
	}
	buf := make([]complex128, n)
	b.ResetTimer()
	b.SetBytes(int64(n * 16))
	for i := 0; i < b.N; i++ {
		copy(buf, src)
		inspection.FFT(buf)
	}
}
