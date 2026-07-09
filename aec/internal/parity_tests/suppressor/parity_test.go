//go:build cgo && aec_oracle

package suppressor

import (
	"math"
	"testing"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
	"github.com/daniel-sullivan/go-mediatoolkit/aec/internal/aec3"
)

// This slice drives ComfortNoiseGenerator, SuppressionGain and
// SuppressionFilter directly (component-level; see shim.h/cgo.go's
// doc comments for why a full render/subtractor/AecState pipeline
// is not needed) with synthetic, deterministic per-iteration
// sequences generated once in Go and fed identically to both the Go
// components and the C++ oracle shim.

// lcg is the same minimal PRNG the other slices use.
type lcg uint32

func (l *lcg) next() float32 {
	*l = *l*1664525 + 1013904223
	return float32(int32(*l)) / float32(math.MaxInt32)
}

// nextPositive returns a non-negative, power-spectrum-shaped value in
// roughly [0, scale).
func (l *lcg) nextPositive(scale float32) float32 {
	v := l.next()
	if v < 0 {
		v = -v
	}
	return v * scale
}

func requireSliceEqual(t *testing.T, iter int, name string, got, want []float32) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("iter %d: %s length mismatch: go %d, c %d", iter, name, len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("iter %d: %s[%d] mismatch: go %v, c %v", iter, name, i, got[i], want[i])
		}
	}
}

func requireEqualF32(t *testing.T, iter int, name string, got, want float32) {
	t.Helper()
	if got != want {
		t.Fatalf("iter %d: %s mismatch: go %v, c %v", iter, name, got, want)
	}
}

func requireEqualBool(t *testing.T, iter int, name string, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("iter %d: %s mismatch: go %v, c %v", iter, name, got, want)
	}
}

