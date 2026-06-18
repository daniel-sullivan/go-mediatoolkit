// SPDX-License-Identifier: FDK-AAC
//go:build aacfdk

package nativeaac

// FDK-AAC-derived. See libfdk/COPYING. Fenced behind the `aacfdk` build tag.
//
// CTns_Apply driver, ported 1:1 from CTns_Apply (libAACdec/src/aacdec_tns.cpp:274).
// It dequantizes each filter's reflection coefficients (tnsCoeff3/4), maps the
// TNS band range to spectral-line offsets, and runs the all-pole synthesis
// lattice (clpcSynthesisLatticeDBL) over each window's spectrum in place.
// All fixed-point integer; bit-identical regardless of build.

// cTnsApply ports CTns_Apply (aacdec_tns.cpp:274). pSpectralCoefficient is the
// flat per-window MDCT buffer (stride granuleLength); igfActive is 0 for AAC-LC;
// flags is the AC_* bitmask (0 for AAC-LC).
func cTnsApply(pTnsData *CTnsData, pIcsInfo *cIcsInfo, pSpectralCoefficient []int32,
	sri *samplingRateInfo, granuleLength int, nbands uint8, igfActive int, flags uint32) {
	if pTnsData.Active == 0 {
		return
	}

	var coeff [tnsMaximumOrder]int32

	startWindow := 0
	winsPerFrame := getWindowsPerFrame(pIcsInfo)

	bandOffsets := getScaleFactorBandOffsets(pIcsInfo, sri)

	for window := startWindow; window < winsPerFrame; window++ {
		pSpectrum := pSpectralCoefficient[window*granuleLength:]

		for index := 0; index < int(pTnsData.NumberOfFilters[window]); index++ {
			filter := &pTnsData.Filter[window][index]

			if filter.Order > 0 {
				if filter.Resolution == 3 {
					for i := 0; i < int(filter.Order); i++ {
						coeff[i] = tnsCoeff3[filter.Coeff[i]+4]
					}
				} else {
					for i := 0; i < int(filter.Order); i++ {
						coeff[i] = tnsCoeff4[filter.Coeff[i]+8]
					}
				}

				var tnsMaxBands uint8
				switch granuleLength {
				case 480:
					tnsMaxBands = tnsMaxBandsTbl480[sri.samplingRateIndex]
				case 512:
					tnsMaxBands = tnsMaxBandsTbl512[sri.samplingRateIndex]
				default:
					tnsMaxBands = getMaximumTnsBands(pIcsInfo, int(sri.samplingRateIndex))
					if flags&(acUSAC|acRSVD50|acRSV603DA) != 0 && sri.samplingRateIndex > 5 {
						tnsMaxBands++
					}
				}

				start := min(min(int(filter.StartBand), int(tnsMaxBands)), int(nbands))
				start = int(bandOffsets[start])

				var stop int
				if igfActive != 0 {
					stop = min(int(filter.StopBand), int(nbands))
				} else {
					stop = min(min(int(filter.StopBand), int(tnsMaxBands)), int(nbands))
				}
				stop = int(bandOffsets[stop])

				size := stop - start

				if size != 0 {
					var state [tnsMaximumOrder]int32
					clpcSynthesisLatticeDBL(pSpectrum[start:], size, 0, 0,
						int(filter.Direction), coeff[:], int(filter.Order), state[:])
				}
			}
		}
	}
}
