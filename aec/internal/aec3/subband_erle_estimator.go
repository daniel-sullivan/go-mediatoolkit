// This file ports modules/audio_processing/aec3/
// subband_erle_estimator.{h,cc}: SubbandErleEstimator, which estimates
// the echo return loss enhancement per frequency subband.
//
// Dropped per this port's conventions: the
// "WebRTC-Aec3MinErleDuringOnsetsKillSwitch" field-trial kill switch.
// EnableMinErleDuringOnsets() resolves to true (no field-trial system,
// so the "!IsEnabled(kill switch)" check is always true), which means
// UpdateBands's inner branch that would lower erle_during_onsets_ on a
// render onset is unreachable in upstream's out-of-the-box behavior
// and is omitted below, matching this port's precedent (see
// subtractor.go) for hardcoding always-resolved field-trial branches
// rather than translating dead code.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

const (
	subbandErleX2BandEnergyThreshold   = 44015068.0
	subbandErleBlocksToHoldErle        = 100
	subbandErleBlocksForOnsetDetection = subbandErleBlocksToHoldErle + 150
	subbandErlePointsToAccumulate      = 6
)

// setMaxErleBands builds the per-band max ERLE array: low bands
// (below FFTLengthBy2/2) get maxErleL, high bands get maxErleH. C:
// (anonymous namespace) SetMaxErleBands.
func setMaxErleBands(maxErleL, maxErleH float32) [FFTLengthBy2Plus1]float32 {
	var maxErle [FFTLengthBy2Plus1]float32
	for k := 0; k < FFTLengthBy2/2; k++ {
		maxErle[k] = maxErleL
	}
	for k := FFTLengthBy2 / 2; k < FFTLengthBy2Plus1; k++ {
		maxErle[k] = maxErleH
	}
	return maxErle
}

// updateErleBand updates *erle towards newErle with a render/decrease
// asymmetric smoothing factor, clamped to [minErle, maxErle]. C:
// SubbandErleEstimator::UpdateBands's update_erle_band lambda.
func updateErleBand(erle *float32, newErle float32, lowRenderEnergy bool, minErle, maxErle float32) {
	alpha := float32(0.05)
	if newErle < *erle {
		if lowRenderEnergy {
			alpha = 0.0
		} else {
			alpha = 0.1
		}
	}
	*erle = clamp32(mla(*erle, alpha, sub32(newErle, *erle)), minErle, maxErle)
}

// subbandAccumulatedSpectra accumulates render/capture/residual
// spectra between subband ERLE re-estimations. C:
// SubbandErleEstimator::AccumulatedSpectra.
type subbandAccumulatedSpectra struct {
	y2              [][FFTLengthBy2Plus1]float32
	e2              [][FFTLengthBy2Plus1]float32
	lowRenderEnergy [][FFTLengthBy2Plus1]bool
	numPoints       []int
}

func newSubbandAccumulatedSpectra(numCaptureChannels int) *subbandAccumulatedSpectra {
	return &subbandAccumulatedSpectra{
		y2:              make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		e2:              make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		lowRenderEnergy: make([][FFTLengthBy2Plus1]bool, numCaptureChannels),
		numPoints:       make([]int, numCaptureChannels),
	}
}

// SubbandErleEstimator estimates the echo return loss enhancement for
// each frequency subband. C: subband_erle_estimator.{h,cc}'s
// SubbandErleEstimator.
type SubbandErleEstimator struct {
	useOnsetDetection    bool
	minErle              float32
	maxErle              [FFTLengthBy2Plus1]float32
	accumSpectra         *subbandAccumulatedSpectra
	erle                 [][FFTLengthBy2Plus1]float32
	erleOnsetCompensated [][FFTLengthBy2Plus1]float32
	erleUnbounded        [][FFTLengthBy2Plus1]float32
	erleDuringOnsets     [][FFTLengthBy2Plus1]float32
	comingOnset          [][FFTLengthBy2Plus1]bool
	holdCounters         [][FFTLengthBy2Plus1]int
}

