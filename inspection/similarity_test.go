package inspection_test

import (
	"math"
	"testing"
	"time"

	"go-mediatoolkit/consts"

	"go-mediatoolkit/generators"
	"go-mediatoolkit/inspection"

	"github.com/stretchr/testify/assert"
)

func TestSimilarityIdentical(t *testing.T) {
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100)
	assert.InDelta(t, 1.0, inspection.SpectralSimilarity(a.Data, consts.SampleRate44100, a.Data, consts.SampleRate44100), 1e-10)
}

func TestSimilarityScaledAmplitude(t *testing.T) {
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100).Data
	b := make([]float64, len(a))
	for i := range a {
		b[i] = a[i] * 0.5
	}
	assert.Greater(t, inspection.SpectralSimilarity(a, consts.SampleRate44100, b, consts.SampleRate44100), 0.99)
}

func TestSimilaritySameFreqDiffPhase(t *testing.T) {
	sr := consts.SampleRate44100
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, sr).Data
	b := make([]float64, len(a))
	angular := 2.0 * math.Pi * 440.0 / float64(sr)
	for i := range b {
		b[i] = math.Sin(angular*float64(i) + math.Pi/2)
	}
	assert.Greater(t, inspection.SpectralSimilarity(a, sr, b, sr), 0.99)
}

func TestSimilarityDifferentFreq(t *testing.T) {
	sr := consts.SampleRate44100
	a := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, sr).Data
	b := generators.Sine(8000, 200*time.Millisecond, sr).Data
	assert.Less(t, inspection.SpectralSimilarity(a, sr, b, sr), 0.1)
}

func TestSimilarityHarmonicRelation(t *testing.T) {
	sr := consts.SampleRate44100
	a := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, sr).Data
	oct := generators.Sine(consts.FreqNoteA5, 200*time.Millisecond, sr).Data
	b := make([]float64, len(a))
	for i := range b {
		b[i] = a[i] + oct[i]
	}
	sim := inspection.SpectralSimilarity(a, sr, b, sr)
	assert.Greater(t, sim, 0.3)
	assert.Less(t, sim, 0.9)
}

func TestSimilarityEmpty(t *testing.T) {
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100).Data
	assert.Equal(t, 0.0, inspection.SpectralSimilarity(nil, consts.SampleRate44100, a, consts.SampleRate44100))
	assert.Equal(t, 0.0, inspection.SpectralSimilarity(a, consts.SampleRate44100, nil, consts.SampleRate44100))
}

func TestSimilaritySilence(t *testing.T) {
	a := make([]float64, 1000)
	b := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100).Data
	assert.Equal(t, 0.0, inspection.SpectralSimilarity(a, consts.SampleRate44100, b, consts.SampleRate44100))
}

func TestSimilarityDifferentLengths(t *testing.T) {
	sr := consts.SampleRate44100
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, sr).Data
	b := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, sr).Data
	assert.Greater(t, inspection.SpectralSimilarity(a, sr, b, sr), 0.8)
}

func TestSimilarityWithSilenceTrimming(t *testing.T) {
	sr := consts.SampleRate44100
	tone := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, sr).Data

	a := make([]float64, 5000+len(tone)+3000)
	copy(a[5000:], tone)
	b := make([]float64, 1000+len(tone)+8000)
	copy(b[1000:], tone)

	sim := inspection.SpectralSimilarity(a, sr, b, sr, inspection.WithSilenceThreshold(0.001))
	assert.Greater(t, sim, 0.99)
}

func TestSimilarityDifferentSampleRates(t *testing.T) {
	a := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, consts.SampleRate44100)
	b := generators.Sine(consts.FreqNoteA4, 200*time.Millisecond, consts.SampleRate48000)
	assert.Greater(t, inspection.SpectralSimilarity(a.Data, consts.SampleRate44100, b.Data, consts.SampleRate48000), 0.95)
}

func TestSimilaritySameRatePassthrough(t *testing.T) {
	a := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100)
	assert.InDelta(t, 1.0, inspection.SpectralSimilarity(a.Data, consts.SampleRate44100, a.Data, consts.SampleRate44100), 1e-10)
}

func TestSimilarityCombinedOpts(t *testing.T) {
	tone1 := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate44100).Data
	tone2 := generators.Sine(consts.FreqNoteA4, 100*time.Millisecond, consts.SampleRate48000).Data

	a := make([]float64, 2000+len(tone1)+3000)
	copy(a[2000:], tone1)
	b := make([]float64, 500+len(tone2)+6000)
	copy(b[500:], tone2)

	sim := inspection.SpectralSimilarity(a, consts.SampleRate44100, b, consts.SampleRate48000, inspection.WithSilenceThreshold(0.001))
	assert.Greater(t, sim, 0.95)
}

func BenchmarkSpectralSimilarity(b *testing.B) {
	a := generators.Sine(consts.FreqNoteA4, time.Second, consts.SampleRate44100).Data
	c := generators.Sine(consts.FreqNoteA4, time.Second, consts.SampleRate44100).Data
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		inspection.SpectralSimilarity(a, consts.SampleRate44100, c, consts.SampleRate44100)
	}
}