// TestSuppressorParity exercises ComfortNoiseGenerator/SuppressionGain/
// SuppressionFilter across 1200 iterations (long enough to cross CNG's
// N2_initial retirement at n2Counter==1000) of varying synthetic
// content: generic uncorrelated spectra, silence, echo-dominant,
// nearend-dominant (double-talk-like), saturation-like large
// magnitudes, and toggling clock-drift/saturated-capture flags.
func TestSuppressorParity(t *testing.T) {
	const numIterations = 1200
	const spectrumLen = aec3.FFTLengthBy2Plus1
	const blockSize = aec3.BlockSize

	config := config.DefaultConfig()
	goCNG := aec3.NewComfortNoiseGenerator(config, 1)
	goAnalyzer := aec3.NewRenderSignalAnalyzer(config)
	goAecState := aec3.NewAecState(config, 1)
	goGain := aec3.NewSuppressionGain(config, 1)
	goFilter := aec3.NewSuppressionFilter(16000, 1)

	cSuppressor := newSuppressorC()
	defer cSuppressor.close()

	renderRng := lcg(0xA5A5)
	y2Rng := lcg(0xC0FFEE)
	nearendRng := lcg(0xBEEF)
	echoRng := lcg(0xFACE)
	residualRng := lcg(0x1234)
	residualUnboundedRng := lcg(0x5678)
	eRng := lcg(0x9ABC)

	for iter := 0; iter < numIterations; iter++ {
		// Segment selection: mix silence, generic, echo-dominant,
		// nearend-dominant (double talk) and saturation-like bursts.
		phase := iter % 400
		silence := phase >= 0 && phase < 40
		echoDominant := phase >= 100 && phase < 160
		nearendDominant := phase >= 200 && phase < 260
		saturationBurst := phase >= 300 && phase < 320

		render := make([]float32, blockSize)
		for i := range render {
			if silence {
				render[i] = 0
			} else {
				render[i] = renderRng.next() * 8000
			}
		}

		scale := float32(1e6)
		if saturationBurst {
			scale = 1e10
		}

		Y2 := make([]float32, spectrumLen)
		nearend := make([]float32, spectrumLen)
		echo := make([]float32, spectrumLen)
		residual := make([]float32, spectrumLen)
		residualUnbounded := make([]float32, spectrumLen)
		eLowestRe := make([]float32, spectrumLen)
		eLowestIm := make([]float32, spectrumLen)

		for k := 0; k < spectrumLen; k++ {
			if silence {
				Y2[k] = 0
				nearend[k] = 0
				echo[k] = 0
				residual[k] = 0
				residualUnbounded[k] = 0
				eLowestRe[k] = 0
				eLowestIm[k] = 0
				continue
			}
			Y2[k] = y2Rng.nextPositive(scale)
			switch {
			case echoDominant:
				nearend[k] = nearendRng.nextPositive(scale * 0.01)
				echo[k] = echoRng.nextPositive(scale)
			case nearendDominant:
				nearend[k] = nearendRng.nextPositive(scale)
				echo[k] = echoRng.nextPositive(scale * 0.01)
			default:
				nearend[k] = nearendRng.nextPositive(scale)
				echo[k] = echoRng.nextPositive(scale)
			}
			residual[k] = residualRng.nextPositive(scale)
			residualUnbounded[k] = residualUnboundedRng.nextPositive(scale * 1.5)
			eLowestRe[k] = eRng.next() * 4000
			eLowestIm[k] = eRng.next() * 4000
		}

		saturatedCapture := saturationBurst
		clockDrift := (iter/50)%2 == 0

		// --- Go side ---
		var Y2Arr, nearendArr, echoArr, residualArr, residualUnboundedArr [spectrumLen]float32
		copy(Y2Arr[:], Y2)
		copy(nearendArr[:], nearend)
		copy(echoArr[:], echo)
		copy(residualArr[:], residual)
		copy(residualUnboundedArr[:], residualUnbounded)

		lowerNoise := make([]aec3.FFTData, 1)
		upperNoise := make([]aec3.FFTData, 1)
		goCNG.Compute(saturatedCapture, [][spectrumLen]float32{Y2Arr}, lowerNoise, upperNoise)

		renderBlock := aec3.NewBlock(1, 1)
		copy(renderBlock.View(0, 0), render)

		var highBandsGain float32
		var G [spectrumLen]float32
		goGain.GetGain([][spectrumLen]float32{nearendArr}, [][spectrumLen]float32{echoArr},
			[][spectrumLen]float32{residualArr}, [][spectrumLen]float32{residualUnboundedArr},
			goCNG.NoiseSpectrum(), goAnalyzer, goAecState, renderBlock, clockDrift,
			&highBandsGain, &G)
		isDominant := goGain.IsDominantNearend()

		var eLowest aec3.FFTData
		copy(eLowest.Re[:], eLowestRe)
		copy(eLowest.Im[:], eLowestIm)
		eLowestVec := []aec3.FFTData{eLowest}

		eBlock := aec3.NewBlock(1, 1)
		goFilter.ApplyGain(lowerNoise, upperNoise, G, highBandsGain, eLowestVec, eBlock)

		// --- C++ side ---
		c := cSuppressor.step(render, Y2, saturatedCapture, nearend, echo, residual,
			residualUnbounded, clockDrift, eLowestRe, eLowestIm)

		requireSliceEqual(t, iter, "CNG.lowerNoise.Re", lowerNoise[0].Re[:], c.lowerNoiseRe[:])
		requireSliceEqual(t, iter, "CNG.lowerNoise.Im", lowerNoise[0].Im[:], c.lowerNoiseIm[:])
		requireSliceEqual(t, iter, "CNG.upperNoise.Re", upperNoise[0].Re[:], c.upperNoiseRe[:])
		requireSliceEqual(t, iter, "CNG.upperNoise.Im", upperNoise[0].Im[:], c.upperNoiseIm[:])
		requireSliceEqual(t, iter, "CNG.NoiseSpectrum", goCNG.NoiseSpectrum()[0][:], c.noiseSpectrum[:])
		requireSliceEqual(t, iter, "SuppressionGain.G", G[:], c.gain[:])
		requireEqualF32(t, iter, "SuppressionGain.highBandsGain", highBandsGain, c.highBandsGain)
		requireEqualBool(t, iter, "SuppressionGain.IsDominantNearend", isDominant, c.isDominantNearend)
		requireSliceEqual(t, iter, "SuppressionFilter.output", eBlock.View(0, 0), c.filterOut[:])
	}
}
