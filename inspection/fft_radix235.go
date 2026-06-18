package inspection

import (
	"math"
	"sync"
)

// radix235FFT computes an in-place DFT for sizes that factor completely into
// primes 2, 3, and 5. This covers the CELT N/4 MDCT sizes (60, 120, 240)
// without the overhead of Bluestein zero-padding.
//
// Uses iterative Cooley-Tukey decimation-in-time with mixed-radix butterflies.
func radix235FFT(x []complex128) {
	n := len(x)
	plan := getRadix235Plan(n)

	// Digit-reversed permutation (uses scratch buffer from plan).
	scratch := plan.scratch[:n]
	copy(scratch, x)
	perm := plan.perm
	for i := 0; i < n; i++ {
		x[i] = scratch[perm[i]]
	}

	// Butterfly passes, one per factor.
	tw := plan.twiddle
	stageSize := 1
	for _, p := range plan.factors {
		groupSize := stageSize * p
		fstride := n / groupSize

		for groupStart := 0; groupStart < n; groupStart += groupSize {
			for j := 0; j < stageSize; j++ {
				base := groupStart + j
				switch p {
				case 2:
					bfly2(x, base, stageSize, tw, j*fstride, n)
				case 3:
					bfly3(x, base, stageSize, tw, j*fstride, n)
				case 5:
					bfly5(x, base, stageSize, tw, j*fstride, n)
				}
			}
		}
		stageSize = groupSize
	}
}

// radix235IFFT computes the inverse DFT.
func radix235IFFT(x []complex128) {
	n := len(x)
	for i := range x {
		x[i] = complex(real(x[i]), -imag(x[i]))
	}
	radix235FFT(x)
	s := 1.0 / float64(n)
	for i := range x {
		x[i] = complex(real(x[i])*s, -imag(x[i])*s)
	}
}

// bfly2 performs a radix-2 butterfly with twiddle.
func bfly2(x []complex128, base, stride int, tw []complex128, twOff, n int) {
	i0 := base
	i1 := base + stride
	t := x[i1] * tw[twOff%n]
	x[i0], x[i1] = x[i0]+t, x[i0]-t
}

// bfly3 performs a radix-3 butterfly with twiddles.
func bfly3(x []complex128, base, stride int, tw []complex128, twOff, n int) {
	i0 := base
	i1 := base + stride
	i2 := base + 2*stride

	// Apply twiddles to elements 1 and 2.
	s1 := x[i1] * tw[twOff%n]
	s2 := x[i2] * tw[(2*twOff)%n]
	a0 := x[i0]

	// 3-point DFT constants: w3 = exp(-2πi/3).
	const (
		c3 = -0.5                    // cos(2π/3)
		s3 = -0.86602540378443864676 // -sin(2π/3) = -√3/2
	)

	sum := s1 + s2
	diff := s1 - s2

	x[i0] = a0 + sum
	x[i1] = complex(
		real(a0)+c3*real(sum)-s3*imag(diff),
		imag(a0)+c3*imag(sum)+s3*real(diff))
	x[i2] = complex(
		real(a0)+c3*real(sum)+s3*imag(diff),
		imag(a0)+c3*imag(sum)-s3*real(diff))
}

// bfly5 performs a radix-5 butterfly with twiddles.
func bfly5(x []complex128, base, stride int, tw []complex128, twOff, n int) {
	i0 := base
	i1 := base + stride
	i2 := base + 2*stride
	i3 := base + 3*stride
	i4 := base + 4*stride

	// Apply twiddles.
	a0 := x[i0]
	a1 := x[i1] * tw[twOff%n]
	a2 := x[i2] * tw[(2*twOff)%n]
	a3 := x[i3] * tw[(3*twOff)%n]
	a4 := x[i4] * tw[(4*twOff)%n]

	// 5-point DFT constants.
	const (
		c1 = 0.30901699437494742410  // cos(2π/5)
		s1 = -0.95105651629515357212 // -sin(2π/5)
		c2 = -0.80901699437494742410 // cos(4π/5)
		s2 = -0.58778525229247312917 // -sin(4π/5)
	)

	// Symmetric pairs.
	p14 := a1 + a4
	m14 := a1 - a4
	p23 := a2 + a3
	m23 := a2 - a3

	x[i0] = a0 + p14 + p23

	x[i1] = complex(
		real(a0)+c1*real(p14)+c2*real(p23)-s1*imag(m14)-s2*imag(m23),
		imag(a0)+c1*imag(p14)+c2*imag(p23)+s1*real(m14)+s2*real(m23))

	x[i2] = complex(
		real(a0)+c2*real(p14)+c1*real(p23)-s2*imag(m14)+s1*imag(m23),
		imag(a0)+c2*imag(p14)+c1*imag(p23)+s2*real(m14)-s1*real(m23))

	x[i3] = complex(
		real(a0)+c2*real(p14)+c1*real(p23)+s2*imag(m14)-s1*imag(m23),
		imag(a0)+c2*imag(p14)+c1*imag(p23)-s2*real(m14)+s1*real(m23))

	x[i4] = complex(
		real(a0)+c1*real(p14)+c2*real(p23)+s1*imag(m14)+s2*imag(m23),
		imag(a0)+c1*imag(p14)+c2*imag(p23)-s1*real(m14)-s2*real(m23))
}

// ── Plan cache ──────────────────────────────────────────────────────

type radix235Plan struct {
	factors []int
	perm    []int
	twiddle []complex128
	scratch []complex128
}

var radix235PlanCache sync.Map

func getRadix235Plan(n int) *radix235Plan {
	if v, ok := radix235PlanCache.Load(n); ok {
		return v.(*radix235Plan)
	}
	factors := factorize235(n)
	if factors == nil {
		panic("radix235FFT: n has prime factors other than 2, 3, 5")
	}

	// Compute digit-reversal permutation.
	perm := make([]int, n)
	for i := 0; i < n; i++ {
		idx := i
		digits := make([]int, len(factors))
		for f := 0; f < len(factors); f++ {
			digits[f] = idx % factors[f]
			idx /= factors[f]
		}
		j := 0
		radix := 1
		for f := len(factors) - 1; f >= 0; f-- {
			j += digits[f] * radix
			radix *= factors[f]
		}
		perm[i] = j
	}

	// Precompute twiddle factors.
	tw := make([]complex128, n)
	for k := 0; k < n; k++ {
		angle := -2.0 * math.Pi * float64(k) / float64(n)
		tw[k] = complex(math.Cos(angle), math.Sin(angle))
	}

	plan := &radix235Plan{
		factors: factors,
		perm:    perm,
		twiddle: tw,
		scratch: make([]complex128, n),
	}
	radix235PlanCache.Store(n, plan)
	return plan
}

// canFactorize235 reports whether n factors completely into 2, 3, and 5.
func canFactorize235(n int) bool {
	for n%2 == 0 {
		n /= 2
	}
	for n%3 == 0 {
		n /= 3
	}
	for n%5 == 0 {
		n /= 5
	}
	return n == 1
}

// factorize235 returns the prime factorization of n using only factors 2, 3, 5.
// Returns nil if n has other prime factors. Factors are smallest-first.
func factorize235(n int) []int {
	if n <= 1 {
		return []int{}
	}
	var factors []int
	for n%2 == 0 {
		factors = append(factors, 2)
		n /= 2
	}
	for n%3 == 0 {
		factors = append(factors, 3)
		n /= 3
	}
	for n%5 == 0 {
		factors = append(factors, 5)
		n /= 5
	}
	if n != 1 {
		return nil
	}
	return factors
}
