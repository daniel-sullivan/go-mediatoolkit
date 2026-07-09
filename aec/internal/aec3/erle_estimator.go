// This file ports modules/audio_processing/aec3/erle_estimator.{h,cc}:
// ErleEstimator, which estimates the echo return loss enhancement.
// One estimate is done per subband and another one is done using the
// aggregation of energy over all the subbands; optionally, a
// signal-dependent estimator refines the subband estimate further.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// ErleEstimator estimates the echo return loss enhancement. C:
// erle_estimator.{h,cc}'s ErleEstimator.
type ErleEstimator struct {
	startupPhaseLengthBlocks     int
	fullbandErleEstimator        *FullBandErleEstimator
	subbandErleEstimator         *SubbandErleEstimator
	signalDependentErleEstimator *SignalDependentErleEstimator // nil if config.Erle.NumSections <= 1, mirrors the C++ unique_ptr being unset.
	blocksSinceReset             int
}

// NewErleEstimator constructs an ErleEstimator. C:
// ErleEstimator::ErleEstimator.
func NewErleEstimator(startupPhaseLengthBlocks int, config config.Config, numCaptureChannels int) *ErleEstimator {
	e := &ErleEstimator{
		startupPhaseLengthBlocks: startupPhaseLengthBlocks,
		fullbandErleEstimator:    NewFullBandErleEstimator(config.Erle, numCaptureChannels),
		subbandErleEstimator:     NewSubbandErleEstimator(config, numCaptureChannels),
	}
	if config.Erle.NumSections > 1 {
		e.signalDependentErleEstimator = NewSignalDependentErleEstimator(config, numCaptureChannels)
	}
	e.Reset(true)
	return e
}

// Reset resets the fullband ERLE estimator and the subbands ERLE
// estimators. C: ErleEstimator::Reset.
func (e *ErleEstimator) Reset(delayChange bool) {
	e.fullbandErleEstimator.Reset()
	e.subbandErleEstimator.Reset()
	if e.signalDependentErleEstimator != nil {
		e.signalDependentErleEstimator.Reset()
	}
	if delayChange {
		e.blocksSinceReset = 0
	}
}

// Update updates the ERLE estimates. C: ErleEstimator::Update.
func (e *ErleEstimator) Update(renderBuffer *RenderBuffer, filterFrequencyResponses [][][FFTLengthBy2Plus1]float32, avgRenderSpectrumWithReverb [FFTLengthBy2Plus1]float32, captureSpectra, subtractorSpectra [][FFTLengthBy2Plus1]float32, convergedFilters []bool) {
	x2Reverb := avgRenderSpectrumWithReverb
	y2 := captureSpectra
	e2 := subtractorSpectra

	e.blocksSinceReset++
	if e.blocksSinceReset < e.startupPhaseLengthBlocks {
		return
	}

	e.subbandErleEstimator.Update(x2Reverb, y2, e2, convergedFilters)

	if e.signalDependentErleEstimator != nil {
		e.signalDependentErleEstimator.Update(renderBuffer, filterFrequencyResponses, x2Reverb, y2, e2,
			e.subbandErleEstimator.Erle(false), e.subbandErleEstimator.Erle(true), convergedFilters)
	}

	e.fullbandErleEstimator.Update(x2Reverb[:], y2, e2, convergedFilters)
}

// Erle returns the most recent subband ERLE estimates. C:
// ErleEstimator::Erle.
func (e *ErleEstimator) Erle(onsetCompensated bool) [][FFTLengthBy2Plus1]float32 {
	if e.signalDependentErleEstimator != nil {
		return e.signalDependentErleEstimator.Erle(onsetCompensated)
	}
	return e.subbandErleEstimator.Erle(onsetCompensated)
}

// ErleUnbounded returns the non-capped subband ERLE. Unbounded ERLE
// is only used with the subband ERLE estimator where the ERLE is
// often capped at low values. When the signal-dependent ERLE
// estimator is used the capped ERLE is returned. C:
// ErleEstimator::ErleUnbounded.
func (e *ErleEstimator) ErleUnbounded() [][FFTLengthBy2Plus1]float32 {
	if e.signalDependentErleEstimator == nil {
		return e.subbandErleEstimator.ErleUnbounded()
	}
	return e.signalDependentErleEstimator.Erle(false)
}

// ErleDuringOnsets returns the subband ERLE that are estimated during
// onsets (only used for testing). C: ErleEstimator::ErleDuringOnsets.
func (e *ErleEstimator) ErleDuringOnsets() [][FFTLengthBy2Plus1]float32 {
	return e.subbandErleEstimator.ErleDuringOnsets()
}

// FullbandErleLog2 returns the fullband ERLE estimate. C:
// ErleEstimator::FullbandErleLog2.
func (e *ErleEstimator) FullbandErleLog2() float32 {
	return e.fullbandErleEstimator.FullbandErleLog2()
}

// GetInstLinearQualityEstimates returns an estimation of the current
// linear filter quality based on the current and past fullband ERLE
// estimates. Entries are nil to mirror std::optional's empty state.
// C: ErleEstimator::GetInstLinearQualityEstimates.
func (e *ErleEstimator) GetInstLinearQualityEstimates() []*float32 {
	return e.fullbandErleEstimator.GetInstLinearQualityEstimates()
}
