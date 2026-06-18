package inspection

import "math"

// MDCT computes the Modified Discrete Cosine Transform of x.
// The input length N must be even. The output has N/2 coefficients.
//
// The MDCT is defined as:
//
//	X[k] = Σ_{n=0}^{N-1} x[n] * cos(π/N * (n + 1/2 + N/4) * (k + 1/2))
func MDCT(x []float64) []float64 {
	N := len(x)
	if N < 2 || N%2 != 0 {
		panic("inspection: MDCT input length must be even and >= 2")
	}

	N2 := N / 2
	out := make([]float64, N2)
	for k := 0; k < N2; k++ {
		var sum float64
		for n := 0; n < N; n++ {
			angle := math.Pi / float64(N) * (float64(n) + 0.5 + float64(N)/4) * (float64(k) + 0.5)
			sum += x[n] * math.Cos(angle)
		}
		out[k] = sum
	}
	return out
}

// IMDCT computes the Inverse Modified Discrete Cosine Transform.
// The input has N/2 coefficients and produces N output samples.
// The caller is responsible for overlap-add with adjacent frames.
//
// The IMDCT is defined as:
//
//	x[n] = (2/N) * Σ_{k=0}^{N/2-1} X[k] * cos(π/N * (n + 1/2 + N/4) * (k + 1/2))
func IMDCT(X []float64, N int) []float64 {
	N2 := N / 2
	if len(X) != N2 {
		panic("inspection: IMDCT input length must be N/2")
	}
	if N < 2 || N%2 != 0 {
		panic("inspection: IMDCT N must be even and >= 2")
	}

	out := make([]float64, N)
	scale := 2.0 / float64(N)
	for n := 0; n < N; n++ {
		var sum float64
		for k := 0; k < N2; k++ {
			angle := math.Pi / float64(N) * (float64(n) + 0.5 + float64(N)/4) * (float64(k) + 0.5)
			sum += X[k] * math.Cos(angle)
		}
		out[n] = sum * scale
	}
	return out
}

// VerifyMDCTRoundTrip checks that MDCT -> IMDCT with overlap-add recovers
// the original signal (within tolerance). Returns the maximum error.
func VerifyMDCTRoundTrip(signal []float64, frameSize int) float64 {
	N := 2 * frameSize
	numFrames := (len(signal) - frameSize) / frameSize
	if numFrames < 2 {
		return 0
	}

	output := make([]float64, len(signal))
	for f := 0; f < numFrames; f++ {
		start := f * frameSize
		frame := signal[start : start+N]
		coeffs := MDCT(frame)
		reconstructed := IMDCT(coeffs, N)
		for i := 0; i < N; i++ {
			output[start+i] += reconstructed[i]
		}
	}

	maxErr := 0.0
	for i := frameSize; i < (numFrames-1)*frameSize; i++ {
		err := math.Abs(output[i] - signal[i])
		if err > maxErr {
			maxErr = err
		}
	}
	return maxErr
}
