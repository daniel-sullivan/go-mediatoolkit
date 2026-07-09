// This file ports modules/audio_processing/aec3/
// signal_dependent_erle_estimator.{h,cc}: SignalDependentErleEstimator,
// which refines an average ERLE estimate based on how many linear
// filter sections (direct path vs. more reverberant sections)
// contribute to the current echo estimate.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// signalDependentErleSubbands is the number of subbands used by this
// estimator. C: SignalDependentErleEstimator::kSubbands.
const signalDependentErleSubbands = 6

const (
	signalDependentErleX2BandEnergyThreshold = 44015068.0
	signalDependentErleSmthConstantDecreases = 0.1
	signalDependentErleSmthConstantIncreases = signalDependentErleSmthConstantDecreases / 2.0
	signalDependentErleNumUpdateThr          = 50
)

// signalDependentErleBandBoundaries splits the FFTLengthBy2Plus1 bands
// into signalDependentErleSubbands subbands. C: (anonymous namespace)
// kBandBoundaries.
var signalDependentErleBandBoundaries = [signalDependentErleSubbands + 1]int{1, 8, 16, 24, 32, 48, FFTLengthBy2Plus1}

// formSubbandMap builds the per-band-to-subband index map. C:
// (anonymous namespace) FormSubbandMap.
func formSubbandMap() [FFTLengthBy2Plus1]int {
	var m [FFTLengthBy2Plus1]int
	subband := 1
	for k := 0; k < FFTLengthBy2Plus1; k++ {
		if k >= signalDependentErleBandBoundaries[subband] {
			subband++
		}
		m[k] = subband - 1
	}
	return m
}

// defineFilterSectionSizes defines the size in blocks of the sections
// that are used for dividing the linear filter. The sections are
// split in a non-linear manner so that lower sections that typically
// represent the direct path have a larger resolution than the higher
// sections which typically represent more reverberant acoustic paths.
// C: (anonymous namespace) DefineFilterSectionSizes.
func defineFilterSectionSizes(delayHeadroomBlocks, numBlocks, numSections int) []int {
	filterLengthBlocks := numBlocks - delayHeadroomBlocks
	sectionSizes := make([]int, numSections)
	remainingBlocks := filterLengthBlocks
	remainingSections := numSections
	estimatorSize := 2
	idx := 0
	for remainingSections > 1 && remainingBlocks > estimatorSize*remainingSections {
		sectionSizes[idx] = estimatorSize
		remainingBlocks -= estimatorSize
		remainingSections--
		estimatorSize *= 2
		idx++
	}

	lastGroupsSize := remainingBlocks / remainingSections
	for ; idx < numSections; idx++ {
		sectionSizes[idx] = lastGroupsSize
	}
	sectionSizes[numSections-1] += remainingBlocks - lastGroupsSize*remainingSections
	return sectionSizes
}

// setSectionsBoundaries forms the limits in blocks for each filter
// section. Those sections are used for analyzing the echo estimates
// and investigating which linear filter sections contribute most to
// the echo estimate energy. C: (anonymous namespace)
// SetSectionsBoundaries.
func setSectionsBoundaries(delayHeadroomBlocks, numBlocks, numSections int) []int {
	estimatorBoundariesBlocks := make([]int, numSections+1)
	if len(estimatorBoundariesBlocks) == 2 {
		estimatorBoundariesBlocks[0] = 0
		estimatorBoundariesBlocks[1] = numBlocks
		return estimatorBoundariesBlocks
	}

	sectionSizes := defineFilterSectionSizes(delayHeadroomBlocks, numBlocks, len(estimatorBoundariesBlocks)-1)

	idx := 0
	currentSizeBlock := 0
	estimatorBoundariesBlocks[0] = delayHeadroomBlocks
	for k := delayHeadroomBlocks; k < numBlocks; k++ {
		currentSizeBlock++
		if currentSizeBlock >= sectionSizes[idx] {
			idx++
			if idx == len(sectionSizes) {
				break
			}
			estimatorBoundariesBlocks[idx] = k + 1
			currentSizeBlock = 0
		}
	}
	estimatorBoundariesBlocks[len(sectionSizes)] = numBlocks
	return estimatorBoundariesBlocks
}

