// This file ports modules/audio_processing/aec3/
// filter_analyzer.{h,cc}: analyzes the properties (delay, gain,
// shape-consistency) of an adaptive filter's time-domain impulse
// response, after passing it through a fixed high-pass pre-filter.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// findPeakIndex returns the index of the largest-squared-magnitude
// sample in filterTimeDomain within [startSample, endSample],
// seeded with peakIndexIn. C: (anonymous namespace) FindPeakIndex.
func findPeakIndex(filterTimeDomain []float32, peakIndexIn, startSample, endSample int) int {
	peakIndexOut := peakIndexIn
	maxH2 := filterTimeDomain[peakIndexOut] * filterTimeDomain[peakIndexOut]
	for k := startSample; k <= endSample; k++ {
		tmp := filterTimeDomain[k] * filterTimeDomain[k]
		if tmp > maxH2 {
			peakIndexOut = k
			maxH2 = tmp
		}
	}
	return peakIndexOut
}

// filterRegion is FilterAnalyzer::FilterRegion.
type filterRegion struct {
	startSample int
	endSample   int
}

// consistentFilterDetector checks whether the shape of the impulse
// response has been consistent over time. Port of
// FilterAnalyzer::ConsistentFilterDetector.
type consistentFilterDetector struct {
	significantPeak           bool
	filterFloorAccum          float32
	filterSecondaryPeak       float32
	filterFloorLowLimit       int
	filterFloorHighLimit      int
	activeRenderThreshold     float32
	consistentEstimateCounter int
	consistentDelayReference  int
}

// newConsistentFilterDetector mirrors ConsistentFilterDetector's C++
// constructor.
func newConsistentFilterDetector(config config.Config) *consistentFilterDetector {
	d := &consistentFilterDetector{
		activeRenderThreshold: config.RenderLevels.ActiveRenderLimit * config.RenderLevels.ActiveRenderLimit * FFTLengthBy2,
	}
	d.Reset()
	return d
}

// Reset resets the detector's state. C:
// ConsistentFilterDetector::Reset.
func (d *consistentFilterDetector) Reset() {
	d.significantPeak = false
	d.filterFloorAccum = 0
	d.filterSecondaryPeak = 0
	d.filterFloorLowLimit = 0
	d.filterFloorHighLimit = 0
	d.consistentEstimateCounter = 0
	d.consistentDelayReference = -10
}

// Detect analyzes filterToAnalyze over region and reports whether the
// filter shape has been consistent for long enough. C:
// ConsistentFilterDetector::Detect.
func (d *consistentFilterDetector) Detect(filterToAnalyze []float32, region filterRegion, xBlock *Block, peakIndex int, delayBlocks int) bool {
	if region.startSample == 0 {
		d.filterFloorAccum = 0
		d.filterSecondaryPeak = 0
		if peakIndex < 64 {
			d.filterFloorLowLimit = 0
		} else {
			d.filterFloorLowLimit = peakIndex - 64
		}
		if peakIndex > len(filterToAnalyze)-129 {
			d.filterFloorHighLimit = 0
		} else {
			d.filterFloorHighLimit = peakIndex + 128
		}
	}

	filterFloorAccum := d.filterFloorAccum
	filterSecondaryPeak := d.filterSecondaryPeak

	end1 := region.endSample + 1
	if d.filterFloorLowLimit < end1 {
		end1 = d.filterFloorLowLimit
	}
	for k := region.startSample; k < end1; k++ {
		absH := abs32(filterToAnalyze[k])
		filterFloorAccum += absH
		if absH > filterSecondaryPeak {
			filterSecondaryPeak = absH
		}
	}

	start2 := d.filterFloorHighLimit
	if region.startSample > start2 {
		start2 = region.startSample
	}
	for k := start2; k <= region.endSample; k++ {
		absH := abs32(filterToAnalyze[k])
		filterFloorAccum += absH
		if absH > filterSecondaryPeak {
			filterSecondaryPeak = absH
		}
	}
	d.filterFloorAccum = filterFloorAccum
	d.filterSecondaryPeak = filterSecondaryPeak

	if region.endSample == len(filterToAnalyze)-1 {
		filterFloor := d.filterFloorAccum / float32(d.filterFloorLowLimit+len(filterToAnalyze)-d.filterFloorHighLimit)
		absPeak := abs32(filterToAnalyze[peakIndex])
		d.significantPeak = absPeak > 10*filterFloor && absPeak > 2*d.filterSecondaryPeak
	}

	if d.significantPeak {
		activeRenderBlock := false
		for ch := 0; ch < xBlock.NumChannels(); ch++ {
			xChannel := xBlock.View(0, ch)
			xEnergy := float32(0)
			for _, v := range xChannel {
				xEnergy = mla(xEnergy, v, v)
			}
			if xEnergy > d.activeRenderThreshold {
				activeRenderBlock = true
				break
			}
		}

		if d.consistentDelayReference == delayBlocks {
			if activeRenderBlock {
				d.consistentEstimateCounter++
			}
		} else {
			d.consistentEstimateCounter = 0
			d.consistentDelayReference = delayBlocks
		}
	}
	return float32(d.consistentEstimateCounter) > 1.5*float32(NumBlocksPerSecond)
}

