// This file ports modules/audio_processing/aec3/
// suppression_filter.{h,cc}: SuppressionFilter, which applies the
// suppressor gain (and adds comfort noise) via an overlap-add
// analysis/synthesis filterbank.
//
// Dropped per this port's conventions:
//   - The Aec3Optimization parameter (see suppression_gain.go's top
//     comment; aec3::VectorMath(optimization_).Sqrt is inlined as a
//     scalar sqrtf loop).
//   - RTC_DCHECK assertions (debug-only, no numerical effect).
//
// Reused rather than redefined: kSqrtHanning128 (fft.go), matching
// this port's established sqrt(hanning(128)) table.
package aec3

import "math"

// SuppressionFilter applies the suppression gain (lower band,
// per-bin, plus a single scalar for the higher bands) and adds
// comfort noise, via an overlap-add analysis/synthesis filterbank. C:
// suppression_filter.{h,cc}'s SuppressionFilter.
type SuppressionFilter struct {
	sampleRateHz       int
	numCaptureChannels int
	fft                *Aec3FFT
	eOutputOld         [][][FFTLengthBy2]float32 // [band][channel][kFftLengthBy2]
}

// NewSuppressionFilter mirrors SuppressionFilter's C++ constructor.
func NewSuppressionFilter(sampleRateHz, numCaptureChannels int) *SuppressionFilter {
	numBands := NumBandsForRate(sampleRateHz)
	eOutputOld := make([][][FFTLengthBy2]float32, numBands)
	for b := range eOutputOld {
		eOutputOld[b] = make([][FFTLengthBy2]float32, numCaptureChannels)
	}
	return &SuppressionFilter{
		sampleRateHz:       sampleRateHz,
		numCaptureChannels: numCaptureChannels,
		fft:                NewAec3FFT(),
		eOutputOld:         eOutputOld,
	}
}

// ApplyGain applies the suppression gain, adds comfort noise, and
// updates e (in place) via the overlap-add filterbank. C:
// SuppressionFilter::ApplyGain.
func (s *SuppressionFilter) ApplyGain(comfortNoise, comfortNoiseHighBand []FFTData, suppressionGain [FFTLengthBy2Plus1]float32, highBandsGain float32, eLowestBand []FFTData, e *Block) {
	// Comfort noise gain is sqrt(1-g^2), where g is the suppression
	// gain.
	var noiseGain [FFTLengthBy2Plus1]float32
	for i := 0; i < FFTLengthBy2Plus1; i++ {
		g := suppressionGain[i]
		noiseGain[i] = sub32(1, mul32(g, g))
	}
	for i := range noiseGain {
		noiseGain[i] = float32(math.Sqrt(float64(noiseGain[i])))
	}

	highBandsNoiseScaling := mul32(0.4, float32(math.Sqrt(float64(sub32(1, mul32(highBandsGain, highBandsGain))))))

	for ch := 0; ch < s.numCaptureChannels; ch++ {
		var E FFTData

		// Analysis filterbank.
		E.Assign(&eLowestBand[ch])

		for i := 0; i < FFTLengthBy2Plus1; i++ {
			// Apply suppression gains.
			g := suppressionGain[i]
			eReal := mul32(E.Re[i], g)
			eImag := mul32(E.Im[i], g)

			// Scale and add the comfort noise.
			E.Re[i] = mla(eReal, noiseGain[i], comfortNoise[ch].Re[i])
			E.Im[i] = mla(eImag, noiseGain[i], comfortNoise[ch].Im[i])
		}

		// Synthesis filterbank.
		var eExtended [FFTLength]float32
		const kIfftNormalization = 2.0 / FFTLength
		s.fft.Ifft(&E, &eExtended)

		e0 := e.View(0, ch)
		e0Old := &s.eOutputOld[0][ch]

		// Window and add the first half of e_extended with the second
		// half of e_extended from the previous block.
		for i := 0; i < FFTLengthBy2; i++ {
			e0i := mul32(e0Old[i], kSqrtHanning128[FFTLengthBy2+i])
			e0i = mla(e0i, eExtended[i], kSqrtHanning128[i])
			e0[i] = mul32(e0i, kIfftNormalization)
		}

		// The second half of e_extended is stored for the succeeding
		// frame.
		copy(s.eOutputOld[0][ch][:], eExtended[FFTLengthBy2:FFTLength])

		// Apply suppression gain to upper bands.
		for b := 1; b < e.NumBands(); b++ {
			eBand := e.View(b, ch)
			for i := 0; i < FFTLengthBy2; i++ {
				eBand[i] = mul32(eBand[i], highBandsGain)
			}
		}

		// Add comfort noise to band 1.
		if e.NumBands() > 1 {
			E.Assign(&comfortNoiseHighBand[ch])
			var timeDomainHighBandNoise [FFTLength]float32
			s.fft.Ifft(&E, &timeDomainHighBandNoise)

			e1 := e.View(1, ch)
			gain := mul32(highBandsNoiseScaling, kIfftNormalization)
			for i := 0; i < FFTLengthBy2; i++ {
				e1[i] = mla(e1[i], timeDomainHighBandNoise[i], gain)
			}
		}

		// Delay upper bands to match the delay of the filter bank.
		for b := 1; b < e.NumBands(); b++ {
			eBand := e.View(b, ch)
			eBandOld := &s.eOutputOld[b][ch]
			for i := 0; i < FFTLengthBy2; i++ {
				eBand[i], eBandOld[i] = eBandOld[i], eBand[i]
			}
		}

		// Clamp output of all bands.
		for b := 0; b < e.NumBands(); b++ {
			eBand := e.View(b, ch)
			for i := 0; i < FFTLengthBy2; i++ {
				eBand[i] = clamp32(eBand[i], -32768, 32767)
			}
		}
	}
}
