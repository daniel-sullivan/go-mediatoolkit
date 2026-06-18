// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// Noise detection, ported 1:1 from libAACenc/src/noisedet.cpp.
// FDKaacEnc_noiseDetect computes, per scalefactor band, the noiseFuzzyMeasure
// the PNS detect chain (aacenc_pns.go) thresholds: it combines a power-
// distribution test (the band is split into four quarters and the min/max
// quarter energies are compared, scaled by the per-band PSD curve) with a
// psychoacoustic-tonality test (the chaosmeasure tonality vs the reference
// tonality). Both feed FDKaacEnc_fuzzyIsSmaller, whose fuzzy outputs are
// min-combined into noiseFuzzyMeasure.
//
// fdk-aac encode is FIXED-POINT: every value is an int32 FIXP_DBL / int16
// FIXP_SGL Q-format quantity. The kernel is pure integer fixed-point (fMult
// int64 products, arithmetic shifts, leading-bit counts) — bit-identical
// regardless of vectorization — so it carries only the aacfdk fence (no
// aac_strict FP split).

// fixMinSGL is the FIXP_SGL overload of the fixMin template (common_fix.h:
// fixMin(a,b) == (a < b) ? a : b), used by noiseDetect to min-combine fuzzy
// values.
func fixMinSGL(a, b int16) int16 {
	if a < b {
		return a
	}
	return b
}

// fuzzyIsSmaller is the 1:1 port of FDKaacEnc_fuzzyIsSmaller
// (noisedet.cpp:119-127): the fuzzy value for "testVal is smaller than refVal".
// Returns FL2FXCONST_SGL(0.0) or MAXVAL_SGL.
//
//	if (refVal <= 0)                                     return 0;
//	else if (testVal >= fMult((hiLim>>1)+(loLim>>1), refVal)) return 0;
//	else                                                 return MAXVAL_SGL;
func fuzzyIsSmaller(testVal, refVal, loLim, hiLim int32) int16 {
	if refVal <= 0 {
		return 0
	} else if testVal >= fMultDD((hiLim>>1)+(loLim>>1), refVal) {
		return 0
	}
	return maxvalSGL
}

// noiseDetect is the 1:1 port of FDKaacEnc_noiseDetect (noisedet.cpp:150-235):
// detect tonal sfb's via the power-distribution and psychoacoustic-tonality
// tests, writing noiseFuzzyMeasure[sfb]. sfbMaxScaleSpec carries the per-band
// headroom; np supplies the tuning thresholds/flags; sfbtonality is the
// chaosmeasure tonality.
func noiseDetect(mdctSpectrum []int32, sfbMaxScaleSpec []int32, sfbActive int,
	sfbOffset []int32, noiseFuzzyMeasure []int16, np *NoiseParams, sfbtonality []int16) {

	// Start noise detection for each band based on a number of checks.
	for sfb := 0; sfb < sfbActive; sfb++ {
		fuzzyTotal := maxvalSGL
		sfbWidth := int(sfbOffset[sfb+1]) - int(sfbOffset[sfb])

		// Reset output for lower bands or too small bands.
		if sfb < int(np.StartSfb) || sfbWidth < np.MinSfbWidth {
			noiseFuzzyMeasure[sfb] = 0
			continue
		}

		if (np.DetectionAlgorithmFlags&usePowerDistribution) != 0 &&
			fuzzyTotal > fl2fxconstSGL(0.5) {
			// max sfbWidth = 96/4 ; 2^5=32 => 5/2 = 3 (spc*spc)
			leadingBits := fixMax(0, int(sfbMaxScaleSpec[sfb])-3)

			// check power distribution in four regions
			var fhelp1, fhelp2, fhelp3, fhelp4 int32
			k := sfbWidth >> 2 // width of a quarter band

			for i := int(sfbOffset[sfb]); i < int(sfbOffset[sfb])+k; i++ {
				fhelp1 = fPow2AddDiv2(fhelp1, mdctSpectrum[i]<<uint(leadingBits))
				fhelp2 = fPow2AddDiv2(fhelp2, mdctSpectrum[i+k]<<uint(leadingBits))
				fhelp3 = fPow2AddDiv2(fhelp3, mdctSpectrum[i+2*k]<<uint(leadingBits))
				fhelp4 = fPow2AddDiv2(fhelp4, mdctSpectrum[i+3*k]<<uint(leadingBits))
			}

			// get max into maxVal:
			maxVal := fixMaxDBL(fhelp1, fhelp2)
			maxVal = fixMaxDBL(maxVal, fhelp3)
			maxVal = fixMaxDBL(maxVal, fhelp4)

			// get min into minVal:
			minVal := fixMinDBL(fhelp1, fhelp2)
			minVal = fixMinDBL(minVal, fhelp3)
			minVal = fixMinDBL(minVal, fhelp4)

			// Normalize min and max Val.
			leadingBits = int(fNorm(maxVal)) // CountLeadingBits == fixnorm_D
			testVal := maxVal << uint(leadingBits)
			refVal := minVal << uint(leadingBits)

			// calculate fuzzy value for power distribution
			testVal = fMultDiv2DS(testVal, np.PowDistPSDcurve[sfb])

			fuzzy := fuzzyIsSmaller(
				testVal,              // 1/2 * maxValue * PSDcurve
				refVal,               // 1   * minValue
				fl2fxconstDBL(0.495), // 1/2 * loLim (0.99f/2)
				fl2fxconstDBL(0.505)) // 1/2 * hiLim (1.01f/2)

			fuzzyTotal = fixMinSGL(fuzzyTotal, fuzzy)
		}

		if (np.DetectionAlgorithmFlags&usePsychTonality) != 0 &&
			fuzzyTotal > fl2fxconstSGL(0.5) {
			// Detection with tonality-value of psych. acoustic (here: 1 is tonal!)
			testVal := (int32(sfbtonality[sfb]) << 16) >> 1 // FX_SGL2FX_DBL then >>1
			refVal := np.RefTonality

			fuzzy := fuzzyIsSmaller(
				testVal, refVal,
				fl2fxconstDBL(0.45), // 1/2 * loLim (0.9f/2)
				fl2fxconstDBL(0.55)) // 1/2 * hiLim (1.1f/2)

			fuzzyTotal = fixMinSGL(fuzzyTotal, fuzzy)
		}

		// Output of final result.
		noiseFuzzyMeasure[sfb] = fuzzyTotal
	}
}