// filterAnalysisState is FilterAnalyzer::FilterAnalysisState.
type filterAnalysisState struct {
	gain                     float32
	peakIndex                int
	filterLengthBlocks       int
	consistentEstimate       bool
	consistentFilterDetector *consistentFilterDetector
}

// newFilterAnalysisState mirrors FilterAnalysisState's C++
// constructor.
func newFilterAnalysisState(config config.Config) *filterAnalysisState {
	s := &filterAnalysisState{
		filterLengthBlocks:       config.Filter.RefinedInitial.LengthBlocks,
		consistentFilterDetector: newConsistentFilterDetector(config),
	}
	s.Reset(config.EpStrength.DefaultGain)
	return s
}

// Reset mirrors FilterAnalysisState::Reset — note this leaves
// consistentEstimate/filterLengthBlocks untouched, matching the C++
// source exactly.
func (s *filterAnalysisState) Reset(defaultGain float32) {
	s.peakIndex = 0
	s.gain = defaultGain
	s.consistentFilterDetector.Reset()
}

// FilterAnalyzer analyzes the properties of an adaptive filter. Port
// of filter_analyzer.{h,cc}'s FilterAnalyzer (minus ApmDataDumper
// logging, dropped per this port's convention).
type FilterAnalyzer struct {
	boundedErl  bool
	defaultGain float32
	hHighpass   [][]float32 // [channel][sample]

	blocksSinceReset int
	region           filterRegion

	filterAnalysisStates []*filterAnalysisState
	filterDelaysBlocks   []int
	minFilterDelayBlocks int
}

// NewFilterAnalyzer mirrors FilterAnalyzer's C++ constructor.
func NewFilterAnalyzer(config config.Config, numCaptureChannels int) *FilterAnalyzer {
	f := &FilterAnalyzer{
		boundedErl:           config.EpStrength.BoundedErl,
		defaultGain:          config.EpStrength.DefaultGain,
		hHighpass:            make([][]float32, numCaptureChannels),
		filterAnalysisStates: make([]*filterAnalysisState, numCaptureChannels),
		filterDelaysBlocks:   make([]int, numCaptureChannels),
	}
	hpLen := GetTimeDomainLength(config.Filter.Refined.LengthBlocks)
	for ch := range f.hHighpass {
		f.hHighpass[ch] = make([]float32, hpLen)
		f.filterAnalysisStates[ch] = newFilterAnalysisState(config)
	}
	f.Reset()
	return f
}

// Reset resets the analysis. C: FilterAnalyzer::Reset.
func (f *FilterAnalyzer) Reset() {
	f.blocksSinceReset = 0
	f.resetRegion()
	for _, st := range f.filterAnalysisStates {
		st.Reset(f.defaultGain)
	}
	for i := range f.filterDelaysBlocks {
		f.filterDelaysBlocks[i] = 0
	}
}

func (f *FilterAnalyzer) resetRegion() {
	f.region.startSample = 0
	f.region.endSample = 0
}

// Update updates the estimates with new input data. C:
// FilterAnalyzer::Update.
func (f *FilterAnalyzer) Update(filtersTimeDomain [][]float32, renderBuffer *RenderBuffer, anyFilterConsistent *bool, maxEchoPathGain *float32) {
	f.blocksSinceReset++
	f.SetRegionToAnalyze(len(filtersTimeDomain[0]))
	f.analyzeRegion(filtersTimeDomain, renderBuffer)

	st0 := f.filterAnalysisStates[0]
	*anyFilterConsistent = st0.consistentEstimate
	*maxEchoPathGain = st0.gain
	f.minFilterDelayBlocks = f.filterDelaysBlocks[0]
	for ch := 1; ch < len(filtersTimeDomain); ch++ {
		st := f.filterAnalysisStates[ch]
		*anyFilterConsistent = *anyFilterConsistent || st.consistentEstimate
		if st.gain > *maxEchoPathGain {
			*maxEchoPathGain = st.gain
		}
		if f.filterDelaysBlocks[ch] < f.minFilterDelayBlocks {
			f.minFilterDelayBlocks = f.filterDelaysBlocks[ch]
		}
	}
}

// FilterDelaysBlocks returns the delay in blocks for each filter. C:
// FilterAnalyzer::FilterDelaysBlocks.
func (f *FilterAnalyzer) FilterDelaysBlocks() []int { return f.filterDelaysBlocks }