// NewSubbandErleEstimator constructs a SubbandErleEstimator. C:
// SubbandErleEstimator::SubbandErleEstimator.
func NewSubbandErleEstimator(config config.Config, numCaptureChannels int) *SubbandErleEstimator {
	e := &SubbandErleEstimator{
		useOnsetDetection:    config.Erle.OnsetDetection,
		minErle:              config.Erle.Min,
		maxErle:              setMaxErleBands(config.Erle.MaxL, config.Erle.MaxH),
		accumSpectra:         newSubbandAccumulatedSpectra(numCaptureChannels),
		erle:                 make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		erleOnsetCompensated: make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		erleUnbounded:        make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		erleDuringOnsets:     make([][FFTLengthBy2Plus1]float32, numCaptureChannels),
		comingOnset:          make([][FFTLengthBy2Plus1]bool, numCaptureChannels),
		holdCounters:         make([][FFTLengthBy2Plus1]int, numCaptureChannels),
	}
	e.Reset()
	return e
}

// Reset resets the ERLE estimator. C: SubbandErleEstimator::Reset.
func (e *SubbandErleEstimator) Reset() {
	numCaptureChannels := len(e.erle)
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := range e.erle[ch] {
			e.erle[ch][k] = e.minErle
			e.erleOnsetCompensated[ch][k] = e.minErle
			e.erleUnbounded[ch][k] = e.minErle
			e.erleDuringOnsets[ch][k] = e.minErle
			e.comingOnset[ch][k] = true
			e.holdCounters[ch][k] = 0
		}
	}
	e.resetAccumulatedSpectra()
}

