// This file ports modules/audio_processing/aec3/
// matched_filter_lag_aggregator.{h,cc}: aggregates per-filter
// MatchedFilter lag estimates produced across all filters in the bank
// into a single reliable combined delay estimate.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

const preEchoHistogramDataNotUpdated = -1

// getDownSamplingBlockSizeLog2 mirrors matched_filter_lag_aggregator.cc's
// anonymous namespace's GetDownSamplingBlockSizeLog2.
func getDownSamplingBlockSizeLog2(downSamplingFactor int) int {
	downSamplingFactorLog2 := 0
	downSamplingFactor >>= 1
	for downSamplingFactor > 0 {
		downSamplingFactorLog2++
		downSamplingFactor >>= 1
	}
	if BlockSizeLog2 > downSamplingFactorLog2 {
		return BlockSizeLog2 - downSamplingFactorLog2
	}
	return 0
}

// highestPeakAggregator is a rolling histogram (over the last 250
// updates) of the highest-energy lag, keyed on lag value. C:
// MatchedFilterLagAggregator::HighestPeakAggregator.
type highestPeakAggregator struct {
	histogram          []int
	histogramData      [250]int
	histogramDataIndex int
	candidate          int
}

func newHighestPeakAggregator(maxFilterLag int) *highestPeakAggregator {
	return &highestPeakAggregator{
		histogram: make([]int, maxFilterLag+1),
		candidate: -1,
	}
}

func (a *highestPeakAggregator) reset() {
	for i := range a.histogram {
		a.histogram[i] = 0
	}
	for i := range a.histogramData {
		a.histogramData[i] = 0
	}
	a.histogramDataIndex = 0
}

func (a *highestPeakAggregator) aggregate(lag int) {
	a.histogram[a.histogramData[a.histogramDataIndex]]--
	a.histogramData[a.histogramDataIndex] = lag
	a.histogram[a.histogramData[a.histogramDataIndex]]++
	a.histogramDataIndex = (a.histogramDataIndex + 1) % len(a.histogramData)

	best := 0
	for i, v := range a.histogram {
		if v > a.histogram[best] {
			best = i
		}
	}
	a.candidate = best
}

// preEchoLagAggregator is a windowed, decay-weighted histogram used to
// aggregate the pre-echo lag. C:
// MatchedFilterLagAggregator::PreEchoLagAggregator.
type preEchoLagAggregator struct {
	blockSizeLog2      int
	histogramData      [250]int
	histogram          []int
	histogramDataIndex int
	preEchoCandidate   int
	numberUpdates      int
}

func newPreEchoLagAggregator(maxFilterLag, downSamplingFactor int) *preEchoLagAggregator {
	a := &preEchoLagAggregator{
		blockSizeLog2: getDownSamplingBlockSizeLog2(downSamplingFactor),
		histogram:     make([]int, ((maxFilterLag+1)*downSamplingFactor)>>BlockSizeLog2),
	}
	a.reset()
	return a
}

func (a *preEchoLagAggregator) reset() {
	for i := range a.histogram {
		a.histogram[i] = 0
	}
	for i := range a.histogramData {
		a.histogramData[i] = preEchoHistogramDataNotUpdated
	}
	a.histogramDataIndex = 0
	a.preEchoCandidate = 0
}

func (a *preEchoLagAggregator) aggregate(preEchoLag int) {
	preEchoBlockSize := preEchoLag >> a.blockSizeLog2
	if preEchoBlockSize < 0 {
		preEchoBlockSize = 0
	} else if preEchoBlockSize > len(a.histogram)-1 {
		preEchoBlockSize = len(a.histogram) - 1
	}

	// Remove the oldest point from the histogram; ignore the initial
	// points where no updates have been done to histogramData.
	if a.histogramData[a.histogramDataIndex] != preEchoHistogramDataNotUpdated {
		a.histogram[a.histogramData[a.histogramDataIndex]]--
	}
	a.histogramData[a.histogramDataIndex] = preEchoBlockSize
	a.histogram[a.histogramData[a.histogramDataIndex]]++
	a.histogramDataIndex = (a.histogramDataIndex + 1) % len(a.histogramData)

	preEchoCandidateBlockSize := 0
	if a.numberUpdates < NumBlocksPerSecond*2 {
		a.numberUpdates++
		penalizationPerDelay := float32(1.0)
		maxHistogramValue := float32(-1.0)
		for start := 0; len(a.histogram)-start >= MatchedFilterWindowSizeSubBlocks; start += MatchedFilterWindowSizeSubBlocks {
			window := a.histogram[start : start+MatchedFilterWindowSizeSubBlocks]
			maxIdx := 0
			for i, v := range window {
				if v > window[maxIdx] {
					maxIdx = i
				}
			}
			weightedMaxValue := float32(window[maxIdx]) * penalizationPerDelay
			if weightedMaxValue > maxHistogramValue {
				maxHistogramValue = weightedMaxValue
				preEchoCandidateBlockSize = start + maxIdx
			}
			penalizationPerDelay *= 0.7
		}
	} else {
		best := 0
		for i, v := range a.histogram {
			if v > a.histogram[best] {
				best = i
			}
		}
		preEchoCandidateBlockSize = best
	}

	a.preEchoCandidate = preEchoCandidateBlockSize << a.blockSizeLog2
}

