// This file ports modules/audio_processing/aec3/
// comfort_noise_generator.{h,cc}: ComfortNoiseGenerator, which
// estimates the background noise spectrum and generates a matching
// comfort-noise FFT signal using a fixed-seed LCG.
//
// Dropped per this port's conventions:
//   - The Aec3Optimization parameter and the SSE2
//     EstimateComfortNoise_SSE2 variant (see suppression_gain.go's top
//     comment; aec3::VectorMath(optimization).Sqrt is inlined as a
//     scalar sqrtf loop).
package aec3

import (
	"math"

	"github.com/daniel-sullivan/go-mediatoolkit/aec/config"
)

// kSqrt2Sin is sqrt(2) * sin(2*pi*i/32), i in [0,32). C:
// comfort_noise_generator.cc's (anonymous)'s kSqrt2Sin.
var kSqrt2Sin = [32]float32{
	+0.0000000, +0.2758994, +0.5411961, +0.7856950, +1.0000000,
	+1.1758756, +1.3065630, +1.3870398, +1.4142136, +1.3870398,
	+1.3065630, +1.1758756, +1.0000000, +0.7856950, +0.5411961,
	+0.2758994, +0.0000000, -0.2758994, -0.5411961, -0.7856950,
	-1.0000000, -1.1758756, -1.3065630, -1.3870398, -1.4142136,
	-1.3870398, -1.3065630, -1.1758756, -1.0000000, -0.7856950,
	-0.5411961, -0.2758994,
}

// getNoiseFloorFactor computes the noise floor value that matches a
// WGN input of noiseFloorDbfs. C: (anonymous)'s GetNoiseFloorFactor.
func getNoiseFloorFactor(noiseFloorDbfs float32) float32 {
	// kdBfsNormalization = 20.f*log10(32768.f).
	const kdBfsNormalization = 90.30899869919436
	return mul32(64, float32(math.Pow(10, float64(mul32(add32(kdBfsNormalization, noiseFloorDbfs), 0.1)))))
}

// generateComfortNoise fills lowerBandNoise/upperBandNoise from the
// noise power spectrum N2, advancing seed via a fixed-seed LCG. C:
// (anonymous)'s GenerateComfortNoise.
func generateComfortNoise(N2 [FFTLengthBy2Plus1]float32, seed *uint32, lowerBandNoise, upperBandNoise *FFTData) {
	nLow := lowerBandNoise
	nHigh := upperBandNoise

	// Compute square root spectrum.
	var N [FFTLengthBy2Plus1]float32
	N = N2
	for i := range N {
		N[i] = float32(math.Sqrt(float64(N[i])))
	}

	// Compute the noise level for the upper bands.
	const kOneByNumBands = 1.0 / (FFTLengthBy2Plus1/2 + 1)
	const kFftLengthBy2Plus1By2 = FFTLengthBy2Plus1 / 2
	var sum float32
	for _, v := range N[kFftLengthBy2Plus1By2:] {
		sum = add32(sum, v)
	}
	highBandNoiseLevel := mul32(sum, kOneByNumBands)

	// The analysis and synthesis windowing cause loss of power when
	// cross-fading the noise where frames are completely uncorrelated
	// (generated with random phase), hence the factor sqrt(2). This is
	// not the case for the speech signal where the input is
	// overlapping (strong correlation).
	nLow.Re[0] = 0
	nLow.Re[FFTLengthBy2] = 0
	nHigh.Re[0] = 0
	nHigh.Re[FFTLengthBy2] = 0
	for k := 1; k < FFTLengthBy2; k++ {
		const kIndexMask = 32 - 1
		// Generate a random 31-bit integer.
		*seed = (*seed*69069 + 1) & (0x80000000 - 1)
		// Convert to a 5-bit index.
		i := *seed >> 26

		// y = sqrt(2) * sin(a)
		x := kSqrt2Sin[i]
		// x = sqrt(2) * cos(a) = sqrt(2) * sin(a + pi/2)
		y := kSqrt2Sin[(i+8)&kIndexMask]

		// Form low-frequency noise via spectral shaping.
		nLow.Re[k] = mul32(N[k], x)
		nLow.Im[k] = mul32(N[k], y)

		// Form the high-frequency noise via simple levelling.
		nHigh.Re[k] = mul32(highBandNoiseLevel, x)
		nHigh.Im[k] = mul32(highBandNoiseLevel, y)
	}
}

