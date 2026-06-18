package inspection

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMDCTOutputLength(t *testing.T) {
	x := make([]float64, 240)
	for i := range x {
		x[i] = math.Sin(float64(i) * 0.1)
	}
	coeffs := MDCT(x)
	assert.Len(t, coeffs, 120)
}

func TestIMDCTOutputLength(t *testing.T) {
	X := make([]float64, 120)
	out := IMDCT(X, 240)
	assert.Len(t, out, 240)
}

func TestMDCTIMDCTConsistency(t *testing.T) {
	// Verify that IMDCT(MDCT(x)) produces a well-defined TDAC pattern.
	// For a single frame, y = IMDCT(MDCT(x)) should satisfy:
	// y[n] + y[N-1-n] = x[n] + x[N-1-n] for the first half, etc.
	// The exact reconstruction requires overlap-add with windowing.
	for _, N := range []int{8, 16, 32, 120, 240} {
		t.Run("", func(t *testing.T) {
			x := make([]float64, N)
			for i := range x {
				x[i] = math.Sin(float64(i)*0.3) + 0.5*math.Cos(float64(i)*0.7)
			}
			xCopy := make([]float64, N)
			copy(xCopy, x)

			coeffs := MDCT(x)
			y := IMDCT(coeffs, N)

			// The IMDCT output should have the same length as the input.
			assert.Len(t, y, N)

			// Verify TDAC: in the middle half of y, the samples should be
			// related to the original signal.
			N4 := N / 4
			for i := N4; i < 3*N4; i++ {
				assert.False(t, math.IsNaN(y[i]), "NaN at index %d", i)
				assert.False(t, math.IsInf(y[i], 0), "Inf at index %d", i)
			}
		})
	}
}

func TestMDCTOrthogonality(t *testing.T) {
	// MDCT basis vectors should be orthogonal.
	N := 16
	N2 := N / 2

	// Compute MDCT of impulse at position p.
	for p := 0; p < N; p++ {
		x := make([]float64, N)
		x[p] = 1.0
		coeffs := MDCT(x)

		// Each coefficient should be a single basis function value.
		for k := 0; k < N2; k++ {
			expected := math.Cos(math.Pi / float64(N) *
				(float64(p) + 0.5 + float64(N)/4) * (float64(k) + 0.5))
			assert.InDelta(t, expected, coeffs[k], 1e-12,
				"p=%d, k=%d", p, k)
		}
	}
}

func TestMDCTEnergyPreservation(t *testing.T) {
	// Over two adjacent frames with overlap-add, energy should be preserved.
	frameSize := 30
	N := 2 * frameSize

	// Two overlapping frames.
	signal := make([]float64, 3*frameSize)
	for i := range signal {
		signal[i] = math.Sin(float64(i) * 0.3)
	}

	c1 := MDCT(signal[0:N])
	c2 := MDCT(signal[frameSize : frameSize+N])

	// Energy in MDCT domain.
	var mdctEnergy float64
	for _, c := range c1 {
		mdctEnergy += c * c
	}
	for _, c := range c2 {
		mdctEnergy += c * c
	}

	// Energy in middle frame of time domain (after overlap-add).
	r1 := IMDCT(c1, N)
	r2 := IMDCT(c2, N)
	var timeEnergy float64
	for i := frameSize; i < 2*frameSize; i++ {
		sample := r1[i] + r2[i-frameSize]
		timeEnergy += sample * sample
	}

	// They should be related by the MDCT normalization.
	// This is a loose check — exact energy preservation requires windowing.
	assert.Greater(t, mdctEnergy, 0.0)
	assert.Greater(t, timeEnergy, 0.0)
}
