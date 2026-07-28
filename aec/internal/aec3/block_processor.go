// This file ports modules/audio_processing/aec3/block_processor.{h,cc}:
// BlockProcessor, which drives one capture block through the render
// delay buffer, render delay controller, and echo remover.
//
// Dropped/collapsed per this port's conventions:
//   - The abstract BlockProcessor interface plus BlockProcessorImpl and
//     BlockProcessor::Create's three overloads (the plain factory, the
//     testing-only overload taking a caller-supplied RenderDelayBuffer,
//     and the testing-only overload additionally taking a caller-
//     supplied RenderDelayController/EchoRemover): collapsed into one
//     concrete BlockProcessor type with a single plain constructor,
//     matching every other ported component in this package (AecState,
//     EchoRemover, Subtractor, ...). The two testing-only overloads
//     exist upstream purely to allow gtest to inject fakes/mocks; this
//     port has no such test-double seam and does not need one.
//   - ApmDataDumper and its DumpRaw/DumpWav calls: pure debug
//     instrumentation, no numerical effect.
//   - RTC_DCHECK assertions: debug-only, no numerical effect.
//   - RTC_LOG(_V) delay-change/buffer-overrun log lines, and the
//     capture_call_counter_ field that existed solely to number those
//     log lines: pure debug instrumentation, no numerical effect and
//     no other reader.
//   - std::atomic<int> instance_count_: fed only the dropped
//     ApmDataDumper's instance id.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// BlockProcessor performs echo cancellation on 64-sample blocks of
// audio data, driving the render delay buffer, the render delay
// controller (unless an external delay estimator is configured), and
// the echo remover. C: BlockProcessor/BlockProcessorImpl.
type BlockProcessor struct {
	config                 config.Config
	captureProperlyStarted bool
	renderProperlyStarted  bool
	sampleRateHz           int
	renderBuffer           *RenderDelayBuffer
	delayController        *RenderDelayController // nil iff config.Delay.UseExternalDelayEstimator
	echoRemover            *EchoRemover
	metrics                *BlockProcessorMetrics
	renderEvent            RenderDelayBufferEvent
	estimatedDelay         *DelayEstimate
}

// NewBlockProcessor mirrors BlockProcessor::Create (the plain,
// non-testing overload) followed by BlockProcessorImpl's C++
// constructor: it builds the RenderDelayBuffer and EchoRemover, and
// the RenderDelayController unless config.Delay.UseExternalDelayEstimator
// is set (matching upstream's Create, which only builds a
// RenderDelayController when !config.delay.use_external_delay_estimator).
func NewBlockProcessor(config config.Config, sampleRateHz int, numRenderChannels, numCaptureChannels int) *BlockProcessor {
	var delayController *RenderDelayController
	if !config.Delay.UseExternalDelayEstimator {
		delayController = NewRenderDelayController(config, sampleRateHz, numCaptureChannels)
	}
	return &BlockProcessor{
		config:          config,
		sampleRateHz:    sampleRateHz,
		renderBuffer:    NewRenderDelayBuffer(config, sampleRateHz, numRenderChannels),
		delayController: delayController,
		echoRemover:     NewEchoRemover(config, sampleRateHz, numRenderChannels, numCaptureChannels),
		metrics:         NewBlockProcessorMetrics(),
		renderEvent:     RenderDelayBufferEventNone,
	}
}

// ProcessCapture processes a block of capture data, in place, removing
// the estimated echo. If linearOutput is non-nil it receives the
// linear (pre-suppression-gain) filter output. C:
// BlockProcessorImpl::ProcessCapture.
func (bp *BlockProcessor) ProcessCapture(echoPathGainChange bool, captureSignalSaturation bool, linearOutput, captureBlock *Block) {
	if bp.renderProperlyStarted {
		if !bp.captureProperlyStarted {
			bp.captureProperlyStarted = true
			bp.renderBuffer.Reset()
			if bp.delayController != nil {
				bp.delayController.Reset(true)
			}
		}
	} else {
		// If no render data has yet arrived, do not process the capture
		// signal.
		bp.renderBuffer.HandleSkippedCaptureProcessing()
		return
	}

	echoPathVariability := NewEchoPathVariability(echoPathGainChange, DelayAdjustmentNone, false)

	if bp.renderEvent == RenderDelayBufferEventRenderOverrun && bp.renderProperlyStarted {
		echoPathVariability.DelayChange = DelayAdjustmentBufferFlush
		if bp.delayController != nil {
			bp.delayController.Reset(true)
		}
	}
	bp.renderEvent = RenderDelayBufferEventNone

	// Update the render buffers with any newly arrived render blocks and
	// prepare the render buffers for reading the render data
	// corresponding to the current capture block.
	bufferEvent := bp.renderBuffer.PrepareCaptureProcessing()
	// Reset the delay controller at render buffer underrun.
	if bufferEvent == RenderDelayBufferEventRenderUnderrun {
		if bp.delayController != nil {
			bp.delayController.Reset(false)
		}
	}

	hasDelayEstimator := !bp.config.Delay.UseExternalDelayEstimator
	if hasDelayEstimator {
		// Compute and apply the render delay required to achieve proper
		// signal alignment.
		bp.estimatedDelay = bp.delayController.GetDelay(bp.renderBuffer.GetDownsampledRenderBuffer(), bp.renderBuffer.Delay(), captureBlock)

		if bp.estimatedDelay != nil {
			delayChange := bp.renderBuffer.AlignFromDelay(bp.estimatedDelay.Delay)
			if delayChange {
				echoPathVariability.DelayChange = DelayAdjustmentNewDetectedDelay
			}
		}

		echoPathVariability.ClockDrift = bp.delayController.HasClockdrift()
	} else {
		bp.renderBuffer.AlignFromExternalDelay()
	}

	// Remove the echo from the capture signal.
	if hasDelayEstimator || bp.renderBuffer.HasReceivedBufferDelay() {
		bp.echoRemover.ProcessCapture(echoPathVariability, captureSignalSaturation, bp.estimatedDelay, bp.renderBuffer.GetRenderBuffer(), linearOutput, captureBlock)
	}

	// Update the metrics.
	bp.metrics.UpdateCapture(false)
}