// ComfortNoiseGenerator estimates the background noise spectrum and
// generates comfort noise matching it. C:
// comfort_noise_generator.{h,cc}'s ComfortNoiseGenerator.
type ComfortNoiseGenerator struct {
	seed               uint32
	numCaptureChannels int
	noiseFloor         float32
	n2Initial          [][FFTLengthBy2Plus1]float32 // nil once retired, mirrors unique_ptr reset
	y2Smoothed         [][FFTLengthBy2Plus1]float32
	n2                 [][FFTLengthBy2Plus1]float32
	n2Counter          int
}

// NewComfortNoiseGenerator mirrors ComfortNoiseGenerator's C++
// constructor.
func NewComfortNoiseGenerator(config config.Config, numCaptureChannels int) *ComfortNoiseGenerator {
	c := &ComfortNoiseGenerator{
		seed:               42,
		numCaptureChannels: numCaptureChannels,
		noiseFloor:         getNoiseFloorFactor(config.ComfortNoise.NoiseFloorDbfs),
		n2Initial:          make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		y2Smoothed:         make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		n2:                 make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
	}
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := range c.n2[ch] {
			c.n2[ch][k] = 1.0e6
		}
	}
	return c
}

// NoiseSpectrum returns the estimate of the background noise
// spectrum. C: ComfortNoiseGenerator::NoiseSpectrum.
func (c *ComfortNoiseGenerator) NoiseSpectrum() [][FFTLengthBy2Plus1]float32 {
	return c.n2
}

// Compute computes the comfort noise. C:
// ComfortNoiseGenerator::Compute.
func (c *ComfortNoiseGenerator) Compute(saturatedCapture bool, captureSpectrum [][FFTLengthBy2Plus1]float32, lowerBandNoise, upperBandNoise []FFTData) {
	Y2 := captureSpectrum

	if !saturatedCapture {
		// Smooth Y2.
		for ch := 0; ch < c.numCaptureChannels; ch++ {
			for k := range c.y2Smoothed[ch] {
				a := c.y2Smoothed[ch][k]
				b := Y2[ch][k]
				c.y2Smoothed[ch][k] = mla(a, 0.1, sub32(b, a))
			}
		}

		if c.n2Counter > 50 {
			// Update N2 from Y2_smoothed.
			for ch := 0; ch < c.numCaptureChannels; ch++ {
				for k := range c.n2[ch] {
					a := c.n2[ch][k]
					b := c.y2Smoothed[ch][k]
					if b < a {
						c.n2[ch][k] = mul32(muladd(0.9, b, 0.1, a), 1.0002)
					} else {
						c.n2[ch][k] = mul32(a, 1.0002)
					}
				}
			}
		}

		if c.n2Initial != nil {
			c.n2Counter++
			if c.n2Counter == 1000 {
				c.n2Initial = nil
			} else {
				// Compute the N2_initial from N2.
				for ch := 0; ch < c.numCaptureChannels; ch++ {
					for k := range c.n2Initial[ch] {
						a := c.n2[ch][k]
						b := c.n2Initial[ch][k]
						if a > b {
							c.n2Initial[ch][k] = mla(b, 0.001, sub32(a, b))
						} else {
							c.n2Initial[ch][k] = a
						}
					}
				}
			}
		}

		for ch := 0; ch < c.numCaptureChannels; ch++ {
			for k := range c.n2[ch] {
				if c.n2[ch][k] < c.noiseFloor {
					c.n2[ch][k] = c.noiseFloor
				}
			}
			if c.n2Initial != nil {
				for k := range c.n2Initial[ch] {
					if c.n2Initial[ch][k] < c.noiseFloor {
						c.n2Initial[ch][k] = c.noiseFloor
					}
				}
			}
		}
	}

	// Choose N2 estimate to use.
	N2 := c.n2
	if c.n2Initial != nil {
		N2 = c.n2Initial
	}

	for ch := 0; ch < c.numCaptureChannels; ch++ {
		generateComfortNoise(N2[ch], &c.seed, &lowerBandNoise[ch], &upperBandNoise[ch])
	}
}