// MatchedFilterLagAggregator aggregates lag estimates produced by the
// MatchedFilter class into a single reliable combined lag estimate.
// Port of MatchedFilterLagAggregator.
type MatchedFilterLagAggregator struct {
	significantCandidateFound bool
	thresholds                config.DelaySelectionThresholds
	headroom                  int
	highestPeak               *highestPeakAggregator
	preEchoLag                *preEchoLagAggregator
}

// NewMatchedFilterLagAggregator mirrors
// MatchedFilterLagAggregator's C++ constructor (minus data_dumper).
func NewMatchedFilterLagAggregator(maxFilterLag int, delayConfig config.DelayConfig) *MatchedFilterLagAggregator {
	m := &MatchedFilterLagAggregator{
		thresholds:  delayConfig.DelaySelectionThresholds,
		headroom:    delayConfig.DelayHeadroomSamples / delayConfig.DownSamplingFactor,
		highestPeak: newHighestPeakAggregator(maxFilterLag),
	}
	if delayConfig.DetectPreEcho {
		m.preEchoLag = newPreEchoLagAggregator(maxFilterLag, delayConfig.DownSamplingFactor)
	}
	return m
}

// Reset resets the aggregator. C: MatchedFilterLagAggregator::Reset.
func (m *MatchedFilterLagAggregator) Reset(hardReset bool) {
	m.highestPeak.reset()
	if m.preEchoLag != nil {
		m.preEchoLag.reset()
	}
	if hardReset {
		m.significantCandidateFound = false
	}
}

// ReliableDelayFound reports whether a reliable delay estimate has
// been found. C: MatchedFilterLagAggregator::ReliableDelayFound.
func (m *MatchedFilterLagAggregator) ReliableDelayFound() bool {
	return m.significantCandidateFound
}

// GetDelayAtHighestPeak returns the delay candidate computed by
// looking at the highest peak on the matched filters. C:
// MatchedFilterLagAggregator::GetDelayAtHighestPeak.
func (m *MatchedFilterLagAggregator) GetDelayAtHighestPeak() int {
	return m.highestPeak.candidate
}

// Aggregate aggregates the provided lag estimate. C:
// MatchedFilterLagAggregator::Aggregate.
func (m *MatchedFilterLagAggregator) Aggregate(lagEstimate *LagEstimate) *DelayEstimate {
	if lagEstimate != nil && m.preEchoLag != nil {
		v := lagEstimate.PreEchoLag - m.headroom
		if v < 0 {
			v = 0
		}
		m.preEchoLag.aggregate(v)
	}

	if lagEstimate != nil {
		v := lagEstimate.Lag - m.headroom
		if v < 0 {
			v = 0
		}
		m.highestPeak.aggregate(v)

		histogram := m.highestPeak.histogram
		candidate := m.highestPeak.candidate
		m.significantCandidateFound = m.significantCandidateFound || histogram[candidate] > m.thresholds.Converged

		if histogram[candidate] > m.thresholds.Converged ||
			(histogram[candidate] > m.thresholds.Initial && !m.significantCandidateFound) {
			quality := DelayEstimateQualityCoarse
			if m.significantCandidateFound {
				quality = DelayEstimateQualityRefined
			}
			reportedDelay := candidate
			if m.preEchoLag != nil {
				reportedDelay = m.preEchoLag.preEchoCandidate
			}
			de := NewDelayEstimate(quality, reportedDelay)
			return &de
		}
	}

	return nil
}
