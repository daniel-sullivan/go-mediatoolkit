// This file ports modules/audio_processing/aec3/
// echo_remover_metrics.{h,cc}: EchoRemoverMetrics, which tracks
// windowed min/max/instant statistics for ERL/ERLE and periodically
// resets them for reporting.
//
// Dropped per this port's conventions (matching
// render_delay_controller_metrics.go's established precedent): the
// RTC_HISTOGRAM_* emission calls, since there is no metrics backend
// in this repo for them to report to. The block-counter state machine
// that upstream computes before reporting (including the periodic
// reset) is kept faithfully.
package aec3

import "math"

// metricsCollectionBlocks == kMetricsCollectionBlocks
// (kMetricsReportingIntervalBlocks - 3).
const metricsCollectionBlocks = metricsRemoverReportingIntervalBlocks - 3

// metricsRemoverReportingIntervalBlocks == kMetricsReportingIntervalBlocks
// (10 * kNumBlocksPerSecond). Named distinctly from
// render_delay_controller_metrics.go's metricsReportingIntervalBlocks
// (same value, separate C++ constant/class).
const metricsRemoverReportingIntervalBlocks = 10 * NumBlocksPerSecond

// DbMetric tracks a running sum/instant value together with the
// observed floor and ceiling. C:
// EchoRemoverMetrics::DbMetric.
type DbMetric struct {
	SumValue   float32
	FloorValue float32
	CeilValue  float32
}

// NewDbMetric mirrors DbMetric's C++ constructor.
func NewDbMetric(sumValue, floorValue, ceilValue float32) DbMetric {
	return DbMetric{SumValue: sumValue, FloorValue: floorValue, CeilValue: ceilValue}
}

// Update accumulates value into SumValue and updates the floor/ceil.
// C: DbMetric::Update.
func (m *DbMetric) Update(value float32) {
	m.SumValue += value
	if value < m.FloorValue {
		m.FloorValue = value
	}
	if value > m.CeilValue {
		m.CeilValue = value
	}
}

// UpdateInstant replaces SumValue with value and updates the
// floor/ceil. C: DbMetric::UpdateInstant.
func (m *DbMetric) UpdateInstant(value float32) {
	m.SumValue = value
	if value < m.FloorValue {
		m.FloorValue = value
	}
	if value > m.CeilValue {
		m.CeilValue = value
	}
}

// EchoRemoverMetrics handles the reporting of metrics for the echo
// remover. C: echo_remover_metrics.{h,cc}'s EchoRemoverMetrics.
type EchoRemoverMetrics struct {
	blockCounter     int
	erlTimeDomain    DbMetric
	erleTimeDomain   DbMetric
	saturatedCapture bool
	metricsReported  bool
}

// NewEchoRemoverMetrics mirrors EchoRemoverMetrics's C++ constructor.
func NewEchoRemoverMetrics() *EchoRemoverMetrics {
	m := &EchoRemoverMetrics{}
	m.resetMetrics()
	return m
}

// resetMetrics resets the metrics. C: EchoRemoverMetrics::ResetMetrics.
func (m *EchoRemoverMetrics) resetMetrics() {
	m.erlTimeDomain = NewDbMetric(0, 10000, 0.000)
	m.erleTimeDomain = NewDbMetric(0, 0, 1000)
	m.saturatedCapture = false
}

// Update updates the metric with new data. C: EchoRemoverMetrics::Update.
func (m *EchoRemoverMetrics) Update(aecState *AecState, comfortNoiseSpectrum, suppressorGain [FFTLengthBy2Plus1]float32) {
	m.metricsReported = false
	m.blockCounter++
	if m.blockCounter <= metricsCollectionBlocks {
		m.erlTimeDomain.UpdateInstant(aecState.ErlTimeDomain())
		m.erleTimeDomain.UpdateInstant(aecState.FullBandErleLog2())
		m.saturatedCapture = m.saturatedCapture || aecState.SaturatedCapture()
	} else {
		// Report the metrics over several frames in order to lower the
		// impact of the logarithms involved on the computational
		// complexity. This port has no metrics backend, so only the
		// counter/reset state machine is retained; the
		// RTC_HISTOGRAM_COUNTS_LINEAR/RTC_HISTOGRAM_BOOLEAN reporting
		// calls that used to run at each of these steps are dropped.
		switch m.blockCounter {
		case metricsCollectionBlocks + 1:
		case metricsCollectionBlocks + 2:
		case metricsCollectionBlocks + 3:
			m.metricsReported = true
			m.blockCounter = 0
			m.resetMetrics()
		default:
			// Unreachable (matches C++'s RTC_DCHECK_NOTREACHED).
		}
	}
}

// MetricsReported returns true if the metrics have just been
// reported, otherwise false. C: EchoRemoverMetrics::MetricsReported.
func (m *EchoRemoverMetrics) MetricsReported() bool {
	return m.metricsReported
}

// UpdateDbMetric updates a banded (2-band) DbMetric statistic with
// the values in value. C: aec3::UpdateDbMetric.
func UpdateDbMetric(value [FFTLengthBy2Plus1]float32, statistic *[2]DbMetric) {
	// Truncation is intended in the band width computation.
	const kNumBands = 2
	const kBandWidth = 65 / kNumBands
	const kOneByBandWidth = 1.0 / kBandWidth
	for k := 0; k < kNumBands; k++ {
		var sum float32
		for _, v := range value[kBandWidth*k : kBandWidth*(k+1)] {
			sum = add32(sum, v)
		}
		averageBand := mul32(sum, kOneByBandWidth)
		statistic[k].Update(averageBand)
	}
}

// TransformDbMetricForReporting transforms a DbMetric from the linear
// domain into the logarithmic domain. C:
// aec3::TransformDbMetricForReporting.
func TransformDbMetricForReporting(negate bool, minValue, maxValue, offset, scaling, value float32) int {
	logArg := mla(1e-10, value, scaling)
	newValue := mla(offset, 10.0, float32(math.Log10(float64(logArg))))
	if negate {
		newValue = -newValue
	}
	return int(clamp32(newValue, minValue, maxValue))
}
