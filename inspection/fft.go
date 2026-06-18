// Package inspection provides audio analysis utilities including FFT-based
// spectral comparison.
package inspection

import (
	"math"
	"math/bits"
	"math/cmplx"
	"sync"
)

// FFT computes the discrete Fourier transform of x using an iterative
// radix-2 Cooley-Tukey algorithm. The input length must be a power of 2;
// use PadPow2 to pad before calling. The transform is computed in-place
// and the result is returned.
func FFT(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}

	// Bit-reversal permutation.
	bitReverse(x)

	// Butterfly passes — dispatched to NEON on arm64.
	tw := getTwiddle(n)
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		step := n / size
		butterflyPass(x, tw, half, step)
	}
	return x
}

// IFFT computes the inverse discrete Fourier transform.
func IFFT(x []complex128) []complex128 {
	n := len(x)
	// Conjugate, FFT, conjugate, scale.
	for i := range x {
		x[i] = cmplx.Conj(x[i])
	}
	FFT(x)
	scale := complex(1.0/float64(n), 0)
	for i := range x {
		x[i] = cmplx.Conj(x[i]) * scale
	}
	return x
}

// RealFFT computes the FFT of a real-valued signal and returns only the
// magnitude spectrum (N/2+1 bins). This is the common case for audio analysis.
func RealFFT(x []float64) []float64 {
	n := NextPow2(len(x))
	buf := make([]complex128, n)
	for i, v := range x {
		buf[i] = complex(v, 0)
	}
	FFT(buf)

	// Only the first N/2+1 bins are unique for real input.
	bins := n/2 + 1
	mag := make([]float64, bins)
	for i := 0; i < bins; i++ {
		mag[i] = cmplx.Abs(buf[i])
	}
	return mag
}

// NextPow2 returns the smallest power of 2 >= n.
func NextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// PadPow2 returns a copy of x zero-padded to the next power of 2 length.
func PadPow2(x []complex128) []complex128 {
	n := NextPow2(len(x))
	if n == len(x) {
		out := make([]complex128, n)
		copy(out, x)
		return out
	}
	out := make([]complex128, n)
	copy(out, x)
	return out
}

// bitReverse performs an in-place bit-reversal permutation on x.
func bitReverse(x []complex128) {
	n := len(x)
	perm := getBitRevPerm(n)
	for i := 0; i < n; i++ {
		j := perm[i]
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
}

// Precomputed bit-reversal permutation tables, keyed by FFT size.
var (
	bitRevMu    sync.Mutex
	bitRevCache = make(map[int][]int)
)

func getBitRevPerm(n int) []int {
	bitRevMu.Lock()
	perm, ok := bitRevCache[n]
	if ok {
		bitRevMu.Unlock()
		return perm
	}
	bitRevMu.Unlock()

	nbits := bits.TrailingZeros(uint(n)) // n is power-of-2, so this gives log2(n)
	perm = make([]int, n)
	for i := 0; i < n; i++ {
		perm[i] = int(bits.Reverse(uint(i)) >> (bits.UintSize - nbits))
	}

	bitRevMu.Lock()
	bitRevCache[n] = perm
	bitRevMu.Unlock()
	return perm
}

// Twiddle factor cache. Each entry stores the pre-computed roots of unity
// for a given FFT size, avoiding repeated sin/cos calls.
var twiddleCache sync.Map

func getTwiddle(n int) []complex128 {
	if v, ok := twiddleCache.Load(n); ok {
		return v.([]complex128)
	}
	tw := make([]complex128, n/2)
	for k := 0; k < n/2; k++ {
		angle := -2.0 * math.Pi * float64(k) / float64(n)
		tw[k] = complex(math.Cos(angle), math.Sin(angle))
	}
	twiddleCache.Store(n, tw)
	return tw
}