// MinFilterDelayBlocks returns the minimum delay of all filters in
// blocks. C: FilterAnalyzer::MinFilterDelayBlocks.
func (f *FilterAnalyzer) MinFilterDelayBlocks() int { return f.minFilterDelayBlocks }

// FilterLengthBlocks returns the number of blocks for the current
// used filter. C: FilterAnalyzer::FilterLengthBlocks.
func (f *FilterAnalyzer) FilterLengthBlocks() int {
	return f.filterAnalysisStates[0].filterLengthBlocks
}

// GetAdjustedFilters returns the preprocessed filter. C:
// FilterAnalyzer::GetAdjustedFilters.
func (f *FilterAnalyzer) GetAdjustedFilters() [][]float32 { return f.hHighpass }

func (f *FilterAnalyzer) analyzeRegion(filtersTimeDomain [][]float32, renderBuffer *RenderBuffer) {
	f.preProcessFilters(filtersTimeDomain)

	const oneByBlockSize = float32(1) / float32(BlockSize)
	for ch := 0; ch < len(filtersTimeDomain); ch++ {
		st := f.filterAnalysisStates[ch]
		if st.peakIndex > len(f.hHighpass[ch])-1 {
			st.peakIndex = len(f.hHighpass[ch]) - 1
		}

		st.peakIndex = findPeakIndex(f.hHighpass[ch], st.peakIndex, f.region.startSample, f.region.endSample)
		f.filterDelaysBlocks[ch] = st.peakIndex >> BlockSizeLog2
		f.updateFilterGain(f.hHighpass[ch], st)
		st.filterLengthBlocks = int(float32(len(filtersTimeDomain[ch])) * oneByBlockSize)

		st.consistentEstimate = st.consistentFilterDetector.Detect(
			f.hHighpass[ch], f.region, renderBuffer.GetBlock(-f.filterDelaysBlocks[ch]), st.peakIndex, f.filterDelaysBlocks[ch])
	}
}

// updateFilterGain mirrors FilterAnalyzer::UpdateFilterGain (a method
// on FilterAnalyzer in Go since it reads blocksSinceReset/boundedErl,
// unlike the C++ version which is also a FilterAnalyzer method).
func (f *FilterAnalyzer) updateFilterGain(filterTimeDomain []float32, st *filterAnalysisState) {
	sufficientTimeToConverge := f.blocksSinceReset > 5*NumBlocksPerSecond

	if sufficientTimeToConverge && st.consistentEstimate {
		st.gain = abs32(filterTimeDomain[st.peakIndex])
	} else if st.gain != 0 {
		g := abs32(filterTimeDomain[st.peakIndex])
		if g > st.gain {
			st.gain = g
		}
	}

	if f.boundedErl && st.gain != 0 {
		if st.gain < 0.01 {
			st.gain = 0.01
		}
	}
}

// preProcessFilters applies a fixed minimum-phase high-pass FIR
// (cutoff ~600 Hz) to filtersTimeDomain over the current region,
// storing the result into hHighpass. C: FilterAnalyzer::PreProcessFilters.
func (f *FilterAnalyzer) preProcessFilters(filtersTimeDomain [][]float32) {
	h := [3]float32{0.7929742, -0.36072128, -0.47047766}

	for ch := 0; ch < len(filtersTimeDomain); ch++ {
		newLen := len(filtersTimeDomain[ch])
		f.hHighpass[ch] = resizeF32Zero(f.hHighpass[ch], newLen)
		hHighpassCh := f.hHighpass[ch]
		filtersTimeDomainCh := filtersTimeDomain[ch]

		for k := f.region.startSample; k <= f.region.endSample; k++ {
			hHighpassCh[k] = 0
		}

		start := len(h) - 1
		if f.region.startSample > start {
			start = f.region.startSample
		}
		for k := start; k <= f.region.endSample; k++ {
			tmp := hHighpassCh[k]
			for j := 0; j < len(h); j++ {
				tmp = mla(tmp, filtersTimeDomainCh[k-j], h[j])
			}
			hHighpassCh[k] = tmp
		}
	}
}

// SetRegionToAnalyze advances the sliding analysis window by one
// block, wrapping around at filterSize. Public for testing purposes
// only, matching the C++ API. C: FilterAnalyzer::SetRegionToAnalyze.
func (f *FilterAnalyzer) SetRegionToAnalyze(filterSize int) {
	const numberBlocksToUpdate = 1
	r := &f.region
	if r.endSample >= filterSize-1 {
		r.startSample = 0
	} else {
		r.startSample = r.endSample + 1
	}
	end := r.startSample + numberBlocksToUpdate*BlockSize - 1
	if end > filterSize-1 {
		end = filterSize - 1
	}
	r.endSample = end
}
