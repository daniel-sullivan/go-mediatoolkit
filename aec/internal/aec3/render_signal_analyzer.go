// This file ports modules/audio_processing/aec3/
// render_signal_analyzer.{h,cc}: analyzes the render signal to detect
// narrow-band spectral components (which harm the NLMS-style gain
// updates) so that the filter update gains can mask the corresponding
// frequency bins.
package aec3

import "github.com/daniel-sullivan/go-mediatoolkit/aec/config"

// narrowBandCounterThreshold is (anonymous namespace)::kCounterThreshold.
const narrowBandCounterThreshold = 5

// identifySmallNarrowBandRegions updates narrowBandCounters (length
// FFTLengthBy2Minus1) from the render buffer's spectrum at
// delayPartitions blocks offset (nil mirrors std::optional's empty
// state: counters are simply zeroed). C: (anonymous namespace)
// IdentifySmallNarrowBandRegions.
func identifySmallNarrowBandRegions(renderBuffer *RenderBuffer, delayPartitions *int, narrowBandCounters *[FFTLengthBy2Minus1]int) {
	if delayPartitions == nil {
		for i := range narrowBandCounters {
			narrowBandCounters[i] = 0
		}
		return
	}

	var channelCounters [FFTLengthBy2Minus1]int
	x2 := renderBuffer.Spectrum(*delayPartitions)
	for _, x2Ch := range x2 {
		for k := 1; k < FFTLengthBy2; k++ {
			neighborMax := x2Ch[k-1]
			if x2Ch[k+1] > neighborMax {
				neighborMax = x2Ch[k+1]
			}
			if x2Ch[k] > 3*neighborMax {
				channelCounters[k-1]++
			}
		}
	}
	for k := 1; k < FFTLengthBy2; k++ {
		if channelCounters[k-1] > 0 {
			narrowBandCounters[k-1]++
		} else {
			narrowBandCounters[k-1] = 0
		}
	}
}

// identifyStrongNarrowBandComponent updates narrowPeakBand (-1 mirrors
// std::optional's empty state)/narrowPeakCounter from the render
// buffer's most recent block and spectrum, respecting
// strongPeakFreezeDuration. C: (anonymous namespace)
// IdentifyStrongNarrowBandComponent.
func identifyStrongNarrowBandComponent(renderBuffer *RenderBuffer, strongPeakFreezeDuration int, narrowPeakBand *int, narrowPeakCounter *int) {
	if *narrowPeakBand != -1 {
		*narrowPeakCounter++
		if *narrowPeakCounter > strongPeakFreezeDuration {
			*narrowPeakBand = -1
		}
	}

	block := renderBuffer.GetBlock(0)
	maxPeakLevel := float32(0)
	for ch := 0; ch < block.NumChannels(); ch++ {
		x2Latest := renderBuffer.Spectrum(0)[ch]

		peakBin := 0
		for k := 1; k < len(x2Latest); k++ {
			if x2Latest[k] > x2Latest[peakBin] {
				peakBin = k
			}
		}

		nonPeakPower := float32(0)
		start := peakBin - 14
		if start < 0 {
			start = 0
		}
		for k := start; k < peakBin-4; k++ {
			if x2Latest[k] > nonPeakPower {
				nonPeakPower = x2Latest[k]
			}
		}
		end := peakBin + 15
		if end > FFTLengthBy2Plus1 {
			end = FFTLengthBy2Plus1
		}
		for k := peakBin + 5; k < end; k++ {
			if x2Latest[k] > nonPeakPower {
				nonPeakPower = x2Latest[k]
			}
		}

		band0 := block.View(0, ch)
		minV, maxV := band0[0], band0[0]
		for _, v := range band0 {
			if v < minV {
				minV = v
			}
			if v > maxV {
				maxV = v
			}
		}
		maxAbs := max32(abs32(minV), abs32(maxV))

		if block.NumBands() > 1 {
			band1 := block.View(1, ch)
			minV1, maxV1 := band1[0], band1[0]
			for _, v := range band1 {
				if v < minV1 {
					minV1 = v
				}
				if v > maxV1 {
					maxV1 = v
				}
			}
			maxAbs = max32(maxAbs, max32(abs32(minV1), abs32(maxV1)))
		}

		peakLevel := x2Latest[peakBin]
		if peakBin > 0 && maxAbs > 100 && peakLevel > 100*nonPeakPower {
			if peakLevel > maxPeakLevel {
				maxPeakLevel = peakLevel
				*narrowPeakBand = peakBin
				*narrowPeakCounter = 0
			}
		}
	}
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// RenderSignalAnalyzer identifies narrow-band regions of the render
// signal that require special caution during the filter adaptation.
// Port of render_signal_analyzer.{h,cc}'s RenderSignalAnalyzer.
type RenderSignalAnalyzer struct {
	strongPeakFreezeDuration int
	narrowBandCounters       [FFTLengthBy2Minus1]int
	narrowPeakBand           int // -1 mirrors std::optional's empty state
	narrowPeakCounter        int
}

// NewRenderSignalAnalyzer mirrors RenderSignalAnalyzer's C++
// constructor.
func NewRenderSignalAnalyzer(config config.Config) *RenderSignalAnalyzer {
	return &RenderSignalAnalyzer{
		strongPeakFreezeDuration: config.Filter.Refined.LengthBlocks,
		narrowPeakBand:           -1,
	}
}

// Update analyzes the current render signal. delayPartitions mirrors
// C++'s std::optional<size_t>: pass nil for "no value". C:
// RenderSignalAnalyzer::Update.
func (r *RenderSignalAnalyzer) Update(renderBuffer *RenderBuffer, delayPartitions *int) {
	identifySmallNarrowBandRegions(renderBuffer, delayPartitions, &r.narrowBandCounters)
	identifyStrongNarrowBandComponent(renderBuffer, r.strongPeakFreezeDuration, &r.narrowPeakBand, &r.narrowPeakCounter)
}

// MaskRegionsAroundNarrowBands zeroes the elements of v that lie in
// narrow-band regions detected in the render signal spectrum. C:
// RenderSignalAnalyzer::MaskRegionsAroundNarrowBands.
func (r *RenderSignalAnalyzer) MaskRegionsAroundNarrowBands(v *[FFTLengthBy2Plus1]float32) {
	if r.narrowBandCounters[0] > narrowBandCounterThreshold {
		v[0] = 0
		v[1] = 0
	}
	for k := 2; k < FFTLengthBy2-1; k++ {
		if r.narrowBandCounters[k-1] > narrowBandCounterThreshold {
			v[k-2] = 0
			v[k-1] = 0
			v[k] = 0
			v[k+1] = 0
			v[k+2] = 0
		}
	}
	if r.narrowBandCounters[FFTLengthBy2-2] > narrowBandCounterThreshold {
		v[FFTLengthBy2] = 0
		v[FFTLengthBy2-1] = 0
	}
}

// PoorSignalExcitation reports whether the render signal contains any
// narrow-band region. C: RenderSignalAnalyzer::PoorSignalExcitation.
func (r *RenderSignalAnalyzer) PoorSignalExcitation() bool {
	for _, c := range r.narrowBandCounters {
		if c > 10 {
			return true
		}
	}
	return false
}

// NarrowPeakBand returns the currently detected narrow peak band, or
// nil if none. C: RenderSignalAnalyzer::NarrowPeakBand.
func (r *RenderSignalAnalyzer) NarrowPeakBand() *int {
	if r.narrowPeakBand == -1 {
		return nil
	}
	v := r.narrowPeakBand
	return &v
}