// setMaxErleSubbands builds the per-subband max ERLE array: subbands
// below limitSubbandL get maxErleL, the rest get maxErleH. C:
// (anonymous namespace) SetMaxErleSubbands.
func setMaxErleSubbands(maxErleL, maxErleH float32, limitSubbandL int) [signalDependentErleSubbands]float32 {
	var maxErle [signalDependentErleSubbands]float32
	for sb := 0; sb < limitSubbandL; sb++ {
		maxErle[sb] = maxErleL
	}
	for sb := limitSubbandL; sb < signalDependentErleSubbands; sb++ {
		maxErle[sb] = maxErleH
	}
	return maxErle
}

// signalDependentSubbandPowers sums powerSpectrum into
// signalDependentErleSubbands subband powers. C: (anonymous namespace,
// inside SignalDependentErleEstimator::UpdateCorrectionFactors)
// subband_powers.
func signalDependentSubbandPowers(powerSpectrum []float32) [signalDependentErleSubbands]float32 {
	var out [signalDependentErleSubbands]float32
	for subband := 0; subband < signalDependentErleSubbands; subband++ {
		var sum float32
		for _, v := range powerSpectrum[signalDependentErleBandBoundaries[subband]:signalDependentErleBandBoundaries[subband+1]] {
			sum += v
		}
		out[subband] = sum
	}
	return out
}

// SignalDependentErleEstimator estimates the dependency of the ERLE
// on the input signal. By looking at the input signal, an estimation
// on whether the current echo estimate is due to the direct path or
// to a more reverberant one is performed. Once that estimation is
// done, it is possible to refine the average ERLE that this
// estimator receives as an input. C:
// signal_dependent_erle_estimator.{h,cc}'s SignalDependentErleEstimator.
type SignalDependentErleEstimator struct {
	minErle                 float32
	numSections             int
	numBlocks               int
	delayHeadroomBlocks     int
	bandToSubband           [FFTLengthBy2Plus1]int
	maxErle                 [signalDependentErleSubbands]float32
	sectionBoundariesBlocks []int
	useOnsetDetection       bool

	erle                 [][FFTLengthBy2Plus1]float32
	erleOnsetCompensated [][FFTLengthBy2Plus1]float32
	s2SectionAccum       [][][FFTLengthBy2Plus1]float32           // [ch][section][band]
	erleEstimators       [][][signalDependentErleSubbands]float32 // [ch][section][subband]
	erleRef              [][signalDependentErleSubbands]float32
	correctionFactors    [][][signalDependentErleSubbands]float32 // [ch][section][subband]
	numUpdates           [][signalDependentErleSubbands]int
	nActiveSections      [][FFTLengthBy2Plus1]int
}

// NewSignalDependentErleEstimator constructs a
// SignalDependentErleEstimator. C:
// SignalDependentErleEstimator::SignalDependentErleEstimator.
func NewSignalDependentErleEstimator(config config.Config, numCaptureChannels int) *SignalDependentErleEstimator {
	delayHeadroomBlocks := config.Delay.DelayHeadroomSamples / BlockSize
	numBlocks := config.Filter.Refined.LengthBlocks
	numSections := config.Erle.NumSections
	bandToSubband := formSubbandMap()

	e := &SignalDependentErleEstimator{
		minErle:                 config.Erle.Min,
		numSections:             numSections,
		numBlocks:               numBlocks,
		delayHeadroomBlocks:     delayHeadroomBlocks,
		bandToSubband:           bandToSubband,
		maxErle:                 setMaxErleSubbands(config.Erle.MaxL, config.Erle.MaxH, bandToSubband[FFTLengthBy2/2]),
		sectionBoundariesBlocks: setSectionsBoundaries(delayHeadroomBlocks, numBlocks, numSections),
		useOnsetDetection:       config.Erle.OnsetDetection,
		erle:                    make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		erleOnsetCompensated:    make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		s2SectionAccum:          make([][][FFTLengthBy2Plus1]float32, numCaptureChannels),
		erleEstimators:          make([][][signalDependentErleSubbands]float32, numCaptureChannels),
		erleRef:                 make([][signalDependentErleSubbands]float32, numCaptureChannels),
		correctionFactors:       make([][][signalDependentErleSubbands]float32, numCaptureChannels),
		numUpdates:              make([][signalDependentErleSubbands]int, numCaptureChannels),
		nActiveSections:         make([][FFTLengthBy2Plus1]int, numCaptureChannels),
	}
	for ch := 0; ch < numCaptureChannels; ch++ {
		e.s2SectionAccum[ch] = make([][FFTLengthBy2Plus1]float32, numSections)
		e.erleEstimators[ch] = make([][signalDependentErleSubbands]float32, numSections)
		e.correctionFactors[ch] = make([][signalDependentErleSubbands]float32, numSections)
	}
	e.Reset()
	return e
}

