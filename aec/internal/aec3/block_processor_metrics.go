// This file ports modules/audio_processing/aec3/
// block_processor_metrics.{h,cc}: BlockProcessorMetrics, which
// tracks render-buffer underrun/overrun counters over a reporting
// window and (upstream) periodically emits them as UMA histograms.
//
// Dropped:
//   - The RTC_HISTOGRAM_ENUMERATION calls in UpdateCapture: these
//     feed Chromium's UMA telemetry backend, which has no Go
//     equivalent and no effect on the canceller's own behavior. The
//     categorization arithmetic and the reporting-interval bookkeeping
//     (which upstream code elsewhere could observe via
//     MetricsReported()) are ported faithfully; only the actual metric
//     emission is omitted.
package aec3

// metricsReportingIntervalBlocks (== kMetricsReportingIntervalBlocks)
// is already declared in render_delay_controller_metrics.go; reused
// here rather than redeclared.

// BlockProcessorMetrics handles the reporting of metrics for
// BlockProcessor. C: BlockProcessorMetrics.
type BlockProcessorMetrics struct {
	captureBlockCounter   int
	metricsReported       bool
	renderBufferUnderruns int
	renderBufferOverruns  int
	bufferRenderCalls     int
}

// NewBlockProcessorMetrics mirrors BlockProcessorMetrics's C++
// default constructor.
func NewBlockProcessorMetrics() *BlockProcessorMetrics {
	return &BlockProcessorMetrics{}
}

// UpdateCapture updates the metric with new capture data. C:
// BlockProcessorMetrics::UpdateCapture.
func (m *BlockProcessorMetrics) UpdateCapture(underrun bool) {
	m.captureBlockCounter++
	if underrun {
		m.renderBufferUnderruns++
	}

	if m.captureBlockCounter == metricsReportingIntervalBlocks {
		m.metricsReported = true
		// C: RTC_HISTOGRAM_ENUMERATION("WebRTC.Audio.EchoCanceller.
		// RenderUnderruns", ...) and "...RenderOverruns" — omitted (see
		// file header); the categorization itself has no consumer in
		// this port.
		m.resetMetrics()
		m.captureBlockCounter = 0
	} else {
		m.metricsReported = false
	}
}

// UpdateRender updates the metric with new render data. C:
// BlockProcessorMetrics::UpdateRender.
func (m *BlockProcessorMetrics) UpdateRender(overrun bool) {
	m.bufferRenderCalls++
	if overrun {
		m.renderBufferOverruns++
	}
}

// MetricsReported returns true if the metrics have just been
// reported, otherwise false. C: BlockProcessorMetrics::MetricsReported.
func (m *BlockProcessorMetrics) MetricsReported() bool {
	return m.metricsReported
}

// resetMetrics resets the metrics. C: BlockProcessorMetrics::ResetMetrics.
func (m *BlockProcessorMetrics) resetMetrics() {
	m.renderBufferUnderruns = 0
	m.renderBufferOverruns = 0
	m.bufferRenderCalls = 0
}
