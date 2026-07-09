// This file ports modules/audio_processing/aec3/
// render_delay_controller_metrics.{h,cc}: tracks reliability/change
// counters for the render delay controller. The upstream file's only
// externally observable effect is emitting RTC_HISTOGRAM_* metrics to
// WebRTC's own metrics backend (no counterpart in this repo, and read
// by nothing else in the C++ source either) -- those emission calls
// are dropped, matching this port's established convention of
// dropping ApmDataDumper-only debug/telemetry side effects. The
// counter state machine that upstream computes before reporting is
// kept faithfully in case a caller wants to inspect it later, but
// note it has zero effect on any DelayEstimate produced by this
// package.
package aec3

// metricsReportingIntervalBlocks == kMetricsReportingIntervalBlocks
// (10 * kNumBlocksPerSecond).
const metricsReportingIntervalBlocks = 10 * NumBlocksPerSecond

// RenderDelayControllerMetrics handles the reporting of metrics for
// the render delay controller. Port of RenderDelayControllerMetrics.
type RenderDelayControllerMetrics struct {
	delayBlocks                  int
	reliableDelayEstimateCounter int
	delayChangeCounter           int
	callCounter                  int
	initialCallCounter           int
	initialUpdate                bool
}

// NewRenderDelayControllerMetrics mirrors
// RenderDelayControllerMetrics's C++ constructor.
func NewRenderDelayControllerMetrics() *RenderDelayControllerMetrics {
	return &RenderDelayControllerMetrics{initialUpdate: true}
}

// Update updates the metric with new data. delaySamples and
// bufferDelayBlocks are nil for std::nullopt. C:
// RenderDelayControllerMetrics::Update.
func (m *RenderDelayControllerMetrics) Update(delaySamples, bufferDelayBlocks *int, clockdrift ClockdriftLevel) {
	m.callCounter++

	if !m.initialUpdate {
		var delayBlocks int
		if delaySamples != nil {
			m.reliableDelayEstimateCounter++
			// Add an offset by 1 (metric is halved before reporting) to
			// reserve 0 for absent delay.
			delayBlocks = *delaySamples/BlockSize + 2
		} else {
			delayBlocks = 0
		}

		if delayBlocks != m.delayBlocks {
			m.delayChangeCounter++
			m.delayBlocks = delayBlocks
		}
	} else {
		m.initialCallCounter++
		if m.initialCallCounter == 5*NumBlocksPerSecond {
			m.initialUpdate = false
		}
	}

	if m.callCounter == metricsReportingIntervalBlocks {
		// Upstream reports WebRTC.Audio.EchoCanceller.{EchoPathDelay,
		// BufferDelay,ReliableDelayEstimates,DelayChanges,Clockdrift}
		// histograms here, derived solely from delayBlocks,
		// bufferDelayBlocks, reliableDelayEstimateCounter,
		// delayChangeCounter and clockdrift. This port has no metrics
		// backend to report to, so the derivation is skipped; only the
		// counter reset below is kept.
		_ = bufferDelayBlocks
		_ = clockdrift

		m.callCounter = 0
		m.resetMetrics()
	}
}

// resetMetrics resets the metrics. C:
// RenderDelayControllerMetrics::ResetMetrics.
func (m *RenderDelayControllerMetrics) resetMetrics() {
	m.delayChangeCounter = 0
	m.reliableDelayEstimateCounter = 0
}
