// This file ports modules/audio_processing/aec3/delay_estimate.h's
// DelayEstimate.
package aec3

// DelayEstimateQuality is DelayEstimate::Quality.
type DelayEstimateQuality int

const (
	// DelayEstimateQualityCoarse == DelayEstimate::Quality::kCoarse.
	DelayEstimateQualityCoarse DelayEstimateQuality = iota
	// DelayEstimateQualityRefined == DelayEstimate::Quality::kRefined.
	DelayEstimateQualityRefined
)

// DelayEstimate stores a delay estimate together with its quality and
// staleness counters. C: DelayEstimate.
type DelayEstimate struct {
	Quality               DelayEstimateQuality
	Delay                 int
	BlocksSinceLastChange int
	BlocksSinceLastUpdate int
}

// NewDelayEstimate mirrors DelayEstimate's C++ constructor.
func NewDelayEstimate(quality DelayEstimateQuality, delay int) DelayEstimate {
	return DelayEstimate{Quality: quality, Delay: delay}
}
