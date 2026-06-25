package inspection

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/mutations"
)

type similarityConfig struct {
	silenceThreshold float64
}

// SimilarityOption configures a SpectralSimilarity call.
type SimilarityOption func(*similarityConfig)

// WithSilenceThreshold trims samples with absolute value <= threshold from
// both ends before comparison. A typical value for normalized audio is 0.001.
func WithSilenceThreshold(threshold float64) SimilarityOption {
	return func(c *similarityConfig) {
		c.silenceThreshold = threshold
	}
}

// SpectralSimilarity computes the similarity between two audio signals based
// on their frequency content. It returns a value in [0.0, 1.0] where 0.0
// means no spectral overlap and 1.0 means identical spectra.
//
// The comparison uses cosine similarity of the magnitude spectra (via FFT).
// This makes it invariant to amplitude scaling — two signals with the same
// frequency content but different volumes will still score 1.0.
//
// When sample rates differ, the magnitude spectra are interpolated onto a
// common frequency axis before comparison.
//
// Usage:
//
//	sim := inspection.SpectralSimilarity(a, 44100, b, 48000,
//	    inspection.WithSilenceThreshold(0.001),
//	)
func SpectralSimilarity(a []float64, aSampleRate int, b []float64, bSampleRate int, opts ...SimilarityOption) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	var cfg similarityConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	sigA, sigB := a, b

	if cfg.silenceThreshold > 0 {
		sigA = mutations.TrimSilence(sigA, mutations.TrimBoth, cfg.silenceThreshold)
		sigB = mutations.TrimSilence(sigB, mutations.TrimBoth, cfg.silenceThreshold)
		if len(sigA) == 0 || len(sigB) == 0 {
			return 0
		}
	}

	if aSampleRate > 0 && bSampleRate > 0 && aSampleRate != bSampleRate {
		return spectralSimilarityRateMatched(sigA, sigB, aSampleRate, bSampleRate)
	}

	n := NextPow2(max(len(sigA), len(sigB)))
	magA := realFFTPadded(sigA, n)
	magB := realFFTPadded(sigB, n)
	return cosineSimilarity(magA, magB)
}

// spectralSimilarityRateMatched compares two signals at different sample rates
// by interpolating their magnitude spectra onto a common frequency axis.
func spectralSimilarityRateMatched(a, b []float64, rateA, rateB int) float64 {
	nA := NextPow2(len(a))
	nB := NextPow2(len(b))
	magA := realFFTPadded(a, nA)
	magB := realFFTPadded(b, nB)

	nyquistA := float64(rateA) / 2.0
	nyquistB := float64(rateB) / 2.0
	maxFreq := math.Min(nyquistA, nyquistB)

	binWidthA := float64(rateA) / float64(nA)
	binWidthB := float64(rateB) / float64(nB)
	resolution := math.Min(binWidthA, binWidthB)

	numBins := int(maxFreq/resolution) + 1
	interpA := make([]float64, numBins)
	interpB := make([]float64, numBins)

	for i := 0; i < numBins; i++ {
		freq := float64(i) * resolution
		interpA[i] = interpolateMag(magA, freq, float64(rateA), nA)
		interpB[i] = interpolateMag(magB, freq, float64(rateB), nB)
	}

	return cosineSimilarity(interpA, interpB)
}

// interpolateMag returns the linearly interpolated magnitude at a given
// frequency from an FFT magnitude spectrum.
func interpolateMag(mag []float64, freq, sampleRate float64, fftSize int) float64 {
	bin := freq * float64(fftSize) / sampleRate
	i := int(bin)
	if i >= len(mag)-1 {
		return mag[len(mag)-1]
	}
	frac := bin - float64(i)
	return mag[i] + frac*(mag[i+1]-mag[i])
}

// realFFTPadded computes the magnitude spectrum of x zero-padded to length n.
func realFFTPadded(x []float64, n int) []float64 {
	padded := make([]float64, n)
	copy(padded, x)
	return RealFFT(padded)
}

// cosineSimilarity computes dot(a,b) / (|a| * |b|).
func cosineSimilarity(a, b []float64) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	sim := dot / denom
	if sim > 1.0 {
		return 1.0
	}
	if sim < 0.0 {
		return 0.0
	}
	return sim
}