// Reset resets the ERLE estimator. C: SignalDependentErleEstimator::Reset.
func (e *SignalDependentErleEstimator) Reset() {
	for ch := range e.erle {
		for k := range e.erle[ch] {
			e.erle[ch][k] = e.minErle
		}
		for k := range e.erleOnsetCompensated[ch] {
			e.erleOnsetCompensated[ch][k] = e.minErle
		}
		for section := range e.erleEstimators[ch] {
			for sb := range e.erleEstimators[ch][section] {
				e.erleEstimators[ch][section][sb] = e.minErle
			}
		}
		for sb := range e.erleRef[ch] {
			e.erleRef[ch][sb] = e.minErle
		}
		for section := range e.correctionFactors[ch] {
			for sb := range e.correctionFactors[ch][section] {
				e.correctionFactors[ch][section][sb] = 1.0
			}
		}
		for sb := range e.numUpdates[ch] {
			e.numUpdates[ch][sb] = 0
		}
		for k := range e.nActiveSections[ch] {
			e.nActiveSections[ch][k] = 0
		}
	}
}

// Erle returns the ERLE per frequency subband. C:
// SignalDependentErleEstimator::Erle.
func (e *SignalDependentErleEstimator) Erle(onsetCompensated bool) [][FFTLengthBy2Plus1]float32 {
	if onsetCompensated && e.useOnsetDetection {
		return e.erleOnsetCompensated
	}
	return e.erle
}

