// This file ports modules/audio_processing/aec3/
// echo_path_delay_estimator.{h,cc}: wires together AlignmentMixer,
// Decimator, MatchedFilter, MatchedFilterLagAggregator and
// ClockdriftDetector to estimate the echo path delay each capture
// block.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// EchoPathDelayEstimator estimates the delay of the echo path. Port
// of EchoPathDelayEstimator (minus the ApmDataDumper logging, which
// has no numeric effect on the estimate).
type EchoPathDelayEstimator struct {
	downSamplingFactor         int
	subBlockSize               int
	captureMixer               *AlignmentMixer
	captureDecimator           *Decimator
	matchedFilter              *MatchedFilter
	matchedFilterLagAggregator *MatchedFilterLagAggregator

	oldAggregatedLag          *DelayEstimate
	consistentEstimateCounter int
	clockdriftDetector        *ClockdriftDetector
}

// NewEchoPathDelayEstimator mirrors EchoPathDelayEstimator's C++
// constructor.
func NewEchoPathDelayEstimator(config config.Config, numCaptureChannels int) *EchoPathDelayEstimator {
	downSamplingFactor := config.Delay.DownSamplingFactor
	subBlockSize := BlockSize
	if downSamplingFactor != 0 {
		subBlockSize = BlockSize / downSamplingFactor
	}

	poorExcitationRenderLimit := config.RenderLevels.PoorExcitationRenderLimit
	if downSamplingFactor == 8 {
		poorExcitationRenderLimit = config.RenderLevels.PoorExcitationRenderLimitDS8
	}

	matchedFilter := NewMatchedFilter(
		subBlockSize,
		MatchedFilterWindowSizeSubBlocks,
		config.Delay.NumFilters,
		MatchedFilterAlignmentShiftSizeSubBlocks,
		poorExcitationRenderLimit,
		config.Delay.DelayEstimateSmoothing,
		config.Delay.DelayEstimateSmoothingDelayFound,
		config.Delay.DelayCandidateDetectionThreshold,
		config.Delay.DetectPreEcho,
	)

	return &EchoPathDelayEstimator{
		downSamplingFactor:         downSamplingFactor,
		subBlockSize:               subBlockSize,
		captureMixer:               NewAlignmentMixerFromConfig(numCaptureChannels, config.Delay.CaptureAlignmentMixing),
		captureDecimator:           NewDecimator(downSamplingFactor),
		matchedFilter:              matchedFilter,
		matchedFilterLagAggregator: NewMatchedFilterLagAggregator(matchedFilter.GetMaxFilterLag(), config.Delay),
		clockdriftDetector:         NewClockdriftDetector(),
	}
}

// Reset resets the estimation. C: EchoPathDelayEstimator::Reset
// (public overload).
func (e *EchoPathDelayEstimator) Reset(resetDelayConfidence bool) {
	e.reset(true, resetDelayConfidence)
}

// EstimateDelay produces a delay estimate if one is available. C:
// EchoPathDelayEstimator::EstimateDelay.
func (e *EchoPathDelayEstimator) EstimateDelay(renderBuffer *DownsampledRenderBuffer, capture *Block) *DelayEstimate {
	downsampledCapture := make([]float32, e.subBlockSize)

	var downmixedCapture [BlockSize]float32
	e.captureMixer.ProduceOutput(capture, downmixedCapture[:])
	e.captureDecimator.Decimate(downmixedCapture[:], downsampledCapture)

	e.matchedFilter.Update(renderBuffer, downsampledCapture, e.matchedFilterLagAggregator.ReliableDelayFound())

	aggregated := e.matchedFilterLagAggregator.Aggregate(e.matchedFilter.GetBestLagEstimate())

	// Run clockdrift detection.
	if aggregated != nil && aggregated.Quality == DelayEstimateQualityRefined {
		e.clockdriftDetector.Update(e.matchedFilterLagAggregator.GetDelayAtHighestPeak())
	}

	// Return the detected delay in samples as the aggregated matched
	// filter lag compensated by the down sampling factor for the
	// signal being correlated.
	if aggregated != nil {
		aggregated.Delay *= e.downSamplingFactor
	}

	if e.oldAggregatedLag != nil && aggregated != nil && e.oldAggregatedLag.Delay == aggregated.Delay {
		e.consistentEstimateCounter++
	} else {
		e.consistentEstimateCounter = 0
	}
	e.oldAggregatedLag = aggregated

	const numBlocksPerSecondBy2 = NumBlocksPerSecond / 2
	if e.consistentEstimateCounter > numBlocksPerSecondBy2 {
		e.reset(false, false)
	}

	return aggregated
}

// LogDelayEstimationProperties is a no-op in this port: it only ever
// forwards to the (also-dropped) ApmDataDumper-based
// MatchedFilter::LogFilterProperties debug logging upstream, with no
// effect on any estimate produced by this package.
func (e *EchoPathDelayEstimator) LogDelayEstimationProperties(sampleRateHz, shift int) {}

// Clockdrift returns the level of detected clockdrift. C:
// EchoPathDelayEstimator::Clockdrift.
func (e *EchoPathDelayEstimator) Clockdrift() ClockdriftLevel {
	return e.clockdriftDetector.ClockdriftLevel()
}

// reset is EchoPathDelayEstimator's private two-argument Reset. C:
// EchoPathDelayEstimator::Reset(bool, bool).
func (e *EchoPathDelayEstimator) reset(resetLagAggregator, resetDelayConfidence bool) {
	if resetLagAggregator {
		e.matchedFilterLagAggregator.Reset(resetDelayConfidence)
	}
	e.matchedFilter.Reset(resetLagAggregator)
	e.oldAggregatedLag = nil
	e.consistentEstimateCounter = 0
}
