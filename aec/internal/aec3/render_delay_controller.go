// This file ports modules/audio_processing/aec3/
// render_delay_controller.{h,cc}: turns raw EchoPathDelayEstimator
// output into a hysteresis-smoothed render delay buffer adjustment
// (in blocks).
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// computeBufferDelay computes the buffer delay increase required to
// achieve the desired latency, in blocks, applying hysteresis against
// currentDelay (nil for std::nullopt). C: (anonymous namespace)
// ComputeBufferDelay.
func computeBufferDelay(currentDelay *DelayEstimate, hysteresisLimitBlocks int, estimatedDelay DelayEstimate) DelayEstimate {
	newDelayBlocks := estimatedDelay.Delay >> BlockSizeLog2
	if currentDelay != nil {
		currentDelayBlocks := currentDelay.Delay
		if newDelayBlocks > currentDelayBlocks && newDelayBlocks <= currentDelayBlocks+hysteresisLimitBlocks {
			newDelayBlocks = currentDelayBlocks
		}
	}
	newDelay := estimatedDelay
	newDelay.Delay = newDelayBlocks
	return newDelay
}

// RenderDelayController aligns the render and capture signal using a
// RenderDelayBuffer. Port of RenderDelayControllerImpl (minus the
// ApmDataDumper logging, which has no numeric effect).
type RenderDelayController struct {
	hysteresisLimitBlocks    int
	delay                    *DelayEstimate
	delayEstimator           *EchoPathDelayEstimator
	metrics                  *RenderDelayControllerMetrics
	delaySamples             *DelayEstimate
	captureCallCounter       int
	delayChangeCounter       int
	lastDelayEstimateQuality DelayEstimateQuality
}

// NewRenderDelayController mirrors RenderDelayControllerImpl's C++
// constructor. sampleRateHz is accepted for parity with the upstream
// signature (it is only used there for a DCHECK and a no-op logging
// call) but is otherwise unused.
func NewRenderDelayController(config config.Config, sampleRateHz int, numCaptureChannels int) *RenderDelayController {
	return &RenderDelayController{
		hysteresisLimitBlocks:    config.Delay.HysteresisLimitBlocks,
		delayEstimator:           NewEchoPathDelayEstimator(config, numCaptureChannels),
		metrics:                  NewRenderDelayControllerMetrics(),
		lastDelayEstimateQuality: DelayEstimateQualityCoarse,
	}
}

// Reset resets the delay controller. If the delay confidence is
// reset, the reset behavior is as if the call is restarted. C:
// RenderDelayControllerImpl::Reset.
func (r *RenderDelayController) Reset(resetDelayConfidence bool) {
	r.delay = nil
	r.delaySamples = nil
	r.delayEstimator.Reset(resetDelayConfidence)
	r.delayChangeCounter = 0
	if resetDelayConfidence {
		r.lastDelayEstimateQuality = DelayEstimateQualityCoarse
	}
}

// LogRenderCall is a no-op in this port (upstream's is also empty).
// C: RenderDelayControllerImpl::LogRenderCall.
func (r *RenderDelayController) LogRenderCall() {}

// GetDelay aligns the render buffer content with the capture signal,
// returning the render delay buffer delay (in blocks) or nil if none
// is available yet. C: RenderDelayControllerImpl::GetDelay.
func (r *RenderDelayController) GetDelay(renderBuffer *DownsampledRenderBuffer, renderDelayBufferDelay int, capture *Block) *DelayEstimate {
	r.captureCallCounter++

	delaySamples := r.delayEstimator.EstimateDelay(renderBuffer, capture)

	if delaySamples != nil {
		if r.delaySamples == nil || delaySamples.Delay != r.delaySamples.Delay {
			r.delayChangeCounter = 0
		}
		if r.delaySamples != nil {
			if r.delaySamples.Delay == delaySamples.Delay {
				r.delaySamples.BlocksSinceLastChange++
			} else {
				r.delaySamples.BlocksSinceLastChange = 0
			}
			r.delaySamples.BlocksSinceLastUpdate = 0
			r.delaySamples.Delay = delaySamples.Delay
			r.delaySamples.Quality = delaySamples.Quality
		} else {
			r.delaySamples = delaySamples
		}
	} else if r.delaySamples != nil {
		r.delaySamples.BlocksSinceLastChange++
		r.delaySamples.BlocksSinceLastUpdate++
	}

	if r.delayChangeCounter < 2*NumBlocksPerSecond {
		r.delayChangeCounter++
	}

	if r.delaySamples != nil {
		// Compute the render delay buffer delay.
		useHysteresis := r.lastDelayEstimateQuality == DelayEstimateQualityRefined &&
			r.delaySamples.Quality == DelayEstimateQualityRefined
		hysteresisLimitBlocks := 0
		if useHysteresis {
			hysteresisLimitBlocks = r.hysteresisLimitBlocks
		}
		newDelay := computeBufferDelay(r.delay, hysteresisLimitBlocks, *r.delaySamples)
		r.delay = &newDelay
		r.lastDelayEstimateQuality = r.delaySamples.Quality
	}

	var delaySamplesForMetrics, bufferDelayBlocksForMetrics *int
	if r.delaySamples != nil {
		d := r.delaySamples.Delay
		delaySamplesForMetrics = &d
	}
	if r.delay != nil {
		d := r.delay.Delay
		bufferDelayBlocksForMetrics = &d
	}
	r.metrics.Update(delaySamplesForMetrics, bufferDelayBlocksForMetrics, r.delayEstimator.Clockdrift())

	return r.delay
}

// HasClockdrift returns true if clockdrift has been detected. C:
// RenderDelayControllerImpl::HasClockdrift.
func (r *RenderDelayController) HasClockdrift() bool {
	return r.delayEstimator.Clockdrift() != ClockdriftLevelNone
}

// ClockdriftLevel returns the raw clockdrift level detected by the
// delay estimator. Go-port-only addition (beyond upstream, which only
// exposes the boolean HasClockdrift): needed so BlockProcessor's
// Go-port-only Clockdrift() accessor (see block_processor.go) can
// surface the full ClockdriftLevel through the public aec API's
// Metrics, not just a boolean.
func (r *RenderDelayController) ClockdriftLevel() ClockdriftLevel {
	return r.delayEstimator.Clockdrift()
}