// Update updates the ERLE estimate by analyzing the current input
// signals. It takes the render buffer and the filter frequency
// response in order to do an estimation of the number of sections of
// the linear filter that are needed for getting the majority of the
// energy in the echo estimate. Based on that number of sections, it
// updates the ERLE estimation by introducing a correction factor to
// the ERLE (averageErle/averageErleOnsetCompensated) that is passed
// as an input. C: SignalDependentErleEstimator::Update.
func (e *SignalDependentErleEstimator) Update(renderBuffer *RenderBuffer, filterFrequencyResponses [][][FFTLengthBy2Plus1]float32, x2 [FFTLengthBy2Plus1]float32, y2, e2 [][FFTLengthBy2Plus1]float32, averageErle, averageErleOnsetCompensated [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	// Gets the number of filter sections that are needed for achieving
	// 90% of the power spectrum energy of the echo estimate.
	e.computeNumberOfActiveFilterSections(renderBuffer, filterFrequencyResponses)

	// Updates the correction factors that are used for correcting the
	// ERLE and adapt it to the particular characteristics of the input
	// signal.
	e.updateCorrectionFactors(x2, y2, e2, convergedFilters)

	// Applies the correction factor to the input ERLE for getting a
	// more refined ERLE estimation for the current input signal.
	for ch := range e.erle {
		for k := 0; k < FFTLengthBy2; k++ {
			subband := e.bandToSubband[k]
			correctionFactor := e.correctionFactors[ch][e.nActiveSections[ch][k]][subband]
			e.erle[ch][k] = clamp32(mul32(averageErle[ch][k], correctionFactor), e.minErle, e.maxErle[subband])
			if e.useOnsetDetection {
				e.erleOnsetCompensated[ch][k] = clamp32(mul32(averageErleOnsetCompensated[ch][k], correctionFactor), e.minErle, e.maxErle[subband])
			}
		}
	}
}

// computeNumberOfActiveFilterSections estimates for each band the
// smallest number of sections in the filter that together constitute
// 90% of the estimated echo energy. C:
// SignalDependentErleEstimator::ComputeNumberOfActiveFilterSections.
func (e *SignalDependentErleEstimator) computeNumberOfActiveFilterSections(renderBuffer *RenderBuffer, filterFrequencyResponses [][][FFTLengthBy2Plus1]float32) {
	// Computes an approximation of the power spectrum if the filter
	// would have been limited to a certain number of filter sections.
	e.computeEchoEstimatePerFilterSection(renderBuffer, filterFrequencyResponses)
	// For each band, computes the number of filter sections that are
	// needed for achieving the 90% energy in the echo estimate.
	e.computeActiveFilterSections()
}

// updateCorrectionFactors updates the correction factor per subband
// and active-section count, based on how well the subsection-limited
// ERLE tracks the full-filter ERLE. C:
// SignalDependentErleEstimator::UpdateCorrectionFactors.
func (e *SignalDependentErleEstimator) updateCorrectionFactors(x2 [FFTLengthBy2Plus1]float32, y2, e2 [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	for ch := 0; ch < len(convergedFilters); ch++ {
		if !convergedFilters[ch] {
			continue
		}

		x2Subbands := signalDependentSubbandPowers(x2[:])
		e2Subbands := signalDependentSubbandPowers(e2[ch][:])
		y2Subbands := signalDependentSubbandPowers(y2[ch][:])

		var idxSubbands [signalDependentErleSubbands]int
		for subband := 0; subband < signalDependentErleSubbands; subband++ {
			// When aggregating the number of active sections in the
			// filter for different bands we choose to take the minimum
			// of all of them. As an example, if for one of the bands it
			// is the direct path its refined contributor to the final
			// echo estimate, we consider the direct path is as well the
			// refined contributor for the subband that contains that
			// particular band. That aggregate number of sections will be
			// later used as the identifier of the ERLE estimator that
			// needs to be updated.
			lo := signalDependentErleBandBoundaries[subband]
			hi := signalDependentErleBandBoundaries[subband+1]
			minSection := e.nActiveSections[ch][lo]
			for k := lo + 1; k < hi; k++ {
				if e.nActiveSections[ch][k] < minSection {
					minSection = e.nActiveSections[ch][k]
				}
			}
			idxSubbands[subband] = minSection
		}

		var newErle [signalDependentErleSubbands]float32
		var isErleUpdated [signalDependentErleSubbands]bool
		for subband := 0; subband < signalDependentErleSubbands; subband++ {
			if x2Subbands[subband] > signalDependentErleX2BandEnergyThreshold && e2Subbands[subband] > 0 {
				newErle[subband] = y2Subbands[subband] / e2Subbands[subband]
				isErleUpdated[subband] = true
				e.numUpdates[ch][subband]++
			}
		}

		for subband := 0; subband < signalDependentErleSubbands; subband++ {
			if !isErleUpdated[subband] {
				continue
			}
			idx := idxSubbands[subband]
			alpha := float32(signalDependentErleSmthConstantDecreases)
			if newErle[subband] > e.erleEstimators[ch][idx][subband] {
				alpha = signalDependentErleSmthConstantIncreases
			}
			e.erleEstimators[ch][idx][subband] = clamp32(mla(e.erleEstimators[ch][idx][subband], alpha, sub32(newErle[subband], e.erleEstimators[ch][idx][subband])), e.minErle, e.maxErle[subband])
		}

		for subband := 0; subband < signalDependentErleSubbands; subband++ {
			if !isErleUpdated[subband] {
				continue
			}
			alpha := float32(signalDependentErleSmthConstantDecreases)
			if newErle[subband] > e.erleRef[ch][subband] {
				alpha = signalDependentErleSmthConstantIncreases
			}
			e.erleRef[ch][subband] = clamp32(mla(e.erleRef[ch][subband], alpha, sub32(newErle[subband], e.erleRef[ch][subband])), e.minErle, e.maxErle[subband])
		}

		for subband := 0; subband < signalDependentErleSubbands; subband++ {
			if isErleUpdated[subband] && e.numUpdates[ch][subband] > signalDependentErleNumUpdateThr {
				idx := idxSubbands[subband]
				// Computes the ratio between the ERLE that is updated
				// using all the points and the ERLE that is updated only
				// on signals that share the same number of active filter
				// sections.
				newCorrectionFactor := e.erleEstimators[ch][idx][subband] / e.erleRef[ch][subband]
				e.correctionFactors[ch][idx][subband] = mla(e.correctionFactors[ch][idx][subband], 0.1, sub32(newCorrectionFactor, e.correctionFactors[ch][idx][subband]))
			}
		}
	}
}

// computeEchoEstimatePerFilterSection computes, for each filter
// section, a cumulative approximation of the echo power spectrum
// contributed by that section and all earlier (more direct-path)
// sections. C:
// SignalDependentErleEstimator::ComputeEchoEstimatePerFilterSection.
func (e *SignalDependentErleEstimator) computeEchoEstimatePerFilterSection(renderBuffer *RenderBuffer, filterFrequencyResponses [][][FFTLengthBy2Plus1]float32) {
	spectrumRenderBuffer := renderBuffer.GetSpectrumBuffer()
	numRenderChannels := len(spectrumRenderBuffer.Buffer[0])
	numCaptureChannels := len(e.s2SectionAccum)
	oneByNumRenderChannels := float32(1.0) / float32(numRenderChannels)

	for captureCh := 0; captureCh < numCaptureChannels; captureCh++ {
		idxRender := renderBuffer.Position()
		idxRender = spectrumRenderBuffer.OffsetIndex(idxRender, e.sectionBoundariesBlocks[0])

		for section := 0; section < e.numSections; section++ {
			var x2Section [FFTLengthBy2Plus1]float32
			var h2Section [FFTLengthBy2Plus1]float32

			blockLimit := e.sectionBoundariesBlocks[section+1]
			if len(filterFrequencyResponses[captureCh]) < blockLimit {
				blockLimit = len(filterFrequencyResponses[captureCh])
			}
			for block := e.sectionBoundariesBlocks[section]; block < blockLimit; block++ {
				for renderCh := 0; renderCh < len(spectrumRenderBuffer.Buffer[idxRender]); renderCh++ {
					renderSpectrum := spectrumRenderBuffer.Buffer[idxRender][renderCh]
					for k := 0; k < FFTLengthBy2Plus1; k++ {
						x2Section[k] = mla(x2Section[k], renderSpectrum[k], oneByNumRenderChannels)
					}
				}
				h2Block := filterFrequencyResponses[captureCh][block]
				for k := 0; k < FFTLengthBy2Plus1; k++ {
					h2Section[k] = add32(h2Section[k], h2Block[k])
				}
				idxRender = spectrumRenderBuffer.IncIndex(idxRender)
			}

			for k := 0; k < FFTLengthBy2Plus1; k++ {
				e.s2SectionAccum[captureCh][section][k] = mul32(x2Section[k], h2Section[k])
			}
		}

		for section := 1; section < e.numSections; section++ {
			for k := 0; k < FFTLengthBy2Plus1; k++ {
				e.s2SectionAccum[captureCh][section][k] = add32(e.s2SectionAccum[captureCh][section][k], e.s2SectionAccum[captureCh][section-1][k])
			}
		}
	}
}

// computeActiveFilterSections finds, for each band, the smallest
// number of (leading) filter sections whose accumulated energy is at
// least 90% of the total accumulated energy across all sections. C:
// SignalDependentErleEstimator::ComputeActiveFilterSections.
func (e *SignalDependentErleEstimator) computeActiveFilterSections() {
	for ch := range e.nActiveSections {
		for k := range e.nActiveSections[ch] {
			e.nActiveSections[ch][k] = 0
		}
		for k := 0; k < FFTLengthBy2Plus1; k++ {
			section := e.numSections
			target := mul32(0.9, e.s2SectionAccum[ch][e.numSections-1][k])
			for section > 0 && e.s2SectionAccum[ch][section-1][k] >= target {
				section--
				e.nActiveSections[ch][k] = section
			}
		}
	}
}
