package inspection

import (
	"math"
	"math/cmplx"
	"sync"
)

// MixedRadixFFT computes the discrete Fourier transform of x for any length N.
// For power-of-2 sizes, it delegates to the optimized radix-2 FFT.
// For other sizes, it uses Bluestein's algorithm (chirp-z transform), which
// converts the N-point DFT to a circular convolution computed via power-of-2 FFTs.
func MixedRadixFFT(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}
	if isPow2(n) {
		return FFT(x)
	}
	if _, ok := radix235PlanCache.Load(n); ok || canFactorize235(n) {
		radix235FFT(x)
		return x
	}
	return bluesteinFFT(x, false)
}

// MixedRadixIFFT computes the inverse DFT for any length N.
func MixedRadixIFFT(x []complex128) []complex128 {
	n := len(x)
	if n <= 1 {
		return x
	}
	if isPow2(n) {
		return IFFT(x)
	}
	if _, ok := radix235PlanCache.Load(n); ok || canFactorize235(n) {
		radix235IFFT(x)
		return x
	}
	return bluesteinFFT(x, true)
}

// bluesteinFFT computes the DFT (or IDFT if inverse=true) using Bluestein's algorithm.
//
// The key identity: n*k = -(n-k)²/2 + n²/2 + k²/2
// transforms X[k] = Σ x[n]*W^{nk} into a convolution:
//
//	X[k] = chirp[k] * (a ⊛ b)[k]
//
// where a[n] = x[n]*chirp[n], b[n] = conj(chirp[n]), and chirp[n] = W^{n²/2}.
//
// The convolution is computed via power-of-2 FFT.
func bluesteinFFT(x []complex128, inverse bool) []complex128 {
	n := len(x)

	// Get precomputed chirp factors for this size.
	chirp := getBluesteinChirp(n)

	// Convolution size: next power of 2 >= 2*N-1.
	m := NextPow2(2*n - 1)

	// Build a[i] = x[i] * chirp[i] (padded to m).
	a := make([]complex128, m)
	for i := 0; i < n; i++ {
		if inverse {
			a[i] = x[i] * cmplx.Conj(chirp[i])
		} else {
			a[i] = x[i] * chirp[i]
		}
	}

	// Build b: chirp kernel padded for circular convolution.
	// b[0..N-1] = conj(chirp[0..N-1])
	// b[m-N+1..m-1] = conj(chirp[N-1..1]) (wrapped around)
	b := make([]complex128, m)
	for i := 0; i < n; i++ {
		if inverse {
			b[i] = chirp[i]
		} else {
			b[i] = cmplx.Conj(chirp[i])
		}
	}
	for i := 1; i < n; i++ {
		if inverse {
			b[m-i] = chirp[i]
		} else {
			b[m-i] = cmplx.Conj(chirp[i])
		}
	}

	// Convolve via FFT: C = IFFT(FFT(a) * FFT(b)).
	FFT(a)
	FFT(b)
	for i := range a {
		a[i] *= b[i]
	}
	IFFT(a)

	// Extract result: X[k] = chirp[k] * C[k].
	for i := 0; i < n; i++ {
		if inverse {
			x[i] = cmplx.Conj(chirp[i]) * a[i] / complex(float64(n), 0)
		} else {
			x[i] = chirp[i] * a[i]
		}
	}
	return x
}

// Bluestein chirp factor cache.
var (
	bluesteinMu    sync.Mutex
	bluesteinCache = make(map[int][]complex128)
)

// getBluesteinChirp returns the precomputed chirp factors: exp(-j*π*k²/N) for k=0..N-1.
func getBluesteinChirp(n int) []complex128 {
	bluesteinMu.Lock()
	c, ok := bluesteinCache[n]
	if ok {
		bluesteinMu.Unlock()
		return c
	}
	bluesteinMu.Unlock()

	c = make([]complex128, n)
	for k := 0; k < n; k++ {
		// chirp[k] = exp(-j * π * k² / N)
		angle := -math.Pi * float64(k) * float64(k) / float64(n)
		c[k] = complex(math.Cos(angle), math.Sin(angle))
	}

	bluesteinMu.Lock()
	bluesteinCache[n] = c
	bluesteinMu.Unlock()
	return c
}

// isPow2 reports whether n is a power of 2.
func isPow2(n int) bool {
	return n > 0 && n&(n-1) == 0
}