// BufferRender buffers a block of render data supplied by a
// FrameBlocker object. C: BlockProcessorImpl::BufferRender.
func (bp *BlockProcessor) BufferRender(block *Block) {
	bp.renderEvent = bp.renderBuffer.Insert(block)

	bp.metrics.UpdateRender(bp.renderEvent != RenderDelayBufferEventNone)

	bp.renderProperlyStarted = true
	if bp.delayController != nil {
		bp.delayController.LogRenderCall()
	}
}

// UpdateEchoLeakageStatus reports whether echo leakage has been
// detected in the echo canceller output. C:
// BlockProcessorImpl::UpdateEchoLeakageStatus.
func (bp *BlockProcessor) UpdateEchoLeakageStatus(leakageDetected bool) {
	bp.echoRemover.UpdateEchoLeakageStatus(leakageDetected)
}

// GetMetrics reads the current metrics. C: BlockProcessorImpl::GetMetrics.
func (bp *BlockProcessor) GetMetrics(metrics *Metrics) {
	bp.echoRemover.GetMetrics(metrics)
	const blockSizeMs = 4
	// Upstream's render_buffer_->Delay() returns a concrete size_t
	// (not actually optional despite the std::optional<size_t> local:
	// see render_delay_buffer.h's Delay(), which always returns a
	// value), so the C++ ternary `delay ? *delay*block_size_ms : 0`
	// always takes the true branch; this port's RenderDelayBuffer.Delay
	// already returns a concrete int, so no nil-check is needed.
	metrics.DelayMs = bp.renderBuffer.Delay() * blockSizeMs
}

// SetAudioBufferDelay provides an optional external estimate of the
// audio buffer delay. C: BlockProcessorImpl::SetAudioBufferDelay.
func (bp *BlockProcessor) SetAudioBufferDelay(delayMs int) {
	bp.renderBuffer.SetAudioBufferDelay(delayMs)
}

// SetCaptureOutputUsage specifies whether the capture output will be
// used, allowing the block processor to skip some processing when the
// result is not used (e.g. a muted endpoint). C:
// BlockProcessorImpl::SetCaptureOutputUsage.
func (bp *BlockProcessor) SetCaptureOutputUsage(captureOutputUsed bool) {
	bp.echoRemover.SetCaptureOutputUsage(captureOutputUsed)
}

// SetSuppressorGating selects whether the echo remover's suppressor is
// gated on demonstrated convergence. Go-port-only, beyond upstream; see
// suppressor_gate.go.
func (bp *BlockProcessor) SetSuppressorGating(mode SuppressorGating) {
	bp.echoRemover.SetSuppressorGating(mode)
}

// SuppressorGate reports whether the suppressor is currently allowed to
// attenuate the capture path. Go-port-only, beyond upstream; see
// suppressor_gate.go.
func (bp *BlockProcessor) SuppressorGate() SuppressorGateState {
	return bp.echoRemover.SuppressorGate()
}

// Clockdrift reports the clockdrift level currently detected by the
// internal RenderDelayController's ClockdriftDetector. Go-port-only
// addition, documented as beyond upstream: upstream only threads
// clockdrift through as the boolean EchoPathVariability.ClockDrift
// (fed to EchoRemover); this accessor exposes the richer three-level
// signal (None/Probable/Verified) for the public aec API's Metrics.
// Returns ClockdriftLevelNone when no delay controller is in use
// (config.Delay.UseExternalDelayEstimator).
func (bp *BlockProcessor) Clockdrift() ClockdriftLevel {
	if bp.delayController == nil {
		return ClockdriftLevelNone
	}
	return bp.delayController.ClockdriftLevel()
}