// Update updates the ERLE estimate. X2 is the render power spectrum,
// Y2/E2 are the per-capture-channel microphone/residual power
// spectra. C: SubbandErleEstimator::Update.
func (e *SubbandErleEstimator) Update(x2 [FFTLengthBy2Plus1]float32, y2, e2 [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	e.updateAccumulatedSpectra(x2, y2, e2, convergedFilters)
	e.updateBands(convergedFilters)

	if e.useOnsetDetection {
		e.decreaseErlePerBandForLowRenderSignals()
	}

	numCaptureChannels := len(e.erle)
	for ch := 0; ch < numCaptureChannels; ch++ {
		erle := &e.erle[ch]
		erle[0] = erle[1]
		erle[FFTLengthBy2] = erle[FFTLengthBy2-1]

		erleOC := &e.erleOnsetCompensated[ch]
		erleOC[0] = erleOC[1]
		erleOC[FFTLengthBy2] = erleOC[FFTLengthBy2-1]

		erleU := &e.erleUnbounded[ch]
		erleU[0] = erleU[1]
		erleU[FFTLengthBy2] = erleU[FFTLengthBy2-1]
	}
}

// Erle returns the ERLE estimate: onset-compensated if requested and
// onset detection is enabled, otherwise the plain estimate. C:
// SubbandErleEstimator::Erle.
func (e *SubbandErleEstimator) Erle(onsetCompensated bool) [][FFTLengthBy2Plus1]float32 {
	if onsetCompensated && e.useOnsetDetection {
		return e.erleOnsetCompensated
	}
	return e.erle
}

// ErleUnbounded returns the non-capped ERLE estimate. C:
// SubbandErleEstimator::ErleUnbounded.
func (e *SubbandErleEstimator) ErleUnbounded() [][FFTLengthBy2Plus1]float32 {
	return e.erleUnbounded
}

// ErleDuringOnsets returns the ERLE estimate at onsets (only used for
// testing). C: SubbandErleEstimator::ErleDuringOnsets.
func (e *SubbandErleEstimator) ErleDuringOnsets() [][FFTLengthBy2Plus1]float32 {
	return e.erleDuringOnsets
}

// updateBands re-estimates the per-band ERLE from the accumulated
// spectra, once enough points have been accumulated. C:
// SubbandErleEstimator::UpdateBands.
func (e *SubbandErleEstimator) updateBands(convergedFilters []bool) {
	numCaptureChannels := len(e.accumSpectra.y2)
	for ch := 0; ch < numCaptureChannels; ch++ {
		// Note that the use of the converged_filter flag already
		// imposes a minimum on the ERLE that can be estimated, as that
		// flag would be false if the filter is performing poorly.
		if !convergedFilters[ch] {
			continue
		}

		if e.accumSpectra.numPoints[ch] != subbandErlePointsToAccumulate {
			continue
		}

		var newErle [FFTLengthBy2]float32
		var isErleUpdated [FFTLengthBy2]bool

		for k := 1; k < FFTLengthBy2; k++ {
			if e.accumSpectra.e2[ch][k] > 0 {
				newErle[k] = e.accumSpectra.y2[ch][k] / e.accumSpectra.e2[ch][k]
				isErleUpdated[k] = true
			}
		}

		if e.useOnsetDetection {
			for k := 1; k < FFTLengthBy2; k++ {
				if isErleUpdated[k] && !e.accumSpectra.lowRenderEnergy[ch][k] {
					if e.comingOnset[ch][k] {
						e.comingOnset[ch][k] = false
						// (dead branch omitted here -- see file doc comment)
					}
					e.holdCounters[ch][k] = subbandErleBlocksForOnsetDetection
				}
			}
		}

		for k := 1; k < FFTLengthBy2; k++ {
			if !isErleUpdated[k] {
				continue
			}
			lowRenderEnergy := e.accumSpectra.lowRenderEnergy[ch][k]
			updateErleBand(&e.erle[ch][k], newErle[k], lowRenderEnergy, e.minErle, e.maxErle[k])
			if e.useOnsetDetection {
				updateErleBand(&e.erleOnsetCompensated[ch][k], newErle[k], lowRenderEnergy, e.minErle, e.maxErle[k])
			}

			// Virtually unbounded ERLE.
			const unboundedErleMax float32 = 100000.0
			updateErleBand(&e.erleUnbounded[ch][k], newErle[k], lowRenderEnergy, e.minErle, unboundedErleMax)
		}
	}
}

// decreaseErlePerBandForLowRenderSignals decays
// erleOnsetCompensated back down towards erleDuringOnsets once the
// onset hold period has (mostly) elapsed. C:
// SubbandErleEstimator::DecreaseErlePerBandForLowRenderSignals.
func (e *SubbandErleEstimator) decreaseErlePerBandForLowRenderSignals() {
	numCaptureChannels := len(e.accumSpectra.y2)
	for ch := 0; ch < numCaptureChannels; ch++ {
		for k := 1; k < FFTLengthBy2; k++ {
			e.holdCounters[ch][k]--
			if e.holdCounters[ch][k] <= subbandErleBlocksForOnsetDetection-subbandErleBlocksToHoldErle {
				if e.erleOnsetCompensated[ch][k] > e.erleDuringOnsets[ch][k] {
					e.erleOnsetCompensated[ch][k] = max32(e.erleDuringOnsets[ch][k], mul32(0.97, e.erleOnsetCompensated[ch][k]))
				}
				if e.holdCounters[ch][k] <= 0 {
					e.comingOnset[ch][k] = true
					e.holdCounters[ch][k] = 0
				}
			}
		}
	}
}

// resetAccumulatedSpectra resets the accumulated spectra. C:
// SubbandErleEstimator::ResetAccumulatedSpectra.
func (e *SubbandErleEstimator) resetAccumulatedSpectra() {
	numCaptureChannels := len(e.erle)
	for ch := 0; ch < numCaptureChannels; ch++ {
		e.accumSpectra.y2[ch] = [FFTLengthBy2Plus1]float32{}
		e.accumSpectra.e2[ch] = [FFTLengthBy2Plus1]float32{}
		e.accumSpectra.numPoints[ch] = 0
		e.accumSpectra.lowRenderEnergy[ch] = [FFTLengthBy2Plus1]bool{}
	}
}

// updateAccumulatedSpectra accumulates Y2/E2 and marks low-render-
// energy bands since the last re-estimation. C:
// SubbandErleEstimator::UpdateAccumulatedSpectra.
func (e *SubbandErleEstimator) updateAccumulatedSpectra(x2 [FFTLengthBy2Plus1]float32, y2, e2 [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	st := e.accumSpectra
	numCaptureChannels := len(y2)
	for ch := 0; ch < numCaptureChannels; ch++ {
		// Note that the use of the converged_filter flag already
		// imposes a minimum on the ERLE that can be estimated, as that
		// flag would be false if the filter is performing poorly.
		if !convergedFilters[ch] {
			continue
		}

		if st.numPoints[ch] == subbandErlePointsToAccumulate {
			st.numPoints[ch] = 0
			st.y2[ch] = [FFTLengthBy2Plus1]float32{}
			st.e2[ch] = [FFTLengthBy2Plus1]float32{}
			st.lowRenderEnergy[ch] = [FFTLengthBy2Plus1]bool{}
		}

		for k := 0; k < FFTLengthBy2Plus1; k++ {
			st.y2[ch][k] = add32(st.y2[ch][k], y2[ch][k])
			st.e2[ch][k] = add32(st.e2[ch][k], e2[ch][k])
		}

		for k := 0; k < FFTLengthBy2Plus1; k++ {
			st.lowRenderEnergy[ch][k] = st.lowRenderEnergy[ch][k] || x2[k] < subbandErleX2BandEnergyThreshold
		}

		st.numPoints[ch]++
	}
}
